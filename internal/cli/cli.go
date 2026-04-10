// Package cli handles command-line argument parsing and wires up the orchestrator.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/manurgdev/departai/internal/config"
	"github.com/manurgdev/departai/internal/orchestrator"
	"github.com/manurgdev/departai/internal/ui"
)

const usage = `departai — AI agent orchestrator

Runs two Claude Code CLI agents in sequential turns on a shared task.
Agents hand off context via a task log until both agree the work is done.

Usage:
  departai [flags] <prompt>

Examples:
  departai "Build a REST API with user authentication"
  departai --dir /path/to/project "Add unit tests for the auth module"
  departai --instructions ./my-instructions.md "Refactor the database layer"
  departai --model claude-opus-4-5 "Migrate the database schema"

Configuration:
  departai reads .departai.yml from the project directory, then
  ~/.config/departai/config.yml, then uses built-in defaults.
  CLI flags always take precedence over config file values.

Flags:
`

// Run parses args, loads config, and starts the orchestrator.
func Run(args []string) error {
	fs := flag.NewFlagSet("departai", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr)
	}

	// CLI flags — zero values mean "not set by user; use config file value".
	dir := fs.String("dir", "", "Working directory for agents (default: current directory)")
	instructionsFlag := fs.String("instructions", "", "Path to a custom base instructions markdown file")
	maxTurnsFlag := fs.Int("max-turns", 0, "Maximum number of agent turns (default: 10, or from config)")
	modelFlag := fs.String("model", "", "Model to use (e.g. claude-opus-4-5); overrides config")
	backendFlag := fs.String("backend", "", "Agent backend to use (default: claude)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("a prompt is required")
	}

	prompt := strings.Join(fs.Args(), " ")

	// Resolve working directory first — config search depends on it.
	workDir := *dir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
	}

	// Load config (defaults → global → project), then overlay CLI flags.
	cfg, err := config.Load(workDir)
	if err != nil {
		ui.Warning(fmt.Sprintf("config load error: %v — using defaults", err))
		cfg = config.Defaults()
	}

	// CLI flags override config file values (only when explicitly provided).
	if *maxTurnsFlag != 0 {
		cfg.MaxTurns = *maxTurnsFlag
	}
	if *modelFlag != "" {
		cfg.Model = *modelFlag
	}
	if *instructionsFlag != "" {
		cfg.InstructionsFile = *instructionsFlag
	}
	if *backendFlag != "" {
		cfg.AgentBackend = *backendFlag
	}

	orch, err := orchestrator.New(orchestrator.Config{
		WorkDir:          workDir,
		Prompt:           prompt,
		InstructionsFile: cfg.InstructionsFile,
		MaxTurns:         cfg.MaxTurns,
		AgentBackend:     cfg.AgentBackend,
		Model:            cfg.Model,
	})
	if err != nil {
		return fmt.Errorf("initialising orchestrator: %w", err)
	}

	return orch.Run()
}
