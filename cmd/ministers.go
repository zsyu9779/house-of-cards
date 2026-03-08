package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/chamber"
	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/runtime"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

var (
	db     *store.DB
	cfg    *config.Config
	hocDir string
)

func initDB() error {
	if db != nil {
		return nil
	}

	var err error
	hocDir = config.GetHOCHome()

	if err := os.MkdirAll(hocDir, 0755); err != nil {
		return fmt.Errorf("create hoc home: %w", err)
	}

	cfg, err = config.LoadConfig(hocDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err = store.NewDB(hocDir)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	return nil
}

// ministersCmd represents the ministers command
var ministersCmd = &cobra.Command{
	Use:   "minister",
	Short: "管理 Minister（部长）",
	Long:  "Minister 管理命令：任命、传召、休会、查看",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	ministersCmd.AddCommand(ministerAppointCmd)
	ministersCmd.AddCommand(ministerSummonCmd)
	ministersCmd.AddCommand(ministerDismissCmd)
	ministersCmd.AddCommand(ministersListCmd)
	ministersCmd.AddCommand(ministerByElectionCmd)
	ministersCmd.AddCommand(ministerAutoCmd)
	ministersCmd.AddCommand(ministerHookCmd)

	ministerAppointCmd.Flags().String("runtime", "claude-code", "Runtime: claude-code, codex, cursor")
	ministerAppointCmd.Flags().StringSlice("portfolio", []string{}, "技能领域")
	ministerAppointCmd.Flags().String("title", "", "部长头衔")

	ministersListCmd.Flags().Bool("json", false, "以 JSON 格式输出")

	ministerSummonCmd.Flags().String("bill", "", "要执行的议案 ID")
	ministerSummonCmd.Flags().String("project", "", "项目名称（与 --bill 配合使用）")
	ministerSummonCmd.Flags().Bool("no-tmux", false, "前台运行（不使用 tmux）")

	ministerAutoCmd.Flags().String("session", "", "限定会期 ID（不填则监控所有活跃会期）")
	ministerAutoCmd.Flags().String("project", "", "默认项目名称（会期未设 project 时使用）")
	ministerAutoCmd.Flags().Int("max-concurrent", 3, "最多同时运行的部长数量")
	ministerAutoCmd.Flags().Bool("no-tmux", false, "前台运行（不使用 tmux）")

	// Phase 3B: hook subcommands
	ministerHookCmd.AddCommand(ministerHookPushCmd)
	ministerHookCmd.AddCommand(ministerHookListCmd)
	ministerHookCmd.AddCommand(ministerHookPopCmd)
}

var ministerAppointCmd = &cobra.Command{
	Use:   "appoint [name]",
	Short: "任命新的 Minister",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := initDB(); err != nil {
			slog.Error("init db", "err", err)
			os.Exit(1)
		}
		defer db.Close()

		rt, _ := cmd.Flags().GetString("runtime")
		portfolio, _ := cmd.Flags().GetStringSlice("portfolio")
		title, _ := cmd.Flags().GetString("title")

		minister := &store.Minister{
			ID:      args[0],
			Title:   title,
			Runtime: rt,
		}

		if len(portfolio) > 0 {
			b, _ := json.Marshal(portfolio)
			minister.Skills = string(b)
		}

		if minister.Title == "" {
			minister.Title = fmt.Sprintf("Minister of %s", args[0])
		}
		minister.Status = "offline"

		if err := db.CreateMinister(minister); err != nil {
			slog.Error("create minister", "err", err)
			os.Exit(1)
		}

		fmt.Printf("✓ 已任命 %s 为 %s (runtime: %s)\n", args[0], minister.Title, rt)
	},
}

