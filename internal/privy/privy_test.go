package privy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─── parseConflictFiles tests ──────────────────────────────────────────────────

func TestParseConflictFiles_Empty(t *testing.T) {
	output := ""
	files := parseConflictFiles(output)
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestParseConflictFiles_NoConflict(t *testing.T) {
	output := `Already up to date.
Auto-merging README.md
Merge made by the 'recursive' strategy.`
	files := parseConflictFiles(output)
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestParseConflictFiles_SingleConflict(t *testing.T) {
	output := `Auto-merging api/auth.go
CONFLICT (content): Merge conflict in api/auth.go
Auto-merging models/user.go
CONFLICT (delete/modify): Merge conflict in models/user.go`
	files := parseConflictFiles(output)
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(files), files)
	}
	if files[0] != "api/auth.go" {
		t.Errorf("expected 'api/auth.go', got '%s'", files[0])
	}
	if files[1] != "models/user.go" {
		t.Errorf("expected 'models/user.go', got '%s'", files[1])
	}
}

func TestParseConflictFiles_MultipleConflicts(t *testing.T) {
	output := `CONFLICT (content): Merge conflict in src/a.go
CONFLICT (content): Merge conflict in src/b.go
CONFLICT (content): Merge conflict in src/c.go
Automatic merge failed; fix conflicts and then commit the result.`
	files := parseConflictFiles(output)
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestParseConflictFiles_Whitespace(t *testing.T) {
	output := `CONFLICT   (content):  Merge conflict  in  path/to/file.go  `
	files := parseConflictFiles(output)
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

// ─── BillBranch tests ───────────────────────────────────────────────────────────

func TestBillBranch_Fields(t *testing.T) {
	bb := BillBranch{
		BillID: "bill-123",
		Branch: "feature/auth",
		Title:  "Add authentication",
	}
	if bb.BillID != "bill-123" {
		t.Errorf("BillID mismatch")
	}
	if bb.Branch != "feature/auth" {
		t.Errorf("Branch mismatch")
	}
	if bb.Title != "Add authentication" {
		t.Errorf("Title mismatch")
	}
}

// ─── MergeResult tests ───────────────────────────────────────────────────────────

func TestMergeResult_Success(t *testing.T) {
	result := &MergeResult{
		Success:     true,
		MergeBranch: "privy/merge-1234567890",
		MergedBills: []string{"bill-1", "bill-2"},
		Message:     "Success",
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if len(result.MergedBills) != 2 {
		t.Errorf("expected 2 merged bills, got %d", len(result.MergedBills))
	}
}

func TestMergeResult_Conflict(t *testing.T) {
	result := &MergeResult{
		Success:       false,
		MergeBranch:   "",
		ConflictFiles: []string{"api/auth.go", "models/user.go"},
		ConflictBills: []string{"bill-1"},
		Message:       "Merge conflict",
	}
	if result.Success {
		t.Error("expected Success to be false")
	}
	if len(result.ConflictFiles) != 2 {
		t.Errorf("expected 2 conflict files, got %d", len(result.ConflictFiles))
	}
}

// ─── MainRepoPath tests ─────────────────────────────────────────────────────────

func TestMainRepoPath(t *testing.T) {
	tests := []struct {
		name     string
		hocDir   string
		project  string
		expected string
	}{
		{
			name:     "standard path",
			hocDir:   "/home/user/.hoc",
			project:  "myapp",
			expected: "/home/user/.hoc/projects/myapp/main",
		},
		{
			name:     "another project",
			hocDir:   "/data/hoc",
			project:  "webapp",
			expected: "/data/hoc/projects/webapp/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MainRepoPath(tt.hocDir, tt.project)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// ─── MainRepoFromWorktree tests ────────────────────────────────────────────────

func TestMainRepoFromWorktree_ValidPath(t *testing.T) {
	worktree := "/home/user/.hoc/projects/myapp/chambers/backend-claude"
	result := MainRepoFromWorktree(worktree)
	expected := "/home/user/.hoc/projects/myapp/main"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestMainRepoFromWorktree_DeepPath(t *testing.T) {
	worktree := "/data/hoc/projects/webapp/chambers/frontend-cursor"
	result := MainRepoFromWorktree(worktree)
	expected := "/data/hoc/projects/webapp/main"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestMainRepoFromWorktree_InvalidPath(t *testing.T) {
	worktree := "/random/path/nowhere"
	result := MainRepoFromWorktree(worktree)
	if result != "" {
		t.Errorf("expected empty result for invalid path, got %q", result)
	}
}

func TestMainRepoFromWorktree_RootPath(t *testing.T) {
	worktree := "/"
	result := MainRepoFromWorktree(worktree)
	if result != "" {
		t.Errorf("expected empty result for root path, got %q", result)
	}
}

// ─── detectConflictType tests ─────────────────────────────────────────────────

func TestDetectConflictType_Content(t *testing.T) {
	output := `Auto-merging api/auth.go
CONFLICT (content): Merge conflict in api/auth.go`
	typ := detectConflictType(output, "api/auth.go")
	if typ != "content" {
		t.Errorf("expected 'content', got %q", typ)
	}
}

func TestDetectConflictType_DeleteVsModify(t *testing.T) {
	output := `CONFLICT (modify/delete): api/auth.go deleted in HEAD and modified in feature-branch. Version feature-branch of api/auth.go left in tree.`
	typ := detectConflictType(output, "api/auth.go")
	if typ != "delete_vs_modify" {
		t.Errorf("expected 'delete_vs_modify', got %q", typ)
	}
}

func TestDetectConflictType_BothAdded(t *testing.T) {
	output := `CONFLICT (add/add): Merge conflict in new-file.go`
	typ := detectConflictType(output, "new-file.go")
	if typ != "both_added" {
		t.Errorf("expected 'both_added', got %q", typ)
	}
}

func TestDetectConflictType_UnknownFile(t *testing.T) {
	output := `CONFLICT (content): Merge conflict in api/auth.go`
	// File not in output → default "content"
	typ := detectConflictType(output, "other/file.go")
	if typ != "content" {
		t.Errorf("expected default 'content' for unknown file, got %q", typ)
	}
}

func TestDetectConflictType_EmptyOutput(t *testing.T) {
	typ := detectConflictType("", "any/file.go")
	if typ != "content" {
		t.Errorf("expected 'content' for empty output, got %q", typ)
	}
}

// ─── FormatConflictGazette tests ──────────────────────────────────────────────

func TestFormatConflictGazette_Basic(t *testing.T) {
	infos := []ConflictInfo{
		{File: "api/auth.go", Blocks: 2, Type: "content"},
		{File: "models/user.go", Blocks: 1, Type: "delete_vs_modify"},
	}
	strategies := []string{"策略 1: git merge --no-ff → 冲突", "策略 2: git merge -X theirs → 冲突"}

	result := FormatConflictGazette("bill-123", "认证模块", "backend-claude", infos, strategies, "需人工仲裁")

	if !strings.Contains(result, "bill-123") {
		t.Error("expected bill ID in gazette")
	}
	if !strings.Contains(result, "认证模块") {
		t.Error("expected bill title in gazette")
	}
	if !strings.Contains(result, "backend-claude") {
		t.Error("expected resolver minister in gazette")
	}
	if !strings.Contains(result, "api/auth.go") {
		t.Error("expected conflict file in gazette")
	}
	if !strings.Contains(result, "2 块冲突") {
		t.Error("expected conflict block count in gazette")
	}
	if !strings.Contains(result, "需人工仲裁") {
		t.Error("expected notes in gazette")
	}
}

func TestFormatConflictGazette_NoConflicts(t *testing.T) {
	result := FormatConflictGazette("bill-456", "空测试", "minister-a", nil, nil, "")

	if !strings.Contains(result, "bill-456") {
		t.Error("expected bill ID in gazette")
	}
	// No conflict files section when infos is empty
	if strings.Contains(result, "## 冲突文件") {
		t.Error("should not have 冲突文件 section when no conflicts")
	}
}

func TestFormatConflictGazette_NoNotes(t *testing.T) {
	infos := []ConflictInfo{{File: "main.go", Blocks: 1, Type: "content"}}
	result := FormatConflictGazette("b1", "test", "m1", infos, nil, "")

	if strings.Contains(result, "## 遗留问题") {
		t.Error("should not have 遗留问题 section when notes is empty")
	}
}

func TestFormatConflictGazette_ZeroBlocks(t *testing.T) {
	infos := []ConflictInfo{{File: "main.go", Blocks: 0, Type: "content"}}
	result := FormatConflictGazette("b1", "test", "m1", infos, nil, "")

	// Zero blocks should not print "0 块冲突"
	if strings.Contains(result, "0 块冲突") {
		t.Error("zero blocks should not be printed")
	}
}

// ─── MergeSession git integration tests ──────────────────────────────────────

// initTestRepo creates a temporary git repo with an initial commit on "main".
// Returns the repo path and a gitCmd helper.
func initTestRepo(t *testing.T) (string, func(...string) (string, error)) {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	cmds := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@hoc.test"},
		{"config", "user.name", "HoC Test"},
	}
	for _, c := range cmds {
		if out, err := run(c...); err != nil {
			t.Fatalf("git %v: %v\n%s", c, err, out)
		}
	}

	// Initial commit on main.
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# House of Cards\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, c := range [][]string{
		{"add", "."},
		{"commit", "-m", "initial"},
	} {
		if out, err := run(c...); err != nil {
			t.Fatalf("git %v: %v\n%s", c, err, out)
		}
	}

	return dir, run
}

func TestMergeSession_NoBills(t *testing.T) {
	dir, _ := initTestRepo(t)

	result, err := MergeSession(dir, nil, "main")
	if err != nil {
		t.Fatalf("MergeSession: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success with no bills, got: %s", result.Message)
	}
	if len(result.MergedBills) != 0 {
		t.Errorf("expected 0 merged bills, got %d", len(result.MergedBills))
	}
}

func TestMergeSession_InvalidRepo(t *testing.T) {
	_, err := MergeSession("/nonexistent/path", nil, "main")
	if err == nil {
		t.Error("expected error for nonexistent repo path")
	}
}

func TestMergeSession_SingleBranchSuccess(t *testing.T) {
	dir, run := initTestRepo(t)

	// Create a feature branch with a new file.
	for _, c := range [][]string{
		{"checkout", "-b", "feature/bill-001"},
	} {
		if out, err := run(c...); err != nil {
			t.Fatalf("git %v: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write feature.go: %v", err)
	}
	for _, c := range [][]string{
		{"add", "."},
		{"commit", "-m", "add feature"},
	} {
		if out, err := run(c...); err != nil {
			t.Fatalf("git %v: %v\n%s", c, err, out)
		}
	}

	bills := []BillBranch{
		{BillID: "bill-001", Branch: "feature/bill-001", Title: "Add feature"},
	}

	result, err := MergeSession(dir, bills, "main")
	if err != nil {
		t.Fatalf("MergeSession: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
	if len(result.MergedBills) != 1 || result.MergedBills[0] != "bill-001" {
		t.Errorf("expected [bill-001] merged, got %v", result.MergedBills)
	}
	if result.MergeBranch == "" {
		t.Error("expected non-empty merge branch")
	}
}

func TestMergeSession_EmptyBranchSkipped(t *testing.T) {
	dir, _ := initTestRepo(t)

	bills := []BillBranch{
		{BillID: "bill-001", Branch: "", Title: "No branch bill"},
	}

	result, err := MergeSession(dir, bills, "main")
	if err != nil {
		t.Fatalf("MergeSession: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success (empty branch bills are skipped), got: %s", result.Message)
	}
	if len(result.MergedBills) != 0 {
		t.Errorf("expected 0 merged (empty branch skipped), got %v", result.MergedBills)
	}
}

func TestMergeSession_DetectDefaultBranch(t *testing.T) {
	dir, _ := initTestRepo(t)

	// Pass empty baseBranch — should auto-detect "main"
	result, err := MergeSession(dir, nil, "")
	if err != nil {
		t.Fatalf("MergeSession: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success with auto-detected branch, got: %s", result.Message)
	}
}
