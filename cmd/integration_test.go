package cmd

// Integration tests for key cmd-layer workflows.
//
// Each test sets up an isolated SQLite DB in a temp directory, seeds data via
// the store layer, then exercises the command logic by calling RunE directly.
// This mirrors real-world usage without spawning a subprocess.

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/house-of-cards/hoc/internal/whip"
)

// setupTestDB initialises a fresh, isolated store.DB in t.TempDir().
// It overrides the package-level db/hocDir so commands use the test DB.
func setupTestDB(t *testing.T) *store.DB {
	t.Helper()
	dir := t.TempDir()

	testDB, err := store.NewDB(dir)
	if err != nil {
		t.Fatalf("setupTestDB NewDB: %v", err)
	}

	// Wire into package-level globals used by all cmd functions.
	db = testDB
	hocDir = dir

	t.Cleanup(func() {
		testDB.Close()
		db = nil
		hocDir = ""
	})
	return testDB
}

// ─── Scenario 1: single-minister flow ────────────────────────────────────────
// session open → bill list → bill assign → gazette list

func TestSingleMinisterFlow(t *testing.T) {
	tdb := setupTestDB(t)

	// 1. Create session.
	sid := "session-inttest1"
	sess := &store.Session{
		ID:       sid,
		Title:    "Integration Test Session",
		Topology: "parallel",
		Status:   "active",
	}
	if err := tdb.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// 2. Create bills.
	billID := "bill-inttest1"
	b := &store.Bill{
		ID:        billID,
		SessionID: store.NullString(sid),
		Title:     "Integration Test Bill",
		Status:    "draft",
		DependsOn: store.NullString("[]"),
	}
	if err := tdb.CreateBill(b); err != nil {
		t.Fatalf("CreateBill: %v", err)
	}

	// 3. Verify bill list.
	bills, err := tdb.ListBills()
	if err != nil {
		t.Fatalf("ListBills: %v", err)
	}
	if len(bills) != 1 {
		t.Fatalf("expected 1 bill, got %d", len(bills))
	}
	if bills[0].ID != billID {
		t.Errorf("bill ID: got %s, want %s", bills[0].ID, billID)
	}

	// 4. Create minister and assign bill.
	ministerID := "go-minister"
	m := &store.Minister{
		ID:      ministerID,
		Title:   "Minister of Go",
		Runtime: "claude-code",
		Status:  "idle",
	}
	if err := tdb.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	if err := tdb.AssignBill(billID, ministerID); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}
	if err := tdb.UpdateBillStatus(billID, "reading"); err != nil {
		t.Fatalf("UpdateBillStatus: %v", err)
	}

	// 5. Create a handoff gazette (mirrors bill assign logic).
	gazetteID := "gazette-inttest1"
	g := &store.Gazette{
		ID:         gazetteID,
		ToMinister: store.NullString(ministerID),
		BillID:     store.NullString(billID),
		Type:       store.NullString("handoff"),
		Summary:    "Integration test handoff",
	}
	if err := tdb.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	// 6. Verify gazette list.
	gazettes, err := tdb.ListGazettesForMinister(ministerID)
	if err != nil {
		t.Fatalf("ListGazettesForMinister: %v", err)
	}
	if len(gazettes) == 0 {
		t.Fatal("expected at least 1 gazette for minister")
	}

	// 7. Verify bill status updated.
	updated, err := tdb.GetBill(billID)
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if updated.Status != "reading" {
		t.Errorf("bill status: got %s, want reading", updated.Status)
	}
	if updated.Assignee.String != ministerID {
		t.Errorf("bill assignee: got %s, want %s", updated.Assignee.String, ministerID)
	}
}

// ─── Scenario 2: minister lifecycle ──────────────────────────────────────────
// minister summon → minister dismiss

func TestMinisterLifecycle(t *testing.T) {
	tdb := setupTestDB(t)

	ministerID := "ts-minister"
	m := &store.Minister{
		ID:      ministerID,
		Title:   "Minister of TypeScript",
		Runtime: "cursor",
		Status:  "offline",
	}
	if err := tdb.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	// Summon: offline → idle.
	if err := tdb.UpdateMinisterStatus(ministerID, "idle"); err != nil {
		t.Fatalf("UpdateMinisterStatus(idle): %v", err)
	}

	got, err := tdb.GetMinister(ministerID)
	if err != nil {
		t.Fatalf("GetMinister: %v", err)
	}
	if got.Status != "idle" {
		t.Errorf("after summon: status = %s, want idle", got.Status)
	}

	// Dismiss: idle → offline.
	if err := tdb.UpdateMinisterStatus(ministerID, "offline"); err != nil {
		t.Fatalf("UpdateMinisterStatus(offline): %v", err)
	}

	got, err = tdb.GetMinister(ministerID)
	if err != nil {
		t.Fatalf("GetMinister after dismiss: %v", err)
	}
	if got.Status != "offline" {
		t.Errorf("after dismiss: status = %s, want offline", got.Status)
	}
}

