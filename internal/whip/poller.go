package whip

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
)

// ─── Done File Polling (8-1) ─────────────────────────────────────────────────

// pollDoneFiles scans each working minister's chamber for `.hoc/bill-<id>.done` files.
// When found the corresponding bill is automatically enacted and a completion Gazette is created.
func (w *Whip) pollDoneFiles() {
	working, err := w.db.ListWorkingMinisters()
	if err != nil {
		slog.Warn("pollDoneFiles: list working ministers", "err", err)
		return
	}

	for _, m := range working {
		worktree := m.Worktree.String
		if worktree == "" {
			continue
		}
		if _, err := os.Stat(worktree); err != nil {
			continue
		}

		bills, err := w.db.GetBillsByAssignee(m.ID)
		if err != nil {
			slog.Warn("pollDoneFiles: get bills", "minister_id", m.ID, "err", err)
			continue
		}

		for _, bill := range bills {
			if bill.Status == "enacted" || bill.Status == "royal_assent" || bill.Status == "failed" {
				continue
			}

			donePath := filepath.Join(worktree, ".hoc", fmt.Sprintf("bill-%s.done", bill.ID))
			if _, err := os.Stat(donePath); os.IsNotExist(err) {
				continue
			}

			// Read optional summary from done file.
			summaryBytes, _ := os.ReadFile(donePath)
			summary := strings.TrimSpace(string(summaryBytes))
			if summary == "" {
				summary = fmt.Sprintf("议案 [%s] 已完成（无摘要）", bill.ID)
			}

			slog.Info("检测到 done 文件，自动 enacted", "bill_id", bill.ID, "minister_id", m.ID)
			if err := w.db.EnactBillFromDone(bill.ID, m.ID, summary); err != nil {
				slog.Warn("pollDoneFiles: enact bill", "bill_id", bill.ID, "err", err)
				continue
			}
			_ = w.db.RecordEvent("bill.enacted", "whip", bill.ID, m.ID, bill.SessionID.String, "")

			// Remove done file to avoid re-processing.
			_ = os.Remove(donePath)
		}

		// Check if minister has no more active bills → mark idle.
		remaining, _ := w.db.GetBillsByAssignee(m.ID)
		hasActive := false
		for _, rb := range remaining {
			if rb.Status != "enacted" && rb.Status != "royal_assent" && rb.Status != "failed" {
				hasActive = true
				break
			}
		}
		if !hasActive {
			_ = w.db.UpdateMinisterStatus(m.ID, "idle")
			slog.Info("部长已完成所有议案，标记为 idle", "minister_id", m.ID)
		}
	}
}

// ─── Phase 3B: Hook Queue & Idle Re-assign ───────────────────────────────────

// pollIdleMinisterReassign checks idle ministers for queued bills in their hook.
// When a minister becomes idle and has a hook queue entry, auto-assign the next bill.
// This enables continuous work: a minister finishes one bill and immediately picks
// up the next from their personal queue.
func (w *Whip) pollIdleMinisterReassign() {
	idleMinisters, err := w.db.ListIdleMinistersForSkill("")
	if err != nil {
		slog.Warn("pollIdleMinisterReassign: list idle ministers", "err", err)
		return
	}

	for _, m := range idleMinisters {
		billID, err := w.db.PopHook(m.ID)
		if err != nil || billID == "" {
			continue
		}

		bill, err := w.db.GetBill(billID)
		if err != nil {
			slog.Warn("pollIdleMinisterReassign: get bill", "bill_id", billID, "err", err)
			continue
		}

		// Only assign if bill is still in a assignable state.
		if bill.Status != "draft" && bill.Status != "reading" {
			slog.Debug("hook 队列议案已完成，跳过", "bill_id", billID, "status", bill.Status)
			continue
		}

		slog.Info("Hook 队列自动接单", "minister_id", m.ID, "bill_id", billID)
		w.autoAssign(bill, m, nil)
	}
}

// ─── Committee Automation (8-2) ──────────────────────────────────────────────

