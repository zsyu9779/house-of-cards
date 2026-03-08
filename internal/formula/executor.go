package formula

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// ExecuteOpts controls how a formula is run.
type ExecuteOpts struct {
	// HocDir is the path to the .hoc directory (for resolving chambers paths).
	HocDir string
	// DryRun prints actions without executing them.
	DryRun bool
	// Vars holds template variables for interpolation (e.g. {"Project": "myapp"}).
	Vars map[string]string
}

// Execute runs a formula and returns its result.
func Execute(ctx context.Context, f *Formula, opts ExecuteOpts) *RunResult {
	result := &RunResult{
		FormulaName: f.Name,
		StartedAt:   time.Now(),
	}

	slog.Info("执行 Formula", "formula", f.Name, "dry_run", opts.DryRun)

	for _, step := range f.Steps {
		sr := runStep(ctx, step, opts)
		result.Steps = append(result.Steps, sr)
		if !sr.Success {
			result.Success = false
			result.FinishedAt = time.Now()
			slog.Warn("Formula 步骤失败，中止", "formula", f.Name, "step", step.Name)
			return result
		}
	}

	result.Success = true
	result.FinishedAt = time.Now()
	slog.Info("Formula 执行完成", "formula", f.Name, "duration", result.Duration())
	return result
}

// runStep executes all actions in a step.
func runStep(ctx context.Context, step Step, opts ExecuteOpts) StepResult {
	sr := StepResult{Name: step.Name, Success: true}

	if step.Parallel {
		// Run actions concurrently and collect results.
		type pair struct {
			idx int
			ar  ActionResult
		}
		ch := make(chan pair, len(step.Actions))
		for i, a := range step.Actions {
			go func(idx int, act Action) {
				ar := runAction(ctx, act, opts)
				ch <- pair{idx, ar}
			}(i, a)
		}
		ars := make([]ActionResult, len(step.Actions))
		for range step.Actions {
			p := <-ch
			ars[p.idx] = p.ar
		}
		for _, ar := range ars {
			sr.Actions = append(sr.Actions, ar)
			if ar.Err != nil {
				sr.Success = false
			}
		}
	} else {
		for _, a := range step.Actions {
			ar := runAction(ctx, a, opts)
			sr.Actions = append(sr.Actions, ar)
			if ar.Err != nil {
				sr.Success = false
				// Run on_failure actions.
				for _, fa := range step.OnFailure {
					_ = runAction(ctx, fa, opts)
				}
				return sr
			}
		}
	}

	return sr
}

// runAction executes a single action.
func runAction(ctx context.Context, a Action, opts ExecuteOpts) ActionResult {
	ar := ActionResult{Type: a.Type, Command: a.Command}

	// Resolve targets — default to HocDir if none specified.
	targets, err := resolveTargets(a.Targets, opts.HocDir)
	if err != nil {
		ar.Err = err
		return ar
	}
	if len(targets) == 0 {
		targets = []string{opts.HocDir}
	}

	for _, target := range targets {
		ar.Target = target
		cmd, err := interpolate(a.Command, opts.Vars, map[string]string{"Target": target})
		if err != nil {
			ar.Err = err
			return ar
		}

		if opts.DryRun {
			slog.Info("[dry-run]", "type", a.Type, "cmd", cmd, "target", target)
			ar.Output = fmt.Sprintf("[dry-run] %s %s @ %s", a.Type, cmd, target)
			continue
		}

		out, runErr := executeAction(ctx, a.Type, cmd, target)
		ar.Output = out
		if runErr != nil {
			ar.Err = runErr
			slog.Warn("Action 失败", "type", a.Type, "cmd", cmd, "target", target, "err", runErr)
			return ar
		}
		slog.Debug("Action 成功", "type", a.Type, "cmd", cmd, "target", target)
	}

	return ar
}

// executeAction runs the actual shell/git/hoc command.
func executeAction(ctx context.Context, actionType, command, workDir string) (string, error) {
	var cmd *exec.Cmd

	switch actionType {
	case "git":
		parts := splitCommand("git " + command)
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	case "shell":
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	case "hoc":
		// Find the hoc binary (same process).
		hoc, err := os.Executable()
		if err != nil {
			hoc = "hoc"
		}
		parts := splitCommand(command)
		cmd = exec.CommandContext(ctx, hoc, parts...)
	case "notify":
		// Notification — just log the message.
		slog.Info("[notify]", "message", command)
		return command, nil
	default:
		return "", fmt.Errorf("unknown action type: %s", actionType)
	}

	if _, err := os.Stat(workDir); err == nil {
		cmd.Dir = workDir
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	return buf.String(), err
}

// ─── helpers ───────────────────────────────────────────────────────────────

// interpolate substitutes {{.Key}} placeholders in s using vars and extras.
func interpolate(s string, vars map[string]string, extras map[string]string) (string, error) {
	combined := make(map[string]string)
	for k, v := range vars {
		combined[k] = v
	}
	for k, v := range extras {
		combined[k] = v
	}

	tmpl, err := template.New("").Parse(s)
	if err != nil {
		return s, fmt.Errorf("template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, combined); err != nil {
		return s, fmt.Errorf("template execute: %w", err)
	}
	return buf.String(), nil
}

// resolveTargets expands glob patterns relative to hocDir.
func resolveTargets(patterns []string, hocDir string) ([]string, error) {
	var targets []string
	for _, p := range patterns {
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(hocDir), p)
		}
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", p, err)
		}
		targets = append(targets, matches...)
	}
	return targets, nil
}

// splitCommand splits a shell-like command string into tokens.
// It does not handle quoting — use exec.Command for complex cases.
func splitCommand(s string) []string {
	return strings.Fields(s)
}
