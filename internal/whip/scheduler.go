package whip

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/house-of-cards/hoc/internal/privy"
	"github.com/house-of-cards/hoc/internal/store"
)

// ─── Order Paper (DAG Engine) ────────────────────────────────────────────────

// orderPaper scans all active Sessions, finds Bills that are ready (all
// dependencies enacted), and auto-assigns them to idle Ministers with matching
// portfolio skills.
func (w *Whip) orderPaper() {
	sessions, err := w.db.ListActiveSessions()
	if err != nil {
		slog.Warn("orderPaper: list sessions", "err", err)
		return
	}

	for _, sess := range sessions {
		w.advanceSession(sess)
	}
}

// advanceSession checks a single Session for ready Bills.
func (w *Whip) advanceSession(sess *store.Session) {
	bills, err := w.db.ListBillsBySession(sess.ID)
	if err != nil {
		slog.Warn("advanceSession: list bills", "session_id", sess.ID, "err", err)
		return
	}

	// Build a status map for dependency resolution.
	statusMap := make(map[string]string, len(bills))
	for _, b := range bills {
		statusMap[b.ID] = b.Status
	}

	// Check if the session is fully complete.
	// Epic bills are considered done when all their sub-bills are done.
	allDone := true
	for _, b := range bills {
		if b.Status == "epic" {
			if !w.epicIsComplete(b.ID) {
				allDone = false
			}
			continue
		}
		if b.Status != "enacted" && b.Status != "royal_assent" && b.Status != "failed" {
			allDone = false
		}
	}
	if allDone && len(bills) > 0 {
		slog.Info("会期全部议案完成", "session_id", sess.ID, "title", sess.Title)
		w.privyAutoMerge(sess)
		return
	}

	for _, bill := range bills {
		if bill.Status != "draft" {
			continue // Only advance draft bills.
		}
		if bill.Assignee.String != "" {
			continue // Already assigned.
		}
		if !w.billIsReady(bill, statusMap, sess.EffectiveAckMode()) {
			continue // Dependencies not yet met.
		}

		// Bill is ready — find the best idle Minister by quality × load score.
		portfolio := bill.Portfolio.String
		minister, err := w.db.FindBestMinisterForSkill(portfolio)
		if err != nil || minister == nil {
			if portfolio != "" {
				slog.Info("议案就绪但无匹配 idle 部长", "bill_id", bill.ID, "portfolio", portfolio)
			}
			continue
		}

		w.autoAssign(bill, minister, sess)
	}
}

