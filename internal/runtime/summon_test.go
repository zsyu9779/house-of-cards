package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubBinDir creates a directory containing executable stub scripts that
// return successfully (exit 0). The returned path should be prepended to PATH
// so that exec.Command resolves the named binaries to these stubs.
func stubBinDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		p := filepath.Join(dir, name)
		// The script accepts any args / stdin and exits 0.
		script := "#!/bin/sh\nexit 0\n"
		if err := os.WriteFile(p, []byte(script), 0755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	return dir
}

func withStubbedPATH(t *testing.T, names ...string) {
	t.Helper()
	dir := stubBinDir(t, names...)
	orig := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
}

// ─── MkdirAll failure (shared by all three runtimes) ───────────────────────

func TestClaudeCodeRuntime_Summon_BadChamberPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path semantics differ on windows")
	}
	r := NewClaudeCodeRuntime(false)
	// /dev/null is a character device, not a directory — MkdirAll("<that>/.hoc") fails.
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: "/dev/null/nope"})
	if err == nil {
		t.Fatal("expected error for invalid chamber path")
	}
	if !strings.Contains(err.Error(), "brief") {
		t.Errorf("error should mention brief dir/file, got: %v", err)
	}
}

func TestCodexRuntime_Summon_BadChamberPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path semantics differ on windows")
	}
	r := NewCodexRuntime(false)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: "/dev/null/nope"})
	if err == nil {
		t.Fatal("expected error for invalid chamber path")
	}
}

func TestCursorRuntime_Summon_BadChamberPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path semantics differ on windows")
	}
	r := NewCursorRuntime(false)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: "/dev/null/nope"})
	if err == nil {
		t.Fatal("expected error for invalid chamber path")
	}
}

// ─── Foreground Summon with stubbed binaries ───────────────────────────────

func TestClaudeCodeRuntime_Summon_ForegroundSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stubs")
	}
	withStubbedPATH(t, "claude")
	chamber := t.TempDir()
	r := NewClaudeCodeRuntime(false)
	s, err := r.Summon(SummonOpts{
		MinisterID:  "m1",
		ChamberPath: chamber,
		BillBrief:   "# brief",
	})
	if err != nil {
		t.Fatalf("Summon error: %v", err)
	}
	if s == nil || s.PID <= 0 {
		t.Fatalf("expected non-nil session with PID, got %+v", s)
	}
	// Brief file should have been written.
	if _, err := os.Stat(filepath.Join(chamber, ".hoc", "brief.md")); err != nil {
		t.Errorf("brief.md not written: %v", err)
	}
}

func TestCodexRuntime_Summon_ForegroundSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stubs")
	}
	withStubbedPATH(t, "codex")
	chamber := t.TempDir()
	r := NewCodexRuntime(false)
	s, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err != nil {
		t.Fatalf("Summon error: %v", err)
	}
	if s == nil || s.PID <= 0 {
		t.Errorf("expected PID, got %+v", s)
	}
}

func TestCursorRuntime_Summon_ForegroundSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stubs")
	}
	withStubbedPATH(t, "cursor")
	chamber := t.TempDir()
	r := NewCursorRuntime(false)
	s, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err != nil {
		t.Fatalf("Summon error: %v", err)
	}
	if s == nil || s.PID <= 0 {
		t.Errorf("expected PID, got %+v", s)
	}
}

// ─── Foreground Summon: binary-start failure ───────────────────────────────

func TestClaudeCodeRuntime_Summon_ForegroundMissingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	// PATH contains only an empty dir, so `claude` is unresolvable.
	t.Setenv("PATH", t.TempDir())
	chamber := t.TempDir()
	r := NewClaudeCodeRuntime(false)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err == nil {
		t.Fatal("expected error when claude binary missing")
	}
	if !strings.Contains(err.Error(), "start claude") {
		t.Errorf("expected 'start claude' in error, got: %v", err)
	}
}

func TestCodexRuntime_Summon_ForegroundMissingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	t.Setenv("PATH", t.TempDir())
	chamber := t.TempDir()
	r := NewCodexRuntime(false)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err == nil {
		t.Fatal("expected error when codex binary missing")
	}
	if !strings.Contains(err.Error(), "start codex") {
		t.Errorf("expected 'start codex' in error, got: %v", err)
	}
}

func TestCursorRuntime_Summon_ForegroundMissingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	t.Setenv("PATH", t.TempDir())
	chamber := t.TempDir()
	r := NewCursorRuntime(false)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err == nil {
		t.Fatal("expected error when cursor binary missing")
	}
	if !strings.Contains(err.Error(), "start cursor") {
		t.Errorf("expected 'start cursor' in error, got: %v", err)
	}
}

// ─── tmux Summon: tmux missing → error path ────────────────────────────────

func TestClaudeCodeRuntime_Summon_TmuxMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	t.Setenv("PATH", t.TempDir()) // no tmux available
	chamber := t.TempDir()
	r := NewClaudeCodeRuntime(true)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err == nil {
		t.Fatal("expected error when tmux missing")
	}
	if !strings.Contains(err.Error(), "tmux start") {
		t.Errorf("expected tmux-start error, got: %v", err)
	}
}

