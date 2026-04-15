package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	goprompt "github.com/c-bata/go-prompt"
	"golang.org/x/term"

	"github.com/manurgdev/departai/internal/config"
	"github.com/manurgdev/departai/internal/orchestrator"
	"github.com/manurgdev/departai/internal/ui"
)

// runInteractive starts the REPL that lets users type tasks interactively.
// Slash commands get autocomplete via go-prompt; everything else is a task prompt.
func runInteractive(workDir string, cfg config.Config) error {
	ui.WelcomeBanner(workDir, cfg.AgentBackend, cfg.Model, cfg.MaxTurns)

	// Save terminal state before go-prompt switches to raw mode.
	// Needed for clean exit via Ctrl+C (os.Exit won't run defers).
	var restoreTerminal func()
	if oldState, err := term.GetState(int(os.Stdin.Fd())); err == nil {
		restoreTerminal = func() { term.Restore(int(os.Stdin.Fd()), oldState) }
	}

	// Mutable config pointer — the executor closure modifies it via /config set.
	cfgPtr := &cfg

	// Executor is called by go-prompt on every Enter press.
	executor := func(line string) {
		line = strings.TrimSpace(line)

		switch {
		case line == "":
			return

		case line == "/exit" || line == "/quit" || line == "exit" || line == "quit":
			gracefulExit(restoreTerminal)(nil)

		case line == "/help":
			ui.InteractiveHelp()

		case line == "/config" || strings.HasPrefix(line, "/config "):
			handleConfigCommand(strings.TrimPrefix(line, "/config"), workDir, cfgPtr)

		case line == "/model":
			ui.ShowModel(cfgPtr.Model)

		case strings.HasPrefix(line, "/model "):
			newModel := strings.TrimSpace(strings.TrimPrefix(line, "/model "))
			if newModel != "" {
				cfgPtr.Model = newModel
				ui.ModelChanged(newModel)
			}

		case strings.HasPrefix(line, "/"):
			ui.ConfigSetError(fmt.Sprintf("unknown command: %s (type /help for commands)", line))

		default:
			if err := runTask(workDir, *cfgPtr, line); err != nil {
				ui.Error(fmt.Sprintf("task failed: %v", err))
			}
			ui.TaskSeparator()
		}
	}

	p := goprompt.New(
		executor,
		completer,
		goprompt.OptionPrefix("departai> "),
		goprompt.OptionPrefixTextColor(goprompt.Cyan),
		goprompt.OptionPreviewSuggestionTextColor(goprompt.DarkGray),
		goprompt.OptionSuggestionBGColor(goprompt.DarkGray),
		goprompt.OptionSuggestionTextColor(goprompt.White),
		goprompt.OptionSelectedSuggestionBGColor(goprompt.Cyan),
		goprompt.OptionSelectedSuggestionTextColor(goprompt.Black),
		goprompt.OptionDescriptionBGColor(goprompt.DarkGray),
		goprompt.OptionDescriptionTextColor(goprompt.LightGray),
		goprompt.OptionSelectedDescriptionBGColor(goprompt.Cyan),
		goprompt.OptionSelectedDescriptionTextColor(goprompt.Black),
		goprompt.OptionCompletionWordSeparator(" "),
		goprompt.OptionCompletionOnDown(),
		goprompt.OptionAddKeyBind(goprompt.KeyBind{
			Key: goprompt.ControlC,
			Fn:  gracefulExit(restoreTerminal),
		}),
	)
	p.Run()

	// If Run() returns (e.g. Ctrl+D), go-prompt restores the terminal itself.
	return nil
}

// gracefulExit returns a KeyBindFunc that restores the terminal and exits cleanly.
// go-prompt runs in raw mode, so Ctrl+C is intercepted as a byte rather than a
// signal — we must restore the terminal state ourselves before calling os.Exit.
func gracefulExit(restore func()) goprompt.KeyBindFunc {
	return func(buf *goprompt.Buffer) {
		fmt.Println()
		if restore != nil {
			restore()
		}
		os.Exit(0)
	}
}

// ── autocomplete ───────────────────────────────────────────────────────────

