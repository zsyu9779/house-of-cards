package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// gazetteCmd represents the gazette command
var gazetteCmd = &cobra.Command{
	Use:   "gazette",
	Short: "Gazette（公报）管理",
	Long:  "公报管理命令：查看公报流转",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	gazetteCmd.AddCommand(gazetteListCmd)
	gazetteCmd.AddCommand(gazetteShowCmd)

	gazetteListCmd.Flags().String("minister", "", "按部长 ID 过滤")
	gazetteListCmd.Flags().String("bill", "", "按议案 ID 过滤")
}

var gazetteListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有公报",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		ministerFilter, _ := cmd.Flags().GetString("minister")
		billFilter, _ := cmd.Flags().GetString("bill")

		if billFilter != "" {
			gs, err := db.ListGazettesForBill(billFilter)
			if err != nil {
				return fmt.Errorf("list gazettes: %w", err)
			}
			if len(gs) == 0 {
				fmt.Printf("暂无议案 [%s] 的公报\n", billFilter)
				return nil
			}
			fmt.Printf("📰 议案 [%s] 公报 (%d 份):\n", billFilter, len(gs))
			fmt.Println("─────────────────────────────────────────")
			for _, g := range gs {
				printGazette(g.ID, g.Type.String, g.FromMinister.String, g.ToMinister.String, g.BillID.String, g.Summary, g.CreatedAt)
			}
			return nil
		}

		if ministerFilter != "" {
			gs, err := db.ListGazettesForMinister(ministerFilter)
			if err != nil {
				return fmt.Errorf("list gazettes: %w", err)
			}
			if len(gs) == 0 {
				fmt.Printf("暂无部长 [%s] 的公报\n", ministerFilter)
				return nil
			}
			fmt.Printf("📰 部长 [%s] 公报 (%d 份):\n", ministerFilter, len(gs))
			fmt.Println("─────────────────────────────────────────")
			for _, g := range gs {
				printGazette(g.ID, g.Type.String, g.FromMinister.String, g.ToMinister.String, g.BillID.String, g.Summary, g.CreatedAt)
			}
			return nil
		}

		// All gazettes.
		gs, err := db.ListGazettes()
		if err != nil {
			return fmt.Errorf("list gazettes: %w", err)
		}
		if len(gs) == 0 {
			fmt.Println("暂无公报。议案完成后公报会自动记录。")
			return nil
		}
		fmt.Printf("📰 公报列表 (%d 份):\n", len(gs))
		fmt.Println("─────────────────────────────────────────")
		for _, g := range gs {
			printGazette(g.ID, g.Type.String, g.FromMinister.String, g.ToMinister.String, g.BillID.String, g.Summary, g.CreatedAt)
		}
		return nil
	},
}

var gazetteShowCmd = &cobra.Command{
	Use:   "show [gazette-id]",
	Short: "查看公报详情",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		// Fetch all and find by ID prefix.
		gs, err := db.ListGazettes()
		if err != nil {
			return fmt.Errorf("list gazettes: %w", err)
		}

		target := args[0]
		for _, g := range gs {
			if g.ID == target || (len(g.ID) > len(target) && g.ID[:len(target)] == target) {
				fmt.Println("📰 公报详情")
				fmt.Println("────────────────────────────────")
				fmt.Printf("ID:      %s\n", g.ID)
				fmt.Printf("类型:    %s\n", orDash(g.Type.String))
				fmt.Printf("发自:    %s\n", orDash(g.FromMinister.String))
				fmt.Printf("致:      %s\n", orDash(g.ToMinister.String))
				fmt.Printf("议案:    %s\n", orDash(g.BillID.String))
				fmt.Printf("时间:    %s\n", g.CreatedAt.Format(time.RFC3339))
				if g.ReadAt.Valid {
					fmt.Printf("已读:    %s\n", g.ReadAt.Time.Format(time.RFC3339))
				}
				fmt.Printf("\n内容:\n%s\n", g.Summary)
				if g.Artifacts.String != "" && g.Artifacts.String != "null" {
					fmt.Printf("\n产物:\n%s\n", g.Artifacts.String)
				}
				return nil
			}
		}
		return fmt.Errorf("gazette not found: %s", target)
	},
}

func printGazette(id, typ, from, to, billID, summary string, createdAt time.Time) {
	typeIcon := "📄"
	switch typ {
	case "completion":
		typeIcon = "✅"
	case "handoff":
		typeIcon = "🔄"
	case "help":
		typeIcon = "🆘"
	case "review":
		typeIcon = "🔍"
	case "conflict":
		typeIcon = "⚠️"
	}
	fromStr := orDash(from)
	toStr := orDash(to)
	billStr := orDash(billID)
	fmt.Printf("%s [%s] %s → %s  (议案: %s)\n", typeIcon, typ, fromStr, toStr, billStr)
	fmt.Printf("   %s\n", truncate(summary, 80))
	fmt.Printf("   ID: %s  |  %s\n", id, createdAt.Format("2006-01-02 15:04"))
	fmt.Println()
}
