// Package claude implements the agent.Agent interface using the Claude Code CLI.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/manurgdev/departai/internal/agent"
)

// CLI binary name and install hint — exported for proactive availability checks
// and actionable "not installed" error messages.
const (
	BinaryName  = "claude"
	InstallHint = "Install the Claude Code CLI: npm install -g @anthropic-ai/claude-code"
)

// EnsureAvailable returns an actionable error if the claude CLI is not on PATH.
func EnsureAvailable() error {
	return agent.CheckCLI(BinaryName, InstallHint)
}

// Agent runs turns by spawning the `claude` CLI process in non-interactive mode.
type Agent struct {
	name         string
	model        string
	onEvent      func(agent.StreamEvent)
	onStreamDone func()
}

// New creates a Claude Code CLI agent with the given display name.
func New(name string) *Agent {
	return &Agent{name: name}
}

// NewWithModel creates a Claude Code CLI agent that uses a specific model.
func NewWithModel(name, model string) *Agent {
	return &Agent{name: name, model: model}
}

func (a *Agent) Name() string { return a.name }

// SetOnEvent implements agent.StreamingAgent.
func (a *Agent) SetOnEvent(fn func(agent.StreamEvent)) { a.onEvent = fn }

// SetOnStreamDone implements agent.StreamingAgent.
func (a *Agent) SetOnStreamDone(fn func()) { a.onStreamDone = fn }

// RunTurn spawns `claude` in non-interactive mode with stream-json output.
func (a *Agent) RunTurn(ctx context.Context, workDir string, prompt string) (agent.TurnResult, error) {
	args := []string{
		"--dangerously-skip-permissions",
		"--verbose",
		"--output-format", "stream-json",
		// Stream token-level deltas (text and tool input). Without this, the
		// CLI emits only whole `assistant` messages — long single blocks
		// (e.g. a Write of a 80 KB spec) would surface to departai as one
		// silent gap of many minutes. With deltas the TUI can show text
		// growing live and confirm the agent is alive.
		"--include-partial-messages",
		"-p", prompt,
	}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}

	cmd := exec.CommandContext(ctx, BinaryName, args...)
	cmd.Dir = workDir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return agent.TurnResult{}, fmt.Errorf("agent %q: creating stdout pipe: %w", a.name, err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return agent.TurnResult{}, fmt.Errorf("agent %q: %s", a.name, InstallHint)
		}
		return agent.TurnResult{}, fmt.Errorf("agent %q: starting process: %w", a.name, err)
	}

	var (
		finalResult string
		activity    []string
	)

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), agent.StreamBufferBytes())

	parser := NewParser()
	for scanner.Scan() {
		line := scanner.Bytes()

		events := parser.Parse(line)
		for _, evt := range events {
			switch evt.Kind {
			case "tool":
				// Finalized tool call — record for activity log. The earlier
				// "tool_start" placeholder is intentionally ignored here;
				// it has no Detail yet.
				entry := evt.Tool
				if evt.Detail != "" {
					entry += " " + evt.Detail
				}
				activity = append(activity, entry)
			case "result":
				finalResult = evt.Result
			}

			if a.onEvent != nil {
				a.onEvent(evt)
			}
		}
	}

	// A scanner error (notably bufio.ErrTooLong on an oversized line) would
	// otherwise be swallowed, truncating the turn silently. Kill the process so
	// cmd.Wait doesn't block on a now-unread pipe, then surface it clearly.
	scanErr := scanner.Err()
	if scanErr != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	if a.onStreamDone != nil {
		a.onStreamDone()
	}

	waitErr := cmd.Wait()

	if scanErr != nil {
		return agent.TurnResult{
			Output:   finalResult,
			Stderr:   stderr.String(),
			Activity: activity,
		}, agent.StreamReadError(a.name, scanErr)
	}

	if waitErr != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = "(no stderr output)"
		}
		return agent.TurnResult{
			Output:   finalResult,
			Stderr:   errMsg,
			Activity: activity,
		}, fmt.Errorf("agent %q exited with error: %w\nstderr: %s", a.name, waitErr, errMsg)
	}

	return agent.TurnResult{
		Output:   finalResult,
		Stderr:   stderr.String(),
		Activity: activity,
	}, nil
}

// ValidateModel checks that the given model name is accepted by the claude CLI.
func ValidateModel(ctx context.Context, model string) error {
	if model == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", "ok",
		"--model", model,
		"--dangerously-skip-permissions",
		"--output-format", "text",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}

	out := stdout.String()
	if strings.Contains(out, "issue with the selected model") ||
		strings.Contains(out, "It may not exist") {
		return fmt.Errorf("%s", strings.TrimSpace(out))
	}

	return nil
}
