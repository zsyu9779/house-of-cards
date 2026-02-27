package cmd

import (
	"github.com/spf13/cobra"
)

// sessionCmd represents the session command
var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "管理 Session（会期）",
	Long:  "会期管理命令：开启、状态、解散",
	Run:   func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	sessionCmd.AddCommand(sessionOpenCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionDissolveCmd)
}

var sessionOpenCmd = &cobra.Command{
	Use:   "open [session-file]",
	Short: "开启新会期",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) {
		println("开启会期:", args[0])
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "查看会期状态",
	Run:   func(cmd *cobra.Command, args []string) {
		println("会期状态:")
	},
}

var sessionDissolveCmd = &cobra.Command{
	Use:   "dissolve [session-id]",
	Short: "解散会期",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) {
		println("解散会期:", args[0])
	},
}
