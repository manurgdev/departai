// Package config loads departai configuration from YAML files,
// merging project-level and user-global settings with built-in defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all user-configurable departai settings.
// Fields map 1:1 to YAML keys and CLI flags.
type Config struct {
	Mode         string `yaml:"mode,omitempty"`          // "dev" (default) or "ask"
	AgentBackend string `yaml:"agent_backend"`           // default backend for all agents
	BackendAlpha string `yaml:"backend_alpha,omitempty"` // override for Agent Alpha
	BackendBeta  string `yaml:"backend_beta,omitempty"`  // override for Agent Beta
	MaxTurns     int    `yaml:"max_turns"`
	// MaxTurnDuration is parsed from MaxTurnDurationStr — yaml.v3 does not
	// support time.Duration natively, so we keep the source as a string and
	// expose a typed helper.
	MaxTurnDurationStr string `yaml:"max_turn_duration,omitempty"` // e.g. "15m", "1h30m"; empty = no limit
	// LogWindow caps the number of turn entries injected into each prompt.
	// 0 = unlimited (full task log); positive = inject only the last N turns
	// (plus header, original task, and all user directives) with an omission
	// marker covering the dropped range.
	LogWindow int `yaml:"log_window,omitempty"`
	// MaxRetries is the number of times a turn is retried on a transient
	// backend failure (rate limit, network blip, 5xx). A pointer so that an
	// explicit 0 (disable retries) is distinguishable from "not configured"
	// (use the default). Read it through Retries().
	MaxRetries       *int   `yaml:"max_retries,omitempty"`
	Model            string `yaml:"model"`                 // default model for all agents
	ModelAlpha       string `yaml:"model_alpha,omitempty"` // override for Agent Alpha
	ModelBeta        string `yaml:"model_beta,omitempty"`  // override for Agent Beta
	InstructionsFile string `yaml:"instructions_file"`

	// BlockedCommands lists tool names or shell command patterns that agents
	// must NOT use during a turn (e.g. "WebFetch", "rm -rf"). Soft enforcement
	// via prompt injection — agents are instructed to refuse and stop.
	BlockedCommands []string `yaml:"blocked_commands,omitempty"`
}

// BackendFor returns the backend to use for the given agent name, preferring
// the agent-specific override and falling back to the global AgentBackend.
func (c Config) BackendFor(agentName string) string {
	switch strings.ToLower(agentName) {
	case "alpha", "agent alpha":
		if c.BackendAlpha != "" {
			return c.BackendAlpha
		}
	case "beta", "agent beta":
		if c.BackendBeta != "" {
			return c.BackendBeta
		}
	}
	if c.AgentBackend == "" {
		return "claude"
	}
	return c.AgentBackend
}

// DefaultMaxRetries is the per-turn retry count used when the user hasn't
// configured one.
const DefaultMaxRetries = 2

// Retries returns the effective per-turn retry count: the configured value, or
// DefaultMaxRetries when unset. A configured 0 disables retries.
func (c Config) Retries() int {
	if c.MaxRetries == nil {
		return DefaultMaxRetries
	}
	if *c.MaxRetries < 0 {
		return 0
	}
	return *c.MaxRetries
}

// MaxTurnDuration parses MaxTurnDurationStr into a time.Duration.
// Returns 0 (no limit) for an empty or unparseable value.
func (c Config) MaxTurnDuration() time.Duration {
	if c.MaxTurnDurationStr == "" {
		return 0
	}
	d, err := time.ParseDuration(c.MaxTurnDurationStr)
	if err != nil {
		return 0
	}
	return d
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
		Mode:         "dev",
		AgentBackend: "claude",
		MaxTurns:     0, // 0 = unlimited turns (run until consensus)
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
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	if src.AgentBackend != "" {
		dst.AgentBackend = src.AgentBackend
	}
	if src.BackendAlpha != "" {
		dst.BackendAlpha = src.BackendAlpha
	}
	if src.BackendBeta != "" {
		dst.BackendBeta = src.BackendBeta
	}
	if src.MaxTurns != 0 {
		dst.MaxTurns = src.MaxTurns
	}
	if src.MaxTurnDurationStr != "" {
		dst.MaxTurnDurationStr = src.MaxTurnDurationStr
	}
	if src.LogWindow != 0 {
		dst.LogWindow = src.LogWindow
	}
	if src.MaxRetries != nil {
		dst.MaxRetries = src.MaxRetries
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
	if len(src.BlockedCommands) > 0 {
		// Union merge: combine both lists, preserving order, deduplicated.
		seen := make(map[string]bool, len(dst.BlockedCommands))
		for _, c := range dst.BlockedCommands {
			seen[c] = true
		}
		for _, c := range src.BlockedCommands {
			if !seen[c] {
				dst.BlockedCommands = append(dst.BlockedCommands, c)
				seen[c] = true
			}
		}
	}
}
