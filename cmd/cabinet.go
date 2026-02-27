package cmd

import (
	"github.com/spf13/cobra"
)

// cabinetCmd represents the cabinet command
var cabinetCmd = &cobra.Command{
	Use:   "cabinet",
	Short: "Cabinet（内阁）管理",
	Long:  "内阁管理命令：查看、改组",
	Run:   func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	cabinetCmd.AddCommand(cabinetListCmd)
	cabinetCmd.AddCommand(cabinetReshuffleCmd)
}

var cabinetListCmd = &cobra.Command{
	Use:   "list",
	Short: "查看内阁花名册",
	Run:   func(cmd *cobra.Command, args []string) {
		println("内阁花名册:")
	},
}

var cabinetReshuffleCmd = &cobra.Command{
	Use:   "reshuffle",
	Short: "内阁改组",
	Run:   func(cmd *cobra.Command, args []string) {
		println("执行内阁改组")
	},
}
