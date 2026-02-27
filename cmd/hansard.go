package cmd

import (
	"github.com/spf13/cobra"
)

// hansardCmd represents the hansard command
var hansardCmd = &cobra.Command{
	Use:   "hansard",
	Short: "Hansard（议事录）管理",
	Long:  "议事录管理命令：查看",
	Run:   func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			println("查看部长履历:", args[0])
		} else {
			cmd.Help()
		}
	},
}

func init() {
	hansardCmd.AddCommand(hansardListCmd)
}

var hansardListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有议事录",
	Run:   func(cmd *cobra.Command, args []string) {
		println("议事录列表:")
	},
}
