// Package ui provides styled terminal output for departai.
// It uses fatih/color for ANSI colours and briandowns/spinner for
// progress indicators. Both automatically degrade gracefully when
// stdout is not a TTY (e.g. piped output).
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/manifoldco/promptui"

	"github.com/manurgdev/departai/internal/config"
)

// ── colour palette ──────────────────────────────────────────────────────────

var (
	boldCyan   = color.New(color.FgCyan, color.Bold)
	boldGreen  = color.New(color.FgGreen, color.Bold)
	boldYellow = color.New(color.FgYellow, color.Bold)
	boldRed    = color.New(color.FgRed, color.Bold)
	faint      = color.New(color.Faint)
	bold       = color.New(color.Bold)
)

const rule = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

// ── public functions ─────────────────────────────────────────────────────────

// Header prints the startup banner with task metadata.
func Header(taskID, taskDir, workDir string) {
	fmt.Println()
	boldCyan.Println("  DepartAI — AI Agent Orchestrator")
	fmt.Println()
	faint.Printf("  Task ID  : %s\n", taskID)
	faint.Printf("  Task dir : %s\n", taskDir)
	faint.Printf("  Work dir : %s\n", workDir)
	fmt.Println()
}

// TurnHeader prints a styled divider before each agent turn.
// model is the effective model for this agent (may be empty for backend default).
func TurnHeader(turn, maxTurns int, agentName, model string) {
	faint.Println("  " + rule)
	bold.Printf("  Turn %d/%d", turn, maxTurns)
	fmt.Print("  •  ")
	boldCyan.Print(agentName)
	fmt.Print("  •  ")
	faint.Println(modelDisplay(model))
	faint.Println("  " + rule)
	fmt.Println()
}

// RunWithSpinner shows a spinner with label while fn executes.
// The result of fn is passed through unchanged.
func RunWithSpinner(label string, fn func() error) error {
	s := spinner.New(spinner.CharSets[14], 80*time.Millisecond)
	s.Suffix = "  " + label
	s.Start()
	err := fn()
	s.Stop()
	return err
}

// TurnDone prints the post-turn summary: elapsed time and agent stdout.
func TurnDone(turn int, agentName string, elapsed time.Duration, output string) {
	boldGreen.Printf("  ✓ %s done", agentName)
	faint.Printf("  (%s)\n", elapsed.Round(time.Second))

	if output = strings.TrimSpace(output); output != "" {
		fmt.Println()
		faint.Println("  Agent output:")
		for _, line := range strings.Split(output, "\n") {
			faint.Print("  │ ")
			fmt.Println(line)
		}
	}
	fmt.Println()
}

// Relocated prints a notice when the task directory is moved to a new project.
func Relocated(oldDir, newDir string) {
	boldYellow.Println("  ⟳ Task directory relocated")
	faint.Printf("    %s\n", oldDir)
	faint.Printf("    → %s\n", newDir)
	fmt.Println()
}

// Success prints the completion banner.
func Success(taskLogPath, workDir string) {
	fmt.Println()
	boldGreen.Println("  " + rule)
	boldGreen.Println("  ✓  Both agents agree — task is complete!")
	boldGreen.Println("  " + rule)
	fmt.Println()
	faint.Printf("  Task log : %s\n", taskLogPath)
	faint.Printf("  Review   : %s\n", workDir)
	fmt.Println()
}

// MaxTurnsReached prints the max-turns warning banner.
func MaxTurnsReached(maxTurns int, taskLogPath, workDir string) {
	fmt.Println()
	boldYellow.Printf("  ⚠  Maximum turns (%d) reached without consensus\n", maxTurns)
	fmt.Println()
	faint.Printf("  Task log : %s\n", taskLogPath)
	faint.Printf("  Review   : %s\n", workDir)
	fmt.Println()
}

// AgentText prints the agent's reasoning/narrative text during a streaming turn.
// This is the human-readable explanation of what the agent is doing and why.
func AgentText(text string) {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			faint.Printf("  %s\n", trimmed)
		}
	}
}

// AgentToolUse prints a single tool-call event during a streaming agent turn.
func AgentToolUse(toolName, detail string) {
	boldCyan.Print("  → ")
	bold.Print(toolName)
	if detail != "" {
		faint.Printf(" %s", detail)
	}
	fmt.Println()
}

// TaskStopped prints a message when the user presses ESC to stop a task.
func TaskStopped() {
	fmt.Println()
	boldYellow.Println("  Task paused. Use /continue to resume or /resume to pick another task.")
	fmt.Println()
}

// TurnTimeout prints a warning when a turn was forcibly killed for exceeding
// the per-turn duration budget. The relay continues with the next agent.
func TurnTimeout(agent string, duration time.Duration) {
	fmt.Println()
	boldYellow.Printf("  ⏱  %s exceeded the %s budget — killed by orchestrator\n", agent, duration)
	faint.Println("     Partial activity preserved in this turn's raw log; relay continues with the next agent.")
	fmt.Println()
}

