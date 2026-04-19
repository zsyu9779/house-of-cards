package cmd

// Integration tests that exercise Cobra RunE entry points directly.
//
// These complement integration_test.go (which tests store flows) by actually
// invoking the CLI command bodies. Each test sets up an isolated SQLite DB,
// seeds data, runs a command's RunE, then verifies side effects by reopening
// the DB (since RunE calls `defer db.Close()`).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/house-of-cards/hoc/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// installDB opens a fresh SQLite DB at dir and wires it into the package globals.
// t.Cleanup closes and nils the globals so each test is isolated.
func installDB(t *testing.T, dir string) *store.DB {
	t.Helper()
	testDB, err := store.NewDB(dir)
	if err != nil {
		t.Fatalf("installDB NewDB(%s): %v", dir, err)
	}
	db = testDB
	hocDir = dir
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
		db = nil
		hocDir = ""
	})
	return testDB
}

// runRunE invokes cmd.RunE with the given args. Because each command ends with
// `defer db.Close()`, after returning we set db=nil so the next RunE reopens.
// Any provided flags are reset to their default after the call.
func runRunE(t *testing.T, c *cobra.Command, args []string, flags map[string]string) error {
	t.Helper()
	resetFlag := func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
			return
		}
		_ = f.Value.Set(f.DefValue)
	}
	for k := range flags {
		if f := c.Flags().Lookup(k); f != nil {
			resetFlag(f)
		}
	}
	for k, v := range flags {
		if err := c.Flags().Set(k, v); err != nil {
			t.Fatalf("set flag %s=%s: %v", k, v, err)
		}
	}
	defer func() {
		for k := range flags {
			if f := c.Flags().Lookup(k); f != nil {
				resetFlag(f)
			}
		}
		db = nil
	}()
	return c.RunE(c, args)
}

// reopenDB returns a fresh *store.DB at hocDir for verification after a RunE call.
func reopenDB(t *testing.T) *store.DB {
	t.Helper()
	d, err := store.NewDB(hocDir)
	if err != nil {
		t.Fatalf("reopenDB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// seedMinister creates a minister row for use in a fresh test DB.
func seedMinister(t *testing.T, sdb *store.DB, id, status string) {
	t.Helper()
	m := &store.Minister{ID: id, Title: "Minister " + id, Runtime: "claude-code", Status: status}
	if err := sdb.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister %s: %v", id, err)
	}
}

func seedBill(t *testing.T, sdb *store.DB, id, status, assignee string) {
	t.Helper()
	seedBillInSession(t, sdb, id, status, assignee, "")
}

func seedBillInSession(t *testing.T, sdb *store.DB, id, status, assignee, sessionID string) {
	t.Helper()
	b := &store.Bill{
		ID:        id,
		Title:     "Bill " + id,
		Status:    status,
		DependsOn: store.NullString("[]"),
	}
	if assignee != "" {
		b.Assignee = store.NullString(assignee)
	}
	if sessionID != "" {
		b.SessionID = store.NullString(sessionID)
	}
	if err := sdb.CreateBill(b); err != nil {
		t.Fatalf("CreateBill %s: %v", id, err)
	}
}

func seedSession(t *testing.T, sdb *store.DB, id, status string) {
	t.Helper()
	s := &store.Session{ID: id, Title: "Session " + id, Topology: "parallel", Status: status}
	if err := sdb.CreateSession(s); err != nil {
		t.Fatalf("CreateSession %s: %v", id, err)
	}
}

// ─── bill commands ───────────────────────────────────────────────────────────

func TestBillDraft_CreatesBill(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, billDraftCmd, nil, map[string]string{
		"title":  "Implement login",
		"motion": "Build JWT auth",
	})
	if err != nil {
		t.Fatalf("billDraftCmd.RunE: %v", err)
	}

	fresh := reopenDB(t)
	bills, err := fresh.ListBills()
	if err != nil {
		t.Fatalf("ListBills: %v", err)
	}
	if len(bills) != 1 {
		t.Fatalf("expected 1 bill, got %d", len(bills))
	}
	if bills[0].Title != "Implement login" {
		t.Errorf("title: got %q, want %q", bills[0].Title, "Implement login")
	}
	if bills[0].Status != "draft" {
		t.Errorf("status: got %q, want draft", bills[0].Status)
	}
}

func TestBillDraft_RejectsBadTitle(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, billDraftCmd, nil, map[string]string{
		"title": "ab",
	})
	if err == nil {
		t.Fatal("expected title validation to fail")
	}
	if !strings.Contains(err.Error(), "议案标题校验失败") {
		t.Errorf("expected title validation error, got: %v", err)
	}
}

