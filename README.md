# departai

AI agent orchestrator CLI that runs two Claude Code agents in sequential relay turns on a shared coding task. Each agent can use its own model. Agents hand off context through a shared task log, critically review each other's work, and only stop when both independently agree the task is complete.

## How It Works

```
departai
  │
  ├─► Interactive REPL with autocomplete
  │
  ├─► User types a task prompt
  │     └─► Agents start working in relay turns:
  │
  │         Turn 1 (Alpha):  Implements navigation + CTA changes → Complete: no
  │         Turn 2 (Beta):   Reviews Alpha, finds missing OG image fix, implements it → Complete: no
  │         Turn 3 (Alpha):  Reviews Beta's fix, runs tests, finds nothing wrong → Complete: yes
  │         Turn 4 (Beta):   Reviews everything, confirms all requirements met → Complete: yes
  │         └─► Consensus → task ends
  │
  ├─► User types another prompt (same task context)
  │     └─► Appended as directive, agents continue from where they left off
  │
  └─► /new to start fresh, /resume to pick a previous task
```

**Why sequential turns?** Each agent gets a fresh context window. The task log is the handoff mechanism — each agent reads what was done and continues from there. This avoids context window exhaustion on large tasks.

**Why two agents?** They critically review each other's work. An agent can only say "Complete: yes" if it made zero code changes during its turn — meaning it reviewed the other's work and found nothing wrong. This forces a real verification cycle.

## Installation

### Using go install

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

```bash
departai                         # uses current directory
departai --dir /path/to/project  # explicit project directory
```

The REPL shows a banner with current config and a prompt. Type `/` to see autocomplete suggestions for all commands:

```
  DepartAI — AI Agent Orchestrator

  Work dir     : /Users/you/projects/my-app
  Backend      : claude
  Max turns    : unlimited

  Models:
    Alpha Global : claude-opus-4-5
    Alpha Local  : (not set)
    Beta Global  : claude-opus-4-5
    Beta Local   : claude-sonnet-4-5

  Type a task to start, or /help for commands.

departai> Build a REST API with user authentication
```

### Direct mode

Run a single task without the REPL:

```bash
departai "Build a REST API with user authentication"
departai --dir /path/to/project "Add unit tests"
departai --model opus "Migrate the database schema"
departai --max-turns 6 "Fix the failing CI pipeline"
```

## Task Lifecycle

departai tracks an **active task**. The REPL prompt shows it:

```
departai>                                ← no active task
departai> Build an API                   ← creates new task, agents start working
departai [20260418-build-an-api]>        ← task is active, agents finished or paused
departai [20260418-build-an-api]> add auth middleware  ← adds directive to SAME task
departai [20260418-build-an-api]> /continue            ← resumes relay without new directive
departai [20260418-build-an-api]> /new                 ← deselects → back to "departai>"
departai> /resume                        ← pick any previous task
```

### Key concepts

- **New prompt with active task** — appended as a "User Directive" to the task log. Agents read it and act on it. Turn counter resets for `max-turns` but task log turn numbers keep incrementing.
- **New prompt without active task** — creates a new task from scratch.
- **`/continue`** — resumes the active task's agent relay loop (no new directive).
- **`/resume`** — shows a list of all previous tasks in the project, select one to make it active (does not run it — use `/continue` or type a prompt after).
- **`/new`** — deselects the active task. Next prompt creates a fresh one.
- **ESC** — press during a running turn to stop the agent immediately. The task stays active for `/continue` later.

## Streaming TUI

While agents work, departai shows a **bubbletea TUI** (alt-screen) with:

- **Pinned header** — turn number, agent name, model, elapsed time (always visible)
- **Live event stream** — agent reasoning text + tool calls as they happen
- **Spinner + total elapsed** in the footer
- **Auto-continue** — when a turn finishes, a 5-second countdown starts. Press any key to enter review mode, or wait to auto-continue to the next turn.

In **review mode** (after a turn finishes, press any key during countdown):