// OscillationDetected prints the loop-detection banner when the orchestrator
// concludes that the agents are stuck churning the same files without progress.
// The user can inject a directive, /continue (to give the relay another K-turn
// window), or abandon.
func OscillationDetected(files []string, turns int) {
	fmt.Println()
	boldYellow.Printf("  🌀 Oscillation detected — relay stopped\n")
	faint.Printf("     Last %d turns kept touching:\n", turns)
	for _, f := range files {
		faint.Printf("     - %s\n", f)
	}
	faint.Println("     Without new Acceptance Criteria being checked.")
	fmt.Println()
	faint.Println("  Type a directive to break the loop, or /continue to retry one more cycle.")
	fmt.Println()
}

// AgentBlocked prints the human-escalation banner when an agent set the
// **Blocked on** field in its turn summary. The reason is shown verbatim;
// the user can answer with a directive or /continue to defer back.
func AgentBlocked(agent, reason string) {
	fmt.Println()
	boldYellow.Printf("  🚧 %s is blocked\n", agent)
	for _, line := range strings.Split(strings.TrimSpace(reason), "\n") {
		faint.Printf("     %s\n", line)
	}
	fmt.Println()
	faint.Println("  Type a directive to unblock, or /continue to tell agents to decide themselves.")
	fmt.Println()
}

// NoActiveTask prints an error when /continue is used with no active task.
func NoActiveTask() {
	boldRed.Println("  No active task. Use /resume to pick a previous task, or type a new prompt.")
}

// TaskSelected confirms a task was selected via /resume (without running it).
func TaskSelected(taskDir string) {
	taskID := filepath.Base(taskDir)
	fmt.Println()
	boldGreen.Printf("  Task selected: %s\n", taskID)
	faint.Println("  Type a prompt to add instructions, or /continue to resume the relay.")
	fmt.Println()
}

// TaskCleared confirms the current task was deselected via /new.
func TaskCleared() {
	fmt.Println()
	faint.Println("  Task cleared. Next prompt will create a new task.")
	fmt.Println()
}

// ModeChanged confirms a switch between /dev and /ask, and reminds the user
// how to persist the change (the /dev and /ask shortcuts are session-only).
func ModeChanged(mode string) {
	boldGreen.Printf("  ✓ Mode set to %s ", mode)
	faint.Printf("(session only — use /config set mode %s to persist)\n", mode)
}

// RespecQueued confirms /respec was set: the next prompt or /continue will
// trigger a fresh spec pre-turn loop before the regular relay.
func RespecQueued() {
	boldGreen.Println("  ✓ Spec re-evaluation queued")
	faint.Println("    Next prompt (or /continue) will run the spec pre-turns first.")
}

// RespecAlreadyQueued is shown when /respec is invoked twice in a row.
func RespecAlreadyQueued() {
	faint.Println("  Spec re-evaluation already queued — type a prompt or /continue to trigger it.")
}

// RespecNoActiveTask hints that /respec has no effect when no task is active,
// because new tasks already run the spec pre-turn loop by default.
func RespecNoActiveTask() {
	boldYellow.Println("  /respec has no effect with no active task.")
	faint.Println("    Your next prompt will create a fresh task, which already runs the spec pre-turns.")
}

// Warning prints a non-fatal warning line.
func Warning(msg string) {
	boldYellow.Printf("  ⚠  %s\n", msg)
}

// Error prints a fatal error line.
func Error(msg string) {
	boldRed.Printf("  ✗  %s\n", msg)
}

// ── interactive mode ────────────────────────────────────────────────────────

// WelcomeBanner prints the startup banner for interactive mode with config summary.
// Shows effective per-agent models (override if set, else global) and the
// instructions file only if a custom one was provided.
func WelcomeBanner(workDir string, cfg config.Config) {
	fmt.Println()
	boldCyan.Println("  DepartAI — AI Agent Orchestrator")
	fmt.Println()
	faint.Printf("  Work dir     : %s\n", workDir)
	faint.Printf("  Mode         : %s\n", modeDisplay(cfg.Mode))
	faint.Printf("  Max turns    : %s\n", maxTurnsDisplay(cfg.MaxTurns))
	faint.Printf("  Max turn time: %s\n", maxTurnDurationDisplay(cfg.MaxTurnDurationStr))
	faint.Printf("  Log window   : %s\n", logWindowDisplay(cfg.LogWindow))
	if cfg.InstructionsFile != "" {
		faint.Printf("  Instructions : %s\n", cfg.InstructionsFile)
	}
	if n := len(cfg.BlockedCommands); n > 0 {
		faint.Printf("  Blocked      : %d command(s)\n", n)
	}
	fmt.Println()
	faint.Println("  Agents:")
	faint.Printf("    Alpha : %s / %s\n", cfg.BackendFor("alpha"), modelDisplay(cfg.ModelFor("alpha")))
	faint.Printf("    Beta  : %s / %s\n", cfg.BackendFor("beta"), modelDisplay(cfg.ModelFor("beta")))
	fmt.Println()
	faint.Println("  Type a task to start, or /help for commands.")
	fmt.Println()
}

