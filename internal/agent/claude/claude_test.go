package claude

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// claudeAvailable reports whether the `claude` CLI is installed.
// Without it, model validation tests are skipped.
func claudeAvailable() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func TestValidateModelEmpty(t *testing.T) {
	// An empty model name means "use backend default" and is always valid,
	// without spawning the CLI.
	if err := ValidateModel(context.Background(), ""); err != nil {
		t.Errorf("empty model should be valid, got: %v", err)
	}
}

func TestValidateModelInvalid(t *testing.T) {
	if !claudeAvailable() {
		t.Skip("claude CLI not installed; skipping validation test")
	}

	err := ValidateModel(context.Background(), "totally-fake-model-12345")
	if err == nil {
		t.Fatal("expected invalid model to return an error, got nil")
	}
	if !strings.Contains(err.Error(), "issue with the selected model") &&
		!strings.Contains(err.Error(), "It may not exist") {
		t.Errorf("error message should reference the selection issue, got: %v", err)
	}
}
