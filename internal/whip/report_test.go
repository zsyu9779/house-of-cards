package whip

import (
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
)

// ─── fmtSeconds tests ─────────────────────────────────────────────────────────

func TestFmtSeconds(t *testing.T) {
	tests := []struct {
		name string
		s    float64
		want string
	}{
		{"seconds only", 30, "30s"},
		{"seconds fractional", 5.7, "6s"},
		{"one minute", 60, "1m0s"},
		{"one minute+seconds", 90, "1m30s"},
		{"one hour", 3600, "1h0m"},
		{"one hour+minutes", 3720, "1h2m"},
		{"many hours", 7200, "2h0m"},
		{"zero", 0, "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmtSeconds(tt.s)
			if got != tt.want {
				t.Errorf("fmtSeconds(%v) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}

// ─── Report tests ─────────────────────────────────────────────────────────────

func TestReport_EmptyDB(t *testing.T) {
	_, db := newTestWhip(t)

	report, err := Report(db, false)
	if err != nil {
		t.Fatalf("Report returned error: %v", err)
	}
	if report == "" {
		t.Fatal("report should not be empty")
	}
	if !contains(report, "党鞭状态报告") {
		t.Error("report should contain header")
	}
	if !contains(report, "0") {
		t.Error("report should contain zero counts")
	}
}

func TestReport_WithDataAndHistory(t *testing.T) {
	_, db := newTestWhip(t)

	// Set up: session + bills + minister + gazette + hansard.
	mustCreateSession(t, db, "sess-report", "Report Session")
	mustCreateBill(t, db, "bill-report-1", "sess-report", "Report Bill 1", "draft", "")
	mustCreateBill(t, db, "bill-report-2", "sess-report", "Report Bill 2", "enacted", "")
	mustCreateIdleMinister(t, db, "m-report")

	g := &store.Gazette{
		ID:         "gaz-report-1",
		ToMinister: store.NullString("m-report"),
		BillID:     store.NullString("bill-report-1"),
		Type:       store.NullString("handoff"),
		Summary:    "Hand off",
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	h := &store.Hansard{
		ID:         "hansard-report-1",
		MinisterID: "m-report",
		BillID:     "bill-report-1",
		Outcome:    store.NullString("enacted"),
		Notes:      store.NullString("Test note"),
	}
	if err := db.CreateHansard(h); err != nil {
		t.Fatalf("CreateHansard: %v", err)
	}

	// Report without history.
	report, err := Report(db, false)
	if err != nil {
		t.Fatalf("Report(false) returned error: %v", err)
	}
	if !contains(report, "党鞭状态报告") {
		t.Error("report should contain header")
	}
	if !contains(report, "Report Session") {
		t.Error("report should contain session title")
	}
	if !contains(report, "m-report") {
		t.Error("report should contain minister id")
	}
	if !contains(report, "draft=1") {
		t.Error("report should contain bill status counts")
	}

	// Report with history.
	reportWithHistory, err := Report(db, true)
	if err != nil {
		t.Fatalf("Report(true) returned error: %v", err)
	}
	if !contains(reportWithHistory, "最近事件日志") {
		t.Error("report with history should contain event log section")
	}
}
