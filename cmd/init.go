package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [workspace-path]",
	Short: "初始化新的 House of Cards 工作区",
	Long:  "在指定目录下创建新的 House of Cards 工作区结构",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var workspace string
		if len(args) > 0 {
			workspace = args[0]
		} else {
			cwd, _ := os.Getwd()
			workspace = cwd
		}

		// Make absolute path
		if !filepath.IsAbs(workspace) {
			abs, err := filepath.Abs(workspace)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}
			workspace = abs
		}

		fmt.Printf("初始化 House of Cards 工作区: %s\n", workspace)

		// Create directory structure
		dirs := []string{
			filepath.Join(workspace, ".hoc"),
			filepath.Join(workspace, ".hoc", "speaker"),
			filepath.Join(workspace, "projects"),
			filepath.Join(workspace, "hansard"),
		}
		for _, d := range dirs {
			if err := os.MkdirAll(d, 0755); err != nil {
				return fmt.Errorf("create directory %s: %w", d, err)
			}
		}

		// Write config.toml
		configPath := filepath.Join(workspace, ".hoc", "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			cfg := config.DefaultConfig(workspace)
			if err := config.SaveConfig(configPath, cfg); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			fmt.Printf("  创建配置: %s\n", configPath)
		} else {
			fmt.Printf("  配置已存在: %s\n", configPath)
		}

		// Initialize SQLite database
		database, err := store.NewDB(workspace)
		if err != nil {
			return fmt.Errorf("init database: %w", err)
		}
		database.Close()
		fmt.Printf("  创建数据库: %s\n", filepath.Join(workspace, ".hoc", "state.db"))

		fmt.Printf("\n✅ House of Cards 工作区初始化完成!\n")
		return nil
	},
}
