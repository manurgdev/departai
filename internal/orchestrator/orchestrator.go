// Package orchestrator manages the turn-based agent relay loop.
package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manurgdev/departai/internal/agent"
	claudeagent "github.com/manurgdev/departai/internal/agent/claude"
	"github.com/manurgdev/departai/internal/tasklog"
	"github.com/manurgdev/departai/internal/ui"
)

// defaultBaseInstructions is embedded when no --instructions file is provided.
const defaultBaseInstructions = `# DepartAI Agent Protocol

You are part of a two-agent relay team working collaboratively on a shared coding task.
You and your partner agent take turns; context is handed off via a shared task log file.

## Turn Protocol

Each turn you must:
1. Read the task log file to understand what has already been done.
2. Continue the work — make real, concrete progress (write/edit code, run commands, etc.).
3. At the end of your turn, **append** your turn summary to the task log file using the
   exact format shown below.

## Turn Summary Format

Append this block to the task log file (fill in the angle-bracket placeholders):

    ## Turn <N> - <Your Agent Name>

    **Working Directory**: <absolute path to the project directory you actually worked in>

    **What I did**: <concise summary of the actions you took this turn>

    **Current State**: <description of the project state right now>

    **Next Steps**: <what still needs to be done, or "None — task is complete">

    **Complete**: <yes or no>

    ---

Rules for **Working Directory**:
- Always write the absolute path of the directory where you actually read and edited files.
- If you were told to work in /path/A but the real project is at /path/B, write /path/B.
- This field lets the orchestrator keep logs co-located with the project being edited.

Rules for **Complete**:
- Write ` + "`yes`" + ` only when the entire original task is fully implemented,
  the code compiles/runs without errors, and all requirements are met.
- Write ` + "`no`" + ` in all other cases.
- The orchestrator stops only when **two consecutive turns** both say ` + "`yes`" + `.

## Working Guidelines

- Work autonomously — no human will intervene between turns.
- Prefer editing real files over creating throwaway scripts.
- Read existing project files before modifying them.
- Follow any project conventions found (CLAUDE.md, .cursorrules, etc.).
- Make meaningful progress each turn; do not just plan.
`

// Config holds all configuration for an Orchestrator run.
type Config struct {
	WorkDir          string // directory where agents do their work
	Prompt           string // original task from the user
	InstructionsFile string // optional path to a custom base instructions file
	MaxTurns         int    // safety cap; 0 defaults to 10
	AgentBackend     string // which CLI backend to use (currently only "claude")
	Model            string // default model for all agents (optional)
	ModelAlpha       string // override for Agent Alpha (optional)
	ModelBeta        string // override for Agent Beta (optional)
}

// Orchestrator manages the sequential agent relay until consensus or max turns.
type Orchestrator struct {
	cfg       Config
	agents    []agent.Agent
	baseInstr string
	taskLog   *tasklog.TaskLog
}

// New creates and initialises a new Orchestrator, including the task directory.
func New(cfg Config) (*Orchestrator, error) {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 10
	}
	if cfg.AgentBackend == "" {
		cfg.AgentBackend = "claude"
	}

	baseInstr, err := loadInstructions(cfg.InstructionsFile)
	if err != nil {
		return nil, fmt.Errorf("loading instructions: %w", err)
	}

	tl, err := tasklog.New(cfg.WorkDir, cfg.Prompt)
	if err != nil {
		return nil, fmt.Errorf("creating task log: %w", err)
	}

	agents, err := buildAgents(cfg)
	if err != nil {
		return nil, err
	}

	return &Orchestrator{
		cfg:       cfg,
		agents:    agents,
		baseInstr: baseInstr,
		taskLog:   tl,
	}, nil
}

// buildAgents constructs the two agent instances based on the configured backend.
// Each agent uses its per-agent model override if set, else the global Model.
func buildAgents(cfg Config) ([]agent.Agent, error) {
	switch cfg.AgentBackend {
	case "claude", "":
		return []agent.Agent{
			claudeagent.NewWithModel("Agent Alpha", modelOrDefault(cfg.ModelAlpha, cfg.Model)),
			claudeagent.NewWithModel("Agent Beta", modelOrDefault(cfg.ModelBeta, cfg.Model)),
		}, nil
	default:
		return nil, fmt.Errorf("unknown agent backend %q (supported: claude)", cfg.AgentBackend)
	}
}

