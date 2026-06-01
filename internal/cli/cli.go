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
	"github.com/manurgdev/departai/internal/version"
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
	maxRetriesFlag := fs.Int("max-retries", -1, "Retries on a transient backend failure per turn (default: 2; 0 disables)")
	modelFlag := fs.String("model", "", "Model to use (e.g. claude-opus-4-5); overrides config")
	backendFlag := fs.String("backend", "", "Agent backend to use (default: claude)")
	versionFlag := fs.Bool("version", false, "Print version, then exit (add --verbose for build details)")
	verboseFlag := fs.Bool("verbose", false, "More detailed output (e.g. full build info with --version)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *versionFlag {
		if *verboseFlag {
			fmt.Println(version.Detailed())
		} else {
			fmt.Println(version.Summary())
		}
		return nil
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
	if *maxRetriesFlag >= 0 { // -1 = not provided
		n := *maxRetriesFlag
		cfg.MaxRetries = &n
	}
	if *instructionsFlag != "" {
		cfg.InstructionsFile = *instructionsFlag
	}
	if *backendFlag != "" {
		cfg.AgentBackend = *backendFlag
	}

	// Verify the configured backends' CLIs are installed before doing anything
	// that needs them (model validation below, running a task). In direct mode
	// a missing CLI is fatal; in interactive mode it's a warning, since the user
	// can switch backends from the REPL.
	directMode := fs.NArg() > 0
	if err := ensureBackendsAvailable(cfg, directMode); err != nil {
		return err
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

// ensureBackendsAvailable checks that the CLI for every backend in use (the
// per-agent backends resolved from config) is installed. When fatal is true, a
// missing CLI returns an error; otherwise it surfaces a warning and continues.
func ensureBackendsAvailable(cfg config.Config, fatal bool) error {
	checked := map[string]bool{}
	for _, agentName := range []string{"alpha", "beta"} {
		backend := cfg.BackendFor(agentName)
		if checked[backend] {
			continue
		}
		checked[backend] = true

		var err error
		switch backend {
		case "codex":
			err = codexagent.EnsureAvailable()
		default: // "claude" or empty
			err = claudeagent.EnsureAvailable()
		}
		if err != nil {
			if fatal {
				return err
			}
			ui.Warning(err.Error())
		}
	}
	return nil
}
