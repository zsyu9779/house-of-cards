package whip

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/util"
)

// ─── Done File Polling (8-1) ─────────────────────────────────────────────────

// parseDoneFile reads a .done file and parses it as TOML (structured) or plain text.
// Returns the summary and the JSON-encoded payload (if TOML), or empty string if not TOML.
func parseDoneFile(path string) (string, string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}

	var payload store.DoneFilePayload
	if _, err := toml.Decode(string(content), &payload); err != nil {
		// Not TOML, treat as plain text summary.
		return strings.TrimSpace(string(content)), ""
	}

	// TOML parsed successfully, serialize to JSON for storage.
	payloadJSON, _ := json.Marshal(payload)
	return payload.Summary, string(payloadJSON)
}

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

			// Parse done file (TOML or plain text).
			summary, payloadJSON := parseDoneFile(donePath)

			slog.Info("检测到 done 文件，自动 enacted", "bill_id", bill.ID, "minister_id", m.ID)
			if err := w.db.EnactBillFromDone(bill.ID, m.ID, summary, payloadJSON); err != nil {
				slog.Warn("pollDoneFiles: enact bill", "bill_id", bill.ID, "err", err)
				continue
			}
			if err := w.db.RecordEvent("bill.enacted", "whip", bill.ID, m.ID, bill.SessionID.String, ""); err != nil {
				slog.Warn("记录 enacted 事件失败", "bill_id", bill.ID, "err", err)
			}

			// Phase 4: B-1.4 — Collect question time metrics for the hansard.
			w.collectQuestionMetrics(bill.ID, m.ID)

			// Remove done file to avoid re-processing.
			_ = os.Remove(donePath) // best-effort: 清理 done 文件
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
			if err := w.db.UpdateMinisterStatus(m.ID, "idle"); err != nil {
				slog.Error("标记 idle 失败", "minister_id", m.ID, "err", err)
			} else {
				slog.Info("部长已完成所有议案，标记为 idle", "minister_id", m.ID)
			}
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
		if err := w.db.RecordEvent("committee.assigned", "whip", bill.ID, reviewer.ID, bill.SessionID.String, ""); err != nil {
			slog.Warn("记录委员会分配事件失败", "bill_id", bill.ID, "err", err)
		}

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
		if err := w.db.UpdateBillStatus(bill.ID, "enacted"); err != nil {
			slog.Error("pollReviewFile: update bill status enacted", "bill_id", bill.ID, "err", err)
			return
		}
	} else {
		// Fail → reset to draft so Whip can reassign.
		if err := w.db.UpdateBillStatus(bill.ID, "draft"); err != nil {
			slog.Error("pollReviewFile: update bill status draft", "bill_id", bill.ID, "err", err)
			return
		}
	}
	if err := w.db.UnassignBill(bill.ID); err != nil {
		slog.Error("pollReviewFile: unassign bill", "bill_id", bill.ID, "err", err)
	}
	if err := w.db.UpdateMinisterStatus(reviewerID, "idle"); err != nil {
		slog.Error("pollReviewFile: update minister status idle", "minister_id", reviewerID, "err", err)
	}
	_ = os.Remove(reviewPath) // best-effort: 清理 review 文件

	outcome := "enacted"
	icon := "✅"
	if !pass {
		outcome = "failed"
		icon = "❌"
	}
	slog.Info("委员会审查结果", "bill_id", bill.ID, "pass", pass)
	if err := w.db.RecordEvent("committee.result", "whip", bill.ID, reviewerID, bill.SessionID.String, fmt.Sprintf(`{"outcome":"%s"}`, outcome)); err != nil {
		slog.Warn("记录委员会结果事件失败", "bill_id", bill.ID, "err", err)
	}

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
	if err := w.db.CreateHansard(h); err != nil {
		slog.Warn("pollReviewFile: create hansard", "bill_id", bill.ID, "err", err)
	}

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
	if err := w.db.CreateGazette(g); err != nil {
		slog.Warn("pollReviewFile: create gazette", "bill_id", bill.ID, "err", err)
	}
}

