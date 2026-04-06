package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
	"github.com/spf13/cobra"
)

// billCmd represents the bill command.
var billCmd = &cobra.Command{
	Use:   "bill",
	Short: "管理 Bill（议案）",
	Long:  "议案管理命令：起草、分配、查看",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	billCmd.AddCommand(billListCmd)
	billCmd.AddCommand(billShowCmd)
	billCmd.AddCommand(billDraftCmd)
	billCmd.AddCommand(billAssignCmd)
	billCmd.AddCommand(billEnactedCmd)
	billCmd.AddCommand(billCommitteeCmd)
	billCmd.AddCommand(billReviewCmd)
	billCmd.AddCommand(billPauseCmd)
	billCmd.AddCommand(billResumeCmd)
	billCmd.AddCommand(billSplitCmd)

	billListCmd.Flags().Bool("json", false, "以 JSON 格式输出")

	billDraftCmd.Flags().String("title", "", "议案标题 (必填)")
	billDraftCmd.Flags().String("session", "", "所属会期 ID")
	billDraftCmd.Flags().String("motion", "", "议案指示（描述需要做什么）")
	billDraftCmd.Flags().String("portfolio", "", "所需技能（如 go,react）")
	billDraftCmd.Flags().Bool("force", false, "跳过标题校验")
	_ = billDraftCmd.MarkFlagRequired("title")

	billEnactedCmd.Flags().Float64("quality", 0, "质量评分 (0.0-1.0)")
	billEnactedCmd.Flags().String("notes", "", "备注（委员会意见等）")
	billEnactedCmd.Flags().Int("duration", 0, "耗时（秒）")

	billReviewCmd.Flags().Bool("pass", false, "审查通过（committee → enacted）")
	billReviewCmd.Flags().Bool("fail", false, "审查未通过（committee → reading，退回修改）")
	billReviewCmd.Flags().String("notes", "", "委员会审查意见")
	billReviewCmd.Flags().Float64("quality", 0, "质量评分 (0.0-1.0)")

	billPauseCmd.Flags().String("reason", "", "暂停原因")
	billSplitCmd.Flags().StringSlice("into", nil, "子议案标题列表（必填）")
	_ = billSplitCmd.MarkFlagRequired("into")
}

var billListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有议案",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		bills, err := db.ListBills()
		if err != nil {
			return fmt.Errorf("list bills: %w", err)
		}

		jsonMode, _ := cmd.Flags().GetBool("json")
		if jsonMode {
			type billJSON struct {
				ID        string `json:"id"`
				Title     string `json:"title"`
				Status    string `json:"status"`
				Assignee  string `json:"assignee"`
				Session   string `json:"session_id"`
				Branch    string `json:"branch"`
				Portfolio string `json:"portfolio"`
			}
			out := make([]billJSON, 0, len(bills))
			for _, b := range bills {
				out = append(out, billJSON{
					ID:        b.ID,
					Title:     b.Title,
					Status:    b.Status,
					Assignee:  b.Assignee.String,
					Session:   b.SessionID.String,
					Branch:    b.Branch.String,
					Portfolio: b.Portfolio.String,
				})
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		if len(bills) == 0 {
			fmt.Println("暂无议案。使用 `hoc session open` 或 `hoc bill draft` 创建议案。")
			return nil
		}

		fmt.Println("📋 议案列表:")
		fmt.Println("─────────────────────────────────────────")
		for _, b := range bills {
			icon := billStatusIcon(b.Status)
			assignee := b.Assignee.String
			if assignee == "" {
				assignee = "(未分配)"
			}
			session := b.SessionID.String
			if session == "" {
				session = "-"
			}
			fmt.Printf("%s [%s] %s\n", icon, b.Status, b.Title)
			fmt.Printf("   ID: %-24s  会期: %s\n", b.ID, session)
			fmt.Printf("   负责人: %-20s  分支: %s\n", assignee, orDash(b.Branch.String))
		}
		return nil
	},
}

