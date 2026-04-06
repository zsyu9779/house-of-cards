package whip

import (
	"encoding/json"
	"testing"

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

// helper.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
