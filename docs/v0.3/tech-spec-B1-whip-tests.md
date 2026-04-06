# 技术方案：B-1 Whip 核心路径测试补强

> Phase 2 | 优先级：P0

---

## 1. 现状分析

### 1.1 当前覆盖率

| 文件 | 行数 | 当前覆盖 | 测试现状 |
|------|------|---------|---------|
| `whip.go` | 137 | 低 | `TestNew_CreatesWhip` 仅测构造 |
| `liveness.go` | 184 | 0% | 无测试 |
| `poller.go` | 359 | 0% | 无测试 |
| `scheduler.go` | 451 | ~25% | `billIsReady` 系列 + `advanceSession` 集成测试 |
| `dispatch.go` | 86 | 0% | 无测试 |

### 1.2 已有测试

`whip_test.go` 当前测试：
- `billIsReady` — 9 个测试（table-driven 风格，覆盖良好）
- `advanceSession` — 5 个集成测试（用真实 in-memory SQLite）
- `TestNew_CreatesWhip` — 构造测试

**缺口**：liveness（threeLineWhip / byElection）、poller（pollDoneFiles / pollReviewFile / pollAckFiles）、dispatch（gazetteDispatch / deliverGazette）完全未覆盖。

---

## 2. 测试策略

### 2.1 核心原则

1. **用真实 DB**：沿用 `newTestWhip(t)` 模式，用 in-memory SQLite，不 mock store
2. **纯函数提取**：将可测试逻辑提取为纯函数/方法，减少对文件系统和进程的依赖
3. **文件系统用 t.TempDir()**：done/review/ack 文件在临时目录创建
4. **进程检查可替换**：`isMinisterAlive` 通过接口或函数字段注入，测试时 stub

### 2.2 需要提取的纯函数

| 当前位置 | 提取为 | 理由 |
|---------|--------|------|
| `parseDoneFile(path)` | 已是纯函数 | 可直接测试 |
| `isMinisterAlive(m)` | `type AliveChecker func(*store.Minister) bool` | tmux/kill 检查在测试中不可用 |
| `threeLineWhip` 中的 "grace period 判断" | `isOverGracePeriod(heartbeat time.Time, grace time.Duration) bool` | 纯时间计算 |
| `threeLineWhip` 中的 "stuck 判断" | `isOverStuckThreshold(heartbeat time.Time, threshold time.Duration) bool` | 纯时间计算 |
| `autoscale` 中的 "阈值决策" | `shouldScaleUp(pending, idle, threshold int) bool` / `shouldScaleDown(idle, pending, threshold int) bool` | 纯数值计算 |

### 2.3 AliveChecker 注入

```go
// whip.go
type Whip struct {
    db           *store.DB
    hocDir       string
    tracer       *otel.Tracer
    cfg          *config.Config
    aliveChecker func(*store.Minister) bool // nil → 使用默认 isMinisterAlive
}

// isAlive returns whether a minister's process is still running.
func (w *Whip) isAlive(m *store.Minister) bool {
    if w.aliveChecker != nil {
        return w.aliveChecker(m)
    }
    return w.isMinisterAlive(m)
}
```

测试中注入：

```go
func newTestWhipWithAlive(t *testing.T, alive func(*store.Minister) bool) (*Whip, *store.DB) {
    w, db := newTestWhip(t)
    w.aliveChecker = alive
    return w, db
}
```

---

## 3. 测试清单

### 3.1 纯函数测试

#### parseDoneFile

```go
func TestParseDoneFile(t *testing.T) {
    tests := []struct {
        name        string
        content     string
        wantSummary string
        wantJSON    bool // payload JSON 是否非空
    }{
        {
            name:        "plain text",
            content:     "Task completed successfully",
            wantSummary: "Task completed successfully",
            wantJSON:    false,
        },
        {
            name: "TOML with contracts",
            content: `summary = "Implemented API"
[contracts]
endpoint = "POST /api/v1/users"
[artifacts]
file = "internal/api/users.go"`,
            wantSummary: "Implemented API",
            wantJSON:    true,
        },
        {
            name:        "empty file",
            content:     "",
            wantSummary: "",
            wantJSON:    false,
        },
        {
            name:        "non-existent file",
            content:     "", // 不创建文件
            wantSummary: "",
            wantJSON:    false,
        },
    }
    // ...
}
```

