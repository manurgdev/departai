// Package claude — stream.go provides minimal parsing for the Claude CLI
// stream-json output format. Only the fields needed for live display and
// result extraction are decoded; everything else is silently ignored.
package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/manurgdev/departai/internal/agent"
)

// ── JSON wire types ─────────────────────────────────────────────────────────

type streamLine struct {
	Type    string     `json:"type"`
	Message *streamMsg `json:"message,omitempty"`
	Result  string     `json:"result,omitempty"`
}

type streamMsg struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`            // "text", "tool_use"
	Name  string          `json:"name,omitempty"`  // tool name for tool_use
	Text  string          `json:"text,omitempty"`  // text content
	Input json.RawMessage `json:"input,omitempty"` // tool input (parsed per-tool)
}

// ── event extraction ────────────────────────────────────────────────────────

// ParseStreamLine extracts all displayable events from one line of
// stream-json output. Returns nil for lines that should be silently consumed
// (system init, rate limits, etc.).
func ParseStreamLine(line []byte) []agent.StreamEvent {
	var sl streamLine
	if err := json.Unmarshal(line, &sl); err != nil {
		return nil
	}

	switch sl.Type {
	case "result":
		return []agent.StreamEvent{{Kind: "result", Result: sl.Result}}

	case "assistant":
		if sl.Message == nil {
			return nil
		}
		var events []agent.StreamEvent
		for _, block := range sl.Message.Content {
			switch block.Type {
			case "text":
				if text := strings.TrimSpace(block.Text); text != "" {
					events = append(events, agent.StreamEvent{Kind: "text", Text: text})
				}
			case "tool_use":
				if block.Name != "" {
					evt := agent.StreamEvent{
						Kind:   "tool",
						Tool:   block.Name,
						Detail: extractToolDetail(block.Name, block.Input),
					}
					if block.Name == "Edit" {
						evt.DiffOld = jsonStringField(block.Input, "old_string")
						evt.DiffNew = jsonStringField(block.Input, "new_string")
					}
					events = append(events, evt)
				}
			}
		}
		return events
	}

	return nil
}

// ── tool detail helpers ─────────────────────────────────────────────────────

func extractToolDetail(name string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	switch name {
	case "Read":
		return jsonStringField(raw, "file_path")
	case "Edit":
		return jsonStringField(raw, "file_path")
	case "Write":
		return jsonStringField(raw, "file_path")
	case "NotebookEdit":
		return jsonStringField(raw, "file_path")
	case "Bash":
		cmd := jsonStringField(raw, "command")
		return singleLine(cmd)
	case "Grep":
		return fmt.Sprintf("%q", jsonStringField(raw, "pattern"))
	case "Glob":
		return jsonStringField(raw, "pattern")
	case "WebFetch":
		return jsonStringField(raw, "url")
	case "WebSearch":
		return jsonStringField(raw, "query")
	default:
		return ""
	}
}

func jsonStringField(raw json.RawMessage, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

func singleLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
