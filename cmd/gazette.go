package cmd

import (
	"github.com/spf13/cobra"
)

// gazetteCmd represents the gazette command
var gazetteCmd = &cobra.Command{
	Use:   "gazette",
	Short: "Gazette（公报）管理",
	Long:  "公报管理命令：查看、发送",
	Run:   func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	gazetteCmd.AddCommand(gazetteListCmd)
	gazetteCmd.AddCommand(gazetteShowCmd)
}

var gazetteListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有公报",
	Run:   func(cmd *cobra.Command, args []string) {
		println("公报列表:")
	},
}

var gazetteShowCmd = &cobra.Command{
	Use:   "show [gazette-id]",
	Short: "查看公报详情",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) {
		println("公报详情:", args[0])
	},
}