var ministerSummonCmd = &cobra.Command{
	Use:   "summon [name]",
	Short: "传召 Minister（启动 session）",
	Long: `传召部长，可选地为其分配议案并在 Chamber 中启动工作会话。

示例:
  hoc minister summon backend-claude                               # 仅标记为 idle
  hoc minister summon backend-claude --bill bill-1a2b --project myapp  # 在 Chamber 中启动并执行议案`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministerID := args[0]
		billID, _ := cmd.Flags().GetString("bill")
		projectName, _ := cmd.Flags().GetString("project")
		noTmux, _ := cmd.Flags().GetBool("no-tmux")

		minister, err := db.GetMinister(ministerID)
		if err != nil {
			return fmt.Errorf("minister not found: %s", ministerID)
		}

		// Simple summon (no bill): just mark idle.
		if billID == "" {
			if err := db.UpdateMinisterStatus(ministerID, "idle"); err != nil {
				return fmt.Errorf("update status: %w", err)
			}
			fmt.Printf("✓ 已传召 %s (状态: idle)\n", minister.Title)
			return nil
		}

		// Summon with bill: need project.
		if projectName == "" {
			return fmt.Errorf("请通过 --project 指定项目名称")
		}

		// Get bill.
		bill, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		// Resolve paths.
		mainRepoPath := filepath.Join(hocDir, "projects", projectName, "main")
		if _, err := os.Stat(mainRepoPath); os.IsNotExist(err) {
			return fmt.Errorf("项目 %s 不存在，请先运行 hoc project add", projectName)
		}

		// Create / reuse chamber.
		ch, err := chamber.NewChamber(hocDir, projectName, ministerID, mainRepoPath)
		if err != nil {
			return fmt.Errorf("init chamber: %w", err)
		}
		if _, statErr := os.Stat(ch.GetWorktreePath()); os.IsNotExist(statErr) {
			fmt.Printf("⚙  创建议事厅 (git worktree): %s\n", ch.GetWorktreePath())
			if err := ch.Create(); err != nil {
				return fmt.Errorf("create chamber: %w", err)
			}
		} else {
			fmt.Printf("⚙  复用现有议事厅: %s\n", ch.GetWorktreePath())
		}

		// Build bill brief.
		brief := buildBillBrief(minister, bill, ch.GetBranchName())

		// Attach upstream gazettes (handoff / completion / review for this bill).
		upstreamGazettes, _ := db.ListGazettesForBill(billID)
		if len(upstreamGazettes) > 0 {
			var sb strings.Builder
			sb.WriteString("\n## 上游公报（来自前序部长）\n\n")
			for _, g := range upstreamGazettes {
				sb.WriteString(fmt.Sprintf("**[%s]** %s:\n\n%s\n\n---\n\n",
					g.Type.String,
					orDash(g.FromMinister.String),
					g.Summary,
				))
			}
			brief += sb.String()
		}

		// Start runtime.
		useTmux := !noTmux
		rt := runtime.New(minister.Runtime, useTmux)
		opts := runtime.SummonOpts{
			MinisterID:    ministerID,
			MinisterTitle: minister.Title,
			ChamberPath:   ch.GetWorktreePath(),
			BillBrief:     brief,
		}

		fmt.Printf("🚀 传召 %s，执行议案 [%s] %s\n", minister.Title, billID, bill.Title)
		agentSess, err := rt.Summon(opts)
		if err != nil {
			return fmt.Errorf("summon runtime: %w", err)
		}

		// Update DB.
		_ = db.UpdateMinisterStatus(ministerID, "working")
		_ = db.UpdateMinisterWorktree(ministerID, ch.GetWorktreePath())
		if agentSess.PID > 0 {
			_ = db.UpdateMinisterPID(ministerID, agentSess.PID)
		}
		_ = db.AssignBill(billID, ministerID)
		_ = db.UpdateBillStatus(billID, "reading")
		_ = db.UpdateBillBranch(billID, ch.GetBranchName())

		if useTmux {
			fmt.Printf("✅ %s 已在 tmux 会话 [hoc-%s] 中就绪\n", minister.Title, ministerID)
			fmt.Printf("   议事厅:  %s\n", ch.GetWorktreePath())
			fmt.Printf("   分支:    %s\n", ch.GetBranchName())
			fmt.Printf("   查看:    tmux attach -t hoc-%s\n", ministerID)
		} else {
			fmt.Printf("✅ %s 已启动 (PID: %d)\n", minister.Title, agentSess.PID)
		}
		return nil
	},
}