// localOverride renders an agent-specific override. Empty overrides fall back
// to the shared global row, so we show "(not set)" explicitly.
func localOverride(override string) string {
	if override == "" {
		return "(not set)"
	}
	return override
}

// InteractiveHelp prints the list of interactive commands.
func InteractiveHelp() {
	fmt.Println()
	bold.Println("  Commands:")
	fmt.Println("    /help                        Show this help message")
	fmt.Println("    /dev                         Switch to development mode (code-focused)")
	fmt.Println("    /ask                         Switch to ask mode (research / Q&A)")
	fmt.Println("    /config                      Show current configuration")
	fmt.Println("    /config set <key> <value>    Set a config value (prompts to save)")
	fmt.Println("    /config save                 Save config directly to project .departai/config.yml")
	fmt.Println("    /config save global          Save config directly to ~/.departai/config.yml")
	fmt.Println("    /model                       Show all agent models")
	fmt.Println("    /model <name>                Set global model (prompts to save)")
	fmt.Println("    /model unset                 Clear global model (falls back to backend default)")
	fmt.Println("    /model alpha [<name>]        Show/set Agent Alpha's model (prompts to save)")
	fmt.Println("    /model alpha unset           Clear Agent Alpha's override (inherits global)")
	fmt.Println("    /model beta [<name>]         Show/set Agent Beta's model (prompts to save)")
	fmt.Println("    /model beta unset            Clear Agent Beta's override (inherits global)")
	fmt.Println("    /continue                    Continue the active task's relay loop")
	fmt.Println("    /respec                      Force a spec pre-turn before the next prompt or /continue")
	fmt.Println("    /resume                      Select a previous task (does not run it)")
	fmt.Println("    /new                         Deselect current task (next prompt = new task)")
	fmt.Println("    /exit, /quit                 Exit departai")
	fmt.Println()
	bold.Println("  Backends:")
	fmt.Println("    claude (default), codex")
	fmt.Println()
	bold.Println("  Config keys:")
	fmt.Println("    model, model.alpha, model.beta")
	fmt.Println("    backend, backend.alpha, backend.beta")
	fmt.Println("    mode, max-turns, instructions, blocked-commands")
	fmt.Println()
	bold.Println("  Security:")
	fmt.Println("    /config set blocked-commands \"WebFetch,rm -rf\"")
	fmt.Println("    Comma-separated tools/patterns agents must NOT use (soft enforcement).")
	fmt.Println()
	bold.Println("  Task control:")
	fmt.Println("    Press ESC during a running task to pause it.")
	fmt.Println()
	bold.Println("  Save scope:")
	fmt.Println("    After any change, pick Project (default), Global, or Session only.")
	fmt.Println()
	bold.Println("  Usage:")
	fmt.Println("    Type any other text to start a task with that prompt.")
	fmt.Println("    Ctrl+C or Ctrl+D also exits.")
	fmt.Println()
}

// ShowModel prints a single model setting (used by /model alpha and /model beta).
func ShowModel(label, model string) {
	fmt.Println()
	if model != "" {
		bold.Printf("  %s: %s\n", label, model)
	} else {
		bold.Printf("  %s: ", label)
		faint.Println("(default)")
	}
	fmt.Println()
}

// ShowModels prints the global default alongside per-agent overrides.
// alpha and beta are the override values (may be empty); global is the fallback.
func ShowModels(global, alpha, beta string) {
	fmt.Println()
	bold.Println("  Models:")
	fmt.Printf("    Global       : %s\n", modelDisplay(global))
	fmt.Printf("    Agent Alpha  : %s\n", modelDisplay(resolveModel(alpha, global)))
	fmt.Printf("    Agent Beta   : %s\n", modelDisplay(resolveModel(beta, global)))
	fmt.Println()
}

// ModelChanged prints a confirmation when the global model is switched.
func ModelChanged(model string) {
	boldGreen.Printf("  ✓ Model set to %s\n", model)
}

// ModelChangedFor prints a confirmation when a per-agent model is switched.
func ModelChangedFor(agentName, model string) {
	boldGreen.Printf("  ✓ %s model set to %s\n", agentName, model)
}

// ModelUnset prints a confirmation when a model value is cleared.
// hint describes the fallback (e.g. "backend default" or "global").
func ModelUnset(target, hint string) {
	boldGreen.Printf("  ✓ %s cleared (now uses %s)\n", target, hint)
}

