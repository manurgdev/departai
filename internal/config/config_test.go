package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExists(t *testing.T) {
	// Isolate HOME so a real ~/.departai/config.yml doesn't leak in.
	t.Setenv("HOME", t.TempDir())
	work := t.TempDir()

	if Exists(work) {
		t.Error("expected no config for a fresh working dir")
	}

	p := ProjectPath(work)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("mode: dev\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !Exists(work) {
		t.Error("expected Exists=true after creating a project config")
	}
}

func TestModelFor(t *testing.T) {
	cases := []struct {
		name   string
		cfg    Config
		agent  string
		expect string
	}{
		{
			name:   "no overrides — falls back to global",
			cfg:    Config{Model: "claude-opus"},
			agent:  "alpha",
			expect: "claude-opus",
		},
		{
			name:   "alpha override is used",
			cfg:    Config{Model: "claude-opus", ModelAlpha: "claude-sonnet"},
			agent:  "alpha",
			expect: "claude-sonnet",
		},
		{
			name:   "beta override does not leak to alpha",
			cfg:    Config{Model: "claude-opus", ModelBeta: "claude-sonnet"},
			agent:  "alpha",
			expect: "claude-opus",
		},
		{
			name:   "beta override is used",
			cfg:    Config{Model: "claude-opus", ModelBeta: "claude-haiku"},
			agent:  "beta",
			expect: "claude-haiku",
		},
		{
			name:   "full agent name is accepted",
			cfg:    Config{ModelAlpha: "claude-sonnet"},
			agent:  "Agent Alpha",
			expect: "claude-sonnet",
		},
		{
			name:   "unknown agent falls back to global",
			cfg:    Config{Model: "claude-opus", ModelAlpha: "claude-sonnet"},
			agent:  "gamma",
			expect: "claude-opus",
		},
		{
			name:   "empty everything returns empty",
			cfg:    Config{},
			agent:  "alpha",
			expect: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.ModelFor(tc.agent)
			if got != tc.expect {
				t.Errorf("ModelFor(%q) = %q, want %q", tc.agent, got, tc.expect)
			}
		})
	}
}

func TestMergePerAgentModels(t *testing.T) {
	dst := Config{Model: "global", ModelAlpha: "old-alpha"}
	src := Config{ModelAlpha: "new-alpha", ModelBeta: "new-beta"}

	merge(&dst, src)

	if dst.Model != "global" {
		t.Errorf("Model: got %q, want %q", dst.Model, "global")
	}
	if dst.ModelAlpha != "new-alpha" {
		t.Errorf("ModelAlpha: got %q, want %q", dst.ModelAlpha, "new-alpha")
	}
	if dst.ModelBeta != "new-beta" {
		t.Errorf("ModelBeta: got %q, want %q", dst.ModelBeta, "new-beta")
	}
}

func TestMergeDoesNotClobberWithEmpty(t *testing.T) {
	dst := Config{Model: "keep", ModelAlpha: "keep-alpha", ModelBeta: "keep-beta"}
	src := Config{} // all empty

	merge(&dst, src)

	if dst.Model != "keep" || dst.ModelAlpha != "keep-alpha" || dst.ModelBeta != "keep-beta" {
		t.Errorf("empty src should not clobber dst; got %+v", dst)
	}
}

func TestRetries(t *testing.T) {
	zero, five := 0, 5
	cases := []struct {
		name string
		cfg  Config
		want int
	}{
		{"unset uses default", Config{}, DefaultMaxRetries},
		{"explicit zero disables", Config{MaxRetries: &zero}, 0},
		{"explicit value", Config{MaxRetries: &five}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Retries(); got != tc.want {
				t.Errorf("Retries() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMergeMaxRetries(t *testing.T) {
	// A configured value (incl. 0) overrides; absence (nil) does not clobber.
	zero := 0
	dst := Config{}
	merge(&dst, Config{MaxRetries: &zero})
	if dst.MaxRetries == nil || *dst.MaxRetries != 0 {
		t.Errorf("explicit 0 should merge in; got %v", dst.MaxRetries)
	}

	three := 3
	dst2 := Config{MaxRetries: &three}
	merge(&dst2, Config{}) // nil src
	if dst2.MaxRetries == nil || *dst2.MaxRetries != 3 {
		t.Errorf("nil src should not clobber; got %v", dst2.MaxRetries)
	}
}
