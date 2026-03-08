package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/house-of-cards/hoc/internal/config"
)

// ─── DefaultConfig tests ──────────────────────────────────────────────────────

func TestDefaultConfig_Fields(t *testing.T) {
	cfg := config.DefaultConfig("/home/test")

	if cfg.Home != "/home/test" {
		t.Errorf("Home: got %q, want /home/test", cfg.Home)
	}
	if cfg.House.Name != "default" {
		t.Errorf("House.Name: got %q, want default", cfg.House.Name)
	}
	if cfg.House.Version != "0.1.0" {
		t.Errorf("House.Version: got %q, want 0.1.0", cfg.House.Version)
	}
	if cfg.Speaker.Runtime != "claude-code" {
		t.Errorf("Speaker.Runtime: got %q, want claude-code", cfg.Speaker.Runtime)
	}
	if cfg.Speaker.Model != "opus" {
		t.Errorf("Speaker.Model: got %q, want opus", cfg.Speaker.Model)
	}
	if cfg.Whip.HeartbeatInterval != "10s" {
		t.Errorf("Whip.HeartbeatInterval: got %q, want 10s", cfg.Whip.HeartbeatInterval)
	}
	if cfg.Whip.StuckThreshold != "5m" {
		t.Errorf("Whip.StuckThreshold: got %q, want 5m", cfg.Whip.StuckThreshold)
	}
	if cfg.Whip.MaxRetries != 2 {
		t.Errorf("Whip.MaxRetries: got %d, want 2", cfg.Whip.MaxRetries)
	}
	if cfg.Whip.MaxMinisters != 10 {
		t.Errorf("Whip.MaxMinisters: got %d, want 10", cfg.Whip.MaxMinisters)
	}
	if cfg.Defaults.Topology != "parallel" {
		t.Errorf("Defaults.Topology: got %q, want parallel", cfg.Defaults.Topology)
	}
	if cfg.Observability.Exporter != "nop" {
		t.Errorf("Observability.Exporter: got %q, want nop", cfg.Observability.Exporter)
	}
	if cfg.Observability.ServiceName != "house-of-cards" {
		t.Errorf("Observability.ServiceName: got %q, want house-of-cards", cfg.Observability.ServiceName)
	}
}

// ─── LoadConfig tests ─────────────────────────────────────────────────────────

func TestLoadConfig_NoFile_ReturnsDefaults(t *testing.T) {
	dir := t.TempDir()

	cfg, err := config.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Home != dir {
		t.Errorf("Home: got %q, want %q", cfg.Home, dir)
	}
	if cfg.Speaker.Runtime != "claude-code" {
		t.Errorf("Speaker.Runtime: got %q, want default claude-code", cfg.Speaker.Runtime)
	}
	if cfg.Whip.MaxMinisters != 10 {
		t.Errorf("Whip.MaxMinisters: got %d, want default 10", cfg.Whip.MaxMinisters)
	}
}

func TestLoadConfig_WithTOML(t *testing.T) {
	dir := t.TempDir()
	hocDir := filepath.Join(dir, ".hoc")
	if err := os.MkdirAll(hocDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	toml := `[house]
name = "my-house"
version = "1.2.3"

[speaker]
runtime = "codex"
model = "gpt-4"

[whip]
heartbeat_interval = "30s"
stuck_threshold = "10m"
max_retries = 5
max_ministers = 20

[defaults]
topology = "pipeline"

[observability]
exporter = "stdout"
service_name = "test-service"
`
	if err := os.WriteFile(filepath.Join(hocDir, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.House.Name != "my-house" {
		t.Errorf("House.Name: got %q, want my-house", cfg.House.Name)
	}
	if cfg.House.Version != "1.2.3" {
		t.Errorf("House.Version: got %q, want 1.2.3", cfg.House.Version)
	}
	if cfg.Speaker.Runtime != "codex" {
		t.Errorf("Speaker.Runtime: got %q, want codex", cfg.Speaker.Runtime)
	}
	if cfg.Speaker.Model != "gpt-4" {
		t.Errorf("Speaker.Model: got %q, want gpt-4", cfg.Speaker.Model)
	}
	if cfg.Whip.HeartbeatInterval != "30s" {
		t.Errorf("Whip.HeartbeatInterval: got %q, want 30s", cfg.Whip.HeartbeatInterval)
	}
	if cfg.Whip.MaxMinisters != 20 {
		t.Errorf("Whip.MaxMinisters: got %d, want 20", cfg.Whip.MaxMinisters)
	}
	if cfg.Defaults.Topology != "pipeline" {
		t.Errorf("Defaults.Topology: got %q, want pipeline", cfg.Defaults.Topology)
	}
	if cfg.Observability.Exporter != "stdout" {
		t.Errorf("Observability.Exporter: got %q, want stdout", cfg.Observability.Exporter)
	}
	if cfg.Home != dir {
		t.Errorf("Home should be set to dir after load, got %q", cfg.Home)
	}
}

func TestLoadConfig_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	hocDir := filepath.Join(dir, ".hoc")
	if err := os.MkdirAll(hocDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hocDir, "config.toml"), []byte("this is [not valid toml {"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := config.LoadConfig(dir)
	if err == nil {
		t.Error("expected error for invalid TOML, got nil")
	}
}

// ─── SaveConfig + round-trip ──────────────────────────────────────────────────

func TestSaveAndLoadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	hocDir := filepath.Join(dir, ".hoc")
	if err := os.MkdirAll(hocDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	original := config.DefaultConfig(dir)
	original.House.Name = "round-trip-test"
	original.Whip.MaxMinisters = 42
	original.Speaker.Runtime = "cursor"

	configPath := filepath.Join(hocDir, "config.toml")
	if err := config.SaveConfig(configPath, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := config.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig after save: %v", err)
	}

	if loaded.House.Name != "round-trip-test" {
		t.Errorf("House.Name: got %q, want round-trip-test", loaded.House.Name)
	}
	if loaded.Whip.MaxMinisters != 42 {
		t.Errorf("Whip.MaxMinisters: got %d, want 42", loaded.Whip.MaxMinisters)
	}
	if loaded.Speaker.Runtime != "cursor" {
		t.Errorf("Speaker.Runtime: got %q, want cursor", loaded.Speaker.Runtime)
	}
}

// ─── GetHOCHome tests ────────────────────────────────────────────────────────

func TestGetHOCHome_EnvVar(t *testing.T) {
	t.Setenv("HOC_HOME", "/custom/hoc/home")

	got := config.GetHOCHome()
	if got != "/custom/hoc/home" {
		t.Errorf("GetHOCHome: got %q, want /custom/hoc/home", got)
	}
}

func TestGetHOCHome_Default(t *testing.T) {
	// Unset HOC_HOME to test default behavior.
	t.Setenv("HOC_HOME", "")

	got := config.GetHOCHome()
	if got == "" {
		t.Error("GetHOCHome should return a non-empty path")
	}
	// Should contain "house-of-cards" in the path.
	if !filepath.IsAbs(got) {
		t.Errorf("GetHOCHome should return absolute path, got %q", got)
	}
}