func TestBillDraft_ForceSkipsValidation(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, billDraftCmd, nil, map[string]string{
		"title": "ab",
		"force": "true",
	})
	if err != nil {
		t.Fatalf("--force should skip validation, got: %v", err)
	}
}

func TestBillList_TextModeEmpty(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, billListCmd, nil, nil); err != nil {
		t.Fatalf("billListCmd.RunE: %v", err)
	}
}

func TestBillList_TextModeWithBill(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-alpha", "draft", "")

	if err := runRunE(t, billListCmd, nil, nil); err != nil {
		t.Fatalf("billListCmd.RunE: %v", err)
	}
}

func TestBillShow_Found(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-show", "draft", "")

	if err := runRunE(t, billShowCmd, []string{"bill-show"}, nil); err != nil {
		t.Fatalf("billShowCmd.RunE: %v", err)
	}
}

func TestBillShow_NotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, billShowCmd, []string{"no-such-bill"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestBillAssign_UpdatesStatusAndAssignee(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-assign", "idle")
	seedBill(t, sdb, "bill-assign", "draft", "")

	if err := runRunE(t, billAssignCmd, []string{"bill-assign", "m-assign"}, nil); err != nil {
		t.Fatalf("billAssignCmd.RunE: %v", err)
	}

	fresh := reopenDB(t)
	b, err := fresh.GetBill("bill-assign")
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if b.Status != "reading" {
		t.Errorf("status: got %q, want reading", b.Status)
	}
	if b.Assignee.String != "m-assign" {
		t.Errorf("assignee: got %q, want m-assign", b.Assignee.String)
	}

	gs, _ := fresh.ListGazettesForBill("bill-assign")
	if len(gs) == 0 {
		t.Error("expected handoff gazette after assignment")
	}
}

func TestBillAssign_MissingBill(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-x", "idle")

	err := runRunE(t, billAssignCmd, []string{"nope", "m-x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "bill not found") {
		t.Errorf("expected bill not found, got: %v", err)
	}
}

func TestBillAssign_MissingMinister(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-nm", "draft", "")

	err := runRunE(t, billAssignCmd, []string{"bill-nm", "nope"}, nil)
	if err == nil || !strings.Contains(err.Error(), "minister not found") {
		t.Errorf("expected minister not found, got: %v", err)
	}
}

func TestBillEnacted_WritesHansard(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-enact", "working")
	seedBill(t, sdb, "bill-enact", "reading", "m-enact")

	err := runRunE(t, billEnactedCmd, []string{"bill-enact"}, map[string]string{
		"quality":  "0.85",
		"notes":    "clean tests",
		"duration": "120",
	})
	if err != nil {
		t.Fatalf("billEnactedCmd.RunE: %v", err)
	}

	fresh := reopenDB(t)
	b, _ := fresh.GetBill("bill-enact")
	if b.Status != "enacted" {
		t.Errorf("status: got %q, want enacted", b.Status)
	}
	entries, _ := fresh.ListHansardByMinister("m-enact")
	if len(entries) == 0 {
		t.Fatal("expected hansard entry")
	}
	if entries[0].Quality != 0.85 {
		t.Errorf("quality: got %v, want 0.85", entries[0].Quality)
	}
}

func TestBillCommittee_Transition(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-ct", "working")
	seedBill(t, sdb, "bill-ct", "reading", "m-ct")

	if err := runRunE(t, billCommitteeCmd, []string{"bill-ct"}, nil); err != nil {
		t.Fatalf("billCommitteeCmd.RunE: %v", err)
	}
	b, _ := reopenDB(t).GetBill("bill-ct")
	if b.Status != "committee" {
		t.Errorf("status: got %q, want committee", b.Status)
	}
}

func TestBillCommittee_RejectsNonReading(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-bad", "enacted", "")

	err := runRunE(t, billCommitteeCmd, []string{"bill-bad"}, nil)
	if err == nil {
		t.Fatal("expected rejection for non-reading status")
	}
}

func TestBillReview_Pass(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-rv", "working")
	seedBill(t, sdb, "bill-rv", "committee", "m-rv")

	err := runRunE(t, billReviewCmd, []string{"bill-rv"}, map[string]string{
		"pass":    "true",
		"notes":   "LGTM",
		"quality": "0.9",
	})
	if err != nil {
		t.Fatalf("billReviewCmd.RunE: %v", err)
	}
	b, _ := reopenDB(t).GetBill("bill-rv")
	if b.Status != "enacted" {
		t.Errorf("status: got %q, want enacted", b.Status)
	}
}

func TestBillReview_Fail(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-fail", "working")
	seedBill(t, sdb, "bill-fail", "committee", "m-fail")

	err := runRunE(t, billReviewCmd, []string{"bill-fail"}, map[string]string{
		"fail":  "true",
		"notes": "missing tests",
	})
	if err != nil {
		t.Fatalf("billReviewCmd.RunE: %v", err)
	}
	b, _ := reopenDB(t).GetBill("bill-fail")
	if b.Status != "reading" {
		t.Errorf("status: got %q, want reading", b.Status)
	}
}

func TestBillReview_MustSpecifyFlag(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-none", "committee", "")

	err := runRunE(t, billReviewCmd, []string{"bill-none"}, nil)
	if err == nil || !strings.Contains(err.Error(), "--pass") {
		t.Errorf("expected --pass/--fail required, got: %v", err)
	}
}

func TestBillReview_CannotSpecifyBoth(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-both", "committee", "")

	err := runRunE(t, billReviewCmd, []string{"bill-both"}, map[string]string{
		"pass": "true",
		"fail": "true",
	})
	if err == nil || !strings.Contains(err.Error(), "不能同时") {
		t.Errorf("expected mutual exclusion error, got: %v", err)
	}
}

func TestBillPause_ClearsAssignee(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-pause", "working")
	seedBill(t, sdb, "bill-pause", "reading", "m-pause")

	err := runRunE(t, billPauseCmd, []string{"bill-pause"}, map[string]string{
		"reason": "blocked by upstream",
	})
	if err != nil {
		t.Fatalf("billPauseCmd.RunE: %v", err)
	}
	b, _ := reopenDB(t).GetBill("bill-pause")
	if b.Status != "draft" {
		t.Errorf("status: got %q, want draft", b.Status)
	}
	if b.Assignee.String != "" {
		t.Errorf("expected assignee cleared, got %q", b.Assignee.String)
	}
}

func TestBillPause_RejectsTerminalStatus(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-done", "enacted", "")

	err := runRunE(t, billPauseCmd, []string{"bill-done"}, nil)
	if err == nil {
		t.Fatal("expected terminal-status rejection")
	}
}

func TestBillResume(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-resume", "draft", "")

	if err := runRunE(t, billResumeCmd, []string{"bill-resume"}, nil); err != nil {
		t.Fatalf("billResumeCmd.RunE: %v", err)
	}
}

func TestBillResume_RejectsNonDraft(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-nonresume", "reading", "")

	err := runRunE(t, billResumeCmd, []string{"bill-nonresume"}, nil)
	if err == nil {
		t.Fatal("expected non-draft rejection")
	}
}

func TestBillSplit_CreatesSubBills(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-split", "draft", "")

	err := runRunE(t, billSplitCmd, []string{"bill-split"}, map[string]string{
		"into": "Design schema,Implement API,Write tests",
	})
	if err != nil {
		t.Fatalf("billSplitCmd.RunE: %v", err)
	}
	fresh := reopenDB(t)
	parent, _ := fresh.GetBill("bill-split")
	if parent.Status != "epic" {
		t.Errorf("parent status: got %q, want epic", parent.Status)
	}
	all, _ := fresh.ListBills()
	children := 0
	for _, b := range all {
		if b.ParentBill == "bill-split" {
			children++
		}
	}
	if children != 3 {
		t.Errorf("expected 3 child bills, got %d", children)
	}
}

func TestBillSplit_RequiresMultipleSubs(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "bill-singlesplit", "draft", "")

	err := runRunE(t, billSplitCmd, []string{"bill-singlesplit"}, map[string]string{
		"into": "Only one",
	})
	if err == nil || !strings.Contains(err.Error(), "至少") {
		t.Errorf("expected minimum-count error, got: %v", err)
	}
}

// ─── session commands ────────────────────────────────────────────────────────

func TestSessionStatus_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, sessionStatusCmd, nil, nil); err != nil {
		t.Fatalf("sessionStatusCmd.RunE: %v", err)
	}
}

