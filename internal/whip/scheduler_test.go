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

