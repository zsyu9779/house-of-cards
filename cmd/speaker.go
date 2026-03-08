package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/house-of-cards/hoc/internal/speaker"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

// speakerCmd represents the speaker command
var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Speaker（议长）管理",
	Long:  "议长管理命令：传召议长、查看议长备忘录、议长巡视、多议长竞标",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	speakerCmd.AddCommand(speakerSummonCmd)
	speakerCmd.AddCommand(speakerContextCmd)
	speakerCmd.AddCommand(speakerPatrolCmd)
	speakerCmd.AddCommand(speakerCouncilCmd)

	speakerSummonCmd.Flags().Bool("no-tmux", false, "前台运行（不使用 tmux）")
	speakerContextCmd.Flags().Bool("refresh", false, "重新生成 context.md 后显示")
	speakerPatrolCmd.Flags().Int("interval", 60, "巡视间隔（秒）")
	speakerPatrolCmd.Flags().Bool("once", false, "只运行一次巡视，不循环")
	speakerCouncilCmd.Flags().String("goal", "", "需要多议长竞标的目标描述")
}

// speakerSummonCmd starts the Speaker AI session.
var speakerSummonCmd = &cobra.Command{
	Use:   "summon",
	Short: "传召议长（启动 Speaker AI session）",
	Long: `传召议长（Speaker），以当前政府状态作为上下文启动 AI 会话。

议长会读取 .hoc/speaker/context.md（当前政府状态备忘录），
并帮助你：
  • 将需求拆解为 Bills 和 Session TOML
  • 分析当前会期进度并给出建议
  • 基于 Gazette 公报做调度决策

示例：
  hoc speaker summon            # 在 tmux 会话 hoc-speaker 中启动
  hoc speaker summon --no-tmux  # 前台交互模式`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		noTmux, _ := cmd.Flags().GetBool("no-tmux")

		// Always refresh context before summoning.
		fmt.Println("📝 更新议长备忘录...")
		content, err := speaker.GenerateContext(db)
		if err != nil {
			return fmt.Errorf("generate context: %w", err)
		}
		if err := speaker.WriteContext(hocDir, content); err != nil {
			return fmt.Errorf("write context: %w", err)
		}

		fmt.Println("🎙  传召议长 (Speaker)...")
		return speaker.Summon(hocDir, !noTmux)
	},
}

// speakerContextCmd shows or refreshes the Speaker context.
var speakerContextCmd = &cobra.Command{
	Use:   "context",
	Short: "查看议长备忘录（context.md）",
	Long: `显示当前议长备忘录内容。
使用 --refresh 先重新生成再显示。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		refresh, _ := cmd.Flags().GetBool("refresh")

		if refresh {
			content, err := speaker.GenerateContext(db)
			if err != nil {
				return fmt.Errorf("generate context: %w", err)
			}
			if err := speaker.WriteContext(hocDir, content); err != nil {
				return fmt.Errorf("write context: %w", err)
			}
			fmt.Println("✓ 议长备忘录已刷新")
			fmt.Println()
		}

		ctxPath := speaker.ContextPath(hocDir)
		data, err := os.ReadFile(ctxPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("备忘录不存在，请先运行：hoc speaker context --refresh")
			}
			return fmt.Errorf("read context: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}

// speakerPatrolCmd runs the Speaker in automated patrol mode.
var speakerPatrolCmd = &cobra.Command{
	Use:   "patrol",
	Short: "议长巡视（自动决策循环）",
	Long: `以指定间隔自动运行 Speaker AI 巡视，读取决策指令并执行。

支持的指令：
  [DIRECTIVE] assign <bill-id> <minister-id>  - 分配议案
  [DIRECTIVE] by-election <minister-id>      - 触发补选
  [DIRECTIVE] escalate <bill-id>             - 升级议案

示例：
  hoc speaker patrol              # 每 60 秒巡视一次
  hoc speaker patrol --once      # 只运行一次
  hoc speaker patrol --interval 30 # 每 30 秒巡视`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		intervalSec, _ := cmd.Flags().GetInt("interval")
		once, _ := cmd.Flags().GetBool("once")

		interval := time.Duration(intervalSec) * time.Second

		fmt.Printf("🎙  启动议长巡视模式（间隔 %d 秒）\n", intervalSec)
		fmt.Println("   按 Ctrl+C 停止")
		fmt.Println()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Println("\n⏹  停止巡视")
			cancel()
		}()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run immediately on startup.
		runPatrol(ctx)

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				runPatrol(ctx)
			}
			if once {
				fmt.Println("✓ 单次巡视完成")
				return nil
			}
		}
	},
}