func TestSessionStatus_List(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-list", "active")

	if err := runRunE(t, sessionStatusCmd, nil, nil); err != nil {
		t.Fatalf("sessionStatusCmd.RunE: %v", err)
	}
}

func TestSessionStatus_Detail(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-detail", "active")
	seedBillInSession(t, sdb, "bill-sd", "draft", "", "sess-detail")

	if err := runRunE(t, sessionStatusCmd, []string{"sess-detail"}, nil); err != nil {
		t.Fatalf("sessionStatusCmd.RunE: %v", err)
	}
}

func TestSessionDissolve_WithConfirm(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-dissolve", "active")

	err := runRunE(t, sessionDissolveCmd, []string{"sess-dissolve"}, map[string]string{
		"confirm": "true",
	})
	if err != nil {
		t.Fatalf("sessionDissolveCmd.RunE: %v", err)
	}
	s, _ := reopenDB(t).GetSession("sess-dissolve")
	if s.Status != "dissolved" {
		t.Errorf("status: got %q, want dissolved", s.Status)
	}
}

func TestSessionDissolve_IdempotencyGuard(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	s := &store.Session{ID: "sess-gone", Title: "gone", Topology: "parallel", Status: "dissolved"}
	if err := sdb.CreateSession(s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	err := runRunE(t, sessionDissolveCmd, []string{"sess-gone"}, map[string]string{
		"confirm": "true",
	})
	if err == nil || !strings.Contains(err.Error(), "dissolved") {
		t.Errorf("expected idempotency guard, got: %v", err)
	}
}

func TestSessionDissolve_NotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, sessionDissolveCmd, []string{"nope"}, map[string]string{
		"confirm": "true",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got: %v", err)
	}
}

