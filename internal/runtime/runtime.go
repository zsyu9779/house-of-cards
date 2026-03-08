// Package runtime provides AI agent runtime abstractions for House of Cards.
// Method names follow the parliamentary metaphor.
package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Runtime defines the interface for AI agent runtimes.
type Runtime interface {
	// Summon starts a minister session in a chamber (git worktree).
	Summon(opts SummonOpts) (*AgentSession, error)

	// IsSeated checks if a session is still running.
	IsSeated(session *AgentSession) bool

	// Dismiss gracefully ends a session.
	Dismiss(session *AgentSession) error

	// Dispatch sends a message or nudge to a running minister.
	Dispatch(session *AgentSession, message string) error
}

// SummonOpts contains the options for starting a minister session.
type SummonOpts struct {
	MinisterID    string
	MinisterTitle string
	ChamberPath   string // absolute path to the git worktree
	BillBrief     string // markdown content injected as the initial prompt
}

// AgentSession represents a running minister session.
type AgentSession struct {
	MinisterID  string
	PID         int
	TmuxSession string
	ChamberPath string
	StartedAt   time.Time
}

// sessionIsSeated checks whether a tmux session or foreground process is still running.
func sessionIsSeated(session *AgentSession) bool {
	if session == nil {
		return false
	}
	if session.TmuxSession != "" {
		err := exec.Command("tmux", "has-session", "-t", session.TmuxSession).Run()
		return err == nil
	}
	if session.PID > 0 {
		err := exec.Command("kill", "-0", fmt.Sprintf("%d", session.PID)).Run()
		return err == nil
	}
	return false
}

// sessionDismiss kills a tmux session or sends SIGINT to a foreground process.
func sessionDismiss(session *AgentSession) error {
	if session == nil {
		return nil
	}
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

// sessionDispatch sends a message to a running tmux session via send-keys.
func sessionDispatch(session *AgentSession, message string) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	if session.TmuxSession == "" {
		return fmt.Errorf("dispatch only supported for tmux sessions")
	}
	cmd := exec.Command("tmux", "send-keys", "-t", session.TmuxSession, message, "Enter")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("dispatch: %w\n%s", err, string(output))
	}
	return nil
}
