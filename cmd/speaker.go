package cmd

import (
	"fmt"
	"os"

	"github.com/house-of-cards/hoc/internal/speaker"
	"github.com/spf13/cobra"
)

// speakerCmd represents the speaker command
var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Speaker（议长）管理",
	Long:  "议长管理命令：传召议长、查看议长备忘录",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	speakerCmd.AddCommand(speakerSummonCmd)
	speakerCmd.AddCommand(speakerContextCmd)

	speakerSummonCmd.Flags().Bool("no-tmux", false, "前台运行（不使用 tmux）")
	speakerContextCmd.Flags().Bool("refresh", false, "重新生成 context.md 后显示")
}

// speakerSummonCmd starts the Speaker AI session.
var speakerSummonCmd = &cobra.Command{
	Use:   "summon",
	Short: "传召议长（启动 Speaker AI session）",
	Long: `传召议长（Speaker），以当前政府状态作为上下文启动 AI 会话。

议长会读取 .hoc/speaker/context.md（当前政府状态备忘录），
并帮助你：
  • 将需求拆解为 Bills 和 Session TOML
  • 分析当前会期进度并给出建议
  • 基于 Gazette 公报做调度决策

示例：
  hoc speaker summon            # 在 tmux 会话 hoc-speaker 中启动
  hoc speaker summon --no-tmux  # 前台交互模式`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		noTmux, _ := cmd.Flags().GetBool("no-tmux")

		// Always refresh context before summoning.
		fmt.Println("📝 更新议长备忘录...")
		content, err := speaker.GenerateContext(db)
		if err != nil {
			return fmt.Errorf("generate context: %w", err)
		}
		if err := speaker.WriteContext(hocDir, content); err != nil {
			return fmt.Errorf("write context: %w", err)
		}

		fmt.Println("🎙  传召议长 (Speaker)...")
		return speaker.Summon(hocDir, !noTmux)
	},
}

// speakerContextCmd shows or refreshes the Speaker context.
var speakerContextCmd = &cobra.Command{
	Use:   "context",
	Short: "查看议长备忘录（context.md）",
	Long: `显示当前议长备忘录内容。
使用 --refresh 先重新生成再显示。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		refresh, _ := cmd.Flags().GetBool("refresh")

		if refresh {
			content, err := speaker.GenerateContext(db)
			if err != nil {
				return fmt.Errorf("generate context: %w", err)
			}
			if err := speaker.WriteContext(hocDir, content); err != nil {
				return fmt.Errorf("write context: %w", err)
			}
			fmt.Println("✓ 议长备忘录已刷新")
			fmt.Println()
		}

		ctxPath := speaker.ContextPath(hocDir)
		data, err := os.ReadFile(ctxPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("备忘录不存在，请先运行：hoc speaker context --refresh")
			}
			return fmt.Errorf("read context: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}