// modelDisplay returns the model name or "(default)" for empty values.
func modeDisplay(mode string) string {
	if mode == "" {
		return "dev"
	}
	return mode
}

func maxTurnsDisplay(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", n)
}

func maxTurnDurationDisplay(s string) string {
	if s == "" {
		return "no limit"
	}
	return s
}

func logWindowDisplay(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("last %d turns", n)
}

func modelDisplay(model string) string {
	if model == "" {
		return "(default)"
	}
	return model
}

// resolveModel returns override if non-empty, else fallback.
func resolveModel(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// ConfigSet confirms a config key was changed.
func ConfigSet(key, value string) {
	boldGreen.Printf("  ✓ %s set to %s\n", key, value)
}

// ConfigSaved confirms a config file was written.
func ConfigSaved(path string) {
	boldGreen.Printf("  ✓ Config saved to %s\n", path)
}

// ConfigSetError prints an error related to /config commands.
func ConfigSetError(msg string) {
	boldRed.Printf("  ✗ %s\n", msg)
}

// ValidationFailed prints a styled error when a model fails validation.
// The caller is expected to keep the previous value in config (no revert here).
func ValidationFailed(target, model, errMsg string) {
	fmt.Println()
	boldRed.Printf("  ✗ Model %q rejected for %s\n", model, target)
	for _, line := range strings.Split(strings.TrimSpace(errMsg), "\n") {
		faint.Printf("    %s\n", line)
	}
	faint.Printf("  %s is unchanged.\n", target)
	fmt.Println()
}

// ShowConfig prints the current configuration, including per-agent model
// overrides when they are set (non-empty).
func ShowConfig(workDir string, cfg config.Config) {
	fmt.Println()
	bold.Println("  Current configuration:")
	fmt.Printf("    Work dir     : %s\n", workDir)
	fmt.Printf("    Mode         : %s\n", modeDisplay(cfg.Mode))
	fmt.Printf("    Max turns    : %s\n", maxTurnsDisplay(cfg.MaxTurns))
	fmt.Printf("    Max turn time: %s\n", maxTurnDurationDisplay(cfg.MaxTurnDurationStr))
	fmt.Printf("    Log window   : %s\n", logWindowDisplay(cfg.LogWindow))
	if cfg.InstructionsFile != "" {
		fmt.Printf("    Instructions : %s\n", cfg.InstructionsFile)
	}
	fmt.Println()
	fmt.Println("    Agents:")
	fmt.Printf("      Alpha : %s / %s\n", cfg.BackendFor("alpha"), modelDisplay(cfg.ModelFor("alpha")))
	fmt.Printf("      Beta  : %s / %s\n", cfg.BackendFor("beta"), modelDisplay(cfg.ModelFor("beta")))
	fmt.Println()
	fmt.Println("    Blocked commands:")
	if len(cfg.BlockedCommands) == 0 {
		faint.Println("      (none)")
	} else {
		for _, c := range cfg.BlockedCommands {
			fmt.Printf("      - %s\n", c)
		}
	}
	fmt.Println()
}

// TaskSeparator prints a visual break between tasks in interactive mode.
func TaskSeparator() {
	fmt.Println()
	faint.Println("  " + rule)
	fmt.Println()
}

// ── save-scope picker ──────────────────────────────────────────────────────

// SaveScope identifies where to persist a config change.
type SaveScope int

const (
	// SaveScopeSession does not write to disk; the change lives only in memory.
	SaveScopeSession SaveScope = iota
	// SaveScopeProject writes to <workdir>/.departai/config.yml.
	SaveScopeProject
	// SaveScopeGlobal writes to ~/.departai/config.yml.
	SaveScopeGlobal
)

// PromptSaveScope shows an arrow-key selector asking where to save a config
// change. "Project" is the default. Returns SaveScopeSession on Ctrl+C,
// selection error, or explicit "Session only" choice.
func PromptSaveScope(projectPath, globalPath string) SaveScope {
	items := []string{
		fmt.Sprintf("Project  (%s)", projectPath),
		fmt.Sprintf("Global   (%s)", globalPath),
		"Session only (don't save)",
	}

	// Compact, single-line prompt and selection templates that match our style.
	templates := &promptui.SelectTemplates{
		Label:    "  {{ . }}",
		Active:   "  ▸ {{ . | cyan }}",
		Inactive: "    {{ . | faint }}",
		Selected: "  ✓ {{ . | green }}",
	}

	prompt := promptui.Select{
		Label:     "Save this change?",
		Items:     items,
		Size:      3,
		HideHelp:  true,
		Templates: templates,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return SaveScopeSession
	}
	switch idx {
	case 0:
		return SaveScopeProject
	case 1:
		return SaveScopeGlobal
	default:
		return SaveScopeSession
	}
}
