package cmd

import (
	"github.com/spf13/cobra"
)

// ministersCmd represents the ministers command
var ministersCmd = &cobra.Command{
	Use:   "minister",
	Short: "管理 Minister（部长）",
	Long:  "Minister 管理命令：任命、传召、休会、查看",
	Run:   func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	ministersCmd.AddCommand(ministerAppointCmd)
	ministersCmd.AddCommand(ministerSummonCmd)
	ministersCmd.AddCommand(ministerDismissCmd)
	ministersCmd.AddCommand(ministersListCmd)
}

var ministerAppointCmd = &cobra.Command{
	Use:   "appoint [name]",
	Short: "任命新的 Minister",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) {
		runtime, _ := cmd.Flags().GetString("runtime")
		portfolio, _ := cmd.Flags().GetStringSlice("portfolio")
		title, _ := cmd.Flags().GetString("title")
		println("任命部长:", args[0], "runtime:", runtime, "portfolio:", portfolio, "title:", title)
	},
}

var ministerSummonCmd = &cobra.Command{
	Use:   "summon [name]",
	Short: "传召 Minister（启动 session）",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) {
		println("传召部长:", args[0])
	},
}

var ministerDismissCmd = &cobra.Command{
	Use:   "dismiss [name]",
	Short: "休会 Minister（停止 session）",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) {
		println("休会部长:", args[0])
	},
}

var ministersListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有 Minister",
	Run:   func(cmd *cobra.Command, args []string) {
		println("内阁花名册:")
	},
}

func init() {
	ministerAppointCmd.Flags().String("runtime", "claude-code", "Runtime: claude-code, codex, cursor")
	ministerAppointCmd.Flags().StringSlice("portfolio", []string{}, "技能领域")
	ministerAppointCmd.Flags().String("title", "", "部长头衔")
}
