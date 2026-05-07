package tasklog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewCreatesSpecAsDraft(t *testing.T) {
	tl, err := New(t.TempDir(), "test prompt")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	data, err := os.ReadFile(tl.SpecPath())
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "**Status**: DRAFT") {
		t.Errorf("expected DRAFT status, spec was:\n%s", content)
	}
	if !strings.Contains(content, tl.TaskID) {
		t.Errorf("expected task ID %q in spec", tl.TaskID)
	}

	draft, err := tl.SpecIsDraft()
	if err != nil {
		t.Fatalf("SpecIsDraft: %v", err)
	}
	if !draft {
		t.Error("SpecIsDraft = false on freshly created spec, want true")
	}
}

func TestLoadBackfillsSpecForLegacyTask(t *testing.T) {
	base := t.TempDir()
	tl, err := New(base, "legacy task")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Simulate a legacy task by removing the spec.
	if err := os.Remove(tl.SpecPath()); err != nil {
		t.Fatalf("removing spec to simulate legacy: %v", err)
	}

	loaded, err := Load(tl.Dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := os.Stat(loaded.SpecPath()); err != nil {
		t.Errorf("Load did not backfill spec.md: %v", err)
	}

	draft, err := loaded.SpecIsDraft()
	if err != nil {
		t.Fatalf("SpecIsDraft: %v", err)
	}
	if !draft {
		t.Error("backfilled spec should be DRAFT")
	}
}

func TestInitializeSpecIsIdempotent(t *testing.T) {
	tl, err := New(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	custom := "# Spec\n\n**Status**: ACTIVE\n\n## Goal\n\ncustom contents\n"
	if err := os.WriteFile(tl.SpecPath(), []byte(custom), 0644); err != nil {
		t.Fatalf("writing custom spec: %v", err)
	}

	if err := tl.initializeSpec(); err != nil {
		t.Fatalf("initializeSpec second call: %v", err)
	}

	got, err := os.ReadFile(tl.SpecPath())
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	if string(got) != custom {
		t.Errorf("initializeSpec overwrote existing spec; got:\n%s", got)
	}
}

func TestSpecIsDraft(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"explicit DRAFT", "**Status**: DRAFT\n", true},
		{"explicit ACTIVE", "**Status**: ACTIVE\n", false},
		{"lowercase active", "**Status**: active\n", false},
		{"no status field", "# Spec\n\n## Goal\n\nblah\n", true},
		{"status with surrounding text", "## Header\n\n**Status**: ACTIVE  \nother stuff\n", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl, err := New(t.TempDir(), "test")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := os.WriteFile(tl.SpecPath(), []byte(tc.content), 0644); err != nil {
				t.Fatalf("writing spec: %v", err)
			}
			got, err := tl.SpecIsDraft()
			if err != nil {
				t.Fatalf("SpecIsDraft: %v", err)
			}
			if got != tc.want {
				t.Errorf("SpecIsDraft = %v, want %v\nspec:\n%s", got, tc.want, tc.content)
			}
		})
	}
}

func TestSpecIsDraftMissingFile(t *testing.T) {
	tl := &TaskLog{
		TaskID: "x",
		Dir:    t.TempDir(),
		Prompt: "x",
	}

	draft, err := tl.SpecIsDraft()
	if err != nil {
		t.Fatalf("SpecIsDraft: %v", err)
	}
	if !draft {
		t.Error("SpecIsDraft = false when spec missing, want true (treat missing as DRAFT)")
	}
}

func TestSpecAllCriteriaChecked(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no acceptance criteria section",
			content: "# Spec\n\n## Goal\n\nfoo\n",
			want:    false,
		},
		{
			name:    "section with no checkboxes",
			content: "## Acceptance Criteria\n\n(to be populated)\n",
			want:    false,
		},
		{
			name:    "all unchecked",
			content: "## Acceptance Criteria\n\n- [ ] one\n- [ ] two\n",
			want:    false,
		},
		{
			name:    "mixed checked and unchecked",
			content: "## Acceptance Criteria\n\n- [x] done\n- [ ] still open\n",
			want:    false,
		},
		{
			name:    "all checked lowercase",
			content: "## Acceptance Criteria\n\n- [x] one\n- [x] two\n",
			want:    true,
		},
		{
			name:    "all checked uppercase",
			content: "## Acceptance Criteria\n\n- [X] one\n- [X] two\n",
			want:    true,
		},
		{
			name:    "checkbox followed by another section",
			content: "## Acceptance Criteria\n\n- [x] one\n\n## Files in scope\n\n- [ ] not a criterion\n",
			want:    true,
		},
		{
			name:    "section is the last in the file",
			content: "## Goal\n\nfoo\n\n## Acceptance Criteria\n\n- [x] only one\n",
			want:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl, err := New(t.TempDir(), "test")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := os.WriteFile(tl.SpecPath(), []byte(tc.content), 0644); err != nil {
				t.Fatalf("writing spec: %v", err)
			}
			got, err := tl.SpecAllCriteriaChecked()
			if err != nil {
				t.Fatalf("SpecAllCriteriaChecked: %v", err)
			}
			if got != tc.want {
				t.Errorf("SpecAllCriteriaChecked = %v, want %v\nspec:\n%s", got, tc.want, tc.content)
			}
		})
	}
}

