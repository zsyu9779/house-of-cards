package cmd

import (
	"github.com/spf13/cobra"
)

// whipCmd represents the whip command
var whipCmd = &cobra.Command{
	Use:   "whip",
	Short: "Whip（党鞭）管理",
	Long:  "党鞭管理命令：状态、报告",
	Run:   func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	whipCmd.AddCommand(whipReportCmd)
	whipCmd.AddCommand(whipStartCmd)
	whipCmd.AddCommand(whipStopCmd)
}

var whipReportCmd = &cobra.Command{
	Use:   "report",
	Short: "查看 Whip 状态报告",
	Run:   func(cmd *cobra.Command, args []string) {
		println(" Whip 状态报告:")
	},
}

var whipStartCmd = &cobra.Command{
	Use:   "start",
	Short: "启动 Whip daemon",
	Run:   func(cmd *cobra.Command, args []string) {
		println("启动 Whip daemon")
	},
}

var whipStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "停止 Whip daemon",
	Run:   func(cmd *cobra.Command, args []string) {
		println("停止 Whip daemon")
	},
}
