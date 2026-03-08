package cmd

import (
	"fmt"
	"log/slog"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/spf13/cobra"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "配置管理",
	Long:  "查看和重新加载配置",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var configReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "重新加载配置文件",
	Long: `手动重新加载 ~/.hoc/config.toml 配置。

支持的参数：
  - whip.heartbeat_interval
  - whip.stuck_threshold
  - whip.max_ministers`,
	RunE: func(cmd *cobra.Command, args []string) error {
		hocDir := config.GetHOCHome()

		cfg, err := config.LoadConfig(hocDir)
		if err != nil {
			return fmt.Errorf("加载配置失败: %w", err)
		}

		// Get hot-reloadable params.
		params := &config.HotReloadableParams{
			WhipInterval:   cfg.Whip.HeartbeatInterval,
			StuckThreshold: cfg.Whip.StuckThreshold,
			MaxMinisters:   cfg.Whip.MaxMinisters,
		}

		fmt.Println("✓ 配置已重新加载")
		fmt.Printf("   Whip 心跳间隔: %s\n", params.WhipInterval)
		fmt.Printf("   Stuck 阈值: %s\n", params.StuckThreshold)
		fmt.Printf("   最大部长数: %d\n", params.MaxMinisters)

		slog.Info("配置重新加载完成", "params", params)
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "显示当前配置",
	Long:  "显示当前加载的配置内容",
	RunE: func(cmd *cobra.Command, args []string) error {
		hocDir := config.GetHOCHome()

		cfg, err := config.LoadConfig(hocDir)
		if err != nil {
			return fmt.Errorf("加载配置失败: %w", err)
		}

		fmt.Printf("=== House of Cards 配置 ===\n\n")
		fmt.Printf("House:\n")
		fmt.Printf("  Name: %s\n", cfg.House.Name)
		fmt.Printf("  Version: %s\n\n", cfg.House.Version)

		fmt.Printf("Speaker:\n")
		fmt.Printf("  Runtime: %s\n", cfg.Speaker.Runtime)
		fmt.Printf("  Model: %s\n\n", cfg.Speaker.Model)

		fmt.Printf("Whip:\n")
		fmt.Printf("  Heartbeat Interval: %s\n", cfg.Whip.HeartbeatInterval)
		fmt.Printf("  Stuck Threshold: %s\n", cfg.Whip.StuckThreshold)
		fmt.Printf("  Max Retries: %d\n", cfg.Whip.MaxRetries)
		fmt.Printf("  Max Ministers: %d\n\n", cfg.Whip.MaxMinisters)

		fmt.Printf("Storage:\n")
		fmt.Printf("  DB Path: %s\n\n", cfg.Storage.DBPath)

		fmt.Printf("Defaults:\n")
		fmt.Printf("  Topology: %s\n", cfg.Defaults.Topology)

		return nil
	},
}

func init() {
	configCmd.AddCommand(configReloadCmd)
	configCmd.AddCommand(configShowCmd)

	rootCmd.AddCommand(configCmd)
}
