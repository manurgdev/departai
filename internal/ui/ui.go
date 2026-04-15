// Package ui provides styled terminal output for departai.
// It uses fatih/color for ANSI colours and briandowns/spinner for
// progress indicators. Both automatically degrade gracefully when
// stdout is not a TTY (e.g. piped output).
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
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
func TurnHeader(turn, maxTurns int, agentName string) {
	faint.Println("  " + rule)
	bold.Printf("  Turn %d/%d", turn, maxTurns)
	fmt.Print("  •  ")
	boldCyan.Println(agentName)
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
func WelcomeBanner(workDir, backend, model string, maxTurns int) {
	fmt.Println()
	boldCyan.Println("  DepartAI — AI Agent Orchestrator")
	fmt.Println()
	faint.Printf("  Work dir  : %s\n", workDir)
	faint.Printf("  Backend   : %s\n", backend)
	if model != "" {
		faint.Printf("  Model     : %s\n", model)
	} else {
		faint.Printf("  Model     : %s\n", "(default)")
	}
	faint.Printf("  Max turns : %d\n", maxTurns)
	fmt.Println()
	faint.Println("  Type a task to start, or /help for commands.")
	fmt.Println()
}

// InteractiveHelp prints the list of interactive commands.
func InteractiveHelp() {
	fmt.Println()
	bold.Println("  Commands:")
	fmt.Println("    /help                        Show this help message")
	fmt.Println("    /config                      Show current configuration")
	fmt.Println("    /config set <key> <value>    Set a config value for this session")
	fmt.Println("    /config save                 Save config to project .departai/config.yml")
	fmt.Println("    /config save global          Save config to ~/.departai/config.yml")
	fmt.Println("    /model                       Show all agent models")
	fmt.Println("    /model <name>                Set global model for this session")
	fmt.Println("    /model alpha [<name>]        Show/set Agent Alpha's model")
	fmt.Println("    /model beta [<name>]         Show/set Agent Beta's model")
	fmt.Println("    /exit, /quit                 Exit departai")
	fmt.Println()
	bold.Println("  Config keys:")
	fmt.Println("    model, model.alpha, model.beta, backend, max-turns, instructions")
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

// modelDisplay returns the model name or "(default)" for empty values.
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
	fmt.Printf("    Max turns    : %d\n", maxTurns)
	fmt.Println()
}

// TaskSeparator prints a visual break between tasks in interactive mode.
func TaskSeparator() {
	fmt.Println()
	faint.Println("  " + rule)
	fmt.Println()
}
