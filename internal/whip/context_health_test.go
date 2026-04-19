package whip

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
)

// seedWorkingMinister inserts a working minister ready to be inspected by
// checkContextHealth.
func seedWorkingMinister(t *testing.T, db *store.DB, id string) *store.Minister {
	t.Helper()
	m := &store.Minister{
		ID:      id,
		Title:   "Ctx " + id,
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "working",
		Pid:     0,
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	return m
}

// recordContextHealth writes a gazette carrying a context_health payload.
func recordContextHealth(t *testing.T, db *store.DB, fromMinister string, used, limit, turns int) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"context_health": store.ContextHealth{
			TokensUsed:   used,
			TokensLimit:  limit,
			TurnsElapsed: turns,
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	g := &store.Gazette{
		ID:           "g-" + fromMinister + "-" + time.Now().Format("150405.000"),
		FromMinister: store.NullString(fromMinister),
		Type:         store.NullString("status"),
		Summary:      "ctx report",
		Payload:      string(body),
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}
}

// countRecoveryGazettesTo returns the number of recovery gazettes addressed to
// the given minister — used to assert the alert was (or wasn't) sent.
func countRecoveryGazettesTo(t *testing.T, db *store.DB, ministerID string) int {
	t.Helper()
	gs, err := db.ListGazettesForMinister(ministerID)
	if err != nil {
		t.Fatalf("ListGazettesForMinister: %v", err)
	}
	n := 0
	for _, g := range gs {
		if g.Type.String == "recovery" && g.ToMinister.String == ministerID {
			n++
		}
	}
	return n
}

func TestCheckContextHealth_BelowWarn_NoGazette(t *testing.T) {
	w, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-below")
	recordContextHealth(t, db, "m-below", 70_000, 100_000, 20)

	w.checkContextHealth()

	if n := countRecoveryGazettesTo(t, db, "m-below"); n != 0 {
		t.Errorf("expected no recovery gazette at 70%%, got %d", n)
	}
}

func TestCheckContextHealth_AtWarn_SendsReminder(t *testing.T) {
	w, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-warn")
	recordContextHealth(t, db, "m-warn", 82_000, 100_000, 40)

	w.checkContextHealth()

	gs, err := db.ListGazettesForMinister("m-warn")
	if err != nil {
		t.Fatalf("ListGazettesForMinister: %v", err)
	}
	var found *store.Gazette
	for _, g := range gs {
		if g.Type.String == "recovery" && g.ToMinister.String == "m-warn" {
			found = g
			break
		}
	}
	if found == nil {
		t.Fatalf("expected a recovery gazette, got none")
	}
	if !strings.Contains(found.Summary, "提醒") {
		t.Errorf("summary should mention 提醒, got %q", found.Summary)
	}
}

func TestCheckContextHealth_AtCritical_SendsUrgentAndRecordsEvent(t *testing.T) {
	w, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-crit")

	bill := &store.Bill{
		ID:       "bill-crit",
		Title:    "critical bill",
		Status:   "reading",
		Assignee: store.NullString("m-crit"),
	}
	if err := db.CreateBill(bill); err != nil {
		t.Fatalf("CreateBill: %v", err)
	}

	recordContextHealth(t, db, "m-crit", 95_000, 100_000, 80)

	w.checkContextHealth()

	gs, _ := db.ListGazettesForMinister("m-crit")
	var found *store.Gazette
	for _, g := range gs {
		if g.Type.String == "recovery" && g.ToMinister.String == "m-crit" {
			found = g
			break
		}
	}
	if found == nil {
		t.Fatalf("expected critical recovery gazette, got none")
	}
	if !strings.Contains(found.Summary, "紧急") {
		t.Errorf("summary should mention 紧急, got %q", found.Summary)
	}

	events, err := db.ListEvents("bill.context_critical", "", "", 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected a bill.context_critical event, got none")
	}
	if events[0].BillID.String != "bill-crit" {
		t.Errorf("event bill_id = %q, want bill-crit", events[0].BillID.String)
	}
}

func TestCheckContextHealth_Cooldown_NoDuplicate(t *testing.T) {
	w, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-cool")
	recordContextHealth(t, db, "m-cool", 85_000, 100_000, 30)

	w.checkContextHealth()
	w.checkContextHealth()
	w.checkContextHealth()

	if n := countRecoveryGazettesTo(t, db, "m-cool"); n != 1 {
		t.Errorf("expected exactly 1 recovery gazette within cooldown, got %d", n)
	}
}

func TestCheckContextHealth_CooldownExpires_SendsAgain(t *testing.T) {
	w, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-reset")
	recordContextHealth(t, db, "m-reset", 85_000, 100_000, 30)

	w.checkContextHealth()
	// Force the cooldown clock past the window so the next call re-alerts.
	w.lastContextAlert["m-reset"] = time.Now().Add(-2 * contextAlertCooldown)
	w.checkContextHealth()

	if n := countRecoveryGazettesTo(t, db, "m-reset"); n != 2 {
		t.Errorf("expected 2 recovery gazettes after cooldown expiry, got %d", n)
	}
}

func TestCheckContextHealth_NoPayload_Skips(t *testing.T) {
	w, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-nopayload")

	g := &store.Gazette{
		ID:           "g-plain",
		FromMinister: store.NullString("m-nopayload"),
		Type:         store.NullString("status"),
		Summary:      "no context payload here",
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	w.checkContextHealth()

	if n := countRecoveryGazettesTo(t, db, "m-nopayload"); n != 0 {
		t.Errorf("expected no recovery gazette when no payload, got %d", n)
	}
}

func TestCheckContextHealth_ZeroLimit_Skips(t *testing.T) {
	w, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-zero")
	recordContextHealth(t, db, "m-zero", 9999, 0, 5)

	w.checkContextHealth()

	if n := countRecoveryGazettesTo(t, db, "m-zero"); n != 0 {
		t.Errorf("expected no gazette when limit=0, got %d", n)
	}
}

func TestGetLatestContextHealth_ParsesPayload(t *testing.T) {
	_, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-parse")
	recordContextHealth(t, db, "m-parse", 12_345, 100_000, 7)

	h, err := db.GetLatestContextHealth("m-parse")
	if err != nil {
		t.Fatalf("GetLatestContextHealth: %v", err)
	}
	if h == nil {
		t.Fatal("expected ContextHealth, got nil")
	}
	if h.TokensUsed != 12_345 || h.TokensLimit != 100_000 || h.TurnsElapsed != 7 {
		t.Errorf("unexpected parse: %+v", h)
	}
}

func TestGetLatestContextHealth_NoRows_ReturnsNilNil(t *testing.T) {
	_, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-empty")

	h, err := db.GetLatestContextHealth("m-empty")
	if err != nil {
		t.Fatalf("expected nil err when no rows, got %v", err)
	}
	if h != nil {
		t.Errorf("expected nil health, got %+v", h)
	}
}

func TestGetLatestContextHealth_PicksMostRecent(t *testing.T) {
	_, db := newTestWhip(t)
	seedWorkingMinister(t, db, "m-recent")
	recordContextHealth(t, db, "m-recent", 10_000, 100_000, 1)
	// Ensure created_at differs — SQLite CURRENT_TIMESTAMP has 1-second
	// granularity, so wait a beat before the second insert.
	time.Sleep(1100 * time.Millisecond)
	recordContextHealth(t, db, "m-recent", 90_000, 100_000, 50)

	h, err := db.GetLatestContextHealth("m-recent")
	if err != nil {
		t.Fatalf("GetLatestContextHealth: %v", err)
	}
	if h == nil || h.TokensUsed != 90_000 {
		t.Errorf("expected latest tokens_used=90000, got %+v", h)
	}
}