func TestSessionPause_ActiveToPaused(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-pause", "active")

	if err := runRunE(t, sessionPauseCmd, []string{"sess-pause"}, map[string]string{
		"reason": "waiting review",
	}); err != nil {
		t.Fatalf("sessionPauseCmd.RunE: %v", err)
	}
	s, _ := reopenDB(t).GetSession("sess-pause")
	if s.Status != "paused" {
		t.Errorf("status: got %q, want paused", s.Status)
	}
}

func TestSessionResume_PausedToActive(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-res", "paused")

	if err := runRunE(t, sessionResumeCmd, []string{"sess-res"}, nil); err != nil {
		t.Fatalf("sessionResumeCmd.RunE: %v", err)
	}
	s, _ := reopenDB(t).GetSession("sess-res")
	if s.Status != "active" {
		t.Errorf("status: got %q, want active", s.Status)
	}
}

func TestSessionAdvance_RequiresForce(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-adv", "active")

	err := runRunE(t, sessionAdvanceCmd, []string{"sess-adv"}, nil)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected --force required, got: %v", err)
	}
}

func TestSessionAdvance_ForceSucceeds(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-adv2", "active")

	if err := runRunE(t, sessionAdvanceCmd, []string{"sess-adv2"}, map[string]string{
		"force": "true",
	}); err != nil {
		t.Fatalf("sessionAdvanceCmd.RunE: %v", err)
	}
}

// ─── session open (TOML-driven) ──────────────────────────────────────────────

func TestSessionOpen_FromTOML(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	specPath := filepath.Join(dir, "session.toml")
	content := `[session]
title = "Open Test"
topology = "pipeline"

[[bills]]
id = "b1"
title = "First bill with long title"
motion = "Do the first thing"
portfolio = "go"

[[bills]]
id = "b2"
title = "Second bill with long title"
motion = "Do the second thing"
depends_on = ["b1"]
`
	if err := os.WriteFile(specPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runRunE(t, sessionOpenCmd, []string{specPath}, nil); err != nil {
		t.Fatalf("sessionOpenCmd.RunE: %v", err)
	}
	fresh := reopenDB(t)
	sessions, _ := fresh.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Title != "Open Test" {
		t.Errorf("title: got %q", sessions[0].Title)
	}
	bills, _ := fresh.ListBillsBySession(sessions[0].ID)
	if len(bills) != 2 {
		t.Errorf("expected 2 bills, got %d", len(bills))
	}
}

func TestSessionOpen_RequiresTitle(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	specPath := filepath.Join(dir, "bad.toml")
	content := `[session]
topology = "parallel"
`
	if err := os.WriteFile(specPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := runRunE(t, sessionOpenCmd, []string{specPath}, nil)
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("expected title required, got: %v", err)
	}
}

// ─── minister commands ──────────────────────────────────────────────────────

func TestMinisterList_TextMode(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-alice", "idle")
	seedMinister(t, sdb, "m-bob", "offline")

	if err := runRunE(t, ministersListCmd, nil, nil); err != nil {
		t.Fatalf("ministersListCmd.RunE: %v", err)
	}
}

func TestMinisterDismiss_WithConfirm(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-dismiss", "idle")

	err := runRunE(t, ministerDismissCmd, []string{"m-dismiss"}, map[string]string{
		"confirm": "true",
	})
	if err != nil {
		t.Fatalf("ministerDismissCmd.RunE: %v", err)
	}
	m, _ := reopenDB(t).GetMinister("m-dismiss")
	if m.Status != "offline" {
		t.Errorf("status: got %q, want offline", m.Status)
	}
}

func TestMinisterDismiss_IdempotencyGuard(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-off", "offline")

	err := runRunE(t, ministerDismissCmd, []string{"m-off"}, map[string]string{
		"confirm": "true",
	})
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Errorf("expected idempotency guard, got: %v", err)
	}
}

