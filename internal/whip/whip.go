// Package whip implements the Whip daemon — the system's driving force.
//
// The Whip runs a background tick loop every 10 seconds performing three duties:
//  1. Three-Line Whip  — heartbeat / liveness check on all working Ministers.
//  2. Order Paper       — DAG engine: find ready Bills and auto-assign to idle Ministers.
//  3. Gazette Dispatch  — mark unread Gazettes as delivered (logged).
//
// Every 60 seconds it also refreshes the Speaker context.md and logs a Hansard snapshot.
package whip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/house-of-cards/hoc/internal/speaker"
	"github.com/house-of-cards/hoc/internal/store"
)

const (
	tickInterval    = 10 * time.Second
	stuckThreshold  = 5 * time.Minute
	hansardInterval = 60 * time.Second
)

// Whip is the daemon that drives session progress.
type Whip struct {
	db     *store.DB
	hocDir string
	logger *log.Logger
}

// New returns a new Whip bound to the given database and hocDir.
// All output is written to w (pass os.Stdout for console output).
func New(db *store.DB, hocDir string, w io.Writer) *Whip {
	return &Whip{
		db:     db,
		hocDir: hocDir,
		logger: log.New(w, "[whip] ", log.Ltime),
	}
}

// Run starts the Whip main loop. It blocks until ctx is cancelled.
func (w *Whip) Run(ctx context.Context) {
	w.logger.Println("党鞭就位 (Whip is seated). 开始监控...")

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	hansardTicker := time.NewTicker(hansardInterval)
	defer hansardTicker.Stop()

	// Run an immediate tick on startup.
	w.tick()

	for {
		select {
		case <-ctx.Done():
			w.logger.Println("党鞭休会 (Whip dismissed).")
			return
		case <-ticker.C:
			w.tick()
		case <-hansardTicker.C:
			w.hansardUpdate()
		}
	}
}

// tick performs all 10-second duties.
func (w *Whip) tick() {
	w.threeLineWhip()
	w.orderPaper()
	w.gazetteDispatch()
}

// ─── Three-Line Whip ────────────────────────────────────────────────────────

// threeLineWhip checks liveness of all working Ministers.
// If a Minister is confirmed alive, its heartbeat is refreshed.
// If a Minister's process or tmux session has disappeared:
//   - Within grace period → skip (might be momentarily pausing)
//   - Beyond stuckThreshold → trigger By-election (补选)
func (w *Whip) threeLineWhip() {
	ministers, err := w.db.ListWorkingMinisters()
	if err != nil {
		w.logger.Printf("threeLineWhip: list ministers: %v", err)
		return
	}

	for _, m := range ministers {
		alive := w.isMinisterAlive(m)
		if alive {
			// Confirm liveness by refreshing heartbeat.
			_ = w.db.UpdateMinisterHeartbeat(m.ID)
			continue
		}

		// Process/session gone. Check how long since last heartbeat.
		if m.Heartbeat.Valid && time.Since(m.Heartbeat.Time) < stuckThreshold {
			// Within grace period — might be momentarily idle.
			continue
		}

		w.logger.Printf("⚠  部长 [%s] 无响应超时 → 触发补选 (By-election)", m.ID)
		w.byElection(m)
	}

	// Also check ministers already marked stuck — trigger by-election if not done yet.
	stuck, err := w.db.ListMinistersWithStatus("stuck")
	if err == nil {
		for _, m := range stuck {
			bills, _ := w.db.GetBillsByAssignee(m.ID)
			if len(bills) > 0 {
				w.logger.Printf("🔁 补选继续：stuck 部长 [%s] 仍有未完成议案", m.ID)
				w.byElection(m)
			}
		}
	}
}

// byElection executes the By-election (补选) procedure for a non-responsive Minister:
//  1. git stash in the minister's worktree (if any uncommitted work)
//  2. Generate a Handoff Gazette for continuity
//  3. Clear the minister's bill assignments (bill → draft)
//  4. Write a Hansard entry (outcome: "failed")
//  5. Mark minister as offline
func (w *Whip) byElection(m *store.Minister) {
	bills, err := w.db.GetBillsByAssignee(m.ID)
	if err != nil {
		w.logger.Printf("byElection[%s]: get bills: %v", m.ID, err)
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
					w.logger.Printf("   💾 stash 保存: %s in %s", stashMsg, worktree)
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
			w.logger.Printf("byElection: create gazette: %v", err)
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
			w.logger.Printf("byElection: create hansard: %v", err)
		}

		// Reset bill to draft so orderPaper() can re-assign it.
		if err := w.db.ClearBillAssignment(bill.ID); err != nil {
			w.logger.Printf("byElection: clear bill assignment: %v", err)
		}

		w.logger.Printf("   📄 议案 [%s] 已重置为 draft，等待重新派发", bill.ID)
	}

	// Mark minister as offline.
	if err := w.db.UpdateMinisterStatus(m.ID, "offline"); err != nil {
		w.logger.Printf("byElection: update minister status: %v", err)
	}

	w.logger.Printf("🗳  补选完成：[%s] → offline，议案重置待重派", m.ID)
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