// committeeAutomation auto-assigns committee-stage bills to idle reviewer ministers,
// and polls for .review files written by assigned reviewers.
func (w *Whip) committeeAutomation() {
	bills, err := w.db.ListBillsForCommittee()
	if err != nil {
		slog.Warn("committeeAutomation: list committee bills", "err", err)
		return
	}

	for _, bill := range bills {
		if bill.Assignee.String != "" {
			// Already assigned to a reviewer — poll for their review file.
			w.pollReviewFile(bill)
			continue
		}

		// Find an idle reviewer minister.
		reviewers, err := w.db.ListIdleMinistersForSkill("reviewer")
		if err != nil || len(reviewers) == 0 {
			slog.Debug("无空闲 reviewer 部长", "bill_id", bill.ID)
			continue
		}

		reviewer := reviewers[0]
		if err := w.db.AssignBill(bill.ID, reviewer.ID); err != nil {
			slog.Warn("committeeAutomation: assign", "bill_id", bill.ID, "err", err)
			continue
		}

		slog.Info("委员会自动分配", "bill_id", bill.ID, "reviewer_id", reviewer.ID)
		_ = w.db.RecordEvent("committee.assigned", "whip", bill.ID, reviewer.ID, bill.SessionID.String, "")

		summary := fmt.Sprintf(
			"委员会令：议案 [%s] \"%s\" 进入委员会审查阶段，已自动分配给 %s。\n"+
				"审查完成后请写入议事厅 `.hoc/bill-%s.review`：\n```\nPASS\n审查意见（可选）\n```\n或写 FAIL 表示退回修改。",
			bill.ID, bill.Title, reviewer.Title, bill.ID,
		)
		g := &store.Gazette{
			ID:           gazetteID(),
			ToMinister:   store.NullString(reviewer.ID),
			BillID:       store.NullString(bill.ID),
			Type:         store.NullString("review"),
			Summary:      summary,
			FromMinister: store.NullString("whip"),
		}
		if err := w.db.CreateGazette(g); err != nil {
			slog.Warn("committeeAutomation: create gazette", "err", err)
		}
	}
}

// pollReviewFile checks if a reviewer has written a .review file for their assigned bill.
// PASS → bill enacted; FAIL → bill reset to draft for re-assignment.
func (w *Whip) pollReviewFile(bill *store.Bill) {
	reviewerID := bill.Assignee.String
	if reviewerID == "" {
		return
	}

	reviewer, err := w.db.GetMinister(reviewerID)
	if err != nil || reviewer.Worktree.String == "" {
		return
	}

	reviewPath := filepath.Join(reviewer.Worktree.String, ".hoc", fmt.Sprintf("bill-%s.review", bill.ID))
	content, err := os.ReadFile(reviewPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		return
	}

	text := strings.TrimSpace(string(content))
	pass := strings.HasPrefix(strings.ToUpper(text), "PASS")

	if pass {
		_ = w.db.UpdateBillStatus(bill.ID, "enacted")
	} else {
		// Fail → reset to draft so Whip can reassign.
		_ = w.db.UpdateBillStatus(bill.ID, "draft")
	}
	_ = w.db.UnassignBill(bill.ID)
	_ = w.db.UpdateMinisterStatus(reviewerID, "idle")
	_ = os.Remove(reviewPath)

	outcome := "enacted"
	icon := "✅"
	if !pass {
		outcome = "failed"
		icon = "❌"
	}
	slog.Info("委员会审查结果", "bill_id", bill.ID, "pass", pass)
	_ = w.db.RecordEvent("committee.result", "whip", bill.ID, reviewerID, bill.SessionID.String, fmt.Sprintf(`{"outcome":"%s"}`, outcome))

	reviewNotes := fmt.Sprintf("委员会审查: %s", util.Truncate(text, 120))
	reviewQuality := store.ComputeBillQuality(outcome, reviewNotes)
	h := &store.Hansard{
		ID:         fmt.Sprintf("hansard-%x", time.Now().UnixNano()),
		MinisterID: reviewerID,
		BillID:     bill.ID,
		Outcome:    store.NullString(outcome),
		Quality:    reviewQuality,
		Notes:      store.NullString(reviewNotes),
	}
	_ = w.db.CreateHansard(h)

	g := &store.Gazette{
		ID:           gazetteID(),
		FromMinister: store.NullString(reviewerID),
		BillID:       store.NullString(bill.ID),
		Type:         store.NullString("review"),
		Summary: fmt.Sprintf(
			"委员会审查公报：议案 [%s] %s %s\n\n%s",
			bill.ID, icon, outcome, text,
		),
	}
	_ = w.db.CreateGazette(g)
}
