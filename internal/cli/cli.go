// Package cli handles command-line argument parsing and wires up the orchestrator.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	claudeagent "github.com/manurgdev/departai/internal/agent/claude"
	codexagent "github.com/manurgdev/departai/internal/agent/codex"
	"github.com/manurgdev/departai/internal/config"
	"github.com/manurgdev/departai/internal/ui"
)

const usage = `departai — AI agent orchestrator

Runs two Claude Code CLI agents in sequential turns on a shared task.
Agents hand off context via a task log until both agree the work is done.

Usage:
  departai [flags]            Start interactive mode
  departai [flags] <prompt>   Run a single task and exit

Examples:
  departai                                             # interactive REPL
  departai "Build a REST API with user authentication"
  departai --dir /path/to/project "Add unit tests"
  departai --model claude-opus-4-5 "Migrate the schema"

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
	maxTurnsFlag := fs.Int("max-turns", 0, "Maximum number of agent turns (default: unlimited)")
	maxTurnDurationFlag := fs.String("max-turn-duration", "", "Per-turn wall-clock budget (e.g. 15m, 1h30m); empty = no limit")
	logWindowFlag := fs.Int("log-window", 0, "Inject only the last N turns into each prompt (default: 0 = full log)")
	modelFlag := fs.String("model", "", "Model to use (e.g. claude-opus-4-5); overrides config")
	backendFlag := fs.String("backend", "", "Agent backend to use (default: claude)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

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
	if *maxTurnDurationFlag != "" {
		if _, err := time.ParseDuration(*maxTurnDurationFlag); err != nil {
			return fmt.Errorf("--max-turn-duration %q invalid: %w (use Go duration format, e.g. 15m, 1h30m)", *maxTurnDurationFlag, err)
		}
		cfg.MaxTurnDurationStr = *maxTurnDurationFlag
	}
	if *logWindowFlag != 0 {
		if *logWindowFlag < 0 {
			return fmt.Errorf("--log-window must be 0 (no windowing) or a positive integer, got %d", *logWindowFlag)
		}
		cfg.LogWindow = *logWindowFlag
	}
	if *instructionsFlag != "" {
		cfg.InstructionsFile = *instructionsFlag
	}
	if *backendFlag != "" {
		cfg.AgentBackend = *backendFlag
	}
	if *modelFlag != "" {
		// Validate the CLI-provided model against the active backend.
		var valErr error
		switch cfg.AgentBackend {
		case "codex":
			valErr = codexagent.ValidateModel(context.Background(), *modelFlag)
		default:
			valErr = claudeagent.ValidateModel(context.Background(), *modelFlag)
		}
		if valErr != nil {
			return fmt.Errorf("--model %q rejected by %s: %s", *modelFlag, cfg.AgentBackend, valErr)
		}
		cfg.Model = *modelFlag
	}

	// No prompt argument → interactive REPL mode.
	if fs.NArg() == 0 {
		return runInteractive(workDir, cfg)
	}

	// Prompt provided → single-task direct mode.
	prompt := strings.Join(fs.Args(), " ")
	_, err = runTask(workDir, cfg, prompt, false)
	return err
}
