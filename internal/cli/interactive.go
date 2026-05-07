package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/knz/bubbline/history"
	"github.com/manifoldco/promptui"

	claudeagent "github.com/manurgdev/departai/internal/agent/claude"
	codexagent "github.com/manurgdev/departai/internal/agent/codex"
	"github.com/manurgdev/departai/internal/config"
	"github.com/manurgdev/departai/internal/orchestrator"
	"github.com/manurgdev/departai/internal/tasklog"
	"github.com/manurgdev/departai/internal/ui"
)

// runInteractive starts the REPL that lets users type tasks interactively.
// Built on knz/bubbline (a bubbletea-based line editor): multi-line input with
// auto-resize, hierarchical autocomplete via Tab, persistent history across
// sessions, smart Up/Down (history at line boundaries, cursor movement
// otherwise). Ctrl+C cancels the current line; Ctrl+D / /exit / /quit exit.
func runInteractive(workDir string, cfg config.Config) error {
	ui.WelcomeBanner(workDir, cfg)

	cfgPtr := &cfg
	var currentTaskDir string
	var respecNextRun bool

	executeTask := func(taskDir string, isResume bool, forceSpecPreturn bool) {
		var td string
		var err error
		if isResume {
			td, err = resumeTask(workDir, *cfgPtr, taskDir, forceSpecPreturn)
		} else {
			td, err = runTask(workDir, *cfgPtr, taskDir, forceSpecPreturn)
		}

		var blocked *orchestrator.ErrAgentBlocked
		var oscillation *orchestrator.ErrOscillationDetected
		switch {
		case errors.As(err, &blocked):
			currentTaskDir = td
			ui.AgentBlocked(blocked.Agent, blocked.Reason)
		case errors.As(err, &oscillation):
			currentTaskDir = td
			ui.OscillationDetected(oscillation.Files, oscillation.Turns)
		case errors.Is(err, orchestrator.ErrUserStopped):
			currentTaskDir = td
			ui.TaskStopped()
		case err != nil:
			ui.Error(fmt.Sprintf("task failed: %v", err))
		default:
			if td != "" {
				currentTaskDir = td
			}
		}
		ui.TaskSeparator()
	}

	// Persistent history at ~/.departai/history.txt. Best-effort: any error
	// (no home dir, permission, etc.) silently degrades to in-memory only.
	histPath := historyFilePath()
	var hist []string
	if histPath != "" {
		_ = os.MkdirAll(filepath.Dir(histPath), 0755)
		if loaded, err := history.LoadHistory(histPath); err == nil {
			hist = loaded
		}
	}

	for {
		prompt := buildPromptPrefix(cfgPtr.Mode, currentTaskDir)

		val, err := runREPL(hist, prompt)
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println()
				return nil
			}
			if errors.Is(err, errCanceled) {
				fmt.Println("^C")
				continue
			}
			ui.Error(fmt.Sprintf("input error: %v", err))
			continue
		}

		line := strings.TrimSpace(val)
		if line == "" {
			continue
		}
		// Append to history (de-duplicating consecutive duplicates) and best-effort save.
		if len(hist) == 0 || hist[len(hist)-1] != val {
			hist = append(hist, val)
			if histPath != "" {
				_ = history.SaveHistory(hist, histPath)
			}
		}

		switch {
		case line == "/exit" || line == "/quit" || line == "exit" || line == "quit":
			fmt.Println()
			return nil

		case line == "/help":
			ui.InteractiveHelp()

		case line == "/dev":
			cfgPtr.Mode = "dev"
			ui.ModeChanged("dev")

		case line == "/ask":
			cfgPtr.Mode = "ask"
			ui.ModeChanged("ask")

		case line == "/continue":
			if currentTaskDir == "" {
				ui.NoActiveTask()
				continue
			}
			force := respecNextRun
			respecNextRun = false
			executeTask(currentTaskDir, true, force)

		case line == "/respec":
			if currentTaskDir == "" {
				ui.RespecNoActiveTask()
				continue
			}
			if respecNextRun {
				ui.RespecAlreadyQueued()
				continue
			}
			respecNextRun = true
			ui.RespecQueued()

		case line == "/resume":
			selected := handleResumeCommand(workDir)
			if selected == "" {
				continue
			}
			currentTaskDir = selected
			ui.TaskSelected(currentTaskDir)

		case line == "/new":
			currentTaskDir = ""
			respecNextRun = false
			ui.TaskCleared()

		case line == "/config" || strings.HasPrefix(line, "/config "):
			handleConfigCommand(strings.TrimPrefix(line, "/config"), workDir, cfgPtr)

		case line == "/model":
			ui.ShowModels(cfgPtr.Model, cfgPtr.ModelAlpha, cfgPtr.ModelBeta)

		case strings.HasPrefix(line, "/model "):
			handleModelCommand(strings.TrimSpace(strings.TrimPrefix(line, "/model ")), workDir, cfgPtr)

		case strings.HasPrefix(line, "/"):
			ui.ConfigSetError(fmt.Sprintf("unknown command: %s (type /help for commands)", line))

		default:
			// Plain text — task prompt.
			if currentTaskDir != "" {
				tl, err := tasklog.Load(currentTaskDir)
				if err != nil {
					ui.Error(fmt.Sprintf("loading task: %v", err))
					continue
				}
				if err := tl.AppendUserDirective(line); err != nil {
					ui.Error(fmt.Sprintf("appending directive: %v", err))
					continue
				}
				force := respecNextRun
				respecNextRun = false
				executeTask(currentTaskDir, true, force)
			} else {
				respecNextRun = false
				executeTask(line, false, false)
			}
		}
	}
}

