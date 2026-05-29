package codex

import (
	"context"
	"os/exec"
	"testing"

	"github.com/manurgdev/departai/internal/agent"
)

// codexAvailable reports whether the `codex` CLI is installed. Without it,
// model validation tests are skipped (mirrors claude_test.go convention).
func codexAvailable() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

func TestNewAndName(t *testing.T) {
	a := New("Agent Beta")
	if a.Name() != "Agent Beta" {
		t.Errorf("Name() = %q, want %q", a.Name(), "Agent Beta")
	}
	if a.model != "" {
		t.Errorf("New should leave model empty, got %q", a.model)
	}
}

func TestNewWithModel(t *testing.T) {
	a := NewWithModel("Agent Beta", "gpt-5.3-codex")
	if a.Name() != "Agent Beta" {
		t.Errorf("Name() = %q, want %q", a.Name(), "Agent Beta")
	}
	if a.model != "gpt-5.3-codex" {
		t.Errorf("model = %q, want %q", a.model, "gpt-5.3-codex")
	}
}

func TestSetOnEventAndStreamDone(t *testing.T) {
	a := New("Agent Beta")

	eventSeen := false
	a.SetOnEvent(func(agent.StreamEvent) { eventSeen = true })
	if a.onEvent == nil {
		t.Fatal("SetOnEvent did not assign the callback")
	}
	a.onEvent(agent.StreamEvent{Kind: "text", Text: "x"})
	if !eventSeen {
		t.Error("onEvent callback was not invoked")
	}

	doneSeen := false
	a.SetOnStreamDone(func() { doneSeen = true })
	if a.onStreamDone == nil {
		t.Fatal("SetOnStreamDone did not assign the callback")
	}
	a.onStreamDone()
	if !doneSeen {
		t.Error("onStreamDone callback was not invoked")
	}
}

func TestValidateModelEmpty(t *testing.T) {
	// Empty model means "use backend default" — always valid, no CLI spawn.
	if err := ValidateModel(context.Background(), ""); err != nil {
		t.Errorf("empty model should be valid, got: %v", err)
	}
}

func TestValidateModelInvalid(t *testing.T) {
	if !codexAvailable() {
		t.Skip("codex CLI not installed; skipping validation test")
	}
	if err := ValidateModel(context.Background(), "totally-fake-model-12345"); err == nil {
		t.Fatal("expected invalid model to return an error, got nil")
	}
}
