# departai

An AI agent orchestrator CLI that runs two Claude Code agents in sequential turns on a shared coding task. Each agent can use its own model (e.g. Alpha on opus, Beta on sonnet). Agents hand off context through a shared task log until both agree the work is complete, then hands control back to you for review.

## How It Works

```
User → departai
         │
         ▼
   Interactive REPL with autocomplete
         │
         ├─► User types a task
         │
         ├─► Agent Alpha: reads log, works, appends turn summary (Complete: no)
         │
         ├─► Agent Beta:  reads log, continues work, appends turn summary (Complete: no)
         │
         ├─► Agent Alpha: reads log, finishes work (Complete: yes)
         │
         ├─► Agent Beta:  verifies, confirms (Complete: yes)
         │
         ├─► Both agreed → user reviews changes
         │
         └─► User types next task or /exit
```

**Why sequential turns?** Each agent gets a fresh context window. The task log is the handoff mechanism — each agent reads what was done and continues from there. This lets agents collaborate on tasks that would otherwise exhaust a single context window.

## Installation

### Using go install (recommended)

```bash
go install github.com/manurgdev/departai@latest
```

> Requires Go 1.21+. The binary lands in `$(go env GOPATH)/bin` — make sure that's on your `$PATH`.

### Build from source

```bash
git clone https://github.com/manurgdev/departai
cd departai
go build -o departai .
mv departai /usr/local/bin/departai   # or anywhere on $PATH
```

### Prerequisite

[Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) must be installed and authenticated:

```bash
npm install -g @anthropic-ai/claude-code
claude --version
```

## Usage

### Interactive mode (default)

Run `departai` with no arguments to start the interactive REPL:

```bash
departai
```

You'll see a prompt where you can type tasks directly or use slash commands with autocomplete:

```
  DepartAI — AI Agent Orchestrator

  Work dir  : /Users/you/projects/my-app
  Backend   : claude
  Model     : (default)
  Max turns : 10

  Type a task to start, or /help for commands.

departai> Build a REST API with user authentication
```

Type `/` to see a dropdown of available commands. Arrow keys (or Tab) navigate, Enter selects, and typing filters suggestions. The dropdown is hierarchical — type `/config ` and it suggests subcommands; type `/config set ` and it suggests config keys.

### Direct mode

Pass a prompt as an argument to run a single task and exit:

```bash
# Works in the current directory
departai "Build a REST API with user authentication"

# Specify a target project directory
departai --dir /path/to/project "Add unit tests for the payment module"

# Use a specific model
departai --model claude-opus-4-5 "Migrate the database schema to Postgres"

# Limit the number of turns
departai --max-turns 6 "Fix the failing CI pipeline"
```

## Interactive Commands

All commands use the `/` prefix. Autocomplete appears when you type `/` and filters as you continue typing.

| Command | Description |
|---------|-------------|
| `/help` | Show all available commands |
| `/config` | Show current configuration |
| `/config set <key> <value>` | Set a config value for the current session (validates if the key is a model) |
| `/config save` | Save current config to project `.departai/config.yml` |
| `/config save global` | Save current config to `~/.departai/config.yml` |
| `/model` | Show global + per-agent models |
| `/model <name>` | Set global model for both agents (validated) |
| `/model alpha` | Show Agent Alpha's current model |
| `/model alpha <name>` | Set Agent Alpha's model override (validated) |
| `/model beta` | Show Agent Beta's current model |
| `/model beta <name>` | Set Agent Beta's model override (validated) |
| `/exit`, `/quit` | Exit departai |

`exit` and `quit` also work without the `/` prefix. You can also press **Ctrl+C** or **Ctrl+D** to exit.

### Per-agent models

departai runs two sequential agents (Alpha and Beta) and you can give each a different model — for example, use a powerful model for the implementer and a cheaper one for the verifier:

```
departai> /model alpha claude-opus-4-5
  ⠋ Validating claude-opus-4-5...
  ✓ Agent Alpha model set to claude-opus-4-5

departai> /model beta claude-sonnet-4-5
  ⠋ Validating claude-sonnet-4-5...
  ✓ Agent Beta model set to claude-sonnet-4-5

departai> /model

  Models:
    Global       : (default)
    Agent Alpha  : claude-opus-4-5
    Agent Beta   : claude-sonnet-4-5
```

Resolution order per agent: the agent's override (`model_alpha` / `model_beta`) wins if set, otherwise the global `model` is used, otherwise the backend default.

### Model validation

Every time you set a model (via `/model`, `/model alpha|beta`, `/config set model*`, or the `--model` CLI flag), departai asks the backend whether it accepts that model name. If not, the session keeps the previous value:

```
departai> /model alpha totally-fake-model
  ⠋ Validating totally-fake-model...

  ✗ Model "totally-fake-model" rejected for Agent Alpha
    There's an issue with the selected model (totally-fake-model). It may not exist or you may not have access to it.
  Agent Alpha is unchanged.
```

Validation takes ~1–2 seconds and prevents typos from surfacing mid-task.

### Config keys for `/config set`

| Key | Example | Description |
|-----|---------|-------------|
| `model` | `/config set model claude-opus-4-5` | Global model (both agents, validated) |
| `model.alpha` | `/config set model.alpha claude-opus-4-5` | Override for Agent Alpha (validated) |
| `model.beta` | `/config set model.beta claude-sonnet-4-5` | Override for Agent Beta (validated) |
| `backend` | `/config set backend claude` | Agent backend |
| `max-turns` | `/config set max-turns 6` | Max agent turns |
| `instructions` | `/config set instructions ./my-rules.md` | Custom instructions file |

