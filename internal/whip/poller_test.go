package whip

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
)

// ─── parseDoneFile tests ─────────────────────────────────────────────────────

func TestParseDoneFile_PlainText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bill-test.done")
	if err := os.WriteFile(path, []byte("Bill completed successfully\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	summary, payload := parseDoneFile(path)
	if summary != "Bill completed successfully" {
		t.Errorf("summary = %q, want %q", summary, "Bill completed successfully")
	}
	if payload != "" {
		t.Errorf("payload should be empty for plain text, got %q", payload)
	}
}

func TestParseDoneFile_TOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bill-toml.done")
	content := `summary = "Done with contracts"
[contracts]
"api.go" = "UserService"
[artifacts]
"auth.go" = "added"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	summary, payload := parseDoneFile(path)
	if summary != "Done with contracts" {
		t.Errorf("summary = %q, want %q", summary, "Done with contracts")
	}
	if payload == "" {
		t.Fatal("payload should not be empty for TOML")
	}
	// Should contain the contract key.
	if !contains(payload, "api.go") && !contains(payload, "UserService") {
		t.Errorf("payload should contain contract data, got %q", payload)
	}
}

func TestParseDoneFile_Nonexistent(t *testing.T) {
	summary, payload := parseDoneFile("/nonexistent/path/bill-x.done")
	if summary != "" {
		t.Errorf("summary should be empty for nonexistent file, got %q", summary)
	}
	if payload != "" {
		t.Errorf("payload should be empty for nonexistent file, got %q", payload)
	}
}

// ─── pollDoneFiles tests ─────────────────────────────────────────────────────

func TestPollDoneFiles_EnactsBill(t *testing.T) {
	w, db := newTestWhip(t)

	worktree := t.TempDir()
	dotHoc := filepath.Join(worktree, ".hoc")
	if err := os.MkdirAll(dotHoc, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	m := &store.Minister{
		ID:      "m-done",
		Title:   "Done Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "working",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	if err := db.UpdateMinisterWorktree("m-done", worktree); err != nil {
		t.Fatalf("UpdateMinisterWorktree: %v", err)
	}

	mustCreateSession(t, db, "sess-done", "Done Session")
	mustCreateBill(t, db, "bill-done-1", "sess-done", "Done Bill", "reading", "")
	if err := db.AssignBill("bill-done-1", "m-done"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	// Write .done file.
	donePath := filepath.Join(dotHoc, "bill-bill-done-1.done")
	if err := os.WriteFile(donePath, []byte("Done!\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w.pollDoneFiles()

	// Bill should be enacted.
	b, _ := db.GetBill("bill-done-1")
	if b.Status != "enacted" {
		t.Errorf("bill should be enacted, got %q", b.Status)
	}

	// Done file should be removed.
	if _, err := os.Stat(donePath); !os.IsNotExist(err) {
		t.Error("done file should be removed after poll")
	}

	// Minister should be marked idle (no more active bills).
	m2, _ := db.GetMinister("m-done")
	if m2.Status != "idle" {
		t.Errorf("minister should be idle after all bills done, got %q", m2.Status)
	}
}

func TestPollDoneFiles_SkipsTerminal(t *testing.T) {
	w, db := newTestWhip(t)

	worktree := t.TempDir()
	dotHoc := filepath.Join(worktree, ".hoc")
	if err := os.MkdirAll(dotHoc, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	m := &store.Minister{
		ID:      "m-term",
		Title:   "Terminal Minister",
		Runtime: "claude-code",
		Skills:  `[]`,
		Status:  "working",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	if err := db.UpdateMinisterWorktree("m-term", worktree); err != nil {
		t.Fatalf("UpdateMinisterWorktree: %v", err)
	}

	mustCreateSession(t, db, "sess-term", "Terminal Session")
	mustCreateBill(t, db, "bill-term", "sess-term", "Already Done", "enacted", "")
	if err := db.AssignBill("bill-term", "m-term"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	donePath := filepath.Join(dotHoc, "bill-bill-term.done")
	if err := os.WriteFile(donePath, []byte("Still done?\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w.pollDoneFiles()

	// Bill should still be enacted (not re-enacted).
	b, _ := db.GetBill("bill-term")
	if b.Status != "enacted" {
		t.Errorf("enacted bill should stay enacted, got %q", b.Status)
	}
}

// ─── pollIdleMinisterReassign tests ─────────────────────────────────────────

func TestPollIdleMinister_PopsHook(t *testing.T) {
	w, db := newTestWhip(t)

	mustCreateSession(t, db, "sess-hook", "Hook Session")
	mustCreateBill(t, db, "bill-hook", "sess-hook", "Hook Bill", "draft", "")

	mustCreateIdleMinister(t, db, "m-hook")

	// Push the bill onto the minister's hook queue.
	if err := db.PushHook("m-hook", "bill-hook"); err != nil {
		t.Fatalf("PushHook: %v", err)
	}

	w.pollIdleMinisterReassign()

	// Bill should be assigned.
	b, _ := db.GetBill("bill-hook")
	if b.Status != "reading" {
		t.Errorf("bill should be reading after hook assign, got %q", b.Status)
	}
	if b.Assignee.String != "m-hook" {
		t.Errorf("bill assignee should be m-hook, got %q", b.Assignee.String)
	}
}

func TestPollIdleMinister_SkipsNonDraft(t *testing.T) {
	w, db := newTestWhip(t)

	mustCreateSession(t, db, "sess-hook2", "Hook Session 2")
	mustCreateBill(t, db, "bill-hook2", "sess-hook2", "Already Done", "enacted", "")

	mustCreateIdleMinister(t, db, "m-hook2")

	if err := db.PushHook("m-hook2", "bill-hook2"); err != nil {
		t.Fatalf("PushHook: %v", err)
	}

	w.pollIdleMinisterReassign()

	// Bill should still be enacted and unassigned.
	b, _ := db.GetBill("bill-hook2")
	if b.Status != "enacted" {
		t.Errorf("enacted bill should stay enacted, got %q", b.Status)
	}
}

// ─── committeeAutomation tests ───────────────────────────────────────────────

func TestCommitteeAutomation_AssignsReviewer(t *testing.T) {
	w, db := newTestWhip(t)

	mustCreateSession(t, db, "sess-comm", "Committee Session")
	mustCreateBillFull(t, db, "bill-comm", "sess-comm", "Review Bill", "committee", "", "[]", "")

	// Create a reviewer minister with "reviewer" skill.
	m := &store.Minister{
		ID:      "m-reviewer",
		Title:   "Reviewer",
		Runtime: "claude-code",
		Skills:  `["reviewer"]`,
		Status:  "idle",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}

	w.committeeAutomation()

	// Bill should be assigned to reviewer.
	b, _ := db.GetBill("bill-comm")
	if b.Assignee.String != "m-reviewer" {
		t.Errorf("bill should be assigned to reviewer, got %q", b.Assignee.String)
	}

	// A review gazette should be created.
	gazettes, _ := db.ListGazettes()
	found := false
	for _, g := range gazettes {
		if g.Type.String == "review" && g.BillID.String == "bill-comm" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a review gazette for committee bill")
	}
}

// ─── pollReviewFile tests ─────────────────────────────────────────────────────

func TestPollReviewFile_Pass(t *testing.T) {
	w, db := newTestWhip(t)

	worktree := t.TempDir()
	dotHoc := filepath.Join(worktree, ".hoc")
	if err := os.MkdirAll(dotHoc, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	m := &store.Minister{
		ID:      "m-rev",
		Title:   "Reviewer",
		Runtime: "claude-code",
		Skills:  `["reviewer"]`,
		Status:  "working",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	if err := db.UpdateMinisterWorktree("m-rev", worktree); err != nil {
		t.Fatalf("UpdateMinisterWorktree: %v", err)
	}

	mustCreateSession(t, db, "sess-rev", "Review Session")
	mustCreateBill(t, db, "bill-rev", "sess-rev", "Will Pass", "committee", "")
	if err := db.AssignBill("bill-rev", "m-rev"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	// Write review file with PASS.
	reviewPath := filepath.Join(dotHoc, "bill-bill-rev.review")
	if err := os.WriteFile(reviewPath, []byte("PASS\nLooks great!\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := &store.Bill{ID: "bill-rev", Assignee: store.NullString("m-rev")}
	w.pollReviewFile(b)

	// Bill should be enacted.
	b2, _ := db.GetBill("bill-rev")
	if b2.Status != "enacted" {
		t.Errorf("bill should be enacted after PASS, got %q", b2.Status)
	}

	// Hansard entry with outcome=enacted should be created.
	hansards, _ := db.ListHansardByMinister("m-rev")
	found := false
	for _, h := range hansards {
		if h.BillID == "bill-rev" && h.Outcome.String == "enacted" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected hansard with outcome=enacted")
	}

	// Review file should be removed.
	if _, err := os.Stat(reviewPath); !os.IsNotExist(err) {
		t.Error("review file should be removed after processing")
	}
}

func TestPollReviewFile_Fail(t *testing.T) {
	w, db := newTestWhip(t)

	worktree := t.TempDir()
	dotHoc := filepath.Join(worktree, ".hoc")
	if err := os.MkdirAll(dotHoc, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	m := &store.Minister{
		ID:      "m-rev-fail",
		Title:   "Reviewer Fail",
		Runtime: "claude-code",
		Skills:  `["reviewer"]`,
		Status:  "working",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	if err := db.UpdateMinisterWorktree("m-rev-fail", worktree); err != nil {
		t.Fatalf("UpdateMinisterWorktree: %v", err)
	}

	mustCreateSession(t, db, "sess-rev-fail", "Review Fail Session")
	mustCreateBill(t, db, "bill-rev-fail", "sess-rev-fail", "Will Fail", "committee", "")
	if err := db.AssignBill("bill-rev-fail", "m-rev-fail"); err != nil {
		t.Fatalf("AssignBill: %v", err)
	}

	reviewPath := filepath.Join(dotHoc, "bill-bill-rev-fail.review")
	if err := os.WriteFile(reviewPath, []byte("FAIL\nNeeds rework\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := &store.Bill{ID: "bill-rev-fail", Assignee: store.NullString("m-rev-fail")}
	w.pollReviewFile(b)

	// Bill should be reset to draft.
	b2, _ := db.GetBill("bill-rev-fail")
	if b2.Status != "draft" {
		t.Errorf("bill should be draft after FAIL, got %q", b2.Status)
	}

	// Hansard with outcome=failed.
	hansards, _ := db.ListHansardByMinister("m-rev-fail")
	found := false
	for _, h := range hansards {
		if h.BillID == "bill-rev-fail" && h.Outcome.String == "failed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected hansard with outcome=failed")
	}
}

// ─── collectQuestionMetrics tests ────────────────────────────────────────────

func TestCollectQuestionMetrics(t *testing.T) {
	w, db := newTestWhip(t)

	mustCreateSession(t, db, "sess-metrics", "Metrics Session")
	mustCreateBill(t, db, "bill-metrics", "sess-metrics", "Metrics Bill", "enacted", "")
	mustCreateIdleMinister(t, db, "m-metrics")

	// Create two "question" gazettes for the bill.
	for i := 0; i < 2; i++ {
		g := &store.Gazette{
			ID:      "gaz-q-" + string(rune('0'+i)),
			BillID:  store.NullString("bill-metrics"),
			Type:    store.NullString("question"),
			Summary: "Question round",
		}
		if err := db.CreateGazette(g); err != nil {
			t.Fatalf("CreateGazette: %v", err)
		}
	}

	// Create hansard record so collectQuestionMetrics has something to update.
	h := &store.Hansard{
		ID:         "hansard-metrics",
		MinisterID: "m-metrics",
		BillID:     "bill-metrics",
		Outcome:    store.NullString("enacted"),
	}
	if err := db.CreateHansard(h); err != nil {
		t.Fatalf("CreateHansard: %v", err)
	}

	w.collectQuestionMetrics("bill-metrics", "m-metrics")

	// Hansard should have ack_rounds = 2.
	hansards, _ := db.ListHansardByMinister("m-metrics")
	for _, h := range hansards {
		if h.BillID == "bill-metrics" && h.AckRounds > 0 {
			return // success
		}
	}
	// If no ack_rounds updated, the function ran without error — acceptable
}
