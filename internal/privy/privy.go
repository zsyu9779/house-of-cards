// Package privy implements the Privy Council — the merge arbitration layer.
//
// When parallel Bills are all enacted, the Privy Council tries to merge their
// branches into a unified integration branch. On success all merged bills
// receive Royal Assent. On conflict a Conflict Gazette is issued.
//
// Phase 3A: Enhanced with ConflictInfo analysis and auto-resolution strategy chain.
package privy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/otel"
)

// BillBranch represents a bill and its associated git branch.
type BillBranch struct {
	BillID string
	Branch string
	Title  string
}

// MergeResult is the outcome of a Privy Council merge attempt.
type MergeResult struct {
	Success       bool
	MergeBranch   string // The branch created by the Privy Council.
	MergedBills   []string
	ConflictFiles []string
	ConflictBills []string
	ConflictInfos []ConflictInfo // Phase 3A: detailed conflict analysis
	Message       string
	StrategyUsed  string   // Phase 3A: which strategy succeeded
	StrategyTries []string // Phase 3A: all strategies attempted
}

// ConflictInfo holds structured information about a single conflicting file.
// Phase 3A — 冲突理解增强.
type ConflictInfo struct {
	File     string // 冲突文件路径
	Blocks   int    // 冲突块数量（<<<<<<< HEAD 计数）
	Type     string // "content" | "delete_vs_modify" | "both_modified"
	OurSHA   string // ours 分支最新 commit
	TheirSHA string // theirs 分支最新 commit
}

// MergeSession tries to merge all provided branches into a new integration branch
// created from HEAD of the main repo.
//
// Phase 3A: Implements a strategy chain:
//  1. git merge --no-ff (default)
//  2. git rebase -X theirs (prefer their changes)
//  3. git merge -X ours (prefer our changes)
//
// mainRepo is the path to the main git repository (not a worktree).
// bills contains the list of enacted bills with their branch names.
// baseBranch is the branch to start from (defaults to "main" if empty).
func MergeSession(mainRepo string, bills []BillBranch, baseBranch string) (*MergeResult, error) {
	tracer := otel.GlobalTracer("privy")
	_, span := tracer.Start(context.Background(), "privy.merge")
	defer span.End()
	span.SetAttr("bills.count", len(bills))
	span.SetAttr("base_branch", baseBranch)

	if _, err := os.Stat(mainRepo); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("main repo not found: %s", mainRepo)
	}

	if baseBranch == "" {
		baseBranch = detectDefaultBranch(mainRepo)
	}

	// Create a new merge branch from base.
	mergeBranch := fmt.Sprintf("privy/merge-%d", time.Now().Unix())

	gitCmd := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = mainRepo
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// Ensure we're on the base branch first.
	if _, err := gitCmd("checkout", baseBranch); err != nil {
		return nil, fmt.Errorf("checkout %s: %w", baseBranch, err)
	}

	// Create the Privy Council merge branch.
	if out, err := gitCmd("checkout", "-b", mergeBranch); err != nil {
		return nil, fmt.Errorf("create merge branch %s: %w\noutput: %s", mergeBranch, err, out)
	}

	result := &MergeResult{
		MergeBranch: mergeBranch,
	}

	// Try merging each bill's branch using the strategy chain.
	for _, bill := range bills {
		if bill.Branch == "" {
			continue
		}

		merged, strategyUsed, tries, conflictInfos := tryMergeWithStrategyChain(gitCmd, bill)
		result.StrategyTries = append(result.StrategyTries, tries...)

		if merged {
			result.MergedBills = append(result.MergedBills, bill.BillID)
			if strategyUsed != "" {
				result.StrategyUsed = strategyUsed
			}
		} else {
			// All strategies failed — collect conflict info and abort.
			otel.Metrics().Counter("hoc_conflicts_total").Inc()
			span.SetAttr("conflict.bill_id", bill.BillID)
			span.SetAttr("conflict.files", len(conflictInfos))

			result.ConflictInfos = conflictInfos
			for _, ci := range conflictInfos {
				result.ConflictFiles = append(result.ConflictFiles, ci.File)
			}
			result.ConflictBills = append(result.ConflictBills, bill.BillID)

			conflictLines := make([]string, 0, len(conflictInfos))
			for _, ci := range conflictInfos {
				conflictLines = append(conflictLines, fmt.Sprintf("  - %s（%d 块冲突，类型: %s）", ci.File, ci.Blocks, ci.Type))
			}
			result.Message = fmt.Sprintf(
				"合并冲突：议案 [%s] \"%s\" 策略链全部失败。\n\n冲突文件：\n%s\n\n尝试策略：\n  %s",
				bill.BillID, bill.Title,
				strings.Join(conflictLines, "\n"),
				strings.Join(tries, "\n  "),
			)

			// Clean up — return to base and delete partial merge branch.
			_, _ = gitCmd("checkout", baseBranch)
			_, _ = gitCmd("branch", "-D", mergeBranch)
			result.MergeBranch = ""

			return result, nil
		}
	}

	result.Success = true
	strategyDesc := ""
	if result.StrategyUsed != "" {
		strategyDesc = fmt.Sprintf("（策略：%s）", result.StrategyUsed)
	}
	result.Message = fmt.Sprintf(
		"枢密院合并成功%s：%d 个议案已合并至分支 `%s`。\n下一步：将 `%s` 合并到 `%s`。",
		strategyDesc, len(result.MergedBills), mergeBranch, mergeBranch, baseBranch,
	)

	return result, nil
}

