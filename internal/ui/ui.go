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