// privyAutoMerge is called when all bills in a session are done.
// If the session has a project and bills have branches, it triggers the Privy Council merge.
// On success: bills → royal_assent, session → completed, completion Gazette created.
// On conflict: Conflict Gazette created, session stays active for manual resolution.
func (w *Whip) privyAutoMerge(sess *store.Session) {
	project := sess.Project.String

	// Collect enacted bills with branches.
	branchBills, err := w.db.ListBillsWithBranchBySession(sess.ID)
	if err != nil {
		slog.Warn("privyAutoMerge: list branch bills", "session_id", sess.ID, "err", err)
	}

	// Build branch list for Privy Council.
	var billBranches []privy.BillBranch
	for _, b := range branchBills {
		if b.Branch.String != "" {
			billBranches = append(billBranches, privy.BillBranch{
				BillID: b.ID,
				Branch: b.Branch.String,
				Title:  b.Title,
			})
		}
	}

	// No branches or no project → just mark completed.
	if len(billBranches) == 0 || project == "" {
		slog.Info("会期标记 completed（无需合并）", "session_id", sess.ID)
		if err := w.db.UpdateSessionStatus(sess.ID, "completed"); err != nil {
			slog.Error("privyAutoMerge: update session status completed", "session_id", sess.ID, "err", err)
			return
		}
		if err := w.db.RecordEvent("session.completed", "whip", "", "", sess.ID, `{"merge":"skipped"}`); err != nil {
			slog.Warn("记录 session.completed 事件失败", "session_id", sess.ID, "err", err)
		}
		return
	}

	mainRepo := privy.MainRepoPath(w.hocDir, project)
	slog.Info("枢密院启动合并", "branches", len(billBranches), "repo", mainRepo)

	result, err := privy.MergeSession(mainRepo, billBranches, "")
	if err != nil {
		slog.Warn("privyAutoMerge: merge error", "session_id", sess.ID, "err", err)
		// Fallback: just mark completed.
		if err := w.db.UpdateSessionStatus(sess.ID, "completed"); err != nil {
			slog.Error("privyAutoMerge: fallback update session status", "session_id", sess.ID, "err", err)
		}
		return
	}

	if result.Success {
		// Royal Assent all merged bills.
		for _, bid := range result.MergedBills {
			if err := w.db.UpdateBillStatus(bid, "royal_assent"); err != nil {
				slog.Warn("privyAutoMerge: royal_assent bill", "bill_id", bid, "err", err)
			}
		}

		// Create a completion Gazette.
		summary := fmt.Sprintf(
			"枢密院公报：会期 \"%s\" 全部议案已合并。\n%s",
			sess.Title, result.Message,
		)
		g := &store.Gazette{
			ID:           gazetteID(),
			FromMinister: store.NullString("privy-council"),
			Type:         store.NullString("completion"),
			Summary:      summary,
		}
		if err := w.db.CreateGazette(g); err != nil {
			slog.Warn("privyAutoMerge: create gazette", "err", err)
		}

		slog.Info("枢密院合并成功，御准完成", "merge_branch", result.MergeBranch)
		if err := w.db.RecordEvent("privy.merge_success", "whip", "", "", sess.ID, fmt.Sprintf(`{"branch":"%s"}`, result.MergeBranch)); err != nil {
			slog.Warn("记录 privy.merge_success 事件失败", "session_id", sess.ID, "err", err)
		}
		if err := w.db.UpdateSessionStatus(sess.ID, "completed"); err != nil {
			slog.Error("privyAutoMerge: update session status completed", "session_id", sess.ID, "err", err)
			return
		}
		if err := w.db.RecordEvent("session.completed", "whip", "", "", sess.ID, `{"merge":"success"}`); err != nil {
			slog.Warn("记录 session.completed 事件失败", "session_id", sess.ID, "err", err)
		}
	} else {
		// Conflict — create a Conflict Gazette and leave session active.
		summary := fmt.Sprintf(
			"枢密院冲突公报：会期 \"%s\" 合并冲突。\n%s",
			sess.Title, result.Message,
		)
		g := &store.Gazette{
			ID:           gazetteID(),
			FromMinister: store.NullString("privy-council"),
			Type:         store.NullString("conflict"),
			Summary:      summary,
		}
		if err := w.db.CreateGazette(g); err != nil {
			slog.Warn("privyAutoMerge: create conflict gazette", "err", err)
		}
		slog.Warn("枢密院合并冲突，Conflict Gazette 已发布，待人工仲裁")
		if err := w.db.RecordEvent("privy.merge_conflict", "whip", "", "", sess.ID, ""); err != nil {
			slog.Warn("记录 privy.merge_conflict 事件失败", "session_id", sess.ID, "err", err)
		}
	}
}

