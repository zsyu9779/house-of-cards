package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CursorRuntime implements Runtime for the Cursor agent CLI.
type CursorRuntime struct {
	// UseTmux runs the session in a detached tmux window (background).
	// When false the process reads the brief content and passes it as an argument.
	UseTmux bool
}

// NewCursorRuntime returns a CursorRuntime with the given tmux preference.
func NewCursorRuntime(useTmux bool) *CursorRuntime {
	return &CursorRuntime{UseTmux: useTmux}
}

// Summon writes the bill brief into the chamber and starts the cursor agent process.
//
// Cursor's -p flag means --print (non-interactive output), and --force is required
// to allow file modifications in agent mode:
//   - tmux mode:      bash -c 'cursor agent --force -p "$(<.hoc/brief.md)"'
//   - foreground mode: read brief content → exec.Command("cursor", "agent", "--force", "-p", content)
func (r *CursorRuntime) Summon(opts SummonOpts) (*AgentSession, error) {
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

		shellCmd := `cursor agent --force -p "$(<.hoc/brief.md)"`
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
		cmd := exec.Command("cursor", "agent", "--force", "-p", string(content))
		cmd.Dir = opts.ChamberPath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start cursor: %w", err)
		}
		session.PID = cmd.Process.Pid
	}

	return session, nil
}

func (r *CursorRuntime) IsSeated(s *AgentSession) bool            { return sessionIsSeated(s) }
func (r *CursorRuntime) Dismiss(s *AgentSession) error            { return sessionDismiss(s) }
func (r *CursorRuntime) Dispatch(s *AgentSession, m string) error { return sessionDispatch(s, m) }
