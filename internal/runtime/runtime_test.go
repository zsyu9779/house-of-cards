package runtime

import (
	"fmt"
	"testing"
	"time"
)

// ─── Factory tests ─────────────────────────────────────────────────────────────

func TestFactory_ClaudeCode(t *testing.T) {
	r := New("claude-code", false)
	if _, ok := r.(*ClaudeCodeRuntime); !ok {
		t.Errorf("expected *ClaudeCodeRuntime, got %T", r)
	}
}

func TestFactory_ClaudeCode_Tmux(t *testing.T) {
	r := New("claude-code", true)
	cc, ok := r.(*ClaudeCodeRuntime)
	if !ok {
		t.Fatalf("expected *ClaudeCodeRuntime, got %T", r)
	}
	if !cc.UseTmux {
		t.Error("expected UseTmux=true")
	}
}

func TestFactory_Codex(t *testing.T) {
	r := New("codex", false)
	if _, ok := r.(*CodexRuntime); !ok {
		t.Errorf("expected *CodexRuntime, got %T", r)
	}
}

func TestFactory_Cursor(t *testing.T) {
	r := New("cursor", false)
	if _, ok := r.(*CursorRuntime); !ok {
		t.Errorf("expected *CursorRuntime, got %T", r)
	}
}

func TestFactory_UnknownFallback(t *testing.T) {
	r := New("unknown-runtime", false)
	if _, ok := r.(*ClaudeCodeRuntime); !ok {
		t.Errorf("unknown type should fall back to ClaudeCodeRuntime, got %T", r)
	}
}

// ─── ClaudeCodeRuntime struct tests ──────────────────────────────────────────

func TestNewClaudeCodeRuntime_Fields(t *testing.T) {
	r := NewClaudeCodeRuntime(false)
	if r.Command != "claude" {
		t.Errorf("expected Command='claude', got %q", r.Command)
	}
	if r.UseTmux {
		t.Error("expected UseTmux=false")
	}
}

func TestClaudeCodeRuntime_IsSeated_Nil(t *testing.T) {
	r := NewClaudeCodeRuntime(false)
	if r.IsSeated(nil) {
		t.Error("IsSeated(nil) should return false")
	}
}

func TestClaudeCodeRuntime_IsSeated_EmptySession(t *testing.T) {
	r := NewClaudeCodeRuntime(false)
	if r.IsSeated(&AgentSession{}) {
		t.Error("IsSeated with empty session should return false")
	}
}

func TestClaudeCodeRuntime_Dismiss_Nil(t *testing.T) {
	r := NewClaudeCodeRuntime(false)
	// Dismiss with nil should not panic
	_ = r.Dismiss(nil)
}

// ─── CodexRuntime struct tests ───────────────────────────────────────────────

func TestNewCodexRuntime_Fields(t *testing.T) {
	r := NewCodexRuntime(true)
	if !r.UseTmux {
		t.Error("expected UseTmux=true")
	}
}

func TestCodexRuntime_IsSeated_Nil(t *testing.T) {
	r := NewCodexRuntime(false)
	if r.IsSeated(nil) {
		t.Error("IsSeated(nil) should return false")
	}
}

// ─── CursorRuntime struct tests ──────────────────────────────────────────────

func TestNewCursorRuntime_Fields(t *testing.T) {
	r := NewCursorRuntime(false)
	if r.UseTmux {
		t.Error("expected UseTmux=false")
	}
}

func TestCursorRuntime_IsSeated_Nil(t *testing.T) {
	r := NewCursorRuntime(false)
	if r.IsSeated(nil) {
		t.Error("IsSeated(nil) should return false")
	}
}

// MockRuntime is a mock implementation of Runtime for testing.
type MockRuntime struct {
	SummonFunc   func(opts SummonOpts) (*AgentSession, error)
	IsSeatedFunc func(session *AgentSession) bool
	DismissFunc  func(session *AgentSession) error
	DispatchFunc func(session *AgentSession, message string) error
}

func (m *MockRuntime) Summon(opts SummonOpts) (*AgentSession, error) {
	if m.SummonFunc != nil {
		return m.SummonFunc(opts)
	}
	return nil, nil
}

func (m *MockRuntime) IsSeated(session *AgentSession) bool {
	if m.IsSeatedFunc != nil {
		return m.IsSeatedFunc(session)
	}
	return false
}

func (m *MockRuntime) Dismiss(session *AgentSession) error {
	if m.DismissFunc != nil {
		return m.DismissFunc(session)
	}
	return nil
}

func (m *MockRuntime) Dispatch(session *AgentSession, message string) error {
	if m.DispatchFunc != nil {
		return m.DispatchFunc(session, message)
	}
	return nil
}

