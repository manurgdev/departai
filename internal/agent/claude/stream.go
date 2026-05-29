// Package claude — stream.go provides parsing for the Claude CLI
// stream-json output format.
//
// Two modes are supported:
//
//  1. Whole-block mode: the CLI emits `assistant` messages with COMPLETE
//     content blocks. ParseStreamLine handles this stateless-ly.
//  2. Partial-message mode: with `--include-partial-messages`, the CLI also
//     emits `stream_event` lines carrying token-level deltas inside open
//     content blocks. Use Parser to consume these — it tracks open blocks
//     and emits agent.StreamEvents the TUI can use to update entries
//     in-place (matched via BlockID) as text streams in.
//
// When partial messages are seen, the final `assistant` message is suppressed
// to avoid rendering the same content twice.
package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/manurgdev/departai/internal/agent"
)

// ── JSON wire types ─────────────────────────────────────────────────────────

type streamLine struct {
	Type    string         `json:"type"`
	Message *streamMsg     `json:"message,omitempty"`
	Result  string         `json:"result,omitempty"`
	Event   *streamEventEv `json:"event,omitempty"` // for type == "stream_event"
}

type streamMsg struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`           // "text", "tool_use"
	Name  string          `json:"name,omitempty"` // tool name for tool_use
	Text  string          `json:"text,omitempty"` // text content
	Input json.RawMessage `json:"input,omitempty"`
}

// streamEventEv is the inner `event` object on a `type: "stream_event"` line.
type streamEventEv struct {
	Type         string        `json:"type"` // "message_start", "content_block_start", "content_block_delta", "content_block_stop", ...
	Index        int           `json:"index,omitempty"`
	ContentBlock *contentBlock `json:"content_block,omitempty"`
	Delta        *streamDelta  `json:"delta,omitempty"`
}

type streamDelta struct {
	Type        string `json:"type"` // "text_delta", "input_json_delta"
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// ── stateful parser ─────────────────────────────────────────────────────────

// Parser tracks open content blocks across stream-json lines. Required when
// the CLI was invoked with `--include-partial-messages`; safe (degrades to
// stateless behavior) when it wasn't.
type Parser struct {
	// blocks maps stream-event index → accumulated state for that block.
	// Reset on each `message_start` because indexes restart at 0 per message.
	blocks map[int]*blockState
	// blockIDs maps stream-event index → globally unique block ID. The TUI
	// uses these IDs to deduplicate entries across delta events.
	blockIDs map[int]int
	// nextBlockID is monotonically increasing across the whole turn.
	nextBlockID int
	// sawPartial is true once any `stream_event` line has been observed.
	// Suppresses the final `assistant` message to avoid duplicate rendering.
	sawPartial bool
}

type blockState struct {
	kind      string // "text" or "tool_use"
	toolName  string
	text      strings.Builder
	inputJSON strings.Builder
}

// NewParser returns a fresh Parser ready to consume one turn's worth of
// stream-json output.
func NewParser() *Parser {
	return &Parser{
		blocks:   map[int]*blockState{},
		blockIDs: map[int]int{},
	}
}

// Parse consumes one line of stream-json and returns zero or more events.
func (p *Parser) Parse(line []byte) []agent.StreamEvent {
	var sl streamLine
	if err := json.Unmarshal(line, &sl); err != nil {
		return nil
	}

	switch sl.Type {
	case "result":
		return []agent.StreamEvent{{Kind: "result", Result: sl.Result}}

	case "stream_event":
		if sl.Event == nil {
			return nil
		}
		return p.handleStreamEvent(sl.Event)

	case "assistant":
		// Once we've seen partial events, the consolidated assistant message
		// is redundant — every block was already rendered via deltas.
		if p.sawPartial {
			return nil
		}
		return parseAssistantBlocks(sl.Message)
	}

	return nil
}

func (p *Parser) handleStreamEvent(e *streamEventEv) []agent.StreamEvent {
	switch e.Type {
	case "message_start":
		// New message — block indexes restart. Block IDs do not.
		p.blocks = map[int]*blockState{}
		p.blockIDs = map[int]int{}
		return nil

	case "content_block_start":
		if e.ContentBlock == nil {
			return nil
		}
		p.sawPartial = true
		p.nextBlockID++
		bid := p.nextBlockID
		bs := &blockState{
			kind:     e.ContentBlock.Type,
			toolName: e.ContentBlock.Name,
		}
		p.blocks[e.Index] = bs
		p.blockIDs[e.Index] = bid

		switch bs.kind {
		case "text":
			// Empty text entry the TUI will grow as deltas arrive.
			return []agent.StreamEvent{{Kind: "text", Text: "", BlockID: bid}}
		case "tool_use":
			// Placeholder so the TUI can show the tool as in-flight from
			// the moment the block opens. The finalized "tool" event with
			// Detail comes at content_block_stop.
			return []agent.StreamEvent{{Kind: "tool_start", Tool: bs.toolName, BlockID: bid}}
		}
		return nil

	case "content_block_delta":
		bs, ok := p.blocks[e.Index]
		if !ok || e.Delta == nil {
			return nil
		}
		bid := p.blockIDs[e.Index]

		switch e.Delta.Type {
		case "text_delta":
			bs.text.WriteString(e.Delta.Text)
			// Emit the full accumulated text — TUI matches by BlockID and
			// replaces the previous title in place.
			return []agent.StreamEvent{{Kind: "text", Text: bs.text.String(), BlockID: bid}}

		case "input_json_delta":
			// Tool input is streamed as JSON fragments. Accumulate silently;
			// emit nothing until content_block_stop where we parse the full
			// JSON. The user already sees the tool as in-flight (spinner +
			// timer) so partial JSON noise wouldn't help.
			bs.inputJSON.WriteString(e.Delta.PartialJSON)
			return nil
		}
		return nil

	case "content_block_stop":
		bs, ok := p.blocks[e.Index]
		if !ok {
			return nil
		}
		bid := p.blockIDs[e.Index]
		delete(p.blocks, e.Index)
		delete(p.blockIDs, e.Index)

		blockEnd := agent.StreamEvent{Kind: "block_end", BlockID: bid}

		switch bs.kind {
		case "tool_use":
			// Now that the input JSON is complete, parse it for Detail and
			// (for Edit) the diff old/new strings.
			input := json.RawMessage(bs.inputJSON.String())
			evt := agent.StreamEvent{
				Kind:    "tool",
				Tool:    bs.toolName,
				Detail:  extractToolDetail(bs.toolName, input),
				BlockID: bid,
			}
			if bs.toolName == "Edit" {
				evt.DiffOld = jsonStringField(input, "old_string")
				evt.DiffNew = jsonStringField(input, "new_string")
			}
			return []agent.StreamEvent{evt, blockEnd}
		case "text":
			return []agent.StreamEvent{blockEnd}
		}
		return []agent.StreamEvent{blockEnd}
	}

	return nil
}

// ── stateless / legacy entry point ──────────────────────────────────────────

// ParseStreamLine retains the old free-function signature for backwards
// compatibility — it does NOT track open blocks across calls, so it works
// only for whole-block streaming (no `--include-partial-messages`).
func ParseStreamLine(line []byte) []agent.StreamEvent {
	var sl streamLine
	if err := json.Unmarshal(line, &sl); err != nil {
		return nil
	}
	switch sl.Type {
	case "result":
		return []agent.StreamEvent{{Kind: "result", Result: sl.Result}}
	case "assistant":
		return parseAssistantBlocks(sl.Message)
	}
	return nil
}

// parseAssistantBlocks converts a complete assistant message's content blocks
// into agent.StreamEvents (legacy path).
func parseAssistantBlocks(msg *streamMsg) []agent.StreamEvent {
	if msg == nil {
		return nil
	}
	var events []agent.StreamEvent
	for _, block := range msg.Content {
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
