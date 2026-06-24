package cli

import (
	"strings"
	"testing"
)

func TestCompletionScript(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		t.Run(sh, func(t *testing.T) {
			s, err := completionScript(sh)
			if err != nil {
				t.Fatalf("completionScript(%q): %v", sh, err)
			}
			if !strings.Contains(s, "departai") {
				t.Errorf("%s script does not mention departai", sh)
			}
			// Sanity: it completes the backend flag (fish uses `-l backend`,
			// bash/zsh use `--backend`, so match the shared substring) with
			// its enum values.
			if !strings.Contains(s, "backend") {
				t.Errorf("%s script missing backend completion", sh)
			}
			if !strings.Contains(s, "claude") || !strings.Contains(s, "codex") {
				t.Errorf("%s script missing backend values", sh)
			}
		})
	}
}

func TestCompletionScriptUnsupported(t *testing.T) {
	if _, err := completionScript("powershell"); err == nil {
		t.Error("expected an error for an unsupported shell")
	}
}

func TestRunCompletionUsage(t *testing.T) {
	if err := runCompletion(nil); err == nil {
		t.Error("expected a usage error with no shell argument")
	}
	if err := runCompletion([]string{"bash", "extra"}); err == nil {
		t.Error("expected a usage error with too many arguments")
	}
	if err := runCompletion([]string{"bash"}); err != nil {
		t.Errorf("valid shell should succeed, got %v", err)
	}
}
