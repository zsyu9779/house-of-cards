package cmd

import (
	"github.com/spf13/cobra"
)

// billCmd represents the bill command
var billCmd = &cobra.Command{
	Use:   "bill",
	Short: "管理 Bill（议案）",
	Long:  "议案管理命令：创建、查看、状态",
	Run:   func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	billCmd.AddCommand(billListCmd)
	billCmd.AddCommand(billShowCmd)
}

var billListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有 Bill",
	Run:   func(cmd *cobra.Command, args []string) {
		println("议案列表:")
	},
}

var billShowCmd = &cobra.Command{
	Use:   "show [bill-id]",
	Short: "查看 Bill 详情",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) {
		println("Bill 详情:", args[0])
	},
}