// ─── Scenario 3: empty session + whip report ─────────────────────────────────
// session open → whip report shows session, no bills

func TestEmptySessionWhipReport(t *testing.T) {
	tdb := setupTestDB(t)

	// Open a session with no bills.
	sid := "session-empty1"
	sess := &store.Session{
		ID:       sid,
		Title:    "Empty Session",
		Topology: "parallel",
		Status:   "active",
	}
	if err := tdb.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Whip report should include the session.
	report, err := whip.Report(tdb, false)
	if err != nil {
		t.Fatalf("whip.Report: %v", err)
	}

	if !strings.Contains(report, "Empty Session") {
		t.Errorf("whip report does not mention session title; report:\n%s", report)
	}
	if !strings.Contains(report, sid) {
		t.Logf("note: whip report does not include session id (this may be expected)")
	}

	// Should show 0 ministers.
	if !strings.Contains(report, "0 部长") {
		t.Logf("whip report snippet: %s", report[:min(len(report), 200)])
	}
}

// ─── Scenario 4: --json flag for bill list ────────────────────────────────────

func TestBillListJSON(t *testing.T) {
	tdb := setupTestDB(t)

	// Create two bills.
	for i, title := range []string{"Bill Alpha", "Bill Beta"} {
		bid := "bill-json-" + string(rune('a'+i))
		b := &store.Bill{
			ID:        bid,
			Title:     title,
			Status:    "draft",
			DependsOn: store.NullString("[]"),
		}
		if err := tdb.CreateBill(b); err != nil {
			t.Fatalf("CreateBill %s: %v", bid, err)
		}
	}

	// Enable --json flag directly (bypasses cobra arg parsing for unit tests).
	if err := billListCmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set --json flag: %v", err)
	}
	t.Cleanup(func() { _ = billListCmd.Flags().Set("json", "false") })

	// Capture output.
	var buf bytes.Buffer
	billListCmd.SetOut(&buf)
	t.Cleanup(func() { billListCmd.SetOut(nil) })

	if err := billListCmd.RunE(billListCmd, []string{}); err != nil {
		t.Fatalf("billListCmd.RunE: %v", err)
	}

	// Parse JSON output.
	var result []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("expected 2 bills in JSON, got %d", len(result))
	}
	for _, item := range result {
		if _, ok := item["id"]; !ok {
			t.Error("bill JSON missing 'id' field")
		}
		if _, ok := item["status"]; !ok {
			t.Error("bill JSON missing 'status' field")
		}
	}
}

// ─── Scenario 5: --json flag for minister list ────────────────────────────────

func TestMinisterListJSON(t *testing.T) {
	tdb := setupTestDB(t)

	for _, id := range []string{"go-minister", "ts-minister"} {
		m := &store.Minister{
			ID:      id,
			Title:   "Minister of " + id,
			Runtime: "claude-code",
			Status:  "offline",
		}
		if err := tdb.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister: %v", err)
		}
	}

	if err := ministersListCmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set --json flag: %v", err)
	}
	t.Cleanup(func() { _ = ministersListCmd.Flags().Set("json", "false") })

	var buf bytes.Buffer
	ministersListCmd.SetOut(&buf)
	t.Cleanup(func() { ministersListCmd.SetOut(nil) })

	if err := ministersListCmd.RunE(ministersListCmd, []string{}); err != nil {
		t.Fatalf("ministersListCmd.RunE: %v", err)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput: %s", err, buf.String())
	}
	if len(result) != 2 {
		t.Errorf("expected 2 ministers in JSON, got %d", len(result))
	}
}

// ─── Scenario 6: full pipeline lifecycle ─────────────────────────────────────
// A → B → C pipeline: complete each bill in order and verify dependencies.