## CLI Flags

Flags work in both interactive and direct mode. In interactive mode, flags set the initial session config.

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | current directory | Working directory where agents operate |
| `--model` | (backend default) | Global model to use (e.g. `claude-opus-4-5`). Validated on startup. |
| `--backend` | `claude` | Agent backend CLI to use |
| `--instructions` | built-in | Path to a custom agent protocol markdown file |
| `--max-turns` | `10` | Safety cap on the number of turns |

If `--model` is rejected by the backend, departai exits immediately with the error — no task runs. Per-agent model overrides are only exposed through the REPL and config file, not the CLI flags.

## Configuration

departai uses YAML config files loaded in layers (later layers override earlier ones):

1. Built-in defaults
2. `~/.departai/config.yml` — user-global settings
3. `<project>/.departai/config.yml` — project-level settings
4. CLI flags
5. Interactive `/config set` commands (session only, unless saved)

### Config file format

```yaml
# .departai/config.yml

# Which CLI backend to use. Currently only "claude" is supported.
agent_backend: claude

# Maximum number of agent turns before stopping (safety cap).
max_turns: 10

# Global default model. Used by any agent that does not have its own override.
# If omitted, the backend uses its own default.
model: claude-opus-4-5

# Per-agent overrides (optional). Each overrides the global `model` for that agent.
model_alpha: claude-opus-4-5
model_beta: claude-sonnet-4-5

# Path to a custom base instructions markdown file (optional).
# instructions_file: ./my-instructions.md
```

### Managing config from the REPL

```
departai> /config set model claude-opus-4-5
  ⠋ Validating claude-opus-4-5...
  ✓ model set to claude-opus-4-5

departai> /config set model.alpha claude-opus-4-5
  ⠋ Validating claude-opus-4-5...
  ✓ model.alpha set to claude-opus-4-5

departai> /config save
  ✓ Config saved to /path/to/project/.departai/config.yml

departai> /config save global
  ✓ Config saved to /Users/you/.departai/config.yml
```

## Shared Context System

### Task Directory

Each run creates a task directory at `.departai/tasks/<task-id>/` inside the working directory:

```
<workdir>/
└── .departai/
    ├── config.yml                   ← project-level config (optional)
    └── tasks/
        └── 20240110-143022-build-rest-api/
            ├── task-log.md              ← structured handoff log
            ├── turn-1-agent-alpha-raw.log   ← full prompt + stdout/stderr
            └── turn-2-agent-beta-raw.log
```

Add `.departai/` to your `.gitignore` or commit the logs — your choice.

### Task Log Format

Agents append structured turn summaries to `task-log.md`:

```markdown
## Turn 1 - Agent Alpha

**Working Directory**: /path/to/project

**What I did**: Scaffolded the Go project, created models for User and Session,
set up the Gin router with /register and /login endpoints.

**Current State**: Endpoints exist but JWT signing is not implemented yet.

**Next Steps**: Implement JWT generation on login and middleware for protected routes.

**Complete**: no

---
```

Consensus is reached when the last two consecutive turns both report `**Complete**: yes`.

### Raw Turn Logs

Every turn generates a raw log file containing:
- The full prompt sent to the agent (base instructions + project rules + task log + turn directive)
- The agent's complete stdout output
- The agent's stderr output (if any)

These are invaluable for debugging agent behavior.

### Working Directory Auto-Detection

If an agent discovers the actual project is in a different directory than `--dir`, it reports the real path in the `**Working Directory**` field. The orchestrator detects this after each turn, moves the task directory to the correct project, and continues subsequent turns from there.

### Project Rules

At the start of each turn the orchestrator automatically reads and injects any project convention files it finds in the working directory:

- `CLAUDE.md`
- `AGENTS.md`
- `.cursorrules`
- `.github/copilot-instructions.md`

## Architecture

```
departai/
├── main.go
└── internal/
    ├── cli/
    │   ├── cli.go              # flag parsing, config loading, mode selection
    │   └── interactive.go      # REPL loop, go-prompt autocomplete, slash commands
    ├── config/
    │   └── config.go           # YAML config: load, save, layered merge
    ├── ui/
    │   └── ui.go               # styled terminal output, spinners, colors
    ├── agent/
    │   ├── agent.go            # Agent interface + TurnResult type
    │   └── claude/
    │       └── claude.go       # Claude Code CLI implementation
    ├── orchestrator/
    │   └── orchestrator.go     # turn loop, prompt builder, consensus check
    └── tasklog/
        └── tasklog.go          # task directory, log read/write/parse, relocate
```

### Adding a New Agent Backend

Implement the `agent.Agent` interface:

```go
type Agent interface {
    Name() string
    RunTurn(ctx context.Context, workDir string, prompt string) (TurnResult, error)
}
```

Then add a case to `buildAgents()` in `orchestrator.go`. For example, a future Codex CLI backend would live at `internal/agent/codex/codex.go` and be selected with `--backend codex` or `agent_backend: codex` in the config file.

## Completion Consensus

The orchestrator checks the task log after each turn. It stops and notifies you when the last two consecutive turns both report `**Complete**: yes`. If `--max-turns` is reached first, it stops and shows the current state for review.
