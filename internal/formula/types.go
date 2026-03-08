// Package formula implements the Formula extension system for House of Cards.
//
// A Formula is a pre-defined workflow template (TOML) that can be listed,
// applied, and monitored via `hoc formula` commands. Formulas encapsulate
// multi-step operations (git, shell, hoc) so operators can run them without
// knowing the underlying details.
//
// Formula lifecycle:
//
//	hoc formula list                        → list available formulas
//	hoc formula apply <name> [--dry-run]    → execute a formula
//	hoc formula status <name>               → last run result
package formula

import "time"

// ─────────────────────────────────────────────
// Top-level types
// ─────────────────────────────────────────────

// Formula is a named, reusable workflow template.
type Formula struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	// Trigger: manual | cron | event
	Trigger string `toml:"trigger"`
	// Steps is an ordered list of action groups to execute.
	Steps []Step `toml:"steps"`
	// builtin marks formulas shipped with the binary.
	builtin bool
}

// IsBuiltin returns true for the built-in formulas included in the binary.
func (f *Formula) IsBuiltin() bool { return f.builtin }

// Step is a named group of actions executed sequentially or in parallel.
type Step struct {
	Name     string   `toml:"name"`
	Parallel bool     `toml:"parallel"`
	Actions  []Action `toml:"action"`
	// OnFailure actions run if any action in Actions fails.
	OnFailure []Action `toml:"on_failure"`
}

// Action is a single unit of work inside a Step.
type Action struct {
	// Type: "git" | "shell" | "hoc"
	Type string `toml:"type"`
	// Command is the sub-command or shell expression.
	// Supports variable interpolation: {{.Variable}}
	Command string `toml:"command"`
	// Targets lists glob patterns for the working directories.
	// e.g. ["chambers/*"]
	Targets []string `toml:"targets"`
	// Message is used for notification-type actions.
	Message string `toml:"message"`
	// If is an optional condition expression (evaluated as Go template bool).
	If string `toml:"if"`
}

// ─────────────────────────────────────────────
// Execution result types
// ─────────────────────────────────────────────

// RunResult is the outcome of a single formula application.
type RunResult struct {
	FormulaName string
	StartedAt   time.Time
	FinishedAt  time.Time
	Success     bool
	Steps       []StepResult
}

// StepResult holds per-step outcome.
type StepResult struct {
	Name    string
	Success bool
	Actions []ActionResult
}

// ActionResult holds per-action outcome.
type ActionResult struct {
	Type    string
	Command string
	Target  string
	Output  string
	Err     error
}

// Duration returns the total elapsed time for the run.
func (r *RunResult) Duration() time.Duration {
	return r.FinishedAt.Sub(r.StartedAt)
}

// ─────────────────────────────────────────────
// Registry
// ─────────────────────────────────────────────

// Registry holds all available formulas (built-in + user-defined).
type Registry struct {
	formulas map[string]*Formula
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{formulas: make(map[string]*Formula)}
}

// Register adds a formula to the registry.
func (r *Registry) Register(f *Formula) {
	r.formulas[f.Name] = f
}

// Get returns a formula by name, or nil if not found.
func (r *Registry) Get(name string) *Formula {
	return r.formulas[name]
}

// List returns all formulas in deterministic order.
func (r *Registry) List() []*Formula {
	names := make([]string, 0, len(r.formulas))
	for n := range r.formulas {
		names = append(names, n)
	}
	// Sort for deterministic output.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	out := make([]*Formula, 0, len(names))
	for _, n := range names {
		out = append(out, r.formulas[n])
	}
	return out
}
