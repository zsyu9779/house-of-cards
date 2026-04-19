package minister

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/house-of-cards/hoc/internal/runtime"
	"github.com/house-of-cards/hoc/internal/store"
)

// ─── Test doubles ────────────────────────────────────────────────────────────

// stubRuntime is an in-memory Runtime that records calls and returns a
// configurable outcome. It never launches tmux sessions or subprocesses.
type stubRuntime struct {
	summonErr    error
	summonCalls  int
	dismissCalls int
	pid          int
}

func (s *stubRuntime) Summon(opts runtime.SummonOpts) (*runtime.AgentSession, error) {
	s.summonCalls++
	if s.summonErr != nil {
		return nil, s.summonErr
	}
	return &runtime.AgentSession{
		MinisterID:  opts.MinisterID,
		PID:         s.pid,
		ChamberPath: opts.ChamberPath,
	}, nil
}

func (s *stubRuntime) IsSeated(*runtime.AgentSession) bool { return true }
func (s *stubRuntime) Dismiss(*runtime.AgentSession) error {
	s.dismissCalls++
	return nil
}
func (s *stubRuntime) Dispatch(*runtime.AgentSession, string) error { return nil }

// withStubRuntime swaps the package-level runtime factory for the duration of
// the calling test and returns the stub it installed.
func withStubRuntime(t *testing.T, stub *stubRuntime) {
	t.Helper()
	orig := runtimeFactory
	runtimeFactory = func(string, bool) runtime.Runtime { return stub }
	t.Cleanup(func() { runtimeFactory = orig })
}

// ─── Fixtures ────────────────────────────────────────────────────────────────

// newTestHoc creates a temp hocDir with an initialised git project and returns
// its absolute path plus the project name. The project has one commit so git
// worktree add succeeds.
func newTestHoc(t *testing.T) (hocDir, project string) {
	t.Helper()
	hocDir = t.TempDir()
	project = "proj"
	mainRepo := filepath.Join(hocDir, "projects", project, "main")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = mainRepo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("commit", "--allow-empty", "-q", "-m", "init")
	return hocDir, project
}

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedMinisterBill(t *testing.T, db *store.DB, mID, bID string) (*store.Minister, *store.Bill) {
	t.Helper()
	m := &store.Minister{
		ID:      mID,
		Title:   "Test Minister",
		Runtime: "claude-code",
		Skills:  `["backend"]`,
		Status:  "idle",
	}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	b := &store.Bill{
		ID:     bID,
		Title:  "Test Bill",
		Status: "reading",
	}
	if err := db.CreateBill(b); err != nil {
		t.Fatalf("CreateBill: %v", err)
	}
	return m, b
}

// ─── Summon tests ────────────────────────────────────────────────────────────

func TestSummon_HappyPath(t *testing.T) {
	hocDir, project := newTestHoc(t)
	db := newTestDB(t)
	seedMinisterBill(t, db, "m-ok", "bill-ok")

	stub := &stubRuntime{pid: 4242}
	withStubRuntime(t, stub)

	res, err := Summon(SummonOpts{
		DB: db, HocDir: hocDir,
		MinisterID: "m-ok", BillID: "bill-ok",
		ProjectName: project, UseTmux: false,
	})
	if err != nil {
		t.Fatalf("Summon: %v", err)
	}
	if res.PID != 4242 {
		t.Errorf("PID: got %d, want 4242", res.PID)
	}
	if res.Reused {
		t.Error("expected fresh chamber (Reused=false)")
	}
	if !strings.HasSuffix(res.Branch, "m-ok") {
		t.Errorf("Branch should reference minister id, got %q", res.Branch)
	}

	// Chamber must exist on disk with CLAUDE.md written.
	claudePath := filepath.Join(res.Worktree, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("CLAUDE.md missing: %v", err)
	}
	if !strings.Contains(string(data), "bill-ok") {
		t.Error("CLAUDE.md should mention the bill id")
	}

	// DB side effects: minister working, bill assigned + branch.
	m, _ := db.GetMinister("m-ok")
	if m.Status != "working" {
		t.Errorf("minister status: got %q, want working", m.Status)
	}
	if m.Worktree.String != res.Worktree {
		t.Errorf("minister worktree not updated: got %q", m.Worktree.String)
	}
	if m.Pid != 4242 {
		t.Errorf("minister pid: got %d, want 4242", m.Pid)
	}
	b, _ := db.GetBill("bill-ok")
	if b.Assignee.String != "m-ok" {
		t.Errorf("bill assignee: got %q, want m-ok", b.Assignee.String)
	}
	if b.Branch.String != res.Branch {
		t.Errorf("bill branch: got %q, want %q", b.Branch.String, res.Branch)
	}
}

