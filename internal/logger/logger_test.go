package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"info":    slog.LevelInfo,
		"INFO":    slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"Debug":   slog.LevelDebug,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"unknown": slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBuild_DefaultLevel_FiltersDebug(t *testing.T) {
	var buf bytes.Buffer
	l := Build(Options{Output: &buf})
	l.Debug("hidden")
	l.Info("shown")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Errorf("debug should be filtered at default info level, got: %s", out)
	}
	if !strings.Contains(out, "shown") {
		t.Errorf("info should be present, got: %s", out)
	}
}

func TestBuild_DebugLevel_EmitsDebug(t *testing.T) {
	var buf bytes.Buffer
	l := Build(Options{Level: "debug", Output: &buf})
	l.Debug("visible")

	if !strings.Contains(buf.String(), "visible") {
		t.Errorf("debug level should emit debug logs, got: %s", buf.String())
	}
}

func TestBuild_ErrorLevel_SuppressesInfoAndWarn(t *testing.T) {
	var buf bytes.Buffer
	l := Build(Options{Level: "error", Output: &buf})
	l.Info("info-hidden")
	l.Warn("warn-hidden")
	l.Error("err-shown")

	out := buf.String()
	if strings.Contains(out, "info-hidden") || strings.Contains(out, "warn-hidden") {
		t.Errorf("error level should suppress info/warn, got: %s", out)
	}
	if !strings.Contains(out, "err-shown") {
		t.Errorf("error should pass through, got: %s", out)
	}
}

func TestBuild_JSONFormat_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	l := Build(Options{Level: "info", Format: "json", Output: &buf})
	l.Info("hello", "k", "v")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no output")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v, got: %s", err, line)
	}
	if parsed["msg"] != "hello" {
		t.Errorf("expected msg=hello, got %v", parsed["msg"])
	}
	if parsed["k"] != "v" {
		t.Errorf("expected k=v, got %v", parsed["k"])
	}
}

func TestBuild_TextFormat_DefaultWhenUnspecified(t *testing.T) {
	var buf bytes.Buffer
	l := Build(Options{Format: "", Output: &buf})
	l.Info("hello")
	// Text handler output should NOT be valid JSON.
	if err := json.Unmarshal(buf.Bytes(), &map[string]any{}); err == nil {
		t.Errorf("expected text output, but got valid JSON: %s", buf.String())
	}
}

func TestResolve_CLIWinsOverConfig(t *testing.T) {
	if got := Resolve("debug", "warn", "info"); got != "debug" {
		t.Errorf("CLI should win, got %q", got)
	}
}

func TestResolve_ConfigUsedWhenCLIEmpty(t *testing.T) {
	if got := Resolve("", "warn", "info"); got != "warn" {
		t.Errorf("config should win, got %q", got)
	}
}

func TestResolve_FallbackWhenBothEmpty(t *testing.T) {
	if got := Resolve("", "", "info"); got != "info" {
		t.Errorf("fallback should win, got %q", got)
	}
}

func TestResolve_WhitespaceTreatedAsEmpty(t *testing.T) {
	if got := Resolve("   ", "warn", "info"); got != "warn" {
		t.Errorf("whitespace-only CLI should be treated as empty, got %q", got)
	}
}

func TestInit_SetsGlobalDefault(t *testing.T) {
	var buf bytes.Buffer
	Init(Options{Level: "debug", Format: "text", Output: &buf})
	defer Init(Options{Level: "info", Format: "text"}) // restore

	slog.Debug("global-debug")
	if !strings.Contains(buf.String(), "global-debug") {
		t.Errorf("Init should set global default logger, got: %s", buf.String())
	}
}