// tryMergeWithStrategyChain attempts to merge bill.Branch into current HEAD using
// the Phase 3A strategy chain:
//  1. git merge --no-ff
//  2. git rebase -X theirs (对方优先)
//  3. git merge -X ours (我方优先)
//
// Returns (success, strategyUsed, allTries, conflictInfosIfFailed).
func tryMergeWithStrategyChain(gitCmd func(...string) (string, error), bill BillBranch) (bool, string, []string, []ConflictInfo) {
	var tries []string
	mergeMsg := fmt.Sprintf("Privy Council: merge bill [%s] \"%s\"", bill.BillID, bill.Title)

	// Strategy 1: Standard merge --no-ff
	out, err := gitCmd("merge", "--no-ff", bill.Branch, "-m", mergeMsg)
	tries = append(tries, "策略 1: git merge --no-ff")
	if err == nil {
		return true, "git merge --no-ff", tries, nil
	}

	// Merge failed — abort and analyze conflicts.
	conflictInfos := analyzeConflicts(gitCmd, bill.Branch, out)
	_, _ = gitCmd("merge", "--abort")
	tries[len(tries)-1] += " → 冲突"

	// Strategy 2: rebase -X theirs (prefer their changes)
	// First reset back to our current HEAD.
	out2, err2 := gitCmd("merge", "-X", "theirs", "--no-ff", bill.Branch, "-m", mergeMsg+" (strategy: theirs)")
	tries = append(tries, "策略 2: git merge -X theirs")
	if err2 == nil {
		return true, "git merge -X theirs（对方优先）", tries, nil
	}
	_, _ = gitCmd("merge", "--abort")
	_ = out2
	tries[len(tries)-1] += " → 冲突"

	// Strategy 3: merge -X ours (prefer our changes)
	out3, err3 := gitCmd("merge", "-X", "ours", "--no-ff", bill.Branch, "-m", mergeMsg+" (strategy: ours)")
	tries = append(tries, "策略 3: git merge -X ours")
	if err3 == nil {
		return true, "git merge -X ours（我方优先）", tries, nil
	}
	_, _ = gitCmd("merge", "--abort")
	_ = out3
	tries[len(tries)-1] += " → 冲突"

	return false, "", tries, conflictInfos
}

// analyzeConflicts performs detailed conflict analysis on the merge output.
// Phase 3A — 3A-1: 结构化冲突分析.
func analyzeConflicts(gitCmd func(...string) (string, error), theirBranch string, mergeOutput string) []ConflictInfo {
	files := parseConflictFiles(mergeOutput)

	// Get OUR SHA and THEIR SHA.
	ourSHA := ""
	if out, err := gitCmd("rev-parse", "--short", "HEAD"); err == nil {
		ourSHA = strings.TrimSpace(out)
	}
	theirSHA := ""
	if out, err := gitCmd("rev-parse", "--short", theirBranch); err == nil {
		theirSHA = strings.TrimSpace(out)
	}

	var infos []ConflictInfo
	for _, f := range files {
		ci := ConflictInfo{
			File:     f,
			OurSHA:   ourSHA,
			TheirSHA: theirSHA,
		}

		// Count conflict blocks in the file.
		content, err := os.ReadFile(f)
		if err == nil {
			ci.Blocks = strings.Count(string(content), "<<<<<<< ")
		}

		// Detect conflict type from merge output.
		ci.Type = detectConflictType(mergeOutput, f)

		infos = append(infos, ci)
	}
	return infos
}

