# 技术方案：E-1 错误处理治理

> Phase 1（E-1.1 Whip / E-1.2 Serve）+ Phase 2（E-1.3 Store）
>
> 优先级：P0

---

## 1. 问题分析

代码中存在大量 `_ = db.*()` 静默吞没错误，分布如下：

### 1.1 Whip 层（`internal/whip/`）— 31 处

**liveness.go（6 处）：**

| 行号 | 代码 | 严重程度 | 分析 |
|------|------|---------|------|
| 32 | `_ = w.db.UpdateMinisterHeartbeat(m.ID)` | **中** | 心跳更新失败→minister 会被误判为 stuck |
| 43 | `_ = w.db.UpdateMinisterStatus(m.ID, "stuck")` | **高** | 标记 stuck 失败→永远不会触发 by-election |
| 44 | `_ = w.db.RecordEvent(...)` | 低 | 审计记录丢失，不影响核心流程 |
| 73 | `_ = w.db.RecordEvent(...)` | 低 | 同上 |
| 164 | `_ = w.db.RecordEvent(...)` | 低 | 同上 |

**poller.go（16 处）：**

| 行号 | 代码 | 严重程度 | 分析 |
|------|------|---------|------|
| 80 | `_ = w.db.RecordEvent("bill.enacted", ...)` | 低 | 审计记录 |
| 86 | `_ = os.Remove(donePath)` | **中** | 删除失败→下次 tick 重复处理 |
| 99 | `_ = w.db.UpdateMinisterStatus(m.ID, "idle")` | **高** | 标记 idle 失败→minister 永远 working |
| 173 | `_ = w.db.RecordEvent(...)` | 低 | 审计记录 |
| 220-226 | `_ = w.db.UpdateBillStatus / UnassignBill / UpdateMinisterStatus` | **高** | 审查结果写入失败→状态不一致 |
| 227 | `_ = os.Remove(reviewPath)` | **中** | 同 donePath |
| 236 | `_ = w.db.RecordEvent(...)` | 低 | 审计记录 |
| 248 | `_ = w.db.CreateHansard(h)` | **中** | Hansard 丢失 |
| 260 | `_ = w.db.CreateGazette(g)` | **中** | Gazette 丢失 |
| 313 | `_ = w.db.UpdateMinisterStatus(...)` | **高** | ACK 后标记 idle 失败 |
| 319 | `_ = os.Remove(ackPath)` | 低 | 清理失败 |
| 353 | `_ = w.db.UpdateHansardMetrics(...)` | 低 | 度量更新丢失 |

**scheduler.go（14 处）：**

| 行号 | 代码 | 严重程度 | 分析 |
|------|------|---------|------|
| 117-118 | `_ = w.db.UpdateSessionStatus / RecordEvent` | **高** | 会期完成标记失败 |
| 129 | `_ = w.db.UpdateSessionStatus(...)` | **高** | 同上 |
| 157-159 | `_ = w.db.RecordEvent / UpdateSessionStatus / RecordEvent` | **高** | 合并成功后状态更新失败 |
| 176 | `_ = w.db.RecordEvent(...)` | 低 | 审计记录 |
| 245 | `_ = w.db.RecordEvent(...)` | 低 | 审计记录 |
| 420-421 | `_ = w.db.UpdateMinisterStatus / RecordEvent` | **高** | autoscale 激活失败 |
| 428 | `_ = w.db.CreateGazette(g)` | **中** | Gazette 丢失 |
| 438-440 | `_ = w.db.RecordEvent / UpdateMinisterStatus` | **高** | autoscale 缩容失败 |
| 447 | `_ = w.db.CreateGazette(g)` | **中** | Gazette 丢失 |

**dispatch.go（2 处）：**

| 行号 | 代码 | 严重程度 | 分析 |
|------|------|---------|------|
| 81 | `_ = os.WriteFile(signalPath, ...)` | 低 | 信号文件写入失败，minister 稍后也会轮询 |
| 84 | `_ = w.db.RecordEvent(...)` | 低 | 审计记录 |

### 1.2 Serve 层（`cmd/serve.go`）— 9 处