// buildPromptPrefix returns the live prompt string in RAW form (no ANSI
// styling). The cyan color is applied by the textarea's FocusedStyle.Prompt
// inside the REPL model — embedding ANSI in the prompt string itself confuses
// the textarea's width measurement (uniseg counts ANSI bytes).
func buildPromptPrefix(mode, currentTaskDir string) string {
	if mode == "" {
		mode = "dev"
	}
	if currentTaskDir != "" {
		short := filepath.Base(currentTaskDir)
		if len(short) > 25 {
			short = short[:22] + "..."
		}
		return fmt.Sprintf("departai (%s) [%s]> ", mode, short)
	}
	return fmt.Sprintf("departai (%s)> ", mode)
}

// historyFilePath returns the path to ~/.departai/history.txt, or "" if the
// user's home directory cannot be resolved.
func historyFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".departai", "history.txt")
}

// ── autocomplete ───────────────────────────────────────────────────────────

// suggestion is a slash-command (or sub-token) candidate. Implements
// complete.Entry so bubbline can render Title + Description in the menu.
type suggestion struct {
	text string
	desc string
}

func (s suggestion) Title() string       { return s.text }
func (s suggestion) Description() string { return s.desc }

// Top-level slash commands.
var topLevelCommands = []suggestion{
	{"/help", "Show available commands"},
	{"/dev", "Switch to development mode (code-focused)"},
	{"/ask", "Switch to ask mode (research / Q&A)"},
	{"/config", "Show current configuration"},
	{"/config set", "Set a config value"},
	{"/config save", "Save config to file"},
	{"/model", "Show all agent models"},
	{"/model alpha", "Show/set Agent Alpha's model"},
	{"/model beta", "Show/set Agent Beta's model"},
	{"/model unset", "Clear the global model (falls back to backend default)"},
	{"/continue", "Continue the active task's relay loop"},
	{"/respec", "Force a spec pre-turn before the next prompt or /continue"},
	{"/resume", "Select a previous task (does not run it)"},
	{"/new", "Deselect current task — next prompt creates a new one"},
	{"/exit", "Exit departai"},
	{"/quit", "Exit departai"},
}

// Subcommands for "/config <sub>".
var configSubcommands = []suggestion{
	{"set", "Set a config value for this session"},
	{"save", "Save config to project or global file"},
}

// Keys for "/config set <key>".
var configSetKeys = []suggestion{
	{"model", "Global model for both agents"},
	{"model.alpha", "Override model for Agent Alpha"},
	{"model.beta", "Override model for Agent Beta"},
	{"backend", "Default backend (claude, codex)"},
	{"backend.alpha", "Override backend for Agent Alpha"},
	{"backend.beta", "Override backend for Agent Beta"},
	{"max-turns", "Maximum agent turns (integer)"},
	{"max-turn-duration", "Per-turn wall-clock budget (e.g. 15m, 1h30m)"},
	{"log-window", "Inject only the last N turns into each prompt (0 = full log)"},
	{"instructions", "Path to instructions markdown file"},
	{"mode", "Active mode: dev or ask"},
	{"blocked-commands", "Comma-separated tools/commands agents must NOT use"},
}

