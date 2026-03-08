package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "健康检查 — 验证 hoc 运行环境",
	Long:  "检查数据库连接、外部依赖（git/tmux/claude）、卡住的部长和公报积压。",
	RunE: func(cmd *cobra.Command, args []string) error {
		hocDir := config.GetHOCHome()

		fmt.Println("🏥 House of Cards — 健康检查")
		fmt.Println(repeat("─", 50))

		allOK := true

		// ── 1. DB connectivity ───────────────────────────────────
		fmt.Print("  数据库 (SQLite)   ... ")
		dbPath := filepath.Join(hocDir, ".hoc", "state.db")
		d, err := store.NewDB(hocDir)
		if err != nil {
			fmt.Printf("❌  %v\n", err)
			allOK = false
		} else {
			stat, statErr := os.Stat(dbPath)
			sizeStr := ""
			if statErr == nil {
				sizeStr = fmt.Sprintf(" (%.1f KB)", float64(stat.Size())/1024)
			}
			fmt.Printf("✅  %s%s\n", dbPath, sizeStr)
			defer d.Close()
		}

		// ── 2. External tools ────────────────────────────────────
		tools := []struct {
			name string
			cmd  string
			arg  string
		}{
			{"git", "git", "--version"},
			{"tmux", "tmux", "-V"},
			{"claude CLI", "claude", "--version"},
		}
		for _, t := range tools {
			fmt.Printf("  %-20s... ", t.name)
			out, err := exec.Command(t.cmd, t.arg).Output()
			if err != nil {
				fmt.Printf("⚠   未找到 (%s not in PATH)\n", t.cmd)
				if t.cmd != "tmux" && t.cmd != "claude" {
					allOK = false
				}
			} else {
				ver := string(out)
				if len(ver) > 40 {
					ver = ver[:40]
				}
				// trim newlines
				for len(ver) > 0 && (ver[len(ver)-1] == '\n' || ver[len(ver)-1] == '\r') {
					ver = ver[:len(ver)-1]
				}
				fmt.Printf("✅  %s\n", ver)
			}
		}

		// ── 3. Stuck ministers ───────────────────────────────────
		if d != nil {
			fmt.Print("  卡住的部长       ... ")
			stuck, err := d.ListMinistersWithStatus("stuck")
			if err != nil {
				fmt.Printf("⚠   查询失败: %v\n", err)
			} else if len(stuck) > 0 {
				fmt.Printf("⚠   %d 位部长卡住: ", len(stuck))
				for i, m := range stuck {
					if i > 0 {
						fmt.Print(", ")
					}
					fmt.Print(m.ID)
				}
				fmt.Println()
				allOK = false
			} else {
				fmt.Println("✅  无")
			}
		}

		// ── 4. Unread gazettes ───────────────────────────────────
		if d != nil {
			fmt.Print("  未读公报积压     ... ")
			gazettes, err := d.ListUnreadGazettes()
			if err != nil {
				fmt.Printf("⚠   查询失败: %v\n", err)
			} else if len(gazettes) > 10 {
				fmt.Printf("⚠   积压 %d 份公报\n", len(gazettes))
			} else {
				fmt.Printf("✅  %d 份未读\n", len(gazettes))
			}
		}

		// ── 5. Active sessions ───────────────────────────────────
		if d != nil {
			fmt.Print("  活跃会期         ... ")
			sessions, err := d.ListActiveSessions()
			if err != nil {
				fmt.Printf("⚠   查询失败: %v\n", err)
			} else {
				fmt.Printf("✅  %d 个活跃\n", len(sessions))
			}
		}

		// ── Summary ──────────────────────────────────────────────
		fmt.Println(repeat("─", 50))
		if allOK {
			fmt.Println("  ✅  一切正常。议会就绪。")
		} else {
			fmt.Println("  ⚠   存在问题，请检查上方输出。")
		}

		return nil
	},
}
