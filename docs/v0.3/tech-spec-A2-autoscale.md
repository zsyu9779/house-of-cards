# 技术方案：A-2 Autoscale 全链路

> Phase 3 | 优先级：P0

---

## 1. 问题分析

### 1.1 现状

`internal/whip/scheduler.go:375-450` 的 `autoscale()` 实现：

```go
// Scale up: 从 reserve pool 拿一个 offline minister → 标记为 idle
if pendingBills > idle*upThresh && pendingBills > 0 {
    reservePool, _ := w.db.ListOfflineMinisters()
    if len(reservePool) > 0 {
        m := reservePool[0]
        _ = w.db.UpdateMinisterStatus(m.ID, "idle")  // ← 只改状态
        _ = w.db.CreateGazette(g)                     // ← 发通知
    }
}
```

**问题**：只更新 DB 中 minister 状态为 idle，不做任何实际操作：
- ❌ 不创建 Chamber（git worktree）
- ❌ 不分配 Bill
- ❌ 不启动 AI runtime（Claude Code session）

标记为 idle 后，minister 会被 `orderPaper()` 的下次 tick 分配 bill，但 minister 没有实际 worktree 和进程，Bill 会卡在 "reading" 状态直到 stuck。

### 1.2 现有 summon 逻辑

`cmd/ministers.go` 的 `minister summon` 命令包含完整的全链路：

1. 获取 minister 和 bill 信息
2. 创建 git worktree（Chamber）
3. 构建 CLAUDE.md 行为规范
4. 启动 tmux session + Claude Code runtime
5. 更新 minister 状态（working）+ worktree 路径 + PID

这些逻辑全部耦合在 Cobra RunE handler 中，无法被 `autoscale()` 复用。

---

## 2. 方案设计

### 2.1 核心思路

1. 提取 `cmd/ministers.go` 中 summon 的核心逻辑为 `internal/minister/summon.go`
2. `autoscale()` 在 scale-up 时调用提取后的函数完成全链路
3. CLI 命令改为调用同一函数
4. 添加速率限制，防止一次 tick 传召过多 minister

### 2.2 新增包 `internal/minister`