#### isOverGracePeriod / isOverStuckThreshold

```go
func TestIsOverGracePeriod(t *testing.T) {
    tests := []struct {
        name      string
        heartbeat time.Time
        grace     time.Duration
        want      bool
    }{
        {"within grace", time.Now().Add(-10 * time.Second), 30 * time.Second, false},
        {"exactly at grace", time.Now().Add(-30 * time.Second), 30 * time.Second, false},
        {"beyond grace", time.Now().Add(-60 * time.Second), 30 * time.Second, true},
        {"zero heartbeat", time.Time{}, 30 * time.Second, true},
    }
    // ...
}
```

#### shouldScaleUp / shouldScaleDown

```go
func TestShouldScaleUp(t *testing.T) {
    tests := []struct {
        name      string
        pending   int
        idle      int
        threshold int
        want      bool
    }{
        {"pending exceeds threshold", 5, 1, 2, true},   // 5 > 1*2
        {"pending at threshold", 2, 1, 2, false},       // 2 == 1*2
        {"no pending", 0, 3, 2, false},                 // 0 pending
        {"many idle", 3, 5, 2, false},                  // 3 < 5*2
        {"zero idle high pending", 3, 0, 2, true},      // 3 > 0*2 && 3 > 0
    }
    // ...
}

func TestShouldScaleDown(t *testing.T) {
    tests := []struct {
        name      string
        idle      int
        pending   int
        threshold int
        want      bool
    }{
        {"excess idle", 5, 1, 2, true},    // 5 > 1+2 && 5 > 2
        {"balanced", 3, 1, 2, false},      // 3 == 1+2
        {"at threshold", 3, 0, 2, true},   // 3 > 0+2 && 3 > 2
        {"below threshold", 2, 1, 2, false}, // 2 < 1+2
    }
    // ...
}
```

### 3.2 Liveness 测试（threeLineWhip + byElection）

```go
func TestThreeLineWhip_HealthyMinister_UpdatesHeartbeat(t *testing.T) {
    w, db := newTestWhipWithAlive(t, func(m *store.Minister) bool { return true })
    mustCreateIdleMinister(t, db, "m1")
    db.UpdateMinisterStatus("m1", "working")

    w.threeLineWhip()

    m, _ := db.GetMinister("m1")
    if m.Status != "working" {
        t.Errorf("healthy minister should stay working, got %q", m.Status)
    }
}

func TestThreeLineWhip_DeadMinister_WithinGrace_StaysWorking(t *testing.T) {
    w, db := newTestWhipWithAlive(t, func(m *store.Minister) bool { return false })
    mustCreateIdleMinister(t, db, "m1")
    db.UpdateMinisterStatus("m1", "working")
    db.UpdateMinisterHeartbeat("m1") // 刚更新心跳

    w.threeLineWhip()

    m, _ := db.GetMinister("m1")
    if m.Status != "working" {
        t.Errorf("minister within grace period should stay working, got %q", m.Status)
    }
}

func TestThreeLineWhip_DeadMinister_BeyondGrace_MarksStuck(t *testing.T) {
    w, db := newTestWhipWithAlive(t, func(m *store.Minister) bool { return false })
    mustCreateIdleMinister(t, db, "m1")
    db.UpdateMinisterStatus("m1", "working")
    // 设置过期心跳
    db.Exec(`UPDATE ministers SET heartbeat = ? WHERE id = ?`,
        time.Now().Add(-2*gracePeriod), "m1")

    w.threeLineWhip()

    m, _ := db.GetMinister("m1")
    if m.Status != "stuck" {
        t.Errorf("minister beyond grace period should be stuck, got %q", m.Status)
    }
}

func TestThreeLineWhip_StuckMinister_BeyondThreshold_ByElection(t *testing.T) {
    w, db := newTestWhipWithAlive(t, func(m *store.Minister) bool { return false })
    mustCreateIdleMinister(t, db, "m1")
    db.UpdateMinisterStatus("m1", "stuck")
    db.Exec(`UPDATE ministers SET heartbeat = ? WHERE id = ?`,
        time.Now().Add(-2*stuckThreshold), "m1")

    // 创建一个 assigned bill
    sess := mustCreateSession(t, db, "s1", "Test")
    mustCreateBill(t, db, "b1", "s1", "Feature", "reading", "")
    db.AssignBill("b1", "m1")

    w.threeLineWhip()

    m, _ := db.GetMinister("m1")
    if m.Status != "offline" {
        t.Errorf("by-election should mark minister offline, got %q", m.Status)
    }

    bill, _ := db.GetBill("b1")
    if bill.Status != "draft" {
        t.Errorf("by-election should reset bill to draft, got %q", bill.Status)
    }
    if bill.Assignee.String != "" {
        t.Errorf("by-election should clear assignee, got %q", bill.Assignee.String)
    }
}

func TestByElection_CreatesHandoffGazette(t *testing.T) {
    w, db := newTestWhip(t)
    mustCreateIdleMinister(t, db, "m1")
    db.UpdateMinisterStatus("m1", "stuck")
    mustCreateBill(t, db, "b1", "", "Feature", "reading", "")
    db.AssignBill("b1", "m1")

    m, _ := db.GetMinister("m1")
    w.byElection(m)

    gazettes, _ := db.ListGazettes()
    found := false
    for _, g := range gazettes {
        if g.Type.String == "handoff" && g.BillID.String == "b1" {
            found = true
        }
    }
    if !found {
        t.Error("byElection should create handoff gazette")
    }
}
```

