// Package logger provides centralized slog initialisation for House of Cards.
//
// CLI 输出（用户交互表格、✓/✗ 状态）仍使用 fmt.Printf；后台守护进程与内部逻辑统一使用 slog。
package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options controls logger initialisation.
type Options struct {
	// Level: debug, info, warn, error. Empty or unknown → info.
	Level string
	// Format: text, json. Empty or unknown → text.
	Format string
	// Output writer. Nil → os.Stderr.
	Output io.Writer
}

// ParseLevel maps a textual level to slog.Level. Unknown values → slog.LevelInfo.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Build constructs a slog.Logger from opts without mutating the global default.
// Useful in tests where multiple loggers coexist.
func Build(opts Options) *slog.Logger {
	w := opts.Output
	if w == nil {
		w = os.Stderr
	}
	handlerOpts := &slog.HandlerOptions{Level: ParseLevel(opts.Level)}
	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "json":
		handler = slog.NewJSONHandler(w, handlerOpts)
	default:
		handler = slog.NewTextHandler(w, handlerOpts)
	}
	return slog.New(handler)
}

// Init configures the global slog default logger.
func Init(opts Options) {
	slog.SetDefault(Build(opts))
}

// Resolve picks the effective value between a CLI flag and a config fallback.
// A non-empty CLI value always wins.
func Resolve(cliValue, configValue, fallback string) string {
	if strings.TrimSpace(cliValue) != "" {
		return cliValue
	}
	if strings.TrimSpace(configValue) != "" {
		return configValue
	}
	return fallback
}
