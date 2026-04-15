package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/manurgdev/departai/internal/config"
	"github.com/manurgdev/departai/internal/orchestrator"
	"github.com/manurgdev/departai/internal/ui"
)

// runInteractive starts the REPL that lets users type tasks interactively.
// It loops until the user types "exit", "quit", or sends EOF (Ctrl+D).
func runInteractive(workDir string, cfg config.Config) error {
	ui.WelcomeBanner(workDir, cfg.AgentBackend, cfg.Model, cfg.MaxTurns)

	scanner := bufio.NewScanner(os.Stdin)
	// Allow long prompts (default scanner buffer is 64KB, bump to 1MB).
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for {
		ui.Prompt()

		if !scanner.Scan() {
			// EOF (Ctrl+D) or read error.
			fmt.Println() // newline after the prompt line
			return scanner.Err()
		}

		line := strings.TrimSpace(scanner.Text())

		switch {
		case line == "":
			continue

		case line == "exit" || line == "quit":
			return nil

		case line == "help":
			ui.InteractiveHelp()

		case line == "config":
			ui.ShowConfig(workDir, cfg.AgentBackend, cfg.Model, cfg.MaxTurns)

		case line == "model":
			ui.ShowModel(cfg.Model)

		case strings.HasPrefix(line, "model "):
			newModel := strings.TrimSpace(strings.TrimPrefix(line, "model "))
			if newModel != "" {
				cfg.Model = newModel
				ui.ModelChanged(newModel)
			}

		default:
			if err := runTask(workDir, cfg, line); err != nil {
				ui.Error(fmt.Sprintf("task failed: %v", err))
			}
			ui.TaskSeparator()
		}
	}
}

// runTask creates an orchestrator for a single task prompt and runs it.
func runTask(workDir string, cfg config.Config, prompt string) error {
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