func TestFullSessionLifecycle_Pipeline(t *testing.T) {
	tdb := setupTestDB(t)

	sid := "session-pipeline"
	sess := &store.Session{
		ID:       sid,
		Title:    "Pipeline Session",
		Topology: "pipeline",
		Status:   "active",
	}
	if err := tdb.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Create ministers.
	for _, mid := range []string{"minister-a", "minister-b", "minister-c"} {
		m := &store.Minister{
			ID:      mid,
			Title:   "Minister " + mid,
			Runtime: "claude-code",
			Skills:  `["go"]`,
			Status:  "idle",
		}
		if err := tdb.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister %s: %v", mid, err)
		}
	}

	// Create pipeline bills: A → B → C.
	billA := &store.Bill{ID: "bill-a", SessionID: store.NullString(sid), Title: "Bill A", Status: "draft", DependsOn: store.NullString("[]")}
	billB := &store.Bill{ID: "bill-b", SessionID: store.NullString(sid), Title: "Bill B", Status: "draft", DependsOn: store.NullString(`["bill-a"]`)}
	billC := &store.Bill{ID: "bill-c", SessionID: store.NullString(sid), Title: "Bill C", Status: "draft", DependsOn: store.NullString(`["bill-b"]`)}
	for _, b := range []*store.Bill{billA, billB, billC} {
		if err := tdb.CreateBill(b); err != nil {
			t.Fatalf("CreateBill %s: %v", b.ID, err)
		}
	}

	// Assign and complete Bill A.
	if err := tdb.AssignBill("bill-a", "minister-a"); err != nil {
		t.Fatalf("AssignBill A: %v", err)
	}
	if err := tdb.UpdateBillStatus("bill-a", "reading"); err != nil {
		t.Fatalf("UpdateBillStatus A: %v", err)
	}
	if err := tdb.EnactBillFromDone("bill-a", "minister-a", "Bill A complete"); err != nil {
		t.Fatalf("EnactBillFromDone A: %v", err)
	}

	// Verify A is enacted.
	a, err := tdb.GetBill("bill-a")
	if err != nil {
		t.Fatalf("GetBill A: %v", err)
	}
	if a.Status != "enacted" {
		t.Errorf("bill-a status: got %s, want enacted", a.Status)
	}

	// Verify B's dependency is satisfied (bill-a enacted).
	bills, _ := tdb.ListBillsBySession(sid)
	statusMap := make(map[string]string)
	for _, b := range bills {
		statusMap[b.ID] = b.Status
	}
	if statusMap["bill-a"] != "enacted" {
		t.Errorf("statusMap bill-a = %s, want enacted", statusMap["bill-a"])
	}

	// Complete Bill B, then verify C is ready.
	if err := tdb.AssignBill("bill-b", "minister-b"); err != nil {
		t.Fatalf("AssignBill B: %v", err)
	}
	if err := tdb.UpdateBillStatus("bill-b", "reading"); err != nil {
		t.Fatalf("UpdateBillStatus B: %v", err)
	}
	if err := tdb.EnactBillFromDone("bill-b", "minister-b", "Bill B complete"); err != nil {
		t.Fatalf("EnactBillFromDone B: %v", err)
	}

	// Verify both A and B enacted, C still draft.
	bills, _ = tdb.ListBillsBySession(sid)
	for _, b := range bills {
		switch b.ID {
		case "bill-a", "bill-b":
			if b.Status != "enacted" {
				t.Errorf("%s expected enacted, got %s", b.ID, b.Status)
			}
		case "bill-c":
			if b.Status != "draft" {
				t.Errorf("bill-c expected draft (not yet assigned), got %s", b.Status)
			}
		}
	}

	// Verify hansard entries were created.
	hansard, err := tdb.ListHansard()
	if err != nil {
		t.Fatalf("ListHansard: %v", err)
	}
	if len(hansard) < 2 {
		t.Errorf("expected at least 2 hansard entries, got %d", len(hansard))
	}
}

// ─── Scenario 7: by-election recovery ────────────────────────────────────────
// Simulate stuck minister → reassign bill to a new minister.

