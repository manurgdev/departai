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

```bash
git clone https://github.com/manurgdev/departai
cd departai
go build -o departai .
# Optionally move to PATH:
mv departai /usr/local/bin/departai
```

**Prerequisite:** [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) must be installed and authenticated.

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

# Custom base instructions (override the built-in agent protocol)
departai --instructions ./my-instructions.md "Refactor the database layer"

# Limit the number of turns (default: 20)
departai --max-turns 10 "Fix the failing CI pipeline"
```

## Shared Context System

### Task Directory

Each run creates a task directory at `.departai/tasks/<task-id>/` inside the working directory:

```
<workdir>/
└── .departai/
    └── tasks/
        └── 20240110-143022-build-rest-api/
            └── task-log.md        ← shared handoff file
```

Add `.departai/` to your `.gitignore` or commit the logs — your choice.

### Task Log Format

Agents append structured turn summaries to `task-log.md`:

```markdown
# Task Log

**Task ID**: 20240110-143022-build-rest-api
**Started**: 2024-01-10 14:30:22

## Original Task

Build a REST API with user authentication

---

## Turn 1 - Agent Alpha

**What I did**: Scaffolded the Go project, created models for User and Session,
set up the Gin router with /register and /login endpoints.

**Current State**: Endpoints exist but JWT signing is not implemented yet.

**Next Steps**: Implement JWT generation on login and middleware for protected routes.

**Complete**: no

---

## Turn 2 - Agent Beta

**What I did**: Implemented JWT signing with HS256, added auth middleware,
protected /profile endpoint, wrote integration tests.

**Current State**: All endpoints work. Tests pass.

**Next Steps**: None — task is complete.

**Complete**: yes

---
```

Consensus is reached when the last two consecutive turns both report `**Complete**: yes`.

### Project Rules

At the start of each turn the orchestrator automatically reads and includes any project convention files it finds:

- `CLAUDE.md`
- `AGENTS.md`
- `.cursorrules`
- `.github/copilot-instructions.md`

Agents are instructed to follow these rules.

## Architecture

```
departai/
├── main.go
└── internal/
    ├── cli/
    │   └── cli.go              # flag parsing, wires orchestrator
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

Then swap it in (or mix it in) via `orchestrator.New`. For example, a future Codex CLI backend would live at `internal/agent/codex/codex.go`.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | current directory | Working directory where agents operate |
| `--instructions` | built-in | Path to a custom agent protocol markdown file |
| `--max-turns` | 20 | Safety cap on the number of turns |

## Completion Consensus

The orchestrator checks the task log after each turn. It stops the loop and notifies you when both of the last two consecutive turns report `**Complete**: yes`. If `--max-turns` is reached first, it stops and asks you to review the current state.
