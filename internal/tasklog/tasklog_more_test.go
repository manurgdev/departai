package tasklog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// appendToLog is a small helper to append raw markdown to a task log file.
func appendToLog(t *testing.T, tl *TaskLog, text string) {
	t.Helper()
	f, err := os.OpenFile(tl.Path(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("opening log: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatalf("appending to log: %v", err)
	}
}

func TestAppendUserDirective(t *testing.T) {
	tl, err := New(t.TempDir(), "initial task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := tl.AppendUserDirective("also add OAuth login"); err != nil {
		t.Fatalf("AppendUserDirective: %v", err)
	}

	content, err := tl.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(content, "## User Directive") {
		t.Error("expected a ## User Directive heading in the log")
	}
	if !strings.Contains(content, "also add OAuth login") {
		t.Error("expected the directive text in the log")
	}
}

func TestLoadPreservesPromptAndTurns(t *testing.T) {
	base := t.TempDir()
	tl, err := New(base, "build a parser")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	appendToLog(t, tl, "\n## Turn 1 - Agent Alpha\n\n**Complete**: no\n\n---\n")

	loaded, err := Load(tl.Dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Prompt != "build a parser" {
		t.Errorf("Load Prompt = %q, want %q", loaded.Prompt, "build a parser")
	}
	turns, err := loaded.ParseTurns()
	if err != nil {
		t.Fatalf("ParseTurns: %v", err)
	}
	if len(turns) != 1 || turns[0].AgentName != "Agent Alpha" {
		t.Errorf("expected 1 turn for Agent Alpha, got %+v", turns)
	}
}

func TestListTasks(t *testing.T) {
	base := t.TempDir()

	// No tasks dir yet → empty, no error.
	got, err := ListTasks(base)
	if err != nil {
		t.Fatalf("ListTasks (empty): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 tasks before any created, got %d", len(got))
	}

	// Create two tasks; IDs are timestamp-prefixed so ordering is by recency.
	first, err := New(base, "first task")
	if err != nil {
		t.Fatalf("New first: %v", err)
	}
	appendToLog(t, first, "\n## Turn 1 - Agent Alpha\n\n**Complete**: no\n\n---\n")

	second, err := New(base, "second task")
	if err != nil {
		t.Fatalf("New second: %v", err)
	}

	got, err = ListTasks(base)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(got))
	}

	// Verify summaries carry prompt + turn count, keyed by TaskID.
	byID := map[string]TaskSummary{}
	for _, s := range got {
		byID[s.TaskID] = s
	}
	if s := byID[first.TaskID]; s.Prompt != "first task" || s.TurnCount != 1 {
		t.Errorf("first summary = %+v, want prompt 'first task' + 1 turn", s)
	}
	if s := byID[second.TaskID]; s.Prompt != "second task" || s.TurnCount != 0 {
		t.Errorf("second summary = %+v, want prompt 'second task' + 0 turns", s)
	}

	// Sorted newest-first: second was created after first.
	if got[0].TaskID < got[1].TaskID {
		t.Errorf("expected newest-first ordering, got %q before %q", got[0].TaskID, got[1].TaskID)
	}
}

func TestListTasksSkipsNonTaskDirs(t *testing.T) {
	base := t.TempDir()
	if _, err := New(base, "real task"); err != nil {
		t.Fatalf("New: %v", err)
	}
	// A directory without a task-log.md must be skipped, not error.
	junk := filepath.Join(base, ".departai", "tasks", "not-a-task")
	if err := os.MkdirAll(junk, 0755); err != nil {
		t.Fatalf("mkdir junk: %v", err)
	}

	got, err := ListTasks(base)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected only the real task, got %d summaries", len(got))
	}
}

