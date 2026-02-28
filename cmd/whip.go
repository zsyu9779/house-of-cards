package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/house-of-cards/hoc/internal/whip"
	"github.com/spf13/cobra"
)

// whipCmd represents the whip command
var whipCmd = &cobra.Command{
	Use:   "whip",
	Short: "Whip（党鞭）管理",
	Long:  "党鞭管理命令：启动守护进程、查看报告、停止",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	whipCmd.AddCommand(whipStartCmd)
	whipCmd.AddCommand(whipStopCmd)
	whipCmd.AddCommand(whipReportCmd)
}

// ─── whip start ──────────────────────────────────────────────────────────────

var whipStartCmd = &cobra.Command{
	Use:   "start",
	Short: "启动 Whip daemon（前台运行，Ctrl+C 停止）",
	Long: `启动党鞭守护进程，在前台持续运行。

每 10 秒执行：
  • 三线鞭令：检查所有 working 部长的心跳与进程存活性
  • 议程推进：DAG 引擎，自动派发就绪议案给空闲部长
  • 公报投递：路由未读公报

建议在单独终端或 tmux 会话中运行：
  tmux new-session -d -s hoc-whip "hoc whip start"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		// Write PID file.
		pidFile := whipPIDFile()
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write PID file: %v\n", err)
		}
		defer os.Remove(pidFile)

		fmt.Printf("🎯 党鞭启动 (PID: %d)  PID 文件: %s\n", os.Getpid(), pidFile)
		fmt.Printf("   按 Ctrl+C 停止，或运行 `hoc whip stop` 从另一终端停止。\n\n")

		// Handle SIGINT / SIGTERM for graceful shutdown.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Println("\n收到停止信号，党鞭退场...")
			cancel()
		}()

		w := whip.New(db, hocDir, os.Stdout)
		w.Run(ctx)
		return nil
	},
}

// ─── whip stop ───────────────────────────────────────────────────────────────

var whipStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "停止 Whip daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidFile := whipPIDFile()
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return fmt.Errorf("找不到 PID 文件 (%s)，党鞭可能未运行", pidFile)
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return fmt.Errorf("无效 PID 文件内容: %w", err)
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("找不到进程 %d: %w", pid, err)
		}

		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("发送停止信号失败: %w", err)
		}

		fmt.Printf("✓ 已向党鞭进程 (PID: %d) 发送停止信号\n", pid)
		return nil
	},
}

// ─── whip report ─────────────────────────────────────────────────────────────

var whipReportCmd = &cobra.Command{
	Use:   "report",
	Short: "查看 Whip 状态报告",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer db.Close()

		// Show daemon status.
		pidFile := whipPIDFile()
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid := strings.TrimSpace(string(data))
			fmt.Printf("🎯 党鞭状态: 运行中 (PID: %s)\n\n", pid)
		} else {
			fmt.Printf("🔴 党鞭状态: 未运行\n\n")
		}

		report, err := whip.Report(db)
		if err != nil {
			return fmt.Errorf("generate report: %w", err)
		}
		fmt.Print(report)
		return nil
	},
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// whipPIDFile returns the path to the Whip PID file.
func whipPIDFile() string {
	if hocDir == "" {
		hocDir = filepath.Join(os.Getenv("HOME"), "house-of-cards")
	}
	return filepath.Join(hocDir, ".hoc", "whip.pid")
}
