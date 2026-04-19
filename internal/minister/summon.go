package minister

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/house-of-cards/hoc/internal/chamber"
	"github.com/house-of-cards/hoc/internal/runtime"
	"github.com/house-of-cards/hoc/internal/store"
)

// SummonOpts carries the parameters needed to summon a minister to a bill.
type SummonOpts struct {
	DB          *store.DB
	HocDir      string
	MinisterID  string
	BillID      string
	ProjectName string
	UseTmux     bool
}

// SummonResult describes the outcome of a successful Summon call.
type SummonResult struct {
	Worktree string
	Branch   string
	PID      int
	Reused   bool // true when the chamber already existed before this summon
}

// runtimeFactory is indirected through a package variable so tests can inject
// a stub runtime without launching real tmux sessions or subprocesses.
var runtimeFactory = runtime.New

// Summon executes the full pipeline that seats a minister in a chamber and
// starts an AI runtime pointed at the given bill. Steps:
//  1. Resolve minister and bill from the store.
//  2. Create (or reuse) the git worktree chamber for the minister+project.
//  3. Write .claude/CLAUDE.md into the chamber.
//  4. Build the bill brief (including upstream gazettes) and hand it to the
//     runtime as the initial prompt.
//  5. Update minister status/worktree/PID and bill status/branch in the store.
//
// On failure after the chamber is created, Summon attempts to roll back by
// removing the worktree and killing any runtime session it started.
func Summon(opts SummonOpts) (result *SummonResult, err error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("summon: db is required")
	}

	minister, err := opts.DB.GetMinister(opts.MinisterID)
	if err != nil {
		return nil, fmt.Errorf("minister not found: %s", opts.MinisterID)
	}

	bill, err := opts.DB.GetBill(opts.BillID)
	if err != nil {
		return nil, fmt.Errorf("bill not found: %s", opts.BillID)
	}

	mainRepoPath := chamberRepoPath(opts.HocDir, opts.ProjectName)
	if _, statErr := os.Stat(mainRepoPath); os.IsNotExist(statErr) {
		return nil, fmt.Errorf("项目 %s 不存在，请先运行 hoc project add", opts.ProjectName)
	}

	var cleanups []func()
	defer func() {
		if err == nil {
			return
		}
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()

	ch, chErr := chamber.NewChamber(opts.HocDir, opts.ProjectName, opts.MinisterID, mainRepoPath)
	if chErr != nil {
		err = fmt.Errorf("init chamber: %w", chErr)
		return nil, err
	}

	reused := true
	if _, statErr := os.Stat(ch.GetWorktreePath()); os.IsNotExist(statErr) {
		if createErr := ch.Create(); createErr != nil {
			err = fmt.Errorf("create chamber: %w", createErr)
			return nil, err
		}
		reused = false
		cleanups = append(cleanups, func() {
			// Try the normal `git worktree remove` path first; if that fails
			// (e.g. because git state is inconsistent) fall back to a plain
			// filesystem delete so we never leak orphan chamber directories.
			if rmErr := ch.Remove(); rmErr != nil {
				slog.Warn("summon rollback: remove chamber", "minister_id", opts.MinisterID, "err", rmErr)
				if fsErr := os.RemoveAll(ch.GetWorktreePath()); fsErr != nil {
					slog.Warn("summon rollback: remove chamber dir", "minister_id", opts.MinisterID, "err", fsErr)
				}
			}
		})
	}

	claudePath := filepath.Join(ch.GetWorktreePath(), ".claude", "CLAUDE.md")
	if mkErr := os.MkdirAll(filepath.Dir(claudePath), 0755); mkErr != nil {
		err = fmt.Errorf("create .claude dir: %w", mkErr)
		return nil, err
	}
	claudeContent := BuildMinisterCLAUDE(minister, bill, ch.GetBranchName())
	if wErr := os.WriteFile(claudePath, []byte(claudeContent), 0644); wErr != nil {
		err = fmt.Errorf("write CLAUDE.md: %w", wErr)
		return nil, err
	}

	brief := BuildBillBrief(minister, bill, ch.GetBranchName())
	if upstream, lgErr := opts.DB.ListGazettesForBill(opts.BillID); lgErr == nil && len(upstream) > 0 {
		var sb strings.Builder
		sb.WriteString("\n## 上游公报（来自前序部长）\n\n")
		for _, g := range upstream {
			sb.WriteString(FormatUpstreamGazette(g))
			sb.WriteString("\n---\n\n")
		}
		brief += sb.String()
	}

	rt := runtimeFactory(minister.Runtime, opts.UseTmux)
	agentSess, sumErr := rt.Summon(runtime.SummonOpts{
		MinisterID:    opts.MinisterID,
		MinisterTitle: minister.Title,
		ChamberPath:   ch.GetWorktreePath(),
		BillBrief:     brief,
	})
	if sumErr != nil {
		err = fmt.Errorf("summon runtime: %w", sumErr)
		return nil, err
	}
	cleanups = append(cleanups, func() {
		if dErr := rt.Dismiss(agentSess); dErr != nil {
			slog.Warn("summon rollback: dismiss runtime", "minister_id", opts.MinisterID, "err", dErr)
		}
	})

	if uErr := opts.DB.UpdateMinisterStatus(opts.MinisterID, "working"); uErr != nil {
		err = fmt.Errorf("update minister status: %w", uErr)
		return nil, err
	}
	if uErr := opts.DB.UpdateMinisterWorktree(opts.MinisterID, ch.GetWorktreePath()); uErr != nil {
		err = fmt.Errorf("update minister worktree: %w", uErr)
		return nil, err
	}
	if agentSess.PID > 0 {
		if uErr := opts.DB.UpdateMinisterPID(opts.MinisterID, agentSess.PID); uErr != nil {
			slog.Warn("summon: update minister PID", "minister_id", opts.MinisterID, "err", uErr)
		}
	}
	if uErr := opts.DB.AssignBill(opts.BillID, opts.MinisterID); uErr != nil {
		err = fmt.Errorf("assign bill: %w", uErr)
		return nil, err
	}
	if uErr := opts.DB.UpdateBillStatus(opts.BillID, "reading"); uErr != nil {
		slog.Warn("summon: update bill status", "bill_id", opts.BillID, "err", uErr)
	}
	if uErr := opts.DB.UpdateBillBranch(opts.BillID, ch.GetBranchName()); uErr != nil {
		slog.Warn("summon: update bill branch", "bill_id", opts.BillID, "err", uErr)
	}

	return &SummonResult{
		Worktree: ch.GetWorktreePath(),
		Branch:   ch.GetBranchName(),
		PID:      agentSess.PID,
		Reused:   reused,
	}, nil
}

// chamberRepoPath returns the expected main-repo path for the given project.
func chamberRepoPath(hocDir, projectName string) string {
	return filepath.Join(hocDir, "projects", projectName, "main")
}
