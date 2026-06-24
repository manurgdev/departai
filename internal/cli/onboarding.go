package cli

import (
	"fmt"
	"os"
	"path/filepath"

	claudeagent "github.com/manurgdev/departai/internal/agent/claude"
	codexagent "github.com/manurgdev/departai/internal/agent/codex"
	"github.com/manurgdev/departai/internal/config"
	"github.com/manurgdev/departai/internal/ui"
)

// onboardedMarkerPath is a flag file written after a first-run onboarding so it
// isn't shown again, even when the user declined to create a config.
func onboardedMarkerPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".departai", ".onboarded")
}

// isFirstRun reports whether this looks like a brand-new install: no config
// anywhere and no onboarding marker.
func isFirstRun(workDir string) bool {
	if config.Exists(workDir) {
		return false
	}
	if p := onboardedMarkerPath(); p != "" && fileExists(p) {
		return false
	}
	return true
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// detectBackends reports which agent CLIs are installed and on PATH.
func detectBackends() (hasClaude, hasCodex bool) {
	return claudeagent.EnsureAvailable() == nil, codexagent.EnsureAvailable() == nil
}

// onboardingConfig returns the config to seed from the detected backends. With
// both installed it sets up cross-vendor collaboration (Alpha → claude,
// Beta → codex), the product's core value. With one, both agents use it. The
// bool is false when nothing usable is installed.
func onboardingConfig(hasClaude, hasCodex bool) (config.Config, bool) {
	cfg := config.Defaults()
	switch {
	case hasClaude && hasCodex:
		cfg.AgentBackend = "claude"
		cfg.BackendBeta = "codex"
		return cfg, true
	case hasClaude:
		cfg.AgentBackend = "claude"
		return cfg, true
	case hasCodex:
		cfg.AgentBackend = "codex"
		return cfg, true
	default:
		return cfg, false
	}
}

// runOnboarding shows the first-run experience and, when a backend is present,
// optionally writes a global config seeded with the detected backends. Returns
// the (possibly updated) config to use for this session.
func runOnboarding(workDir string, cfg config.Config) config.Config {
	hasClaude, hasCodex := detectBackends()
	ui.OnboardingWelcome(hasClaude, hasCodex)

	seed, usable := onboardingConfig(hasClaude, hasCodex)
	if !usable {
		// Nothing to configure yet — show install hints and DON'T mark as
		// onboarded, so the guidance reappears until a backend is installed.
		ui.OnboardingNoBackends(claudeagent.InstallHint, codexagent.InstallHint)
		return cfg
	}

	// At least one backend is present — don't nag on future runs.
	writeOnboardedMarker()

	if !ui.PromptOnboardingCreateConfig(seed.AgentBackend, seed.BackendBeta) {
		return cfg
	}

	path := config.GlobalPath()
	if err := seed.Save(path); err != nil {
		ui.Warning(fmt.Sprintf("could not write config to %s: %v", path, err))
		return cfg
	}
	ui.OnboardingConfigCreated(path)
	return seed
}

func writeOnboardedMarker() {
	p := onboardedMarkerPath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("onboarded\n"), 0o644)
}