| 行号 | 代码 | 严重程度 | 分析 |
|------|------|---------|------|
| 282 | `_ = db.RecordEvent("minister.summoned", ...)` | 低 | 审计记录 |
| 345 | `_ = json.NewDecoder(r.Body).Decode(&req)` | **高** | 解析失败静默忽略→质量/备注全为零值 |
| 369 | `_ = db.CreateHansard(h)` | **中** | Hansard 丢失 |
| 383 | `_ = db.CreateGazette(g)` | **中** | 完成 Gazette 丢失 |
| 453 | `_ = db.RecordEvent(...)` | 低 | 审计记录 |
| 471 | `_ = db.CreateBill(b)` | **高** | Bill 创建失败但返回 "processed" |
| 472 | `_ = db.RecordEvent(...)` | 低 | 审计记录 |
| 495 | `_ = db.CreateBill(b)` | **高** | 同 471 |
| 496 | `_ = db.RecordEvent(...)` | 低 | 审计记录 |

---

## 2. 治理方案

### 2.1 分级策略

| 级别 | 处理方式 | 适用场景 |
|------|---------|---------|
| **关键路径** | 返回 error / 中断操作 / HTTP 500 | 状态变更（bill/minister/session status）、数据创建（bill/gazette） |
| **辅助路径** | `slog.Warn` 记录，不中断 | 审计记录（RecordEvent）、Hansard 写入、Gazette 创建（非核心） |
| **可忽略** | `_ =` 保留，加 `// best-effort:` 注释 | 文件清理（os.Remove）、信号文件写入 |

### 2.2 E-1.1 Whip 错误治理（Phase 1）

#### liveness.go 改造

```go
// threeLineWhip — Pass 1: 标记 stuck
for _, m := range working {
    if w.isMinisterAlive(m) {
        if err := w.db.UpdateMinisterHeartbeat(m.ID); err != nil {
            slog.Warn("更新心跳失败", "minister_id", m.ID, "err", err)
        }
        continue
    }

    if m.Heartbeat.Valid && time.Since(m.Heartbeat.Time) < gracePeriod {
        continue
    }

    // 关键路径：标记 stuck
    if err := w.db.UpdateMinisterStatus(m.ID, "stuck"); err != nil {
        slog.Error("标记 stuck 失败，跳过本次检查", "minister_id", m.ID, "err", err)
        continue // 不能继续处理这个 minister
    }
    slog.Warn("部长无响应，标记为 stuck", "minister_id", m.ID)
    // best-effort: 审计记录
    if err := w.db.RecordEvent("minister.stuck", "whip", "", m.ID, "", 
        fmt.Sprintf(`{"reason":"heartbeat_timeout"}`)); err != nil {
        slog.Warn("记录 stuck 事件失败", "minister_id", m.ID, "err", err)
    }
}
```

#### poller.go 改造——pollDoneFiles

```go
// 关键路径：bill enacted 后标记 minister idle
if !hasActive {
    if err := w.db.UpdateMinisterStatus(m.ID, "idle"); err != nil {
        slog.Error("标记 idle 失败", "minister_id", m.ID, "err", err)
        // 不 continue——其他 minister 仍需处理
    } else {
        slog.Info("部长已完成所有议案，标记为 idle", "minister_id", m.ID)
    }
}
```

#### poller.go 改造——pollReviewFile

```go
// 关键路径：审查结果必须全部写入成功
if pass {
    if err := w.db.UpdateBillStatus(bill.ID, "enacted"); err != nil {
        slog.Error("审查通过但更新状态失败", "bill_id", bill.ID, "err", err)
        return // 不执行后续操作，等下次 tick 重试
    }
} else {
    if err := w.db.UpdateBillStatus(bill.ID, "draft"); err != nil {
        slog.Error("审查失败但重置状态失败", "bill_id", bill.ID, "err", err)
        return
    }
}
if err := w.db.UnassignBill(bill.ID); err != nil {
    slog.Error("取消分配失败", "bill_id", bill.ID, "err", err)
}
if err := w.db.UpdateMinisterStatus(reviewerID, "idle"); err != nil {
    slog.Error("标记 reviewer idle 失败", "minister_id", reviewerID, "err", err)
}
// best-effort: 清理文件
_ = os.Remove(reviewPath) // best-effort: 清理 review 文件，失败不影响
```

#### scheduler.go 改造——autoscale

