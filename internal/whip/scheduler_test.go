package whip

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/store"
)

// ─── formatGazettePayload tests ─────────────────────────────────────────────

func TestFormatGazettePayload_EmptyPayload(t *testing.T) {
	g := &store.Gazette{
		Type:    store.NullString("completion"),
		Summary: "Bill completed successfully",
	}
	result := formatGazettePayload(g, "bill-1")
	if result == "" {
		t.Fatal("should not return empty string")
	}
	if want := "Bill completed successfully"; !contains(result, want) {
		t.Errorf("should contain summary %q, got:\n%s", want, result)
	}
	if !contains(result, "bill-1") {
		t.Error("should contain dep ID")
	}
}

func TestFormatGazettePayload_FullPayload(t *testing.T) {
	payload := store.DoneFilePayload{
		Summary:     "Implemented user auth",
		Contracts:   map[string]string{"auth.go": "AuthService interface"},
		Artifacts:   map[string]string{"internal/auth.go": "新增"},
		Assumptions: map[string]string{"api-version": "v2"},
	}
	pJSON, _ := json.Marshal(payload)
	g := &store.Gazette{
		Type:    store.NullString("completion"),
		Summary: "Raw summary",
		Payload: string(pJSON),
	}

	result := formatGazettePayload(g, "bill-auth")
	if !contains(result, "Implemented user auth") {
		t.Error("should contain payload summary")
	}
	if !contains(result, "AuthService interface") {
		t.Error("should contain contracts")
	}
	if !contains(result, "internal/auth.go") {
		t.Error("should contain artifact path")
	}
	if !contains(result, "v2") {
		t.Error("should contain assumptions")
	}
	if contains(result, "Raw summary") {
		t.Error("should NOT fall back to raw summary when payload has content")
	}
}

func TestFormatGazettePayload_PartialPayload(t *testing.T) {
	payload := store.DoneFilePayload{
		Summary: "Only summary here",
	}
	pJSON, _ := json.Marshal(payload)
	g := &store.Gazette{
		Type:    store.NullString("completion"),
		Summary: "Fallback",
		Payload: string(pJSON),
	}

	result := formatGazettePayload(g, "bill-x")
	if !contains(result, "Only summary here") {
		t.Error("should contain payload summary")
	}
	if contains(result, "接口契约") {
		t.Error("should not render empty contracts section")
	}
}

func TestFormatGazettePayload_MalformedJSON(t *testing.T) {
	g := &store.Gazette{
		Type:    store.NullString("completion"),
		Summary: "Fallback summary",
		Payload: "not-json-at-all{{{",
	}

	result := formatGazettePayload(g, "bill-bad")
	if !contains(result, "Fallback summary") {
		t.Error("should fall back to summary on malformed JSON")
	}
}

func TestFormatGazettePayload_EmptyStructPayload(t *testing.T) {
	// All fields empty in the parsed struct
	payload := store.DoneFilePayload{}
	pJSON, _ := json.Marshal(payload)
	g := &store.Gazette{
		Type:    store.NullString("completion"),
		Summary: "Should use this",
		Payload: string(pJSON),
	}

	result := formatGazettePayload(g, "bill-empty")
	if !contains(result, "Should use this") {
		t.Error("should fall back to summary when all payload fields are empty")
	}
}

// ─── billIsReady blocking ACK tests ──────────────────────────────────────────

