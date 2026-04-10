// Package cli handles command-line argument parsing and wires up the orchestrator.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/manurgdev/departai/internal/orchestrator"
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

Flags:
`

// Run parses args and starts the orchestrator. It is the main entry point.
func Run(args []string) error {
	fs := flag.NewFlagSet("departai", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr)
	}

	dir := fs.String("dir", "", "Working directory for agents (default: current directory)")
	instructions := fs.String("instructions", "", "Path to a custom base instructions markdown file")
	maxTurns := fs.Int("max-turns", 20, "Maximum number of agent turns before stopping")

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

	workDir := *dir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
	}

	orch, err := orchestrator.New(orchestrator.Config{
		WorkDir:          workDir,
		Prompt:           prompt,
		InstructionsFile: *instructions,
		MaxTurns:         *maxTurns,
	})
	if err != nil {
		return fmt.Errorf("initialising orchestrator: %w", err)
	}

	return orch.Run()
}
