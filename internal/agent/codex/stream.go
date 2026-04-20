// Package codex — stream.go provides parsing for the Codex CLI --json
// (JSONL) output format, converting events into agent.StreamEvent.
package codex

import (
	"encoding/json"
	"strings"

	"github.com/manurgdev/departai/internal/agent"
)

// ── JSON wire types ─────────────────────────────────────────────────────────

type jsonLine struct {
	Type string   `json:"type"`
	Item *jsonItem `json:"item,omitempty"`
}

type jsonItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"` // "agent_message", "command_execution"
	Text             string `json:"text,omitempty"`
	Command          string `json:"command,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	Status           string `json:"status,omitempty"`
}

// ── event extraction ────────────────────────────────────────────────────────

// ParseStreamLine extracts displayable events from one line of Codex JSONL.
func ParseStreamLine(line []byte) []agent.StreamEvent {
	var jl jsonLine
	if err := json.Unmarshal(line, &jl); err != nil {
		return nil
	}

	switch jl.Type {
	case "item.completed":
		if jl.Item == nil {
			return nil
		}
		return parseCompletedItem(jl.Item)

	case "item.started":
		if jl.Item == nil {
			return nil
		}
		return parseStartedItem(jl.Item)

	case "turn.completed":
		// Codex turn.completed doesn't carry the result text — the last
		// agent_message item before it IS the result. We handle this in
		// the agent's RunTurn by tracking the last message.
		return nil
	}

	return nil
}

func parseCompletedItem(item *jsonItem) []agent.StreamEvent {
	switch item.Type {
	case "agent_message":
		text := strings.TrimSpace(item.Text)
		if text == "" {
			return nil
		}
		return []agent.StreamEvent{{Kind: "text", Text: text}}

	case "command_execution":
		cmd := singleLine(item.Command)
		// Strip the shell wrapper (e.g., "/bin/zsh -lc \"...\"")
		cmd = unwrapShellCommand(cmd)
		return []agent.StreamEvent{{
			Kind:   "tool",
			Tool:   "Bash",
			Detail: cmd,
		}}
	}

	return nil
}

func parseStartedItem(item *jsonItem) []agent.StreamEvent {
	if item.Type != "command_execution" {
		return nil
	}
	// Show the command as soon as it starts (don't wait for completion).
	cmd := singleLine(item.Command)
	cmd = unwrapShellCommand(cmd)
	return []agent.StreamEvent{{
		Kind:   "tool",
		Tool:   "Bash",
		Detail: cmd,
	}}
}

// unwrapShellCommand strips common shell wrappers like `/bin/zsh -lc "..."`.
func unwrapShellCommand(cmd string) string {
	// Pattern: /bin/zsh -lc "actual command" or /bin/bash -c "actual command"
	for _, prefix := range []string{
		`/bin/zsh -lc "`, `/bin/bash -lc "`, `/bin/sh -c "`,
		`/bin/zsh -c "`, `/bin/bash -c "`,
	} {
		if strings.HasPrefix(cmd, prefix) && strings.HasSuffix(cmd, `"`) {
			inner := cmd[len(prefix) : len(cmd)-1]
			// Unescape inner quotes
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			return inner
		}
	}
	return cmd
}

func singleLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