func TestBillIsReady_BlockingRequiresACK(t *testing.T) {
	w, db := newTestWhip(t)

	// Create upstream bill and enact it.
	mustCreateSession(t, db, "sess-ack", "ACK Test")
	mustCreateBill(t, db, "bill-up", "sess-ack", "Upstream", "enacted", "")
	mustCreateBill(t, db, "bill-down", "sess-ack", "Downstream", "draft", `["bill-up"]`)

	// Create a completion gazette WITHOUT ack.
	g := &store.Gazette{
		ID:      "gaz-completion-1",
		BillID:  store.NullString("bill-up"),
		Type:    store.NullString(store.GazetteCompletion),
		Summary: "Done",
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	statusMap := map[string]string{"bill-up": "enacted", "bill-down": "draft"}
	billDown, _ := db.GetBill("bill-down")

	// Non-blocking: should be ready (deps are enacted).
	if !w.billIsReady(billDown, statusMap, store.AckModeNonBlocking) {
		t.Error("non-blocking mode should be ready when deps are enacted")
	}

	// Blocking: should NOT be ready (no ACK on completion gazette).
	if w.billIsReady(billDown, statusMap, store.AckModeBlocking) {
		t.Error("blocking mode should NOT be ready without ACK on completion gazette")
	}
}

func TestBillIsReady_BlockingWithACK(t *testing.T) {
	w, db := newTestWhip(t)

	mustCreateSession(t, db, "sess-ack2", "ACK Test 2")
	mustCreateBill(t, db, "bill-up2", "sess-ack2", "Upstream", "enacted", "")
	mustCreateBill(t, db, "bill-down2", "sess-ack2", "Downstream", "draft", `["bill-up2"]`)

	// Create a completion gazette WITH ack.
	g := &store.Gazette{
		ID:        "gaz-completion-2",
		BillID:    store.NullString("bill-up2"),
		Type:      store.NullString(store.GazetteCompletion),
		Summary:   "Done",
		AckStatus: store.AckStatusAcked,
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	statusMap := map[string]string{"bill-up2": "enacted", "bill-down2": "draft"}
	billDown, _ := db.GetBill("bill-down2")

	// Blocking: should be ready (ACK present).
	if !w.billIsReady(billDown, statusMap, store.AckModeBlocking) {
		t.Error("blocking mode should be ready when completion gazette is ACK'd")
	}
}

// ─── epicIsComplete tests ───────────────────────────────────────────────────

func TestEpicIsComplete_AllTerminal(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-epic", "Epic Session")
	mustCreateBillFull(t, db, "epic-1", "sess-epic", "Epic", "epic", "", "", "")
	mustCreateBillFull(t, db, "sub-1", "sess-epic", "Sub 1", "enacted", "", "", "epic-1")
	mustCreateBillFull(t, db, "sub-2", "sess-epic", "Sub 2", "royal_assent", "", "", "epic-1")
	mustCreateBillFull(t, db, "sub-3", "sess-epic", "Sub 3", "failed", "", "", "epic-1")

	if !w.epicIsComplete("epic-1") {
		t.Error("epic should be complete when all sub-bills are terminal")
	}
}

func TestEpicIsComplete_SomeInProgress(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-epic2", "Epic Session 2")
	mustCreateBillFull(t, db, "epic-2", "sess-epic2", "Epic", "epic", "", "", "")
	mustCreateBillFull(t, db, "sub-2a", "sess-epic2", "Sub A", "enacted", "", "", "epic-2")
	mustCreateBillFull(t, db, "sub-2b", "sess-epic2", "Sub B", "reading", "", "", "epic-2")

	if w.epicIsComplete("epic-2") {
		t.Error("epic should NOT be complete when sub-bill is still reading")
	}
}

func TestEpicIsComplete_NoSubBills(t *testing.T) {
	w, _ := newTestWhip(t)
	if w.epicIsComplete("nonexistent-epic") {
		t.Error("epic with no sub-bills should not be considered complete")
	}
}

// ─── buildUpstreamGazetteSection tests ──────────────────────────────────────

func TestBuildUpstreamGazette_WithDeps(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-upstream", "Upstream Session")
	mustCreateBill(t, db, "bill-up-g", "sess-upstream", "Upstream", "enacted", "")

	g := &store.Gazette{
		ID:      "gaz-up-1",
		BillID:  store.NullString("bill-up-g"),
		Type:    store.NullString("completion"),
		Summary: "Upstream completed successfully",
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	bill := &store.Bill{
		ID:        "bill-down-g",
		DependsOn: store.NullString(`["bill-up-g"]`),
	}

	result := w.buildUpstreamGazetteSection(bill)
	if !contains(result, "上游议案公报") {
		t.Error("should contain upstream gazette header")
	}
	if !contains(result, "Upstream completed successfully") {
		t.Error("should contain upstream gazette summary")
	}
}

func TestBuildUpstreamGazette_NoDeps(t *testing.T) {
	w, _ := newTestWhip(t)
	bill := &store.Bill{
		ID:        "bill-no-deps",
		DependsOn: store.NullString(""),
	}
	result := w.buildUpstreamGazetteSection(bill)
	if result != "" {
		t.Errorf("should return empty string for bill without deps, got %q", result)
	}
}

// ─── autoAssign tests ───────────────────────────────────────────────────────

func TestAutoAssign_CreatesGazetteAndUpdates(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-auto", "Auto Assign Session")
	mustCreateBill(t, db, "bill-auto", "sess-auto", "Auto Bill", "draft", "")
	mustCreateIdleMinister(t, db, "m-auto")

	bill, _ := db.GetBill("bill-auto")
	minister, _ := db.GetMinister("m-auto")

	w.autoAssign(bill, minister, nil)

	b, _ := db.GetBill("bill-auto")
	if b.Status != "reading" {
		t.Errorf("bill should be reading, got %q", b.Status)
	}
	if b.Assignee.String != "m-auto" {
		t.Errorf("bill assignee should be m-auto, got %q", b.Assignee.String)
	}

	gazettes, _ := db.ListGazettes()
	found := false
	for _, g := range gazettes {
		if g.Type.String == "handoff" && g.BillID.String == "bill-auto" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a handoff gazette for auto-assigned bill")
	}
}

// ─── privyAutoMerge tests ───────────────────────────────────────────────────

func TestPrivyAutoMerge_NoBranches(t *testing.T) {
	w, db := newTestWhip(t)
	sess := mustCreateSession(t, db, "sess-merge", "Merge Session")
	mustCreateBill(t, db, "bill-merge-1", "sess-merge", "Merge Bill", "enacted", "")

	w.privyAutoMerge(sess)

	loaded, _ := db.GetSession("sess-merge")
	if loaded.Status != "completed" {
		t.Errorf("session should be completed, got %q", loaded.Status)
	}
}

// ─── autoscale tests ────────────────────────────────────────────────────────

func TestAutoscale_ScaleUp(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-scale", "Scale Session")

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("bill-scale-%d", i)
		mustCreateBill(t, db, id, "sess-scale", "Scale Bill", "draft", "")
	}

	// Offline minister with skills (reserve pool).
	m := &store.Minister{
		ID:      "m-reserve",
		Title:   "Reserve Minister",
		Runtime: "claude-code",
		Skills:  `["backend"]`,
		Status:  "offline",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	w.autoscale()

	m2, _ := db.GetMinister("m-reserve")
	if m2.Status != "idle" {
		t.Errorf("reserve minister should be idle after scale-up, got %q", m2.Status)
	}
}

func TestAutoscale_ScaleDown(t *testing.T) {
	w, db := newTestWhip(t)

	for i := 0; i < 4; i++ {
		mustCreateIdleMinister(t, db, fmt.Sprintf("m-idle-%d", i))
	}

	w.autoscale()

	offlineCount := 0
	ministers, _ := db.ListMinisters()
	for _, m := range ministers {
		if m.Status == "offline" {
			offlineCount++
		}
	}
	if offlineCount == 0 {
		t.Error("at least one idle minister should be marked offline during scale-down")
	}
}

func TestAutoscale_NoAction(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-bal", "Balanced Session")
	mustCreateBill(t, db, "bill-bal", "sess-bal", "Balanced Bill", "draft", "")
	mustCreateIdleMinister(t, db, "m-bal")

	w.autoscale()

	m, _ := db.GetMinister("m-bal")
	if m.Status != "idle" {
		t.Errorf("minister should stay idle in balanced state, got %q", m.Status)
	}
}

// ─── scaleThreshold tests ───────────────────────────────────────────────────

func TestScaleThresholds(t *testing.T) {
	t.Run("defaults without config", func(t *testing.T) {
		w := &Whip{}
		if got := w.scaleUpThreshold(); got != 2 {
			t.Errorf("scaleUpThreshold default: got %d, want 2", got)
		}
		if got := w.scaleDownThreshold(); got != 2 {
			t.Errorf("scaleDownThreshold default: got %d, want 2", got)
		}
	})

	t.Run("with config", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Whip.ScaleUpThreshold = 5
		cfg.Whip.ScaleDownThreshold = 3
		w := &Whip{cfg: cfg}
		if got := w.scaleUpThreshold(); got != 5 {
			t.Errorf("scaleUpThreshold with config: got %d, want 5", got)
		}
		if got := w.scaleDownThreshold(); got != 3 {
			t.Errorf("scaleDownThreshold with config: got %d, want 3", got)
		}
	})
}

// ─── autoscale pure-function tests ──────────────────────────────────────────

func TestShouldScaleUp(t *testing.T) {
	tests := []struct {
		name                     string
		pending, idle, threshold int
		want                     bool
	}{
		{"no pending", 0, 0, 2, false},
		{"under threshold", 2, 1, 2, false},       // 2 > 1*2? no, 2 > 2 = false
		{"exactly threshold", 4, 2, 2, false},     // 4 > 4 = false
		{"just over threshold", 5, 2, 2, true},    // 5 > 4
		{"zero idle with pending", 1, 0, 2, true}, // 1 > 0
		{"large backlog", 10, 1, 2, true},         // 10 > 2
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldScaleUp(tt.pending, tt.idle, tt.threshold); got != tt.want {
				t.Errorf("shouldScaleUp(%d, %d, %d) = %v, want %v",
					tt.pending, tt.idle, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestShouldScaleDown(t *testing.T) {
	tests := []struct {
		name                     string
		pending, idle, threshold int
		want                     bool
	}{
		{"balanced", 1, 1, 2, false},
		{"idle equal to threshold", 0, 2, 2, false}, // floor: never drain to 0
		{"idle over threshold no pending", 0, 3, 2, true},
		{"excess idle with pending", 1, 4, 2, true}, // 4 > 1+2 && 4 > 2
		{"pending absorbs slack", 3, 4, 2, false},   // 4 > 3+2? no
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldScaleDown(tt.pending, tt.idle, tt.threshold); got != tt.want {
				t.Errorf("shouldScaleDown(%d, %d, %d) = %v, want %v",
					tt.pending, tt.idle, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestListPendingBills(t *testing.T) {
	bills := []*store.Bill{
		{ID: "a", Status: "draft"},
		{ID: "b", Status: "reading"},
		{ID: "c", Status: "reading", Assignee: store.NullString("m1")}, // assigned → excluded
		{ID: "d", Status: "enacted"},                                   // done → excluded
		{ID: "e", Status: "draft"},
	}
	pending := listPendingBills(bills)
	if len(pending) != 3 {
		t.Fatalf("want 3 pending, got %d", len(pending))
	}
	ids := []string{pending[0].ID, pending[1].ID, pending[2].ID}
	want := []string{"a", "b", "e"}
	for i, id := range want {
		if ids[i] != id {
			t.Errorf("pending[%d]: got %q, want %q", i, ids[i], id)
		}
	}
}

func TestMatchBillToMinister_SkillMatch(t *testing.T) {
	bills := []*store.Bill{
		{ID: "backend-bill", Portfolio: store.NullString("backend")},
		{ID: "frontend-bill", Portfolio: store.NullString("frontend")},
	}
	ministers := []*store.Minister{
		{ID: "m-fe", Skills: `["frontend"]`},
		{ID: "m-be", Skills: `["backend","go"]`},
	}
	// First bill (backend) should match m-be even though m-fe comes first.
	bill, m := matchBillToMinister(bills, ministers)
	if bill == nil || bill.ID != "backend-bill" {
		t.Errorf("bill: got %+v, want backend-bill", bill)
	}
	if m == nil || m.ID != "m-be" {
		t.Errorf("minister: got %+v, want m-be", m)
	}
}

func TestMatchBillToMinister_EmptyPortfolioMatchesAny(t *testing.T) {
	bills := []*store.Bill{{ID: "any", Portfolio: store.NullString("")}}
	ministers := []*store.Minister{{ID: "first", Skills: `["frontend"]`}}
	_, m := matchBillToMinister(bills, ministers)
	if m == nil || m.ID != "first" {
		t.Errorf("empty portfolio should match first minister, got %+v", m)
	}
}

func TestMatchBillToMinister_NoMatch(t *testing.T) {
	bills := []*store.Bill{{ID: "be", Portfolio: store.NullString("backend")}}
	ministers := []*store.Minister{{ID: "m-fe", Skills: `["frontend"]`}}
	bill, m := matchBillToMinister(bills, ministers)
	if bill != nil || m != nil {
		t.Errorf("no match expected, got bill=%+v minister=%+v", bill, m)
	}
}

func TestHasSkill(t *testing.T) {
	tests := []struct {
		name      string
		skills    string
		portfolio string
		want      bool
	}{
		{"empty portfolio always matches", `["go"]`, "", true},
		{"empty skills no match", "", "go", false},
		{"direct match", `["go","sql"]`, "go", true},
		{"case-insensitive", `["Go"]`, "go", true},
		{"no match", `["rust"]`, "go", false},
		{"invalid json falls back to substring", `go,sql`, "go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasSkill(tt.skills, tt.portfolio); got != tt.want {
				t.Errorf("hasSkill(%q, %q) = %v, want %v", tt.skills, tt.portfolio, got, tt.want)
			}
		})
	}
}

// TestAutoscale_MaxPerTick verifies that a single tick only activates at most
// maxScaleUpPerTick ministers even when the backlog and reserve pool are huge.
func TestAutoscale_MaxPerTick(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-cap", "Cap Session")

	// Create a large backlog (10 pending bills) with no portfolio.
	for i := 0; i < 10; i++ {
		mustCreateBill(t, db, fmt.Sprintf("bill-cap-%d", i), "sess-cap", "Cap Bill", "draft", "")
	}

	// Create 5 offline reserve ministers.
	for i := 0; i < 5; i++ {
		m := &store.Minister{
			ID:      fmt.Sprintf("m-cap-%d", i),
			Title:   "Cap",
			Runtime: "claude-code",
			Skills:  `["backend"]`,
			Status:  "offline",
		}
		if err := db.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister: %v", err)
		}
	}

	w.autoscale()

	// With no project attached to the session, summonReserve falls back to the
	// offline→idle activation path — but it is still capped at maxScaleUpPerTick.
	activated := 0
	ms, _ := db.ListMinisters()
	for _, m := range ms {
		if m.Status == "idle" {
			activated++
		}
	}
	if activated != maxScaleUpPerTick {
		t.Errorf("activated ministers: got %d, want %d (maxScaleUpPerTick)", activated, maxScaleUpPerTick)
	}
}

// TestAutoscale_RespectsMaxMinisters verifies the MaxMinisters cap blocks further
// scale-up once the active count already hits the configured ceiling.
func TestAutoscale_RespectsMaxMinisters(t *testing.T) {
	w, db := newTestWhip(t)
	w.cfg = &config.Config{}
	w.cfg.Whip.MaxMinisters = 2 // Only 2 total active ministers allowed.

	mustCreateSession(t, db, "sess-max", "Max Session")
	for i := 0; i < 6; i++ {
		mustCreateBill(t, db, fmt.Sprintf("bill-max-%d", i), "sess-max", "Max Bill", "draft", "")
	}

	// 2 working ministers already — at the cap.
	for i := 0; i < 2; i++ {
		m := &store.Minister{
			ID:      fmt.Sprintf("m-working-%d", i),
			Title:   "W",
			Runtime: "claude-code",
			Skills:  `["backend"]`,
			Status:  "working",
		}
		if err := db.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister: %v", err)
		}
	}
	// 3 reserve ministers available, but cap should stop activation.
	for i := 0; i < 3; i++ {
		m := &store.Minister{
			ID:      fmt.Sprintf("m-reserve-%d", i),
			Title:   "R",
			Runtime: "claude-code",
			Skills:  `["backend"]`,
			Status:  "offline",
		}
		if err := db.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister: %v", err)
		}
	}

	w.autoscale()

	ms, _ := db.ListMinisters()
	active := 0
	for _, m := range ms {
		if m.Status == "working" || m.Status == "idle" {
			active++
		}
	}
	if active > 2 {
		t.Errorf("active ministers exceeded MaxMinisters cap: got %d, want ≤2", active)
	}
}

// ─── orderPaper tests ───────────────────────────────────────────────────────

func TestOrderPaper_DelegatesToAdvance(t *testing.T) {
	w, db := newTestWhip(t)
	mustCreateSession(t, db, "sess-op", "OrderPaper Session")
	mustCreateBill(t, db, "bill-op", "sess-op", "OP Bill", "draft", "")
	mustCreateIdleMinister(t, db, "m-op")

	w.orderPaper()

	b, _ := db.GetBill("bill-op")
	if b.Status != "reading" {
		t.Errorf("bill should be reading after orderPaper, got %q", b.Status)
	}
}

// ─── EffectiveAckMode tests ──────────────────────────────────────────────────

func TestEffectiveAckMode(t *testing.T) {
	tests := []struct {
		name     string
		topology string
		ackMode  string
		want     string
	}{
		{"pipeline auto", "pipeline", "auto", store.AckModeBlocking},
		{"pipeline empty", "pipeline", "", store.AckModeBlocking},
		{"parallel auto", "parallel", "auto", store.AckModeNonBlocking},
		{"parallel empty", "parallel", "", store.AckModeNonBlocking},
		{"explicit blocking", "parallel", "blocking", store.AckModeBlocking},
		{"explicit non-blocking", "pipeline", "non-blocking", store.AckModeNonBlocking},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store.Session{
				Topology: tt.topology,
				AckMode:  store.NullString(tt.ackMode),
			}
			got := s.EffectiveAckMode()
			if got != tt.want {
				t.Errorf("EffectiveAckMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
