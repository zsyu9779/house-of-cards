// Package config provides configuration management for House of Cards.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
)

// ValidationError wraps multiple config validation failures.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation failed (%d errors):\n  - %s",
		len(e.Errors), strings.Join(e.Errors, "\n  - "))
}

// Validate checks all config fields for correctness.
// Returns a ValidationError containing all failures (not just the first).
func (c *Config) Validate() error {
	var errs []string

	// --- Whip durations ---
	if c.Whip.HeartbeatInterval != "" {
		if _, err := time.ParseDuration(c.Whip.HeartbeatInterval); err != nil {
			errs = append(errs, fmt.Sprintf("whip.heartbeat_interval: invalid duration %q: %v",
				c.Whip.HeartbeatInterval, err))
		}
	}

	if c.Whip.StuckThreshold != "" {
		d, err := time.ParseDuration(c.Whip.StuckThreshold)
		if err != nil {
			errs = append(errs, fmt.Sprintf("whip.stuck_threshold: invalid duration %q: %v",
				c.Whip.StuckThreshold, err))
		} else if d < 30*time.Second {
			errs = append(errs, fmt.Sprintf("whip.stuck_threshold: %v is too small (minimum 30s)", d))
		}
	}

	// --- Heartbeat vs Stuck relationship ---
	if c.Whip.HeartbeatInterval != "" && c.Whip.StuckThreshold != "" {
		hb, err1 := time.ParseDuration(c.Whip.HeartbeatInterval)
		st, err2 := time.ParseDuration(c.Whip.StuckThreshold)
		if err1 == nil && err2 == nil && st <= hb*3 {
			errs = append(errs, fmt.Sprintf(
				"whip.stuck_threshold (%v) should be > 3x heartbeat_interval (%v)",
				st, hb))
		}
	}

	// --- Whip numeric ranges ---
	if c.Whip.MaxMinisters <= 0 {
		errs = append(errs, fmt.Sprintf("whip.max_ministers: must be > 0, got %d",
			c.Whip.MaxMinisters))
	}

	if c.Whip.MaxRetries < 0 {
		errs = append(errs, fmt.Sprintf("whip.max_retries: must be >= 0, got %d",
			c.Whip.MaxRetries))
	}

	if c.Whip.ScaleUpThreshold <= 0 {
		errs = append(errs, fmt.Sprintf("whip.scale_up_threshold: must be > 0, got %d",
			c.Whip.ScaleUpThreshold))
	}

	if c.Whip.ScaleDownThreshold <= 0 {
		errs = append(errs, fmt.Sprintf("whip.scale_down_threshold: must be > 0, got %d",
			c.Whip.ScaleDownThreshold))
	}

	// --- Storage ---
	if c.Storage.DBPath == "" {
		errs = append(errs, "storage.db_path: must not be empty")
	}

	// --- Observability ---
	// otlp is accepted at parse time but flagged as a stub: spans would be
	// dropped silently, so we reject it at validation with a clear error.
	validExporters := map[string]bool{"stdout": true, "otlp": true, "nop": true}
	if c.Observability.Exporter != "" && !validExporters[c.Observability.Exporter] {
		errs = append(errs, fmt.Sprintf(
			"observability.exporter: %q is not valid (choose: stdout, otlp, nop)",
			c.Observability.Exporter))
	}
	if c.Observability.Exporter == "otlp" {
		errs = append(errs, "observability.exporter: \"otlp\" is a stub and not yet supported "+
			"(spans would be dropped silently) — use \"stdout\" or \"nop\"")
	}

	// --- Home directory ---
	if c.Home != "" {
		if fi, err := os.Stat(c.Home); err != nil {
			errs = append(errs, fmt.Sprintf("home directory %q: %v", c.Home, err))
		} else if !fi.IsDir() {
			errs = append(errs, fmt.Sprintf("home %q is not a directory", c.Home))
		}
	}

	// --- Doctor ---
	if c.Doctor.DBSizeWarnMB <= 0 {
		errs = append(errs, fmt.Sprintf("doctor.db_size_warn_mb: must be > 0, got %d",
			c.Doctor.DBSizeWarnMB))
	}

	// --- Log ---
	validLevels := map[string]bool{"": true, "debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(c.Log.Level)] {
		errs = append(errs, fmt.Sprintf(
			"log.level: %q is not valid (choose: debug, info, warn, error)", c.Log.Level))
	}
	validFormats := map[string]bool{"": true, "text": true, "json": true}
	if !validFormats[strings.ToLower(c.Log.Format)] {
		errs = append(errs, fmt.Sprintf(
			"log.format: %q is not valid (choose: text, json)", c.Log.Format))
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

type Config struct {
	Home          string              `toml:"-"`
	House         HouseConfig         `toml:"house"`
	Speaker       SpeakerConfig       `toml:"speaker"`
	Whip          WhipConfig          `toml:"whip"`
	Storage       StorageConfig       `toml:"storage"`
	Defaults      DefaultsConfig      `toml:"defaults"`
	Observability ObservabilityConfig `toml:"observability"`
	Doctor        DoctorConfig        `toml:"doctor"`
	Log           LogConfig           `toml:"log"`
}

// LogConfig controls the global slog handler.
type LogConfig struct {
	// Level: debug | info | warn | error
	Level string `toml:"level"`
	// Format: text | json
	Format string `toml:"format"`
}

// DoctorConfig controls doctor check thresholds.
type DoctorConfig struct {
	DBSizeWarnMB       int `toml:"db_size_warn_mb"`
	GazetteBacklogWarn int `toml:"gazette_backlog_warn"`
}

// ObservabilityConfig controls the OpenTelemetry-compatible observability layer.
type ObservabilityConfig struct {
	// Exporter selects the export backend: "stdout" | "otlp" | "nop".
	// NOTE: "otlp" is a stub in v0.3 and rejected by Validate; use "stdout" or "nop".
	Exporter string `toml:"exporter"`
	// OTLPEndpoint is the gRPC/HTTP endpoint for OTLP export.
	// NOTE: OTLP export is not yet implemented in v0.3.
	OTLPEndpoint string `toml:"otlp_endpoint"`
	// ServiceName is the logical service name in traces/metrics.
	ServiceName string `toml:"service_name"`
}

type HouseConfig struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

type SpeakerConfig struct {
	Runtime string `toml:"runtime"`
	Model   string `toml:"model"`
}

type WhipConfig struct {
	HeartbeatInterval  string `toml:"heartbeat_interval"`
	StuckThreshold     string `toml:"stuck_threshold"`
	MaxRetries         int    `toml:"max_retries"`
	MaxMinisters       int    `toml:"max_ministers"`        // Maximum number of active ministers
	ScaleUpThreshold   int    `toml:"scale_up_threshold"`   // Pending bills > idle * threshold → scale up
	ScaleDownThreshold int    `toml:"scale_down_threshold"` // Idle > pending + threshold → scale down
}

type StorageConfig struct {
	DBPath string `toml:"db_path"`
}

type DefaultsConfig struct {
	Topology string `toml:"topology"`
}

// HotReloadableParams are the config parameters that can be hot-reloaded.
type HotReloadableParams struct {
	WhipInterval       string // whip.heartbeat_interval
	StuckThreshold     string // whip.stuck_threshold
	MaxMinisters       int    // whip.max_ministers
	ScaleUpThreshold   int    // whip.scale_up_threshold
	ScaleDownThreshold int    // whip.scale_down_threshold
}

// ConfigWatcher watches for config file changes.
type ConfigWatcher struct {
	watcher    *fsnotify.Watcher
	configPath string
	onChange   func(*HotReloadableParams)
	stopCh     chan struct{}
}

// NewConfigWatcher creates a new config watcher.
func NewConfigWatcher(configPath string, onChange func(*HotReloadableParams)) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	cw := &ConfigWatcher{
		watcher:    watcher,
		configPath: configPath,
		onChange:   onChange,
		stopCh:     make(chan struct{}),
	}

	// Watch the config file's directory.
	dir := filepath.Dir(configPath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	return cw, nil
}

// Start begins watching for config changes.
func (cw *ConfigWatcher) Start() {
	go func() {
		for {
			select {
			case <-cw.stopCh:
				return
			case event, ok := <-cw.watcher.Events:
				if !ok {
					return
				}
				// Only react to writes to the config file.
				if event.Op&fsnotify.Write == fsnotify.Write && event.Name == cw.configPath {
					cw.reload()
				}
			case err, ok := <-cw.watcher.Errors:
				if !ok {
					return
				}
				slog.Error("config watcher error", "err", err)
			}
		}
	}()
}

// reload reads the config and triggers the onChange callback.
func (cw *ConfigWatcher) reload() {
	cfg, err := LoadConfig(filepath.Dir(filepath.Dir(cw.configPath)))
	if err != nil {
		slog.Error("config reload failed", "err", err, "path", cw.configPath)
		return
	}

	params := &HotReloadableParams{
		WhipInterval:       cfg.Whip.HeartbeatInterval,
		StuckThreshold:     cfg.Whip.StuckThreshold,
		MaxMinisters:       cfg.Whip.MaxMinisters,
		ScaleUpThreshold:   cfg.Whip.ScaleUpThreshold,
		ScaleDownThreshold: cfg.Whip.ScaleDownThreshold,
	}

	if cw.onChange != nil {
		cw.onChange(params)
	}
}

// Stop stops the config watcher.
func (cw *ConfigWatcher) Stop() {
	close(cw.stopCh)
	cw.watcher.Close()
}

func DefaultConfig(homeDir string) *Config {
	return &Config{
		Home: homeDir,
		House: HouseConfig{
			Name:    "default",
			Version: "0.1.0",
		},
		Speaker: SpeakerConfig{
			Runtime: "claude-code",
			Model:   "opus",
		},
		Whip: WhipConfig{
			HeartbeatInterval:  "10s",
			StuckThreshold:     "5m",
			MaxRetries:         2,
			MaxMinisters:       10, // Default max active ministers
			ScaleUpThreshold:   2,  // pending > idle * 2 → scale up
			ScaleDownThreshold: 2,  // idle > pending + 2 → scale down
		},
		Storage: StorageConfig{
			DBPath: ".hoc/state.db",
		},
		Defaults: DefaultsConfig{
			Topology: "parallel",
		},
		Observability: ObservabilityConfig{
			Exporter:     "nop",
			ServiceName:  "house-of-cards",
			OTLPEndpoint: "localhost:4317",
		},
		Doctor: DoctorConfig{
			DBSizeWarnMB:       100,
			GazetteBacklogWarn: 50,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

func LoadConfig(homeDir string) (*Config, error) {
	configPath := filepath.Join(homeDir, ".hoc", "config.toml")

	cfg := DefaultConfig(homeDir)

	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("default config invalid: %w", err)
		}
		return cfg, nil
	}

	_, err = toml.DecodeFile(configPath, cfg)
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	cfg.Home = homeDir
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", configPath, err)
	}
	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer f.Close()

	fmt.Fprintln(f, "# House of Cards 配置文件")
	return toml.NewEncoder(f).Encode(cfg)
}

func GetHOCHome() string {
	if home := os.Getenv("HOC_HOME"); home != "" {
		return home
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home, _ = os.Getwd()
	}

	return filepath.Join(home, "house-of-cards")
}
