// Package agent defines the interface for AI coding agent backends.
package agent

import "context"

// TurnResult holds the output from a single agent turn.
type TurnResult struct {
	Output   string   // Final result text from the agent
	Stderr   string   // Raw stderr captured from the agent process
	Activity []string // Tool calls made during the turn (human-readable, for logging)
}

// Agent is the interface that all AI coding agent backends must implement.
type Agent interface {
	// Name returns a human-readable identifier for this agent instance.
	Name() string

	// RunTurn executes a single autonomous turn.
	// The agent works in workDir, driven by the provided prompt.
	RunTurn(ctx context.Context, workDir string, prompt string) (TurnResult, error)
}

// StreamEvent is a backend-agnostic representation of something worth showing
// to the user during a streaming agent turn.
//
// Kinds:
//   - "text"       — text content. With BlockID > 0, multiple events with
//     the same BlockID carry the GROWING content for that
//     block (TUI replaces the previous title in place).
//   - "tool_start" — a tool_use block opened. Tool is set, Detail is empty,
//     BlockID identifies the block (always > 0).
//   - "tool"       — finalized tool call. Tool, Detail (and DiffOld/DiffNew
//     for Edit) are populated. With BlockID > 0, the event
//     finalizes a tool_start with the same BlockID.
//   - "block_end"  — a content block closed. BlockID identifies which.
//     Lets consumers drop in-flight indicators.
//   - "result"     — the agent's final result text.
type StreamEvent struct {
	Kind    string
	Tool    string // tool name (Kind == "tool" or "tool_start")
	Detail  string // human-readable detail (file path, command, pattern, …)
	Text    string // agent reasoning / narrative (Kind == "text")
	Result  string // final output text (Kind == "result")
	DiffOld string // old_string from Edit input (for collapsible diff view)
	DiffNew string // new_string from Edit input

	// BlockID identifies the source content block when emitted from a
	// partial-message stream. Same BlockID across multiple events means the
	// consumer should UPDATE the existing entry in place (text growing as
	// deltas arrive) instead of appending a new one. 0 means "untracked"
	// (legacy whole-block path — always append).
	BlockID int
}

// StreamingAgent extends Agent with live event streaming support.
// Backends that support streaming implement this so the orchestrator can
// wire up the bubbletea TUI for live display.
type StreamingAgent interface {
	Agent
	SetOnEvent(func(StreamEvent))
	SetOnStreamDone(func())
}

// ModelValidator is implemented by backends that can check whether a model
// name is valid before running a full turn.
type ModelValidator interface {
	ValidateModel(ctx context.Context, model string) error
}