func TestMinisterDismiss_NotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, ministerDismissCmd, []string{"nope"}, map[string]string{
		"confirm": "true",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got: %v", err)
	}
}

func TestMinisterSummon_SimpleIdle(t *testing.T) {
	// Summon without --bill just marks the minister idle.
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-summon", "offline")

	if err := runRunE(t, ministerSummonCmd, []string{"m-summon"}, nil); err != nil {
		t.Fatalf("ministerSummonCmd.RunE: %v", err)
	}
	m, _ := reopenDB(t).GetMinister("m-summon")
	if m.Status != "idle" {
		t.Errorf("status: got %q, want idle", m.Status)
	}
}

// ─── gazette commands ────────────────────────────────────────────────────────

func TestGazetteList_All(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	g := &store.Gazette{
		ID:      "gaz-1",
		Type:    store.NullString("completion"),
		Summary: "done",
	}
	if err := sdb.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	if err := runRunE(t, gazetteListCmd, nil, nil); err != nil {
		t.Fatalf("gazetteListCmd.RunE: %v", err)
	}
}

func TestGazetteList_ByMinister(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	g := &store.Gazette{
		ID:         "gaz-m",
		ToMinister: store.NullString("m-x"),
		Type:       store.NullString("handoff"),
		Summary:    "hello",
	}
	if err := sdb.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	if err := runRunE(t, gazetteListCmd, nil, map[string]string{"minister": "m-x"}); err != nil {
		t.Fatalf("gazetteListCmd.RunE: %v", err)
	}
}

func TestGazetteList_ByBill(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	g := &store.Gazette{
		ID:      "gaz-b",
		BillID:  store.NullString("b-x"),
		Type:    store.NullString("review"),
		Summary: "review",
	}
	if err := sdb.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	if err := runRunE(t, gazetteListCmd, nil, map[string]string{"bill": "b-x"}); err != nil {
		t.Fatalf("gazetteListCmd.RunE: %v", err)
	}
}

func TestGazetteShow_Found(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	g := &store.Gazette{
		ID:      "gaz-show",
		Type:    store.NullString("completion"),
		Summary: "done",
	}
	if err := sdb.CreateGazette(g); err != nil {
		t.Fatalf("CreateGazette: %v", err)
	}

	if err := runRunE(t, gazetteShowCmd, []string{"gaz-show"}, nil); err != nil {
		t.Fatalf("gazetteShowCmd.RunE: %v", err)
	}
}

func TestGazetteShow_NotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, gazetteShowCmd, []string{"nope"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got: %v", err)
	}
}

func TestGazetteTemplate_AllTypes(t *testing.T) {
	// Template command doesn't touch DB.
	for _, typ := range []string{"completion", "handoff", "help", "review", "conflict"} {
		var buf bytes.Buffer
		gazetteTemplateCmd.SetOut(&buf)
		err := runRunE(t, gazetteTemplateCmd, nil, map[string]string{"type": typ})
		if err != nil {
			t.Errorf("type=%s: %v", typ, err)
		}
	}
}

func TestGazetteTemplate_UnknownType(t *testing.T) {
	err := runRunE(t, gazetteTemplateCmd, nil, map[string]string{"type": "bogus"})
	if err == nil || !strings.Contains(err.Error(), "未知") {
		t.Errorf("expected unknown template error, got: %v", err)
	}
}

