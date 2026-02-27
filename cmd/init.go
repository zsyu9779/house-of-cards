package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var initCmd = &cobra.Command{
	Use:   "init [workspace-name]",
	Short: "初始化新的 House of Cards 工作区",
	Long:  "在指定目录下创建新的 House of Cards 工作区结构",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var workspace string
		if len(args) > 0 {
			workspace = args[0]
		} else {
			cwd, _ := os.Getwd()
			workspace = cwd
		}

		fmt.Printf("初始化 House of Cards 工作区: %s\n", workspace)
		
		// 创建 .hoc 目录
		hocDir := filepath.Join(workspace, ".hoc")
		os.MkdirAll(hocDir, 0755)
		
		// 创建 config.toml
		configPath := filepath.Join(hocDir, "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			configContent := `# House of Cards 配置文件
# version: 0.1.0

[house]
name = "default"
version = "0.1.0"

[speaker]
# Speaker 配置
runtime = "claude-code"
model = "opus"

[whip]
# Whip 配置
heartbeat_interval = "10s"
stuck_threshold = "5m"
max_retries = 2

[storage]
# 存储配置
db_path = ".hoc/state.db"

[defaults]
# 默认配置
topology = "parallel"
`
			os.WriteFile(configPath, []byte(configContent), 0644)
			fmt.Printf("创建配置: %s\n", configPath)
		}

		// 创建 speaker 目录
		os.MkdirAll(filepath.Join(hocDir, "speaker"), 0755)
		
		// 创建 projects 目录
		os.MkdirAll(filepath.Join(workspace, "projects"), 0755)
		
		// 创建 hansard 目录
		os.MkdirAll(filepath.Join(workspace, "hansard"), 0755)
		
		// 初始化 SQLite 数据库
		dbPath := filepath.Join(hocDir, "state.db")
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		
		fmt.Printf("\n✅ House of Cards 工作区初始化完成!\n")
		fmt.Printf("   配置: %s\n", configPath)
		fmt.Printf("   数据库: %s\n", dbPath)
	},
}
