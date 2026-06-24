# DepartAI

> Two AI coding agents — from different vendors — take turns on a shared task, critically reviewing each other until both agree the work is done.

[![CI](https://github.com/manurgdev/departai/actions/workflows/ci.yml/badge.svg)](https://github.com/manurgdev/departai/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/manurgdev/departai?sort=semver)](https://github.com/manurgdev/departai/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/manurgdev/departai)](go.mod)
![Backends](https://img.shields.io/badge/backends-Claude%20%7C%20Codex-blue)

DepartAI is a **free, open-source CLI** that orchestrates two AI coding agents in a **deterministic, sequential relay** on a shared task. Each agent can use a **different backend and model** — e.g. Alpha on Claude Opus, Beta on OpenAI Codex — so two different model families catch each other's blind spots. Agents hand off context through a shared task log, review each other's work each turn, and stop only when **both independently agree** the task is complete *and* every acceptance criterion in a shared spec is checked.

**Why this and not a bigger multi-model swarm?** Deliberate focus. DepartAI is a single self-contained Go binary — no IDE or plugin lock-in — running a simple, predictable two-agent relay with a spec as the contract. Less ceremony, more determinism.

> **Bring your own backend.** DepartAI drives the official `claude` and/or `codex` CLIs, so you use your own LLM subscription(s). At least one must be installed and authenticated — see [Prerequisites](#prerequisites).

## How It Works

```
departai
  │
  ├─► Interactive REPL with autocomplete
  │
  ├─► User types a task prompt
  │     │
  │     ├─► Spec pre-turns (collaborative): each agent contributes Goal +
  │     │   Acceptance Criteria + Files in scope to a shared spec.md before
  │     │   any code is written. Append-only — later agents can extend but
  │     │   not weaken what earlier ones defined.
  │     │
  │     └─► Relay loop:
  │
  │         Turn 1 (Alpha):  Implements navigation + CTA → marks criterion [x] → Complete: no
  │         Turn 2 (Beta):   Reviews Alpha, fixes missing OG image, marks criterion [x] → Complete: no
  │         Turn 3 (Alpha):  Reviews Beta, runs tests, all criteria checked → Complete: yes
  │         Turn 4 (Beta):   Reviews everything, made zero changes → Complete: yes
  │         └─► Consensus + spec satisfied → task ends
  │
  ├─► User types another prompt (same task context)
  │     └─► Appended as directive, agents continue from where they left off
  │     └─► /respec re-runs the pre-turn loop to integrate the directive into the spec
  │
  └─► /new to start fresh, /resume to pick a previous task
```

**Why sequential turns?** Each agent gets a fresh context window. The task log is the handoff mechanism — each agent reads what was done and continues from there. This avoids context window exhaustion on large tasks.

**Why two agents?** They critically review each other's work. An agent can only say "Complete: yes" if (1) it made zero code changes during its turn — meaning it reviewed the other's work and found nothing wrong — AND (2) every Acceptance Criterion in the spec is checked off. This forces a real verification cycle anchored to a stable definition of done.

## Installation

### Using go install

```bash
go install github.com/manurgdev/departai@latest
```

> Requires Go 1.25+. The binary lands in `$(go env GOPATH)/bin` — make sure that's on your `$PATH`.

Pre-built binaries for macOS, Linux, and Windows are also attached to each [GitHub release](https://github.com/manurgdev/departai/releases).

### Build from source

```bash
git clone https://github.com/manurgdev/departai
cd departai
go build -o departai .
mv departai /usr/local/bin/departai   # or anywhere on $PATH
```

### Prerequisites

At least one supported AI CLI must be installed and authenticated:

**Claude Code CLI** (default backend):
```bash
npm install -g @anthropic-ai/claude-code
claude --version
```

**Codex CLI** (alternative backend):
```bash
npm install -g @openai/codex
codex --version
```

You can switch backends with `/config set backend codex` or `--backend codex`.

> **First run:** the very first time you launch `departai` with no config, it shows a short welcome, detects which backends are installed, and offers to write a starter `~/.departai/config.yml` — defaulting to cross-vendor (Alpha → Claude, Beta → Codex) when both are available.

Check your version anytime:

```bash
departai --version            # departai vX.Y.Z (os/arch)
departai --version --verbose  # + commit, build date, Go toolchain
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
  Mode         : dev
  Max turns    : unlimited
  Max turn time: no limit
  Log window   : unlimited
  Retries      : 2

  Agents:
    Alpha : claude / opus
    Beta  : codex / gpt-5.3-codex

  Type a task to start, or /help for commands.

departai (dev)> Build a REST API with user authentication
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
- **Token-level text streaming** (Claude backend) — text from the agent appears live, character by character, as the LLM generates it. Long generations (e.g. writing an updated spec.md) no longer surface as a silent gap.
- **In-flight indicators** — each tool call shows a spinner and a `(running Xs)` timer while the block is open, so you can tell when an agent is actively working on something vs. when a step has settled.
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
| `/dev` | Switch to development mode (code-focused) |
| `/ask` | Switch to ask mode (research / Q&A) |
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
| `/respec` | One-shot: force a fresh spec pre-turn before the next prompt or `/continue` (re-evaluates the spec to incorporate new directives or current state) |
| `/resume` | Select a previous task from a list |
| `/new` | Deselect active task (next prompt = new task) |
| `/exit`, `/quit` | Exit departai |

`exit` and `quit` also work without `/`. Press **Ctrl+D** on an empty line to exit. **Ctrl+C** cancels the current line without exiting (standard terminal UX).

The REPL supports **multi-line input** — long prompts wrap automatically to fit the terminal width and the editor grows vertically as you type. The prompt prefix is shown only on the first line; continuation lines align under the input column.

- **Shift+Enter** inserts a newline (multi-line input). Works in modern terminals that distinguish Shift+Enter from Enter via the kitty keyboard protocol: kitty, iTerm2 (with "Report modifiers using CSI u" enabled), alacritty, ghostty, WezTerm.
- **Alt+Enter** is the universal fallback for terminals that send Shift+Enter as a plain `\r` (Terminal.app default, older terminals).
- **Enter** submits the current input.

Pasting large multi-line texts works as expected — bracketed paste is enabled, so the entire paste is treated as a single insert (newlines inside don't submit early).

**Up/Down** arrows navigate command history when the cursor is at the first/last line of the input; otherwise they move the cursor between lines. Command history persists across sessions in `~/.departai/history.txt`.

### Config keys for `/config set`

| Key | Example | Description |
|-----|---------|-------------|
| `model` | `/config set model opus` | Global model for both agents (validated) |
| `model.alpha` | `/config set model.alpha opus` | Override for Agent Alpha (validated) |
| `model.beta` | `/config set model.beta sonnet` | Override for Agent Beta (validated) |
| `backend` | `/config set backend codex` | Default backend (`claude` or `codex`) |
| `backend.alpha` | `/config set backend.alpha claude` | Override backend for Agent Alpha |
| `backend.beta` | `/config set backend.beta codex` | Override backend for Agent Beta |
| `max-turns` | `/config set max-turns 20` | Max turns per run (0 = unlimited) |
| `max-turn-duration` | `/config set max-turn-duration 15m` | Per-turn wall-clock budget (Go duration format, e.g. `15m`, `1h30m`); empty = no limit |
| `log-window` | `/config set log-window 6` | Inject only the last N turns into each prompt (0 = full log). Reduces token cost on long tasks |
| `max-retries` | `/config set max-retries 2` | Retries per turn on a transient backend failure (0 disables) |
| `mode` | `/config set mode ask` | Active mode: `dev` (default) or `ask` |
| `instructions` | `/config set instructions ./rules.md` | Custom instructions file |
| `blocked-commands` | `/config set blocked-commands "WebFetch,rm -rf"` | Comma-separated list of tools/patterns agents must NOT use (soft enforcement) |

### Model validation

Every model change is validated against the backend before being accepted (~1-2s). Invalid names are rejected and the previous value is kept.

### Persistence

After any config change, a menu asks where to save: **Project** (default), **Global**, or **Session only**. Ctrl+C on the menu = Session only.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | current directory | Working directory where agents operate |
| `--model` | (backend default) | Global model (validated on startup) |
| `--backend` | `claude` | Agent backend: `claude` or `codex` |
| `--instructions` | built-in | Path to custom agent protocol markdown file |
| `--max-turns` | unlimited (0) | Max turns per run; 0 = no limit |
| `--max-turn-duration` | no limit | Per-turn wall-clock budget (e.g. `15m`, `1h30m`); empty = no limit |
| `--log-window` | unlimited (0) | Inject only the last N turns into each prompt; 0 = full log |
| `--max-retries` | `2` | Retries per turn on a transient backend failure (rate limit, 5xx, network blip); 0 disables |
| `--version` | — | Print version and exit (add `--verbose` for full build info) |
| `--verbose` | `false` | More detailed output (e.g. full build info with `--version`) |

## Configuration

YAML config files loaded in layers (later wins):

1. Built-in defaults
2. `~/.departai/config.yml` — user-global
3. `<project>/.departai/config.yml` — project-level
4. CLI flags
5. Interactive `/config set` commands

```yaml
# .departai/config.yml

agent_backend: claude           # default backend: "claude" or "codex"
backend_alpha: claude           # per-agent backend overrides (optional)
backend_beta: codex             # Alpha uses Claude, Beta uses Codex

max_turns: 0                    # turn-count cap per run (0 = unlimited)
max_turn_duration: 15m          # per-turn wall-clock budget (Go duration; empty = no limit)
log_window: 6                   # inject only the last N turns into prompts (0 = full log)
max_retries: 2                  # retries per turn on a transient backend failure (0 = disabled)

model: opus                     # default model (alias or full name; depends on the backend)
model_alpha: opus               # per-agent model overrides (optional)
model_beta: gpt-5.3-codex       # each agent can use its backend's models

# instructions_file: ./my-instructions.md
# blocked_commands:
#   - WebFetch
#   - "rm -rf"
```

## Modes — `/dev` and `/ask`

departai supports two modes:

- **`/dev`** (default) — coding tasks. Agents critically review each other, edit code, run tests, and only declare consensus when both made zero changes and verified the work.
- **`/ask`** — research / Q&A / analysis tasks. Agents discuss, gather evidence, cite sources, and produce a written answer. They can edit code if the question demands it, but the default output is analysis, not edits.

Switch from the REPL:

```
departai (dev)> /ask
  ✓ Mode set to ask

departai (ask)>
```

Or set explicitly:

```yaml
# .departai/config.yml
mode: ask
```

The active mode is always visible:

- In the **banner**: `Mode : ask`
- In the **REPL prompt**: `departai (ask)>` or `departai (ask) [task-id]>`
- In **`/config`** output

Each mode has its own built-in agent protocol. The dev mode emphasises code-edit cycles + tests; the ask mode emphasises evidence-based reasoning, citing sources, and answering precisely. Both share the same two-agent relay and consensus rule.

## Definition of Done — the spec

Every task gets a `spec.md` file alongside the task log. It is the **stable contract** both agents work from: Goal, Acceptance Criteria, Files in scope, Open questions, Decisions log. The orchestrator only declares the task complete when every Acceptance Criterion is checked off — no amount of agent enthusiasm overrides this.

### How the spec is populated

When a task is created, `spec.md` starts in `Status: DRAFT`. Before any code is written, the orchestrator runs a **spec pre-turn** for each agent in sequence:

```
Spec Pre-turn 1/2 (Alpha):  Drafts initial Goal + Acceptance Criteria + Files in scope
                            from the user's prompt. Sets Status to ACTIVE.
Spec Pre-turn 2/2 (Beta):   Reviews Alpha's draft, ADDS missing criteria/files,
                            moves ambiguities to Open questions. Append-only —
                            cannot remove or weaken what Alpha defined.
```

Both agents read the user's prompt and contribute their best version. The cross-vendor diversity catches blind spots in the spec itself, not just in the code.

After the pre-turn loop, the regular relay starts with the spec as anchor. Every turn:

- Reads the spec at the top of the prompt
- Marks `- [ ]` → `- [x]` on Acceptance Criteria as work is verified
- Appends rationale to **Decisions log** when making non-obvious choices
- Escalates ambiguities to **Open questions** rather than guessing
- Cannot remove or weaken existing criteria — the contract is append-only

### `/respec` — re-evaluate the spec

When you add a new directive to an active task that meaningfully changes scope, the spec needs to be updated to reflect it. Type `/respec` before your prompt:

```
departai (dev) [task-id]> /respec
  ✓ Spec re-evaluation queued
    Next prompt (or /continue) will run the spec pre-turns first.

departai (dev) [task-id]> también añade login con OAuth
  → Spec Pre-turn 1/2 (Alpha)   ← integrates the directive into the spec
  → Spec Pre-turn 2/2 (Beta)    ← reviews and extends if needed
  → Turn N (relay normal con spec actualizado)
```

`/respec` is one-shot — it consumes itself after the next prompt or `/continue`. It applies append-only rules: agents add criteria for the new directive but cannot remove existing ones. If a directive contradicts an existing criterion, the conflict goes to **Open questions** for the relay to resolve.

If you forget `/respec` and just type the directive, the relay still incorporates it (agents read User Directives in the task log), but the spec criteria stay as they were — agents may or may not extend them. `/respec` makes the spec update explicit and verifiable.

### Spec-aware completion

The relay stops only when ALL of:

1. The last two consecutive turns both report `**Complete**: yes`
2. The spec `Status` is `ACTIVE` (not DRAFT)
3. **Every** Acceptance Criterion is checked `- [x]`

If agents declare `Complete: yes` while criteria are unchecked, the orchestrator overrides them and continues the relay with a warning. The spec is the source of truth.

## Security — restricting commands

Agents run with full permissions and can use any tool the backend exposes (filesystem, shell, web, MCPs, etc.). For sensitive projects you can configure a blocklist of commands or tools that agents must NOT use.

```yaml
# .departai/config.yml
blocked_commands:
  - WebFetch              # block a whole tool
  - WebSearch
  - "rm -rf"              # block a shell pattern
  - "git push --force"
```

Or from the REPL:

```
departai> /config set blocked-commands "WebFetch,rm -rf,git push --force"
  ✓ blocked-commands set to 3 commands
```

When the blocklist is non-empty, departai injects a "Forbidden Commands" section into every turn's prompt instructing agents to refuse and stop if the task seems to require any of them. The second agent's review pass catches violations in the first agent's work.

> **This is soft enforcement.** The agent reads the instruction and is expected to comply. It is not a sandbox or syscall-level restriction. For hard isolation, run departai inside a container or use the host OS's permission system.

Global + project blocklists are **union-merged**: a project cannot un-block what's blocked globally. The merged list shows up in the banner as `Blocked : N command(s)` and in `/config` output.

## Reliability Features

Long-running autonomous relays can fail in subtle ways: an agent stalls, two agents disagree forever, a task quietly drifts off-scope, the prompt grows linearly forever as the log accumulates. departai includes opt-in safeguards for each.

### Per-turn time budget

Set `max_turn_duration` (or `--max-turn-duration`) and the orchestrator forcibly cancels a turn that exceeds the budget:

```yaml
max_turn_duration: 15m
```

When set, the prompt for every turn includes a **Time budget** section telling the agent how long it has, instructing it to checkpoint progress to the task log every 3-5 substantial actions, and to stop gathering context at ~80% of the budget so it can write its turn summary.

If the deadline fires anyway, the orchestrator:
- Kills the agent process via context cancellation
- Appends a synthetic turn entry to the task log marking the timeout
- Continues the relay with the next agent (whose budget is fresh)

The next agent reads the timeout note plus whatever the killed agent had time to write, and picks up from there. Time pressure alone is **never** a reason for an agent to escalate to the human — it just means the next agent gets a fresh budget.

### `Blocked on` — escalating to the human

Agents can pause the relay when they hit a decision the human must make. They add an optional field to their turn summary:

```markdown
**Complete**: no
**Blocked on**: The OAuth flow needs to know whether to use PKCE or implicit
flow. Acceptance criterion is ambiguous — need human decision.
```

The orchestrator detects the field and surfaces the question:

```
🚧 Agent Beta is blocked
   The OAuth flow needs to know whether to use PKCE or implicit flow.
   Acceptance criterion is ambiguous — need human decision.

Type a directive to unblock, or /continue to tell agents to decide themselves.
```

Three responses:
- **Type a prompt** — appended as a User Directive that resolves the question, relay continues
- **`/continue`** — relay continues without new info; the next agent sees the previous block and the protocol's anti-loop rule says "if the human did not respond, decide yourselves"
- **`/new`** — abandon

The protocol explicitly forbids using `Blocked on` for time pressure or technical workarounds. Reserved for genuine human-intent decisions.

### Scope warnings

The spec's `Files in scope` section names the files agents should be touching. If a turn modifies files outside that list — and doesn't add them to scope with a Decisions log entry — the next agent's prompt receives a warning:

```
## Scope warning

The previous turn (Turn 4, Agent Beta) modified files NOT listed in `Files in scope`:
- /unrelated/config.go

Either:
- Justify the change in **Decisions log** AND add the file to `Files in scope`, or
- Revert the off-scope change.
```

Soft enforcement (no syscall blocking), consistent with `blocked_commands`. The next agent reviews the off-scope change and either legitimises it (adds to scope + justification) or reverts.

> **Limitation**: file-modification tracking relies on Claude's per-tool stream events (`Edit`, `Write`, `MultiEdit`, `NotebookEdit`). Codex only exposes `Bash` with the raw command — modifications inside bash (`cat >`, `sed -i`, etc.) are not captured. Detection works when at least one agent is Claude; degrades to no-op when both are Codex.

### Oscillation detection

Two agents can disagree forever — Alpha fixes X, Beta breaks X, Alpha fixes again, ad infinitum. The orchestrator watches for this pattern: if the last 4 turns all touch ≥50% the same files (Jaccard overlap) AND no new Acceptance Criteria have been checked, it injects a warning:

```
## Possible oscillation detected

The last 4 turns have all touched mostly the same files (foo.go, bar.go) without
any new Acceptance Criteria being checked off.

Step back and identify the root cause:
- Are you and the previous agent disagreeing on the same point?
- Is there a misunderstanding of the spec?
- Is the criterion underspecified?

Take ONE decisive action this turn — or, if there's genuine disagreement that
needs human input, set **Blocked on** with the specifics.

If no progress within the next couple of turns, the orchestrator will stop the
relay and return control to the human.
```

If the pattern persists for 2 more turns (6 total), the orchestrator stops the relay and surfaces the situation:

```
🌀 Oscillation detected — relay stopped
   Last 6 turns kept touching: foo.go, bar.go
   Without new Acceptance Criteria being checked.

Type a directive to break the loop, or /continue to retry one more cycle.
```

`/continue` resets the detection — the relay gets a fresh K-turn window to escape. If it loops again, stops again. The same Codex limitation applies: oscillation detection works when at least one agent is Claude.

### Log windowing

For very long tasks the task log can grow to hundreds of turns. Injecting the entire log into every prompt is expensive and pushes recent context away from the agent's focus. Set `log_window` (or `--log-window`) to inject only the last N turns:

```yaml
log_window: 6
```

What's preserved regardless of windowing:
- The task header and `## Original Task`
- **All** `## User Directive` blocks (directives may add requirements)
- The last N `## Turn` entries
- An omission marker (`> _Turns 1–14 omitted to keep context bounded — full history in task-log.md._`) just before the kept turns

The full log on disk is never trimmed — the windowing only affects what's injected into each agent's prompt. The spec acts as the long-term anchor: Decisions log, Open questions, and checked criteria preserve the durable state.

Default is `0` (no windowing) for backward compatibility. Enable on long-running projects.

### Transient-error retry

Backend CLIs occasionally fail transiently — rate limits (`429`/`529`), `5xx`, or network blips. Instead of aborting the whole relay on a momentary hiccup, departai retries the turn with exponential backoff + jitter:

```yaml
max_retries: 2   # default; 0 disables
```

The failure is classified from the exit error + stderr. **Transient** errors (rate limit, overloaded, 5xx, timeouts, connection reset) are retried; **permanent** ones (invalid model, auth failure, missing CLI, cancelled by ESC/timeout) abort immediately — retrying them wouldn't help. On exhausting the retries, the turn fails as before. Configurable via `max_retries`, `--max-retries`, or `/config set max-retries`.

### Context-window awareness

On long tasks the prompt (instructions + spec + task log) grows turn by turn and can approach the model's context window. Before each turn, departai estimates the prompt size and, once per run, warns when it crosses ~80% of the window — suggesting `log_window` to bound growth:

```
⚠  Agent Alpha's prompt is ~175k tokens — 87% of the ~200k context window
   Bound prompt growth by windowing the task log: /config set log-window 6
```

The estimate is a backend-agnostic heuristic (it doesn't need a tokenizer); the spec preserves long-term state, so older turns can be safely elided.

> **Large outputs:** a single backend stream line is capped at 16 MB by default (raise with `DEPARTAI_MAX_STREAM_LINE_MB`). On overflow, departai surfaces a clear error rather than silently truncating the turn.

## Agent Protocol

Agents follow a built-in protocol (overridable with `--instructions`):

- **Anchor on the spec** — the `spec.md` is the definition of done. Mark `[x]` on Acceptance Criteria as work is verified. Append to **Decisions log** for non-obvious choices. Move ambiguities to **Open questions**. Append-only — never weaken or remove existing criteria.
- **Review first** — each agent critically reviews the previous agent's work before doing anything else. Look for bugs, missing edge cases, regressions.
- **Fix, don't note** — if something is wrong, fix it. Don't just write "there's a bug".
- **Run tests** — execute existing tests, write new ones if the project has a test framework.
- **Incremental work** — focus on one aspect per turn for large tasks. Leave clear handoff notes.
- **Complete: yes requires (1) zero changes AND (2) all spec criteria checked** — an agent can only mark "Complete: yes" if it reviewed the other agent's work, found no issues, made no code changes itself, AND every Acceptance Criterion is `- [x]`. This forces a real verification cycle anchored to the spec.
- **Treat orchestrator warnings as priority** — when the prompt includes a `## Scope warning` or `## Possible oscillation detected` section, address it FIRST before continuing the work.
- **Escalate to the human only for genuine intent decisions** — set `**Blocked on**: <reason>` in the turn summary only when a decision genuinely affects the human's intent and the spec doesn't unambiguously cover it. Time pressure or technical workarounds are NOT valid reasons.

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

**Blocked on**: <OPTIONAL — only set when escalating to the human; see Reliability Features above>

---
```

The `**Working Directory**` field must be the **project root** (where source code lives), not the task directory itself. If an agent reports a different path than what the orchestrator started with, the task directory is moved to match — this lets agents discover that the actual project lives somewhere else and signal it back.

### Completion consensus

The orchestrator stops when ALL of:

1. The last **two consecutive turns** both report `Complete: yes`
2. The spec `Status` is `ACTIVE` (not DRAFT)
3. **Every** Acceptance Criterion in the spec is checked `- [x]`

Since an agent can only say "yes" without making changes, this guarantees a review cycle: implement → review/fix → verify → confirm. The spec criteria requirement adds a second anchor: agents cannot prematurely declare done if the spec still has open work.

## Shared Context System

### Task directory

```
<workdir>/
└── .departai/
    ├── config.yml                              ← project-level config
    └── tasks/
        └── 20260418-build-rest-api/
            ├── task-log.md                     ← structured handoff log
            ├── spec.md                         ← definition of done (Goal, Criteria, Files in scope, ...)
            ├── spec-preturn-1-agent-alpha-raw.log  ← spec pre-turn raw activity
            ├── spec-preturn-2-agent-beta-raw.log
            ├── turn-1-agent-alpha-raw.log      ← per-turn activity + output (no internal prompts)
            ├── turn-1-agent-alpha-files.txt    ← per-turn files modified (for scope/oscillation detection)
            ├── turn-2-agent-beta-raw.log
            └── turn-2-agent-beta-files.txt
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
    │   ├── cli.go              # flag parsing, config layering, --version, backend availability check
    │   ├── interactive.go      # REPL loop, slash commands, task state, history persistence
    │   ├── repl_model.go       # custom bubbletea REPL (textarea + popover autocomplete)
    │   └── onboarding.go       # first-run detection + starter-config seeding
    ├── version/
    │   └── version.go          # build/version info (ldflags + runtime/debug fallback)
    ├── config/
    │   └── config.go           # YAML config: load, save, layered merge, per-agent models, retries
    ├── tui/
    │   ├── agentview.go        # bubbletea model: streaming + review + auto-continue
    │   └── style.go            # lipgloss styles
    ├── ui/
    │   └── ui.go               # styled terminal output, spinners, colors
    ├── agent/
    │   ├── agent.go            # Agent, StreamingAgent, StreamEvent interfaces
    │   ├── claude/
    │   │   ├── claude.go       # Claude Code CLI implementation + model validation
    │   │   ├── stream.go       # stream-json parser (stateful Parser for partial-message
    │   │   │                   # deltas + stateless ParseStreamLine fallback for legacy)
    │   │   └── stream_test.go  # parser tests (legacy + partial-message paths)
    │   └── codex/
    │       ├── codex.go        # Codex CLI implementation + model validation
    │       ├── stream.go       # Codex JSONL parser
    │       └── *_test.go       # parser + ValidateModel tests
    ├── orchestrator/
    │   └── orchestrator.go     # turn loop, spec pre-turns, prompt builder, consensus, ESC-to-stop,
    │                           # ErrAgentBlocked / ErrTurnTimeout / ErrOscillationDetected,
    │                           # scope + oscillation detection, transient-error retry,
    │                           # context-window awareness
    └── tasklog/
        ├── tasklog.go          # task directory, log read/write/parse, load (+corrupt-log recovery),
        │                       # spec.md primitives, windowed content, touched-files sidecars,
        │                       # synthetic timeout entries
        └── *_test.go           # parsing, spec, windowing, scope, relocate-safety, recovery tests
```

### Key dependencies

| Package | Purpose |
|---------|---------|
| `charmbracelet/bubbletea` | TUI for the streaming agent view and the custom REPL |
| `charmbracelet/bubbles` | `textarea` (REPL input) + `viewport` (scrollable content) |
| `charmbracelet/lipgloss` | TUI styling |
| `knz/bubbline/history` | Persistent REPL command history (`~/.departai/history.txt`) |
| `fatih/color` | ANSI colors for non-TUI output |
| `briandowns/spinner` | Spinner for model validation |
| `manifoldco/promptui` | Arrow-key menus (save scope, task resume, onboarding) |
| `gopkg.in/yaml.v3` | Config file parsing |

> The REPL is built directly on `bubbles/textarea` (custom multi-line input + inline command popover); only the `history` package from `knz/bubbline` is used, for persistent history.

### Adding a new agent backend

Implement the `agent.Agent` interface:

```go
type Agent interface {
    Name() string
    RunTurn(ctx context.Context, workDir string, prompt string) (TurnResult, error)
}
```

Then add a case to `buildAgents()` in `orchestrator.go` and select it with `--backend <name>` or `agent_backend: <name>` in config.

### Supported backends

| Backend | CLI | Auto-approve flag | Output format |
|---------|-----|-------------------|---------------|
| `claude` | `claude -p <prompt>` | `--dangerously-skip-permissions` | `--output-format stream-json --include-partial-messages` |
| `codex` | `codex exec <prompt>` | `--dangerously-bypass-approvals-and-sandbox` | `--json` (JSONL) |

Both backends implement the `agent.StreamingAgent` interface, providing live tool-call streaming via the bubbletea TUI. Model validation is backend-specific — each backend's `ValidateModel` function runs a minimal test prompt to verify the model name is accepted.

The Claude backend uses `--include-partial-messages` to receive token-level deltas (`stream_event` lines with `content_block_start` / `content_block_delta` / `content_block_stop` sub-events). The parser is stateful: it accumulates deltas per content block and emits `agent.StreamEvent`s carrying a `BlockID` so the TUI can update the same entry in place as text grows. This eliminates the "silent gap" the TUI used to show when the LLM generated a single large block (e.g. a `Write` of an 80 KB spec). The Codex backend continues to emit whole-block events; if a future Codex release adds a partial-stream mode, the same pattern can be applied.

## Contributing

Contributions are welcome — issues, ideas, and pull requests. See [ROADMAP.md](ROADMAP.md) for where the project is headed and what's up next.

Before submitting a PR, make sure the basics pass:

```bash
gofmt -l .        # should print nothing
go vet ./...
go test ./...     # backend-CLI tests auto-skip when claude/codex aren't installed
```

A full `CONTRIBUTING.md` (how to add a backend, coding conventions) is on the way.

## License

DepartAI is free and open-source software, released under the [MIT License](LICENSE).