- `↑/↓` or `j/k` — navigate between tool calls
- `Enter` or `Space` — expand/collapse a tool call (shows diff for Edit operations)
- `q` or `Esc` — continue to next turn

After the TUI exits, a compact summary is printed to the terminal so the turn activity persists in scroll-back history.

## Interactive Commands

All commands use the `/` prefix with hierarchical autocomplete.

| Command | Description |
|---------|-------------|
| `/help` | Show all available commands |
| `/config` | Show current configuration |
| `/config set <key> <value>` | Set a config value (validates models, prompts to save) |
| `/config save` | Save config to project `.departai/config.yml` |
| `/config save global` | Save config to `~/.departai/config.yml` |
| `/model` | Show global + per-agent models |
| `/model <name>` | Set global model (validated) |
| `/model alpha [<name>]` | Show/set Agent Alpha's model (validated) |
| `/model beta [<name>]` | Show/set Agent Beta's model (validated) |
| `/model <agent> unset` | Clear an agent's override (inherits global) |
| `/continue` | Continue the active task's relay loop |
| `/resume` | Select a previous task from a list |
| `/new` | Deselect active task (next prompt = new task) |
| `/exit`, `/quit` | Exit departai |

`exit` and `quit` also work without `/`. Press **Ctrl+C** or **Ctrl+D** to exit.

### Config keys for `/config set`

| Key | Example | Description |
|-----|---------|-------------|
| `model` | `/config set model opus` | Global model for both agents (validated) |
| `model.alpha` | `/config set model.alpha opus` | Override for Agent Alpha (validated) |
| `model.beta` | `/config set model.beta sonnet` | Override for Agent Beta (validated) |
| `backend` | `/config set backend claude` | Agent backend |
| `max-turns` | `/config set max-turns 20` | Max turns per run (0 = unlimited) |
| `instructions` | `/config set instructions ./rules.md` | Custom instructions file |

### Model validation

Every model change is validated against the backend before being accepted (~1-2s). Invalid names are rejected and the previous value is kept.

### Persistence

After any config change, a menu asks where to save: **Project** (default), **Global**, or **Session only**. Ctrl+C on the menu = Session only.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | current directory | Working directory where agents operate |
| `--model` | (backend default) | Global model (validated on startup) |
| `--backend` | `claude` | Agent backend CLI to use |
| `--instructions` | built-in | Path to custom agent protocol markdown file |
| `--max-turns` | unlimited (0) | Max turns per run; 0 = no limit |

## Configuration

YAML config files loaded in layers (later wins):

1. Built-in defaults
2. `~/.departai/config.yml` — user-global
3. `<project>/.departai/config.yml` — project-level
4. CLI flags
5. Interactive `/config set` commands

```yaml
# .departai/config.yml
agent_backend: claude
max_turns: 0                    # 0 = unlimited
model: claude-opus-4-5          # global default
model_alpha: claude-opus-4-5    # per-agent overrides (optional)
model_beta: claude-sonnet-4-5
# instructions_file: ./my-instructions.md
```

## Agent Protocol

Agents follow a built-in protocol (overridable with `--instructions`):

- **Review first** — each agent critically reviews the previous agent's work before doing anything else. Look for bugs, missing edge cases, regressions.
- **Fix, don't note** — if something is wrong, fix it. Don't just write "there's a bug".
- **Run tests** — execute existing tests, write new ones if the project has a test framework.
- **Incremental work** — focus on one aspect per turn for large tasks. Leave clear handoff notes.
- **Complete: yes requires zero changes** — an agent can only mark "Complete: yes" if it reviewed the other agent's work, found no issues, and made no code changes itself. This forces a real verification cycle.

### Turn summary format

Each turn, agents append a structured block to the task log:

```markdown
## Turn 3 - Agent Alpha

**Working Directory**: /path/to/project

**Review of previous turn**: Checked Beta's OG image fix — badge text is correct,
image regenerated successfully, grep confirms no stray registration references.

**What I did**: Reviewed all modified files. No changes needed.

**Tests**: Ran `pnpm build` — completed successfully.

**Current State**: Registration fully disabled across all surfaces.

**Remaining Issues**: None

**Next Steps**: None — task is complete.

**Complete**: yes

---
```