### 3.3 Poller 测试

#### pollDoneFiles

```go
func TestPollDoneFiles_EnactsBill(t *testing.T) {
    w, db := newTestWhip(t)
    dir := t.TempDir()

    // Setup minister with worktree
    mustCreateIdleMinister(t, db, "m1")
    db.UpdateMinisterStatus("m1", "working")
    db.Exec(`UPDATE ministers SET worktree = ? WHERE id = ?`, dir, "m1")

    // Setup bill
    mustCreateBill(t, db, "b1", "", "Feature", "reading", "")
    db.AssignBill("b1", "m1")

    // Create .done file
    hocDir := filepath.Join(dir, ".hoc")
    os.MkdirAll(hocDir, 0755)
    os.WriteFile(filepath.Join(hocDir, "bill-b1.done"), []byte("Task done"), 0644)

    w.pollDoneFiles()

    bill, _ := db.GetBill("b1")
    if bill.Status != "enacted" {
        t.Errorf("bill should be enacted, got %q", bill.Status)
    }
}

func TestPollDoneFiles_NoDoneFile_NoChange(t *testing.T) {
    w, db := newTestWhip(t)
    dir := t.TempDir()

    mustCreateIdleMinister(t, db, "m1")
    db.UpdateMinisterStatus("m1", "working")
    db.Exec(`UPDATE ministers SET worktree = ? WHERE id = ?`, dir, "m1")
    mustCreateBill(t, db, "b1", "", "Feature", "reading", "")
    db.AssignBill("b1", "m1")

    w.pollDoneFiles()

    bill, _ := db.GetBill("b1")
    if bill.Status != "reading" {
        t.Errorf("bill should stay reading, got %q", bill.Status)
    }
}

func TestPollDoneFiles_MinisterBecomesIdle_WhenAllDone(t *testing.T) {
    // minister 的所有 bill 都 enacted → 标记 idle
    // ...
}

func TestPollDoneFiles_SkipsTerminalBills(t *testing.T) {
    // enacted/royal_assent/failed 的 bill 不处理
    // ...
}
```

#### pollReviewFile

```go
func TestPollReviewFile_Pass_EnactsBill(t *testing.T) {
    // 写入 "PASS\n审查通过" → bill enacted
}

func TestPollReviewFile_Fail_ResetsToDraft(t *testing.T) {
    // 写入 "FAIL\n需要修改" → bill draft
}

func TestPollReviewFile_NoFile_NoChange(t *testing.T) {
    // 无 .review 文件 → 不变
}
```

### 3.4 Dispatch 测试

```go
func TestGazetteDispatch_TargetedGazette_WritesToInbox(t *testing.T) {
    w, db := newTestWhip(t)
    dir := t.TempDir()

    mustCreateIdleMinister(t, db, "m1")
    db.Exec(`UPDATE ministers SET worktree = ? WHERE id = ?`, dir, "m1")

    g := &store.Gazette{
        ID:         "gaz-001",
        ToMinister: store.NullString("m1"),
        Type:       store.NullString("handoff"),
        Summary:    "Test gazette",
    }
    db.CreateGazette(g)

    w.gazetteDispatch()

    // Check inbox file exists
    inboxFile := filepath.Join(dir, ".hoc", "inbox", "gaz-001.md")
    if _, err := os.Stat(inboxFile); os.IsNotExist(err) {
        t.Error("gazette should be delivered to inbox")
    }

    // Check gazette marked as read
    reloaded, _ := db.ListUnreadGazettes()
    if len(reloaded) != 0 {
        t.Errorf("gazette should be marked read, got %d unread", len(reloaded))
    }
}

func TestGazetteDispatch_BroadcastGazette_NoInbox(t *testing.T) {
    // to_minister 为空 → 不写 inbox，仅标记已读
}
```

