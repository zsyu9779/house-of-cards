package store_test

import (
	"fmt"
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
)

// newTestDB creates an in-memory SQLite database for testing.
func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := store.NewDB(dir)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ─── Minister tests ───────────────────────────────────────────────────────────

func TestCreateGetMinister(t *testing.T) {
	db := newTestDB(t)

	m := &store.Minister{
		ID:      "backend-claude",
		Title:   "Minister of Backend",
		Runtime: "claude-code",
		Skills:  `["go","python"]`,
		Status:  "offline",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	got, err := db.GetMinister(m.ID)
	if err != nil {
		t.Fatalf("GetMinister: %v", err)
	}
	if got.ID != m.ID {
		t.Errorf("ID: got %q, want %q", got.ID, m.ID)
	}
	if got.Title != m.Title {
		t.Errorf("Title: got %q, want %q", got.Title, m.Title)
	}
	if got.Runtime != m.Runtime {
		t.Errorf("Runtime: got %q, want %q", got.Runtime, m.Runtime)
	}
	if got.Status != m.Status {
		t.Errorf("Status: got %q, want %q", got.Status, m.Status)
	}
}

func TestListIdleMinistersForSkill(t *testing.T) {
	db := newTestDB(t)

	ministers := []*store.Minister{
		{ID: "go-m", Title: "Go Minister", Runtime: "claude-code", Skills: `["go","sql"]`, Status: "idle"},
		{ID: "react-m", Title: "React Minister", Runtime: "claude-code", Skills: `["react","ts"]`, Status: "idle"},
		{ID: "offline-m", Title: "Offline Minister", Runtime: "claude-code", Skills: `["go"]`, Status: "offline"},
	}
	for _, m := range ministers {
		if err := db.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister %s: %v", m.ID, err)
		}
	}

	t.Run("exact match go", func(t *testing.T) {
		got, err := db.ListIdleMinistersForSkill("go")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "go-m" {
			t.Errorf("expected [go-m], got %v", ministerIDs(got))
		}
	})

	t.Run("exact match react", func(t *testing.T) {
		got, err := db.ListIdleMinistersForSkill("react")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "react-m" {
			t.Errorf("expected [react-m], got %v", ministerIDs(got))
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got, err := db.ListIdleMinistersForSkill("rust")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty, got %v", ministerIDs(got))
		}
	})

	t.Run("empty skill returns all idle", func(t *testing.T) {
		got, err := db.ListIdleMinistersForSkill("")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("expected 2 idle ministers, got %d: %v", len(got), ministerIDs(got))
		}
	})

	t.Run("offline not included", func(t *testing.T) {
		got, _ := db.ListIdleMinistersForSkill("")
		for _, m := range got {
			if m.ID == "offline-m" {
				t.Error("offline minister should not be in idle list")
			}
		}
	})
}

// ─── Bill tests ───────────────────────────────────────────────────────────────

func TestCreateGetBill(t *testing.T) {
	db := newTestDB(t)

	b := &store.Bill{
		ID:          "bill-test1",
		Title:       "Build Auth API",
		Status:      "draft",
		Description: store.NullString("Implement JWT endpoints"),
		Portfolio:   store.NullString("go"),
		DependsOn:   store.NullString(`["bill-dep1"]`),
	}
	if err := db.CreateBill(b); err != nil {
		t.Fatalf("CreateBill: %v", err)
	}

	got, err := db.GetBill(b.ID)
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if got.Title != b.Title {
		t.Errorf("Title: got %q, want %q", got.Title, b.Title)
	}
	if got.Portfolio.String != "go" {
		t.Errorf("Portfolio: got %q, want %q", got.Portfolio.String, "go")
	}
	if got.DependsOn.String != `["bill-dep1"]` {
		t.Errorf("DependsOn: got %q", got.DependsOn.String)
	}
}

