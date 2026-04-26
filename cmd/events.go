package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "查看事件日志 (Event Ledger)",
	Long:  "查看系统事件日志，支持按主题、议案、部长筛选",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	eventsCmd.AddCommand(eventsListCmd)
	eventsCmd.AddCommand(eventsTimelineCmd)

	eventsListCmd.Flags().String("topic", "", "按事件主题筛选")
	eventsListCmd.Flags().String("bill", "", "按议案 ID 筛选")
	eventsListCmd.Flags().String("minister", "", "按部长 ID 筛选")
	eventsListCmd.Flags().String("since", "", "时间范围（如 1h, 30m, 24h）")
}

var eventsListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出事件日志",
	Long:  "列出事件日志，支持按主题、议案、部长、时间范围筛选",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		topic, _ := cmd.Flags().GetString("topic")
		billID, _ := cmd.Flags().GetString("bill")
		ministerID, _ := cmd.Flags().GetString("minister")
		sinceStr, _ := cmd.Flags().GetString("since")

		var since time.Duration
		if sinceStr != "" {
			var err error
			since, err = time.ParseDuration(sinceStr)
			if err != nil {
				return fmt.Errorf("无效的时间范围 %q: %w", sinceStr, err)
			}
		}

		events, err := db.ListEvents(topic, billID, ministerID, since)
		if err != nil {
			return fmt.Errorf("list events: %w", err)
		}

		if len(events) == 0 {
			fmt.Println("暂无事件记录。")
			return nil
		}

		fmt.Printf("📋 事件日志（共 %d 条）:\n", len(events))
		fmt.Println("─────────────────────────────────────────────────")
		for _, e := range events {
			fmt.Printf("[%s] %s  ← %s\n",
				e.Timestamp.Format("2006-01-02 15:04:05"),
				e.Topic,
				e.Source,
			)
			if e.BillID.String != "" {
				fmt.Printf("   议案: %s", e.BillID.String)
			}
			if e.MinisterID.String != "" {
				fmt.Printf("   部长: %s", e.MinisterID.String)
			}
			if e.SessionID.String != "" {
				fmt.Printf("   会期: %s", e.SessionID.String)
			}
			if e.BillID.String != "" || e.MinisterID.String != "" || e.SessionID.String != "" {
				fmt.Println()
			}
			if e.Payload != "" && e.Payload != "{}" {
				fmt.Printf("   载荷: %s\n", truncate(e.Payload, 120))
			}
		}
		return nil
	},
}

var eventsTimelineCmd = &cobra.Command{
	Use:   "timeline <session-id>",
	Short: "显示会期事件时间线",
	Long:  "按时间顺序显示指定会期的所有事件",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		sessionID := args[0]

		events, err := db.ListEventsBySession(sessionID)
		if err != nil {
			return fmt.Errorf("list events by session: %w", err)
		}

		if len(events) == 0 {
			fmt.Printf("会期 [%s] 暂无事件记录。\n", sessionID)
			return nil
		}

		fmt.Printf("📅 会期 [%s] 事件时间线（共 %d 条）:\n", sessionID, len(events))
		fmt.Println("─────────────────────────────────────────────────")
		for i, e := range events {
			connector := "├─"
			if i == len(events)-1 {
				connector = "└─"
			}
			fmt.Printf("%s [%s] %s  ← %s\n",
				connector,
				e.Timestamp.Format("15:04:05"),
				e.Topic,
				e.Source,
			)
			if e.BillID.String != "" || e.MinisterID.String != "" {
				prefix := "│  "
				if i == len(events)-1 {
					prefix = "   "
				}
				details := ""
				if e.BillID.String != "" {
					details += fmt.Sprintf("议案:%s ", e.BillID.String)
				}
				if e.MinisterID.String != "" {
					details += fmt.Sprintf("部长:%s", e.MinisterID.String)
				}
				fmt.Printf("%s %s\n", prefix, details)
			}
		}
		return nil
	},
}
