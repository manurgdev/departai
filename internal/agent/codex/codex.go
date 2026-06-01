// Package codex implements the agent.Agent interface using the OpenAI Codex CLI.
package codex

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
	BinaryName  = "codex"
	InstallHint = "Install the Codex CLI: npm install -g @openai/codex"
)

// EnsureAvailable returns an actionable error if the codex CLI is not on PATH.
func EnsureAvailable() error {
	return agent.CheckCLI(BinaryName, InstallHint)
}

// Agent runs turns by spawning the `codex` CLI process in non-interactive mode.
type Agent struct {
	name         string
	model        string
	onEvent      func(agent.StreamEvent)
	onStreamDone func()
}

// New creates a Codex CLI agent with the given display name.
func New(name string) *Agent {
	return &Agent{name: name}
}

// NewWithModel creates a Codex CLI agent that uses a specific model.
func NewWithModel(name, model string) *Agent {
	return &Agent{name: name, model: model}
}

func (a *Agent) Name() string { return a.name }

// SetOnEvent implements agent.StreamingAgent.
func (a *Agent) SetOnEvent(fn func(agent.StreamEvent)) { a.onEvent = fn }

// SetOnStreamDone implements agent.StreamingAgent.
func (a *Agent) SetOnStreamDone(fn func()) { a.onStreamDone = fn }

// RunTurn spawns `codex exec` in non-interactive mode with JSONL output.
func (a *Agent) RunTurn(ctx context.Context, workDir string, prompt string) (agent.TurnResult, error) {
	args := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--json",
		"-C", workDir,
	}
	if a.model != "" {
		args = append(args, "-m", a.model)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, BinaryName, args...)

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
		lastMessage string // track the last agent_message as result
		activity    []string
		seenTools   = make(map[string]bool) // deduplicate started+completed
	)

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		events := ParseStreamLine(line)
		for _, evt := range events {
			switch evt.Kind {
			case "tool":
				// Deduplicate: item.started and item.completed emit the same tool
				key := evt.Tool + "|" + evt.Detail
				if !seenTools[key] {
					seenTools[key] = true
					entry := evt.Tool
					if evt.Detail != "" {
						entry += " " + evt.Detail
					}
					activity = append(activity, entry)
				}
			case "text":
				lastMessage = evt.Text
			}

			if a.onEvent != nil {
				a.onEvent(evt)
			}
		}
	}

	if a.onStreamDone != nil {
		a.onStreamDone()
	}

	waitErr := cmd.Wait()

	if waitErr != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = "(no stderr output)"
		}
		return agent.TurnResult{
			Output:   lastMessage,
			Stderr:   errMsg,
			Activity: activity,
		}, fmt.Errorf("agent %q exited with error: %w\nstderr: %s", a.name, waitErr, errMsg)
	}

	return agent.TurnResult{
		Output:   lastMessage,
		Stderr:   stderr.String(),
		Activity: activity,
	}, nil
}

// ValidateModel checks that the given model name is accepted by the codex CLI.
func ValidateModel(ctx context.Context, model string) error {
	if model == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "codex", "exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--json",
		"-m", model,
		"say ok",
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

	return nil
}