```go
// internal/minister/summon.go
package minister

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/house-of-cards/hoc/internal/chamber"
    "github.com/house-of-cards/hoc/internal/config"
    "github.com/house-of-cards/hoc/internal/store"
)

// SummonOpts contains parameters for summoning a minister.
type SummonOpts struct {
    MinisterID string
    BillID     string
    Project    string
    HocDir     string
    DB         *store.DB
    Cfg        *config.Config
}

// SummonResult contains the outcome of a summon operation.
type SummonResult struct {
    Worktree string
    Pid      int
    Branch   string
}

// Summon executes the full minister summon pipeline:
//  1. Validate minister/bill state
//  2. Create Chamber (git worktree)
//  3. Build CLAUDE.md
//  4. Start AI runtime (tmux + claude-code)
//  5. Update DB state
func Summon(ctx context.Context, opts SummonOpts) (*SummonResult, error) {
    db := opts.DB

    // 1. Validate
    m, err := db.GetMinister(opts.MinisterID)
    if err != nil {
        return nil, fmt.Errorf("get minister %s: %w", opts.MinisterID, err)
    }
    if m.Status != "idle" && m.Status != "offline" {
        return nil, fmt.Errorf("minister %s status is %q, must be idle or offline", m.ID, m.Status)
    }

    bill, err := db.GetBill(opts.BillID)
    if err != nil {
        return nil, fmt.Errorf("get bill %s: %w", opts.BillID, err)
    }

    // 2. Create Chamber
    branch := fmt.Sprintf("hoc/%s/%s", m.ID, bill.ID)
    repoPath := chamberRepoPath(opts.HocDir, opts.Project)
    worktree, err := chamber.Create(repoPath, branch, m.ID)
    if err != nil {
        return nil, fmt.Errorf("create chamber: %w", err)
    }

    // 3. Build CLAUDE.md
    claudeMD := BuildMinisterCLAUDE(m, bill, opts.Cfg)
    claudePath := filepath.Join(worktree, "CLAUDE.md")
    if err := os.WriteFile(claudePath, []byte(claudeMD), 0644); err != nil {
        return nil, fmt.Errorf("write CLAUDE.md: %w", err)
    }

    // 4. Start runtime
    pid, err := startRuntime(m, bill, worktree)
    if err != nil {
        return nil, fmt.Errorf("start runtime: %w", err)
    }

    // 5. Update DB
    if err := db.UpdateMinisterStatus(m.ID, "working"); err != nil {
        return nil, fmt.Errorf("update minister status: %w", err)
    }
    if err := db.UpdateMinisterWorktree(m.ID, worktree); err != nil {
        return nil, fmt.Errorf("update minister worktree: %w", err)
    }
    if err := db.UpdateMinisterPid(m.ID, pid); err != nil {
        return nil, fmt.Errorf("update minister pid: %w", err)
    }
    if err := db.AssignBill(bill.ID, m.ID); err != nil {
        return nil, fmt.Errorf("assign bill: %w", err)
    }
    if err := db.UpdateBillStatus(bill.ID, "reading"); err != nil {
        return nil, fmt.Errorf("update bill status: %w", err)
    }
    if err := db.UpdateBillBranch(bill.ID, branch); err != nil {
        slog.Warn("update bill branch", "err", err)
    }

    // Record event
    if err := db.RecordEvent("minister.summoned", "autoscale", bill.ID, m.ID, 
        bill.SessionID.String, ""); err != nil {
        slog.Warn("record summon event", "err", err)
    }

    slog.Info("部长传召完成",
        "minister_id", m.ID,
        "bill_id", bill.ID,
        "worktree", worktree,
        "pid", pid,
    )

    return &SummonResult{
        Worktree: worktree,
        Pid:      pid,
        Branch:   branch,
    }, nil
}

// startRuntime starts the AI runtime in a tmux session.
func startRuntime(m *store.Minister, bill *store.Bill, worktree string) (int, error) {
    tmuxName := fmt.Sprintf("hoc-%s", m.ID)

    // Kill existing session if any
    exec.Command("tmux", "kill-session", "-t", tmuxName).Run()

    // Build claude-code command
    claudeCmd := fmt.Sprintf(
        "cd %s && claude --dangerously-skip-permissions",
        worktree,
    )

    cmd := exec.Command("tmux", "new-session", "-d", "-s", tmuxName, claudeCmd)
    if err := cmd.Run(); err != nil {
        return 0, fmt.Errorf("tmux new-session: %w", err)
    }

    // Get tmux server PID (approximate)
    out, err := exec.Command("tmux", "display-message", "-t", tmuxName, "-p", "#{pane_pid}").Output()
    if err != nil {
        return 0, nil // PID optional
    }

    var pid int
    fmt.Sscanf(string(out), "%d", &pid)
    return pid, nil
}

// chamberRepoPath returns the main repo path for creating worktrees.
func chamberRepoPath(hocDir, project string) string {
    if project != "" {
        return filepath.Join(hocDir, "projects", project)
    }
    return hocDir
}

// BuildMinisterCLAUDE builds the CLAUDE.md content for a minister.
// Extracted from cmd/ministers.go:buildMinisterCLAUDE() for reuse.
func BuildMinisterCLAUDE(m *store.Minister, bill *store.Bill, cfg *config.Config) string {
    // ... 从 cmd/ministers.go 提取
    // 包含：角色定义、Bill 描述、行为规范、context_health 上报指示
    return "" // placeholder
}
```

### 2.3 autoscale 改造