func TestClearBillAssignment(t *testing.T) {
	db := newTestDB(t)

	m := &store.Minister{ID: "m1", Title: "M1", Runtime: "claude-code", Status: "idle"}
	_ = db.CreateMinister(m)

	b := &store.Bill{ID: "bill-clear", Title: "Test", Status: "reading"}
	_ = db.CreateBill(b)
	_ = db.AssignBill(b.ID, m.ID)
	_ = db.UpdateBillStatus(b.ID, "reading")

	// Verify assignment.
	got, _ := db.GetBill(b.ID)
	if got.Assignee.String != m.ID {
		t.Fatalf("expected assignee %q, got %q", m.ID, got.Assignee.String)
	}

	// Clear.
	if err := db.ClearBillAssignment(b.ID); err != nil {
		t.Fatalf("ClearBillAssignment: %v", err)
	}

	got, _ = db.GetBill(b.ID)
	if got.Assignee.String != "" {
		t.Errorf("assignee should be empty after clear, got %q", got.Assignee.String)
	}
	if got.Status != "draft" {
		t.Errorf("status should be draft after clear, got %q", got.Status)
	}
}

// ─── Hansard tests ────────────────────────────────────────────────────────────

func TestHansardSuccessRate(t *testing.T) {
	db := newTestDB(t)

	m := &store.Minister{ID: "m-rate", Title: "Rate Minister", Runtime: "claude-code", Status: "idle"}
	_ = db.CreateMinister(m)

	t.Run("zero records", func(t *testing.T) {
		enacted, total, err := db.HansardSuccessRate(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if enacted != 0 || total != 0 {
			t.Errorf("expected 0/0, got %d/%d", enacted, total)
		}
	})

	outcomes := []string{"enacted", "enacted", "failed"}
	for i, outcome := range outcomes {
		h := &store.Hansard{
			ID:         fmt.Sprintf("h-%d", i),
			MinisterID: m.ID,
			BillID:     fmt.Sprintf("bill-%d", i),
			Outcome:    store.NullString(outcome),
		}
		_ = db.CreateHansard(h)
	}

	t.Run("mixed outcomes", func(t *testing.T) {
		enacted, total, err := db.HansardSuccessRate(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Errorf("expected total=3, got %d", total)
		}
		if enacted != 2 {
			t.Errorf("expected enacted=2, got %d", enacted)
		}
	})
}

// ─── Session tests ────────────────────────────────────────────────────────────

func TestListActiveSessions(t *testing.T) {
	db := newTestDB(t)

	sessions := []*store.Session{
		{ID: "s1", Title: "Active Session", Topology: "parallel", Status: "active"},
		{ID: "s2", Title: "Completed Session", Topology: "parallel", Status: "completed"},
		{ID: "s3", Title: "Another Active", Topology: "parallel", Status: "active"},
	}
	for _, s := range sessions {
		_ = db.CreateSession(s)
	}

	active, err := db.ListActiveSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active sessions, got %d", len(active))
	}
	for _, s := range active {
		if s.Status != "active" {
			t.Errorf("non-active session in result: %s [%s]", s.ID, s.Status)
		}
	}
}

// ─── Phase 3B: Hook Queue Tests ───────────────────────────────────────────────