var billShowCmd = &cobra.Command{
	Use:   "show [bill-id]",
	Short: "查看议案详情",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		b, err := db.GetBill(args[0])
		if err != nil {
			return fmt.Errorf("bill not found: %s", args[0])
		}

		icon := billStatusIcon(b.Status)
		fmt.Printf("%s 议案详情\n", icon)
		fmt.Println("────────────────────────────────")
		fmt.Printf("ID:       %s\n", b.ID)
		fmt.Printf("标题:     %s\n", b.Title)
		fmt.Printf("状态:     %s\n", b.Status)
		fmt.Printf("会期:     %s\n", orDash(b.SessionID.String))
		fmt.Printf("负责人:   %s\n", orDash(b.Assignee.String))
		fmt.Printf("分支:     %s\n", orDash(b.Branch.String))
		fmt.Printf("创建于:   %s\n", b.CreatedAt.Format(time.RFC3339))
		if b.UpdatedAt.Valid {
			fmt.Printf("更新于:   %s\n", b.UpdatedAt.Time.Format(time.RFC3339))
		}
		if b.Description.String != "" {
			fmt.Printf("\n指示（Motion）:\n%s\n", b.Description.String)
		}
		if b.DependsOn.String != "" && b.DependsOn.String != "[]" {
			fmt.Printf("\n依赖:\n%s\n", b.DependsOn.String)
		}

		// Show complexity estimate.
		complexity, conf := util.EstimateBillComplexity(b.Title, b.Description.String)
		fmt.Printf("\n复杂度预测:  %s %s (%.0f%% 置信度)\n",
			util.ComplexityIcon(complexity), complexity, conf*100)

		// Show gazettes for this bill.
		gazettes, err := db.ListGazettesForBill(b.ID)
		if err == nil && len(gazettes) > 0 {
			fmt.Printf("\n公报（%d 份）:\n", len(gazettes))
			for _, g := range gazettes {
				fmt.Printf("  [%s] %s → %s: %s\n",
					g.Type.String,
					orDash(g.FromMinister.String),
					orDash(g.ToMinister.String),
					truncate(g.Summary, 60),
				)
			}
		}
		return nil
	},
}

var billDraftCmd = &cobra.Command{
	Use:   "draft",
	Short: "起草新议案",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		title, _ := cmd.Flags().GetString("title")
		sessionID, _ := cmd.Flags().GetString("session")
		motion, _ := cmd.Flags().GetString("motion")

		// D-2: Input Guard
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			if err := store.ValidateBillTitle(title); err != nil {
				return fmt.Errorf("议案标题校验失败: %w\n使用 --force 跳过校验", err)
			}
		}

		billID := shortID("bill")

		bill := &store.Bill{
			ID:          billID,
			SessionID:   store.NullString(sessionID),
			Title:       title,
			Description: store.NullString(motion),
			Status:      "draft",
			DependsOn:   store.NullString("[]"),
		}

		if err := db.CreateBill(bill); err != nil {
			return fmt.Errorf("create bill: %w", err)
		}
		_ = db.RecordEvent("bill.created", "cli", billID, "", bill.SessionID.String, "")

		fmt.Printf("📄 议案已起草\n")
		fmt.Printf("   ID:    %s\n", billID)
		fmt.Printf("   标题:  %s\n", title)
		if motion != "" {
			fmt.Printf("   指示:  %s\n", motion)
		}
		fmt.Printf("\n使用 `hoc bill assign %s <minister-id>` 分配议案。\n", billID)
		return nil
	},
}

var billAssignCmd = &cobra.Command{
	Use:   "assign [bill-id] [minister-id]",
	Short: "将议案分配给部长",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		billID := args[0]
		ministerID := args[1]

		// Verify bill exists.
		b, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		// Verify minister exists.
		m, err := db.GetMinister(ministerID)
		if err != nil {
			return fmt.Errorf("minister not found: %s", ministerID)
		}

		// Assign and update bill status to reading.
		if err := db.AssignBill(billID, ministerID); err != nil {
			return fmt.Errorf("assign bill: %w", err)
		}
		if err := db.UpdateBillStatus(billID, "reading"); err != nil {
			return fmt.Errorf("update bill status: %w", err)
		}

		// Record branch name.
		branch := fmt.Sprintf("minister/%s", ministerID)
		if err := db.UpdateBillBranch(billID, branch); err != nil {
			slog.Warn("could not update branch", "err", err)
		}

		fmt.Printf("✓ 议案 [%s] %s 已分配给 %s\n", billID, b.Title, m.Title)
		fmt.Printf("  状态: draft → reading\n")
		fmt.Printf("  分支: %s\n", branch)
		fmt.Printf("\n传召部长: `hoc minister summon %s --bill %s --project <project>`\n",
			ministerID, billID)

		// Auto-log bill assignment to gazette store.
		gazetteID := shortID("gazette")
		summary := fmt.Sprintf("议案 [%s] \"%s\" 已由议长分配给 %s，进入一读阶段。",
			billID, b.Title, m.Title)
		g := &store.Gazette{
			ID:         gazetteID,
			ToMinister: store.NullString(ministerID),
			BillID:     store.NullString(billID),
			Type:       store.NullString("handoff"),
			Summary:    summary,
		}
		_ = db.CreateGazette(g)

		return nil
	},
}

