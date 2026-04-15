// Package claude implements the agent.Agent interface using the Claude Code CLI.
package claude

import (
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

// RunTurn spawns `claude --dangerously-skip-permissions -p <prompt>` in workDir
// and waits for it to finish. The agent is expected to write its turn summary
// to the shared task log file as part of its work.
func (a *Agent) RunTurn(ctx context.Context, workDir string, prompt string) (agent.TurnResult, error) {
	args := []string{
		"--dangerously-skip-permissions",
		"--output-format", "text",
		"-p", prompt,
	}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = "(no stderr output)"
		}
		return agent.TurnResult{
			Output: stdout.String(),
			Stderr: errMsg,
		}, fmt.Errorf("agent %q exited with error: %w\nstderr: %s", a.name, err, errMsg)
	}

	return agent.TurnResult{
		Output: stdout.String(),
		Stderr: stderr.String(),
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