// ─── Phase 2: ACK Protocol ───────────────────────────────────────────────────

// pollAckFiles checks for .ack files from downstream ministers.
// When a minister is in "briefing" status (waiting for downstream ACK),
// this polls for .ack files from downstream bills. If all downstream ACK,
// the minister is marked as idle.
func (w *Whip) pollAckFiles() {
	// Get ministers in briefing status (waiting for downstream ACK).
	ministers, err := w.db.ListMinistersWithStatus(store.MinisterStatusBriefing)
	if err != nil {
		slog.Warn("pollAckFiles: list briefing ministers", "err", err)
		return
	}

	for _, m := range ministers {
		worktree := m.Worktree.String
		if worktree == "" {
			continue
		}

		// Get bills assigned to this minister that are enacted.
		bills, err := w.db.GetBillsByAssignee(m.ID)
		if err != nil {
			continue
		}

		allAcked := true
		for _, bill := range bills {
			if bill.Status != "enacted" && bill.Status != "royal_assent" {
				continue
			}

			// Find downstream bills that depend on this bill.
			downstream, err := w.db.GetDownstreamBills(bill.ID)
			if err != nil || len(downstream) == 0 {
				continue // No downstream bills.
			}

			// Check if all downstream have ACK'd.
			for _, downBill := range downstream {
				ackPath := filepath.Join(worktree, ".hoc", fmt.Sprintf("bill-%s.ack", downBill.ID))
				if _, err := os.Stat(ackPath); os.IsNotExist(err) {
					allAcked = false
					break
				}
			}
		}

		if allAcked {
			// All downstream ACK'd, mark minister as idle.
			if err := w.db.UpdateMinisterStatus(m.ID, store.MinisterStatusIdle); err != nil {
				slog.Error("pollAckFiles: update minister status idle", "minister_id", m.ID, "err", err)
				continue
			}
			slog.Info("所有下游已 ACK，部长标记为 idle", "minister_id", m.ID)

			// Clean up .ack files.
			for _, bill := range bills {
				ackPath := filepath.Join(worktree, ".hoc", fmt.Sprintf("bill-%s.ack", bill.ID))
				_ = os.Remove(ackPath) // best-effort: 清理 ack 文件
			}
		}
	}
}

// ─── Phase 4: B-1.4 Question Time Metrics ────────────────────────────────────

// collectQuestionMetrics computes ACK rounds and briefing time for a bill's hansard.
func (w *Whip) collectQuestionMetrics(billID, ministerID string) {
	// Count question rounds.
	ackRounds, err := w.db.CountACKRoundsForBill(billID)
	if err != nil {
		slog.Debug("collectQuestionMetrics: count ACK rounds", "bill_id", billID, "err", err)
		return
	}

	// Compute briefing time from gazette timestamps.
	briefingTimeS := 0
	gazettes, err := w.db.ListGazettesForBill(billID)
	if err == nil && len(gazettes) >= 2 {
		// Briefing time = last gazette timestamp - first gazette timestamp.
		first := gazettes[len(gazettes)-1].CreatedAt
		last := gazettes[0].CreatedAt
		briefingTimeS = int(last.Sub(first).Seconds())
	}

	// Find the hansard record for this bill + minister.
	hansards, err := w.db.ListHansardByMinister(ministerID)
	if err != nil {
		return
	}
	for _, h := range hansards {
		if h.BillID == billID {
			if err := w.db.UpdateHansardMetrics(h.ID, ackRounds, briefingTimeS); err != nil {
				slog.Warn("collectQuestionMetrics: update hansard metrics", "hansard_id", h.ID, "err", err)
			}
			slog.Debug("已更新 Hansard 度量", "hansard_id", h.ID, "ack_rounds", ackRounds, "briefing_s", briefingTimeS)
			break
		}
	}
}
