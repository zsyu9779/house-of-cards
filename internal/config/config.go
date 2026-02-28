// Package config provides configuration management for House of Cards
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Home     string         `toml:"-"`
	House    HouseConfig    `toml:"house"`
	Speaker  SpeakerConfig  `toml:"speaker"`
	Whip     WhipConfig     `toml:"whip"`
	Storage  StorageConfig  `toml:"storage"`
	Defaults DefaultsConfig `toml:"defaults"`
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
	HeartbeatInterval string `toml:"heartbeat_interval"`
	StuckThreshold    string `toml:"stuck_threshold"`
	MaxRetries        int    `toml:"max_retries"`
}

type StorageConfig struct {
	DBPath string `toml:"db_path"`
}

type DefaultsConfig struct {
	Topology string `toml:"topology"`
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
			HeartbeatInterval: "10s",
			StuckThreshold:    "5m",
			MaxRetries:        2,
		},
		Storage: StorageConfig{
			DBPath: ".hoc/state.db",
		},
		Defaults: DefaultsConfig{
			Topology: "parallel",
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