### Completion consensus

The orchestrator stops when the last **two consecutive turns** both report `Complete: yes`. Since an agent can only say "yes" without making changes, this guarantees a review cycle: implement → review/fix → verify → confirm.

## Shared Context System

### Task directory

```
<workdir>/
└── .departai/
    ├── config.yml                           ← project-level config
    └── tasks/
        └── 20260418-build-rest-api/
            ├── task-log.md                  ← structured handoff log
            ├── turn-1-agent-alpha-raw.log   ← activity + output (no internal prompts)
            └── turn-2-agent-beta-raw.log
```

### Git recommendations

Add the task logs to your project's `.gitignore` — they contain verbose agent output and are only useful locally:

```gitignore
# departai task logs (local agent output)
.departai/tasks/
```

However, **keep `.departai/config.yml` tracked** if you work in a team. This way all developers share the same departai settings (models, max turns, instructions file) when collaborating on the project:

```gitignore
# departai — ignore task logs, keep config
.departai/tasks/
!.departai/config.yml
```

### Raw turn logs

Each turn generates a log file with:
- **Activity** — tool calls the agent made (Read, Edit, Bash, etc.)
- **Output** — the agent's final result text
- **Stderr** — error output (if any)

Internal prompting (base instructions, protocol) is NOT included — raw logs show only task-relevant information.

### Working directory auto-detection

If an agent discovers the project is in a different directory than `--dir`, it reports the real path in its `Working Directory` field. The orchestrator detects the mismatch, moves the task directory to the correct project, and continues from there.

### Project rules

The orchestrator automatically reads and injects any project convention files it finds:

- `CLAUDE.md`
- `AGENTS.md`
- `.cursorrules`
- `.github/copilot-instructions.md`

## Architecture

```
departai/
├── main.go
├── .gitignore
├── go.mod / go.sum
└── internal/
    ├── cli/
    │   ├── cli.go              # flag parsing, config loading, mode selection
    │   └── interactive.go      # REPL, go-prompt autocomplete, slash commands, task state
    ├── config/
    │   └── config.go           # YAML config: load, save, layered merge, per-agent models
    ├── tui/
    │   ├── agentview.go        # bubbletea model: streaming + review + auto-continue
    │   └── style.go            # lipgloss styles
    ├── ui/
    │   └── ui.go               # styled terminal output, spinners, colors
    ├── agent/
    │   ├── agent.go            # Agent interface + TurnResult type
    │   └── claude/
    │       ├── claude.go       # Claude Code CLI implementation + model validation
    │       └── stream.go       # stream-json parser for live tool call display
    ├── orchestrator/
    │   └── orchestrator.go     # turn loop, prompt builder, consensus, resume, ESC-to-stop
    └── tasklog/
        └── tasklog.go          # task directory, log read/write/parse, load, list, directives
```

### Key dependencies

| Package | Purpose |
|---------|---------|
| `c-bata/go-prompt` | Interactive REPL with autocomplete |
| `charmbracelet/bubbletea` | TUI for streaming agent output |
| `charmbracelet/bubbles` | Viewport component for scrollable content |
| `charmbracelet/lipgloss` | TUI styling |
| `fatih/color` | ANSI colors for non-TUI output |
| `briandowns/spinner` | Spinner for model validation |
| `manifoldco/promptui` | Arrow-key menus (save scope, task resume) |
| `gopkg.in/yaml.v3` | Config file parsing |

### Adding a new agent backend

Implement the `agent.Agent` interface:

```go
type Agent interface {
    Name() string
    RunTurn(ctx context.Context, workDir string, prompt string) (TurnResult, error)
}
```

Then add a case to `buildAgents()` in `orchestrator.go` and select it with `--backend <name>` or `agent_backend: <name>` in config.