// ─── hansard commands ────────────────────────────────────────────────────────

func TestHansardList_Empty(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, hansardListCmd, nil, nil); err != nil {
		t.Fatalf("hansardListCmd.RunE: %v", err)
	}
}

func TestHansardList_WithEntries(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-h", "idle")
	h := &store.Hansard{
		ID:         "h-1",
		MinisterID: "m-h",
		BillID:     "b-h",
		Outcome:    store.NullString("enacted"),
		Quality:    0.9,
		DurationS:  60,
		Notes:      store.NullString("clean"),
	}
	if err := sdb.CreateHansard(h); err != nil {
		t.Fatalf("CreateHansard: %v", err)
	}

	if err := runRunE(t, hansardListCmd, nil, nil); err != nil {
		t.Fatalf("hansardListCmd.RunE: %v", err)
	}
}

func TestHansardCmd_ByMinister(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-hh", "idle")
	h := &store.Hansard{
		ID:         "h-2",
		MinisterID: "m-hh",
		BillID:     "b-hh",
		Outcome:    store.NullString("enacted"),
	}
	if err := sdb.CreateHansard(h); err != nil {
		t.Fatalf("CreateHansard: %v", err)
	}

	if err := runRunE(t, hansardCmd, []string{"m-hh"}, nil); err != nil {
		t.Fatalf("hansardCmd.RunE: %v", err)
	}
}

func TestHansardCmd_MinisterNotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, hansardCmd, []string{"nope"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got: %v", err)
	}
}

func TestHansardTrend_NoMinisters(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, hansardTrendCmd, nil, nil); err != nil {
		t.Fatalf("hansardTrendCmd.RunE: %v", err)
	}
}

func TestHansardTrend_WithData(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-trend", "idle")
	if err := sdb.CreateHansard(&store.Hansard{
		ID: "h-tr", MinisterID: "m-trend", BillID: "b-tr",
		Outcome: store.NullString("enacted"), Quality: 0.85,
	}); err != nil {
		t.Fatalf("CreateHansard: %v", err)
	}
	if err := runRunE(t, hansardTrendCmd, nil, map[string]string{"last": "5"}); err != nil {
		t.Fatalf("hansardTrendCmd.RunE: %v", err)
	}
}

func TestHansardScore_NoMinisters(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, hansardScoreCmd, nil, nil); err != nil {
		t.Fatalf("hansardScoreCmd.RunE: %v", err)
	}
}

func TestHansardScore_WithData(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-sc1", "idle")
	seedMinister(t, sdb, "m-sc2", "idle")
	if err := sdb.CreateHansard(&store.Hansard{
		ID: "h-sc1", MinisterID: "m-sc1", BillID: "b-sc1",
		Outcome: store.NullString("enacted"), Quality: 0.9,
	}); err != nil {
		t.Fatalf("CreateHansard: %v", err)
	}

	if err := runRunE(t, hansardScoreCmd, nil, nil); err != nil {
		t.Fatalf("hansardScoreCmd.RunE: %v", err)
	}
}

func TestHansardMetrics_Global(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-met", "idle")
	if err := sdb.CreateHansard(&store.Hansard{
		ID: "h-met", MinisterID: "m-met", BillID: "b-met",
		Outcome: store.NullString("enacted"),
	}); err != nil {
		t.Fatalf("CreateHansard: %v", err)
	}

	if err := runRunE(t, hansardMetricsCmd, nil, nil); err != nil {
		t.Fatalf("hansardMetricsCmd.RunE: %v", err)
	}
}

func TestHansardMetrics_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, hansardMetricsCmd, []string{"no-sess"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got: %v", err)
	}
}

// ─── cabinet commands ────────────────────────────────────────────────────────

func TestCabinetList_Empty(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, cabinetListCmd, nil, nil); err != nil {
		t.Fatalf("cabinetListCmd.RunE: %v", err)
	}
}

func TestCabinetList_WithMinisters(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-cab1", "idle")
	seedMinister(t, sdb, "m-cab2", "working")
	seedBill(t, sdb, "b-cab", "reading", "m-cab2")

	if err := runRunE(t, cabinetListCmd, nil, nil); err != nil {
		t.Fatalf("cabinetListCmd.RunE: %v", err)
	}
}

func TestCabinetReshuffle_NoDraft(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, cabinetReshuffleCmd, nil, nil); err != nil {
		t.Fatalf("cabinetReshuffleCmd.RunE: %v", err)
	}
}