var ministerDismissCmd = &cobra.Command{
	Use:   "dismiss [name]",
	Short: "休会 Minister（停止 session）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministerID := args[0]
		minister, err := db.GetMinister(ministerID)
		if err != nil {
			return fmt.Errorf("minister not found: %s", ministerID)
		}

		// Try to kill tmux session if it exists.
		rt := runtime.New(minister.Runtime, true)
		sess := &runtime.AgentSession{
			MinisterID:  ministerID,
			TmuxSession: fmt.Sprintf("hoc-%s", ministerID),
		}
		if rt.IsSeated(sess) {
			if err := rt.Dismiss(sess); err != nil {
				slog.Warn("could not dismiss tmux session", "err", err)
			}
		}

		if err := db.UpdateMinisterStatus(ministerID, "offline"); err != nil {
			return fmt.Errorf("update status: %w", err)
		}

		fmt.Printf("✓ 已休会 %s (状态: offline)\n", minister.Title)
		return nil
	},
}

var ministersListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有 Minister",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministers, err := db.ListMinisters()
		if err != nil {
			return fmt.Errorf("list ministers: %w", err)
		}

		jsonMode, _ := cmd.Flags().GetBool("json")
		if jsonMode {
			type ministerJSON struct {
				ID        string   `json:"id"`
				Title     string   `json:"title"`
				Status    string   `json:"status"`
				Runtime   string   `json:"runtime"`
				Portfolio []string `json:"portfolio"`
				Worktree  string   `json:"worktree,omitempty"`
			}
			out := make([]ministerJSON, 0, len(ministers))
			for _, m := range ministers {
				portfolio := parsePortfolio(m.Skills)
				if portfolio == nil {
					portfolio = []string{}
				}
				out = append(out, ministerJSON{
					ID:        m.ID,
					Title:     m.Title,
					Status:    m.Status,
					Runtime:   m.Runtime,
					Portfolio: portfolio,
					Worktree:  m.Worktree.String,
				})
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Println("📋 内阁花名册:")
		fmt.Println("─────────────────────────────────────────")
		if len(ministers) == 0 {
			fmt.Println("  (暂无部长)")
		}
		for _, m := range ministers {
			statusIcon := "⚪"
			switch m.Status {
			case "working":
				statusIcon = "🟢"
			case "idle":
				statusIcon = "🟡"
			case "stuck":
				statusIcon = "🔴"
			}
			fmt.Printf("%s %s [%s]\n", statusIcon, m.Title, m.Status)
			fmt.Printf("   ID: %-24s  Runtime: %s\n", m.ID, m.Runtime)
			if m.Worktree.String != "" {
				fmt.Printf("   议事厅: %s\n", m.Worktree.String)
			}
			if m.Heartbeat.Valid {
				fmt.Printf("   心跳: %s\n", m.Heartbeat.Time.Format(time.RFC3339))
			}
		}
		return nil
	},
}