```go
// internal/whip/scheduler.go

const maxScaleUpPerTick = 2 // 每次 tick 最多传召 2 个 minister

func (w *Whip) autoscale() {
    allMinisters, err := w.db.ListMinisters()
    if err != nil {
        slog.Debug("autoscale: 拉取部长列表失败", "err", err)
        return
    }

    var idle int
    var idleMinisters []*store.Minister
    for _, m := range allMinisters {
        if m.Status == "idle" {
            idle++
            idleMinisters = append(idleMinisters, m)
        }
    }

    // 获取可分配的 pending bills（draft，未分配）
    pendingBills := w.listPendingBills()
    if pendingBills == nil {
        return
    }

    upThresh := w.scaleUpThreshold()
    downThresh := w.scaleDownThreshold()

    // Scale up
    if shouldScaleUp(len(pendingBills), idle, upThresh) {
        reservePool, err := w.db.ListOfflineMinisters()
        if err != nil || len(reservePool) == 0 {
            slog.Debug("autoscale: 无可用预备池部长")
        } else {
            summoned := 0
            for _, m := range reservePool {
                if summoned >= maxScaleUpPerTick {
                    break
                }
                // 找一个匹配的 pending bill
                bill := w.matchBillToMinister(pendingBills, m)
                if bill == nil {
                    continue
                }

                slog.Info("autoscale: 全链路传召",
                    "minister_id", m.ID,
                    "bill_id", bill.ID,
                )

                result, err := minister.Summon(context.Background(), minister.SummonOpts{
                    MinisterID: m.ID,
                    BillID:     bill.ID,
                    Project:    bill.Project.String,
                    HocDir:     w.hocDir,
                    DB:         w.db,
                    Cfg:        w.cfg,
                })
                if err != nil {
                    slog.Error("autoscale: 传召失败", "minister_id", m.ID, "err", err)
                    continue
                }

                // 发 Gazette
                g := &store.Gazette{
                    ID:      gazetteID(),
                    Type:    store.NullString("autoscale"),
                    Summary: fmt.Sprintf("自动扩容：部长 [%s] 已传召处理议案 [%s]（worktree: %s）",
                        m.ID, bill.ID, result.Worktree),
                }
                if err := w.db.CreateGazette(g); err != nil {
                    slog.Warn("autoscale: 创建 Gazette 失败", "err", err)
                }

                summoned++
            }
        }
    }

    // Scale down（保持原逻辑，加错误处理）
    if shouldScaleDown(idle, len(pendingBills), downThresh) && len(idleMinisters) > 0 {
        m := idleMinisters[0]
        slog.Info("autoscale: 缩容", "minister_id", m.ID)
        if err := w.db.UpdateMinisterStatus(m.ID, "offline"); err != nil {
            slog.Error("autoscale: 缩容失败", "minister_id", m.ID, "err", err)
        } else {
            if err := w.db.RecordEvent("autoscale.triggered", "whip", "", m.ID, "",
                fmt.Sprintf(`{"direction":"down","pending":%d,"idle":%d}`,
                    len(pendingBills), idle)); err != nil {
                slog.Warn("autoscale: 记录事件失败", "err", err)
            }
        }
    }
}

// listPendingBills returns draft bills without assignee.
func (w *Whip) listPendingBills() []*store.Bill {
    allBills, err := w.db.ListBills()
    if err != nil {
        slog.Debug("autoscale: 拉取议案列表失败", "err", err)
        return nil
    }

    var pending []*store.Bill
    for _, b := range allBills {
        if (b.Status == "draft" || b.Status == "reading") && b.Assignee.String == "" {
            pending = append(pending, b)
        }
    }
    return pending
}

// matchBillToMinister finds a pending bill that matches the minister's skills.
func (w *Whip) matchBillToMinister(bills []*store.Bill, m *store.Minister) *store.Bill {
    // 优先匹配 portfolio
    for _, b := range bills {
        if b.Portfolio.String != "" && skillsMatch(m.Skills, b.Portfolio.String) {
            return b
        }
    }
    // 无 portfolio 要求的 bill
    for _, b := range bills {
        if b.Portfolio.String == "" {
            return b
        }
    }
    return nil
}

// shouldScaleUp is a pure function for testability.
func shouldScaleUp(pending, idle, threshold int) bool {
    return pending > idle*threshold && pending > 0
}

// shouldScaleDown is a pure function for testability.
func shouldScaleDown(idle, pending, threshold int) bool {
    return idle > pending+threshold && idle > threshold
}
```

### 2.4 CLI 命令改造

`cmd/ministers.go` 的 `summon` 命令改为调用 `minister.Summon()`：

```go
RunE: func(cmd *cobra.Command, args []string) error {
    if err := initDB(); err != nil {
        return err
    }

    result, err := minister.Summon(cmd.Context(), minister.SummonOpts{
        MinisterID: args[0],
        BillID:     summonBill,
        Project:    summonProject,
        HocDir:     hocDir,
        DB:         db,
        Cfg:        cfg,
    })
    if err != nil {
        return err
    }

    fmt.Printf("✓ 部长已传召\n")
    fmt.Printf("  Worktree: %s\n", result.Worktree)
    fmt.Printf("  Branch: %s\n", result.Branch)
    fmt.Printf("  PID: %d\n", result.Pid)
    return nil
},
```

### 2.5 Store 新增方法