// modelOrDefault returns override if non-empty, otherwise fallback.
func modelOrDefault(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// Run executes the relay loop. It returns nil on successful completion (consensus
// or max-turns reached) and an error only on infrastructure failures.
func (o *Orchestrator) Run() error {
	ui.Header(o.taskLog.TaskID, o.taskLog.Dir, o.cfg.WorkDir)

	ctx := context.Background()

	for turn := 1; turn <= o.cfg.MaxTurns; turn++ {
		ag := o.agents[(turn-1)%len(o.agents)]

		ui.TurnHeader(turn, o.cfg.MaxTurns, ag.Name())

		prompt, err := o.buildPrompt(turn, ag.Name())
		if err != nil {
			return fmt.Errorf("building prompt for turn %d: %w", turn, err)
		}

		start := time.Now()
		var result agent.TurnResult

		runErr := ui.RunWithSpinner(ag.Name()+" working…", func() error {
			var e error
			result, e = ag.RunTurn(ctx, o.cfg.WorkDir, prompt)
			return e
		})

		elapsed := time.Since(start)

		// Always persist raw logs — even on error, for diagnostics.
		if logErr := o.taskLog.WriteRawLog(turn, ag.Name(), prompt, result.Output, result.Stderr); logErr != nil {
			ui.Warning(fmt.Sprintf("could not write raw log: %v", logErr))
		}

		if runErr != nil {
			return fmt.Errorf("turn %d (%s) failed: %w", turn, ag.Name(), runErr)
		}

		ui.TurnDone(turn, ag.Name(), elapsed, result.Output)

		// Relocate task dir if the agent worked in a different directory.
		if err := o.maybeRelocateTaskDir(); err != nil {
			ui.Warning(fmt.Sprintf("could not relocate task dir: %v", err))
		}

		// Need at least two turns before checking consensus.
		if turn >= 2 {
			complete, err := o.taskLog.BothAgentsAgreeComplete()
			if err != nil {
				return fmt.Errorf("checking completion after turn %d: %w", turn, err)
			}
			if complete {
				ui.Success(o.taskLog.Path(), o.cfg.WorkDir)
				return nil
			}
		}
	}

	ui.MaxTurnsReached(o.cfg.MaxTurns, o.taskLog.Path(), o.cfg.WorkDir)
	return nil
}

// maybeRelocateTaskDir reads the Working Directory field from the last turn entry.
// If it differs from the current workDir, the task directory is moved there and
// o.cfg.WorkDir is updated so subsequent turns use the correct location.
func (o *Orchestrator) maybeRelocateTaskDir() error {
	reported, err := o.taskLog.ParseLastWorkingDir()
	if err != nil || reported == "" {
		return err
	}

	if reported == filepath.Clean(o.cfg.WorkDir) {
		return nil
	}

	if _, err := os.Stat(reported); err != nil {
		return fmt.Errorf("agent reported working dir %q but it does not exist: %w", reported, err)
	}

	oldDir := o.taskLog.Dir
	if err := o.taskLog.Relocate(reported); err != nil {
		return err
	}

	ui.Relocated(oldDir, o.taskLog.Dir)
	o.cfg.WorkDir = reported
	return nil
}

// buildPrompt constructs the full prompt string for a given agent turn.
func (o *Orchestrator) buildPrompt(turnNumber int, agentName string) (string, error) {
	taskLogContent, err := o.taskLog.Read()
	if err != nil {
		return "", err
	}

	projectRules := loadProjectRules(o.cfg.WorkDir)

	var b strings.Builder

	fmt.Fprintf(&b, "# DepartAI — Turn %d\n\n", turnNumber)
	fmt.Fprintf(&b, "You are **%s**, an AI coding agent in a two-agent relay team.\n\n", agentName)

	b.WriteString("## Agent Protocol\n\n")
	b.WriteString(o.baseInstr)
	b.WriteString("\n\n")

	if projectRules != "" {
		b.WriteString("## Project Rules\n\n")
		b.WriteString(projectRules)
		b.WriteString("\n\n")
	}

	b.WriteString("## Task Log\n\n")
	fmt.Fprintf(&b, "File path: `%s`\n\n", o.taskLog.Path())
	b.WriteString("Current contents:\n\n")
	b.WriteString("```\n")
	b.WriteString(taskLogContent)
	b.WriteString("\n```\n\n")

	b.WriteString("## Your Turn\n\n")
	fmt.Fprintf(&b, "This is **Turn %d**. You are **%s**.\n\n", turnNumber, agentName)
	b.WriteString("Steps:\n")
	b.WriteString("1. Read the task log above to understand what has been done so far.\n")
	b.WriteString("2. Continue the work — implement, test, fix, iterate.\n")
	fmt.Fprintf(&b, "3. When finished, append your turn summary to the task log at:\n   `%s`\n\n", o.taskLog.Path())
	fmt.Fprintf(&b,
		"Your summary MUST begin with `## Turn %d - %s` and include `**Complete**: yes` or `**Complete**: no`.\n\n",
		turnNumber, agentName,
	)
	b.WriteString("Begin now.\n")

	return b.String(), nil
}

// loadInstructions returns custom instructions from path, or the built-in default.
func loadInstructions(path string) (string, error) {
	if path == "" {
		return defaultBaseInstructions, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading instructions file %q: %w", path, err)
	}
	return string(data), nil
}

// loadProjectRules reads common project convention files and concatenates them.
func loadProjectRules(workDir string) string {
	candidates := []string{
		"CLAUDE.md",
		"AGENTS.md",
		".cursorrules",
		".github/copilot-instructions.md",
	}

	var parts []string
	for _, name := range candidates {
		data, err := os.ReadFile(filepath.Join(workDir, name))
		if err == nil && len(data) > 0 {
			parts = append(parts, fmt.Sprintf("### %s\n\n%s", name, string(data)))
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}