func TestSummon_ReusesExistingChamber(t *testing.T) {
	hocDir, project := newTestHoc(t)
	db := newTestDB(t)
	seedMinisterBill(t, db, "m-reuse", "bill-reuse")

	withStubRuntime(t, &stubRuntime{pid: 1})

	first, err := Summon(SummonOpts{DB: db, HocDir: hocDir, MinisterID: "m-reuse", BillID: "bill-reuse", ProjectName: project})
	if err != nil {
		t.Fatalf("first Summon: %v", err)
	}
	if first.Reused {
		t.Fatal("first summon should not report Reused=true")
	}

	second, err := Summon(SummonOpts{DB: db, HocDir: hocDir, MinisterID: "m-reuse", BillID: "bill-reuse", ProjectName: project})
	if err != nil {
		t.Fatalf("second Summon: %v", err)
	}
	if !second.Reused {
		t.Error("second summon should report Reused=true when chamber already exists")
	}
	if second.Worktree != first.Worktree {
		t.Error("reused chamber should return the same worktree path")
	}
}

func TestSummon_MissingMinister(t *testing.T) {
	hocDir, project := newTestHoc(t)
	db := newTestDB(t)
	b := &store.Bill{ID: "b-orphan", Title: "Orphan", Status: "reading"}
	if err := db.CreateBill(b); err != nil {
		t.Fatalf("CreateBill: %v", err)
	}
	withStubRuntime(t, &stubRuntime{})

	_, err := Summon(SummonOpts{DB: db, HocDir: hocDir, MinisterID: "ghost", BillID: "b-orphan", ProjectName: project})
	if err == nil {
		t.Fatal("expected error for missing minister")
	}
	if !strings.Contains(err.Error(), "minister not found") {
		t.Errorf("error should mention minister: %v", err)
	}
}

func TestSummon_MissingBill(t *testing.T) {
	hocDir, project := newTestHoc(t)
	db := newTestDB(t)
	m := &store.Minister{ID: "m-noBill", Title: "T", Runtime: "claude-code", Status: "idle"}
	if err := db.CreateMinister(m); err != nil {
		t.Fatalf("CreateMinister: %v", err)
	}
	withStubRuntime(t, &stubRuntime{})

	_, err := Summon(SummonOpts{DB: db, HocDir: hocDir, MinisterID: "m-noBill", BillID: "ghost", ProjectName: project})
	if err == nil || !strings.Contains(err.Error(), "bill not found") {
		t.Errorf("expected bill not found, got %v", err)
	}
}

