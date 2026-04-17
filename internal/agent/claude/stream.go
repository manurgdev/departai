// Package claude — stream.go provides minimal parsing for the Claude CLI
// stream-json output format. Only the fields needed for live display and
// result extraction are decoded; everything else is silently ignored.
package claude

import (
	"encoding/json"
	"fmt"
	"strings"
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

// StreamEvent is a simplified representation of something worth showing
// to the user or logging.
type StreamEvent struct {
	Kind   string // "tool", "text", "result"
	Tool   string // tool name (only when Kind == "tool")
	Detail string // human-readable detail (file path, command, pattern, …)
	Text   string // agent reasoning / narrative (only when Kind == "text")
	Result string // final output text (only when Kind == "result")
}

// ParseStreamLine extracts all displayable events from one line of
// stream-json output. Returns nil for lines that should be silently consumed
// (system init, rate limits, etc.).
//
// A single assistant message may contain multiple content blocks (text + tool_use),
// so this returns a slice.
func ParseStreamLine(line []byte) []StreamEvent {
	var sl streamLine
	if err := json.Unmarshal(line, &sl); err != nil {
		return nil
	}

	switch sl.Type {
	case "result":
		return []StreamEvent{{Kind: "result", Result: sl.Result}}

	case "assistant":
		if sl.Message == nil {
			return nil
		}
		var events []StreamEvent
		for _, block := range sl.Message.Content {
			switch block.Type {
			case "text":
				if text := strings.TrimSpace(block.Text); text != "" {
					events = append(events, StreamEvent{Kind: "text", Text: text})
				}
			case "tool_use":
				if block.Name != "" {
					events = append(events, StreamEvent{
						Kind:   "tool",
						Tool:   block.Name,
						Detail: extractToolDetail(block.Name, block.Input),
					})
				}
			}
		}
		return events
	}

	return nil
}

// ── tool detail helpers ─────────────────────────────────────────────────────

// extractToolDetail pulls the most relevant field from a tool_use input
// so the live display can show a concise description of what's happening.
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
		return truncate(cmd, 80)
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

// jsonStringField extracts a top-level string field from a JSON object.
// Returns "" on any error.
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

// truncate shortens s to maxLen characters, adding "…" if truncated.
func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
