package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

// billCmd represents the bill command
var billCmd = &cobra.Command{
	Use:   "bill",
	Short: "管理 Bill（议案）",
	Long:  "议案管理命令：起草、分配、查看",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	billCmd.AddCommand(billListCmd)
	billCmd.AddCommand(billShowCmd)
	billCmd.AddCommand(billDraftCmd)
	billCmd.AddCommand(billAssignCmd)
	billCmd.AddCommand(billEnactedCmd)

	billDraftCmd.Flags().String("title", "", "议案标题 (必填)")
	billDraftCmd.Flags().String("session", "", "所属会期 ID")
	billDraftCmd.Flags().String("motion", "", "议案指示（描述需要做什么）")
	billDraftCmd.Flags().String("portfolio", "", "所需技能（如 go,react）")
	_ = billDraftCmd.MarkFlagRequired("title")

	billEnactedCmd.Flags().Float64("quality", 0, "质量评分 (0.0-1.0)")
	billEnactedCmd.Flags().String("notes", "", "备注（委员会意见等）")
	billEnactedCmd.Flags().Int("duration", 0, "耗时（秒）")
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
			// Non-fatal.
			fmt.Fprintf(os.Stderr, "warning: could not update branch: %v\n", err)
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
			ID:           gazetteID,
			ToMinister:   store.NullString(ministerID),
			BillID:       store.NullString(billID),
			Type:         store.NullString("handoff"),
			Summary:      summary,
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
				fmt.Fprintf(os.Stderr, "warning: create hansard: %v\n", err)
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

// Add a helper to print billspec depends in readable format.
func dependsOnStr(raw string) string {
	if raw == "" || raw == "[]" {
		return "-"
	}
	var deps []string
	if err := json.Unmarshal([]byte(raw), &deps); err != nil {
		return raw
	}
	return strings.Join(deps, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