func TestSummon_MissingProject(t *testing.T) {
	hocDir := t.TempDir() // no project directory created
	db := newTestDB(t)
	seedMinisterBill(t, db, "m-np", "bill-np")
	withStubRuntime(t, &stubRuntime{})

	_, err := Summon(SummonOpts{DB: db, HocDir: hocDir, MinisterID: "m-np", BillID: "bill-np", ProjectName: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("expected project-missing error, got %v", err)
	}
}

// TestSummon_FailureRollback verifies that when runtime.Summon fails, the
// chamber worktree is removed and DB fields (status/worktree/branch/assignee)
// are not mutated.
func TestSummon_FailureRollback(t *testing.T) {
	hocDir, project := newTestHoc(t)
	db := newTestDB(t)
	seedMinisterBill(t, db, "m-fail", "bill-fail")

	stub := &stubRuntime{summonErr: errors.New("runtime boom")}
	withStubRuntime(t, stub)

	res, err := Summon(SummonOpts{DB: db, HocDir: hocDir, MinisterID: "m-fail", BillID: "bill-fail", ProjectName: project})
	if err == nil {
		t.Fatal("expected error")
	}
	if res != nil {
		t.Error("result should be nil on failure")
	}

	// Chamber worktree should no longer exist (rolled back).
	worktree := filepath.Join(hocDir, "projects", project, "chambers", "m-fail")
	if _, statErr := os.Stat(worktree); !os.IsNotExist(statErr) {
		t.Errorf("chamber worktree should be removed on rollback: stat err = %v", statErr)
	}

	// DB state: minister status unchanged (still idle), no bill branch assigned.
	m, _ := db.GetMinister("m-fail")
	if m.Status != "idle" {
		t.Errorf("minister status should remain idle after rollback, got %q", m.Status)
	}
	b, _ := db.GetBill("bill-fail")
	if b.Branch.String != "" {
		t.Errorf("bill branch should remain empty after rollback, got %q", b.Branch.String)
	}
	if b.Assignee.String != "" {
		t.Errorf("bill assignee should remain empty after rollback, got %q", b.Assignee.String)
	}
}

func TestSummon_DBNilReturnsError(t *testing.T) {
	_, err := Summon(SummonOpts{MinisterID: "x", BillID: "y", ProjectName: "p"})
	if err == nil || !strings.Contains(err.Error(), "db is required") {
		t.Errorf("expected db-required error, got %v", err)
	}
}

// ─── Brief tests ─────────────────────────────────────────────────────────────

func TestBuildBillBrief_IncludesMinisterAndBill(t *testing.T) {
	m := &store.Minister{ID: "m1", Title: "Backend", Skills: `["go","sql"]`}
	b := &store.Bill{ID: "bill-1", Title: "Do the thing", Status: "reading"}
	out := BuildBillBrief(m, b, "minister/m1")
	for _, want := range []string{"Backend", "m1", "bill-1", "Do the thing", "minister/m1", "go, sql"} {
		if !strings.Contains(out, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

func TestBuildBillBrief_FallsBackToTitle(t *testing.T) {
	m := &store.Minister{ID: "m1", Title: "T"}
	b := &store.Bill{ID: "b1", Title: "Only title"}
	// Description is zero sql.NullString — brief should substitute title.
	out := BuildBillBrief(m, b, "minister/m1")
	if !strings.Contains(out, "Only title") {
		t.Error("brief should fall back to bill.Title when Description is empty")
	}
}

func TestBuildBillBrief_GenericWhenNoSkills(t *testing.T) {
	m := &store.Minister{ID: "m1", Title: "T"}
	b := &store.Bill{ID: "b1", Title: "Title"}
	out := BuildBillBrief(m, b, "minister/m1")
	if !strings.Contains(out, "通用") {
		t.Error("brief should label skills as 通用 when minister has no skills")
	}
}

func TestFormatUpstreamGazette_PayloadRendered(t *testing.T) {
	payload := `{"summary":"done","contracts":{"api.go":"UserService"},"artifacts":{"x.go":"新增"}}`
	g := &store.Gazette{
		Type:         sql.NullString{String: "completion", Valid: true},
		FromMinister: sql.NullString{String: "upstream", Valid: true},
		Summary:      "raw fallback",
		Payload:      payload,
	}
	out := FormatUpstreamGazette(g)
	for _, want := range []string{"completion", "upstream", "done", "api.go", "UserService", "x.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "raw fallback") {
		t.Error("structured payload should take precedence over raw summary")
	}
}

func TestFormatUpstreamGazette_FallsBackToSummary(t *testing.T) {
	g := &store.Gazette{
		Type:    sql.NullString{String: "handoff", Valid: true},
		Summary: "just a summary",
	}
	out := FormatUpstreamGazette(g)
	if !strings.Contains(out, "just a summary") {
		t.Error("should fall back to Summary when Payload is empty")
	}
}
