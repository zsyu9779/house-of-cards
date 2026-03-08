package runtime

// New returns the Runtime implementation for the given type string.
// Unknown types fall back to ClaudeCodeRuntime.
func New(runtimeType string, useTmux bool) Runtime {
	switch runtimeType {
	case "codex":
		return NewCodexRuntime(useTmux)
	case "cursor":
		return NewCursorRuntime(useTmux)
	default: // "claude-code" and fallback
		return NewClaudeCodeRuntime(useTmux)
	}
}