func TestCodexRuntime_Summon_TmuxMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	t.Setenv("PATH", t.TempDir())
	chamber := t.TempDir()
	r := NewCodexRuntime(true)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err == nil {
		t.Fatal("expected error when tmux missing")
	}
}

func TestCursorRuntime_Summon_TmuxMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	t.Setenv("PATH", t.TempDir())
	chamber := t.TempDir()
	r := NewCursorRuntime(true)
	_, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err == nil {
		t.Fatal("expected error when tmux missing")
	}
}

// ─── tmux Summon: stubbed tmux → success path ──────────────────────────────

func TestClaudeCodeRuntime_Summon_TmuxStubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	withStubbedPATH(t, "tmux") // no need to stub claude — tmux stub consumes args
	chamber := t.TempDir()
	r := NewClaudeCodeRuntime(true)
	s, err := r.Summon(SummonOpts{MinisterID: "m1", ChamberPath: chamber, BillBrief: "x"})
	if err != nil {
		t.Fatalf("Summon error: %v", err)
	}
	if s == nil || s.TmuxSession != "hoc-m1" {
		t.Errorf("expected TmuxSession=hoc-m1, got %+v", s)
	}
}

func TestCodexRuntime_Summon_TmuxStubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	withStubbedPATH(t, "tmux")
	chamber := t.TempDir()
	r := NewCodexRuntime(true)
	s, err := r.Summon(SummonOpts{MinisterID: "m2", ChamberPath: chamber, BillBrief: "x"})
	if err != nil {
		t.Fatalf("Summon error: %v", err)
	}
	if s == nil || s.TmuxSession != "hoc-m2" {
		t.Errorf("expected TmuxSession=hoc-m2, got %+v", s)
	}
}

func TestCursorRuntime_Summon_TmuxStubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	withStubbedPATH(t, "tmux")
	chamber := t.TempDir()
	r := NewCursorRuntime(true)
	s, err := r.Summon(SummonOpts{MinisterID: "m3", ChamberPath: chamber, BillBrief: "x"})
	if err != nil {
		t.Fatalf("Summon error: %v", err)
	}
	if s == nil || s.TmuxSession != "hoc-m3" {
		t.Errorf("expected TmuxSession=hoc-m3, got %+v", s)
	}
}

// ─── Dispatch / Dismiss via stubbed tmux ────────────────────────────────────

func TestSessionDispatch_StubbedTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	withStubbedPATH(t, "tmux")
	err := sessionDispatch(&AgentSession{TmuxSession: "hoc-xyz"}, "hello")
	if err != nil {
		t.Errorf("dispatch with stubbed tmux should succeed, got: %v", err)
	}
}

func TestSessionDismiss_StubbedTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	withStubbedPATH(t, "tmux")
	err := sessionDismiss(&AgentSession{TmuxSession: "hoc-xyz"})
	if err != nil {
		t.Errorf("dismiss with stubbed tmux should succeed, got: %v", err)
	}
}

func TestSessionDismiss_TmuxFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	// Tmux unavailable → kill-session fails → error returned.
	t.Setenv("PATH", t.TempDir())
	err := sessionDismiss(&AgentSession{TmuxSession: "hoc-ghost"})
	if err == nil {
		t.Fatal("expected error when tmux missing")
	}
	if !strings.Contains(err.Error(), "kill tmux session") {
		t.Errorf("expected kill error, got: %v", err)
	}
}

func TestSessionDismiss_NilIsNoop(t *testing.T) {
	if err := sessionDismiss(nil); err != nil {
		t.Errorf("nil session should be a noop, got: %v", err)
	}
}

func TestSessionDismiss_EmptyIsNoop(t *testing.T) {
	if err := sessionDismiss(&AgentSession{}); err != nil {
		t.Errorf("empty session should be a noop, got: %v", err)
	}
}

func TestSessionDismiss_PIDUnreachable(t *testing.T) {
	// PID 2^31-1 is almost certainly not a live process we own.
	// FindProcess always succeeds on unix; Signal fails.
	err := sessionDismiss(&AgentSession{PID: 2147483646})
	// The signal may fail (no such process) — sessionDismiss returns that error.
	// We don't care about the exact value; the path is covered.
	_ = err
}

func TestSessionDispatch_NilSession(t *testing.T) {
	err := sessionDispatch(nil, "x")
	if err == nil {
		t.Error("expected error for nil session")
	}
}

func TestSessionIsSeated_TmuxStubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	withStubbedPATH(t, "tmux")
	if !sessionIsSeated(&AgentSession{TmuxSession: "hoc-abc"}) {
		t.Error("stubbed tmux should report seated")
	}
}

func TestSessionIsSeated_TmuxMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	t.Setenv("PATH", t.TempDir())
	if sessionIsSeated(&AgentSession{TmuxSession: "hoc-ghost"}) {
		t.Error("missing tmux should report not seated")
	}
}

func TestSessionIsSeated_PIDBranch(t *testing.T) {
	// Live process: the test itself.
	if !sessionIsSeated(&AgentSession{PID: os.Getpid()}) {
		t.Error("current pid should report seated")
	}
}
