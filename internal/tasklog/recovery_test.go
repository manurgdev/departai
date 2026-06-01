package tasklog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corruptLog overwrites a task's log file with the given (possibly malformed)
// content, simulating corruption on disk. Returns the task dir.
func corruptLog(t *testing.T, content string) (taskDir string) {
	t.Helper()
	tl, err := New(t.TempDir(), "original prompt text")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.WriteFile(tl.Path(), []byte(content), 0644); err != nil {
		t.Fatalf("overwriting log: %v", err)
	}
	return tl.Dir
}

// findBackup returns the path of the recovery backup file, or "" if none.
func findBackup(t *testing.T, taskDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(taskDir, logFileName+".corrupt-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func TestLoadValidLogIsUntouched(t *testing.T) {
	base := t.TempDir()
	tl, err := New(base, "a normal task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before, _ := os.ReadFile(tl.Path())

	loaded, err := Load(tl.Dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Recovered) != 0 {
		t.Errorf("valid log should not trigger recovery, got %v", loaded.Recovered)
	}
	if b := findBackup(t, tl.Dir); b != "" {
		t.Errorf("valid log should not create a backup, found %s", b)
	}
	after, _ := os.ReadFile(tl.Path())
	if string(before) != string(after) {
		t.Error("valid log content changed on Load")
	}
	if loaded.Prompt != "a normal task" {
		t.Errorf("prompt = %q, want %q", loaded.Prompt, "a normal task")
	}
}

func TestLoadRecoversMissingHeader(t *testing.T) {
	// No "# Task Log" header, no Original Task → corrupt.
	dir := corruptLog(t, "garbage content with no structure at all\n")

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load should recover, not error: %v", err)
	}
	if len(loaded.Recovered) == 0 {
		t.Fatal("expected a recovery note")
	}
	if b := findBackup(t, dir); b == "" {
		t.Error("expected a .corrupt backup of the original")
	}
	// Repaired log must now be valid.
	repaired, _ := os.ReadFile(loaded.Path())
	if !strings.Contains(string(repaired), "# Task Log") {
		t.Error("repaired log missing header")
	}
	if !logLooksValid(string(repaired)) {
		t.Error("repaired log still not valid")
	}
}

func TestLoadRecoversPreservingTurns(t *testing.T) {
	// Header is damaged, but real turn entries exist mid-file — they must survive.
	corrupt := "TASK LOG (header smashed)\n\n" +
		"## Turn 1 - Agent Alpha\n\n**Complete**: no\n\n---\n" +
		"## Turn 2 - Agent Beta\n\n**Complete**: yes\n\n---\n"
	dir := corruptLog(t, corrupt)

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Recovered) == 0 {
		t.Fatal("expected recovery")
	}
	turns, err := loaded.ParseTurns()
	if err != nil {
		t.Fatalf("ParseTurns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 preserved turns, got %d", len(turns))
	}
	if turns[0].AgentName != "Agent Alpha" || turns[1].AgentName != "Agent Beta" {
		t.Errorf("turns not preserved correctly: %+v", turns)
	}
}

func TestLoadRecoversPreservingExtractablePrompt(t *testing.T) {
	// Header line missing but Original Task block intact → prompt should be kept.
	corrupt := "oops\n\n## Original Task\n\nbuild the parser\n\n---\n\n" +
		"## Turn 1 - Agent Alpha\n\n**Complete**: no\n\n---\n"
	dir := corruptLog(t, corrupt)

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Prompt != "build the parser" {
		t.Errorf("recovered prompt = %q, want %q", loaded.Prompt, "build the parser")
	}
}

func TestLoadRecoversEmptyFile(t *testing.T) {
	dir := corruptLog(t, "")

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on empty file should recover: %v", err)
	}
	if len(loaded.Recovered) == 0 {
		t.Error("expected recovery for empty file")
	}
	repaired, _ := os.ReadFile(loaded.Path())
	if !logLooksValid(string(repaired)) {
		t.Error("repaired empty log not valid")
	}
}

func TestLoadRecoversInvalidUTF8(t *testing.T) {
	// Valid-looking header but with invalid UTF-8 bytes appended.
	dir := corruptLog(t, "# Task Log\n\n## Original Task\n\nx\n\n---\n\xff\xfe\xff")

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Recovered) == 0 {
		t.Error("expected recovery for invalid UTF-8")
	}
}

func TestLogLooksValid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"healthy", "# Task Log\n\n## Original Task\n\nfoo\n\n---\n", true},
		{"no header", "## Original Task\n\nfoo\n\n---\n", false},
		{"no original task", "# Task Log\n\nsome notes\n", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := logLooksValid(tc.in); got != tc.want {
				t.Errorf("logLooksValid = %v, want %v", got, tc.want)
			}
		})
	}
}