func TestHookQueuePushPopPeek(t *testing.T) {
	db := newTestDB(t)

	m := &store.Minister{ID: "backend", Title: "Backend", Runtime: "claude-code", Status: "idle"}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	// Initially empty.
	queue, err := db.PeekHook(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("expected empty hook queue, got %v", queue)
	}

	// Push two bill IDs.
	if err := db.PushHook(m.ID, "bill-001"); err != nil {
		t.Fatalf("PushHook(bill-001): %v", err)
	}
	if err := db.PushHook(m.ID, "bill-002"); err != nil {
		t.Fatalf("PushHook(bill-002): %v", err)
	}

	// Peek should show both, in order.
	queue, err = db.PeekHook(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 || queue[0] != "bill-001" || queue[1] != "bill-002" {
		t.Errorf("PeekHook: got %v, want [bill-001 bill-002]", queue)
	}

	// Duplicate push should be a no-op.
	if err := db.PushHook(m.ID, "bill-001"); err != nil {
		t.Fatal(err)
	}
	queue, _ = db.PeekHook(m.ID)
	if len(queue) != 2 {
		t.Errorf("duplicate push: expected 2 items, got %d", len(queue))
	}

	// Pop should return FIFO order.
	got, err := db.PopHook(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "bill-001" {
		t.Errorf("PopHook: got %q, want %q", got, "bill-001")
	}

	got2, _ := db.PopHook(m.ID)
	if got2 != "bill-002" {
		t.Errorf("PopHook #2: got %q, want %q", got2, "bill-002")
	}

	// Empty after all pops.
	got3, err := db.PopHook(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got3 != "" {
		t.Errorf("empty pop: expected empty string, got %q", got3)
	}
}

func TestFindLeastLoadedMinister(t *testing.T) {
	db := newTestDB(t)

	// Create two idle ministers with matching skill.
	skills := `["go"]`
	m1 := &store.Minister{ID: "m1", Title: "M1", Runtime: "claude-code", Skills: skills, Status: "idle"}
	m2 := &store.Minister{ID: "m2", Title: "M2", Runtime: "claude-code", Skills: skills, Status: "idle"}
	_ = db.CreateMinister(m1)
	_ = db.CreateMinister(m2)

	// Assign a bill to m1 to give it higher load.
	b := &store.Bill{ID: "bill-x", Title: "Bill X", Status: "reading"}
	_ = db.CreateBill(b)
	_ = db.AssignBill("bill-x", "m1")

	best, err := db.FindLeastLoadedMinister("go")
	if err != nil {
		t.Fatal(err)
	}
	if best == nil {
		t.Fatal("expected a minister, got nil")
	}
	if best.ID != "m2" {
		t.Errorf("expected least-loaded m2, got %s", best.ID)
	}
}

// ─── Phase 4A: Quality Scoring Tests ─────────────────────────────────────────

func TestComputeBillQuality(t *testing.T) {
	tests := []struct {
		outcome string
		notes   string
		wantMin float64
		wantMax float64
	}{
		{"enacted", "done 文件检测，由 Whip 自动记录", 0.80, 0.90}, // enacted + no-by-election
		{"enacted", "委员会审查: PASS 很好", 0.90, 1.0},         // enacted + committee pass + no-by-election
		{"partial", "部分完成", 0.40, 0.50},                  // partial + no-by-election
		{"failed", "补选触发：心跳超时", 0.0, 0.01},               // failed with by-election → no bonus
		{"enacted", "补选触发：心跳超时，由 Whip 自动记录", 0.78, 0.82}, // enacted but by-election → 0.80 only
	}

	for _, tc := range tests {
		got := store.ComputeBillQuality(tc.outcome, tc.notes)
		if got < tc.wantMin || got > tc.wantMax {
			t.Errorf("ComputeBillQuality(%q, %q) = %.3f, want [%.2f, %.2f]",
				tc.outcome, tc.notes, got, tc.wantMin, tc.wantMax)
		}
	}
}

func TestGetMinisterAvgQuality(t *testing.T) {
	db := newTestDB(t)

	m := &store.Minister{ID: "m-quality", Title: "Quality M", Runtime: "claude-code", Status: "idle"}
	_ = db.CreateMinister(m)

	t.Run("no data returns 0.5", func(t *testing.T) {
		avg, err := db.GetMinisterAvgQuality(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if avg != 0.5 {
			t.Errorf("expected 0.5 for no data, got %.3f", avg)
		}
	})

	// Insert two hansard entries with quality values.
	h1 := &store.Hansard{ID: "hq1", MinisterID: m.ID, BillID: "b1", Quality: 0.85, Outcome: store.NullString("enacted")}
	h2 := &store.Hansard{ID: "hq2", MinisterID: m.ID, BillID: "b2", Quality: 0.75, Outcome: store.NullString("enacted")}
	_ = db.CreateHansard(h1)
	_ = db.CreateHansard(h2)

	t.Run("average of two entries", func(t *testing.T) {
		avg, err := db.GetMinisterAvgQuality(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := 0.80
		if avg < want-0.01 || avg > want+0.01 {
			t.Errorf("expected avg ~0.80, got %.3f", avg)
		}
	})
}

func TestFindBestMinisterForSkill(t *testing.T) {
	db := newTestDB(t)

	skills := `["go"]`
	m1 := &store.Minister{ID: "bm1", Title: "M1", Runtime: "claude-code", Skills: skills, Status: "idle"}
	m2 := &store.Minister{ID: "bm2", Title: "M2", Runtime: "claude-code", Skills: skills, Status: "idle"}
	_ = db.CreateMinister(m1)
	_ = db.CreateMinister(m2)

	// Give m1 higher quality history, m2 lower.
	_ = db.CreateHansard(&store.Hansard{ID: "h-bm1-1", MinisterID: "bm1", BillID: "bb1", Quality: 0.95, Outcome: store.NullString("enacted")})
	_ = db.CreateHansard(&store.Hansard{ID: "h-bm2-1", MinisterID: "bm2", BillID: "bb2", Quality: 0.50, Outcome: store.NullString("partial")})

	best, err := db.FindBestMinisterForSkill("go")
	if err != nil {
		t.Fatal(err)
	}
	if best == nil {
		t.Fatal("expected a minister, got nil")
	}
	if best.ID != "bm1" {
		t.Errorf("expected best quality minister bm1, got %s", best.ID)
	}
}

// ─── Phase 5A: Session Stats Tests ───────────────────────────────────────────

func TestGetSessionStats_Empty(t *testing.T) {
	db := newTestDB(t)

	sess := &store.Session{ID: "s-stats-empty", Title: "Stats Test", Topology: "parallel", Status: "active"}
	if err := db.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stats, err := db.GetSessionStats("s-stats-empty")
	if err != nil {
		t.Fatalf("GetSessionStats: %v", err)
	}

	if stats.TotalBills != 0 {
		t.Errorf("expected 0 bills, got %d", stats.TotalBills)
	}
	if stats.EnactedRate != 0 {
		t.Errorf("expected 0 enacted rate, got %f", stats.EnactedRate)
	}
	if len(stats.Ministers) != 0 {
		t.Errorf("expected no minister loads, got %d", len(stats.Ministers))
	}
}

func TestGetSessionStats_WithData(t *testing.T) {
	db := newTestDB(t)

	sess := &store.Session{ID: "s-stats-data", Title: "Data Stats", Topology: "pipeline", Status: "active"}
	if err := db.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	m := &store.Minister{ID: "m-stats", Title: "Stats Minister", Runtime: "claude-code", Skills: `["go"]`, Status: "idle"}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	// Create 3 bills: 2 enacted, 1 draft.
	for i, status := range []string{"enacted", "enacted", "draft"} {
		billID := fmt.Sprintf("b-stats-%d", i)
		b := &store.Bill{
			ID:        billID,
			SessionID: store.NullString("s-stats-data"),
			Title:     fmt.Sprintf("Bill %d", i),
			Status:    status,
			DependsOn: store.NullString("[]"),
		}
		if status != "draft" {
			b.Assignee = store.NullString("m-stats")
		}
		if err := db.CreateBill(b); err != nil {
			t.Fatalf("CreateBill: %v", err)
		}
		// Create hansard for enacted bills.
		if status == "enacted" {
			h := &store.Hansard{
				ID:         fmt.Sprintf("h-stats-%d", i),
				MinisterID: "m-stats",
				BillID:     billID,
				Outcome:    store.NullString("enacted"),
				Quality:    0.85,
				DurationS:  120,
			}
			if err := db.CreateHansard(h); err != nil {
				t.Fatalf("CreateHansard: %v", err)
			}
		}
	}

	stats, err := db.GetSessionStats("s-stats-data")
	if err != nil {
		t.Fatalf("GetSessionStats: %v", err)
	}

	if stats.TotalBills != 3 {
		t.Errorf("expected 3 total bills, got %d", stats.TotalBills)
	}
	if stats.ByStatus["enacted"] != 2 {
		t.Errorf("expected 2 enacted, got %d", stats.ByStatus["enacted"])
	}
	if stats.ByStatus["draft"] != 1 {
		t.Errorf("expected 1 draft, got %d", stats.ByStatus["draft"])
	}

	wantRate := 2.0 / 3.0
	if stats.EnactedRate < wantRate-0.01 || stats.EnactedRate > wantRate+0.01 {
		t.Errorf("enacted rate: got %.3f, want ~%.3f", stats.EnactedRate, wantRate)
	}
	if stats.AvgQuality < 0.84 || stats.AvgQuality > 0.86 {
		t.Errorf("avg quality: got %.3f, want ~0.85", stats.AvgQuality)
	}
	if stats.TotalDurS != 240 {
		t.Errorf("total duration: got %d, want 240", stats.TotalDurS)
	}

	if len(stats.Ministers) != 1 {
		t.Fatalf("expected 1 minister load, got %d", len(stats.Ministers))
	}
	ml := stats.Ministers[0]
	if ml.ID != "m-stats" {
		t.Errorf("minister ID: got %q, want m-stats", ml.ID)
	}
	if ml.Bills != 2 {
		t.Errorf("minister bills: got %d, want 2", ml.Bills)
	}
	if ml.Enacted != 2 {
		t.Errorf("minister enacted: got %d, want 2", ml.Enacted)
	}
}

// ─── Whip-related store method tests ─────────────────────────────────────────

func TestUpdateMinisterStatus(t *testing.T) {
	db := newTestDB(t)

	m := &store.Minister{ID: "whip-m1", Title: "Whip Test", Runtime: "claude-code", Skills: `[]`, Status: "idle"}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	if err := db.UpdateMinisterStatus("whip-m1", "working"); err != nil {
		t.Fatalf("UpdateMinisterStatus: %v", err)
	}

	got, err := db.GetMinister("whip-m1")
	if err != nil {
		t.Fatalf("GetMinister: %v", err)
	}
	if got.Status != "working" {
		t.Errorf("status: got %q, want %q", got.Status, "working")
	}
}

func TestUpdateMinisterHeartbeat(t *testing.T) {
	db := newTestDB(t)

	m := &store.Minister{ID: "whip-m2", Title: "Heartbeat Test", Runtime: "claude-code", Skills: `[]`, Status: "working"}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	if err := db.UpdateMinisterHeartbeat("whip-m2"); err != nil {
		t.Fatalf("UpdateMinisterHeartbeat: %v", err)
	}

	got, err := db.GetMinister("whip-m2")
	if err != nil {
		t.Fatalf("GetMinister: %v", err)
	}
	if !got.Heartbeat.Valid {
		t.Error("expected heartbeat to be set after UpdateMinisterHeartbeat")
	}
}

func TestListWorkingMinisters(t *testing.T) {
	db := newTestDB(t)

	ministers := []*store.Minister{
		{ID: "lw-idle", Title: "Idle One", Runtime: "claude-code", Skills: `[]`, Status: "idle"},
		{ID: "lw-working1", Title: "Working One", Runtime: "claude-code", Skills: `[]`, Status: "working"},
		{ID: "lw-working2", Title: "Working Two", Runtime: "claude-code", Skills: `[]`, Status: "working"},
		{ID: "lw-stuck", Title: "Stuck One", Runtime: "claude-code", Skills: `[]`, Status: "stuck"},
	}
	for _, m := range ministers {
		if err := db.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister %s: %v", m.ID, err)
		}
	}

	got, err := db.ListWorkingMinisters()
	if err != nil {
		t.Fatalf("ListWorkingMinisters: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 working ministers, got %d", len(got))
	}
	for _, m := range got {
		if m.Status != "working" {
			t.Errorf("unexpected status %q for minister %s", m.Status, m.ID)
		}
	}
}

func TestListMinistersWithStatus(t *testing.T) {
	db := newTestDB(t)

	ministers := []*store.Minister{
		{ID: "ls-idle1", Title: "Idle One", Runtime: "claude-code", Skills: `[]`, Status: "idle"},
		{ID: "ls-idle2", Title: "Idle Two", Runtime: "claude-code", Skills: `[]`, Status: "idle"},
		{ID: "ls-stuck", Title: "Stuck One", Runtime: "claude-code", Skills: `[]`, Status: "stuck"},
	}
	for _, m := range ministers {
		if err := db.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister %s: %v", m.ID, err)
		}
	}

	// Query idle
	idle, err := db.ListMinistersWithStatus("idle")
	if err != nil {
		t.Fatalf("ListMinistersWithStatus(idle): %v", err)
	}
	if len(idle) != 2 {
		t.Errorf("expected 2 idle ministers, got %d", len(idle))
	}

	// Query stuck
	stuck, err := db.ListMinistersWithStatus("stuck")
	if err != nil {
		t.Fatalf("ListMinistersWithStatus(stuck): %v", err)
	}
	if len(stuck) != 1 || stuck[0].ID != "ls-stuck" {
		t.Errorf("expected 1 stuck minister, got %v", ministerIDs(stuck))
	}

	// Query status with no members
	offline, err := db.ListMinistersWithStatus("offline")
	if err != nil {
		t.Fatalf("ListMinistersWithStatus(offline): %v", err)
	}
	if len(offline) != 0 {
		t.Errorf("expected 0 offline ministers, got %d", len(offline))
	}
}

// ─── D-1: Event Ledger Tests ──────────────────────────────────────────────────

func TestRecordAndListEvents(t *testing.T) {
	db := newTestDB(t)

	// Record some events.
	if err := db.RecordEvent("bill.created", "speaker", "bill-001", "", "sess-1", `{"title":"Build API"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent("bill.assigned", "speaker", "bill-001", "m-backend", "sess-1", `{"assignee":"m-backend"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent("minister.idle", "whip", "", "m-backend", "", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	t.Run("list all events", func(t *testing.T) {
		events, err := db.ListEvents("", "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 3 {
			t.Errorf("expected 3 events, got %d", len(events))
		}
		// Should be newest first.
		if len(events) >= 2 && events[0].Timestamp.Before(events[1].Timestamp) {
			t.Error("events should be ordered newest first")
		}
	})

	t.Run("filter by topic", func(t *testing.T) {
		events, err := db.ListEvents("bill.created", "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 {
			t.Errorf("expected 1 event with topic bill.created, got %d", len(events))
		}
		if len(events) > 0 && events[0].Topic != "bill.created" {
			t.Errorf("topic: got %q, want %q", events[0].Topic, "bill.created")
		}
	})

	t.Run("filter by bill_id", func(t *testing.T) {
		events, err := db.ListEvents("", "bill-001", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events for bill-001, got %d", len(events))
		}
	})

	t.Run("filter by minister_id", func(t *testing.T) {
		events, err := db.ListEvents("", "", "m-backend", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events for m-backend, got %d", len(events))
		}
	})

	t.Run("filter by topic and bill_id combined", func(t *testing.T) {
		events, err := db.ListEvents("bill.assigned", "bill-001", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 {
			t.Errorf("expected 1 event, got %d", len(events))
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		events, err := db.ListEvents("nonexistent.topic", "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Errorf("expected 0 events, got %d", len(events))
		}
	})
}

func TestListEventsBySession(t *testing.T) {
	db := newTestDB(t)

	// Record events for two sessions.
	_ = db.RecordEvent("bill.created", "speaker", "bill-001", "", "sess-A", `{}`)
	_ = db.RecordEvent("bill.assigned", "speaker", "bill-001", "m1", "sess-A", `{}`)
	_ = db.RecordEvent("bill.created", "speaker", "bill-002", "", "sess-B", `{}`)

	t.Run("session A events", func(t *testing.T) {
		events, err := db.ListEventsBySession("sess-A")
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events for sess-A, got %d", len(events))
		}
		// Should be oldest first (timeline order).
		if len(events) >= 2 && events[0].Timestamp.After(events[1].Timestamp) {
			t.Error("timeline events should be ordered oldest first")
		}
	})

	t.Run("session B events", func(t *testing.T) {
		events, err := db.ListEventsBySession("sess-B")
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 {
			t.Errorf("expected 1 event for sess-B, got %d", len(events))
		}
	})

	t.Run("nonexistent session", func(t *testing.T) {
		events, err := db.ListEventsBySession("sess-NONE")
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Errorf("expected 0 events, got %d", len(events))
		}
	})
}

func TestRecordEventPayload(t *testing.T) {
	db := newTestDB(t)

	payload := `{"key":"value","nested":{"a":1}}`
	if err := db.RecordEvent("test.payload", "test", "", "", "", payload); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	events, err := db.ListEvents("test.payload", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Payload != payload {
		t.Errorf("payload: got %q, want %q", events[0].Payload, payload)
	}
	if events[0].Source != "test" {
		t.Errorf("source: got %q, want %q", events[0].Source, "test")
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func ministerIDs(ms []*store.Minister) []string {
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	return ids
}
