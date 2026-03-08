package formula_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/house-of-cards/hoc/internal/formula"
)

// TestBuiltinsRegistered verifies all 5 built-in formulas are present.
func TestBuiltinsRegistered(t *testing.T) {
	reg, err := formula.LoadRegistryFromDirs()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"cleanup-chambers",
		"auto-merge",
		"sync-main",
		"health-check",
		"archive-session",
	}

	for _, name := range want {
		if reg.Get(name) == nil {
			t.Errorf("built-in formula %q not found", name)
		}
	}

	if len(reg.List()) < len(want) {
		t.Errorf("expected at least %d formulas, got %d", len(want), len(reg.List()))
	}
}

// TestBuiltinsAreMarked verifies that all built-ins have IsBuiltin() == true.
func TestBuiltinsAreMarked(t *testing.T) {
	for _, f := range formula.Builtins() {
		if !f.IsBuiltin() {
			t.Errorf("formula %q: IsBuiltin() should be true", f.Name)
		}
	}
}

// TestRegistryList verifies that List() returns formulas in sorted order.
func TestRegistryList(t *testing.T) {
	reg := formula.NewRegistry()
	reg.Register(&formula.Formula{Name: "zzz"})
	reg.Register(&formula.Formula{Name: "aaa"})
	reg.Register(&formula.Formula{Name: "mmm"})

	list := reg.List()
	for i := 1; i < len(list); i++ {
		if list[i-1].Name >= list[i].Name {
			t.Errorf("list not sorted: %s >= %s", list[i-1].Name, list[i].Name)
		}
	}
}

// TestLoadFromFile verifies TOML parsing of a formula file.
func TestLoadFromFile(t *testing.T) {
	const toml = `
name = "test-formula"
description = "A test formula"
trigger = "manual"

[[steps]]
name = "step-one"
  [[steps.action]]
  type = "shell"
  command = "echo hello"
`
	tmp, err := os.CreateTemp(t.TempDir(), "*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString(toml); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	f, err := formula.LoadFromFile(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "test-formula" {
		t.Errorf("name: got %q, want test-formula", f.Name)
	}
	if len(f.Steps) != 1 || len(f.Steps[0].Actions) != 1 {
		t.Errorf("unexpected steps/actions: %+v", f.Steps)
	}
	if f.Steps[0].Actions[0].Type != "shell" {
		t.Errorf("action type: got %q, want shell", f.Steps[0].Actions[0].Type)
	}
}

// TestLoadDirectory verifies that formula directory loading works.
func TestLoadDirectory(t *testing.T) {
	dir := t.TempDir()

	const toml = `name = "user-formula"
description = "from dir"
trigger = "manual"
`
	if err := os.WriteFile(filepath.Join(dir, "user-formula.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}
	// Write a non-toml file (should be skipped).
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	formulas, err := formula.LoadDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(formulas) != 1 {
		t.Errorf("expected 1 formula, got %d", len(formulas))
	}
	if formulas[0].Name != "user-formula" {
		t.Errorf("formula name: got %q, want user-formula", formulas[0].Name)
	}
}

// TestDryRunExecution verifies that dry-run mode does not actually execute.
func TestDryRunExecution(t *testing.T) {
	f := &formula.Formula{
		Name:    "dry-test",
		Trigger: "manual",
		Steps: []formula.Step{
			{
				Name: "shell-step",
				Actions: []formula.Action{
					{
						Type:    "shell",
						Command: "this-command-does-not-exist-xyz",
					},
				},
			},
		},
	}

	result := formula.Execute(context.Background(), f, formula.ExecuteOpts{
		DryRun: true,
	})

	// In dry-run mode, the command should NOT be run (so no error).
	if !result.Success {
		t.Errorf("dry-run should succeed even with invalid command")
	}
	for _, sr := range result.Steps {
		for _, ar := range sr.Actions {
			if ar.Err != nil {
				t.Errorf("dry-run action should have no error: %v", ar.Err)
			}
			if !strings.Contains(ar.Output, "[dry-run]") {
				t.Errorf("dry-run output should say [dry-run]: %q", ar.Output)
			}
		}
	}
}

// TestInterpolation verifies that template variables are substituted.
func TestInterpolation(t *testing.T) {
	f := &formula.Formula{
		Name: "interpolation-test",
		Steps: []formula.Step{
			{
				Name: "echo",
				Actions: []formula.Action{
					{
						Type:    "shell",
						Command: "echo {{.Greeting}} {{.Name}}",
					},
				},
			},
		},
	}

	result := formula.Execute(context.Background(), f, formula.ExecuteOpts{
		DryRun: true,
		Vars: map[string]string{
			"Greeting": "Hello",
			"Name":     "World",
		},
	})

	if !result.Success {
		t.Errorf("expected success, got failure")
	}
	// Check the interpolated command appears in output.
	found := false
	for _, sr := range result.Steps {
		for _, ar := range sr.Actions {
			if strings.Contains(ar.Output, "Hello World") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected interpolated 'Hello World' in output")
	}
}

// TestUserOverridesBuiltin verifies user-defined formulas can shadow built-ins.
func TestUserOverridesBuiltin(t *testing.T) {
	dir := t.TempDir()
	const toml = `name = "health-check"
description = "custom health check"
trigger = "manual"
`
	if err := os.WriteFile(filepath.Join(dir, "health-check.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := formula.LoadRegistryFromDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := reg.Get("health-check")
	if f == nil {
		t.Fatal("health-check not found")
	}
	if f.Description != "custom health check" {
		t.Errorf("expected override: got %q", f.Description)
	}
}
