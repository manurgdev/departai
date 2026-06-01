// Package agent defines the interface for AI coding agent backends.
package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// CheckCLI returns an actionable error when the named CLI binary is not found
// on the user's PATH. installHint should tell the user how to install it.
// Backends use this for a proactive availability check before running a turn,
// so users see a clear "install with …" message instead of a raw exec error.
func CheckCLI(binary, installHint string) error {
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("the %q CLI is not installed or not on your PATH.\n%s", binary, installHint)
	}
	return nil
}

// ── stream buffer sizing ─────────────────────────────────────────────────────

// defaultMaxStreamLineMB is the per-line cap for backend JSONL output. A single
// stream line can be large (e.g. a tool_use input embedding a whole file), so
// the default is generous. Override with DEPARTAI_MAX_STREAM_LINE_MB.
const defaultMaxStreamLineMB = 16

// streamBufferEnvVar lets advanced users raise the per-line cap without a
// rebuild, for pathological outputs.
const streamBufferEnvVar = "DEPARTAI_MAX_STREAM_LINE_MB"

// StreamBufferMB returns the effective per-line cap in megabytes: the default,
// or a positive integer from DEPARTAI_MAX_STREAM_LINE_MB.
func StreamBufferMB() int {
	if v := os.Getenv(streamBufferEnvVar); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxStreamLineMB
}

// StreamBufferBytes returns the per-line cap in bytes, for bufio.Scanner.Buffer.
func StreamBufferBytes() int {
	return StreamBufferMB() * 1024 * 1024
}

// StreamReadError wraps a bufio.Scanner error with an actionable message. The
// "line too long" case gets a specific hint pointing at the env-var override;
// other read errors are wrapped verbatim. Returns nil for a nil error.
func StreamReadError(agentName string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, bufio.ErrTooLong) {
		return fmt.Errorf(
			"agent %q: a single output line exceeded the %d MB stream buffer — raise %s or simplify the task",
			agentName, StreamBufferMB(), streamBufferEnvVar,
		)
	}
	return fmt.Errorf("agent %q: reading stream output: %w", agentName, err)
}

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