```go
// 关键路径：autoscale 激活必须成功
if pendingBills > idle*upThresh && pendingBills > 0 {
    reservePool, err := w.db.ListOfflineMinisters()
    if err != nil {
        slog.Warn("autoscale: 拉取预备池失败", "err", err)
    } else if len(reservePool) > 0 {
        m := reservePool[0]
        if err := w.db.UpdateMinisterStatus(m.ID, "idle"); err != nil {
            slog.Error("autoscale: 激活部长失败", "minister_id", m.ID, "err", err)
        } else {
            slog.Info("autoscale: 从预备池激活部长", "minister_id", m.ID,
                "pending_bills", pendingBills, "idle", idle)
            // 辅助路径
            if err := w.db.RecordEvent(...); err != nil {
                slog.Warn("autoscale: 记录事件失败", "err", err)
            }
            // 辅助路径
            if err := w.db.CreateGazette(g); err != nil {
                slog.Warn("autoscale: 创建 Gazette 失败", "err", err)
            }
        }
    }
}
```

#### scheduler.go 改造——privyAutoMerge

```go
// 关键路径：会期完成标记
if err := w.db.UpdateSessionStatus(sess.ID, "completed"); err != nil {
    slog.Error("标记会期完成失败", "session_id", sess.ID, "err", err)
    return // 不发事件，等下次 tick
}
// 辅助路径
if err := w.db.RecordEvent("session.completed", ...); err != nil {
    slog.Warn("记录会期完成事件失败", "session_id", sess.ID, "err", err)
}
```

### 2.3 E-1.2 Serve 错误治理（Phase 1）

#### Webhook handler——Bill 创建必须返回错误

```go
case "push":
    commits, _ := payload["commits"].([]interface{})
    if len(commits) > 0 {
        firstCommit, _ := commits[0].(map[string]interface{})
        message, _ := firstCommit["message"].(string)
        if message != "" {
            billID := shortID("bill")
            b := &store.Bill{
                ID:     billID,
                Title:  truncate(message, 100),
                Status: "draft",
                DependsOn: store.NullString("[]"),
            }
            if err := db.CreateBill(b); err != nil {
                slog.Error("webhook: 创建 bill 失败", "err", err)
                writeError(w, http.StatusInternalServerError, "failed to create bill")
                return
            }
            // 辅助路径
            if err := db.RecordEvent("bill.created", "webhook", billID, "", "", 
                fmt.Sprintf(`{"event":"push"}`)); err != nil {
                slog.Warn("webhook: 记录事件失败", "bill_id", billID, "err", err)
            }
        }
    }
```

#### Bill enacted handler——解析 body 不能静默忽略

```go
if action == "enacted" {
    var req struct {
        Quality  float64 `json:"quality"`
        Notes    string  `json:"notes"`
        Duration int     `json:"duration"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
        return
    }
    // ...后续 Hansard 和 Gazette 改为 slog.Warn
    if ministerID != "" {
        h := &store.Hansard{...}
        if err := db.CreateHansard(h); err != nil {
            slog.Warn("enacted: 创建 Hansard 失败", "bill_id", billID, "err", err)
        }
    }
    g := &store.Gazette{...}
    if err := db.CreateGazette(g); err != nil {
        slog.Warn("enacted: 创建完成 Gazette 失败", "bill_id", billID, "err", err)
    }
}
```

### 2.4 E-1.3 Store 层治理（Phase 2）

Store 层 `migrate()` 中的 `_, _ = db.conn.Exec(ALTER TABLE ...)` 是**合理的 best-effort**（列已存在时 ALTER 会失败，这是预期行为）。

**改造方案**：不改逻辑，加注释说明：

```go
// best-effort: ALTER TABLE 在列已存在时会失败，这是预期行为（幂等迁移）
_, _ = db.conn.Exec(`ALTER TABLE bills ADD COLUMN portfolio TEXT DEFAULT ''`)
```

---

## 3. Whip 自动恢复梯度（E-1.1 补充）

### 3.1 动机

当前 `threeLineWhip()` 的恢复路径过于简单：
```
working → (grace period 30s) → stuck → (stuck threshold 5min) → byElection
```

byElection 代价高昂（stash + 重置 bill + 重新分配），很多临时卡顿的 minister 不需要走到这一步。

### 3.2 新增：三级恢复梯度

```
working → (grace period 30s) → stuck
  → recovery_attempts=0: 发 Gazette 提醒 checkpoint（2 min 观察期）
  → recovery_attempts=1: 标记 bill at-risk + 发 Gazette 请求自检（3 min 观察期）
  → recovery_attempts≥2: 触发 byElection