// ─── Order Paper (DAG Engine) ────────────────────────────────────────────────

// orderPaper scans all active Sessions, finds Bills that are ready (all
// dependencies enacted), and auto-assigns them to idle Ministers with matching
// portfolio skills.
func (w *Whip) orderPaper() {
	sessions, err := w.db.ListActiveSessions()
	if err != nil {
		w.logger.Printf("orderPaper: list sessions: %v", err)
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
		w.logger.Printf("advanceSession[%s]: list bills: %v", sess.ID, err)
		return
	}

	// Build a status map for dependency resolution.
	statusMap := make(map[string]string, len(bills))
	for _, b := range bills {
		statusMap[b.ID] = b.Status
	}

	// Check if the session is fully complete.
	allDone := true
	for _, b := range bills {
		if b.Status != "enacted" && b.Status != "royal_assent" && b.Status != "failed" {
			allDone = false
		}
	}
	if allDone && len(bills) > 0 {
		w.logger.Printf("✅ 会期 [%s] \"%s\" 全部议案完成，标记 completed", sess.ID, sess.Title)
		_ = w.db.UpdateSessionStatus(sess.ID, "completed")
		return
	}

	for _, bill := range bills {
		if bill.Status != "draft" {
			continue // Only advance draft bills.
		}
		if bill.Assignee.String != "" {
			continue // Already assigned.
		}
		if !w.billIsReady(bill, statusMap) {
			continue // Dependencies not yet met.
		}

		// Bill is ready — find an idle Minister.
		portfolio := bill.Portfolio.String
		candidates, err := w.db.ListIdleMinistersForSkill(portfolio)
		if err != nil || len(candidates) == 0 {
			if portfolio != "" {
				w.logger.Printf("📋 议案 [%s] 就绪但无匹配 idle 部长 (portfolio: %s)", bill.ID, portfolio)
			}
			continue
		}

		minister := candidates[0] // Pick first match (simplest strategy).
		w.autoAssign(bill, minister, sess)
	}
}

// billIsReady returns true if all of bill's depends_on entries are enacted or royal_assent.
func (w *Whip) billIsReady(bill *store.Bill, statusMap map[string]string) bool {
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
	return true
}

// autoAssign assigns a ready Bill to an idle Minister at the database level
// and creates a handoff Gazette notifying the Minister.
func (w *Whip) autoAssign(bill *store.Bill, minister *store.Minister, sess *store.Session) {
	if err := w.db.AssignBill(bill.ID, minister.ID); err != nil {
		w.logger.Printf("autoAssign: assign bill %s to %s: %v", bill.ID, minister.ID, err)
		return
	}
	if err := w.db.UpdateBillStatus(bill.ID, "reading"); err != nil {
		w.logger.Printf("autoAssign: update bill status: %v", err)
	}

	w.logger.Printf("📋 党鞭派单: 议案 [%s] \"%s\" → %s", bill.ID, bill.Title, minister.Title)

	// Create an assignment Gazette.
	summary := fmt.Sprintf(
		"党鞭令：议案 [%s] \"%s\" 已就绪（依赖全部完成），自动分配给 %s。\n请运行：hoc minister summon %s --bill %s --project <project>",
		bill.ID, bill.Title, minister.Title, minister.ID, bill.ID,
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
		w.logger.Printf("autoAssign: create gazette: %v", err)
	}
}

// ─── Gazette Dispatch ────────────────────────────────────────────────────────

// gazetteDispatch routes unread Gazettes. In v1 it simply logs delivery and
// marks them read. In v2 this will send nudges to tmux sessions.
func (w *Whip) gazetteDispatch() {
	gazettes, err := w.db.ListUnreadGazettes()
	if err != nil {
		w.logger.Printf("gazetteDispatch: list: %v", err)
		return
	}

	for _, g := range gazettes {
		to := g.ToMinister.String
		if to == "" {
			to = "(broadcast)"
		}
		w.logger.Printf("📰 投递公报 [%s] → %s (%s)", g.Type.String, to, truncate(g.Summary, 60))
		if err := w.db.MarkGazetteRead(g.ID); err != nil {
			w.logger.Printf("gazetteDispatch: mark read %s: %v", g.ID, err)
		}
	}
}

