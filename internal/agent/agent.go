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