var ministerByElectionCmd = &cobra.Command{
	Use:   "by-election [minister-id]",
	Short: "手动触发部长补选（By-election）",
	Long: `手动触发部长补选流程：
  1. 尝试 git stash 保存议事厅进度
  2. 生成 Handoff Gazette
  3. 将议案重置为 draft（可被重新派发）
  4. 写入 Hansard（outcome: failed）
  5. 将部长标记为 offline`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministerID := args[0]
		m, err := db.GetMinister(ministerID)
		if err != nil {
			return fmt.Errorf("minister not found: %s", ministerID)
		}

		bills, err := db.GetBillsByAssignee(ministerID)
		if err != nil {
			return fmt.Errorf("get bills: %w", err)
		}

		if len(bills) == 0 {
			fmt.Printf("部长 [%s] 当前没有分配的议案，标记为 offline。\n", ministerID)
			return db.UpdateMinisterStatus(ministerID, "offline")
		}

		fmt.Printf("🗳  开始补选：%s\n", m.Title)

		for _, b := range bills {
			if b.Status == "enacted" || b.Status == "royal_assent" || b.Status == "failed" {
				continue
			}

			// Try git stash.
			stashInfo := ""
			if m.Worktree.String != "" {
				stashMsg := fmt.Sprintf("hoc-by-election-%s-%d", ministerID, time.Now().Unix())
				out, stashErr := runGitInDir(m.Worktree.String, "stash", "push", "-m", stashMsg)
				if stashErr == nil && !strings.Contains(out, "No local changes") {
					stashInfo = fmt.Sprintf("\n未提交进度已 stash: `%s`", stashMsg)
					fmt.Printf("   💾 stash: %s\n", stashMsg)
				}
			}

			branchInfo := ""
			if b.Branch.String != "" {
				branchInfo = fmt.Sprintf("  分支: `%s`", b.Branch.String)
			}

			// Handoff Gazette.
			summary := fmt.Sprintf(
				"补选公报：部长 [%s] 手动触发补选。\n\n议案 [%s] \"%s\" 需要接手。%s%s\n\n接手：hoc minister summon <new-minister> --bill %s --project <project>",
				ministerID, b.ID, b.Title, branchInfo, stashInfo, b.ID,
			)
			g := &store.Gazette{
				ID:           shortID("gazette"),
				FromMinister: store.NullString(ministerID),
				BillID:       store.NullString(b.ID),
				Type:         store.NullString("handoff"),
				Summary:      summary,
			}
			_ = db.CreateGazette(g)

			// Hansard entry.
			h := &store.Hansard{
				ID:         shortID("hansard"),
				MinisterID: ministerID,
				BillID:     b.ID,
				Outcome:    store.NullString("failed"),
				Notes:      store.NullString("手动补选触发"),
			}
			_ = db.CreateHansard(h)

			// Reset bill.
			if err := db.ClearBillAssignment(b.ID); err != nil {
				slog.Warn("clear bill assignment", "bill_id", b.ID, "err", err)
			}
			fmt.Printf("   📄 议案 [%s] 已重置为 draft\n", b.ID)
		}

		_ = db.UpdateMinisterStatus(ministerID, "offline")
		fmt.Printf("✓ 补选完成：%s → offline\n", m.Title)
		fmt.Printf("  使用 `hoc whip start` 让 Whip 自动重新派发就绪议案。\n")
		return nil
	},
}

// ─── minister auto (8-4) ─────────────────────────────────────────────────────

