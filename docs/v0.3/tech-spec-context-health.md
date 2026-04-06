# 技术方案：Minister 上下文健康监控

> Phase 2 | 优先级：P1
>
> 来源：Claude Code 源码分析——自动监控消息列表大小 + 阈值触发压缩

---

## 1. 问题分析

### 1.1 现状

Minister 在长任务中 token 耗尽，Whip 无法感知：

```
Minister 执行 Bill
  → 持续产生对话 turn
  → Context Window 逐渐填满
  → 接近 token 上限
  → AI runtime 自动压缩/截断（或失败）
  → Minister 输出质量下降或直接卡死
  → Whip 只能等 stuck threshold（5min）才发现
```

**核心问题**：Whip 对 Minister 的"内部健康状态"完全盲区，只有外部心跳（进程存活）检查。

### 1.2 Claude Code 的做法

Claude Code 的三层上下文管理：
1. 主动压缩（接近阈值时自动折叠旧消息）
2. 反应式压缩（溢出后紧急压缩）
3. max_tokens 加倍（截断后增加上限重试）

HOC 无法控制 Minister 的 AI runtime 内部行为，但可以**监控**并**提前干预**。

---

## 2. 方案设计

### 2.1 Gazette 协议扩展

在 Gazette 的 payload 中增加 `context_health` 结构：

```go
// internal/store/gazette.go

// ContextHealth reports the minister's AI runtime context consumption.
// Included in periodic status gazettes sent by the minister agent.
type ContextHealth struct {
    TokensUsed  int `json:"tokens_used"`  // 已使用 token 数
    TokensLimit int `json:"tokens_limit"` // 上限
    TurnsElapsed int `json:"turns_elapsed"` // 已经历的对话轮次
}
```

**不修改 Gazette 表结构**——`context_health` 通过现有 `payload` JSON 字段传输：

```json
{
  "context_health": {
    "tokens_used": 85000,
    "tokens_limit": 100000,
    "turns_elapsed": 42
  }
}
```

### 2.2 Minister 端：心跳 Gazette 中附带 context_health

Minister 的 CLAUDE.md 行为规范中增加指示，要求 Minister agent 在写 gazette 时附带 context 信息。

在 `cmd/ministers.go` 的 `buildMinisterCLAUDE()` 中增加：

```markdown
## 上下文健康报告

每次写入 gazette 时，在 payload 中附带当前 context 状态：
- tokens_used: 你当前已使用的 token 数（估算）
- tokens_limit: 你的 token 上限
- turns_elapsed: 你当前对话的轮次数

示例 gazette payload:
```toml
[context_health]
tokens_used = 85000
tokens_limit = 100000
turns_elapsed = 42
```
```

### 2.3 Whip 端：tick 中检查 context_health

在 `whip.go` 的 `tick()` 中新增 `checkContextHealth()` 调用：

```go
func (w *Whip) tick() {
    ctx, cancel := context.WithTimeout(context.Background(), tickTimeout)
    defer cancel()

    _, span := w.tracer.Start(ctx, "whip.tick")
    defer span.End()

    w.threeLineWhip()
    w.orderPaper()
    w.pollDoneFiles()
    w.pollAckFiles()
    w.pollIdleMinisterReassign()
    w.committeeAutomation()
    w.checkContextHealth()  // 新增
    w.gazetteDispatch()
    w.autoscale()
}
```

### 2.4 checkContextHealth 实现

```go
// internal/whip/liveness.go（或新文件 context_health.go）

const (
    contextHealthWarnRatio = 0.80  // 80% → 发提醒
    contextHealthCritRatio = 0.90  // 90% → 标记 at-risk
)

// checkContextHealth inspects the latest gazette from each working minister
// for context_health data. When token usage approaches the limit, the Whip
// proactively intervenes.
func (w *Whip) checkContextHealth() {
    working, err := w.db.ListWorkingMinisters()
    if err != nil {
        slog.Warn("checkContextHealth: list working ministers", "err", err)
        return
    }

    for _, m := range working {
        health, err := w.db.GetLatestContextHealth(m.ID)
        if err != nil || health == nil {
            continue // 无 context_health 数据，跳过
        }

        if health.TokensLimit <= 0 {
            continue // 无上限信息
        }

        ratio := float64(health.TokensUsed) / float64(health.TokensLimit)

        switch {
        case ratio >= contextHealthCritRatio:
            // 90%+ → 标记 at-risk，发紧急 Gazette
            slog.Warn("部长 context 接近上限",
                "minister_id", m.ID,
                "tokens_used", health.TokensUsed,
                "tokens_limit", health.TokensLimit,
                "ratio", fmt.Sprintf("%.1f%%", ratio*100),
            )

            // 记录 at-risk 事件
            bills, _ := w.db.GetBillsByAssignee(m.ID)
            for _, bill := range bills {
                if bill.Status == "reading" {
                    if err := w.db.RecordEvent("bill.context_critical", "whip",
                        bill.ID, m.ID, bill.SessionID.String,
                        fmt.Sprintf(`{"ratio":%.2f,"tokens_used":%d}`, ratio, health.TokensUsed),
                    ); err != nil {
                        slog.Warn("记录 context_critical 事件失败", "err", err)
                    }
                }
            }

            g := &store.Gazette{
                ID:           gazetteID(),
                ToMinister:   store.NullString(m.ID),
                Type:         store.NullString("recovery"),
                Summary: fmt.Sprintf(
                    "紧急：你的 context 已使用 %.0f%%（%d/%d tokens）。"+
                        "请立即做 checkpoint（写 .done 文件保存当前进度），"+
                        "或总结已完成部分并请求拆分 Bill。",
                    ratio*100, health.TokensUsed, health.TokensLimit,
                ),
                FromMinister: store.NullString("whip"),
            }
            if err := w.db.CreateGazette(g); err != nil {
                slog.Warn("创建 context critical gazette 失败", "err", err)
            }

        case ratio >= contextHealthWarnRatio:
            // 80%+ → 发提醒 Gazette
            slog.Info("部长 context 使用率较高",
                "minister_id", m.ID,
                "ratio", fmt.Sprintf("%.1f%%", ratio*100),
            )

            g := &store.Gazette{
                ID:           gazetteID(),
                ToMinister:   store.NullString(m.ID),
                Type:         store.NullString("recovery"),
                Summary: fmt.Sprintf(
                    "提醒：你的 context 已使用 %.0f%%（%d/%d tokens, %d turns）。"+
                        "建议做一次 checkpoint 保存进度。",
                    ratio*100, health.TokensUsed, health.TokensLimit, health.TurnsElapsed,
                ),
                FromMinister: store.NullString("whip"),
            }
            if err := w.db.CreateGazette(g); err != nil {
                slog.Warn("创建 context warn gazette 失败", "err", err)
            }
        }
    }
}
```

