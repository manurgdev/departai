package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	goprompt "github.com/c-bata/go-prompt"
	"golang.org/x/term"

	claudeagent "github.com/manurgdev/departai/internal/agent/claude"
	"github.com/manurgdev/departai/internal/config"
	"github.com/manurgdev/departai/internal/orchestrator"
	"github.com/manurgdev/departai/internal/ui"
)

// runInteractive starts the REPL that lets users type tasks interactively.
// Slash commands get autocomplete via go-prompt; everything else is a task prompt.
func runInteractive(workDir string, cfg config.Config) error {
	ui.WelcomeBanner(workDir, cfg.AgentBackend, cfg.Model, cfg.ModelAlpha, cfg.ModelBeta, cfg.InstructionsFile, cfg.MaxTurns)

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
			ui.ShowModels(cfgPtr.Model, cfgPtr.ModelAlpha, cfgPtr.ModelBeta)

		case strings.HasPrefix(line, "/model "):
			handleModelCommand(strings.TrimSpace(strings.TrimPrefix(line, "/model ")), workDir, cfgPtr)

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
	{Text: "/model", Description: "Show all agent models"},
	{Text: "/model alpha", Description: "Show/set Agent Alpha's model"},
	{Text: "/model beta", Description: "Show/set Agent Beta's model"},
	{Text: "/model unset", Description: "Clear the global model (falls back to backend default)"},
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
	{Text: "model", Description: "Global model for both agents"},
	{Text: "model.alpha", Description: "Override model for Agent Alpha"},
	{Text: "model.beta", Description: "Override model for Agent Beta"},
	{Text: "backend", Description: "Agent backend (e.g. claude)"},
	{Text: "max-turns", Description: "Maximum agent turns (integer)"},
	{Text: "instructions", Description: "Path to instructions markdown file"},
}

// Targets for "/config save <target>".
var configSaveTargets = []goprompt.Suggest{
	{Text: "global", Description: "Save to ~/.departai/config.yml"},
}

// Subcommands for "/model <sub>".
var modelSubcommands = []goprompt.Suggest{
	{Text: "alpha", Description: "Show/set Agent Alpha's model"},
	{Text: "beta", Description: "Show/set Agent Beta's model"},
	{Text: "unset", Description: "Clear the global model (falls back to backend default)"},
}