func TestSpecAllCriteriaCheckedMissingFile(t *testing.T) {
	tl := &TaskLog{
		TaskID: "x",
		Dir:    t.TempDir(),
		Prompt: "x",
	}

	got, err := tl.SpecAllCriteriaChecked()
	if err != nil {
		t.Fatalf("SpecAllCriteriaChecked: %v", err)
	}
	if got {
		t.Error("SpecAllCriteriaChecked = true when spec missing, want false")
	}
}

func TestParseTurnsBlocker(t *testing.T) {
	cases := []struct {
		name    string
		section string
		want    string
	}{
		{
			name: "no blocker field",
			section: `## Turn 1 - Agent Alpha

**Complete**: no

---`,
			want: "",
		},
		{
			name: "blocker field present and empty",
			section: `## Turn 1 - Agent Alpha

**Complete**: no
**Blocked on**:

---`,
			want: "",
		},
		{
			name: "single-line blocker",
			section: `## Turn 1 - Agent Alpha

**Complete**: no
**Blocked on**: Need decision on auth flow.

---`,
			want: "Need decision on auth flow.",
		},
		{
			name: "multiline blocker terminated by blank line",
			section: `## Turn 1 - Agent Alpha

**Complete**: no
**Blocked on**: Need decision on auth flow.
The criterion is ambiguous about PKCE vs implicit.

---`,
			want: "Need decision on auth flow.\nThe criterion is ambiguous about PKCE vs implicit.",
		},
		{
			name: "blocker followed immediately by another field",
			section: `## Turn 1 - Agent Alpha

**Blocked on**: Quick question about scope.
**Complete**: no

---`,
			want: "Quick question about scope.",
		},
		{
			name: "case insensitive field name",
			section: `## Turn 1 - Agent Alpha

**blocked ON**: lower-case detection works.

---`,
			want: "lower-case detection works.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			turns := parseTurns(tc.section)
			if len(turns) != 1 {
				t.Fatalf("expected 1 turn, got %d", len(turns))
			}
			if turns[0].Blocker != tc.want {
				t.Errorf("Blocker = %q, want %q", turns[0].Blocker, tc.want)
			}
		})
	}
}

func TestParseTurnsBlockerLastTurnOnly(t *testing.T) {
	content := `## Turn 1 - Agent Alpha

**Complete**: no
**Blocked on**: First turn was blocked.

---

## Turn 2 - Agent Beta

**Complete**: no

---
`

	turns := parseTurns(content)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[0].Blocker != "First turn was blocked." {
		t.Errorf("turn 1 Blocker = %q, want %q", turns[0].Blocker, "First turn was blocked.")
	}
	if turns[1].Blocker != "" {
		t.Errorf("turn 2 Blocker = %q, want empty (not blocked)", turns[1].Blocker)
	}
}