```go
// UpdateMinisterWorktree updates a minister's worktree path.
func (db *DB) UpdateMinisterWorktree(id, worktree string) error {
    _, err := db.conn.Exec(`UPDATE ministers SET worktree = ? WHERE id = ?`, worktree, id)
    return err
}

// UpdateMinisterPid updates a minister's process ID.
func (db *DB) UpdateMinisterPid(id string, pid int) error {
    _, err := db.conn.Exec(`UPDATE ministers SET pid = ? WHERE id = ?`, pid, id)
    return err
}

// UpdateBillBranch updates a bill's branch name.
func (db *DB) UpdateBillBranch(billID, branch string) error {
    _, err := db.conn.Exec(`UPDATE bills SET branch = ? WHERE id = ?`, branch, billID)
    return err
}

// ListOfflineMinisters returns ministers with status "offline" that have skills.
func (db *DB) ListOfflineMinisters() ([]*Minister, error) {
    // ... 已存在，确认签名
}
```

---

## 3. 速率限制

### 3.1 每 tick 上限

`maxScaleUpPerTick = 2`：每次 10s tick 最多传召 2 个 minister。

**理由**：
- 避免 git worktree 并发创建冲突
- 避免 tmux session 并发启动资源竞争
- 给系统消化时间——下次 tick 重新评估

### 3.2 全局上限

通过 `config.toml` 的 `whip.max_ministers` 控制：

```go
if len(workingMinisters)+summoned >= w.cfg.Whip.MaxMinisters {
    slog.Info("autoscale: 已达部长上限", "max", w.cfg.Whip.MaxMinisters)
    break
}
```

---

## 4. 错误恢复

Summon 过程中任何步骤失败，需要回滚已完成的步骤：

```go
func Summon(ctx context.Context, opts SummonOpts) (*SummonResult, error) {
    // ...

    // 2. Create Chamber
    worktree, err := chamber.Create(repoPath, branch, m.ID)
    if err != nil {
        return nil, fmt.Errorf("create chamber: %w", err)
    }
    // 注册清理：后续步骤失败时删除 worktree
    var cleanups []func()
    cleanups = append(cleanups, func() {
        chamber.Remove(worktree)
    })
    defer func() {
        if err != nil {
            for _, fn := range cleanups {
                fn()
            }
        }
    }()

    // 4. Start runtime
    pid, err := startRuntime(m, bill, worktree)
    if err != nil {
        return nil, fmt.Errorf("start runtime: %w", err)
    }
    cleanups = append(cleanups, func() {
        exec.Command("tmux", "kill-session", "-t", fmt.Sprintf("hoc-%s", m.ID)).Run()
    })

    // 5. Update DB（最后一步，前面都成功才写）
    // ...

    err = nil // 清除 defer 的回滚触发
    return result, nil
}
```

---

## 5. 测试计划

| 测试 | 验证点 |
|------|--------|
| `TestShouldScaleUp` | table-driven 纯函数测试 |
| `TestShouldScaleDown` | table-driven 纯函数测试 |
| `TestMatchBillToMinister_PortfolioMatch` | 优先匹配 portfolio |
| `TestMatchBillToMinister_NoPortfolio` | 无 portfolio 时兜底匹配 |
| `TestMatchBillToMinister_NoMatch` | 无可匹配 bill 返回 nil |
| `TestAutoscale_MaxPerTick` | 一次 tick 不超过 maxScaleUpPerTick |
| `TestAutoscale_RespectMaxMinisters` | 不超过 max_ministers 全局上限 |

注：全链路 Summon 涉及 git worktree 和 tmux，在 CI 中通过 mock startRuntime 测试。

---

## 6. 变更文件清单

| 文件 | 变更类型 |
|------|---------|
| `internal/minister/summon.go` | **新文件**：Summon 全链路逻辑 |
| `internal/minister/claude_md.go` | **新文件**：BuildMinisterCLAUDE 提取 |
| `internal/whip/scheduler.go` | autoscale 改为调用 Summon + 纯函数提取 |
| `cmd/ministers.go` | summon 命令改为调用 `minister.Summon()` |
| `internal/store/store.go` | 新增 UpdateMinisterWorktree / UpdateMinisterPid / UpdateBillBranch |
| `internal/whip/scheduler_test.go` | 新增 autoscale + 纯函数测试 |
| `internal/minister/summon_test.go` | 新增 Summon 逻辑测试 |