// Top-level slash commands.
var topLevelCommands = []goprompt.Suggest{
	{Text: "/help", Description: "Show available commands"},
	{Text: "/config", Description: "Show current configuration"},
	{Text: "/config set", Description: "Set a config value"},
	{Text: "/config save", Description: "Save config to file"},
	{Text: "/model", Description: "Show or set the model"},
	{Text: "/exit", Description: "Exit departai"},
	{Text: "/quit", Description: "Exit departai"},
}

// Subcommands for "/config <sub>".
var configSubcommands = []goprompt.Suggest{
	{Text: "set", Description: "Set a config value for this session"},
	{Text: "save", Description: "Save config to project or global file"},
}

// Keys for "/config set <key>".
var configSetKeys = []goprompt.Suggest{
	{Text: "model", Description: "Model name (e.g. claude-opus-4-5)"},
	{Text: "backend", Description: "Agent backend (e.g. claude)"},
	{Text: "max-turns", Description: "Maximum agent turns (integer)"},
	{Text: "instructions", Description: "Path to instructions markdown file"},
}

// Targets for "/config save <target>".
var configSaveTargets = []goprompt.Suggest{
	{Text: "global", Description: "Save to ~/.departai/config.yml"},
}

// completer provides hierarchical, context-aware suggestions.
func completer(d goprompt.Document) []goprompt.Suggest {
	text := d.TextBeforeCursor()

	// No completions for non-slash input (task prompts).
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	// "/config save " → suggest targets
	if strings.HasPrefix(text, "/config save ") {
		return goprompt.FilterHasPrefix(configSaveTargets, d.GetWordBeforeCursor(), true)
	}

	// "/config set " → suggest keys
	if strings.HasPrefix(text, "/config set ") {
		return goprompt.FilterHasPrefix(configSetKeys, d.GetWordBeforeCursor(), true)
	}

	// "/config " → suggest subcommands
	if strings.HasPrefix(text, "/config ") {
		return goprompt.FilterHasPrefix(configSubcommands, d.GetWordBeforeCursor(), true)
	}

	// "/" → filter top-level commands
	return goprompt.FilterHasPrefix(topLevelCommands, text, true)
}

// ── config command handlers ────────────────────────────────────────────────

// handleConfigCommand processes "/config", "/config set ...", "/config save ...".
func handleConfigCommand(args string, workDir string, cfg *config.Config) {
	args = strings.TrimSpace(args)

	switch {
	case args == "":
		ui.ShowConfig(workDir, cfg.AgentBackend, cfg.Model, cfg.MaxTurns)

	case strings.HasPrefix(args, "set "):
		handleConfigSet(strings.TrimPrefix(args, "set "), cfg)

	case args == "save global":
		path := config.GlobalPath()
		if path == "" {
			ui.ConfigSetError("could not determine home directory")
			return
		}
		if err := cfg.Save(path); err != nil {
			ui.ConfigSetError(fmt.Sprintf("save failed: %v", err))
			return
		}
		ui.ConfigSaved(path)

	case args == "save":
		path := config.ProjectPath(workDir)
		if err := cfg.Save(path); err != nil {
			ui.ConfigSetError(fmt.Sprintf("save failed: %v", err))
			return
		}
		ui.ConfigSaved(path)

	default:
		ui.ConfigSetError(fmt.Sprintf("unknown config subcommand: %s (try /help)", args))
	}
}

// handleConfigSet parses "key value" and updates the config.
func handleConfigSet(kv string, cfg *config.Config) {
	parts := strings.SplitN(strings.TrimSpace(kv), " ", 2)
	if len(parts) < 2 || parts[1] == "" {
		ui.ConfigSetError("usage: /config set <key> <value>")
		ui.ConfigSetError("keys: model, backend, max-turns, instructions")
		return
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	switch key {
	case "model":
		cfg.Model = value
		ui.ConfigSet(key, value)

	case "backend":
		cfg.AgentBackend = value
		ui.ConfigSet(key, value)

	case "max-turns":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			ui.ConfigSetError("max-turns must be a positive integer")
			return
		}
		cfg.MaxTurns = n
		ui.ConfigSet(key, value)

	case "instructions":
		cfg.InstructionsFile = value
		ui.ConfigSet(key, value)

	default:
		ui.ConfigSetError(fmt.Sprintf("unknown key %q (valid: model, backend, max-turns, instructions)", key))
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