// ministerAutoCmd continuously monitors active sessions and auto-summons ministers
// for bills that have been assigned but not yet started.
var ministerAutoCmd = &cobra.Command{
	Use:   "auto",
	Short: "全自动模式：监控 Whip 分配，自动传召部长执行议案",
	Long: `全自动模式：持续循环，检测 Whip 分配的就绪议案，自动传召部长，监控完成信号，自动休会。
需要同时运行 hoc whip start 以驱动任务分配。

示例:
  hoc minister auto                           # 监控所有活跃会期
  hoc minister auto --session session-abc     # 只处理指定会期
  hoc minister auto --max-concurrent 2        # 最多 2 个并发部长
  hoc minister auto --project myapp           # 默认项目（会期未配置 project 时使用）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		sessionFilter, _ := cmd.Flags().GetString("session")
		defaultProject, _ := cmd.Flags().GetString("project")
		maxConcurrent, _ := cmd.Flags().GetInt("max-concurrent")
		noTmux, _ := cmd.Flags().GetBool("no-tmux")

		fmt.Printf("🤖 部长全自动模式启动（最大并发: %d）\n", maxConcurrent)
		if sessionFilter != "" {
			fmt.Printf("   限定会期: %s\n", sessionFilter)
		}
		fmt.Printf("   按 Ctrl+C 停止。\n\n")

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		// Run immediately on start.
		autoIterate(db, hocDir, sessionFilter, defaultProject, maxConcurrent, noTmux)

		for {
			select {
			case <-ctx.Done():
				fmt.Println("\n✓ 全自动模式已停止")
				return nil
			case <-ticker.C:
				autoIterate(db, hocDir, sessionFilter, defaultProject, maxConcurrent, noTmux)
			}
		}
	},
}

// autoIterate is one cycle of the auto loop:
//  1. Count currently working ministers (respect max-concurrent).
//  2. Find sessions with assigned-but-not-started bills (status=reading, minister=idle).
//  3. Summon those ministers (create chamber + start runtime).
func autoIterate(db *store.DB, hocDir, sessionFilter, defaultProject string, maxConcurrent int, noTmux bool) {
	// Count currently working ministers.
	working, err := db.ListWorkingMinisters()
	if err != nil {
		slog.Warn("autoIterate: list working", "err", err)
		return
	}
	if len(working) >= maxConcurrent {
		slog.Debug("auto: 已达最大并发数", "working", len(working), "max", maxConcurrent)
		return
	}

	sessions, err := db.ListActiveSessions()
	if err != nil {
		slog.Warn("autoIterate: list sessions", "err", err)
		return
	}

	for _, sess := range sessions {
		if sessionFilter != "" && sess.ID != sessionFilter {
			continue
		}

		project := sess.Project.String
		if project == "" {
			project = defaultProject
		}
		if project == "" {
			slog.Debug("auto: 会期无 project，跳过", "session_id", sess.ID)
			continue
		}

		bills, err := db.ListBillsBySession(sess.ID)
		if err != nil {
			continue
		}

		for _, bill := range bills {
			if bill.Status != "reading" || bill.Assignee.String == "" {
				continue
			}

			minister, err := db.GetMinister(bill.Assignee.String)
			if err != nil {
				continue
			}
			if minister.Status != "idle" {
				continue // Already working or offline.
			}

			if len(working) >= maxConcurrent {
				return
			}

			fmt.Printf("🤖 自动传召 %s → 议案 [%s] %s\n", minister.Title, bill.ID, bill.Title)
			if err := doSummon(db, hocDir, minister.ID, bill.ID, project, !noTmux); err != nil {
				slog.Warn("autoIterate: summon failed", "minister", minister.ID, "bill", bill.ID, "err", err)
				continue
			}

			// Refresh minister from DB for working list.
			if refreshed, err := db.GetMinister(minister.ID); err == nil {
				working = append(working, refreshed)
			}
		}
	}
}

// doSummon performs the core summon logic: create/reuse chamber, write bill brief, start runtime, update DB.
func doSummon(db *store.DB, hocDir, ministerID, billID, projectName string, useTmux bool) error {
	minister, err := db.GetMinister(ministerID)
	if err != nil {
		return fmt.Errorf("minister not found: %s", ministerID)
	}

	bill, err := db.GetBill(billID)
	if err != nil {
		return fmt.Errorf("bill not found: %s", billID)
	}

	mainRepoPath := filepath.Join(hocDir, "projects", projectName, "main")
	if _, err := os.Stat(mainRepoPath); os.IsNotExist(err) {
		return fmt.Errorf("项目 %s 不存在，请先运行 hoc project add", projectName)
	}

	ch, err := chamber.NewChamber(hocDir, projectName, ministerID, mainRepoPath)
	if err != nil {
		return fmt.Errorf("init chamber: %w", err)
	}
	if _, statErr := os.Stat(ch.GetWorktreePath()); os.IsNotExist(statErr) {
		if err := ch.Create(); err != nil {
			return fmt.Errorf("create chamber: %w", err)
		}
	}

	brief := buildBillBrief(minister, bill, ch.GetBranchName())

	upstreamGazettes, _ := db.ListGazettesForBill(billID)
	if len(upstreamGazettes) > 0 {
		var sb strings.Builder
		sb.WriteString("\n## 上游公报（来自前序部长）\n\n")
		for _, g := range upstreamGazettes {
			sb.WriteString(fmt.Sprintf("**[%s]** %s:\n\n%s\n\n---\n\n",
				g.Type.String,
				orDash(g.FromMinister.String),
				g.Summary,
			))
		}
		brief += sb.String()
	}

	rt := runtime.New(minister.Runtime, useTmux)
	opts := runtime.SummonOpts{
		MinisterID:    ministerID,
		MinisterTitle: minister.Title,
		ChamberPath:   ch.GetWorktreePath(),
		BillBrief:     brief,
	}

	agentSess, err := rt.Summon(opts)
	if err != nil {
		return fmt.Errorf("summon runtime: %w", err)
	}

	_ = db.UpdateMinisterStatus(ministerID, "working")
	_ = db.UpdateMinisterWorktree(ministerID, ch.GetWorktreePath())
	if agentSess.PID > 0 {
		_ = db.UpdateMinisterPID(ministerID, agentSess.PID)
	}
	_ = db.AssignBill(billID, ministerID)
	_ = db.UpdateBillStatus(billID, "reading")
	_ = db.UpdateBillBranch(billID, ch.GetBranchName())

	if useTmux {
		fmt.Printf("   ✅ %s 已在 tmux [hoc-%s] 就绪\n", minister.Title, ministerID)
	} else {
		fmt.Printf("   ✅ %s 已启动 (PID: %d)\n", minister.Title, agentSess.PID)
	}
	return nil
}

// runGitInDir runs a git command in the given directory and returns stdout+stderr.
func runGitInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// buildBillBrief composes the markdown brief that is injected into the Minister's chamber.
func buildBillBrief(minister *store.Minister, bill *store.Bill, branch string) string {
	var skills []string
	if minister.Skills != "" {
		_ = json.Unmarshal([]byte(minister.Skills), &skills)
	}
	skillsStr := strings.Join(skills, ", ")
	if skillsStr == "" {
		skillsStr = "通用"
	}

	motion := bill.Description.String
	if motion == "" {
		motion = bill.Title
	}

	today := time.Now().Format("2006-01-02")

	return fmt.Sprintf(`# 部长就职简报

> **你是 %s**（ID: `+"`%s`"+`），一位 AI Agent。
> 你正在 House of Cards 多 Agent 协作框架中工作。
> 技能领域：%s

---

## 你的议案（Bill）

| 字段 | 值 |
|------|----|
| **议案 ID** | `+"`%s`"+` |
| **标题** | %s |
| **状态** | %s → In Progress |
| **工作分支** | `+"`%s`"+` |
| **日期** | %s |

## 任务指示（Motion）

%s

---

## 工作规范

1. 你正在 **git worktree（议事厅）** 中工作，分支为 `+"`%s`"+`，已与 main 分离。
2. 专注完成上述议案，不要做额外的事情。
3. 完成后，**在当前目录的 `+"`gazettes/%s.md`"+` 中创建公报**（见模板）。
4. 提交所有代码：

   `+"`"+`git add -A && git commit -m "bill(%s): %s"`+"`"+`

5. **最后一步（必须执行）**：写入完成信号文件，让 Whip 自动将议案标记为 enacted：

   `+"```bash"+`
   mkdir -p .hoc
   echo "工作已完成。[简短摘要]" > .hoc/bill-%s.done
   `+"```"+`

---

## 完成后公报模板

将以下内容写入 `+"`gazettes/%s.md`"+`：

`+"```markdown"+`
# Gazette: %s
> From: %s | Bill: %s | Date: %s

## 决议
[3 句话以内描述你完成了什么]

## 变更清单
- `+"`file/path`"+` — 说明

## 接口契约（下游部长需要知道的）
[如有 API/接口，列出这里；否则写"无"]

## 假设与风险
[列出关键假设；否则写"无"]

## 状态
✅ Enacted | 测试通过 | 分支: %s
`+"```"+`

---

*议案已就绪，请开始工作。*
`,
		minister.Title,
		minister.ID,
		skillsStr,
		bill.ID,
		bill.Title,
		bill.Status,
		branch,
		today,
		motion,
		branch,
		bill.ID,
		bill.ID,
		bill.ID,
		bill.ID, // done file: .hoc/bill-%s.done
		bill.Title,
		bill.ID,
		minister.Title,
		bill.ID,
		today,
		branch,
	)
}

// ─── Phase 3B: Minister Hook Queue Commands ───────────────────────────────────

// ministerHookCmd is the parent command for hook queue management.
var ministerHookCmd = &cobra.Command{
	Use:   "hook",
	Short: "管理 Minister 的 Hook 队列（Phase 3B）",
	Long: `Hook 队列允许预先为 Minister 排队分配议案。
当 Minister 完成当前议案变为 idle 时，Whip 自动从队列中接单。

这解决了 cabinet reshuffle 每个 Minister 只分配一份议案的限制。`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// ministerHookPushCmd enqueues a bill into a minister's hook queue.
var ministerHookPushCmd = &cobra.Command{
	Use:   "push <minister-id> <bill-id>",
	Short: "将议案加入 Minister 的 Hook 队列",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministerID := args[0]
		billID := args[1]

		// Verify minister exists.
		if _, err := db.GetMinister(ministerID); err != nil {
			return fmt.Errorf("minister not found: %s", ministerID)
		}
		// Verify bill exists.
		if _, err := db.GetBill(billID); err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		if err := db.PushHook(ministerID, billID); err != nil {
			return fmt.Errorf("push hook: %w", err)
		}

		queue, _ := db.PeekHook(ministerID)
		fmt.Printf("✅ 议案 [%s] 已加入 %s 的 Hook 队列（队列长度: %d）\n", billID, ministerID, len(queue))
		return nil
	},
}

// ministerHookListCmd shows the hook queue for a minister.
var ministerHookListCmd = &cobra.Command{
	Use:   "list <minister-id>",
	Short: "查看 Minister 的 Hook 队列",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministerID := args[0]
		queue, err := db.PeekHook(ministerID)
		if err != nil {
			return fmt.Errorf("peek hook: %w", err)
		}

		if len(queue) == 0 {
			fmt.Printf("📭 %s 的 Hook 队列为空\n", ministerID)
			return nil
		}

		fmt.Printf("📋 %s 的 Hook 队列（%d 个议案）：\n", ministerID, len(queue))
		for i, billID := range queue {
			b, err := db.GetBill(billID)
			title := billID
			status := "?"
			if err == nil {
				title = b.Title
				status = b.Status
			}
			fmt.Printf("  %d. [%s] %s (%s)\n", i+1, billID, title, status)
		}
		return nil
	},
}

// ministerHookPopCmd removes the first item from a minister's hook queue.
var ministerHookPopCmd = &cobra.Command{
	Use:   "pop <minister-id>",
	Short: "从 Minister 的 Hook 队列取出最早的议案",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministerID := args[0]
		billID, err := db.PopHook(ministerID)
		if err != nil {
			return fmt.Errorf("pop hook: %w", err)
		}

		if billID == "" {
			fmt.Printf("📭 %s 的 Hook 队列为空\n", ministerID)
			return nil
		}

		fmt.Printf("✅ 已取出议案 [%s]\n", billID)
		return nil
	},
}
