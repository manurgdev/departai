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
func WelcomeBanner(workDir, backend, model, modelAlpha, modelBeta, instructionsFile string, maxTurns int) {
	fmt.Println()
	boldCyan.Println("  DepartAI — AI Agent Orchestrator")
	fmt.Println()
	faint.Printf("  Work dir     : %s\n", workDir)
	faint.Printf("  Backend      : %s\n", backend)
	faint.Printf("  Max turns    : %s\n", maxTurnsDisplay(maxTurns))
	if instructionsFile != "" {
		faint.Printf("  Instructions : %s\n", instructionsFile)
	}
	fmt.Println()
	faint.Println("  Models:")
	faint.Printf("    Alpha Global : %s\n", modelDisplay(model))
	faint.Printf("    Alpha Local  : %s\n", localOverride(modelAlpha))
	faint.Printf("    Beta Global  : %s\n", modelDisplay(model))
	faint.Printf("    Beta Local   : %s\n", localOverride(modelBeta))
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
	fmt.Println("    /resume                      Select a previous task (does not run it)")
	fmt.Println("    /new                         Deselect current task (next prompt = new task)")
	fmt.Println("    /exit, /quit                 Exit departai")
	fmt.Println()
	bold.Println("  Config keys:")
	fmt.Println("    model, model.alpha, model.beta, backend, max-turns, instructions")
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
func maxTurnsDisplay(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", n)
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
func ShowConfig(workDir, backend, model, modelAlpha, modelBeta string, maxTurns int) {
	fmt.Println()
	bold.Println("  Current configuration:")
	fmt.Printf("    Work dir     : %s\n", workDir)
	fmt.Printf("    Backend      : %s\n", backend)
	fmt.Printf("    Model        : %s\n", modelDisplay(model))
	if modelAlpha != "" {
		fmt.Printf("    Model Alpha  : %s\n", modelAlpha)
	}
	if modelBeta != "" {
		fmt.Printf("    Model Beta   : %s\n", modelBeta)
	}
	fmt.Printf("    Max turns    : %s\n", maxTurnsDisplay(maxTurns))
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
