// Package config loads departai configuration from YAML files,
// merging project-level and user-global settings with built-in defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all user-configurable departai settings.
// Fields map 1:1 to YAML keys and CLI flags.
type Config struct {
	AgentBackend     string `yaml:"agent_backend"`
	MaxTurns         int    `yaml:"max_turns"`
	Model            string `yaml:"model"`                  // default model for all agents
	ModelAlpha       string `yaml:"model_alpha,omitempty"`  // override for Agent Alpha
	ModelBeta        string `yaml:"model_beta,omitempty"`   // override for Agent Beta
	InstructionsFile string `yaml:"instructions_file"`
}

// ModelFor returns the model to use for the given agent name, preferring
// the agent-specific override and falling back to the global Model.
// Accepts "alpha"/"beta" or full names like "Agent Alpha"/"Agent Beta".
func (c Config) ModelFor(agentName string) string {
	switch strings.ToLower(agentName) {
	case "alpha", "agent alpha":
		if c.ModelAlpha != "" {
			return c.ModelAlpha
		}
	case "beta", "agent beta":
		if c.ModelBeta != "" {
			return c.ModelBeta
		}
	}
	return c.Model
}

// Defaults returns the built-in baseline configuration.
func Defaults() Config {
	return Config{
		AgentBackend: "claude",
		MaxTurns:     10,
	}
}

// ── config file paths ──────────────────────────────────────────────────────

// GlobalPath returns the path to the user-global config file: ~/.departai/config.yml
func GlobalPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".departai", "config.yml")
}

// ProjectPath returns the path to the project-level config file:
// <workDir>/.departai/config.yml
func ProjectPath(workDir string) string {
	return filepath.Join(workDir, ".departai", "config.yml")
}

// ── loading ────────────────────────────────────────────────────────────────

// Load builds a Config for the given working directory by layering:
//  1. Built-in defaults
//  2. ~/.departai/config.yml           (user-global, lower priority)
//  3. <workDir>/.departai/config.yml   (project-level, higher priority)
//
// For backwards compatibility, the old locations (~/.config/departai/config.yml
// and <workDir>/.departai.yml) are also checked as fallbacks.
//
// CLI flags are not applied here — the caller overlays them on top.
func Load(workDir string) (Config, error) {
	cfg := Defaults()

	// Layer 1: user-global config (try new path first, fall back to old)
	for _, p := range []string{GlobalPath(), legacyGlobalPath()} {
		if p == "" {
			continue
		}
		if err := loadFile(p, &cfg); err != nil {
			return cfg, fmt.Errorf("global config: %w", err)
		}
		if fileExists(p) {
			break
		}
	}

	// Layer 2: project config (try new path first, fall back to old)
	projectPaths := []string{ProjectPath(workDir)}
	for _, name := range []string{".departai.yml", ".departai.yaml"} {
		projectPaths = append(projectPaths, filepath.Join(workDir, name))
	}
	for _, p := range projectPaths {
		if err := loadFile(p, &cfg); err != nil {
			return cfg, fmt.Errorf("project config %s: %w", p, err)
		}
		if fileExists(p) {
			break
		}
	}

	return cfg, nil
}

// ── saving ─────────────────────────────────────────────────────────────────

// Save writes the config to the given path as YAML, creating parent
// directories as needed.
func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	header := "# departai configuration\n# See: https://github.com/manurgdev/departai\n\n"
	return os.WriteFile(path, []byte(header+string(data)), 0644)
}

// ── internal helpers ───────────────────────────────────────────────────────

func legacyGlobalPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "departai", "config.yml")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
	if src.ModelAlpha != "" {
		dst.ModelAlpha = src.ModelAlpha
	}
	if src.ModelBeta != "" {
		dst.ModelBeta = src.ModelBeta
	}
	if src.InstructionsFile != "" {
		dst.InstructionsFile = src.InstructionsFile
	}
}
