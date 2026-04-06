package whip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
)

func TestDeliverGazette_InboxAndSignal(t *testing.T) {
	w, db := newTestWhip(t)

	// Create a minister with a worktree pointing to a temp dir.
	worktree := t.TempDir()
	m := &store.Minister{
		ID:       "m-inbox",
		Title:    "Inbox Minister",
		Runtime:  "claude-code",
		Status:   "working",
		Worktree: store.NullString(worktree),
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	if err := db.UpdateMinisterWorktree("m-inbox", worktree); err != nil {
		t.Fatalf("UpdateMinisterWorktree: %v", err)
	}

	g := &store.Gazette{
		ID:           "gaz-deliver-1",
		FromMinister: store.NullString("whip"),
		ToMinister:   store.NullString("m-inbox"),
		BillID:       store.NullString("bill-test"),
		Type:         store.NullString("handoff"),
		Summary:      "测试投递公报",
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	w.deliverGazette(g)

	// Verify inbox file exists.
	inboxFile := filepath.Join(worktree, ".hoc", "inbox", "gaz-deliver-1.md")
	content, err := os.ReadFile(inboxFile)
	if err != nil {
		t.Fatalf("inbox file not found: %v", err)
	}
	if !strings.Contains(string(content), "测试投递公报") {
		t.Error("inbox file should contain gazette summary")
	}
	if !strings.Contains(string(content), "whip") {
		t.Error("inbox file should contain from_minister")
	}

	// Verify gazette-signal marker exists.
	signalPath := filepath.Join(worktree, ".hoc", "gazette-signal")
	if _, err := os.Stat(signalPath); os.IsNotExist(err) {
		t.Error("gazette-signal marker file should exist")
	}
}

func TestDeliverGazette_WithPayload(t *testing.T) {
	w, db := newTestWhip(t)

	worktree := t.TempDir()
	m := &store.Minister{
		ID:       "m-payload",
		Title:    "Payload Minister",
		Runtime:  "claude-code",
		Status:   "working",
		Worktree: store.NullString(worktree),
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	if err := db.UpdateMinisterWorktree("m-payload", worktree); err != nil {
		t.Fatalf("UpdateMinisterWorktree: %v", err)
	}

	g := &store.Gazette{
		ID:           "gaz-deliver-payload",
		FromMinister: store.NullString("whip"),
		ToMinister:   store.NullString("m-payload"),
		Type:         store.NullString("completion"),
		Summary:      "Completed bill",
		Payload:      `{"summary":"done","contracts":{"api.go":"UserService"}}`,
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	w.deliverGazette(g)

	inboxFile := filepath.Join(worktree, ".hoc", "inbox", "gaz-deliver-payload.md")
	content, err := os.ReadFile(inboxFile)
	if err != nil {
		t.Fatalf("inbox file not found: %v", err)
	}
	if !strings.Contains(string(content), "结构化数据") {
		t.Error("inbox file should contain structured data section when payload is present")
	}
	if !strings.Contains(string(content), "UserService") {
		t.Error("inbox file should contain payload content")
	}
}

func TestDeliverGazette_NoWorktree(t *testing.T) {
	w, db := newTestWhip(t)

	// Minister with no worktree — delivery should be a no-op.
	m := &store.Minister{
		ID:      "m-nowt",
		Title:   "No Worktree Minister",
		Runtime: "claude-code",
		Status:  "idle",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	g := &store.Gazette{
		ID:         "gaz-nowt",
		ToMinister: store.NullString("m-nowt"),
		Type:       store.NullString("handoff"),
		Summary:    "Should not crash",
	}
	if err := db.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	// Should not panic.
	w.deliverGazette(g)
}
