# departai

An AI agent orchestrator CLI that runs two Claude Code agents in sequential turns on a shared coding task. Agents hand off context through a shared task log until both agree the work is complete, then hands control back to you for review.

## How It Works

```
User → departai "Build a REST API"
         │
         ▼
   Orchestrator creates task directory + log
         │
         ├─► Agent Alpha: reads log, works, appends turn summary (Complete: no)
         │
         ├─► Agent Beta:  reads log, works, appends turn summary (Complete: no)
         │
         ├─► Agent Alpha: reads log, works, appends turn summary (Complete: yes)
         │
         ├─► Agent Beta:  reads log, works, appends turn summary (Complete: yes)
         │
         └─► Both agreed → notify user to review
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

```bash
# Basic — works in the current directory
departai "Build a REST API with user authentication"

# Specify a target project directory
departai --dir /path/to/project "Add unit tests for the payment module"

# Use a specific Claude model
departai --model claude-opus-4-5 "Migrate the database schema to Postgres"

# Custom base instructions (override the built-in agent protocol)
departai --instructions ./my-instructions.md "Refactor the database layer"

# Limit the number of turns
departai --max-turns 6 "Fix the failing CI pipeline"
```

## Configuration

departai supports a YAML config file. Settings are loaded in this order (later layers win):

1. Built-in defaults
2. `~/.config/departai/config.yml` — user-global settings
3. `<project-dir>/.departai.yml` — project-level settings
4. CLI flags — always take highest precedence

### Config file format

```yaml
# .departai.yml

# Which CLI backend to use. Currently only "claude" is supported.
agent_backend: claude

# Maximum number of agent turns before stopping (safety cap).
max_turns: 10

# Model to pass to the backend (optional).
# If omitted, the backend uses its own default.
model: claude-opus-4-5

# Path to a custom base instructions markdown file (optional).
# instructions_file: ./my-instructions.md
```

Place `.departai.yml` in your project root to set per-project defaults, or in
`~/.config/departai/config.yml` for user-wide defaults.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | current directory | Working directory where agents operate |
| `--model` | (backend default) | Model to use (e.g. `claude-opus-4-5`) |
| `--backend` | `claude` | Agent backend CLI to use |
| `--instructions` | built-in | Path to a custom agent protocol markdown file |
| `--max-turns` | `10` | Safety cap on the number of turns |

## Shared Context System

### Task Directory

Each run creates a task directory at `.departai/tasks/<task-id>/` inside the working directory:

```
<workdir>/
└── .departai/
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

### Working Directory Auto-Detection

If an agent discovers the actual project is in a different directory than `--dir`,
it reports the real path in the `**Working Directory**` field. The orchestrator
detects this, moves the task directory to the correct project, and continues
subsequent turns from there.

### Project Rules

At the start of each turn the orchestrator automatically reads and injects any
project convention files it finds in the working directory:

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
    │   └── cli.go              # flag parsing, config loading, wires orchestrator
    ├── config/
    │   └── config.go           # YAML config loading with layered merge
    ├── ui/
    │   └── ui.go               # styled terminal output + spinner
    ├── agent/
    │   ├── agent.go            # Agent interface + TurnResult type
    │   └── claude/
    │       └── claude.go       # Claude Code CLI implementation
    ├── orchestrator/
    │   └── orchestrator.go     # turn loop, prompt construction, consensus check
    └── tasklog/
        └── tasklog.go          # task directory creation, log read/write/parse
```

### Adding a New Agent Backend

Implement the `agent.Agent` interface:

```go
type Agent interface {
    Name() string
    RunTurn(ctx context.Context, workDir string, prompt string) (TurnResult, error)
}
```

Then add a case to `buildAgents()` in `orchestrator.go`. For example, a future
Codex CLI backend would live at `internal/agent/codex/codex.go` and be selected
with `--backend codex` or `agent_backend: codex` in the config file.

## Completion Consensus

The orchestrator checks the task log after each turn. It stops and notifies you
when the last two consecutive turns both report `**Complete**: yes`. If
`--max-turns` is reached first, it stops and shows the current state for review.
