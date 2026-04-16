package whip

import (
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
)

// ─── threeLineWhip tests ─────────────────────────────────────────────────────

// TestThreeLineWhip_WorkingNotAlive_BeyondGrace covers lines 30-59:
// minister with PID=0 and no tmux session → isMinisterAlive=false;
// heartbeat beyond 30s gracePeriod → mark stuck + record event.
func TestThreeLineWhip_WorkingNotAlive_BeyondGrace(t *testing.T) {
	w, db := newTestWhip(t)

	// Create a "working" minister with stale heartbeat (PID=0, no tmux → isMinisterAlive returns false).
	m := &store.Minister{
		ID:      "m-grace",
		Title:   "Grace Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "working",
		Pid:     0, // no real process
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	// Set heartbeat to 2 minutes ago — beyond 30s gracePeriod.
	if err := db.UpdateMinisterHeartbeat("m-grace"); err != nil {
		t.Fatalf("UpdateMinisterHeartbeat: %v", err)
	}
	// Directly set the heartbeat to the past via a raw update.
	_, err := db.DB().Exec(`UPDATE ministers SET heartbeat = datetime('now', '-2 minutes') WHERE id = 'm-grace'`)
	if err != nil {
		t.Fatalf("set stale heartbeat: %v", err)
	}

	w.threeLineWhip()

	// Minister should now be stuck.
	m2, _ := db.GetMinister("m-grace")
	if m2.Status != "stuck" {
		t.Errorf("minister should be stuck, got %q", m2.Status)
	}
}

// TestThreeLineWhip_WorkingNotAlive_WithinGrace covers lines 47-49:
// heartbeat within grace period → minister stays working.
func TestThreeLineWhip_WorkingNotAlive_WithinGrace(t *testing.T) {
	w, db := newTestWhip(t)

	m := &store.Minister{
		ID:      "m-within",
		Title:   "Within Grace Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "working",
		Pid:     0,
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	// Heartbeat 10 seconds ago — within 30s grace.
	_, err := db.DB().Exec(`UPDATE ministers SET heartbeat = datetime('now', '-10 seconds') WHERE id = 'm-within'`)
	if err != nil {
		t.Fatalf("set heartbeat: %v", err)
	}

	w.threeLineWhip()

	m2, _ := db.GetMinister("m-within")
	if m2.Status != "working" {
		t.Errorf("minister should stay working within grace, got %q", m2.Status)
	}
}

// TestThreeLineWhip_StuckLevel1_Coverage checkpoint reminder path.
// A stuck minister with RecoveryAttempts=0 → attempt becomes 1 after Increment,
// Level 1: create checkpoint recovery gazette.
func TestThreeLineWhip_StuckLevel1(t *testing.T) {
	w, db := newTestWhip(t)

	m := &store.Minister{
		ID:               "m-l1",
		Title:            "Level 1 Minister",
		Runtime:          "claude-code",
		Skills:           `[]`,
		Status:           "stuck",
		RecoveryAttempts: 0,
		Pid:              0,
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	// Heartbeat 6 minutes ago — beyond 5min stuckThreshold.
	_, err := db.DB().Exec(`UPDATE ministers SET heartbeat = datetime('now', '-6 minutes') WHERE id = 'm-l1'`)
	if err != nil {
		t.Fatalf("set stale heartbeat: %v", err)
	}

	w.threeLineWhip()

	// RecoveryAttempts should be incremented to 1.
	m2, _ := db.GetMinister("m-l1")
	if m2.RecoveryAttempts != 1 {
		t.Errorf("recovery attempts: got %d, want 1", m2.RecoveryAttempts)
	}

	// A "recovery" gazette should be created.
	gazettes, _ := db.ListGazettes()
	found := false
	for _, g := range gazettes {
		if g.Type.String == "recovery" && g.ToMinister.String == "m-l1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a recovery gazette for level-1 stuck minister")
	}
}

// TestThreeLineWhip_StuckLevel2 covers lines 96-116:
// RecoveryAttempts becomes 2 → at-risk event + warning gazette.
func TestThreeLineWhip_StuckLevel2(t *testing.T) {
	w, db := newTestWhip(t)

	m := &store.Minister{
		ID:      "m-l2",
		Title:   "Level 2 Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "stuck",
		Pid:     0,
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	// Set recovery_attempts=1 (CreateMinister doesn't insert this field).
	_, err := db.DB().Exec(`UPDATE ministers SET recovery_attempts = 1, heartbeat = datetime('now', '-6 minutes') WHERE id = 'm-l2'`)
	if err != nil {
		t.Fatalf("set stale heartbeat: %v", err)
	}

	// Assign a "reading" bill so level-2 can mark it at-risk.
	mustCreateSession(t, db, "sess-l2", "Level 2 Session")
	mustCreateBill(t, db, "bill-l2", "sess-l2", "At Risk Bill", "reading", "")
	if err := db.AssignBill("bill-l2", "m-l2"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	w.threeLineWhip()

	m2, _ := db.GetMinister("m-l2")
	if m2.RecoveryAttempts != 2 {
		t.Errorf("recovery attempts: got %d, want 2", m2.RecoveryAttempts)
	}

	// Level 2 creates a "recovery" gazette (warning).
	gazettes, _ := db.ListGazettes()
	found := false
	for _, g := range gazettes {
		if g.Type.String == "recovery" && g.ToMinister.String == "m-l2" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a recovery gazette for level-2 stuck minister")
	}
}

// TestThreeLineWhip_StuckLevel3 covers lines 118-126:
// RecoveryAttempts >= 2 → byElection is called.
func TestThreeLineWhip_StuckLevel3(t *testing.T) {
	w, db := newTestWhip(t)

	m := &store.Minister{
		ID:      "m-l3",
		Title:   "Level 3 Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "stuck",
		Pid:     0,
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	// Set recovery_attempts=2 so IncrementRecoveryAttempts → 3 → byElection.
	_, err := db.DB().Exec(`UPDATE ministers SET recovery_attempts = 2, heartbeat = datetime('now', '-6 minutes') WHERE id = 'm-l3'`)
	if err != nil {
		t.Fatalf("set stale heartbeat: %v", err)
	}

	mustCreateSession(t, db, "sess-l3", "Level 3 Session")
	mustCreateBill(t, db, "bill-l3", "sess-l3", "Will Reset Bill", "reading", "")
	if err := db.AssignBill("bill-l3", "m-l3"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	w.threeLineWhip()

	// byElection should have marked minister offline.
	m2, _ := db.GetMinister("m-l3")
	if m2.Status != "offline" {
		t.Errorf("minister should be offline after by-election, got %q", m2.Status)
	}

	// RecoveryAttempts should be reset to 0.
	if m2.RecoveryAttempts != 0 {
		t.Errorf("recovery attempts should be reset to 0, got %d", m2.RecoveryAttempts)
	}

	// Bill should be back to draft and unassigned.
	b, _ := db.GetBill("bill-l3")
	if b.Status != "draft" {
		t.Errorf("bill should be draft after by-election, got %q", b.Status)
	}
	if b.Assignee.String != "" {
		t.Errorf("bill assignee should be cleared, got %q", b.Assignee.String)
	}

	// Handoff gazette should be created.
	gazettes, _ := db.ListGazettes()
	found := false
	for _, g := range gazettes {
		if g.Type.String == "handoff" && g.BillID.String == "bill-l3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a handoff gazette after by-election")
	}
}

// ─── byElection tests ─────────────────────────────────────────────────────────

// TestByElection_ResetsAndOfflines covers byElection lines 135-233:
// No worktree → git stash skipped; reading bill → draft + handoff gazette + hansard + offline.
func TestByElection_ResetsAndOfflines(t *testing.T) {
	w, db := newTestWhip(t)

	// Minister with no worktree — git stash block is skipped.
	m := &store.Minister{
		ID:      "m-bye",
		Title:   "ByElection Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "stuck",
		Pid:     0,
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	mustCreateSession(t, db, "sess-bye", "ByElection Session")

	// Two bills: one "reading" (reset), one "enacted" (skipped).
	mustCreateBill(t, db, "bill-bye-reset", "sess-bye", "Will Reset", "reading", "")
	mustCreateBill(t, db, "bill-bye-skip", "sess-bye", "Will Skip", "enacted", "")

	if err := db.AssignBill("bill-bye-reset", "m-bye"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}
	if err := db.AssignBill("bill-bye-skip", "m-bye"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	w.byElection(m)

	// Reading bill reset to draft and unassigned.
	bReset, _ := db.GetBill("bill-bye-reset")
	if bReset.Status != "draft" {
		t.Errorf("reading bill should be draft, got %q", bReset.Status)
	}
	if bReset.Assignee.String != "" {
		t.Errorf("reading bill assignee should be cleared, got %q", bReset.Assignee.String)
	}

	// Enacted bill unchanged.
	bSkip, _ := db.GetBill("bill-bye-skip")
	if bSkip.Status != "enacted" {
		t.Errorf("enacted bill should stay enacted, got %q", bSkip.Status)
	}

	// Minister offline.
	m2, _ := db.GetMinister("m-bye")
	if m2.Status != "offline" {
		t.Errorf("minister should be offline, got %q", m2.Status)
	}

	// Handoff gazette created for reading bill.
	gazettes, _ := db.ListGazettes()
	handoffCount := 0
	for _, g := range gazettes {
		if g.Type.String == "handoff" {
			handoffCount++
		}
	}
	if handoffCount != 1 {
		t.Errorf("expected 1 handoff gazette, got %d", handoffCount)
	}

	// Hansard with outcome=failed created.
	hansards, _ := db.ListHansardByMinister("m-bye")
	found := false
	for _, h := range hansards {
		if h.BillID == "bill-bye-reset" && h.Outcome.String == "failed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected hansard entry with outcome=failed for bill-bye-reset")
	}
}

// TestByElection_WithWorktreeNoGit covers lines 152-168:
// Worktree dir exists but no git repo → stashRef stays empty, summary has no stash info.
func TestByElection_WithWorktreeNoGit(t *testing.T) {
	w, db := newTestWhip(t)

	worktree := t.TempDir() // real dir, but not a git repo

	m := &store.Minister{
		ID:      "m-wt",
		Title:   "Worktree No Git",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "stuck",
		Pid:     0,
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	if err := db.UpdateMinisterWorktree("m-wt", worktree); err != nil {
		t.Fatalf("UpdateMinisterWorktree: %v", err)
	}

	mustCreateSession(t, db, "sess-wt", "Worktree Session")
	mustCreateBill(t, db, "bill-wt", "sess-wt", "Bill In Worktree", "reading", "")
	if err := db.AssignBill("bill-wt", "m-wt"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	w.byElection(m)

	// Minister should still be offline.
	m2, _ := db.GetMinister("m-wt")
	if m2.Status != "offline" {
		t.Errorf("minister should be offline, got %q", m2.Status)
	}

	// Bill reset to draft.
	b, _ := db.GetBill("bill-wt")
	if b.Status != "draft" {
		t.Errorf("bill should be draft, got %q", b.Status)
	}

	// Handoff gazette should NOT mention stash info (no git repo).
	gazettes, _ := db.ListGazettes()
	for _, g := range gazettes {
		if g.Type.String == "handoff" && g.BillID.String == "bill-wt" {
			if contains(g.Summary, "stash") {
				t.Error("handoff gazette should not mention stash when no git repo")
			}
		}
	}
}