// billIsReady returns true if all of bill's depends_on entries are enacted or royal_assent.
// When ackMode is "blocking", it additionally requires that each upstream dependency's
// completion gazette has been ACK'd (ack_status == "ack").
func (w *Whip) billIsReady(bill *store.Bill, statusMap map[string]string, ackMode string) bool {
	depsJSON := bill.DependsOn.String
	if depsJSON == "" || depsJSON == "[]" {
		return true
	}

	var deps []string
	if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
		return true // Malformed JSON — treat as no dependencies.
	}

	for _, dep := range deps {
		s, ok := statusMap[dep]
		if !ok {
			return false // Dependency bill not found.
		}
		if s != "enacted" && s != "royal_assent" {
			return false
		}
	}

	// In blocking mode, require ACK on upstream completion gazettes.
	if ackMode == store.AckModeBlocking {
		for _, dep := range deps {
			gazettes, err := w.db.ListGazettesForBill(dep)
			if err != nil {
				return false
			}
			// Find the completion gazette and check its ACK status.
			found := false
			for _, g := range gazettes {
				if g.Type.String == store.GazetteCompletion && g.AckStatus == store.AckStatusAcked {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// autoAssign assigns a ready Bill to an idle Minister at the database level
// and creates a handoff Gazette notifying the Minister.
//
// Phase 4C — Pipeline Gazette Injection:
// When the bill has upstream dependencies, the completion gazettes from those
// upstream bills are injected into the handoff summary. This enables pipeline
// topology where each minister knows exactly what the previous step produced.
func (w *Whip) autoAssign(bill *store.Bill, minister *store.Minister, _ *store.Session) {
	if err := w.db.AssignBill(bill.ID, minister.ID); err != nil {
		slog.Warn("autoAssign: assign bill", "bill_id", bill.ID, "minister_id", minister.ID, "err", err)
		return
	}
	if err := w.db.UpdateBillStatus(bill.ID, "reading"); err != nil {
		slog.Warn("autoAssign: update bill status", "err", err)
	}

	slog.Info("党鞭派单", "bill_id", bill.ID, "title", bill.Title, "minister", minister.Title)
	if err := w.db.RecordEvent("bill.assigned", "whip", bill.ID, minister.ID, bill.SessionID.String, fmt.Sprintf(`{"portfolio":"%s"}`, bill.Portfolio.String)); err != nil {
		slog.Warn("记录 bill.assigned 事件失败", "bill_id", bill.ID, "err", err)
	}

	// Phase 4C: Build upstream gazette section for pipeline topology.
	upstreamSection := w.buildUpstreamGazetteSection(bill)

	summary := fmt.Sprintf(
		"党鞭令：议案 [%s] \"%s\" 已就绪（依赖全部完成），自动分配给 %s。\n请运行：hoc minister summon %s --bill %s --project <project>%s",
		bill.ID, bill.Title, minister.Title, minister.ID, bill.ID, upstreamSection,
	)
	g := &store.Gazette{
		ID:           gazetteID(),
		ToMinister:   store.NullString(minister.ID),
		BillID:       store.NullString(bill.ID),
		Type:         store.NullString("handoff"),
		Summary:      summary,
		FromMinister: store.NullString("whip"),
	}
	if err := w.db.CreateGazette(g); err != nil {
		slog.Warn("autoAssign: create gazette", "bill_id", bill.ID, "err", err)
	}
}

// formatGazettePayload renders a gazette's structured payload as markdown.
// If the gazette has a valid JSON payload (DoneFilePayload), it renders contracts,
// artifacts, and assumptions. Otherwise, it falls back to g.Summary.
func formatGazettePayload(g *store.Gazette, depID string) string {
	if g.Payload == "" {
		return fmt.Sprintf("### 来自议案 [%s] 的公报（类型: %s）\n%s", depID, g.Type.String, g.Summary)
	}

	var p store.DoneFilePayload
	if err := json.Unmarshal([]byte(g.Payload), &p); err != nil {
		return fmt.Sprintf("### 来自议案 [%s] 的公报（类型: %s）\n%s", depID, g.Type.String, g.Summary)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### 来自议案 [%s] 的公报（类型: %s）\n", depID, g.Type.String))

	if p.Summary != "" {
		sb.WriteString(fmt.Sprintf("**摘要**: %s\n", p.Summary))
	}

	if len(p.Contracts) > 0 {
		sb.WriteString("**接口契约**:\n")
		for k, v := range p.Contracts {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
	}

	if len(p.Artifacts) > 0 {
		sb.WriteString("**产出物**:\n")
		for k, v := range p.Artifacts {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
	}

	if len(p.Assumptions) > 0 {
		sb.WriteString("**假设**:\n")
		for k, v := range p.Assumptions {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
	}

	// If payload had no meaningful content, fall back to summary.
	if p.Summary == "" && len(p.Contracts) == 0 && len(p.Artifacts) == 0 && len(p.Assumptions) == 0 {
		sb.WriteString(g.Summary)
	}

	return sb.String()
}

// buildUpstreamGazetteSection collects the most recent completion gazette from each
// upstream (depends_on) bill and formats them as a context section.
// Returns an empty string when the bill has no dependencies or none have gazettes.
func (w *Whip) buildUpstreamGazetteSection(bill *store.Bill) string {
	depsJSON := bill.DependsOn.String
	if depsJSON == "" || depsJSON == "[]" {
		return ""
	}

	var deps []string
	if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil || len(deps) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## 上游议案公报（Pipeline Context）")
	found := 0
	for _, depID := range deps {
		gazettes, err := w.db.ListGazettesForBill(depID)
		if err != nil || len(gazettes) == 0 {
			continue
		}
		// Use the most recent gazette (list is newest-first).
		g := gazettes[0]
		sb.WriteString("\n\n")
		sb.WriteString(formatGazettePayload(g, depID))
		found++
	}
	if found == 0 {
		return ""
	}
	return sb.String()
}

// epicIsComplete returns true if all sub-bills of the given epic bill are in a terminal state.
func (w *Whip) epicIsComplete(epicID string) bool {
	subs, err := w.db.ListSubBills(epicID)
	if err != nil || len(subs) == 0 {
		return false // No sub-bills means the epic is not yet split properly.
	}
	for _, sub := range subs {
		if sub.Status != "enacted" && sub.Status != "royal_assent" && sub.Status != "failed" {
			return false
		}
	}
	return true
}

// ─── Autoscale ───────────────────────────────────────────────────────────────

// autoscale dynamically adjusts the number of active Ministers based on workload.
//
// Scale-up: when pending bills > idle ministers * scaleUpThreshold,
// activate ministers from the reserve pool (offline with skills).
//
// Scale-down: when idle ministers > pending bills + scaleDownThreshold,
// mark excess idle ministers as offline.
//
// Thresholds are configurable via config.toml (whip.scale_up_threshold / scale_down_threshold).
func (w *Whip) autoscale() {
	// Get all ministers and their status.
	allMinisters, err := w.db.ListMinisters()
	if err != nil {
		slog.Debug("autoscale: 拉取部长列表失败", "err", err)
		return
	}

	// Count by status.
	var idle int
	var idleMinisters []*store.Minister
	for _, m := range allMinisters {
		if m.Status == "idle" {
			idle++
			idleMinisters = append(idleMinisters, m)
		}
	}

	// Count pending bills (draft or reading status, not assigned).
	allBills, err := w.db.ListBills()
	if err != nil {
		slog.Debug("autoscale: 拉取议案列表失败", "err", err)
		return
	}

	var pendingBills int
	for _, b := range allBills {
		if (b.Status == "draft" || b.Status == "reading") && b.Assignee.String == "" {
			pendingBills++
		}
	}

	upThresh := w.scaleUpThreshold()
	downThresh := w.scaleDownThreshold()

	// Scale up: pending bills > idle ministers * threshold
	if pendingBills > idle*upThresh && pendingBills > 0 {
		// Collect reserve pool: offline ministers with skills.
		reservePool, err := w.db.ListOfflineMinisters()
		if err != nil || len(reservePool) == 0 {
			slog.Debug("autoscale: 无可用预备池部长")
		} else {
			// Activate one minister from the reserve pool.
			m := reservePool[0]
			slog.Info("autoscale: 从预备池激活部长", "minister_id", m.ID, "pending_bills", pendingBills, "idle", idle)
			if err := w.db.UpdateMinisterStatus(m.ID, "idle"); err != nil {
				slog.Error("autoscale: update minister status idle", "minister_id", m.ID, "err", err)
			} else {
				if err := w.db.RecordEvent("autoscale.triggered", "whip", "", m.ID, "", fmt.Sprintf(`{"direction":"up","pending":%d,"idle":%d,"activated":"%s"}`, pendingBills, idle, m.ID)); err != nil {
					slog.Warn("记录 autoscale.triggered 事件失败", "minister_id", m.ID, "err", err)
				}

				g := &store.Gazette{
					ID:      gazetteID(),
					Type:    store.NullString("autoscale"),
					Summary: fmt.Sprintf("自动扩容：部长 [%s] %s 已从预备池激活（待处理 %d > 空闲 %d × %d）", m.ID, m.Title, pendingBills, idle, upThresh),
				}
				if err := w.db.CreateGazette(g); err != nil {
					slog.Warn("autoscale: create scale-up gazette", "err", err)
				}
			}
		}
	}

	// Scale down: idle ministers > pending bills + threshold
	if idle > pendingBills+downThresh && idle > downThresh {
		// Dismiss the oldest idle minister.
		if len(idleMinisters) > 0 {
			m := idleMinisters[0]
			slog.Info("autoscale: 准备缩容", "idle_ministers", idle, "pending_bills", pendingBills)
			if err := w.db.RecordEvent("autoscale.triggered", "whip", "", m.ID, "", fmt.Sprintf(`{"direction":"down","pending":%d,"idle":%d}`, pendingBills, idle)); err != nil {
				slog.Warn("记录 autoscale.triggered 事件失败", "minister_id", m.ID, "err", err)
			}

			if err := w.db.UpdateMinisterStatus(m.ID, "offline"); err != nil {
				slog.Error("autoscale: update minister status offline", "minister_id", m.ID, "err", err)
			} else {
				g := &store.Gazette{
					ID:      gazetteID(),
					Type:    store.NullString("autoscale"),
					Summary: fmt.Sprintf("自动缩容：部长 [%s] 已离线（空闲 %d > 待处理 %d + %d）", m.ID, idle, pendingBills, downThresh),
				}
				if err := w.db.CreateGazette(g); err != nil {
					slog.Warn("autoscale: create scale-down gazette", "err", err)
				}
			}
		}
	}
}