// ─── Hansard Update ──────────────────────────────────────────────────────────

// hansardUpdate runs every 60 seconds: logs a status snapshot and refreshes
// the Speaker context.md.
func (w *Whip) hansardUpdate() {
	ministers, _ := w.db.ListMinisters()
	working, idle, stuck := 0, 0, 0
	for _, m := range ministers {
		switch m.Status {
		case "working":
			working++
		case "idle":
			idle++
		case "stuck":
			stuck++
		}
	}

	sessions, _ := w.db.ListActiveSessions()
	w.logger.Printf("📊 Hansard: 活跃会期=%d  工作中=%d  待命=%d  卡住=%d",
		len(sessions), working, idle, stuck)

	// Refresh Speaker context.md.
	if w.hocDir != "" {
		if content, err := speaker.GenerateContext(w.db); err == nil {
			ctxPath := filepath.Join(w.hocDir, ".hoc", "speaker", "context.md")
			dirPath := filepath.Dir(ctxPath)
			if mkErr := os.MkdirAll(dirPath, 0755); mkErr == nil {
				if writeErr := os.WriteFile(ctxPath, []byte(content), 0644); writeErr == nil {
					w.logger.Printf("📝 Speaker context.md 已更新")
				}
			}
		}
	}
}

// ─── Report ──────────────────────────────────────────────────────────────────

// Report returns a human-readable status string for `hoc whip report`.
func Report(db *store.DB) (string, error) {
	var sb strings.Builder

	sessions, err := db.ListActiveSessions()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}

	allSessions, err := db.ListSessions()
	if err != nil {
		return "", fmt.Errorf("list all sessions: %w", err)
	}

	ministers, err := db.ListMinisters()
	if err != nil {
		return "", fmt.Errorf("list ministers: %w", err)
	}

	gazettes, err := db.ListUnreadGazettes()
	if err != nil {
		return "", fmt.Errorf("list gazettes: %w", err)
	}

	// Header
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("  党鞭状态报告 (Whip Report)\n")
	sb.WriteString(fmt.Sprintf("  %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("═══════════════════════════════════════\n\n")

	// Sessions
	sb.WriteString(fmt.Sprintf("📋 会期 (Sessions): %d 活跃 / %d 总计\n", len(sessions), len(allSessions)))
	for _, s := range allSessions {
		icon := "🟢"
		if s.Status == "completed" {
			icon = "✅"
		} else if s.Status == "dissolved" {
			icon = "⚫"
		}
		sb.WriteString(fmt.Sprintf("  %s [%s] %s\n", icon, s.Status, s.Title))

		bills, _ := db.ListBillsBySession(s.ID)
		counts := map[string]int{}
		for _, b := range bills {
			counts[b.Status]++
		}
		sb.WriteString(fmt.Sprintf("     议案: draft=%d reading=%d enacted=%d failed=%d\n",
			counts["draft"], counts["reading"], counts["enacted"], counts["failed"]))
	}

	// Ministers
	sb.WriteString(fmt.Sprintf("\n🏛  内阁 (Cabinet): %d 部长\n", len(ministers)))
	for _, m := range ministers {
		icon := "⚪"
		switch m.Status {
		case "working":
			icon = "🟢"
		case "idle":
			icon = "🟡"
		case "stuck":
			icon = "🔴"
		}
		hbStr := "从未"
		if m.Heartbeat.Valid {
			ago := time.Since(m.Heartbeat.Time).Round(time.Second)
			hbStr = fmt.Sprintf("%s 前", ago)
		}
		enacted, total, _ := db.HansardSuccessRate(m.ID)
		rateStr := "-"
		if total > 0 {
			rateStr = fmt.Sprintf("%d/%d", enacted, total)
		}
		sb.WriteString(fmt.Sprintf("  %s %s [%s]  心跳: %s  Hansard: %s\n",
			icon, m.Title, m.Status, hbStr, rateStr))
	}

	// Unread Gazettes
	sb.WriteString(fmt.Sprintf("\n📰 未读公报: %d 份\n", len(gazettes)))
	for _, g := range gazettes {
		sb.WriteString(fmt.Sprintf("  [%s] → %s: %s\n",
			g.Type.String, orDash(g.ToMinister.String), truncate(g.Summary, 60)))
	}

	sb.WriteString("\n═══════════════════════════════════════\n")
	return sb.String(), nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func gazetteID() string {
	return fmt.Sprintf("gazette-%x", time.Now().UnixNano())
}
