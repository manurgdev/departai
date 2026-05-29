package codex

import (
	"testing"
)

func TestParseStreamLine_AgentMessage(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"  hello world  "}}`)
	got := ParseStreamLine(line)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Kind != "text" || got[0].Text != "hello world" {
		t.Errorf("unexpected event: %+v", got[0])
	}
}

func TestParseStreamLine_AgentMessageEmpty(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"   "}}`)
	if got := ParseStreamLine(line); got != nil {
		t.Errorf("whitespace-only message should yield nil, got %+v", got)
	}
}

func TestParseStreamLine_CommandCompleted(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"type":"command_execution","command":"/bin/zsh -lc \"go test ./...\""}}`)
	got := ParseStreamLine(line)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Kind != "tool" || got[0].Tool != "Bash" {
		t.Errorf("unexpected kind/tool: %+v", got[0])
	}
	if got[0].Detail != "go test ./..." {
		t.Errorf("Detail = %q, want unwrapped %q", got[0].Detail, "go test ./...")
	}
}

func TestParseStreamLine_CommandStarted(t *testing.T) {
	line := []byte(`{"type":"item.started","item":{"type":"command_execution","command":"ls -la"}}`)
	got := ParseStreamLine(line)
	if len(got) != 1 || got[0].Kind != "tool" || got[0].Detail != "ls -la" {
		t.Errorf("unexpected event: %+v", got)
	}
}

func TestParseStreamLine_StartedNonCommand(t *testing.T) {
	// item.started for a non-command item type yields nothing.
	line := []byte(`{"type":"item.started","item":{"type":"agent_message","text":"thinking"}}`)
	if got := ParseStreamLine(line); got != nil {
		t.Errorf("started agent_message should yield nil, got %+v", got)
	}
}

func TestParseStreamLine_IgnoredAndMalformed(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"turn.completed", `{"type":"turn.completed"}`},
		{"unknown type", `{"type":"session.created"}`},
		{"completed nil item", `{"type":"item.completed"}`},
		{"started nil item", `{"type":"item.started"}`},
		{"malformed json", `not json at all`},
		{"empty line", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseStreamLine([]byte(tc.line)); got != nil {
				t.Errorf("expected nil, got %+v", got)
			}
		})
	}
}

func TestUnwrapShellCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"zsh -lc", `/bin/zsh -lc "go build ./..."`, "go build ./..."},
		{"bash -lc", `/bin/bash -lc "echo hi"`, "echo hi"},
		{"bash -c", `/bin/bash -c "ls"`, "ls"},
		{"zsh -c", `/bin/zsh -c "pwd"`, "pwd"},
		{"sh -c", `/bin/sh -c "whoami"`, "whoami"},
		{"escaped inner quotes", `/bin/zsh -lc "echo \"hello\""`, `echo "hello"`},
		{"no wrapper", "ls -la", "ls -la"},
		{"wrapper-like but no trailing quote", `/bin/zsh -lc "unterminated`, `/bin/zsh -lc "unterminated`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := unwrapShellCommand(tc.in); got != tc.want {
				t.Errorf("unwrapShellCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSingleLine(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  foo  ", "foo"},
		{"foo\nbar", "foo bar"},
		{"  a\nb\nc  ", "a b c"},
		{"noNewlines", "noNewlines"},
	}
	for _, tc := range cases {
		if got := singleLine(tc.in); got != tc.want {
			t.Errorf("singleLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