### 3.5 恢复梯度测试（E-1.1 配套）

```go
func TestRecoveryGradient_Level1_SendsCheckpointGazette(t *testing.T) {
    // stuck minister, recovery_attempts=0 → 发 recovery gazette, attempts → 1
}

func TestRecoveryGradient_Level2_MarksAtRisk(t *testing.T) {
    // stuck minister, recovery_attempts=1 → 发警告 gazette + 记录 at-risk, attempts → 2
}

func TestRecoveryGradient_Level3_TriggersByElection(t *testing.T) {
    // stuck minister, recovery_attempts=2 → byElection + reset attempts
}

func TestRecoveryGradient_ResetOnAlive(t *testing.T) {
    // working minister with recovery_attempts > 0, alive → reset to 0
}
```

---

## 4. 辅助工具函数

新增到 `whip_test.go` 的测试辅助：

```go
// setMinisterHeartbeat sets a minister's heartbeat to a specific time.
func setMinisterHeartbeat(t *testing.T, db *store.DB, id string, ts time.Time) {
    t.Helper()
    _, err := db.Exec(`UPDATE ministers SET heartbeat = ? WHERE id = ?`, ts, id)
    if err != nil {
        t.Fatalf("setMinisterHeartbeat %s: %v", id, err)
    }
}

// setMinisterWorktree sets a minister's worktree path.
func setMinisterWorktree(t *testing.T, db *store.DB, id, path string) {
    t.Helper()
    _, err := db.Exec(`UPDATE ministers SET worktree = ? WHERE id = ?`, path, id)
    if err != nil {
        t.Fatalf("setMinisterWorktree %s: %v", id, err)
    }
}

// createDoneFile creates a .done file in the minister's chamber.
func createDoneFile(t *testing.T, worktree, billID, content string) {
    t.Helper()
    dir := filepath.Join(worktree, ".hoc")
    if err := os.MkdirAll(dir, 0755); err != nil {
        t.Fatal(err)
    }
    path := filepath.Join(dir, fmt.Sprintf("bill-%s.done", billID))
    if err := os.WriteFile(path, []byte(content), 0644); err != nil {
        t.Fatal(err)
    }
}
```

**注意**：`db.Exec` 目前未导出。需要在 Store 层新增一个测试辅助方法：

```go
// Exec exposes the underlying sql.DB Exec for test helpers.
// Only available in test builds via store_test_helpers.go.
func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
    return db.conn.Exec(query, args...)
}
```

---

## 5. 覆盖率目标

| 文件 | 当前 | 目标 | 新增测试数 |
|------|------|------|-----------|
| `whip.go` | 低 | 60% | 2（tick 路径） |
| `liveness.go` | 0% | 70% | 6（threeLineWhip + byElection + 恢复梯度） |
| `poller.go` | 0% | 60% | 8（pollDoneFiles + pollReviewFile + pollAckFiles） |
| `scheduler.go` | 25% | 65% | 6（autoscale + privyAutoMerge 路径） |
| `dispatch.go` | 0% | 70% | 3（gazetteDispatch + deliverGazette） |
| **合计** | ~12.9% | **65%+** | **~25 个** |

---

## 6. 变更文件清单

| 文件 | 变更类型 |
|------|---------|
| `internal/whip/whip.go` | 新增 `aliveChecker` 字段 + `isAlive()` 方法 |
| `internal/whip/liveness.go` | 使用 `w.isAlive()` 替代 `w.isMinisterAlive()` |
| `internal/whip/scheduler.go` | 提取 `shouldScaleUp` / `shouldScaleDown` 纯函数 |
| `internal/whip/whip_test.go` | 大量新增测试（~25 个） |
| `internal/store/store.go` | 新增 `Exec()` 测试辅助（或放在 `store_test_helpers.go`） |
