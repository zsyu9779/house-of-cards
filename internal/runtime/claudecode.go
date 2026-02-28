package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ClaudeCodeRuntime implements Runtime for the claude-code CLI.
type ClaudeCodeRuntime struct {
	// Command is the name of the claude binary (default: "claude").
	Command string
	// UseTmux runs the session in a detached tmux window (background).
	// When false the process is started in the foreground.
	UseTmux bool
}

// NewClaudeCodeRuntime returns a ClaudeCodeRuntime with sensible defaults.
func NewClaudeCodeRuntime(useTmux bool) *ClaudeCodeRuntime {
	return &ClaudeCodeRuntime{
		Command: "claude",
		UseTmux: useTmux,
	}
}

// Summon writes the bill brief into the chamber and starts the claude process.
func (r *ClaudeCodeRuntime) Summon(opts SummonOpts) (*AgentSession, error) {
	// Write bill brief into chamber's .hoc directory.
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
		// Kill any stale session with the same name silently.
		_ = exec.Command("tmux", "kill-session", "-t", tmuxName).Run()

		// tmux new-session -d -s <name> -c <chamber> "<claude> -p @.hoc/brief.md"
		shellCmd := fmt.Sprintf("%s -p @.hoc/brief.md", r.Command)
		cmd := exec.Command("tmux", "new-session", "-d",
			"-s", tmuxName,
			"-c", opts.ChamberPath,
			shellCmd,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("tmux start: %w\n%s", err, string(output))
		}
		session.TmuxSession = tmuxName
	} else {
		// Foreground — user sees the output directly.
		cmd := exec.Command(r.Command, "-p", "@.hoc/brief.md")
		cmd.Dir = opts.ChamberPath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start claude: %w", err)
		}
		session.PID = cmd.Process.Pid
	}

	return session, nil
}

// IsSeated returns true if the session process is still running.
func (r *ClaudeCodeRuntime) IsSeated(session *AgentSession) bool {
	if session.TmuxSession != "" {
		err := exec.Command("tmux", "has-session", "-t", session.TmuxSession).Run()
		return err == nil
	}
	if session.PID > 0 {
		// kill -0 checks process existence without sending a signal.
		err := exec.Command("kill", "-0", fmt.Sprintf("%d", session.PID)).Run()
		return err == nil
	}
	return false
}

// Dismiss kills the session.
func (r *ClaudeCodeRuntime) Dismiss(session *AgentSession) error {
	if session.TmuxSession != "" {
		cmd := exec.Command("tmux", "kill-session", "-t", session.TmuxSession)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("kill tmux session: %w\n%s", err, string(output))
		}
		return nil
	}
	if session.PID > 0 {
		proc, err := os.FindProcess(session.PID)
		if err != nil {
			return fmt.Errorf("find process: %w", err)
		}
		return proc.Signal(os.Interrupt)
	}
	return nil
}

// Dispatch sends a message to a running tmux session.
func (r *ClaudeCodeRuntime) Dispatch(session *AgentSession, message string) error {
	if session.TmuxSession == "" {
		return fmt.Errorf("dispatch only supported for tmux sessions")
	}
	cmd := exec.Command("tmux", "send-keys", "-t", session.TmuxSession, message, "Enter")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("dispatch: %w\n%s", err, string(output))
	}
	return nil
}
