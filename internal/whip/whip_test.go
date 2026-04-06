package whip

import (
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
)

// newTestWhip creates a Whip bound to a real in-memory SQLite DB for integration tests.
func newTestWhip(t *testing.T) (*Whip, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.NewDB(dir)
	if err != nil {
		t.Fatalf("store.NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	w := New(db, dir)
	return w, db
}

// ─── DAG / billIsReady tests ──────────────────────────────────────────────────

// makeWhip creates a minimal Whip for unit-testing pure functions.
func makeWhip() *Whip {
	return &Whip{}
}

func TestBillIsReady_NoDeps(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b1",
		DependsOn: store.NullString(""),
	}
	statusMap := map[string]string{}
	if !w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("bill with no deps should always be ready")
	}
}

func TestBillIsReady_EmptyDepsArray(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b1",
		DependsOn: store.NullString("[]"),
	}
	if !w.billIsReady(bill, map[string]string{}, store.AckModeNonBlocking) {
		t.Error("bill with empty deps array should be ready")
	}
}

func TestBillIsReady_DepsEnacted(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b2",
		DependsOn: store.NullString(`["b1"]`),
	}
	statusMap := map[string]string{"b1": "enacted"}
	if !w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("bill should be ready when dep is enacted")
	}
}

func TestBillIsReady_DepsRoyalAssent(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b2",
		DependsOn: store.NullString(`["b1"]`),
	}
	statusMap := map[string]string{"b1": "royal_assent"}
	if !w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("bill should be ready when dep is royal_assent")
	}
}

func TestBillIsReady_DepsDraft(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b2",
		DependsOn: store.NullString(`["b1"]`),
	}
	statusMap := map[string]string{"b1": "draft"}
	if w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("bill should NOT be ready when dep is draft")
	}
}

func TestBillIsReady_MixedDeps(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b3",
		DependsOn: store.NullString(`["b1","b2"]`),
	}
	statusMap := map[string]string{
		"b1": "enacted",
		"b2": "reading", // not done yet
	}
	if w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("bill should NOT be ready when any dep is still in progress")
	}
}

func TestBillIsReady_AllDepsEnacted(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b3",
		DependsOn: store.NullString(`["b1","b2"]`),
	}
	statusMap := map[string]string{
		"b1": "enacted",
		"b2": "royal_assent",
	}
	if !w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("bill should be ready when all deps are enacted/royal_assent")
	}
}

func TestBillIsReady_UnknownDep(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b2",
		DependsOn: store.NullString(`["nonexistent"]`),
	}
	statusMap := map[string]string{}
	if w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("bill should NOT be ready when dep is not in statusMap")
	}
}

func TestBillIsReady_MalformedJSON(t *testing.T) {
	w := makeWhip()
	bill := &store.Bill{
		ID:        "b2",
		DependsOn: store.NullString(`not-json`),
	}
	// Malformed JSON → treated as no dependencies → ready.
	if !w.billIsReady(bill, map[string]string{}, store.AckModeNonBlocking) {
		t.Error("malformed JSON deps should be treated as no deps (ready)")
	}
}

// ─── advanceSession path tests ────────────────────────────────────────────────

// TestBillIsReady_ChainDependencies verifies multi-hop pipeline dependency checking.
func TestBillIsReady_ChainDependencies(t *testing.T) {
	w := makeWhip()

	billC := &store.Bill{
		ID:        "c",
		DependsOn: store.NullString(`["a","b"]`),
	}

	t.Run("both deps enacted", func(t *testing.T) {
		statusMap := map[string]string{"a": "enacted", "b": "enacted"}
		if !w.billIsReady(billC, statusMap, store.AckModeNonBlocking) {
			t.Error("should be ready when all deps enacted")
		}
	})

	t.Run("one dep in reading", func(t *testing.T) {
		statusMap := map[string]string{"a": "enacted", "b": "reading"}
		if w.billIsReady(billC, statusMap, store.AckModeNonBlocking) {
			t.Error("should NOT be ready when one dep is still reading")
		}
	})

	t.Run("one dep missing from map", func(t *testing.T) {
		statusMap := map[string]string{"a": "enacted"}
		if w.billIsReady(billC, statusMap, store.AckModeNonBlocking) {
			t.Error("should NOT be ready when a dep is missing from statusMap")
		}
	})
}

// TestBillIsReady_RoyalAssentCountsAsDone verifies royal_assent satisfies dependency.
func TestBillIsReady_RoyalAssentCountsAsDone(t *testing.T) {
	w := makeWhip()

	bill := &store.Bill{
		ID:        "downstream",
		DependsOn: store.NullString(`["upstream"]`),
	}
	statusMap := map[string]string{"upstream": "royal_assent"}
	if !w.billIsReady(bill, statusMap, store.AckModeNonBlocking) {
		t.Error("royal_assent should satisfy dependency (counts as done)")
	}
}

// ─── New() creation test ──────────────────────────────────────────────────────

func TestNew_CreatesWhip(t *testing.T) {
	w, _ := newTestWhip(t)
	if w == nil {
		t.Fatal("New should return a non-nil Whip")
	}
	if w.db == nil {
		t.Error("Whip.db should not be nil")
	}
}

// ─── advanceSession integration tests ────────────────────────────────────────