```

### 3.3 数据库变更

```sql
ALTER TABLE ministers ADD COLUMN recovery_attempts INTEGER DEFAULT 0;
```

在 `store.go` `migrate()` 中追加：

```go
// v0.3: Whip 恢复梯度
_, _ = db.conn.Exec(`ALTER TABLE ministers ADD COLUMN recovery_attempts INTEGER DEFAULT 0`)
```

`Minister` struct 追加：

```go
type Minister struct {
    // ...existing fields...
    RecoveryAttempts int // v0.3: 恢复尝试次数
}
```

### 3.4 Store 新增方法

```go
// IncrementRecoveryAttempts increments the recovery attempt counter and returns new value.
func (db *DB) IncrementRecoveryAttempts(ministerID string) (int, error) {
    _, err := db.conn.Exec(
        `UPDATE ministers SET recovery_attempts = recovery_attempts + 1 WHERE id = ?`,
        ministerID,
    )
    if err != nil {
        return 0, err
    }
    var count int
    err = db.conn.QueryRow(
        `SELECT recovery_attempts FROM ministers WHERE id = ?`, ministerID,
    ).Scan(&count)
    return count, err
}

// ResetRecoveryAttempts resets the recovery counter (called on byElection or recovery).
func (db *DB) ResetRecoveryAttempts(ministerID string) error {
    _, err := db.conn.Exec(
        `UPDATE ministers SET recovery_attempts = 0 WHERE id = ?`, ministerID,
    )
    return err
}
```

### 3.5 threeLineWhip 改造

```go
// Pass 2: stuck ministers — graduated recovery
for _, m := range stuck {
    if m.Heartbeat.Valid && time.Since(m.Heartbeat.Time) < stuckThreshold {
        continue
    }

    attempts, err := w.db.IncrementRecoveryAttempts(m.ID)
    if err != nil {
        slog.Error("恢复计数更新失败", "minister_id", m.ID, "err", err)
        continue
    }

    switch {
    case attempts <= 1:
        // Level 1: 发 Gazette 提醒 checkpoint
        slog.Warn("部长 stuck，发送 checkpoint 提醒", "minister_id", m.ID, 
            "attempt", attempts)
        g := &store.Gazette{
            ID:           gazetteID(),
            ToMinister:   store.NullString(m.ID),
            Type:         store.NullString("recovery"),
            Summary:      fmt.Sprintf("党鞭提醒：检测到您可能卡住（第 %d 次）。请做 checkpoint 保存进度。如已恢复请忽略。", attempts),
            FromMinister: store.NullString("whip"),
        }
        if err := w.db.CreateGazette(g); err != nil {
            slog.Warn("创建恢复 Gazette 失败", "err", err)
        }

    case attempts == 2:
        // Level 2: 标记 bill at-risk，请求自检
        slog.Warn("部长 stuck 持续，标记 at-risk", "minister_id", m.ID, 
            "attempt", attempts)
        bills, _ := w.db.GetBillsByAssignee(m.ID)
        for _, bill := range bills {
            if bill.Status == "reading" {
                // 辅助路径：记录 at-risk 状态
                if err := w.db.RecordEvent("bill.at_risk", "whip", bill.ID, m.ID, 
                    bill.SessionID.String, ""); err != nil {
                    slog.Warn("记录 at-risk 事件失败", "err", err)
                }
            }
        }
        g := &store.Gazette{
            ID:           gazetteID(),
            ToMinister:   store.NullString(m.ID),
            Type:         store.NullString("recovery"),
            Summary:      fmt.Sprintf("党鞭警告：第 %d 次检测到您卡住。下一次检测将触发补选。请立即做 checkpoint 或 done。", attempts),
            FromMinister: store.NullString("whip"),
        }
        if err := w.db.CreateGazette(g); err != nil {
            slog.Warn("创建恢复 Gazette 失败", "err", err)
        }

    default:
        // Level 3: 触发 byElection
        slog.Warn("部长 stuck 超限，触发补选", "minister_id", m.ID, 
            "attempts", attempts, "threshold", stuckThreshold)
        w.byElection(m)
        if err := w.db.ResetRecoveryAttempts(m.ID); err != nil {
            slog.Warn("重置恢复计数失败", "minister_id", m.ID, "err", err)
        }
    }
}
```

### 3.6 恢复成功时重置

在 `threeLineWhip` Pass 1 中，如果之前 stuck 的 minister 恢复了心跳：

```go
for _, m := range working {
    if w.isMinisterAlive(m) {
        if err := w.db.UpdateMinisterHeartbeat(m.ID); err != nil {
            slog.Warn("更新心跳失败", "minister_id", m.ID, "err", err)
        }
        // 恢复心跳→重置恢复计数
        if m.RecoveryAttempts > 0 {
            if err := w.db.ResetRecoveryAttempts(m.ID); err != nil {
                slog.Warn("重置恢复计数失败", "minister_id", m.ID, "err", err)
            }
            slog.Info("部长已恢复，重置恢复计数", "minister_id", m.ID)
        }
        continue
    }
}
```

---

## 4. 测试计划

### 4.1 错误治理测试（配合 B-1/B-2）

| 测试 | 验证点 |
|------|--------|
| `TestThreeLineWhip_StuckMarkFails` | UpdateMinisterStatus 失败时不触发 byElection |
| `TestPollDoneFiles_EnactFails_SkipsBill` | EnactBillFromDone 失败时跳过该 bill，继续处理其他 |
| `TestPollReviewFile_UpdateFails_NoSideEffect` | UpdateBillStatus 失败时不执行 UnassignBill/UpdateMinisterStatus |
| `TestAutoscale_ActivateFails_NoGazette` | 激活失败时不发 Gazette |
| `TestWebhook_CreateBillFails_Returns500` | Bill 创建失败返回 HTTP 500，不是 "processed" |

### 4.2 恢复梯度测试

| 测试 | 验证点 |
|------|--------|
| `TestRecoveryGradient_Level1_SendsGazette` | 第 1 次 stuck 发 checkpoint Gazette |
| `TestRecoveryGradient_Level2_MarksAtRisk` | 第 2 次 stuck 记录 at-risk 事件 |
| `TestRecoveryGradient_Level3_ByElection` | 第 3 次 stuck 触发 byElection |
| `TestRecoveryGradient_ResetOnRecovery` | minister 恢复后 recovery_attempts 归零 |

---

## 5. 实施步骤

```
E-1.1 Whip 错误治理（1 PR）
  1. store: 新增 recovery_attempts 字段 + IncrementRecoveryAttempts / ResetRecoveryAttempts
  2. liveness.go: 替换所有关键路径 `_ =` 为错误处理 + 实现恢复梯度
  3. poller.go: 替换所有关键路径 `_ =` 为错误处理
  4. scheduler.go: 替换所有关键路径 `_ =` 为错误处理
  5. dispatch.go: 保留 best-effort 加注释
  6. 测试

E-1.2 Serve 错误治理（1 PR）
  1. serve.go: webhook handler Bill 创建返回 500
  2. serve.go: enacted handler body 解析失败返回 400
  3. serve.go: 辅助路径改为 slog.Warn
  4. 测试

E-1.3 Store 错误治理（1 PR）
  1. store.go migrate(): 加 best-effort 注释
  2. 审计所有 Store public 方法，确保错误向上传播
```

---

## 6. 变更文件清单

| 文件 | 变更类型 |
|------|---------|
| `internal/store/store.go` | 新增 recovery_attempts 字段 + 2 个方法 + 注释 |
| `internal/whip/liveness.go` | 错误处理 + 恢复梯度 |
| `internal/whip/poller.go` | 错误处理 |
| `internal/whip/scheduler.go` | 错误处理 |
| `internal/whip/dispatch.go` | 注释 |
| `internal/whip/whip_test.go` | 新增错误路径 + 恢复梯度测试 |
| `cmd/serve.go` | 错误处理 |
| `cmd/serve_test.go` | 新增 webhook 错误路径测试 |