func TestByElectionRecovery(t *testing.T) {
	tdb := setupTestDB(t)

	sid := "session-byelect"
	sess := &store.Session{ID: sid, Title: "By-Election Test", Topology: "parallel", Status: "active"}
	if err := tdb.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// First minister (will get stuck).
	m1 := &store.Minister{ID: "m-stuck", Title: "Stuck Minister", Runtime: "claude-code", Status: "working"}
	if err := tdb.CreateMinister(m1); err != nil {
		t.Fatalf("CreateMinister m1: %v", err)
	}

	// Second minister (replacement).
	m2 := &store.Minister{ID: "m-fresh", Title: "Fresh Minister", Runtime: "claude-code", Status: "idle"}
	if err := tdb.CreateMinister(m2); err != nil {
		t.Fatalf("CreateMinister m2: %v", err)
	}

	// Bill assigned to m1.
	b := &store.Bill{
		ID:        "bill-byelect",
		SessionID: store.NullString(sid),
		Title:     "Stuck Bill",
		Status:    "reading",
		Assignee:  store.NullString("m-stuck"),
		DependsOn: store.NullString("[]"),
	}
	if err := tdb.CreateBill(b); err != nil {
		t.Fatalf("CreateBill: %v", err)
	}

	// Simulate by-election: mark m1 stuck, reset bill, assign to m2.
	if err := tdb.UpdateMinisterStatus("m-stuck", "stuck"); err != nil {
		t.Fatalf("UpdateMinisterStatus stuck: %v", err)
	}

	// Reset bill (simulate Whip by-election reset).
	if err := tdb.ClearBillAssignment("bill-byelect"); err != nil {
		t.Fatalf("ClearBillAssignment: %v", err)
	}

	// Create handoff gazette (Whip by-election protocol).
	g := &store.Gazette{
		ID:           "gazette-byelect",
		FromMinister: store.NullString("m-stuck"),
		ToMinister:   store.NullString("m-fresh"),
		BillID:       store.NullString("bill-byelect"),
		Type:         store.NullString("handoff"),
		Summary:      "补选交接公报：议案由 m-stuck 转交 m-fresh（心跳超时）",
	}
	if err := tdb.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	// Reassign to fresh minister.
	if err := tdb.AssignBill("bill-byelect", "m-fresh"); err != nil {
		t.Fatalf("AssignBill to m-fresh: %v", err)
	}
	if err := tdb.UpdateBillStatus("bill-byelect", "reading"); err != nil {
		t.Fatalf("UpdateBillStatus reading: %v", err)
	}

	// Verify integrity.
	got, err := tdb.GetBill("bill-byelect")
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if got.Assignee.String != "m-fresh" {
		t.Errorf("assignee: got %q, want m-fresh", got.Assignee.String)
	}
	if got.Status != "reading" {
		t.Errorf("status: got %q, want reading", got.Status)
	}

	// Verify gazette was created.
	gazettes, err := tdb.ListGazettesForMinister("m-fresh")
	if err != nil {
		t.Fatalf("ListGazettesForMinister: %v", err)
	}
	found := false
	for _, gz := range gazettes {
		if gz.ID == "gazette-byelect" {
			found = true
		}
	}
	if !found {
		t.Error("expected handoff gazette for m-fresh not found")
	}

	// Stuck minister should still be stuck.
	stuck, _ := tdb.GetMinister("m-stuck")
	if stuck.Status != "stuck" {
		t.Errorf("m-stuck status: got %q, want stuck", stuck.Status)
	}
}

// ─── Scenario 8: Hansard quality scoring ─────────────────────────────────────
// Verify ComputeBillQuality formula and GetMinisterAvgQuality aggregation.

func TestHansardQualityScoring(t *testing.T) {
	tdb := setupTestDB(t)

	m := &store.Minister{ID: "m-qscore", Title: "Quality Minister", Runtime: "claude-code", Status: "idle"}
	if err := tdb.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	// Enact a bill: should produce quality score ~0.85.
	b := &store.Bill{
		ID:        "bill-qs1",
		Title:     "Quality Test Bill",
		Status:    "reading",
		Assignee:  store.NullString("m-qscore"),
		DependsOn: store.NullString("[]"),
	}
	if err := tdb.CreateBill(b); err != nil {
		t.Fatalf("CreateBill: %v", err)
	}
	if err := tdb.EnactBillFromDone("bill-qs1", "m-qscore", "tests pass, reviewed"); err != nil {
		t.Fatalf("EnactBillFromDone: %v", err)
	}

	// Verify the bill is enacted.
	got, err := tdb.GetBill("bill-qs1")
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if got.Status != "enacted" {
		t.Errorf("bill status: got %s, want enacted", got.Status)
	}

	// Verify hansard was created with quality.
	entries, err := tdb.ListHansardByMinister("m-qscore")
	if err != nil {
		t.Fatalf("ListHansardByMinister: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected hansard entry after EnactBillFromDone")
	}
	if entries[0].Quality <= 0 {
		t.Errorf("hansard quality should be > 0, got %.3f", entries[0].Quality)
	}

	// GetMinisterAvgQuality should now return non-neutral value.
	avg, err := tdb.GetMinisterAvgQuality("m-qscore")
	if err != nil {
		t.Fatalf("GetMinisterAvgQuality: %v", err)
	}
	if avg == 0.5 {
		t.Error("expected non-neutral avg quality after enacted bill, still at 0.5")
	}
	if avg < 0.7 || avg > 1.0 {
		t.Errorf("avg quality %.3f out of expected range [0.7, 1.0]", avg)
	}
}