// Targets for "/config save <target>".
var configSaveTargets = []suggestion{
	{"global", "Save to ~/.departai/config.yml"},
}

// Subcommands for "/model <sub>".
var modelSubcommands = []suggestion{
	{"alpha", "Show/set Agent Alpha's model"},
	{"beta", "Show/set Agent Beta's model"},
	{"unset", "Clear the global model (falls back to backend default)"},
}

// Values suggested after "/model alpha " or "/model beta ".
var modelValueSuggestions = []suggestion{
	{"unset", "Clear this agent's override (inherits global)"},
}

// (Filter logic and popover live in repl_model.go — the 6 suggestion lists
// declared above are consumed there directly.)

// ── config command handlers ────────────────────────────────────────────────

// handleConfigCommand processes "/config", "/config set ...", "/config save ...".
func handleConfigCommand(args string, workDir string, cfg *config.Config) {
	args = strings.TrimSpace(args)

	switch {
	case args == "":
		ui.ShowConfig(workDir, *cfg)

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
		ui.ConfigSetError("keys: model, model.alpha, model.beta, backend, backend.alpha, backend.beta, mode, max-turns, max-turn-duration, log-window, instructions, blocked-commands")
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
		setModelValidated(cfg.AgentBackend, "Global model", value, func() {
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
		setModelValidated(cfg.BackendFor("alpha"), "Agent Alpha", value, func() {
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
		setModelValidated(cfg.BackendFor("beta"), "Agent Beta", value, func() {
			cfg.ModelBeta = value
			ui.ConfigSet(key, value)
			promptAndSave(workDir, *cfg)
		})

	case "backend":
		cfg.AgentBackend = value
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	case "backend.alpha":
		cfg.BackendAlpha = value
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	case "backend.beta":
		cfg.BackendBeta = value
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	case "max-turns":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			ui.ConfigSetError("max-turns must be 0 (unlimited) or a positive integer")
			return
		}
		cfg.MaxTurns = n
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	case "max-turn-duration":
		v := strings.TrimSpace(value)
		if isUnsetValue(v) || v == "0" {
			cfg.MaxTurnDurationStr = ""
			ui.ConfigSet(key, "(no limit)")
			promptAndSave(workDir, *cfg)
			return
		}
		if _, err := time.ParseDuration(v); err != nil {
			ui.ConfigSetError(fmt.Sprintf("max-turn-duration %q invalid: %v (use Go duration format, e.g. 15m, 1h30m)", v, err))
			return
		}
		cfg.MaxTurnDurationStr = v
		ui.ConfigSet(key, v)
		promptAndSave(workDir, *cfg)

	case "log-window":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			ui.ConfigSetError("log-window must be 0 (full log) or a positive integer")
			return
		}
		cfg.LogWindow = n
		if n == 0 {
			ui.ConfigSet(key, "(unlimited)")
		} else {
			ui.ConfigSet(key, value)
		}
		promptAndSave(workDir, *cfg)

	case "instructions":
		cfg.InstructionsFile = value
		ui.ConfigSet(key, value)
		promptAndSave(workDir, *cfg)

	case "mode":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "dev" && v != "ask" {
			ui.ConfigSetError(`mode must be "dev" or "ask"`)
			return
		}
		cfg.Mode = v
		ui.ConfigSet(key, v)
		promptAndSave(workDir, *cfg)

	case "blocked-commands":
		if isUnsetValue(value) {
			cfg.BlockedCommands = nil
			ui.ConfigSet(key, "(none)")
			promptAndSave(workDir, *cfg)
			return
		}
		// Comma-separated input → trimmed slice without empties.
		parts := strings.Split(value, ",")
		var list []string
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				list = append(list, t)
			}
		}
		cfg.BlockedCommands = list
		ui.ConfigSet(key, fmt.Sprintf("%d commands", len(list)))
		promptAndSave(workDir, *cfg)

	default:
		ui.ConfigSetError(fmt.Sprintf("unknown key %q (valid: model, model.alpha, model.beta, backend, backend.alpha, backend.beta, mode, max-turns, max-turn-duration, log-window, instructions, blocked-commands)", key))
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
		setModelValidated(cfg.BackendFor("alpha"), "Agent Alpha", value, func() {
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
		setModelValidated(cfg.BackendFor("beta"), "Agent Beta", value, func() {
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
		setModelValidated(cfg.AgentBackend, "Global model", first, func() {
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

// setModelValidated runs ValidateModel for newValue with a spinner, dispatching
// to the correct backend validator. On failure, shows an error and does NOT
// call onSuccess.
func setModelValidated(backend, target, newValue string, onSuccess func()) {
	var vErr error
	_ = ui.RunWithSpinner(fmt.Sprintf("Validating %s...", newValue), func() error {
		switch backend {
		case "codex":
			vErr = codexagent.ValidateModel(context.Background(), newValue)
		default: // "claude" or empty
			vErr = claudeagent.ValidateModel(context.Background(), newValue)
		}
		return nil
	})
	if vErr != nil {
		ui.ValidationFailed(target, newValue, vErr.Error())
		return
	}
	onSuccess()
}

// runTask creates an orchestrator for a single task prompt and runs it.
// forceSpecPreturn forces the spec pre-turn loop even if the spec is ACTIVE
// (no effect on a fresh task — spec starts DRAFT and pre-turns always run).
// Returns the task directory (for tracking) and any error.
func runTask(workDir string, cfg config.Config, prompt string, forceSpecPreturn bool) (string, error) {
	orch, err := orchestrator.New(orchestrator.Config{
		WorkDir:          workDir,
		Prompt:           prompt,
		Mode:             cfg.Mode,
		InstructionsFile: cfg.InstructionsFile,
		MaxTurns:         cfg.MaxTurns,
		AgentBackend:     cfg.AgentBackend,
		BackendAlpha:     cfg.BackendAlpha,
		BackendBeta:      cfg.BackendBeta,
		Model:            cfg.Model,
		ModelAlpha:       cfg.ModelAlpha,
		ModelBeta:        cfg.ModelBeta,
		BlockedCommands:  cfg.BlockedCommands,
		ForceSpecPreturn: forceSpecPreturn,
		MaxTurnDuration:  cfg.MaxTurnDuration(),
		LogWindow:        cfg.LogWindow,
	})
	if err != nil {
		return "", fmt.Errorf("initialising orchestrator: %w", err)
	}

	return orch.TaskDir(), orch.Run()
}

// resumeTask resumes an existing task from its task directory.
// forceSpecPreturn forces a fresh spec pre-turn loop even when the spec is
// already ACTIVE (used by /respec to incorporate new directives).
func resumeTask(workDir string, cfg config.Config, taskDir string, forceSpecPreturn bool) (string, error) {
	orch, err := orchestrator.NewFromExisting(orchestrator.Config{
		WorkDir:          workDir,
		Mode:             cfg.Mode,
		InstructionsFile: cfg.InstructionsFile,
		MaxTurns:         cfg.MaxTurns,
		AgentBackend:     cfg.AgentBackend,
		BackendAlpha:     cfg.BackendAlpha,
		BackendBeta:      cfg.BackendBeta,
		Model:            cfg.Model,
		ModelAlpha:       cfg.ModelAlpha,
		ModelBeta:        cfg.ModelBeta,
		BlockedCommands:  cfg.BlockedCommands,
		ForceSpecPreturn: forceSpecPreturn,
		MaxTurnDuration:  cfg.MaxTurnDuration(),
		LogWindow:        cfg.LogWindow,
	}, taskDir)
	if err != nil {
		return taskDir, fmt.Errorf("resuming task: %w", err)
	}

	return orch.TaskDir(), orch.Run()
}

// handleResumeCommand shows a promptui.Select with existing tasks and returns
// the selected task directory, or "" if cancelled.
func handleResumeCommand(workDir string) string {
	tasks, err := tasklog.ListTasks(workDir)
	if err != nil {
		ui.Error(fmt.Sprintf("listing tasks: %v", err))
		return ""
	}
	if len(tasks) == 0 {
		ui.Warning("No previous tasks found in this directory.")
		return ""
	}

	items := make([]string, len(tasks))
	for i, t := range tasks {
		prompt := t.Prompt
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}
		items[i] = fmt.Sprintf("%s  (%d turns)  %s", t.TaskID, t.TurnCount, prompt)
	}

	sel := promptui.Select{
		Label: "Select a task to resume",
		Items: items,
		Size:  10,
		Templates: &promptui.SelectTemplates{
			Label:    "  {{ . }}",
			Active:   "  ▸ {{ . | cyan }}",
			Inactive: "    {{ . | faint }}",
			Selected: "  ✓ {{ . | green }}",
		},
	}

	idx, _, err := sel.Run()
	if err != nil {
		return "" // cancelled
	}

	return tasks[idx].Dir
}

