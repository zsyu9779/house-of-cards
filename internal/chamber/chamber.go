// Package chamber provides Git worktree management for Minister sandboxes.
package chamber

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Chamber struct {
	Name     string
	Path     string
	Minister string
	MainRepo string
	Branch   string
}

// NewChamber creates a new worktree chamber for a Minister.
func NewChamber(homeDir, projectName, ministerID, mainRepo string) (*Chamber, error) {
	chambersDir := filepath.Join(homeDir, "projects", projectName, "chambers")
	if err := os.MkdirAll(chambersDir, 0755); err != nil {
		return nil, fmt.Errorf("create chambers dir: %w", err)
	}

	worktreePath := filepath.Join(chambersDir, ministerID)
	branchName := fmt.Sprintf("minister/%s", ministerID)

	return &Chamber{
		Name:     ministerID,
		Path:     worktreePath,
		Minister: ministerID,
		MainRepo: mainRepo,
		Branch:   branchName,
	}, nil
}

// Create creates a new git worktree for this chamber.
func (c *Chamber) Create() error {
	// Check if worktree already exists
	if _, err := os.Stat(c.Path); err == nil {
		return fmt.Errorf("chamber already exists: %s", c.Path)
	}

	// Create worktree
	cmd := exec.Command("git", "worktree", "add", c.Path, "-b", c.Branch)
	cmd.Dir = c.MainRepo
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create worktree: %w, output: %s", err, string(output))
	}

	return nil
}

// Remove removes the worktree.
func (c *Chamber) Remove() error {
	// Remove worktree
	cmd := exec.Command("git", "worktree", "remove", "--force", c.Path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("remove worktree: %w, output: %s", err, string(output))
	}

	// Delete branch
	cmd = exec.Command("git", "branch", "-D", c.Branch)
	cmd.Dir = c.MainRepo
	if output, err := cmd.CombinedOutput(); err != nil {
		// Non-fatal - branch might not exist
		fmt.Printf("warning: could not delete branch: %s\n", string(output))
	}

	return nil
}

// Status returns the status of the worktree.
func (c *Chamber) Status() (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = c.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return string(output), nil
}

// HasUncommittedChanges returns true if there are uncommitted changes.
func (c *Chamber) HasUncommittedChanges() (bool, error) {
	status, err := c.Status()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(status) != "", nil
}

// Stash saves uncommitted changes.
func (c *Chamber) Stash() error {
	cmd := exec.Command("git", "stash", "push", "-m", fmt.Sprintf("chamber:%s:emergency-stash", c.Minister))
	cmd.Dir = c.Path
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git stash: %w, output: %s", err, string(output))
	}
	return nil
}

// Commit commits staged changes.
func (c *Chamber) Commit(message string) error {
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = c.Path
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w, output: %s", err, string(output))
	}

	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = c.Path
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w, output: %s", err, string(output))
	}
	return nil
}

// Push pushes the branch to remote.
func (c *Chamber) Push() error {
	cmd := exec.Command("git", "push", "-u", "origin", c.Branch)
	cmd.Dir = c.Path
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w, output: %s", err, string(output))
	}
	return nil
}

// GetBranchName returns the branch name for this chamber.
func (c *Chamber) GetBranchName() string {
	return c.Branch
}

// GetWorktreePath returns the path to the worktree.
func (c *Chamber) GetWorktreePath() string {
	return c.Path
}

// ListChambers lists all chambers in a project.
func ListChambers(homeDir, projectName string) ([]*Chamber, error) {
	chambersDir := filepath.Join(homeDir, "projects", projectName, "chambers")

	entries, err := os.ReadDir(chambersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Chamber{}, nil
		}
		return nil, err
	}

	var chambers []*Chamber
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		chambers = append(chambers, &Chamber{
			Name: entry.Name(),
			Path: filepath.Join(chambersDir, entry.Name()),
		})
	}

	return chambers, nil
}
