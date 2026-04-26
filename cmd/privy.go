package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/house-of-cards/hoc/internal/privy"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
)

var privyCmd = &cobra.Command{
	Use:   "privy",
	Short: "Privy Council（枢密院）— 合并仲裁",
	Long:  "枢密院命令：将并行 Bills 的分支自动合并，处理冲突",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

//nolint:gochecknoinits // Cobra convention: register subcommands in init().
func init() {
	privyCmd.AddCommand(privyMergeCmd)
	privyCmd.AddCommand(privyAnalyzeCmd)

	privyMergeCmd.Flags().String("project", "", "项目名称（必填）")
	privyMergeCmd.Flags().String("base", "", "基础分支（默认 main/master）")
	privyMergeCmd.Flags().Bool("dry-run", false, "预演：只显示会合并哪些分支，不实际执行")
	_ = privyMergeCmd.MarkFlagRequired("project")

	privyAnalyzeCmd.Flags().String("project", "", "项目名称（必填）")
	privyAnalyzeCmd.Flags().String("base", "", "基础分支（默认 main/master）")
	_ = privyAnalyzeCmd.MarkFlagRequired("project")
}

var privyMergeCmd = &cobra.Command{
	Use:   "merge [session-id]",
	Short: "将 Session 中所有 enacted Bills 的分支合并（Privy Council）",
	Long: `枢密院合并：将会期中所有已通过（enacted）议案的 git 分支合并到一个新的集成分支。

成功后，议案状态升级为 royal_assent。
冲突时，创建 Conflict Gazette 并指出冲突文件。

示例：
  hoc privy merge session-1a2b3c4d --project myapp
  hoc privy merge session-1a2b3c4d --project myapp --base develop --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initDB(); err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		sessionID := args[0]
		projectName, _ := cmd.Flags().GetString("project")
		baseBranch, _ := cmd.Flags().GetString("base")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		// Get session.
		sess, err := db.GetSession(sessionID)
		if err != nil {
			return fmt.Errorf("session not found: %s", sessionID)
		}

		// Get enacted bills with branches.
		bills, err := db.ListBillsBySession(sessionID)
		if err != nil {
			return fmt.Errorf("list bills: %w", err)
		}

		var billBranches []privy.BillBranch
		for _, b := range bills {
			if b.Status == "enacted" && b.Branch.String != "" {
				billBranches = append(billBranches, privy.BillBranch{
					BillID: b.ID,
					Branch: b.Branch.String,
					Title:  b.Title,
				})
			}
		}

		if len(billBranches) == 0 {
			fmt.Printf("ℹ  会期 [%s] \"%s\" 中没有已通过且有分支的议案。\n", sessionID, sess.Title)
			fmt.Println("  (只有通过 `hoc minister summon --bill` 方式处理的议案才有 git 分支)")
			return nil
		}

		// Resolve main repo path.
		mainRepo := privy.MainRepoPath(hocDir, projectName)
		if _, err := os.Stat(mainRepo); os.IsNotExist(err) {
			return fmt.Errorf("项目 %s 的主仓库不存在: %s\n请先运行 hoc project add", projectName, mainRepo)
		}

		fmt.Printf("🏛  枢密院合并启动\n")
		fmt.Printf("   会期:   [%s] %s\n", sessionID, sess.Title)
		fmt.Printf("   项目:   %s (%s)\n", projectName, mainRepo)
		fmt.Printf("   基础分支: %s\n", orDefault(baseBranch, "自动检测"))
		fmt.Printf("   待合并议案: %d 个\n", len(billBranches))
		fmt.Println()

		for _, bb := range billBranches {
			fmt.Printf("   📄 [%s] \"%s\" ← 分支: %s\n", bb.BillID, bb.Title, bb.Branch)
		}
		fmt.Println()

		if dryRun {
			fmt.Println("  ℹ  预演模式，未执行实际合并。去掉 --dry-run 以执行。")
			return nil
		}

		// Execute Privy Council merge.
		result, err := privy.MergeSession(mainRepo, billBranches, baseBranch)
		if err != nil {
			return fmt.Errorf("privy council merge: %w", err)
		}

		if result.Success {
			// Mark enacted bills as royal_assent.
			for _, billID := range result.MergedBills {
				if err := db.UpdateBillStatus(billID, "royal_assent"); err != nil {
					slog.Warn("更新议案状态失败", "bill_id", billID, "err", err)
				}
			}

			// Create completion gazette.
			mergedTitles := make([]string, 0, len(billBranches))
			for _, bb := range billBranches {
				mergedTitles = append(mergedTitles, fmt.Sprintf("[%s]", bb.BillID))
			}
			summary := fmt.Sprintf(
				"枢密院合并成功。会期 [%s] \"%s\" 共 %d 个议案已合并至分支 `%s`。\n\n议案: %s\n\n下一步: git checkout %s && git merge %s",
				sessionID, sess.Title, len(result.MergedBills), result.MergeBranch,
				strings.Join(mergedTitles, ", "), orDefault(baseBranch, "main"), result.MergeBranch,
			)
			g := &store.Gazette{
				ID:      shortID("gazette"),
				Type:    store.NullString("completion"),
				Summary: summary,
			}
			warnIfErr("create gazette", db.CreateGazette(g), "gazette_id", g.ID, "session_id", sessionID)

			fmt.Printf("✅ 枢密院合并成功！\n")
			fmt.Printf("   合并分支: %s\n", result.MergeBranch)
			fmt.Printf("   Royal Assent: %s\n", strings.Join(result.MergedBills, ", "))
			fmt.Printf("\n下一步，将合并分支推送/合并到主分支：\n")
			fmt.Printf("  cd %s\n", mainRepo)
			fmt.Printf("  git checkout %s && git merge --no-ff %s\n",
				orDefault(baseBranch, "main"), result.MergeBranch)
		} else {
			// Merge conflict — create Conflict Gazette.
			summary := fmt.Sprintf(
				"枢密院合并冲突：会期 [%s] \"%s\"。\n\n%s\n\n请手动解决冲突后重新运行 hoc privy merge。",
				sessionID, sess.Title, result.Message,
			)
			g := &store.Gazette{
				ID:      shortID("gazette"),
				Type:    store.NullString("conflict"),
				Summary: summary,
			}
			warnIfErr("create gazette", db.CreateGazette(g), "gazette_id", g.ID, "session_id", sessionID)

			fmt.Printf("⚠  枢密院合并冲突\n")
			fmt.Printf("   冲突议案: %s\n", strings.Join(result.ConflictBills, ", "))
			if len(result.ConflictFiles) > 0 {
				fmt.Printf("   冲突文件:\n")
				for _, f := range result.ConflictFiles {
					fmt.Printf("     - %s\n", f)
				}
			}
			fmt.Println()
			fmt.Println("  Conflict Gazette 已创建。请手动解决冲突：")
			fmt.Printf("  cd %s\n", mainRepo)
			fmt.Printf("  git checkout -b privy/manual-merge\n")
			fmt.Println("  # 逐个 merge 分支，手动解决冲突")
		}

		return nil
	},
}

// privyAnalyzeCmd performs a dry-run conflict analysis without actually merging.
// Phase 3A — hoc privy analyze.
var privyAnalyzeCmd = &cobra.Command{
	Use:   "analyze <branch>",
	Short: "分析分支合并冲突（干跑，不执行实际合并）",
	Long: `枢密院分析：预测将指定分支合并到基础分支时可能产生的冲突。

不修改任何工作目录或分支，只做预测分析。

示例：
  hoc privy analyze feat/auth-backend --project myapp
  hoc privy analyze feat/auth-backend --project myapp --base develop`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		branch := args[0]
		projectName, _ := cmd.Flags().GetString("project")
		baseBranch, _ := cmd.Flags().GetString("base")

		mainRepo := privy.MainRepoPath(hocDir, projectName)
		if _, err := os.Stat(mainRepo); os.IsNotExist(err) {
			return fmt.Errorf("项目 %s 的主仓库不存在: %s\n请先运行 hoc project add", projectName, mainRepo)
		}

		fmt.Printf("🔍 枢密院冲突分析\n")
		fmt.Printf("   分支:   %s\n", branch)
		fmt.Printf("   项目:   %s (%s)\n", projectName, mainRepo)
		fmt.Printf("   基础分支: %s\n", orDefault(baseBranch, "自动检测"))
		fmt.Println()

		infos, err := privy.AnalyzeBranch(mainRepo, branch, baseBranch)
		if err != nil {
			return fmt.Errorf("分析失败: %w", err)
		}

		if len(infos) == 0 {
			fmt.Println("✅ 无预测冲突 — 可安全合并")
			return nil
		}

		fmt.Printf("⚠  预测到 %d 个冲突文件：\n\n", len(infos))
		for _, ci := range infos {
			blocksStr := ""
			if ci.Blocks > 0 {
				blocksStr = fmt.Sprintf("，约 %d 块冲突", ci.Blocks)
			}
			fmt.Printf("   📄 %s\n", ci.File)
			fmt.Printf("      类型: %s%s\n", ci.Type, blocksStr)
		}
		fmt.Println()
		fmt.Println("💡 建议：使用 hoc privy merge 时可配合策略链自动解决部分冲突。")

		return nil
	},
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
