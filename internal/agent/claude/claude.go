// Package claude implements the agent.Agent interface using the Claude Code CLI.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/manurgdev/departai/internal/agent"
)

// Agent runs turns by spawning the `claude` CLI process in non-interactive mode.
type Agent struct {
	name  string
	model string // optional: passed as --model if non-empty

	// OnEvent is called for every displayable stream event (tool calls,
	// agent reasoning text). Set by the orchestrator for live feedback.
	// Nil means silent (no live display).
	OnEvent func(evt StreamEvent)
}

// New creates a Claude Code CLI agent with the given display name.
func New(name string) *Agent {
	return &Agent{name: name}
}

// NewWithModel creates a Claude Code CLI agent that uses a specific model.
func NewWithModel(name, model string) *Agent {
	return &Agent{name: name, model: model}
}

func (a *Agent) Name() string {
	return a.name
}

// RunTurn spawns `claude` in non-interactive mode with stream-json output.
// It reads stdout line-by-line, parses stream events for live display, and
// captures the final result text. The agent is expected to write its turn
// summary to the shared task log file as part of its work.
func (a *Agent) RunTurn(ctx context.Context, workDir string, prompt string) (agent.TurnResult, error) {
	args := []string{
		"--dangerously-skip-permissions",
		"--verbose",
		"--output-format", "stream-json",
		"-p", prompt,
	}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = workDir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return agent.TurnResult{}, fmt.Errorf("agent %q: creating stdout pipe: %w", a.name, err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return agent.TurnResult{}, fmt.Errorf("agent %q: starting process: %w", a.name, err)
	}

	// Read stream-json lines, parse events, display live, capture result.
	var (
		finalResult string
		activity    []string
	)

	scanner := bufio.NewScanner(stdoutPipe)
	// Allow large lines (stream-json system/init events can be very long).
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		events := ParseStreamLine(line)
		for _, evt := range events {
			switch evt.Kind {
			case "tool":
				entry := evt.Tool
				if evt.Detail != "" {
					entry += " " + evt.Detail
				}
				activity = append(activity, entry)

			case "result":
				finalResult = evt.Result
			}

			if a.OnEvent != nil {
				a.OnEvent(evt)
			}
		}
	}

	waitErr := cmd.Wait()

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
// It runs a minimal prompt call that fails fast locally for unknown models.
// Returns nil for valid models (and for empty model, which means "use backend default").
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

	// claude sometimes reports the model error on stdout with exit code 0.
	out := stdout.String()
	if strings.Contains(out, "issue with the selected model") ||
		strings.Contains(out, "It may not exist") {
		return fmt.Errorf("%s", strings.TrimSpace(out))
	}

	return nil
}
