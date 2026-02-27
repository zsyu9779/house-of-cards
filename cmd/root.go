// Package cmd CLI commands
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version   = "0.1.0"
	GitCommit = "dev"
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "hoc",
	Short: "House of Cards - AI Agent 协作框架",
	Long: `House of Cards 是一个 AI Agent 协作框架，使用政府隐喻构建多 Agent 编排系统。

核心概念：
  Speaker（议长）- 编排决策者
  Minister（部长）- 执行 Agent
  Whip（党鞭）- 系统推进力
  Gazette（公报）- 信息凝练层
  Hansard（议事录）- 审计记录`,
	Version: fmt.Sprintf("%s (%s)", Version, GitCommit),
}

// Execute adds all child commands to the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(ministersCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(billCmd)
	rootCmd.AddCommand(whipCmd)
	rootCmd.AddCommand(cabinetCmd)
	rootCmd.AddCommand(floorCmd)
	rootCmd.AddCommand(gazetteCmd)
	rootCmd.AddCommand(hansardCmd)
}
