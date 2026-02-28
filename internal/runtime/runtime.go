// Package runtime provides AI agent runtime abstractions for House of Cards.
// Method names follow the parliamentary metaphor.
package runtime

import "time"

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
