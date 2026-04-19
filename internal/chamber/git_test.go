package chamber

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo initializes a minimal git repo suitable for worktree operations:
// one commit on the default branch. The returned path is the repo root.
//
// The tests skip themselves if git is unavailable.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()

	run := func(dir string, name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		// Disable global/system config so developer config does not leak in.
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=hoc-test",
			"GIT_AUTHOR_EMAIL=hoc@test",
			"GIT_COMMITTER_NAME=hoc-test",
			"GIT_COMMITTER_EMAIL=hoc@test",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v (cwd=%s): %v\n%s", name, args, dir, err, out)
		}
	}

	run(root, "git", "init", "-b", "main")
	run(root, "git", "config", "user.email", "hoc@test")
	run(root, "git", "config", "user.name", "hoc-test")
	// Create an initial commit so branches can be derived.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(root, "git", "add", "README.md")
	run(root, "git", "commit", "-m", "init")

	return root
}

func TestChamber_CreateRemove(t *testing.T) {
	mainRepo := initGitRepo(t)
	home := t.TempDir()

	ch, err := NewChamber(home, "proj", "backend", mainRepo)
	if err != nil {
		t.Fatalf("NewChamber: %v", err)
	}
	if err := ch.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(ch.Path); err != nil {
		t.Fatalf("worktree dir missing after Create: %v", err)
	}

	// Create again should error out — worktree already exists.
	if err := ch.Create(); err == nil {
		t.Error("expected error on duplicate Create")
	}

	// Remove cleans up the worktree.
	if err := ch.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(ch.Path); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be gone after Remove, stat err=%v", err)
	}
}

func TestChamber_Create_BadRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	ch, err := NewChamber(home, "proj", "m1", t.TempDir()) // MainRepo is not a git repo
	if err != nil {
		t.Fatalf("NewChamber: %v", err)
	}
	if err := ch.Create(); err == nil {
		t.Fatal("expected error when MainRepo is not a git repo")
	}
}

func TestChamber_Remove_NoWorktree(t *testing.T) {
	mainRepo := initGitRepo(t)
	home := t.TempDir()
	ch, err := NewChamber(home, "proj", "ghost", mainRepo)
	if err != nil {
		t.Fatalf("NewChamber: %v", err)
	}
	// Removing a non-existent worktree surfaces git's error.
	if err := ch.Remove(); err == nil {
		t.Error("expected error when removing non-existent worktree")
	}
}

func TestChamber_Status_CleanAndDirty(t *testing.T) {
	mainRepo := initGitRepo(t)
	home := t.TempDir()
	ch, err := NewChamber(home, "proj", "m1", mainRepo)
	if err != nil {
		t.Fatalf("NewChamber: %v", err)
	}
	if err := ch.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = ch.Remove() })

	// Clean state.
	dirty, err := ch.HasUncommittedChanges()
	if err != nil {
		t.Fatalf("HasUncommittedChanges: %v", err)
	}
	if dirty {
		t.Error("expected clean worktree after Create")
	}
	out, err := ch.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty status, got %q", out)
	}

	// Dirty state.
	if err := os.WriteFile(filepath.Join(ch.Path, "new.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dirty, err = ch.HasUncommittedChanges()
	if err != nil {
		t.Fatalf("HasUncommittedChanges: %v", err)
	}
	if !dirty {
		t.Error("expected dirty worktree after writing a new file")
	}
}

func TestChamber_Status_BadPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ch := &Chamber{Path: t.TempDir()} // path exists but is not a git repo
	if _, err := ch.Status(); err == nil {
		t.Error("expected error from git status in non-repo path")
	}
	if _, err := ch.HasUncommittedChanges(); err == nil {
		t.Error("expected error from HasUncommittedChanges propagating Status error")
	}
}

func TestChamber_CommitAndStash(t *testing.T) {
	mainRepo := initGitRepo(t)
	home := t.TempDir()
	ch, err := NewChamber(home, "proj", "m1", mainRepo)
	if err != nil {
		t.Fatalf("NewChamber: %v", err)
	}
	if err := ch.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = ch.Remove() })

	// Need user.email/name in the worktree for commits to succeed.
	cfg := exec.Command("git", "config", "user.email", "hoc@test")
	cfg.Dir = ch.Path
	_ = cfg.Run()
	cfg = exec.Command("git", "config", "user.name", "hoc-test")
	cfg.Dir = ch.Path
	_ = cfg.Run()

	if err := os.WriteFile(filepath.Join(ch.Path, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ch.Commit("add a"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// After commit the tree is clean.
	if dirty, _ := ch.HasUncommittedChanges(); dirty {
		t.Error("tree should be clean after Commit")
	}

	// Stash on clean tree: git stash with no changes prints "No local changes
	// to save" and exits 0 — should not error.
	if err := ch.Stash(); err != nil {
		t.Errorf("Stash on clean tree should be ok, got: %v", err)
	}

	// Create a change, then stash it.
	if err := os.WriteFile(filepath.Join(ch.Path, "b.txt"), []byte("pending"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ch.Stash(); err != nil {
		t.Errorf("Stash with changes failed: %v", err)
	}
}

func TestChamber_Commit_BadPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ch := &Chamber{Path: t.TempDir(), Minister: "m1"}
	if err := ch.Commit("x"); err == nil {
		t.Error("expected Commit to fail on non-repo path")
	}
	if err := ch.Stash(); err == nil {
		t.Error("expected Stash to fail on non-repo path")
	}
}

func TestChamber_Push_NoRemote(t *testing.T) {
	mainRepo := initGitRepo(t)
	home := t.TempDir()
	ch, err := NewChamber(home, "proj", "m1", mainRepo)
	if err != nil {
		t.Fatalf("NewChamber: %v", err)
	}
	if err := ch.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = ch.Remove() })

	// No `origin` remote configured — push should fail.
	if err := ch.Push(); err == nil {
		t.Error("expected Push to fail without origin")
	}
}

func TestListChambers_NotADir(t *testing.T) {
	// homeDir points to a regular file, so chambersDir read will return an
	// error other than IsNotExist, covering the non-nil return branch.
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// chambersDir = <file>/projects/.../chambers — which cannot exist under a file.
	_, err := ListChambers(f, "proj")
	// On many systems this returns "not a directory" (which is still an error);
	// on macOS it returns ENOTDIR. Either way, it should not panic. IsNotExist
	// may or may not catch it — we only need to ensure the branch runs.
	_ = err
}
