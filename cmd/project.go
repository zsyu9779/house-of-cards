package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "管理项目（Project）",
	Long:  "项目管理命令：添加仓库、列出项目",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var projectAddCmd = &cobra.Command{
	Use:   "add [name] [repo-url]",
	Short: "添加项目仓库到工作区",
	Long:  "Clone 仓库到 projects/<name>/main/，并创建 chambers/ 和 gazettes/ 目录",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		repoURL := args[1]

		hocHome := config.GetHOCHome()

		// Verify workspace is initialized
		configPath := filepath.Join(hocHome, ".hoc", "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return fmt.Errorf("工作区未初始化，请先运行 hoc init")
		}

		projectDir := filepath.Join(hocHome, "projects", name)

		// Check if project already exists
		mainDir := filepath.Join(projectDir, "main")
		if _, err := os.Stat(mainDir); err == nil {
			return fmt.Errorf("项目 %s 已存在: %s", name, mainDir)
		}

		// Create project directories
		dirs := []string{
			mainDir,
			filepath.Join(projectDir, "chambers"),
			filepath.Join(projectDir, "gazettes"),
		}
		for _, d := range dirs {
			if err := os.MkdirAll(d, 0755); err != nil {
				return fmt.Errorf("create directory %s: %w", d, err)
			}
		}

		// Clone repository
		fmt.Printf("克隆仓库 %s → %s\n", repoURL, mainDir)
		gitCmd := exec.Command("git", "clone", repoURL, mainDir)
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
		if err := gitCmd.Run(); err != nil {
			// Clean up on failure
			os.RemoveAll(projectDir)
			return fmt.Errorf("git clone 失败: %w", err)
		}

		fmt.Printf("\n✅ 项目 %s 添加成功!\n", name)
		fmt.Printf("   仓库: %s\n", mainDir)
		fmt.Printf("   Chambers: %s\n", filepath.Join(projectDir, "chambers"))
		fmt.Printf("   Gazettes: %s\n", filepath.Join(projectDir, "gazettes"))
		return nil
	},
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有项目",
	RunE: func(cmd *cobra.Command, args []string) error {
		hocHome := config.GetHOCHome()
		projectsDir := filepath.Join(hocHome, "projects")

		entries, err := os.ReadDir(projectsDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("暂无项目。请先运行 hoc init 和 hoc project add。")
				return nil
			}
			return fmt.Errorf("read projects dir: %w", err)
		}

		fmt.Println("📋 项目列表:")
		fmt.Println("─────────────────")
		if len(entries) == 0 {
			fmt.Println("  (暂无项目)")
			return nil
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			mainDir := filepath.Join(projectsDir, e.Name(), "main")
			status := "✓"
			if _, err := os.Stat(mainDir); os.IsNotExist(err) {
				status = "?"
			}
			fmt.Printf("  %s %s\n", status, e.Name())
			fmt.Printf("      路径: %s\n", filepath.Join(projectsDir, e.Name()))
		}
		return nil
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	projectCmd.AddCommand(projectAddCmd)
	projectCmd.AddCommand(projectListCmd)
}
