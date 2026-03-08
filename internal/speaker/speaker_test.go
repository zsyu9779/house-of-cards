package speaker_test

import (
	"strings"
	"testing"

	"github.com/house-of-cards/hoc/internal/speaker"
	"github.com/house-of-cards/hoc/internal/store"
)

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGenerateContext_Empty(t *testing.T) {
	db := newTestDB(t)

	content, err := speaker.GenerateContext(db)
	if err != nil {
		t.Fatalf("GenerateContext on empty DB: %v", err)
	}

	// Should contain all expected sections even when empty.
	for _, section := range []string{
		"# Speaker Context",
		"## 政府现状",
		"## 会期进度",
		"## 内阁档案",
		"## 近期公报",
	} {
		if !strings.Contains(content, section) {
			t.Errorf("missing section %q in context output", section)
		}
	}
}

func TestGenerateContext_WithData(t *testing.T) {
	db := newTestDB(t)

	// Add a session with bills.
	sess := &store.Session{ID: "s1", Title: "Auth System", Topology: "parallel", Status: "active"}
	_ = db.CreateSession(sess)

	bills := []*store.Bill{
		{ID: "b1", SessionID: store.NullString("s1"), Title: "Build API", Status: "enacted"},
		{ID: "b2", SessionID: store.NullString("s1"), Title: "Build UI", Status: "reading"},
	}
	for _, b := range bills {
		_ = db.CreateBill(b)
	}

	// Add ministers.
	ministers := []*store.Minister{
		{ID: "m1", Title: "Backend Minister", Runtime: "claude-code", Skills: `["go"]`, Status: "working"},
		{ID: "m2", Title: "Frontend Minister", Runtime: "claude-code", Skills: `["react"]`, Status: "idle"},
	}
	for _, m := range ministers {
		_ = db.CreateMinister(m)
	}

	// Add a gazette.
	g := &store.Gazette{
		ID:           "g1",
		FromMinister: store.NullString("m1"),
		BillID:       store.NullString("b1"),
		Type:         store.NullString("completion"),
		Summary:      "Build API completed successfully",
	}
	_ = db.CreateGazette(g)

	content, err := speaker.GenerateContext(db)
	if err != nil {
		t.Fatalf("GenerateContext: %v", err)
	}

	// Session title should appear.
	if !strings.Contains(content, "Auth System") {
		t.Error("session title 'Auth System' not found in context")
	}

	// Minister names should appear.
	for _, title := range []string{"Backend Minister", "Frontend Minister"} {
		if !strings.Contains(content, title) {
			t.Errorf("minister %q not found in context", title)
		}
	}

	// Gazette summary should appear (truncated).
	if !strings.Contains(content, "Build API completed") {
		t.Error("gazette summary not found in context")
	}

	// Should show correct bill count fraction.
	if !strings.Contains(content, "1/2") {
		t.Errorf("expected '1/2' bills done fraction in context, got:\n%s", content)
	}
}
