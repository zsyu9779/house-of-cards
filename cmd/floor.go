package cmd

import (
	"github.com/spf13/cobra"
)

// floorCmd represents the floor command
var floorCmd = &cobra.Command{
	Use:   "floor",
	Short: "议会大厅 - 全局状态 TUI",
	Long:  "启动议会大厅实时监控界面",
	Run:   func(cmd *cobra.Command, args []string) {
		println("启动议会大厅 TUI...")
	},
}
