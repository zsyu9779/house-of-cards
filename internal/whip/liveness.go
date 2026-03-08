package whip

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
)

// ─── Three-Line Whip ────────────────────────────────────────────────────────

// threeLineWhip checks liveness of all working and stuck Ministers.
//
// Two-step detection (design doc compliant):
//  1. working → no heartbeat for gracePeriod (30s) → mark stuck
//  2. stuck → heartbeat stale beyond stuckThreshold (5min) → byElection
func (w *Whip) threeLineWhip() {
	// Pass 1: check working ministers → mark stuck if unresponsive.
	working, err := w.db.ListWorkingMinisters()
	if err != nil {
		slog.Warn("threeLineWhip: list working ministers", "err", err)
		return
	}

	for _, m := range working {
		if w.isMinisterAlive(m) {
			_ = w.db.UpdateMinisterHeartbeat(m.ID)
			continue
		}

		// Process/session gone. Within grace period → skip.
		if m.Heartbeat.Valid && time.Since(m.Heartbeat.Time) < gracePeriod {
			continue
		}

		// Beyond grace period → mark stuck (not byElection yet).
		slog.Warn("部长无响应，标记为 stuck", "minister_id", m.ID)
		_ = w.db.UpdateMinisterStatus(m.ID, "stuck")
		_ = w.db.RecordEvent("minister.stuck", "whip", "", m.ID, "", fmt.Sprintf(`{"reason":"heartbeat_timeout"}`))
	}

	// Pass 2: check stuck ministers → byElection if stuck beyond stuckThreshold.
	stuck, err := w.db.ListMinistersWithStatus("stuck")
	if err != nil {
		slog.Warn("threeLineWhip: list stuck ministers", "err", err)
		return
	}

	for _, m := range stuck {
		// If heartbeat is recent enough, give them more time.
		if m.Heartbeat.Valid && time.Since(m.Heartbeat.Time) < stuckThreshold {
			continue
		}

		slog.Warn("部长 stuck 超时，触发补选", "minister_id", m.ID, "threshold", stuckThreshold)
		w.byElection(m)
	}
}

// byElection executes the By-election (补选) procedure for a non-responsive Minister:
//  1. git stash in the minister's worktree (if any uncommitted work)
//  2. Generate a Handoff Gazette for continuity
//  3. Clear the minister's bill assignments (bill → draft)
//  4. Write a Hansard entry (outcome: "failed")
//  5. Mark minister as offline
func (w *Whip) byElection(m *store.Minister) {
	whipMetrics.byElectionTotal.Inc()
	_ = w.db.RecordEvent("by_election.triggered", "whip", "", m.ID, "", fmt.Sprintf(`{"minister":"%s"}`, m.ID))

	_, span := w.tracer.Start(context.Background(), "whip.by_election")
	defer span.End()
	span.SetAttr("minister_id", m.ID)

	bills, err := w.db.GetBillsByAssignee(m.ID)
	if err != nil {
		slog.Warn("byElection: get bills", "minister_id", m.ID, "err", err)
		span.RecordError(err)
		return
	}

	worktree := m.Worktree.String
	stashRef := ""

	// Try to stash any uncommitted work in the minister's chamber.
	if worktree != "" {
		if _, statErr := os.Stat(worktree); statErr == nil {
			stashMsg := fmt.Sprintf("hoc-by-election-%s-%d", m.ID, time.Now().Unix())
			cmd := exec.Command("git", "stash", "push", "-m", stashMsg)
			cmd.Dir = worktree
			if out, err := cmd.CombinedOutput(); err == nil {
				// Check if stash was created (vs "No local changes").
				if !strings.Contains(string(out), "No local changes") {
					stashRef = stashMsg
					slog.Info("stash 保存进度", "ref", stashMsg, "worktree", worktree)
				}
			}
		}
	}

	// Process each assigned bill.
	for _, bill := range bills {
		if bill.Status == "enacted" || bill.Status == "royal_assent" || bill.Status == "failed" {
			continue // Already terminal.
		}

		branchInfo := ""
		if bill.Branch.String != "" {
			branchInfo = fmt.Sprintf("分支: `%s`", bill.Branch.String)
		}
		stashInfo := ""
		if stashRef != "" {
			stashInfo = fmt.Sprintf("\n未提交的进度已 stash 保存：`%s`（位于议事厅 `%s`）", stashRef, worktree)
		}

		// Create Handoff Gazette for continuity.
		summary := fmt.Sprintf(
			"补选公报：部长 [%s] 工作中断（心跳超时）。\n\n"+
				"议案 [%s] \"%s\" 需要接手。%s%s\n\n"+
				"接手指令：\n```\nhoc minister summon <new-minister> --bill %s --project <project>\n```",
			m.ID, bill.ID, bill.Title, branchInfo, stashInfo, bill.ID,
		)
		g := &store.Gazette{
			ID:           gazetteID(),
			FromMinister: store.NullString(m.ID),
			BillID:       store.NullString(bill.ID),
			Type:         store.NullString("handoff"),
			Summary:      summary,
		}
		if err := w.db.CreateGazette(g); err != nil {
			slog.Warn("byElection: create gazette", "err", err)
		}

		// Write Hansard entry.
		h := &store.Hansard{
			ID:         fmt.Sprintf("hansard-%x", time.Now().UnixNano()),
			MinisterID: m.ID,
			BillID:     bill.ID,
			Outcome:    store.NullString("failed"),
			Notes:      store.NullString("补选触发：心跳超时，由 Whip 自动记录"),
		}
		if err := w.db.CreateHansard(h); err != nil {
			slog.Warn("byElection: create hansard", "err", err)
		}

		// Reset bill to draft so orderPaper() can re-assign it.
		if err := w.db.ClearBillAssignment(bill.ID); err != nil {
			slog.Warn("byElection: clear bill assignment", "err", err)
		}

		slog.Info("议案已重置为 draft，等待重新派发", "bill_id", bill.ID)
	}

	// Mark minister as offline.
	if err := w.db.UpdateMinisterStatus(m.ID, "offline"); err != nil {
		slog.Warn("byElection: update minister status", "err", err)
	}

	slog.Info("补选完成", "minister_id", m.ID, "status", "offline")
	_ = w.db.RecordEvent("by_election.completed", "whip", "", m.ID, "", fmt.Sprintf(`{"status":"offline"}`))
}

// isMinisterAlive checks if the Minister's backing process is still running.
func (w *Whip) isMinisterAlive(m *store.Minister) bool {
	// Check tmux session first (most common for claude-code runtime).
	tmuxName := fmt.Sprintf("hoc-%s", m.ID)
	if exec.Command("tmux", "has-session", "-t", tmuxName).Run() == nil {
		return true
	}

	// Check by PID.
	if m.Pid > 0 {
		if exec.Command("kill", "-0", fmt.Sprintf("%d", m.Pid)).Run() == nil {
			return true
		}
	}

	return false
}
