package cli

import "testing"

func TestOnboardingConfig(t *testing.T) {
	cases := []struct {
		name          string
		claude, codex bool
		wantBackend   string
		wantBeta      string
		wantUsable    bool
	}{
		{"both → cross-vendor", true, true, "claude", "codex", true},
		{"claude only", true, false, "claude", "", true},
		{"codex only", false, true, "codex", "", true},
		{"none", false, false, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, usable := onboardingConfig(tc.claude, tc.codex)
			if usable != tc.wantUsable {
				t.Fatalf("usable = %v, want %v", usable, tc.wantUsable)
			}
			if !tc.wantUsable {
				return
			}
			if cfg.AgentBackend != tc.wantBackend {
				t.Errorf("AgentBackend = %q, want %q", cfg.AgentBackend, tc.wantBackend)
			}
			if cfg.BackendBeta != tc.wantBeta {
				t.Errorf("BackendBeta = %q, want %q", cfg.BackendBeta, tc.wantBeta)
			}
		})
	}
}

func TestIsFirstRunFalseWithMarker(t *testing.T) {
	// With an onboarding marker present, it's not a first run even without config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()

	if !isFirstRun(work) {
		t.Fatal("expected first run on a clean home + workdir")
	}

	writeOnboardedMarker()

	if isFirstRun(work) {
		t.Error("expected not-first-run after the marker is written")
	}
}