func TestBothAgentsAgreeComplete(t *testing.T) {
	cases := []struct {
		name  string
		turns string
		want  bool
	}{
		{
			"fewer than two turns",
			"\n## Turn 1 - Agent Alpha\n\n**Complete**: yes\n\n---\n",
			false,
		},
		{
			"last two both yes",
			"\n## Turn 1 - Agent Alpha\n\n**Complete**: yes\n\n---\n" +
				"\n## Turn 2 - Agent Beta\n\n**Complete**: yes\n\n---\n",
			true,
		},
		{
			"last is no",
			"\n## Turn 1 - Agent Alpha\n\n**Complete**: yes\n\n---\n" +
				"\n## Turn 2 - Agent Beta\n\n**Complete**: no\n\n---\n",
			false,
		},
		{
			"second-last is no",
			"\n## Turn 1 - Agent Alpha\n\n**Complete**: no\n\n---\n" +
				"\n## Turn 2 - Agent Beta\n\n**Complete**: yes\n\n---\n",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl, err := New(t.TempDir(), "task")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			appendToLog(t, tl, tc.turns)
			got, err := tl.BothAgentsAgreeComplete()
			if err != nil {
				t.Fatalf("BothAgentsAgreeComplete: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseLastWorkingDir(t *testing.T) {
	tl, err := New(t.TempDir(), "task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// No turn with a Working Directory yet.
	got, err := tl.ParseLastWorkingDir()
	if err != nil {
		t.Fatalf("ParseLastWorkingDir: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty before any turn, got %q", got)
	}

	appendToLog(t, tl, "\n## Turn 1 - Agent Alpha\n\n**Working Directory**: /home/user/proj-a\n\n**Complete**: no\n\n---\n")
	appendToLog(t, tl, "\n## Turn 2 - Agent Beta\n\n**Working Directory**: /home/user/proj-b\n\n**Complete**: no\n\n---\n")

	got, err = tl.ParseLastWorkingDir()
	if err != nil {
		t.Fatalf("ParseLastWorkingDir: %v", err)
	}
	if got != "/home/user/proj-b" {
		t.Errorf("ParseLastWorkingDir = %q, want the most recent /home/user/proj-b", got)
	}
}

func TestRelocateMovesTaskDir(t *testing.T) {
	srcBase := t.TempDir()
	tl, err := New(srcBase, "relocatable task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	oldDir := tl.Dir

	dstBase := t.TempDir()
	if err := tl.Relocate(dstBase); err != nil {
		t.Fatalf("Relocate: %v", err)
	}

	wantDir := filepath.Join(dstBase, ".departai", "tasks", tl.TaskID)
	if tl.Dir != wantDir {
		t.Errorf("tl.Dir = %q, want %q", tl.Dir, wantDir)
	}
	if _, err := os.Stat(tl.Path()); err != nil {
		t.Errorf("task log not present at new location: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old task dir should no longer exist, stat err = %v", err)
	}
}

func TestRelocateNoOpWhenSameLocation(t *testing.T) {
	base := t.TempDir()
	tl, err := New(base, "task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before := tl.Dir
	// Relocating to the same base resolves to the same task dir → no-op.
	if err := tl.Relocate(base); err != nil {
		t.Fatalf("Relocate (same base): %v", err)
	}
	if tl.Dir != before {
		t.Errorf("Dir changed on no-op relocate: %q → %q", before, tl.Dir)
	}
}

func TestCountSpecDecisions(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want int
	}{
		{"no section", "# Spec\n\n## Goal\nfoo\n", 0},
		{"empty placeholder", "## Decisions log\n\n(empty)\n", 0},
		{
			"two bullets",
			"## Decisions log\n\n- alpha: chose X\n- beta: verified Y\n",
			2,
		},
		{
			"bullet with continuation line",
			"## Decisions log\n\n- alpha: chose X\n  because of Z\n- beta: ok\n",
			2,
		},
		{
			"stops at next section",
			"## Decisions log\n\n- alpha: one\n\n## Open questions\n\n- not a decision\n",
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountSpecDecisions(tc.spec); got != tc.want {
				t.Errorf("CountSpecDecisions = %d, want %d", got, tc.want)
			}
		})
	}
}