### 2.5 Store 层：提取 context_health

```go
// internal/store/store.go

// GetLatestContextHealth returns the most recent context_health from a minister's gazettes.
func (db *DB) GetLatestContextHealth(ministerID string) (*ContextHealth, error) {
    var payload string
    err := db.conn.QueryRow(
        `SELECT payload FROM gazettes
         WHERE from_minister = ? AND payload LIKE '%context_health%'
         ORDER BY created_at DESC LIMIT 1`,
        ministerID,
    ).Scan(&payload)
    if err != nil {
        return nil, err
    }

    // Parse payload JSON
    var wrapper struct {
        ContextHealth *ContextHealth `json:"context_health"`
    }
    if err := json.Unmarshal([]byte(payload), &wrapper); err != nil {
        return nil, err
    }
    return wrapper.ContextHealth, nil
}
```

### 2.6 防重复发送

避免每次 tick（10s）都发同一个提醒，加去重：

```go
// Whip struct 新增
type Whip struct {
    // ...existing...
    lastContextAlert map[string]time.Time // minister_id → 上次告警时间
}

const contextAlertCooldown = 5 * time.Minute
```

在 `checkContextHealth` 中：

```go
if w.lastContextAlert == nil {
    w.lastContextAlert = make(map[string]time.Time)
}

lastAlert, exists := w.lastContextAlert[m.ID]
if exists && time.Since(lastAlert) < contextAlertCooldown {
    continue // 冷却期内，跳过
}

// ...发送 gazette 后...
w.lastContextAlert[m.ID] = time.Now()
```

---

## 3. 数据流

```
Minister Agent
  │
  │  写 Gazette (from_minister=m1)
  │  payload: {"context_health": {"tokens_used": 85000, ...}}
  │
  ▼
┌──────────┐
│  SQLite   │  gazettes 表（payload 字段）
└────┬─────┘
     │
     │  Whip tick → checkContextHealth()
     │  → db.GetLatestContextHealth("m1")
     │
     ▼
┌──────────┐
│   Whip   │  ratio = 85000/100000 = 85% > 80%
│          │  → 创建 recovery Gazette → minister inbox
└──────────┘
```

---

## 4. 测试计划

```go
func TestCheckContextHealth_BelowWarn_NoGazette(t *testing.T) {
    // 70% usage → 不发 gazette
}

func TestCheckContextHealth_AtWarn_SendsReminder(t *testing.T) {
    // 82% usage → 发提醒 gazette
}

func TestCheckContextHealth_AtCritical_SendsUrgent(t *testing.T) {
    // 92% usage → 发紧急 gazette + 记录 at-risk 事件
}

func TestCheckContextHealth_Cooldown_NoDuplicate(t *testing.T) {
    // 连续两次 tick 都是 85% → 只发一次
}

func TestCheckContextHealth_NoPayload_Skips(t *testing.T) {
    // gazette 无 context_health → 跳过
}

func TestGetLatestContextHealth_ParsesPayload(t *testing.T) {
    // 验证从 payload JSON 中正确解析 context_health
}
```

---

## 5. 变更文件清单

| 文件 | 变更类型 |
|------|---------|
| `internal/store/gazette.go` | 新增 `ContextHealth` struct |
| `internal/store/store.go` | 新增 `GetLatestContextHealth()` |
| `internal/whip/whip.go` | Whip struct 新增 `lastContextAlert` + tick 调用 |
| `internal/whip/context_health.go` | 新文件：`checkContextHealth()` 实现 |
| `internal/whip/context_health_test.go` | 新文件：测试 |
| `cmd/ministers.go` | `buildMinisterCLAUDE()` 新增 context_health 上报指示 |

---

## 6. 局限性与后续演进

| 局限 | 说明 | 后续 |
|------|------|------|
| 依赖 Minister 主动上报 | AI agent 可能不遵守指示 | v0.4 可通过 runtime API 直接查询 |
| token 估算不精确 | Agent 只能估算已用 token | v0.4 接入 runtime 的 usage API |
| 只能建议，不能强制 | 发 gazette 提醒，minister 可能忽略 | v0.4 结合恢复梯度，critical 时强制 by-election |
