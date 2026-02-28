package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// hansardCmd represents the hansard command
var hansardCmd = &cobra.Command{
	Use:   "hansard [minister-id]",
	Short: "Hansard（议事录）— 查看部长工作履历",
	Long:  "查看议事录。不带参数显示全部记录；带部长 ID 显示该部长的工作历史。",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		if len(args) > 0 {
			return showMinisterHansard(args[0])
		}
		return listAllHansard()
	},
}

func init() {
	hansardCmd.AddCommand(hansardListCmd)
}

var hansardListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有议事录",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()
		return listAllHansard()
	},
}

func listAllHansard() error {
	entries, err := db.ListHansard()
	if err != nil {
		return fmt.Errorf("list hansard: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("暂无议事录。议案通过后会自动记录。")
		return nil
	}

	fmt.Printf("📜 议事录（共 %d 条）:\n", len(entries))
	fmt.Println("─────────────────────────────────────────────────")
	for _, h := range entries {
		outcomeIcon := "⚪"
		switch h.Outcome.String {
		case "enacted":
			outcomeIcon = "✅"
		case "failed":
			outcomeIcon = "❌"
		case "partial":
			outcomeIcon = "🟡"
		}
		qualStr := ""
		if h.Quality > 0 {
			qualStr = fmt.Sprintf("  质量: %.2f", h.Quality)
		}
		durStr := ""
		if h.DurationS > 0 {
			durStr = fmt.Sprintf("  耗时: %s", formatDuration(h.DurationS))
		}
		fmt.Printf("%s [%s] %s → %s%s%s\n",
			outcomeIcon,
			h.Outcome.String,
			h.MinisterID,
			h.BillID,
			qualStr,
			durStr,
		)
		fmt.Printf("   %s\n", h.CreatedAt.Format("2006-01-02 15:04"))
		if h.Notes.String != "" {
			fmt.Printf("   备注: %s\n", truncate(h.Notes.String, 80))
		}
	}
	return nil
}

func showMinisterHansard(ministerID string) error {
	// Check minister exists.
	m, err := db.GetMinister(ministerID)
	if err != nil {
		return fmt.Errorf("minister not found: %s", ministerID)
	}

	entries, err := db.ListHansardByMinister(ministerID)
	if err != nil {
		return fmt.Errorf("list hansard: %w", err)
	}

	enacted, total, _ := db.HansardSuccessRate(ministerID)

	fmt.Printf("📜 %s 的议事录（工作履历）\n", m.Title)
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Printf("ID:      %s\n", m.ID)
	fmt.Printf("技能:    %s\n", orDash(m.Skills))
	fmt.Printf("状态:    %s\n", m.Status)
	fmt.Printf("Hansard: %d 条记录\n", len(entries))
	if total > 0 {
		rate := float64(enacted) / float64(total) * 100
		fmt.Printf("成功率:  %d/%d (%.0f%%)\n", enacted, total, rate)
	}
	fmt.Println()

	if len(entries) == 0 {
		fmt.Println("暂无议事录。")
		return nil
	}

	for _, h := range entries {
		outcomeIcon := "⚪"
		switch h.Outcome.String {
		case "enacted":
			outcomeIcon = "✅"
		case "failed":
			outcomeIcon = "❌"
		case "partial":
			outcomeIcon = "🟡"
		}
		fmt.Printf("%s 议案 [%s]", outcomeIcon, h.BillID)
		if h.Quality > 0 {
			fmt.Printf("  质量: %.2f", h.Quality)
		}
		if h.DurationS > 0 {
			fmt.Printf("  耗时: %s", formatDuration(h.DurationS))
		}
		fmt.Printf("  %s\n", h.CreatedAt.Format("2006-01-02 15:04"))
		if h.Notes.String != "" {
			fmt.Printf("   备注: %s\n", h.Notes.String)
		}
	}
	return nil
}

func formatDuration(seconds int) string {
	d := time.Duration(seconds) * time.Second
	if d < time.Minute {
		return fmt.Sprintf("%ds", seconds)
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
