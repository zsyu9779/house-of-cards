// Package store provides shared types and constants for House of Cards.
package store

import "time"

// Gazette type constants.
const (
	GazetteCompletion = "completion"
	GazetteHandoff    = "handoff"
	GazetteHelp       = "help"
	GazetteReview     = "review"
	GazetteConflict   = "conflict"
	GazetteQuestion   = "question"  // Phase 2: Question Time
	GazetteAnswer     = "answer"    // Phase 2: Question Time
	GazetteAck        = "ack"       // Phase 2: ACK protocol
	GazetteEscalated  = "escalated" // Phase 2: escalation
	GazetteAutoscale  = "autoscale"
)

// AckStatus constants for Gazette ACK protocol.
const (
	AckStatusNone       = ""
	AckStatusDelivered  = "delivered"
	AckStatusAcked      = "ack"
	AckStatusQuestioned = "questioned"
	AckStatusEscalated  = "escalated"
)

// ACK mode constants for Session.
const (
	AckModeBlocking    = "blocking"
	AckModeNonBlocking = "non-blocking"
	AckModeAuto        = "auto"
)

// DoneFilePayload represents the TOML structure of a .done file.
type DoneFilePayload struct {
	Summary     string            `toml:"summary"`
	Contracts   map[string]string `toml:"contracts"`
	Artifacts   map[string]string `toml:"artifacts"`
	Assumptions map[string]string `toml:"assumptions"`
}

// QuestionRoundLimit is the maximum number of question-answer rounds allowed.
const QuestionRoundLimit = 3

// QuestionTimeout is the timeout for a question round.
const QuestionTimeout = 2 * time.Minute

// BriefingTimeout is the timeout for a briefing session.
const BriefingTimeout = 10 * time.Minute

// Minister status constants.
const (
	MinisterStatusOffline  = "offline"
	MinisterStatusIdle     = "idle"
	MinisterStatusWorking  = "working"
	MinisterStatusStuck    = "stuck"
	MinisterStatusBriefing = "briefing" // Phase 2: waiting for downstream ACK
)
