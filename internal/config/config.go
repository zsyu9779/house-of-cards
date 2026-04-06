// Package config provides configuration management for House of Cards.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
)

type Config struct {
	Home          string              `toml:"-"`
	House         HouseConfig         `toml:"house"`
	Speaker       SpeakerConfig       `toml:"speaker"`
	Whip          WhipConfig          `toml:"whip"`
	Storage       StorageConfig       `toml:"storage"`
	Defaults      DefaultsConfig      `toml:"defaults"`
	Observability ObservabilityConfig `toml:"observability"`
	Doctor        DoctorConfig        `toml:"doctor"`
}

// DoctorConfig controls doctor check thresholds.
type DoctorConfig struct {
	DBSizeWarnMB       int `toml:"db_size_warn_mb"`
	GazetteBacklogWarn int `toml:"gazette_backlog_warn"`
}

// ObservabilityConfig controls the OpenTelemetry-compatible observability layer.
type ObservabilityConfig struct {
	// Exporter selects the export backend: "stdout" | "otlp" | "nop"
	Exporter string `toml:"exporter"`
	// OTLPEndpoint is the gRPC/HTTP endpoint for OTLP export.
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
				fmt.Fprintf(os.Stderr, "config watcher error: %v\n", err)
			}
		}
	}()
}

// reload reads the config and triggers the onChange callback.
func (cw *ConfigWatcher) reload() {
	cfg, err := LoadConfig(filepath.Dir(filepath.Dir(cw.configPath)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to reload config: %v\n", err)
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
	}
}

func LoadConfig(homeDir string) (*Config, error) {
	configPath := filepath.Join(homeDir, ".hoc", "config.toml")

	cfg := DefaultConfig(homeDir)

	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		return cfg, nil
	}

	_, err = toml.DecodeFile(configPath, cfg)
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	cfg.Home = homeDir
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
