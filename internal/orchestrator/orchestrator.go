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
	MaxTurns         int    // safety cap; 0 defaults to 20
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
		cfg.MaxTurns = 20
	}

	baseInstr, err := loadInstructions(cfg.InstructionsFile)
	if err != nil {
		return nil, fmt.Errorf("loading instructions: %w", err)
	}

	tl, err := tasklog.New(cfg.WorkDir, cfg.Prompt)
	if err != nil {
		return nil, fmt.Errorf("creating task log: %w", err)
	}

	// Two Claude Code CLI agents alternating turns.
	agents := []agent.Agent{
		claudeagent.New("Agent Alpha"),
		claudeagent.New("Agent Beta"),
	}

	return &Orchestrator{
		cfg:       cfg,
		agents:    agents,
		baseInstr: baseInstr,
		taskLog:   tl,
	}, nil
}

// Run executes the relay loop. It returns nil on successful completion (consensus
// or max-turns reached) and an error only on infrastructure failures.
func (o *Orchestrator) Run() error {
	fmt.Printf("DepartAI — task started\n")
	fmt.Printf("  Task ID  : %s\n", o.taskLog.TaskID)
	fmt.Printf("  Task dir : %s\n", o.taskLog.Dir)
	fmt.Printf("  Work dir : %s\n", o.cfg.WorkDir)
	fmt.Println()

	ctx := context.Background()

	for turn := 1; turn <= o.cfg.MaxTurns; turn++ {
		ag := o.agents[(turn-1)%len(o.agents)]

		fmt.Printf("[Turn %d/%d] %s working...\n", turn, o.cfg.MaxTurns, ag.Name())
		start := time.Now()

		prompt, err := o.buildPrompt(turn, ag.Name())
		if err != nil {
			return fmt.Errorf("building prompt for turn %d: %w", turn, err)
		}

		result, err := ag.RunTurn(ctx, o.cfg.WorkDir, prompt)

		// Always save raw logs, even if the turn errored, so we have full diagnostics.
		if logErr := o.taskLog.WriteRawLog(turn, ag.Name(), prompt, result.Output, result.Stderr); logErr != nil {
			fmt.Printf("[Turn %d/%d] warning: could not write raw log: %v\n", turn, o.cfg.MaxTurns, logErr)
		}

		if err != nil {
			return fmt.Errorf("turn %d (%s) failed: %w", turn, ag.Name(), err)
		}

		elapsed := time.Since(start).Round(time.Second)
		fmt.Printf("[Turn %d/%d] %s done (%s)\n", turn, o.cfg.MaxTurns, ag.Name(), elapsed)

		// If the agent worked in a different directory than our configured workDir,
		// relocate the task directory to sit inside the actual project.
		if err := o.maybeRelocateTaskDir(); err != nil {
			// Non-fatal: warn and continue with the current location.
			fmt.Printf("[Turn %d/%d] warning: could not relocate task dir: %v\n", turn, o.cfg.MaxTurns, err)
		}

		// Need at least two turns before checking consensus.
		if turn >= 2 {
			complete, err := o.taskLog.BothAgentsAgreeComplete()
			if err != nil {
				return fmt.Errorf("checking completion after turn %d: %w", turn, err)
			}
			if complete {
				fmt.Println()
				fmt.Println("Both agents agree: task is complete!")
				fmt.Printf("Task log : %s\n", o.taskLog.Path())
				fmt.Printf("Review   : %s\n", o.cfg.WorkDir)
				return nil
			}
		}
	}

	fmt.Println()
	fmt.Printf("Maximum turns (%d) reached without consensus.\n", o.cfg.MaxTurns)
	fmt.Printf("Task log : %s\n", o.taskLog.Path())
	fmt.Printf("Review   : %s\n", o.cfg.WorkDir)
	return nil
}

// maybeRelocateTaskDir reads the Working Directory field from the last turn entry.
// If it differs from the current workDir, the task directory is moved there and
// o.cfg.WorkDir is updated so subsequent turns use the correct location.
func (o *Orchestrator) maybeRelocateTaskDir() error {
	reported, err := o.taskLog.ParseLastWorkingDir()
	if err != nil || reported == "" {
		return err // nothing reported yet, or read error
	}

	if reported == filepath.Clean(o.cfg.WorkDir) {
		return nil // already consistent
	}

	// Verify the reported path actually exists before trusting it.
	if _, err := os.Stat(reported); err != nil {
		return fmt.Errorf("agent reported working dir %q but it does not exist: %w", reported, err)
	}

	oldDir := o.taskLog.Dir
	if err := o.taskLog.Relocate(reported); err != nil {
		return err
	}

	fmt.Printf("  Task dir relocated: %s\n    -> %s\n", oldDir, o.taskLog.Dir)
	o.cfg.WorkDir = reported
	return nil
}

// buildPrompt constructs the full prompt string for a given agent turn.
// It combines base instructions, project rules, the task log, and turn-specific directives.
func (o *Orchestrator) buildPrompt(turnNumber int, agentName string) (string, error) {
	taskLogContent, err := o.taskLog.Read()
	if err != nil {
		return "", err
	}

	projectRules := loadProjectRules(o.cfg.WorkDir)

	var b strings.Builder

	// Identity
	fmt.Fprintf(&b, "# DepartAI — Turn %d\n\n", turnNumber)
	fmt.Fprintf(&b, "You are **%s**, an AI coding agent in a two-agent relay team.\n\n", agentName)

	// Base instructions
	b.WriteString("## Agent Protocol\n\n")
	b.WriteString(o.baseInstr)
	b.WriteString("\n\n")

	// Project rules (if any)
	if projectRules != "" {
		b.WriteString("## Project Rules\n\n")
		b.WriteString(projectRules)
		b.WriteString("\n\n")
	}

	// Current task log
	b.WriteString("## Task Log\n\n")
	fmt.Fprintf(&b, "File path: `%s`\n\n", o.taskLog.Path())
	b.WriteString("Current contents:\n\n")
	b.WriteString("```\n")
	b.WriteString(taskLogContent)
	b.WriteString("\n```\n\n")

	// Turn-specific instruction
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