// Values suggested after "/model alpha " or "/model beta ".
var modelValueSuggestions = []goprompt.Suggest{
	{Text: "unset", Description: "Clear this agent's override (inherits global)"},
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

	// "/config set ..." → suggest keys while typing the key, or model values
	// (e.g. "unset") while typing a value for a model key.
	if strings.HasPrefix(text, "/config set ") {
		rest := strings.TrimPrefix(text, "/config set ")
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) == 1 {
			return goprompt.FilterHasPrefix(configSetKeys, d.GetWordBeforeCursor(), true)
		}
		switch parts[0] {
		case "model", "model.alpha", "model.beta":
			return goprompt.FilterHasPrefix(modelValueSuggestions, d.GetWordBeforeCursor(), true)
		}
		return nil
	}

	// "/config " → suggest subcommands
	if strings.HasPrefix(text, "/config ") {
		return goprompt.FilterHasPrefix(configSubcommands, d.GetWordBeforeCursor(), true)
	}

	// "/model alpha " or "/model beta " → suggest "unset" (values are otherwise free-form)
	if strings.HasPrefix(text, "/model alpha ") || strings.HasPrefix(text, "/model beta ") {
		return goprompt.FilterHasPrefix(modelValueSuggestions, d.GetWordBeforeCursor(), true)
	}

	// "/model " → suggest agent-specific subcommands (alpha, beta, unset)
	if strings.HasPrefix(text, "/model ") {
		return goprompt.FilterHasPrefix(modelSubcommands, d.GetWordBeforeCursor(), true)
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
		ui.ShowConfig(workDir, cfg.AgentBackend, cfg.Model, cfg.ModelAlpha, cfg.ModelBeta, cfg.MaxTurns)

	case strings.HasPrefix(args, "set "):
		handleConfigSet(strings.TrimPrefix(args, "set "), workDir, cfg)

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
// After any successful change, the user is prompted to save (project/global/session).
func handleConfigSet(kv string, workDir string, cfg *config.Config) {
	parts := strings.SplitN(strings.TrimSpace(kv), " ", 2)
	if len(parts) < 2 || parts[1] == "" {
		ui.ConfigSetError("usage: /config set <key> <value>")
		ui.ConfigSetError("keys: model, model.alpha, model.beta, backend, max-turns, instructions")
		return
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	switch key {
	case "model":
		if isUnsetValue(value) {
			cfg.Model = ""
			ui.ModelUnset("Global model", "backend default")
			promptAndSave(workDir, *cfg)
			return
		}
		setModelValidated("Global model", value, func() {
			cfg.Model = value
			ui.ConfigSet(key, value)
			promptAndSave(workDir, *cfg)
		})

	case "model.alpha":
		if isUnsetValue(value) {
			cfg.ModelAlpha = ""
			ui.ModelUnset("Agent Alpha override", "global")
			promptAndSave(workDir, *cfg)
			return
		}
		setModelValidated("Agent Alpha", value, func() {
			cfg.ModelAlpha = value
			ui.ConfigSet(key, value)
			promptAndSave(workDir, *cfg)
		})

	case "model.beta":
		if isUnsetValue(value) {
			cfg.ModelBeta = ""
			ui.ModelUnset("Agent Beta override", "global")
			promptAndSave(workDir, *cfg)
			return
		}
		setModelValidated("Agent Beta", value, func() {
			cfg.ModelBeta = value
			ui.ConfigSet(key, value)
			promptAndSave(workDir, *cfg)
		})

	case "backend":
		cfg.AgentBackend = value
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	case "max-turns":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			ui.ConfigSetError("max-turns must be a positive integer")
			return
		}
		cfg.MaxTurns = n
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	case "instructions":
		cfg.InstructionsFile = value
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	default:
		ui.ConfigSetError(fmt.Sprintf("unknown key %q (valid: model, model.alpha, model.beta, backend, max-turns, instructions)", key))
	}
}

// handleModelCommand dispatches "/model <args>" where args comes after "/model ".
// After any successful model change, the user is prompted to save (project/global/session).
//
//   - "alpha"           → show Agent Alpha's current model
//   - "alpha <name>"    → set Agent Alpha's model override
//   - "beta"            → show Agent Beta's current model
//   - "beta <name>"     → set Agent Beta's model override
//   - "<name>"          → set global Model (any other single word)
func handleModelCommand(args string, workDir string, cfg *config.Config) {
	parts := strings.SplitN(args, " ", 2)
	first := strings.TrimSpace(parts[0])

	switch strings.ToLower(first) {
	case "alpha":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			ui.ShowModel("Agent Alpha", cfg.ModelFor("alpha"))
			return
		}
		value := strings.TrimSpace(parts[1])
		if isUnsetValue(value) {
			cfg.ModelAlpha = ""
			ui.ModelUnset("Agent Alpha override", "global")
			promptAndSave(workDir, *cfg)
			return
		}
		setModelValidated("Agent Alpha", value, func() {
			cfg.ModelAlpha = value
			ui.ModelChangedFor("Agent Alpha", value)
			promptAndSave(workDir, *cfg)
		})

	case "beta":
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			ui.ShowModel("Agent Beta", cfg.ModelFor("beta"))
			return
		}
		value := strings.TrimSpace(parts[1])
		if isUnsetValue(value) {
			cfg.ModelBeta = ""
			ui.ModelUnset("Agent Beta override", "global")
			promptAndSave(workDir, *cfg)
			return
		}
		setModelValidated("Agent Beta", value, func() {
			cfg.ModelBeta = value
			ui.ModelChangedFor("Agent Beta", value)
			promptAndSave(workDir, *cfg)
		})

	default:
		// Any other single word is treated as the global model name,
		// except "unset" which clears the global value.
		if first == "" {
			return
		}
		if isUnsetValue(first) {
			cfg.Model = ""
			ui.ModelUnset("Global model", "backend default")
			promptAndSave(workDir, *cfg)
			return
		}
		setModelValidated("Global model", first, func() {
			cfg.Model = first
			ui.ModelChanged(first)
			promptAndSave(workDir, *cfg)
		})
	}
}

// isUnsetValue reports whether the given value means "clear this setting".
// Accepts case-insensitive aliases so users can pick whichever feels natural.
func isUnsetValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "unset", "clear", "reset", "none":
		return true
	}
	return false
}

// promptAndSave asks the user (via an arrow-key menu) where to persist the
// current config and writes the file if a scope other than "session" is chosen.
func promptAndSave(workDir string, cfg config.Config) {
	projectPath := config.ProjectPath(workDir)
	globalPath := config.GlobalPath()
	displayGlobal := globalPath
	if displayGlobal == "" {
		displayGlobal = "(home dir unavailable)"
	}

	scope := ui.PromptSaveScope(projectPath, displayGlobal)

	var target string
	switch scope {
	case ui.SaveScopeProject:
		target = projectPath
	case ui.SaveScopeGlobal:
		if globalPath == "" {
			ui.ConfigSetError("cannot resolve home directory")
			return
		}
		target = globalPath
	default:
		return // session-only, nothing to persist
	}

	if err := cfg.Save(target); err != nil {
		ui.ConfigSetError(fmt.Sprintf("save failed: %v", err))
		return
	}
	ui.ConfigSaved(target)
}

// setModelValidated runs ValidateModel for newValue with a spinner.
// On success, invokes onSuccess (which commits the config change).
// On failure, shows a validation error and does NOT call onSuccess.
func setModelValidated(target, newValue string, onSuccess func()) {
	var vErr error
	_ = ui.RunWithSpinner(fmt.Sprintf("Validating %s...", newValue), func() error {
		vErr = claudeagent.ValidateModel(context.Background(), newValue)
		return nil
	})
	if vErr != nil {
		ui.ValidationFailed(target, newValue, vErr.Error())
		return
	}
	onSuccess()
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
		ModelAlpha:       cfg.ModelAlpha,
		ModelBeta:        cfg.ModelBeta,
	})
	if err != nil {
		return fmt.Errorf("initialising orchestrator: %w", err)
	}

	return orch.Run()
}
