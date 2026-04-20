# DepartAI Roadmap

Pending work compiled from the conversation history, categorized by theme. Not strict priorities — pick what matches the moment.

## Core features (explicitly mentioned)

- [ ] **Telegram notifications** — original spec mentioned "Telegram integration later" for completion notifications. Send a message when consensus is reached or when ESC is pressed.
- [ ] **Custom backend support** — deferred in favor of Claude + Codex. Let users define a backend in config with command template, flags, and output format (config-driven) or via a script wrapper that follows a simple JSONL protocol.
- [ ] **MCP server mode** — expose departai as an MCP server so other tools can invoke it (e.g. from Claude Code itself).

## Task management

- [ ] **`/tasks` command** — read-only listing of all tasks in the project (resume without selecting).
- [ ] **Task renaming** — let users rename tasks from auto-generated IDs to human labels (`/task rename my-api-work`).
- [ ] **Task deletion/cleanup** — `/task delete <id>` and maybe `/tasks clean --older-than 30d`.
- [ ] **Persist `currentTaskDir` across sessions** — remember the last active task when departai restarts (state in `~/.departai/state.yml` or similar).
- [ ] **Multi-project task view** — see tasks from all projects, not just the current working dir (e.g. `/resume --all`).

## Agent protocol

- [ ] **Per-turn timeout** — besides `max_turns`, add a per-turn wall-clock timeout (e.g. `max_turn_duration: 15m`) to prevent a single turn from hanging forever.
- [ ] **Custom agent names** — let users name agents beyond "Agent Alpha"/"Agent Beta" (e.g. "Implementer" / "Reviewer") via config.
- [ ] **More than 2 agents** — generalise the relay to N agents (config: `agents: [alpha, beta, gamma]`). Consensus rule becomes "all last-N agree" or similar.
- [ ] **Consensus strategies** — beyond "last two consecutive yes": majority vote, explicit reviewer role, timeout-based.
- [ ] **Agent specialization** — let each agent have its own instructions file (implementer vs verifier personas) — `instructions_alpha` / `instructions_beta`.

## TUI / UX

- [ ] **Copy result to clipboard** — hotkey in review mode to copy the final agent output.
- [ ] **Filter events** — toggle showing only tool calls, only text, or everything in review mode.
- [ ] **Search within a turn** — `/` key in TUI to find an event (e.g. which file was edited).
- [ ] **Tool result display** — currently only tool calls are shown; show the result/output of each tool (especially Bash exit codes).
- [ ] **Project rules display** — show which `CLAUDE.md`/`AGENTS.md`/`.cursorrules` files were loaded at startup or via `/config`.
- [ ] **Inline code syntax highlighting** — in the expanded diff view, highlight diff syntax.

## CLI / distribution

- [ ] **Tag `v0.1.0` release** — so `go install github.com/manurgdev/departai@latest` works with a stable version.
- [ ] **GitHub Actions CI** — run `go test`, `go vet`, `go build` on push/PR.
- [ ] **GoReleaser** — pre-built binaries for macOS/Linux/Windows on GitHub releases.
- [ ] **Homebrew formula** — `brew install departai`.
- [ ] **Shell completions** — bash/zsh/fish completions for `--dir`, `--model`, `--backend`, etc.
- [ ] **Man page** — `man departai` with full flag documentation.
- [ ] **`--verbose` / `--debug` flag** — dump the full prompt sent to each agent for debugging (currently only in raw logs).
- [ ] **`--version` flag** — print version + build info.

## Testing

- [ ] **tasklog package tests** — turn parsing, AppendUserDirective, Load, ListTasks, Relocate.
- [ ] **orchestrator package tests** — buildAgents with different backend combos, consensus logic, turn counting.
- [ ] **codex package tests** — stream parsing, command unwrapping, ValidateModel (skip if codex CLI unavailable).
- [ ] **End-to-end smoke test** — spin up a fake backend, verify full turn flow.

## Polish / reliability

- [ ] **Graceful handling of corrupt task log** — if the markdown is malformed, recover (skip bad entries, show warning) instead of crashing.
- [ ] **Better error messages on missing CLI** — if `claude`/`codex` isn't installed, show a clear "Install with `npm install -g @anthropic-ai/claude-code`" message instead of a generic exec error.
- [ ] **Stream buffering tuning** — large prompts or very verbose turns may hit the 1MB scanner buffer. Make it configurable or larger.
- [ ] **Context window awareness** — detect when an agent is approaching its context limit and log a warning.
- [ ] **Handle network/API failures** — retry on transient errors from the backend CLI instead of failing the turn.

## Configuration UX

- [ ] **`/config unset <key>`** — symmetric with `/config set` for clearing any key (not just models).
- [ ] **`/config reset`** — restore defaults in session (without touching saved files).
- [ ] **Config file validation** — on load, warn about unknown keys or invalid values instead of silently ignoring.
- [ ] **Env var overrides** — `DEPARTAI_MODEL`, `DEPARTAI_BACKEND`, etc. for scripting.

## Documentation

- [ ] **Contributing guide** — CONTRIBUTING.md explaining how to add a backend, how tests work, coding conventions.
- [ ] **Example use cases** — README section with 2-3 real workflow examples (e.g. "migrate a schema", "add tests to a module").
- [ ] **Troubleshooting FAQ** — common errors (CLI not installed, model rejected, terminal issues with alt-screen).
