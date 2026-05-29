package claude

import (
	"reflect"
	"testing"

	"github.com/manurgdev/departai/internal/agent"
)

// ── legacy whole-block path ────────────────────────────────────────────────

func TestParseStreamLine_AssistantText(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`)
	got := ParseStreamLine(line)
	want := []agent.StreamEvent{{Kind: "text", Text: "hello"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseStreamLine_AssistantToolUse(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/foo.go"}}]}}`)
	got := ParseStreamLine(line)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(got), got)
	}
	if got[0].Kind != "tool" || got[0].Tool != "Read" || got[0].Detail != "/foo.go" {
		t.Errorf("unexpected event: %+v", got[0])
	}
	if got[0].BlockID != 0 {
		t.Errorf("legacy path should leave BlockID=0, got %d", got[0].BlockID)
	}
}

func TestParseStreamLine_Result(t *testing.T) {
	line := []byte(`{"type":"result","result":"all done"}`)
	got := ParseStreamLine(line)
	want := []agent.StreamEvent{{Kind: "result", Result: "all done"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseStreamLine_EditDiff(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/x.go","old_string":"foo","new_string":"bar"}}]}}`)
	got := ParseStreamLine(line)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].DiffOld != "foo" || got[0].DiffNew != "bar" {
		t.Errorf("diff fields not extracted: %+v", got[0])
	}
}

// ── partial-message path ───────────────────────────────────────────────────

func TestParser_TextBlockStreaming(t *testing.T) {
	p := NewParser()

	// content_block_start (text, idx 0) → starter event with empty Text.
	got := p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`))
	if len(got) != 1 || got[0].Kind != "text" || got[0].Text != "" || got[0].BlockID != 1 {
		t.Errorf("start: got %+v, want one text event with BlockID=1", got)
	}

	// content_block_delta text "hello "
	got = p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}}`))
	if len(got) != 1 || got[0].Text != "hello " || got[0].BlockID != 1 {
		t.Errorf("delta1: got %+v", got)
	}

	// content_block_delta text "world"
	got = p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}}`))
	if len(got) != 1 || got[0].Text != "hello world" || got[0].BlockID != 1 {
		t.Errorf("delta2 should carry accumulated text: got %+v", got)
	}

	// content_block_stop → block_end
	got = p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`))
	if len(got) != 1 || got[0].Kind != "block_end" || got[0].BlockID != 1 {
		t.Errorf("stop: got %+v, want block_end with BlockID=1", got)
	}
}

func TestParser_ToolUseBlock(t *testing.T) {
	p := NewParser()

	// content_block_start (tool_use Bash) → tool_start placeholder.
	got := p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","name":"Bash"}}}`))
	if len(got) != 1 || got[0].Kind != "tool_start" || got[0].Tool != "Bash" || got[0].BlockID != 1 {
		t.Errorf("tool_start: got %+v", got)
	}
	if got[0].Detail != "" {
		t.Errorf("placeholder should have empty Detail, got %q", got[0].Detail)
	}

	// input_json_delta — parser accumulates silently, emits nothing.
	got = p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\""}}}`))
	if len(got) != 0 {
		t.Errorf("input_json_delta should be silent, got %+v", got)
	}
	got = p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ls -la\"}"}}}`))
	if len(got) != 0 {
		t.Errorf("input_json_delta should be silent, got %+v", got)
	}

	// content_block_stop → finalized tool event + block_end.
	got = p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`))
	if len(got) != 2 {
		t.Fatalf("stop should emit 2 events (tool + block_end), got %d: %+v", len(got), got)
	}
	if got[0].Kind != "tool" || got[0].Tool != "Bash" || got[0].Detail != "ls -la" || got[0].BlockID != 1 {
		t.Errorf("final tool event: got %+v", got[0])
	}
	if got[1].Kind != "block_end" || got[1].BlockID != 1 {
		t.Errorf("block_end: got %+v", got[1])
	}
}

func TestParser_SuppressesAssistantAfterPartial(t *testing.T) {
	p := NewParser()

	// Receiving any stream_event triggers suppression of subsequent assistant.
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"message_start"}}`))
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`))
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}`))

	// Now an assistant final message arrives — must be ignored.
	got := p.Parse([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`))
	if len(got) != 0 {
		t.Errorf("assistant after partial events should be suppressed, got %+v", got)
	}
}

func TestParser_LegacyFallbackWithoutPartial(t *testing.T) {
	p := NewParser()

	// No stream_event seen — assistant should still be parsed normally.
	got := p.Parse([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`))
	if len(got) != 1 || got[0].Text != "hello" || got[0].BlockID != 0 {
		t.Errorf("legacy fallback: got %+v", got)
	}
}

func TestParser_BlockIDsAreMonotonic(t *testing.T) {
	p := NewParser()

	got := p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`))
	if got[0].BlockID != 1 {
		t.Errorf("first block ID = %d, want 1", got[0].BlockID)
	}
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`))

	// New block — ID must be 2 even though stream-event index is again 0
	// (e.g., next message starts a fresh block at idx 0).
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"message_start"}}`))
	got = p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`))
	if got[0].BlockID != 2 {
		t.Errorf("second block ID = %d, want 2", got[0].BlockID)
	}
}

func TestParser_MalformedLineIgnored(t *testing.T) {
	p := NewParser()
	if got := p.Parse([]byte(`not json`)); got != nil {
		t.Errorf("malformed line should yield nil, got %+v", got)
	}
	if got := p.Parse([]byte(``)); got != nil {
		t.Errorf("empty line should yield nil, got %+v", got)
	}
}

func TestParser_EditDiffOnStop(t *testing.T) {
	p := NewParser()
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","name":"Edit"}}}`))
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/x.go\","}}}`))
	p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"old_string\":\"foo\",\"new_string\":\"bar\"}"}}}`))

	got := p.Parse([]byte(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`))
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0].DiffOld != "foo" || got[0].DiffNew != "bar" {
		t.Errorf("diff fields missing on Edit stop: %+v", got[0])
	}
}