func mustCreateSession(t *testing.T, db *store.DB, id, title string) *store.Session {
	t.Helper()
	sess := &store.Session{
		ID:       id,
		Title:    title,
		Topology: "parallel",
		Status:   "active",
	}
	if err := db.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession %s: %v", id, err)
	}
	return sess
}

func mustCreateBill(t *testing.T, db *store.DB, id, sessionID, title, status, deps string) {
	t.Helper()
	b := &store.Bill{
		ID:        id,
		SessionID: store.NullString(sessionID),
		Title:     title,
		Status:    status,
		DependsOn: store.NullString(deps),
		Portfolio: store.NullString(""),
	}
	if err := db.CreateBill(b); err != nil {
		t.Fatalf("CreateBill %s: %v", id, err)
	}
}

func mustCreateIdleMinister(t *testing.T, db *store.DB, id string) {
	t.Helper()
	m := &store.Minister{
		ID:      id,
		Title:   "Test Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "idle",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister %s: %v", id, err)
	}
}

func TestAdvanceSession_NoBills(t *testing.T) {
	w, db := newTestWhip(t)
	sess := mustCreateSession(t, db, "sess-empty", "Empty Session")

	// Should not panic or return error with no bills.
	w.advanceSession(sess)

	// Session stays active.
	loaded, err := db.GetSession("sess-empty")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if loaded.Status != "active" {
		t.Errorf("empty session should stay active, got %q", loaded.Status)
	}
}

func TestAdvanceSession_BillReadyNoMinister(t *testing.T) {
	w, db := newTestWhip(t)
	sess := mustCreateSession(t, db, "sess-no-m", "No Minister")
	mustCreateBill(t, db, "bill-001", "sess-no-m", "Feature A", "draft", "")

	// No ministers available → bill stays draft.
	w.advanceSession(sess)

	bill, err := db.GetBill("bill-001")
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if bill.Status != "draft" {
		t.Errorf("bill should stay draft when no minister available, got %q", bill.Status)
	}
}

func TestAdvanceSession_BillReadyAssigned(t *testing.T) {
	w, db := newTestWhip(t)
	sess := mustCreateSession(t, db, "sess-assign", "Assign Test")
	mustCreateBill(t, db, "bill-assign", "sess-assign", "Feature B", "draft", "")
	mustCreateIdleMinister(t, db, "minister-alpha")

	w.advanceSession(sess)

	bill, err := db.GetBill("bill-assign")
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if bill.Status != "reading" {
		t.Errorf("bill should be 'reading' after assignment, got %q", bill.Status)
	}
	if bill.Assignee.String != "minister-alpha" {
		t.Errorf("bill.Assignee: got %q, want minister-alpha", bill.Assignee.String)
	}
}

func TestAdvanceSession_BillUnmetDeps_StaysDraft(t *testing.T) {
	w, db := newTestWhip(t)
	sess := mustCreateSession(t, db, "sess-deps", "Dep Test")
	// bill-a is draft (upstream)
	mustCreateBill(t, db, "bill-dep-a", "sess-deps", "Upstream", "draft", "")
	// bill-b depends on bill-a which is not yet enacted
	mustCreateBill(t, db, "bill-dep-b", "sess-deps", "Downstream", "draft", `["bill-dep-a"]`)
	mustCreateIdleMinister(t, db, "minister-beta")

	w.advanceSession(sess)

	billB, err := db.GetBill("bill-dep-b")
	if err != nil {
		t.Fatalf("GetBill bill-dep-b: %v", err)
	}
	// bill-dep-b should NOT be assigned because bill-dep-a is still draft.
	if billB.Status != "draft" {
		t.Errorf("downstream bill should stay draft when dep unmet, got %q", billB.Status)
	}
	if billB.Assignee.String != "" {
		t.Errorf("downstream bill should not be assigned, got %q", billB.Assignee.String)
	}
}

func TestAdvanceSession_AllDoneNoProject_MarksCompleted(t *testing.T) {
	w, db := newTestWhip(t)
	sess := mustCreateSession(t, db, "sess-done", "Done Session")
	// All bills already enacted, no project (no git merge needed)
	mustCreateBill(t, db, "bill-done-1", "sess-done", "Done A", "enacted", "")
	mustCreateBill(t, db, "bill-done-2", "sess-done", "Done B", "enacted", "")

	w.advanceSession(sess)

	loaded, err := db.GetSession("sess-done")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if loaded.Status != "completed" {
		t.Errorf("session with all bills done should be 'completed', got %q", loaded.Status)
	}
}

func TestAdvanceSession_AlreadyAssignedSkipped(t *testing.T) {
	w, db := newTestWhip(t)
	sess := mustCreateSession(t, db, "sess-skip", "Skip Test")
	// Create bill already assigned
	mustCreateIdleMinister(t, db, "minister-gamma")
	mustCreateBill(t, db, "bill-skip", "sess-skip", "Already Assigned", "draft", "")
	if err := db.AssignBill("bill-skip", "minister-gamma"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	mustCreateIdleMinister(t, db, "minister-delta")

	w.advanceSession(sess)

	bill, err := db.GetBill("bill-skip")
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	// Assignee should remain "minister-gamma", not reassigned.
	if bill.Assignee.String != "minister-gamma" {
		t.Errorf("bill assignee should not change, got %q", bill.Assignee.String)
	}
}