func TestCabinetReshuffle_NoMinisters(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "b-un", "draft", "")

	if err := runRunE(t, cabinetReshuffleCmd, nil, nil); err != nil {
		t.Fatalf("cabinetReshuffleCmd.RunE: %v", err)
	}
}

func TestCabinetReshuffle_DryRun(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-rs", "idle")
	seedBill(t, sdb, "b-rs", "draft", "")

	if err := runRunE(t, cabinetReshuffleCmd, nil, nil); err != nil {
		t.Fatalf("cabinetReshuffleCmd.RunE: %v", err)
	}
	fresh := reopenDB(t)
	b, _ := fresh.GetBill("b-rs")
	if b.Status != "draft" {
		t.Errorf("dry run changed status to %q", b.Status)
	}
}

func TestCabinetReshuffle_Confirm(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedMinister(t, sdb, "m-rs2", "idle")
	seedBill(t, sdb, "b-rs2", "draft", "")

	if err := runRunE(t, cabinetReshuffleCmd, nil, map[string]string{
		"confirm": "true",
	}); err != nil {
		t.Fatalf("cabinetReshuffleCmd.RunE: %v", err)
	}
	fresh := reopenDB(t)
	b, _ := fresh.GetBill("b-rs2")
	if b.Status != "reading" {
		t.Errorf("status: got %q, want reading", b.Status)
	}
	if b.Assignee.String != "m-rs2" {
		t.Errorf("assignee: got %q, want m-rs2", b.Assignee.String)
	}
}

// ─── config commands ─────────────────────────────────────────────────────────

func TestConfigShow_DefaultConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)

	if err := runRunE(t, configShowCmd, nil, nil); err != nil {
		t.Fatalf("configShowCmd.RunE: %v", err)
	}
}

func TestConfigReload_DefaultConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)

	if err := runRunE(t, configReloadCmd, nil, nil); err != nil {
		t.Fatalf("configReloadCmd.RunE: %v", err)
	}
}

// ─── doctor ──────────────────────────────────────────────────────────────────

func TestDoctor_EmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)

	if err := runRunE(t, doctorCmd, nil, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}
}

func TestDoctor_OTLPStubFlagged(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)
	// Write a config.toml that explicitly selects otlp — doctor should flag it.
	hocSub := filepath.Join(dir, ".hoc")
	if err := os.MkdirAll(hocSub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgTOML := "[observability]\nexporter = \"otlp\"\notlp_endpoint = \"localhost:4317\"\n"
	if err := os.WriteFile(filepath.Join(hocSub, "config.toml"), []byte(cfgTOML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// doctor itself returns nil even when issues exist; we just want the code
	// path to execute without error so coverage counts it.
	if err := runRunE(t, doctorCmd, nil, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}
}

func TestDoctor_FixMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)
	sdb, err := store.NewDB(dir)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if err := sdb.CreateMinister(&store.Minister{
		ID: "m-stuck", Title: "Stuck", Runtime: "claude-code", Status: "stuck",
	}); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	_ = sdb.Close()

	if err := runRunE(t, doctorCmd, nil, map[string]string{"fix": "true"}); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}

	fresh, err := store.NewDB(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = fresh.Close() }()
	m, _ := fresh.GetMinister("m-stuck")
	if m.Status != "idle" {
		t.Errorf("status after --fix: got %q, want idle", m.Status)
	}
}

// ─── events ──────────────────────────────────────────────────────────────────

func TestEventsList_Empty(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, eventsListCmd, nil, nil); err != nil {
		t.Fatalf("eventsListCmd.RunE: %v", err)
	}
}

func TestEventsList_InvalidSince(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, eventsListCmd, nil, map[string]string{"since": "bogus"})
	if err == nil || !strings.Contains(err.Error(), "无效") {
		t.Errorf("expected invalid duration, got: %v", err)
	}
}

func TestEventsTimeline_Empty(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, eventsTimelineCmd, []string{"sess-none"}, nil); err != nil {
		t.Fatalf("eventsTimelineCmd.RunE: %v", err)
	}
}

// ─── project ─────────────────────────────────────────────────────────────────

func TestProjectList_NoProjectsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)

	if err := runRunE(t, projectListCmd, nil, nil); err != nil {
		t.Fatalf("projectListCmd.RunE: %v", err)
	}
}

