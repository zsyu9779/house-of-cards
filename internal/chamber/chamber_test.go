package chamber

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── NewChamber tests ────────────────────────────────────────────────────────────

func TestNewChamber(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := tmpDir
	projectName := "myapp"
	ministerID := "backend-claude"
	mainRepo := filepath.Join(tmpDir, "projects", "myapp", "main")

	ch, err := NewChamber(homeDir, projectName, ministerID, mainRepo)
	if err != nil {
		t.Fatalf("NewChamber failed: %v", err)
	}

	if ch.Name != ministerID {
		t.Errorf("expected Name %q, got %q", ministerID, ch.Name)
	}
	if ch.Minister != ministerID {
		t.Errorf("expected Minister %q, got %q", ministerID, ch.Minister)
	}
	expectedPath := filepath.Join(tmpDir, "projects", "myapp", "chambers", "backend-claude")
	if ch.Path != expectedPath {
		t.Errorf("expected Path %q, got %q", expectedPath, ch.Path)
	}
	if ch.MainRepo != mainRepo {
		t.Errorf("expected MainRepo %q, got %q", mainRepo, ch.MainRepo)
	}
	expectedBranch := "minister/backend-claude"
	if ch.Branch != expectedBranch {
		t.Errorf("expected Branch %q, got %q", expectedBranch, ch.Branch)
	}
}

func TestNewChamber_DifferentIDs(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := tmpDir
	mainRepo := filepath.Join(tmpDir, "main")

	tests := []struct {
		ministerID     string
		expectedBranch string
	}{
		{"frontend-cursor", "minister/frontend-cursor"},
		{"reviewer-gemini", "minister/reviewer-gemini"},
		{"go-developer", "minister/go-developer"},
	}

	for _, tt := range tests {
		t.Run(tt.ministerID, func(t *testing.T) {
			ch, err := NewChamber(homeDir, "testproj", tt.ministerID, mainRepo)
			if err != nil {
				t.Fatalf("NewChamber failed: %v", err)
			}
			if ch.Branch != tt.expectedBranch {
				t.Errorf("expected Branch %q, got %q", tt.expectedBranch, ch.Branch)
			}
		})
	}
}

// ─── Chamber Getters tests ────────────────────────────────────────────────────────

func TestChamber_GetBranchName(t *testing.T) {
	ch := &Chamber{
		Name:   "test-minister",
		Branch: "minister/test-minister",
	}
	if ch.GetBranchName() != "minister/test-minister" {
		t.Error("GetBranchName failed")
	}
}

func TestChamber_GetWorktreePath(t *testing.T) {
	ch := &Chamber{
		Path: "/home/user/.hoc/projects/myapp/chambers/test",
	}
	if ch.GetWorktreePath() != "/home/user/.hoc/projects/myapp/chambers/test" {
		t.Error("GetWorktreePath failed")
	}
}

// ─── Chamber Structure tests ─────────────────────────────────────────────────────

func TestChamber_Structure(t *testing.T) {
	ch := &Chamber{
		Name:     "backend",
		Path:     "/path/to/chamber",
		Minister: "backend-minister",
		MainRepo: "/path/to/main",
		Branch:   "minister/backend",
	}

	// Verify all fields are accessible
	_ = ch.Name
	_ = ch.Path
	_ = ch.Minister
	_ = ch.MainRepo
	_ = ch.Branch

	// Verify methods return correct values
	if ch.GetBranchName() != ch.Branch {
		t.Error("GetBranchName should return Branch field")
	}
	if ch.GetWorktreePath() != ch.Path {
		t.Error("GetWorktreePath should return Path field")
	}
}

// ─── ListChambers tests ─────────────────────────────────────────────────────────

func TestListChambers_EmptyDir(t *testing.T) {
	// Create a temp directory without chambers
	tmpDir := t.TempDir()
	chambers, err := ListChambers(tmpDir, "nonexistent")
	if err != nil {
		t.Fatalf("ListChambers failed: %v", err)
	}
	if len(chambers) != 0 {
		t.Errorf("expected 0 chambers, got %d", len(chambers))
	}
}

func TestListChambers_WithSubdirs(t *testing.T) {
	tmpDir := t.TempDir()
	chambersDir := filepath.Join(tmpDir, "projects", "testproj", "chambers")
	if err := os.MkdirAll(chambersDir, 0755); err != nil {
		t.Fatalf("create chambers dir: %v", err)
	}

	// Create some chamber directories.
	if err := os.MkdirAll(filepath.Join(chambersDir, "backend-claude"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(chambersDir, "frontend-cursor"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(chambersDir, "reviewer"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a file (should be ignored).
	if err := os.WriteFile(filepath.Join(chambersDir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	chambers, err := ListChambers(tmpDir, "testproj")
	if err != nil {
		t.Fatalf("ListChambers failed: %v", err)
	}
	if len(chambers) != 3 {
		t.Errorf("expected 3 chambers, got %d", len(chambers))
	}

	// Verify chamber names
	expectedNames := map[string]bool{
		"backend-claude":  true,
		"frontend-cursor": true,
		"reviewer":        true,
	}
	for _, ch := range chambers {
		if !expectedNames[ch.Name] {
			t.Errorf("unexpected chamber: %s", ch.Name)
		}
		delete(expectedNames, ch.Name)
	}
	if len(expectedNames) > 0 {
		t.Errorf("missing chambers: %v", expectedNames)
	}
}

func TestListChambers_FileInChambers(t *testing.T) {
	tmpDir := t.TempDir()
	chambersDir := filepath.Join(tmpDir, "projects", "testproj", "chambers")
	if err := os.MkdirAll(chambersDir, 0755); err != nil {
		t.Fatalf("create chambers dir: %v", err)
	}

	// Create chamber directories.
	if err := os.MkdirAll(filepath.Join(chambersDir, "chamber1"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(chambersDir, "chamber2"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	chambers, err := ListChambers(tmpDir, "testproj")
	if err != nil {
		t.Fatalf("ListChambers failed: %v", err)
	}
	if len(chambers) != 2 {
		t.Errorf("expected 2 chambers, got %d", len(chambers))
	}
}