func runPatrol(ctx context.Context) {
	// Refresh context before each patrol.
	content, err := speaker.GenerateContext(db)
	if err != nil {
		slog.Warn("生成议长上下文失败", "err", err)
		return
	}
	if err := speaker.WriteContext(hocDir, content); err != nil {
		slog.Warn("写入议长上下文失败", "err", err)
		return
	}

	// Run Speaker in patrol mode and get decisions.
	decisions, err := speaker.RunPatrol(hocDir, content)
	if err != nil {
		slog.Warn("议长巡视失败", "err", err)
		fmt.Printf("⚠  巡视失败: %v\n", err)
		return
	}

	if len(decisions) == 0 {
		fmt.Printf("[%s] 无需行动\n", time.Now().Format("15:04:05"))
		return
	}

	// Execute each decision.
	fmt.Printf("[%s] 收到 %d 条指令\n", time.Now().Format("15:04:05"), len(decisions))
	for _, d := range decisions {
		execDecision(d)
	}
}

func execDecision(d speaker.Decision) {
	switch d.Action {
	case "assign":
		// bill assign <bill-id> <minister-id>
		fmt.Printf("   → 分配议案 [%s] 给部长 [%s]\n", d.Target, d.Secondary)
		bill, err := db.GetBill(d.Target)
		if err != nil {
			slog.Warn("分配失败：议案不存在", "bill", d.Target)
			fmt.Printf("      ⚠ 议案不存在: %s\n", d.Target)
			return
		}
		minister, err := db.GetMinister(d.Secondary)
		if err != nil {
			slog.Warn("分配失败：部长不存在", "minister", d.Secondary)
			fmt.Printf("      ⚠ 部长不存在: %s\n", d.Secondary)
			return
		}
		if err := db.AssignBill(d.Target, d.Secondary); err != nil {
			slog.Warn("分配失败", "err", err)
			return
		}
		if err := db.UpdateBillStatus(d.Target, "reading"); err != nil {
			slog.Warn("更新状态失败", "err", err)
			return
		}
		branch := fmt.Sprintf("minister/%s", d.Secondary)
		_ = db.UpdateBillBranch(d.Target, branch)

		// Create handoff gazette.
		gazetteID := fmt.Sprintf("gaz-%d", time.Now().UnixNano())
		g := &store.Gazette{
			ID:         gazetteID,
			ToMinister: store.NullString(d.Secondary),
			BillID:     store.NullString(d.Target),
			Type:       store.NullString("handoff"),
			Summary:    fmt.Sprintf("由议长巡视自动分配：议案 [%s] \"%s\" 已分配给 %s", d.Target, bill.Title, minister.Title),
		}
		_ = db.CreateGazette(g)
		fmt.Printf("      ✅ 完成\n")

	case "by-election":
		// Trigger by-election for stuck minister.
		fmt.Printf("   → 触发补选：部长 [%s]\n", d.Target)
		// Verify minister exists but don't fail if already gone.
		_, err := db.GetMinister(d.Target)
		if err != nil {
			slog.Warn("补选失败：部长不存在", "minister", d.Target)
			fmt.Printf("      ⚠ 部长不存在: %s\n", d.Target)
		}
		// Get current bill if any.
		bills, _ := db.GetBillsByAssignee(d.Target)
		var billID string
		if len(bills) > 0 {
			billID = bills[0].ID
		}

		// Mark minister as offline.
		_ = db.UpdateMinisterStatus(d.Target, "offline")

		// Clear bill assignment if exists.
		if billID != "" {
			_ = db.ClearBillAssignment(billID)
		}

		// Create handoff gazette.
		if billID != "" {
			gazetteID := fmt.Sprintf("gaz-%d", time.Now().UnixNano())
			g := &store.Gazette{
				ID:      gazetteID,
				BillID:  store.NullString(billID),
				Type:    store.NullString("handoff"),
				Summary: fmt.Sprintf("补选触发：部长 [%s] 离开，议案 [%s] 待重新分配", d.Target, billID),
			}
			_ = db.CreateGazette(g)
		}

		// Create hansard record.
		if billID != "" {
			h := &store.Hansard{
				ID:         fmt.Sprintf("hansard-%d", time.Now().UnixNano()),
				MinisterID: d.Target,
				BillID:     billID,
				Outcome:    store.NullString("failed"),
				Notes:      store.NullString("补选触发：由 Speaker 巡视自动检测到 stuck 状态"),
			}
			_ = db.CreateHansard(h)
		}

		fmt.Printf("      ✅ 补选完成，部长已标记为 offline\n")

	case "escalate":
		// Mark bill as needing special attention.
		fmt.Printf("   → 升级议案：%s\n", d.Target)
		bill, err := db.GetBill(d.Target)
		if err != nil {
			slog.Warn("升级失败：议案不存在", "bill", d.Target)
			fmt.Printf("      ⚠ 议案不存在: %s\n", d.Target)
			return
		}
		// Add a note to the bill (via description or create a gazette).
		gazetteID := fmt.Sprintf("gaz-%d", time.Now().UnixNano())
		g := &store.Gazette{
			ID:      gazetteID,
			Type:    store.NullString("escalation"),
			Summary: fmt.Sprintf("⚠ 议案 [%s] \"%s\" 已升级到议长，需要特别关注", d.Target, bill.Title),
		}
		_ = db.CreateGazette(g)
		fmt.Printf("      ✅ 已生成升级公报\n")

	default:
		fmt.Printf("   ⚠ 未知指令: %s\n", d.Action)
	}
}