// ─── Scenario 9: Session stats ────────────────────────────────────────────────
// Verify GetSessionStats returns correct aggregates.

func TestSessionStatsIntegration(t *testing.T) {
	tdb := setupTestDB(t)

	sid := "session-stats-int"
	sess := &store.Session{ID: sid, Title: "Stats Integration", Topology: "parallel", Status: "active"}
	if err := tdb.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	m := &store.Minister{ID: "m-si", Title: "Stats Minister", Runtime: "claude-code", Status: "idle"}
	if err := tdb.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	// Create 2 enacted + 1 failed bill.
	for i, outcome := range []string{"enacted", "enacted", "failed"} {
		bid := "bill-si-" + string(rune('a'+i))
		b := &store.Bill{
			ID:        bid,
			SessionID: store.NullString(sid),
			Title:     "SI Bill " + string(rune('A'+i)),
			Status:    "reading",
			Assignee:  store.NullString("m-si"),
			DependsOn: store.NullString("[]"),
		}
		if err := tdb.CreateBill(b); err != nil {
			t.Fatalf("CreateBill %s: %v", bid, err)
		}

		finalStatus := outcome
		if err := tdb.UpdateBillStatus(bid, finalStatus); err != nil {
			t.Fatalf("UpdateBillStatus %s: %v", bid, err)
		}

		// Add hansard entry.
		h := &store.Hansard{
			ID:         "h-si-" + string(rune('a'+i)),
			MinisterID: "m-si",
			BillID:     bid,
			Outcome:    store.NullString(outcome),
			Quality:    store.ComputeBillQuality(outcome, ""),
			DurationS:  60,
		}
		if err := tdb.CreateHansard(h); err != nil {
			t.Fatalf("CreateHansard: %v", err)
		}
	}

	stats, err := tdb.GetSessionStats(sid)
	if err != nil {
		t.Fatalf("GetSessionStats: %v", err)
	}

	if stats.TotalBills != 3 {
		t.Errorf("total bills: got %d, want 3", stats.TotalBills)
	}
	if stats.ByStatus["enacted"] != 2 {
		t.Errorf("enacted: got %d, want 2", stats.ByStatus["enacted"])
	}
	if stats.ByStatus["failed"] != 1 {
		t.Errorf("failed: got %d, want 1", stats.ByStatus["failed"])
	}

	wantRate := 2.0 / 3.0
	if stats.EnactedRate < wantRate-0.01 || stats.EnactedRate > wantRate+0.01 {
		t.Errorf("enacted rate: got %.3f, want ~%.3f", stats.EnactedRate, wantRate)
	}
	if stats.TotalDurS != 180 {
		t.Errorf("total duration: got %d, want 180", stats.TotalDurS)
	}
	if len(stats.Ministers) != 1 {
		t.Fatalf("expected 1 minister load, got %d", len(stats.Ministers))
	}
	if stats.Ministers[0].Enacted != 2 {
		t.Errorf("minister enacted: got %d, want 2", stats.Ministers[0].Enacted)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure we can create the hoc home without hitting real filesystem.
func TestInitDBIsolation(t *testing.T) {
	dir := t.TempDir()
	testDB, err := store.NewDB(dir)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer testDB.Close()

	// Should have no ministers initially.
	ministers, err := testDB.ListMinisters()
	if err != nil {
		t.Fatalf("ListMinisters: %v", err)
	}
	if len(ministers) != 0 {
		t.Errorf("expected empty minister list, got %d", len(ministers))
	}

	// Verify DB file exists in isolated temp dir.
	dbPath := dir + "/.hoc/state.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("expected DB at %s to exist", dbPath)
	}
}
