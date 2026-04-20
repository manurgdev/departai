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
type StreamEvent struct {
	Kind    string // "tool", "text", "result"
	Tool    string // tool name (only when Kind == "tool")
	Detail  string // human-readable detail (file path, command, pattern, …)
	Text    string // agent reasoning / narrative (only when Kind == "text")
	Result  string // final output text (only when Kind == "result")
	DiffOld string // old_string from Edit input (for collapsible diff view)
	DiffNew string // new_string from Edit input
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
