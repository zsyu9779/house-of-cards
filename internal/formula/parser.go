package formula

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadFromFile parses a single Formula from a TOML file.
func LoadFromFile(path string) (*Formula, error) {
	var f Formula
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("parse formula %s: %w", path, err)
	}
	if f.Name == "" {
		// Derive name from filename if not set.
		base := filepath.Base(path)
		f.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return &f, nil
}

// LoadDirectory walks dir and loads all *.toml files as formulas.
// Non-TOML files are silently skipped.
func LoadDirectory(dir string) ([]*Formula, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty directory is fine
		}
		return nil, fmt.Errorf("read formula dir %s: %w", dir, err)
	}

	var formulas []*Formula
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		f, err := LoadFromFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		formulas = append(formulas, f)
	}
	return formulas, nil
}

// LoadRegistryFromDirs builds a Registry from one or more formula directories.
// Built-in formulas are registered first, then user-defined ones (which can
// override built-ins by using the same name).
func LoadRegistryFromDirs(dirs ...string) (*Registry, error) {
	reg := NewRegistry()

	// Register built-ins first.
	for _, b := range Builtins() {
		reg.Register(b)
	}

	// Load user-defined formulas (can override built-ins).
	for _, dir := range dirs {
		formulas, err := LoadDirectory(dir)
		if err != nil {
			return nil, err
		}
		for _, f := range formulas {
			reg.Register(f)
		}
	}

	return reg, nil
}