func TestAppendTimeoutTurn(t *testing.T) {
	tl, err := New(t.TempDir(), "test prompt")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := tl.AppendTimeoutTurn(3, "Agent Beta", 15*time.Minute); err != nil {
		t.Fatalf("AppendTimeoutTurn: %v", err)
	}

	turns, err := tl.ParseTurns()
	if err != nil {
		t.Fatalf("ParseTurns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].TurnNumber != 3 {
		t.Errorf("TurnNumber = %d, want 3", turns[0].TurnNumber)
	}
	if turns[0].AgentName != "Agent Beta" {
		t.Errorf("AgentName = %q, want %q", turns[0].AgentName, "Agent Beta")
	}
	if turns[0].Complete {
		t.Error("Complete = true, want false (timeout entries must always be incomplete)")
	}
	if turns[0].Blocker != "" {
		t.Errorf("Blocker = %q, want empty (timeouts are not human-escalated blocks)", turns[0].Blocker)
	}
}

func TestExtractTouchedFiles(t *testing.T) {
	cases := []struct {
		name     string
		activity []string
		want     []string
	}{
		{
			name:     "empty activity",
			activity: nil,
			want:     nil,
		},
		{
			name: "edits and writes",
			activity: []string{
				"Edit /tmp/foo.go",
				"Write /tmp/bar.go",
				"Bash go test ./...",
			},
			want: []string{"/tmp/foo.go", "/tmp/bar.go"},
		},
		{
			name: "deduplication",
			activity: []string{
				"Edit /tmp/foo.go",
				"Edit /tmp/foo.go",
				"Write /tmp/foo.go",
			},
			want: []string{"/tmp/foo.go"},
		},
		{
			name: "read-only activity",
			activity: []string{
				"Read /tmp/foo.go",
				"Grep \"pattern\"",
				"Bash ls",
			},
			want: nil,
		},
		{
			name: "multiedit and notebookedit",
			activity: []string{
				"MultiEdit /tmp/a.py",
				"NotebookEdit /tmp/b.ipynb",
			},
			want: []string{"/tmp/a.py", "/tmp/b.ipynb"},
		},
		{
			name: "ignores prefix-like substrings inside other tools",
			activity: []string{
				"Bash echo Edit /tmp/foo.go", // not a real Edit call
			},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTouchedFiles(tc.activity)
			if !equalStringSlices(got, tc.want) {
				t.Errorf("ExtractTouchedFiles = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteReadTurnFiles(t *testing.T) {
	tl, err := New(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	files := []string{"/tmp/foo.go", "/tmp/bar.go"}
	if err := tl.WriteTurnFiles(1, "Agent Alpha", files); err != nil {
		t.Fatalf("WriteTurnFiles: %v", err)
	}

	got, err := tl.ReadTurnFiles(1, "Agent Alpha")
	if err != nil {
		t.Fatalf("ReadTurnFiles: %v", err)
	}
	if !equalStringSlices(got, files) {
		t.Errorf("round trip = %v, want %v", got, files)
	}
}

func TestReadTurnFilesMissing(t *testing.T) {
	tl, err := New(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := tl.ReadTurnFiles(99, "Agent Beta")
	if err != nil {
		t.Errorf("ReadTurnFiles for missing file should not error, got: %v", err)
	}
	if got != nil {
		t.Errorf("ReadTurnFiles for missing file = %v, want nil", got)
	}
}

func TestWriteTurnFilesEmpty(t *testing.T) {
	tl, err := New(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := tl.WriteTurnFiles(1, "Agent Alpha", nil); err != nil {
		t.Fatalf("WriteTurnFiles with empty list: %v", err)
	}

	got, err := tl.ReadTurnFiles(1, "Agent Alpha")
	if err != nil {
		t.Fatalf("ReadTurnFiles: %v", err)
	}
	if got != nil {
		t.Errorf("ReadTurnFiles after writing empty = %v, want nil", got)
	}
}

func TestSpecFilesInScope(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want []string
	}{
		{
			name: "missing section",
			spec: "# Spec\n\n## Goal\n\nfoo\n",
			want: nil,
		},
		{
			name: "section with placeholder text only",
			spec: "## Files in scope\n\n(to be populated)\n",
			want: nil,
		},
		{
			name: "plain list",
			spec: "## Files in scope\n\n- internal/foo.go\n- internal/bar.go\n",
			want: []string{"internal/foo.go", "internal/bar.go"},
		},
		{
			name: "list with backticks around paths",
			spec: "## Files in scope\n\n- `internal/foo.go`\n- `internal/bar.go`\n",
			want: []string{"internal/foo.go", "internal/bar.go"},
		},
		{
			name: "section followed by another heading",
			spec: "## Files in scope\n\n- internal/foo.go\n\n## Out of scope\n\n- everything else\n",
			want: []string{"internal/foo.go"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl, err := New(t.TempDir(), "test")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := os.WriteFile(tl.SpecPath(), []byte(tc.spec), 0644); err != nil {
				t.Fatalf("writing spec: %v", err)
			}
			got, err := tl.SpecFilesInScope()
			if err != nil {
				t.Fatalf("SpecFilesInScope: %v", err)
			}
			if !equalStringSlices(got, tc.want) {
				t.Errorf("SpecFilesInScope = %v, want %v", got, tc.want)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWindowedContent(t *testing.T) {
	// Helper: build a task log body with N turn entries and an optional list
	// of (afterTurn, directiveText) injection points.
	build := func(numTurns int, directives map[int]string) string {
		var b strings.Builder
		b.WriteString("# Task Log\n\n**Task ID**: x\n**Started**: now\n\n## Original Task\n\nbuild a thing\n\n---\n\n")
		if d, ok := directives[0]; ok {
			fmt.Fprintf(&b, "## User Directive\n\n%s\n\n---\n\n", d)
		}
		for i := 1; i <= numTurns; i++ {
			agent := "Agent Alpha"
			if i%2 == 0 {
				agent = "Agent Beta"
			}
			fmt.Fprintf(&b, "## Turn %d - %s\n\n**Complete**: no\n\n---\n\n", i, agent)
			if d, ok := directives[i]; ok {
				fmt.Fprintf(&b, "## User Directive\n\n%s\n\n---\n\n", d)
			}
		}
		return b.String()
	}

	writeLog := func(t *testing.T, body string) *TaskLog {
		t.Helper()
		tl, err := New(t.TempDir(), "test")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := os.WriteFile(tl.Path(), []byte(body), 0644); err != nil {
			t.Fatalf("writing log: %v", err)
		}
		return tl
	}

	t.Run("window=0 returns full content", func(t *testing.T) {
		body := build(20, nil)
		tl := writeLog(t, body)
		got, err := tl.WindowedContent(0)
		if err != nil {
			t.Fatalf("WindowedContent: %v", err)
		}
		if got != body {
			t.Errorf("expected full content with window=0; differed")
		}
	})

	t.Run("turn count <= window returns full content", func(t *testing.T) {
		body := build(4, nil)
		tl := writeLog(t, body)
		got, err := tl.WindowedContent(6)
		if err != nil {
			t.Fatalf("WindowedContent: %v", err)
		}
		if got != body {
			t.Errorf("expected full content when turns <= window; differed")
		}
	})

	t.Run("windowing drops mid-range turns and inserts marker", func(t *testing.T) {
		body := build(20, nil)
		tl := writeLog(t, body)
		got, err := tl.WindowedContent(4)
		if err != nil {
			t.Fatalf("WindowedContent: %v", err)
		}
		if !strings.Contains(got, "Turns 1–16 omitted") {
			t.Errorf("missing omission marker for range; got:\n%s", got)
		}
		// Last 4 turns must be present.
		for n := 17; n <= 20; n++ {
			marker := fmt.Sprintf("## Turn %d - ", n)
			if !strings.Contains(got, marker) {
				t.Errorf("expected %q in windowed output", marker)
			}
		}
		// Earlier turns must be gone.
		for _, n := range []int{1, 5, 10, 16} {
			marker := fmt.Sprintf("## Turn %d - ", n)
			if strings.Contains(got, marker) {
				t.Errorf("turn %d should have been omitted; got:\n%s", n, got)
			}
		}
	})

	t.Run("user directives are preserved across windowing", func(t *testing.T) {
		body := build(20, map[int]string{
			5:  "directive-after-turn-5",
			15: "directive-after-turn-15",
		})
		tl := writeLog(t, body)
		got, err := tl.WindowedContent(4)
		if err != nil {
			t.Fatalf("WindowedContent: %v", err)
		}
		if !strings.Contains(got, "directive-after-turn-5") {
			t.Error("directive originally between dropped turns must still be present")
		}
		if !strings.Contains(got, "directive-after-turn-15") {
			t.Error("directive between dropped turns must still be present")
		}
		if !strings.Contains(got, "Turns 1–16 omitted") {
			t.Error("missing omission marker")
		}
	})

	t.Run("singular omission marker when only one turn dropped", func(t *testing.T) {
		body := build(5, nil)
		tl := writeLog(t, body)
		got, err := tl.WindowedContent(4)
		if err != nil {
			t.Fatalf("WindowedContent: %v", err)
		}
		if !strings.Contains(got, "Turn 1 omitted") {
			t.Errorf("expected singular 'Turn 1 omitted' marker; got:\n%s", got)
		}
	})
}

func TestRelocateRefusesNestedDestination(t *testing.T) {
	// Reproduces the bug where an agent reported the task dir itself as
	// Working Directory: Relocate(taskDir) would compute a destination
	// inside the source and fail with an opaque "invalid argument".
	base := t.TempDir()
	tl, err := New(base, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Reporting the current task dir as the new base would produce a destination
	// at <taskDir>/.departai/tasks/<id> — strictly inside <taskDir>. Refuse.
	err = tl.Relocate(tl.Dir)
	if err == nil {
		t.Fatal("expected error when destination is inside source, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to relocate") {
		t.Errorf("expected refusal error, got: %v", err)
	}

	// Task dir must still exist at the original location after the refused move.
	if _, statErr := os.Stat(tl.Dir); statErr != nil {
		t.Errorf("task dir disappeared after refused relocate: %v", statErr)
	}
}

func TestSpecPathIsInsideTaskDir(t *testing.T) {
	tl, err := New(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := filepath.Dir(tl.SpecPath()), tl.Dir; got != want {
		t.Errorf("spec path dir = %q, want %q", got, want)
	}
	if got, want := filepath.Base(tl.SpecPath()), specFileName; got != want {
		t.Errorf("spec path base = %q, want %q", got, want)
	}
}
