package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CodexRuntime implements Runtime for the OpenAI Codex CLI.
type CodexRuntime struct {
	// UseTmux runs the session in a detached tmux window (background).
	// When false the process reads the brief content and passes it as an argument.
	UseTmux bool
}

// NewCodexRuntime returns a CodexRuntime with the given tmux preference.
func NewCodexRuntime(useTmux bool) *CodexRuntime {
	return &CodexRuntime{UseTmux: useTmux}
}

// Summon writes the bill brief into the chamber and starts the codex process.
//
// Codex does not support a "-p @file" syntax, so:
//   - tmux mode:      bash -c 'codex --full-auto "$(<.hoc/brief.md)"'
//   - foreground mode: read brief content → exec.Command("codex", "--full-auto", content)
func (r *CodexRuntime) Summon(opts SummonOpts) (*AgentSession, error) {
	briefDir := filepath.Join(opts.ChamberPath, ".hoc")
	if err := os.MkdirAll(briefDir, 0755); err != nil {
		return nil, fmt.Errorf("create brief dir: %w", err)
	}
	briefPath := filepath.Join(briefDir, "brief.md")
	if err := os.WriteFile(briefPath, []byte(opts.BillBrief), 0644); err != nil {
		return nil, fmt.Errorf("write brief: %w", err)
	}

	session := &AgentSession{
		MinisterID:  opts.MinisterID,
		ChamberPath: opts.ChamberPath,
		StartedAt:   time.Now(),
	}

	if r.UseTmux {
		tmuxName := fmt.Sprintf("hoc-%s", opts.MinisterID)
		_ = exec.Command("tmux", "kill-session", "-t", tmuxName).Run()

		// Use shell substitution to pass brief content because codex has no @file syntax.
		shellCmd := `codex --full-auto "$(<.hoc/brief.md)"`
		cmd := exec.Command("tmux", "new-session", "-d",
			"-s", tmuxName,
			"-c", opts.ChamberPath,
			"bash", "-c", shellCmd,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("tmux start: %w\n%s", err, string(output))
		}
		session.TmuxSession = tmuxName
	} else {
		// Foreground: read file content and pass directly as argument.
		content, err := os.ReadFile(briefPath)
		if err != nil {
			return nil, fmt.Errorf("read brief: %w", err)
		}
		cmd := exec.Command("codex", "--full-auto", string(content))
		cmd.Dir = opts.ChamberPath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start codex: %w", err)
		}
		session.PID = cmd.Process.Pid
	}

	return session, nil
}

func (r *CodexRuntime) IsSeated(s *AgentSession) bool            { return sessionIsSeated(s) }
func (r *CodexRuntime) Dismiss(s *AgentSession) error            { return sessionDismiss(s) }
func (r *CodexRuntime) Dispatch(s *AgentSession, m string) error { return sessionDispatch(s, m) }
