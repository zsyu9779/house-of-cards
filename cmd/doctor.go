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
		fixMode, _ := cmd.Flags().GetBool("fix")

		// Load config for threshold values.
		cfg, _ := config.LoadConfig(hocDir)
		if cfg == nil {
			cfg = config.DefaultConfig(hocDir)
		}

		fmt.Println("🏥 House of Cards — 健康检查")
		fmt.Println(repeat("─", 50))

		allOK := true
		fixCount := 0

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
				sizeMB := float64(stat.Size()) / (1024 * 1024)
				sizeStr = fmt.Sprintf(" (%.1f KB)", float64(stat.Size())/1024)
				// DB size threshold check.
				if int(sizeMB) >= cfg.Doctor.DBSizeWarnMB {
					sizeStr = fmt.Sprintf(" (%.1f MB ⚠ 超过 %d MB 阈值)", sizeMB, cfg.Doctor.DBSizeWarnMB)
					allOK = false
				}
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

				if fixMode {
					for _, m := range stuck {
						if err := d.UpdateMinisterStatus(m.ID, "idle"); err != nil {
							fmt.Printf("     ❌ 修复失败 [%s]: %v\n", m.ID, err)
						} else {
							fmt.Printf("     🔧 已修复: %s → idle\n", m.ID)
							fixCount++
						}
					}
				}
			} else {
				fmt.Println("✅  无")
			}
		}

		// ── 4. Unread gazettes (config-driven threshold) ─────────
		if d != nil {
			fmt.Print("  未读公报积压     ... ")
			gazettes, err := d.ListUnreadGazettes()
			if err != nil {
				fmt.Printf("⚠   查询失败: %v\n", err)
			} else if len(gazettes) > cfg.Doctor.GazetteBacklogWarn {
				fmt.Printf("⚠   积压 %d 份公报（阈值: %d）\n", len(gazettes), cfg.Doctor.GazetteBacklogWarn)
				allOK = false
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

		// ── 6. Orphan worktrees ──────────────────────────────────
		if d != nil {
			fmt.Print("  孤儿 worktree    ... ")
			withWT, err := d.ListMinistersWithWorktree()
			if err != nil {
				fmt.Printf("⚠   查询失败: %v\n", err)
			} else {
				orphans := 0
				for _, m := range withWT {
					wtPath := m.Worktree.String
					if wtPath == "" {
						continue
					}
					// Orphan: minister is offline/idle but worktree dir exists or doesn't exist.
					isOrphan := false
					if m.Status == "offline" || m.Status == "idle" {
						if _, err := os.Stat(wtPath); err == nil {
							// Dir exists but minister not working → orphan.
							isOrphan = true
						} else {
							// Dir doesn't exist but DB still references it → stale ref.
							isOrphan = true
						}
					}
					if isOrphan {
						orphans++
						if fixMode {
							// Try to remove the worktree if it exists.
							if _, err := os.Stat(wtPath); err == nil {
								rmCmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
								if rmErr := rmCmd.Run(); rmErr != nil {
									fmt.Printf("\n     ⚠ worktree 移除失败 [%s]: %v", wtPath, rmErr)
								} else {
									fmt.Printf("\n     🔧 已移除 worktree: %s", wtPath)
									fixCount++
								}
							}
							// Clear DB field.
							if err := d.ClearMinisterWorktree(m.ID); err != nil {
								fmt.Printf("\n     ⚠ 清除 DB 字段失败 [%s]: %v", m.ID, err)
							} else {
								fmt.Printf("\n     🔧 已清除 worktree 引用: %s", m.ID)
								fixCount++
							}
						}
					}
				}
				if orphans > 0 {
					fmt.Printf("⚠   %d 个孤儿 worktree\n", orphans)
					allOK = false
				} else {
					fmt.Println("✅  无")
				}
			}
		}

		// ── 7. Config file validation ────────────────────────────
		fmt.Print("  配置文件校验     ... ")
		configPath := filepath.Join(hocDir, ".hoc", "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Println("ℹ   使用默认配置（无 config.toml）")
		} else {
			_, cfgErr := config.LoadConfig(hocDir)
			if cfgErr != nil {
				fmt.Printf("⚠   解析失败: %v\n", cfgErr)
				allOK = false
			} else {
				fmt.Println("✅  有效")
			}
		}

		// ── Summary ──────────────────────────────────────────────
		fmt.Println(repeat("─", 50))
		if fixMode && fixCount > 0 {
			fmt.Printf("  🔧 已修复 %d 个问题\n", fixCount)
		}
		if allOK {
			fmt.Println("  ✅  一切正常。议会就绪。")
		} else {
			fmt.Println("  ⚠   存在问题，请检查上方输出。")
			if !fixMode {
				fmt.Println("  💡 使用 `hoc doctor --fix` 尝试自动修复。")
			}
		}

		return nil
	},
}

//nolint:gochecknoinits // Cobra convention: register flags in init().
func init() {
	doctorCmd.Flags().Bool("fix", false, "自动修复可修复的问题")
}