// speakerCouncilCmd runs experimental multi-Speaker bidding.
var speakerCouncilCmd = &cobra.Command{
	Use:   "council",
	Short: "多议长竞标（实验性）",
	Long: `运行多个独立 Speaker 实例并行生成决策，以多数投票方式确定最终行动。

这是一个实验性功能，用于探索多 Agent 共识决策。

示例：
  hoc speaker council --goal "实现用户认证系统"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		goal, _ := cmd.Flags().GetString("goal")
		if goal == "" {
			// Read from stdin if not provided.
			fmt.Print("请输入需要竞标的目标：")
			fmt.Scanln(&goal)
		}
		if goal == "" {
			return fmt.Errorf("目标不能为空，请使用 --goal 指定")
		}

		fmt.Println("🎙  启动多议长竞标...")
		fmt.Printf("   目标: %s\n\n", goal)

		// Generate base context.
		content, err := speaker.GenerateContext(db)
		if err != nil {
			return fmt.Errorf("generate context: %w", err)
		}

		// Run council (placeholder - would need full implementation).
		fmt.Println("⚠  多议长竞标功能为实验性功能，当前仅生成单议长建议：")
		fmt.Println()

		// For now, just run a single Speaker and show its output.
		patrolDecisions, err := speaker.RunPatrol(hocDir, content)
		if err != nil {
			return fmt.Errorf("run speaker: %w", err)
		}

		if len(patrolDecisions) == 0 {
			fmt.Println("   议长建议：无需行动")
		} else {
			fmt.Println("   议长建议：")
			for _, d := range patrolDecisions {
				fmt.Printf("   - %s %s", d.Action, d.Target)
				if d.Secondary != "" {
					fmt.Printf(" %s", d.Secondary)
				}
				fmt.Println()
			}
		}

		// Write council result.
		resultPath := fmt.Sprintf("%s/.hoc/council-%d.md", hocDir, time.Now().Unix())
		result := fmt.Sprintf("# Council Result\n\nGoal: %s\n\nDecisions:\n", goal)
		for _, d := range patrolDecisions {
			result += fmt.Sprintf("- %s %s\n", d.Action, d.Target)
		}
		_ = os.WriteFile(resultPath, []byte(result), 0644)
		fmt.Printf("\n✓ 竞标结果已保存: %s\n", resultPath)

		return nil
	},
}
