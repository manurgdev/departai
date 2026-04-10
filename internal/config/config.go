// Package config loads departai configuration from YAML files,
// merging project-level and user-global settings with built-in defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all user-configurable departai settings.
// Fields map 1:1 to YAML keys and CLI flags.
type Config struct {
	AgentBackend     string `yaml:"agent_backend"`
	MaxTurns         int    `yaml:"max_turns"`
	Model            string `yaml:"model"`
	InstructionsFile string `yaml:"instructions_file"`
}

// Defaults returns the built-in baseline configuration.
func Defaults() Config {
	return Config{
		AgentBackend: "claude",
		MaxTurns:     10,
	}
}

// Load builds a Config for the given working directory by layering:
//  1. Built-in defaults
//  2. ~/.config/departai/config.yml   (user-global, lower priority)
//  3. <workDir>/.departai.yml         (project-level, higher priority)
//
// CLI flags are not applied here — the caller overlays them on top.
func Load(workDir string) (Config, error) {
	cfg := Defaults()

	// Layer 1: user-global config
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".config", "departai", "config.yml")
		if err := loadFile(globalPath, &cfg); err != nil {
			return cfg, fmt.Errorf("global config: %w", err)
		}
	}

	// Layer 2: project config (.departai.yml or .departai.yaml)
	for _, name := range []string{".departai.yml", ".departai.yaml"} {
		path := filepath.Join(workDir, name)
		if err := loadFile(path, &cfg); err != nil {
			return cfg, fmt.Errorf("project config %s: %w", path, err)
		}
		// stop at the first one found
		if _, err := os.Stat(path); err == nil {
			break
		}
	}

	return cfg, nil
}

// loadFile reads and merges a YAML config file into dst.
// If the file does not exist it is silently skipped.
func loadFile(path string, dst *Config) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var src Config
	if err := yaml.Unmarshal(data, &src); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	merge(dst, src)
	return nil
}

// merge overlays non-zero fields from src onto dst.
func merge(dst *Config, src Config) {
	if src.AgentBackend != "" {
		dst.AgentBackend = src.AgentBackend
	}
	if src.MaxTurns != 0 {
		dst.MaxTurns = src.MaxTurns
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.InstructionsFile != "" {
		dst.InstructionsFile = src.InstructionsFile
	}
}