// Ensure MockRuntime implements Runtime interface.
var _ Runtime = (*MockRuntime)(nil)

// ─── Runtime Interface tests ─────────────────────────────────────────────────────

func TestRuntime_SummonOpts(t *testing.T) {
	opts := SummonOpts{
		MinisterID:    "backend-claude",
		MinisterTitle: "Minister of Backend",
		ChamberPath:   "/home/user/.hoc/projects/myapp/chambers/backend-claude",
		BillBrief:     "# Bill Brief\n\nImplement authentication API",
	}

	if opts.MinisterID != "backend-claude" {
		t.Errorf("MinisterID mismatch")
	}
	if opts.ChamberPath == "" {
		t.Error("ChamberPath should not be empty")
	}
	if opts.BillBrief == "" {
		t.Error("BillBrief should not be empty")
	}
}

func TestRuntime_AgentSession(t *testing.T) {
	session := &AgentSession{
		MinisterID:  "backend-claude",
		PID:         12345,
		TmuxSession: "",
		ChamberPath: "/home/user/.hoc/projects/myapp/chambers/backend-claude",
		StartedAt:   time.Now(),
	}

	if session.MinisterID != "backend-claude" {
		t.Errorf("MinisterID mismatch")
	}
	if session.PID != 12345 {
		t.Errorf("PID mismatch")
	}
	if session.ChamberPath == "" {
		t.Error("ChamberPath should not be empty")
	}
}

func TestRuntime_MockSummon(t *testing.T) {
	mock := &MockRuntime{
		SummonFunc: func(opts SummonOpts) (*AgentSession, error) {
			if opts.MinisterID == "" {
				return nil, fmt.Errorf("MinisterID should not be empty")
			}
			return &AgentSession{
				MinisterID:  opts.MinisterID,
				ChamberPath: opts.ChamberPath,
				StartedAt:   time.Now(),
			}, nil
		},
	}

	session, err := mock.Summon(SummonOpts{
		MinisterID:  "test-minister",
		ChamberPath: "/tmp/test",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if session == nil {
		t.Fatal("session should not be nil")
	}
	if session.MinisterID != "test-minister" {
		t.Errorf("expected MinisterID 'test-minister', got '%s'", session.MinisterID)
	}
}

func TestRuntime_MockIsSeated(t *testing.T) {
	mock := &MockRuntime{
		IsSeatedFunc: func(session *AgentSession) bool {
			return session != nil && session.PID > 0
		},
	}

	// Session with PID should be seated
	session1 := &AgentSession{PID: 12345}
	if !mock.IsSeated(session1) {
		t.Error("session with PID should be seated")
	}

	// Session without PID should not be seated
	session2 := &AgentSession{PID: 0}
	if mock.IsSeated(session2) {
		t.Error("session without PID should not be seated")
	}

	// Nil session should not be seated (should not panic)
	result := mock.IsSeated(nil)
	if result {
		t.Error("nil session should not be seated")
	}
}

func TestRuntime_MockDismiss(t *testing.T) {
	called := false
	mock := &MockRuntime{
		DismissFunc: func(session *AgentSession) error {
			called = true
			if session == nil {
				return fmt.Errorf("session should not be nil")
			}
			return nil
		},
	}

	err := mock.Dismiss(&AgentSession{MinisterID: "test"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DismissFunc was not called")
	}
}

func TestRuntime_MockDispatch(t *testing.T) {
	var receivedMessage string
	mock := &MockRuntime{
		DispatchFunc: func(session *AgentSession, message string) error {
			receivedMessage = message
			return nil
		},
	}

	err := mock.Dispatch(&AgentSession{}, "Hello Minister")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if receivedMessage != "Hello Minister" {
		t.Errorf("expected message 'Hello Minister', got '%s'", receivedMessage)
	}
}

// ─── sessionIsSeated tests ───────────────────────────────────────────────────────

func TestSessionIsSeated_NilSession(t *testing.T) {
	// Should not panic with nil session
	result := sessionIsSeated(nil)
	if result {
		t.Error("nil session should not be seated")
	}
}

func TestSessionIsSeated_EmptySession(t *testing.T) {
	session := &AgentSession{}
	result := sessionIsSeated(session)
	if result {
		t.Error("empty session should not be seated")
	}
}

// ─── sessionDispatch tests ───────────────────────────────────────────────────────

func TestSessionDispatch_NoTmux(t *testing.T) {
	session := &AgentSession{
		TmuxSession: "",
		PID:         0,
	}
	err := sessionDispatch(session, "test message")
	if err == nil {
		t.Error("expected error for session without tmux")
	}
}