var billEnactedCmd = &cobra.Command{
	Use:   "enacted [bill-id]",
	Short: "标记议案通过（Enacted）并写入 Hansard",
	Long: `将议案标记为 enacted 状态并在 Hansard（议事录）中记录。
同时创建一份 completion Gazette。

示例：
  hoc bill enacted 5f058ac9-auth-api
  hoc bill enacted 5f058ac9-auth-api --quality 0.9 --notes "代码整洁，测试充分"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		billID := args[0]
		quality, _ := cmd.Flags().GetFloat64("quality")
		notes, _ := cmd.Flags().GetString("notes")
		duration, _ := cmd.Flags().GetInt("duration")

		b, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		// Update bill status.
		if err := db.UpdateBillStatus(billID, "enacted"); err != nil {
			return fmt.Errorf("update bill status: %w", err)
		}

		// Write Hansard entry.
		ministerID := b.Assignee.String
		if ministerID != "" {
			h := &store.Hansard{
				ID:         shortID("hansard"),
				MinisterID: ministerID,
				BillID:     billID,
				Outcome:    store.NullString("enacted"),
				DurationS:  duration,
				Quality:    quality,
				Notes:      store.NullString(notes),
			}
			if err := db.CreateHansard(h); err != nil {
				slog.Warn("create hansard", "err", err)
			}
		}

		// Create completion Gazette.
		summary := fmt.Sprintf("议案 [%s] \"%s\" 已通过（Enacted）", billID, b.Title)
		if notes != "" {
			summary += "。备注：" + notes
		}
		g := &store.Gazette{
			ID:           shortID("gazette"),
			FromMinister: store.NullString(ministerID),
			BillID:       store.NullString(billID),
			Type:         store.NullString("completion"),
			Summary:      summary,
		}
		_ = db.CreateGazette(g)

		fmt.Printf("✅ 议案 [%s] \"%s\" 已标记为 Enacted\n", billID, b.Title)
		if ministerID != "" {
			fmt.Printf("   部长: %s  已写入 Hansard\n", ministerID)
		}
		if quality > 0 {
			fmt.Printf("   质量评分: %.2f\n", quality)
		}
		return nil
	},
}

var billCommitteeCmd = &cobra.Command{
	Use:   "committee [bill-id]",
	Short: "提交议案至委员会审查（reading → committee）",
	Long: `部长完成工作后，将议案提交至委员会审查。
状态转换：reading → committee

审查完成后，使用 hoc bill review <id> --pass/--fail 记录审查结论。`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		billID := args[0]
		b, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		if b.Status != "reading" && b.Status != "draft" {
			return fmt.Errorf("议案当前状态为 [%s]，只有 reading/draft 状态的议案可提交委员会", b.Status)
		}

		if err := db.UpdateBillStatus(billID, "committee"); err != nil {
			return fmt.Errorf("update bill status: %w", err)
		}

		// Create a gazette announcing the committee review.
		ministerID := b.Assignee.String
		summary := fmt.Sprintf("议案 [%s] \"%s\" 已提交委员会审查。部长: %s",
			billID, b.Title, orDash(ministerID))
		g := &store.Gazette{
			ID:           shortID("gazette"),
			FromMinister: store.NullString(ministerID),
			BillID:       store.NullString(billID),
			Type:         store.NullString("review"),
			Summary:      summary,
		}
		_ = db.CreateGazette(g)

		fmt.Printf("📋 议案 [%s] \"%s\" 已提交委员会\n", billID, b.Title)
		fmt.Printf("   状态: %s → committee\n", b.Status)
		fmt.Printf("\n委员会审查后，运行:\n")
		fmt.Printf("  hoc bill review %s --pass              # 通过\n", billID)
		fmt.Printf("  hoc bill review %s --fail --notes \"...\" # 退回\n", billID)
		return nil
	},
}

var billReviewCmd = &cobra.Command{
	Use:   "review [bill-id]",
	Short: "委员会审查结论（committee → enacted 或 → reading）",
	Long: `记录委员会对议案的审查结论：
  --pass  审查通过 → 状态变为 enacted
  --fail  审查未通过 → 状态退回为 reading（请部长修改后再次提交）`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		billID := args[0]
		pass, _ := cmd.Flags().GetBool("pass")
		fail, _ := cmd.Flags().GetBool("fail")
		notes, _ := cmd.Flags().GetString("notes")
		quality, _ := cmd.Flags().GetFloat64("quality")

		if !pass && !fail {
			return fmt.Errorf("必须指定 --pass 或 --fail")
		}
		if pass && fail {
			return fmt.Errorf("--pass 和 --fail 不能同时使用")
		}

		b, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		if b.Status != "committee" {
			return fmt.Errorf("议案当前状态为 [%s]，只有 committee 状态的议案可接受审查结论", b.Status)
		}

		ministerID := b.Assignee.String

		if pass {
			// Committee approved: enacted.
			if err := db.UpdateBillStatus(billID, "enacted"); err != nil {
				return fmt.Errorf("update bill status: %w", err)
			}

			// Write Hansard entry.
			if ministerID != "" {
				h := &store.Hansard{
					ID:         shortID("hansard"),
					MinisterID: ministerID,
					BillID:     billID,
					Outcome:    store.NullString("enacted"),
					Quality:    quality,
					Notes:      store.NullString(notes),
				}
				_ = db.CreateHansard(h)
			}

			// Create Review Gazette (pass).
			summary := fmt.Sprintf("委员会审查通过：议案 [%s] \"%s\" 已 Enacted。", billID, b.Title)
			if notes != "" {
				summary += "\n审查意见：" + notes
			}
			if quality > 0 {
				summary += fmt.Sprintf("\n质量评分：%.2f", quality)
			}
			g := &store.Gazette{
				ID:         shortID("gazette"),
				ToMinister: store.NullString(ministerID),
				BillID:     store.NullString(billID),
				Type:       store.NullString("review"),
				Summary:    summary,
			}
			_ = db.CreateGazette(g)

			fmt.Printf("✅ 委员会审查通过：议案 [%s] \"%s\"\n", billID, b.Title)
			fmt.Printf("   状态: committee → enacted\n")
			if quality > 0 {
				fmt.Printf("   质量评分: %.2f\n", quality)
			}
		} else {
			// Committee rejected: back to reading.
			if err := db.UpdateBillStatus(billID, "reading"); err != nil {
				return fmt.Errorf("update bill status: %w", err)
			}

			// Create Review Gazette (reject).
			summary := fmt.Sprintf("委员会审查未通过：议案 [%s] \"%s\" 退回修改。", billID, b.Title)
			if notes != "" {
				summary += "\n退回理由：" + notes
			}
			g := &store.Gazette{
				ID:         shortID("gazette"),
				ToMinister: store.NullString(ministerID),
				BillID:     store.NullString(billID),
				Type:       store.NullString("review"),
				Summary:    summary,
			}
			_ = db.CreateGazette(g)

			fmt.Printf("🔄 委员会审查退回：议案 [%s] \"%s\"\n", billID, b.Title)
			fmt.Printf("   状态: committee → reading（请部长修改后重新提交）\n")
			if notes != "" {
				fmt.Printf("   退回理由: %s\n", notes)
			}
		}
		return nil
	},
}

// ─── Phase 3: D-3 Governance Commands ──────────────────────────────────────

var billPauseCmd = &cobra.Command{
	Use:   "pause [bill-id]",
	Short: "暂停议案（→ draft，清除 assignee）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		billID := args[0]
		reason, _ := cmd.Flags().GetString("reason")

		b, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		if b.Status == "enacted" || b.Status == "royal_assent" || b.Status == "failed" {
			return fmt.Errorf("议案状态为 [%s]，终态议案不可暂停", b.Status)
		}

		prevStatus := b.Status
		if err := db.UpdateBillStatus(billID, "draft"); err != nil {
			return fmt.Errorf("update bill status: %w", err)
		}
		if err := db.ClearBillAssignment(billID); err != nil {
			slog.Warn("clear bill assignment", "err", err)
		}

		payload := fmt.Sprintf(`{"prev_status":"%s","reason":"%s"}`, prevStatus, reason)
		_ = db.RecordEvent("governance.bill_paused", "cli", billID, b.Assignee.String, b.SessionID.String, payload)

		summary := fmt.Sprintf("治理公报：议案 [%s] \"%s\" 已暂停（%s → draft）。", billID, b.Title, prevStatus)
		if reason != "" {
			summary += " 原因：" + reason
		}
		g := &store.Gazette{
			ID:      shortID("gazette"),
			BillID:  store.NullString(billID),
			Type:    store.NullString("handoff"),
			Summary: summary,
		}
		_ = db.CreateGazette(g)

		fmt.Printf("⏸  议案 [%s] \"%s\" 已暂停\n", billID, b.Title)
		fmt.Printf("   状态: %s → draft  负责人: 已清除\n", prevStatus)
		if reason != "" {
			fmt.Printf("   原因: %s\n", reason)
		}
		return nil
	},
}

var billResumeCmd = &cobra.Command{
	Use:   "resume [bill-id]",
	Short: "恢复已暂停的议案",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		billID := args[0]
		b, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		if b.Status != "draft" {
			return fmt.Errorf("议案状态为 [%s]，只有 draft 状态的议案可恢复", b.Status)
		}

		_ = db.RecordEvent("governance.bill_resumed", "cli", billID, "", b.SessionID.String, "")

		fmt.Printf("▶  议案 [%s] \"%s\" 已恢复（状态: draft，可被 Whip 重新派发）\n", billID, b.Title)
		return nil
	},
}

// ─── Phase 3: B-3 Bill Split ───────────────────────────────────────────────

var billSplitCmd = &cobra.Command{
	Use:   "split [bill-id]",
	Short: "拆分议案为多个子议案",
	Long: `将一个议案拆分为多个子议案。原议案变为 epic 状态。

示例：
  hoc bill split bill-abc --into "实现API","编写测试","更新文档"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		billID := args[0]
		subTitles, _ := cmd.Flags().GetStringSlice("into")

		b, err := db.GetBill(billID)
		if err != nil {
			return fmt.Errorf("bill not found: %s", billID)
		}

		if b.Status != "draft" && b.Status != "reading" {
			return fmt.Errorf("议案状态为 [%s]，只有 draft/reading 状态的议案可拆分", b.Status)
		}

		if len(subTitles) < 2 {
			return fmt.Errorf("至少需要拆分为 2 个子议案")
		}

		// Create sub-bills.
		var subIDs []string
		for _, title := range subTitles {
			subID := shortID("bill")
			sub := &store.Bill{
				ID:         subID,
				SessionID:  b.SessionID,
				Title:      title,
				Status:     "draft",
				DependsOn:  store.NullString("[]"),
				Portfolio:  b.Portfolio,
				Project:    b.Project,
				ParentBill: billID,
			}
			if err := db.CreateBill(sub); err != nil {
				return fmt.Errorf("create sub-bill: %w", err)
			}
			subIDs = append(subIDs, subID)
			fmt.Printf("   📄 子议案 [%s] %s\n", subID, title)
		}

		// Mark original as epic.
		if err := db.UpdateBillStatus(billID, "epic"); err != nil {
			return fmt.Errorf("update bill status: %w", err)
		}

		payload := fmt.Sprintf(`{"sub_bills":%d}`, len(subIDs))
		_ = db.RecordEvent("bill.split", "cli", billID, "", b.SessionID.String, payload)

		fmt.Printf("\n✂  议案 [%s] \"%s\" 已拆分为 %d 个子议案（状态: epic）\n", billID, b.Title, len(subIDs))
		return nil
	},
}