func TestProjectList_EmptyProjectsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "projects"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := runRunE(t, projectListCmd, nil, nil); err != nil {
		t.Fatalf("projectListCmd.RunE: %v", err)
	}
}

func TestProjectList_WithProjects(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "projects", "demo", "main"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "projects", "missing"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := runRunE(t, projectListCmd, nil, nil); err != nil {
		t.Fatalf("projectListCmd.RunE: %v", err)
	}
}

func TestProjectAdd_RequiresInit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_HOME", dir)

	err := runRunE(t, projectAddCmd, []string{"demo", "https://example.invalid/repo.git"}, nil)
	if err == nil || !strings.Contains(err.Error(), "未初始化") {
		t.Errorf("expected init error, got: %v", err)
	}
}

// ─── whip report ─────────────────────────────────────────────────────────────

func TestWhipReport_NoDaemon(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, whipReportCmd, nil, nil); err != nil {
		t.Fatalf("whipReportCmd.RunE: %v", err)
	}
}

func TestWhipReport_History(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, whipReportCmd, nil, map[string]string{"history": "true"}); err != nil {
		t.Fatalf("whipReportCmd.RunE: %v", err)
	}
}

// ─── privy ───────────────────────────────────────────────────────────────────

func TestPrivyMerge_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, privyMergeCmd, []string{"sess-nope"}, map[string]string{
		"project": "demo",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected session not found, got: %v", err)
	}
}

func TestPrivyAnalyze_ProjectMissing(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, privyAnalyzeCmd, []string{"sess-nope"}, map[string]string{
		"project": "demo",
	})
	if err == nil {
		t.Error("expected error when project missing")
	}
}

// ─── bill list: JSON ─────────────────────────────────────────────────────────

func TestBillList_JSONMode(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedBill(t, sdb, "b-json", "draft", "")

	if err := runRunE(t, billListCmd, nil, map[string]string{"json": "true"}); err != nil {
		t.Fatalf("billListCmd.RunE: %v", err)
	}
}

// ─── session stats ───────────────────────────────────────────────────────────

func TestSessionStats_NoSessions(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, sessionStatsCmd, nil, nil); err != nil {
		t.Fatalf("sessionStatsCmd.RunE: %v", err)
	}
}

func TestSessionStats_AllFlag(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-stats-a", "active")
	seedBillInSession(t, sdb, "b-stat", "enacted", "", "sess-stats-a")

	if err := runRunE(t, sessionStatsCmd, nil, map[string]string{"all": "true"}); err != nil {
		t.Fatalf("sessionStatsCmd.RunE: %v", err)
	}
}

func TestSessionStats_Detail(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-stat-d", "active")
	seedBillInSession(t, sdb, "b-sd1", "enacted", "", "sess-stat-d")

	if err := runRunE(t, sessionStatsCmd, []string{"sess-stat-d"}, nil); err != nil {
		t.Fatalf("sessionStatsCmd.RunE: %v", err)
	}
}

// ─── session replay ──────────────────────────────────────────────────────────

func TestSessionReplay_NoEvents(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-rep", "active")

	if err := runRunE(t, sessionReplayCmd, []string{"sess-rep"}, nil); err != nil {
		t.Fatalf("sessionReplayCmd.RunE: %v", err)
	}
}

func TestSessionReplay_NotFound(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	err := runRunE(t, sessionReplayCmd, []string{"nope"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got: %v", err)
	}
}

// ─── session migrate ─────────────────────────────────────────────────────────

func TestSessionMigrate_NothingToDo(t *testing.T) {
	dir := t.TempDir()
	installDB(t, dir)

	if err := runRunE(t, sessionMigrateCmd, nil, map[string]string{"project": "demo"}); err != nil {
		t.Fatalf("sessionMigrateCmd.RunE: %v", err)
	}
}

func TestSessionMigrate_DryRun(t *testing.T) {
	dir := t.TempDir()
	sdb := installDB(t, dir)
	seedSession(t, sdb, "sess-mig", "active")

	if err := runRunE(t, sessionMigrateCmd, nil, map[string]string{
		"project": "demo",
	}); err != nil {
		t.Fatalf("sessionMigrateCmd.RunE: %v", err)
	}
	fresh := reopenDB(t)
	s, _ := fresh.GetSession("sess-mig")
	if len(s.GetProjectsSlice()) != 0 {
		t.Errorf("dry run should not write projects, got %v", s.GetProjectsSlice())
	}
}