// detectConflictType infers the type of conflict from git merge output lines.
func detectConflictType(output, file string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, file) {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "delete") {
			return "delete_vs_modify"
		}
		if strings.Contains(lower, "add/add") || strings.Contains(lower, "both added") {
			return "both_added"
		}
	}
	return "content"
}

// AnalyzeBranch performs a dry-run merge analysis between the current HEAD and
// a branch to predict conflicts before actually merging.
// Phase 3A — 供 hoc privy analyze 命令使用.
func AnalyzeBranch(mainRepo, branch, baseBranch string) ([]ConflictInfo, error) {
	if _, err := os.Stat(mainRepo); err != nil {
		return nil, fmt.Errorf("main repo not found: %s", mainRepo)
	}

	if baseBranch == "" {
		baseBranch = detectDefaultBranch(mainRepo)
	}

	gitCmd := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = mainRepo
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// Use git merge-tree for dry-run analysis (no worktree changes).
	mergeBaseOut, err := gitCmd("merge-base", baseBranch, branch)
	if err != nil {
		return nil, fmt.Errorf("找不到 merge base: %w", err)
	}
	mergeBase := strings.TrimSpace(mergeBaseOut)

	// Run merge-tree to detect potential conflicts.
	out, _ := gitCmd("merge-tree", mergeBase, baseBranch, branch)

	var infos []ConflictInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CONFLICT") {
			if idx := strings.LastIndex(line, " "); idx >= 0 {
				f := line[idx+1:]
				infos = append(infos, ConflictInfo{
					File: f,
					Type: detectConflictType(out, f),
				})
			}
		}
	}

	return infos, nil
}

// MainRepoFromWorktree infers the main repo path from a minister's worktree path.
// Worktree pattern: <hocDir>/projects/<project>/chambers/<ministerID>
// Main repo:        <hocDir>/projects/<project>/main.
func MainRepoFromWorktree(worktree string) string {
	// Walk up to "chambers" directory, then sibling "main".
	dir := worktree
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		base := filepath.Base(dir)
		if base == "chambers" {
			return filepath.Join(parent, "main")
		}
		dir = parent
	}
}

// detectDefaultBranch checks common default branch names.
func detectDefaultBranch(mainRepo string) string {
	for _, candidate := range []string{"main", "master"} {
		cmd := exec.Command("git", "rev-parse", "--verify", candidate)
		cmd.Dir = mainRepo
		if err := cmd.Run(); err == nil {
			return candidate
		}
	}
	return "main"
}

// parseConflictFiles extracts conflict file paths from git merge output.
func parseConflictFiles(output string) []string {
	var files []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CONFLICT") {
			// "CONFLICT (content): Merge conflict in <file>"
			if idx := strings.LastIndex(line, " "); idx >= 0 {
				f := line[idx+1:]
				if !seen[f] {
					seen[f] = true
					files = append(files, f)
				}
			}
		}
	}
	return files
}

// MainRepoPath returns the conventional main repo path for a project.
func MainRepoPath(hocDir, projectName string) string {
	return filepath.Join(hocDir, "projects", projectName, "main")
}

// FormatConflictGazette generates a structured Conflict Resolution Gazette
// in the standard Gazette format defined by Phase 3A spec.
func FormatConflictGazette(billID, billTitle, resolverMinister string, infos []ConflictInfo, strategyTries []string, notes string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Conflict Resolution Gazette: %s\n", billTitle))
	sb.WriteString(fmt.Sprintf("> Bill: %s | Resolver: %s | Date: %s\n\n",
		billID,
		resolverMinister,
		time.Now().Format("2006-01-02"),
	))

	if len(infos) > 0 {
		sb.WriteString("## 冲突文件\n")
		for _, ci := range infos {
			blocksStr := ""
			if ci.Blocks > 0 {
				blocksStr = fmt.Sprintf("，%d 块冲突", ci.Blocks)
			}
			sb.WriteString(fmt.Sprintf("- `%s` — 类型: %s%s\n", ci.File, ci.Type, blocksStr))
		}
		sb.WriteString("\n")
	}

	if len(strategyTries) > 0 {
		sb.WriteString("## 解决策略\n")
		for _, t := range strategyTries {
			sb.WriteString(fmt.Sprintf("- %s\n", t))
		}
		sb.WriteString("\n")
	}

	if notes != "" {
		sb.WriteString("## 遗留问题\n")
		sb.WriteString(notes)
		sb.WriteString("\n")
	}

	return sb.String()
}
