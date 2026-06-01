package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestDetailedContainsAllFields(t *testing.T) {
	s := Detailed()
	for _, want := range []string{"departai", "commit:", "built:", "go:", runtime.GOOS} {
		if !strings.Contains(s, want) {
			t.Errorf("Detailed() missing %q; got:\n%s", want, s)
		}
	}
}

func TestSummaryIsCleanOneLiner(t *testing.T) {
	s := Summary()
	// Must carry version + os/arch …
	if !strings.Contains(s, "departai") || !strings.Contains(s, runtime.GOOS) {
		t.Errorf("Summary() missing version or os; got %q", s)
	}
	// … but must NOT leak internal metadata.
	for _, leak := range []string{"commit:", "built:", "go:"} {
		if strings.Contains(s, leak) {
			t.Errorf("Summary() should not expose %q; got %q", leak, s)
		}
	}
	if strings.Contains(s, "\n") {
		t.Errorf("Summary() should be a single line, got %q", s)
	}
}

func TestShortNotEmpty(t *testing.T) {
	if strings.TrimSpace(Short()) == "" {
		t.Error("Short() returned empty")
	}
}

func TestOrDefault(t *testing.T) {
	if got := orDefault("", "fallback"); got != "fallback" {
		t.Errorf("orDefault(empty) = %q, want fallback", got)
	}
	if got := orDefault("  ", "fallback"); got != "fallback" {
		t.Errorf("orDefault(whitespace) = %q, want fallback", got)
	}
	if got := orDefault("v1", "fallback"); got != "v1" {
		t.Errorf("orDefault(set) = %q, want v1", got)
	}
}
