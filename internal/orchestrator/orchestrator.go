// Package orchestrator manages the turn-based agent relay loop.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manurgdev/departai/internal/agent"
	claudeagent "github.com/manurgdev/departai/internal/agent/claude"
	"github.com/manurgdev/departai/internal/tasklog"
	"github.com/manurgdev/departai/internal/tui"
	"github.com/manurgdev/departai/internal/ui"
)

// ErrUserStopped is returned when the user presses ESC to stop a running task.
// The REPL uses this to track the task for /continue.
var ErrUserStopped = errors.New("task stopped by user")

// defaultBaseInstructions is embedded when no --instructions file is provided.
const defaultBaseInstructions = `# DepartAI Agent Protocol

You are part of a two-agent relay team. You and a partner agent take turns on a shared
coding task, handing off context through a task log file. You are NOT on the same side —
your job is to produce the best possible result for the human, which means being
**critical, rigorous, and pragmatic** about the work, including your partner's.

## Your Role

On every turn you must:

1. **Review first.** If this is not the first turn, critically review what the previous
   agent did. Do NOT blindly trust it. Read the actual code changes, run the code or
   tests, and verify the work is correct. Look for:
   - Bugs, logic errors, edge cases not handled
   - Missing error handling or input validation
   - Code that compiles but does not actually work as intended
   - Incomplete implementations (stubs, TODOs, hardcoded values)
   - Regressions — did the previous change break something that worked before?
   - Poor code quality: duplication, unnecessary complexity, bad naming

2. **Fix problems you find.** If the previous agent's work has issues, fix them. Do not
   just note them — fix them. Be direct: "Agent Alpha left X broken, I fixed it by Y."

3. **Continue the work.** After reviewing, make real progress on whatever remains.
   Implement, test, iterate. Every turn should leave the codebase measurably closer to
   done.

4. **Write or run tests.** Whenever you implement functionality, verify it works:
   - Run existing tests (` + "`go test`" + `, ` + "`npm test`" + `, ` + "`pytest`" + `, etc.) to catch regressions.
   - Write new tests for the code you added if the project has a test framework.
   - If the project has no tests, at minimum verify your changes manually (run the app,
     try the endpoints, check the output).
   - Report test results explicitly in your turn summary.

5. **Log your turn.** Append a structured summary to the task log file (format below).

## Critical Mindset

- **Do not rubber-stamp.** If your partner says "Complete: yes" but the code has issues,
  say "Complete: no" and explain what is wrong. Agreeing prematurely wastes the human's
  time.
- **Do not repeat work.** Read the log carefully. If something is already done and works,
  move on. Focus on what is missing or broken.
- **Be specific in criticism.** "The code looks fine" is useless. Instead: "The /login
  endpoint returns 200 on invalid credentials because the password check on line 42 is
  inverted."
- **Be pragmatic.** Perfect is the enemy of done. Fix real problems, not style nits. If
  the task asks for a REST API, ship a working REST API — don't get lost debating naming
  conventions.
- **Challenge scope creep.** If the previous agent started implementing things not asked
  for in the original task, note it and stay focused on what the human requested.

## Incremental Work

You do NOT need to finish the entire task in a single turn. In fact, trying to do
everything at once often leads to mistakes. Instead:

- **Focus on one aspect per turn.** For example: "this turn I'll handle the navigation
  component" — then leave clear Next Steps for the other agent to handle the footer, CTA,
  etc.
- **For large tasks, work in phases:** plan → core implementation → edge cases → tests →
  cleanup. Each turn covers one phase.
- **Leave clear handoff notes.** Your "Next Steps" field is what the other agent reads
  first. Be specific: "Footer still has a registration link on line 22 that needs updating."
- **Small tasks can be quick.** If the task is genuinely simple (one file, one change),
  doing it in one turn is fine — but the other agent still needs to verify.

## Turn Summary Format

At the end of your turn, **append** this block to the task log file:

    ## Turn <N> - <Your Agent Name>

    **Working Directory**: <absolute path to the directory where you actually worked>

    **Review of previous turn**: <what you checked, what was correct, what was wrong>

    **What I did**: <concise list of actions taken this turn>

    **Tests**: <which tests you ran, pass/fail results, new tests written>

    **Current State**: <honest assessment of where the project stands>

    **Remaining Issues**: <known problems, edge cases, missing pieces — or "None">

    **Next Steps**: <what the next agent should focus on — or "None — task is complete">

    **Complete**: <yes or no>

    ---

Rules for **Working Directory**:
- Always write the absolute path of the directory where you actually read and edited files.
- If you were told to work in /path/A but the real project is at /path/B, write /path/B.

Rules for **Complete**:
- Write ` + "`yes`" + ` ONLY when ALL of the following are true:
  1. You made **ZERO code changes** during this turn (no edits, no new files, no deletions).
  2. You reviewed the previous agent's work and found no outstanding issues.
  3. Every requirement from the original task is implemented.
  4. The code compiles/runs without errors.
  5. Tests pass (or manual verification confirms it works).
- **If you edited ANY file during your turn, you MUST write ` + "`no`" + `**, even if you
  believe the task is now finished. The other agent needs to verify your changes.
- Write ` + "`no`" + ` in all other cases. Being honest here saves everyone time.
- The orchestrator stops only when **two consecutive turns** both say ` + "`yes`" + `.

Why this rule matters: if you fix something your partner missed and then say "Complete: yes",
nobody verifies YOUR fix. By saying "no", you force a verification cycle. The task only
ends when both agents agree that nothing more needs to change — not when one agent
heroically does everything and declares victory.

## Working Guidelines

- Work autonomously — no human will intervene between turns.
- Read existing project files before modifying them.
- Follow project conventions (CLAUDE.md, .cursorrules, etc.).
- Make real progress each turn — implement, test, fix. Do not just plan.
- Prefer small, correct changes over large, sweeping refactors.
- When in doubt about the original intent, stay close to what the human asked for.

## Example turn flow for a medium task

Turn 1 (Alpha): Implements navigation and CTA changes → Complete: no
Turn 2 (Beta):  Reviews Alpha's work, implements footer + OG image changes → Complete: no
Turn 3 (Alpha): Reviews Beta's work, runs full test suite, finds nothing wrong → Complete: yes
Turn 4 (Beta):  Reviews everything, confirms all requirements met → Complete: yes
→ Consensus reached, task ends.
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

// New creates and initialises a new Orchestrator with a fresh task directory.
func New(cfg Config) (*Orchestrator, error) {
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

// NewFromExisting creates an Orchestrator that resumes an existing task
// from the given task directory. The orchestrator reads the existing task log
// and continues from where it left off.
func NewFromExisting(cfg Config, taskDir string) (*Orchestrator, error) {
	if cfg.AgentBackend == "" {
		cfg.AgentBackend = "claude"
	}

	baseInstr, err := loadInstructions(cfg.InstructionsFile)
	if err != nil {
		return nil, fmt.Errorf("loading instructions: %w", err)
	}

	tl, err := tasklog.Load(taskDir)
	if err != nil {
		return nil, fmt.Errorf("loading existing task: %w", err)
	}

	// Use the prompt from the existing task log.
	cfg.Prompt = tl.Prompt

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

// TaskDir returns the absolute path to the task directory.
func (o *Orchestrator) TaskDir() string {
	return o.taskLog.Dir
}

// buildAgents constructs the two agent instances based on the configured backend.
// Each agent uses its per-agent model override if set, else the global Model.
// OnEvent is NOT set here — the orchestrator's Run loop sets it per-turn to
// feed events into the bubbletea TUI via a channel.
func buildAgents(cfg Config) ([]agent.Agent, error) {
	switch cfg.AgentBackend {
	case "claude", "":
		alpha := claudeagent.NewWithModel("Agent Alpha", modelOrDefault(cfg.ModelAlpha, cfg.Model))
		beta := claudeagent.NewWithModel("Agent Beta", modelOrDefault(cfg.ModelBeta, cfg.Model))
		return []agent.Agent{alpha, beta}, nil
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

// agentModel returns the effective model for the named agent.
func (o *Orchestrator) agentModel(name string) string {
	switch name {
	case "Agent Alpha":
		return modelOrDefault(o.cfg.ModelAlpha, o.cfg.Model)
	case "Agent Beta":
		return modelOrDefault(o.cfg.ModelBeta, o.cfg.Model)
	}
	return o.cfg.Model
}

// Run executes the relay loop. It returns nil on successful completion (consensus
// or max-turns reached), ErrUserStopped if the user pressed ESC, or an error on
// infrastructure failures.
func (o *Orchestrator) Run() error {
	ui.Header(o.taskLog.TaskID, o.taskLog.Dir, o.cfg.WorkDir)

	taskStart := time.Now()

	// Task log turn numbers continue incrementing across runs (Turn 5, 6, 7...),
	// but MaxTurns counts turns-in-this-run (resets on each /continue or new directive).
	existingTurns, _ := o.taskLog.ParseTurns()
	nextLogTurn := len(existingTurns) + 1

	for runTurn := 1; o.cfg.MaxTurns == 0 || runTurn <= o.cfg.MaxTurns; runTurn++ {
		turn := nextLogTurn + runTurn - 1
		ag := o.agents[(turn-1)%len(o.agents)]

		prompt, err := o.buildPrompt(turn, ag.Name())
		if err != nil {
			return fmt.Errorf("building prompt for turn %d: %w", turn, err)
		}

		result, stopped, runErr := o.runTurnWithTUI(ag, prompt, turn, taskStart)

		// Always persist raw logs — even on error/stop, for diagnostics.
		if logErr := o.taskLog.WriteRawLog(turn, ag.Name(), result.Activity, result.Output, result.Stderr); logErr != nil {
			ui.Warning(fmt.Sprintf("could not write raw log: %v", logErr))
		}

		if stopped {
			return ErrUserStopped
		}

		if runErr != nil {
			return fmt.Errorf("turn %d (%s) failed: %w", turn, ag.Name(), runErr)
		}

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

	// Only reachable when MaxTurns > 0 and the loop exhausted.
	if o.cfg.MaxTurns > 0 {
		ui.MaxTurnsReached(o.cfg.MaxTurns, o.taskLog.Path(), o.cfg.WorkDir)
	}
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

// runTurnWithTUI runs an agent turn with a bubbletea TUI for live output.
// Returns the result, whether the user stopped (ESC), and any error.
func (o *Orchestrator) runTurnWithTUI(ag agent.Agent, prompt string, turn int, taskStart time.Time) (agent.TurnResult, bool, error) {
	// Cancellable context — ESC in the TUI calls cancel() to kill the agent.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Event channel — agent pushes events, TUI consumes them.
	eventCh := make(chan claudeagent.StreamEvent, 100)

	if ca, ok := ag.(*claudeagent.Agent); ok {
		ca.OnEvent = func(evt claudeagent.StreamEvent) {
			eventCh <- evt
		}
		ca.OnStreamDone = func() {
			close(eventCh) // TUI gets channelClosedMsg immediately
		}
	}

	// Run agent in background.
	type turnOutcome struct {
		result agent.TurnResult
		err    error
	}
	outcomeCh := make(chan turnOutcome, 1)

	go func() {
		r, e := ag.RunTurn(ctx, o.cfg.WorkDir, prompt)
		// eventCh already closed by OnStreamDone (before cmd.Wait)
		outcomeCh <- turnOutcome{r, e}
	}()

	// Launch bubbletea TUI — blocks until agent finishes (+ auto-continue or review)
	// or user presses ESC (which calls cancel, killing the agent).
	_, stopped := tui.RunAgentView(eventCh, cancel, ag.Name(), o.agentModel(ag.Name()), turn, o.cfg.MaxTurns, taskStart)

	// Collect agent result.
	outcome := <-outcomeCh

	if stopped {
		return outcome.result, true, nil
	}
	return outcome.result, false, outcome.err
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
