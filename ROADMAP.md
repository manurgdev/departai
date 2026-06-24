# DepartAI Roadmap

DepartAI is a **free, open-source (MIT) CLI tool**. The differentiator vs. broader multi-model orchestrators (e.g. claude-octopus) is deliberate focus: a **deterministic, sequential 2-agent relay** with consensus + a spec-as-contract, shipped as a **standalone Go binary** (no IDE/plugin lock-in). We compete on technical merit and community, not price. An optional "Contribute"/Sponsors link is welcome but there is no paid tier, license server, or commercial backend.

This roadmap has two layers:

1. **Release path** — the phased path to a polished public OSS release (below). This is the priority spine.
2. **Feature backlog** — the thematic list of capabilities, picked opportunistically.

---

## 🚀 Release path

### Phase 1 — Product hardening ✅ COMPLETE

Make it boringly reliable and self-explanatory. (Detailed items live in the thematic backlog below — this is the curated phase checklist.) All items done — next up is Phase 2 (public release).

- [x] ~~**`--version` flag**~~ — **DONE**. `internal/version` resolves version/commit/date from ldflags (GoReleaser-ready) with a `runtime/debug.ReadBuildInfo` fallback. `--version` prints a clean one-liner (`departai vX.Y.Z (os/arch)`); full build metadata (commit, build date, Go toolchain) is under `--version --verbose`.
- [x] ~~**Clear error on missing/old backend CLI**~~ — **DONE**. `agent.CheckCLI` + per-backend `EnsureAvailable()`/`InstallHint`. `cli.Run` checks the configured backends up front (fatal in direct mode, warning in the REPL); `RunTurn` also maps `exec.ErrNotFound` to the actionable install hint as a safety net.
- [x] ~~**Graceful corrupt-task-log recovery**~~ — **DONE**. `Load` runs an integrity check (`logLooksValid`: valid UTF-8 + `# Task Log` header + extractable Original Task). On corruption it backs up the original to `task-log.md.corrupt-<ts>` and rebuilds a usable log (fresh header preserving the extractable prompt + recoverable turn/directive sections verbatim), recording a note in `TaskLog.Recovered` that the orchestrator surfaces as a warning. Never crashes, never proceeds on garbage.
- [x] ~~**Network/API failure retry**~~ — **DONE**. Transient backend failures (rate limit, 429/529, 5xx, network blips) retry the whole turn with exponential backoff + jitter; permanent failures (bad model, auth, missing CLI, cancelled context) abort immediately. Configurable via `max_retries` / `--max-retries` / `/config set max-retries` (default 2, 0 disables). `isTransientError` is a pure heuristic over the exit error + stderr; a `sleep` seam keeps the retry loop testable.
- [x] ~~**Stream buffer tuning**~~ — **DONE**. Per-line scanner cap raised 1 MB → 16 MB default, overridable via `DEPARTAI_MAX_STREAM_LINE_MB`. Both backends now check `scanner.Err()` (previously swallowed): on overflow they kill the process (avoiding a pipe-full deadlock) and return an actionable error instead of truncating the turn silently. Shared helpers `agent.StreamBufferBytes`/`StreamReadError`.
- [x] ~~**Context-window awareness**~~ — **DONE**. Before each turn, estimates the prompt's tokens (`len/4` heuristic) vs. the model's context window (conservative: 1M for explicit `[1m]` variants, else 200k) and warns once per run at ≥80%, suggesting `log_window` to bound prompt growth. Backend-agnostic; pure `contextBudgetExceeded` is unit-tested.
- [x] ~~**Orchestrator + codex package tests + E2E smoke**~~ — **DONE**. See Testing section.
- [x] ~~**First-run onboarding**~~ — **DONE**. On a first run (no config anywhere + no `~/.departai/.onboarded` marker), `runInteractive` shows a welcome (what departai is + detected backends) and, when a backend is present, offers to write a global config seeded from what's installed — defaulting to cross-vendor (Alpha → claude, Beta → codex) when both exist. With zero backends it shows install hints and keeps nagging until one is installed. Pure `onboardingConfig` decision is unit-tested.
- [ ] **`--verbose` / `--debug` flag** — dump full prompts for debugging (flag exists; prompt-dump still TODO).

### Phase 2 — Public release & distribution

Standard OSS distribution — public, no signing/licensing infra. `go install github.com/manurgdev/departai@latest` is back on the table (the README already documents it).

- [x] ~~**LICENSE (MIT)**~~ — **DONE**. MIT `LICENSE` file added (© 2026 Manuel Rodríguez Gil).
- [ ] **Tag `v0.1.0`** — first public release so `go install …@latest` resolves to a stable version.
- [x] ~~**GitHub Actions CI**~~ — **DONE**. `.github/workflows/ci.yml`: on push/PR to main, runs gofmt check, `go vet`, `go test -race`, and `go build` on ubuntu, using the Go version from `go.mod`. Backend-CLI tests auto-skip when the CLI is absent (as on the runner).
- [x] ~~**GoReleaser**~~ — **DONE**. `.goreleaser.yaml` (v2) builds 6 cross-platform binaries (macOS/Linux/Windows × amd64/arm64), tar.gz/zip archives + checksums + GitHub-sourced changelog, injecting version/commit/date into `internal/version` via ldflags (so `--version` shows real build time). `.github/workflows/release.yml` runs it on `v*` tags. Validated locally with `goreleaser check` + a `--snapshot` build (binary confirmed showing injected version).
- [x] ~~**Homebrew formula/tap**~~ — **DONE**. `homebrew_casks` in `.goreleaser.yaml` pushes a cask to the `manurgdev/homebrew-departai` tap on each release (installs binary + completions + man page; post-install strips Gatekeeper quarantine for the unsigned binary). `brew install manurgdev/departai/departai`. *Maintainer setup:* create the tap repo + `HOMEBREW_TAP_GITHUB_TOKEN` secret.
- [x] ~~**Shell completions + man page**~~ — **DONE**. `departai completion <bash|zsh|fish>` ([internal/cli/completion.go](internal/cli/completion.go)) prints the script; GoReleaser generates them at release time and the cask installs them. Static `man/departai.1` (lint-clean) installed by the cask. README documents both.
- [ ] **Changelog discipline** — semver, tagged releases, CHANGELOG.md.

> macOS code signing / notarization is **optional** for an OSS tool installed via `go install`/Homebrew (those paths don't trip Gatekeeper the way a downloaded `.app` does). Revisit only if shipping standalone downloadable binaries that users double-click.

### Phase 3 — Pre-public polish & community (before making the repo public)

The gate before the repo goes public. Three priorities:

**1. Documentation cleanup** — get every doc presentation-ready before strangers read it.
- [x] ~~**README rewrite — present departai to the world.**~~ — **DONE**. Header with tagline + badges (CI, release, MIT, Go version, backends), sharpened pitch (deterministic standalone 2-agent relay; "bring your own backend" expectations set), updated flag/config tables (`--max-retries`/`--version`/`--verbose`, `max_retries`), new Reliability sections (transient-error retry, context-window awareness, stream-buffer note), corrected Architecture/dependencies (bubbletea REPL not go-prompt; added version/onboarding), and Contributing + License sections.
- [x] ~~**Audit all docs for the OSS pivot**~~ — **DONE**. ROADMAP reframed to OSS; README has no commercial-era wording and `go install` is current; stale references (Go 1.21, go-prompt, bubbline-as-REPL) fixed.

**2. Stabilize the ROADMAP — a clear map for contributors.**
- [x] ~~**Define the next concrete development objectives**~~ — **DONE**. Added a prioritized **🎯 Up next**, a **Good first issues** list, and **Longer-term & bigger bets**, with the exhaustive thematic list moved under **Full backlog (by theme)**. Contributors now see "what to work on" at a glance.

**3. Community & OSS hygiene** — lower the barrier for users and contributors.
- [x] ~~**CONTRIBUTING.md**~~ — **DONE**. Covers prerequisites, build/run, the exact CI checks, testing conventions (stdlib testing, table-driven, `t.TempDir`/`t.Setenv`, skip-when-no-CLI, pure-logic + seams), code/commit conventions (Conventional Commits), a step-by-step "add a new backend" guide, and the PR process.
- [x] ~~**CODE_OF_CONDUCT.md** + issue/PR templates~~ — **DONE**. Contributor Covenant 2.1 (adopted by reference) + `.github/ISSUE_TEMPLATE/` (bug, feature) + `PULL_REQUEST_TEMPLATE.md`.
- [ ] **"Contribute" / Sponsors link** — *(maintainer)* add a `.github/FUNDING.yml` (GitHub Sponsors / Ko-fi / etc.) + a README link. Optional, fully free, no paywalled features.
- [ ] **Project docs** — optional lightweight docs site or GitHub Pages; not required (README may suffice).

### Open questions / decisions pending

- **Platforms at release** — macOS only first, or macOS + Linux + Windows? Drives GoReleaser + CI test matrix.
- **Backend CLI dependency** — departai requires the user to have `claude`/`codex` installed and authenticated (their own LLM subscription). Fine for an OSS dev-tool audience, but worth calling out clearly in the README so expectations are set.
- ~~**Business model**~~ — **DECIDED**: free & open-source (MIT). No paid tier, no license server, no commercial backend. The previously-planned `departai-web` (accounts/payments/licensing) is dropped; at most a simple optional landing/docs page remains.

---

## 🎯 Up next

The focused near-term plan — what actually moves departai toward a polished public release, in rough order. (The thematic backlog further down is the full idea pool, not a commitment.)

1. **Finish the public release (Phase 2).** Tag `v0.1.0`, Homebrew tap/formula, shell completions + man page, start a `CHANGELOG.md`.
2. **OSS community files (Phase 3).** `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, issue/PR templates, optional Sponsors link.
3. **`--verbose` / `--debug` prompt dump.** The flag exists; make it dump the full prompt sent to each agent (currently only in raw logs) for easier debugging.

### Good first issues

Well-scoped, low-risk, self-contained — good entry points for new contributors:

- **`/config unset <key>` + `/config reset`** — symmetric with `/config set`; isolated to the config command handler.
- **Config file validation** — warn on unknown keys / invalid values at load instead of silently ignoring them.
- **Env var overrides** — `DEPARTAI_MODEL`, `DEPARTAI_BACKEND`, etc. for scripting.
- **`/tasks` command** — read-only listing of project tasks (`tasklog.ListTasks` already exists).
- **Task renaming** — rename a task from its auto-generated ID to a human label.
- **Telegram notifications** — ping on consensus / ESC; self-contained, opt-in via config.
- **Shell completions** — bash/zsh/fish for the flags; mostly mechanical.
- **"Connecting…" TUI indicator** — placeholder line before the first stream event.
- **Project rules display** — show which `CLAUDE.md`/`AGENTS.md`/… files were loaded.
- **README example workflows + Troubleshooting FAQ** — docs only.

### Longer-term & bigger bets

Architectural or higher-uncertainty — worth doing, but not the near-term focus:

- **More than 2 agents** — generalize the relay to N agents + an N-aware consensus rule.
- **MCP server mode** — expose departai as an MCP server.
- **Custom backend support** — config/script-defined backends beyond claude/codex.
- **Custom modes** (`/security`, `/docs`, `/refactor`), **agent specialization**, **consensus strategies**.
- **Hard enforcement of the blocklist** — `--disallowedTools`, codex stream-watching, sandboxed execution.

---

## Full backlog (by theme)

The complete idea pool, grouped by area — the detail behind **Up next** above. Not a commitment; items are picked opportunistically.

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

## Modes

Today: `/dev` (default, code-focused) and `/ask` (research / Q&A) — see README.

Possible future modes (each with its own protocol):
- [ ] **`/security`** — agents focused on threat modelling, code review for vulnerabilities, fixes with minimal blast radius.
- [ ] **`/docs`** — agents collaborate on technical writing: structure first, then prose, then review for clarity/accuracy.
- [ ] **`/refactor`** — pure refactoring sessions: behaviour-preserving changes only, with strict test verification at each step.
- [ ] **Custom modes** — let the user define their own mode name + instructions file in config (`modes: { my-mode: ./instructions/my-mode.md }`).

## Agent protocol

- [x] ~~**Per-turn timeout**~~ — **DONE**. `max_turn_duration` (config + `--max-turn-duration`): hard wall-clock budget per turn; on deadline the orchestrator kills the process, appends a synthetic timeout entry, and continues with the next agent (fresh budget).
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
- [ ] **"Connecting…" indicator before first event** — Claude CLI emits `{"type":"system","subtype":"status","status":"requesting"}` when it starts the LLM call. Surface this as a placeholder line in the TUI viewport so the first 1–3 seconds of a turn aren't a blank screen. Low priority because partial-message streaming already makes the first text/tool block appear quickly; mostly cosmetic for the very initial wait.

## CLI / distribution

- [ ] **Tag `v0.1.0` release** — so `go install github.com/manurgdev/departai@latest` works with a stable version.
- [x] ~~**GitHub Actions CI**~~ — **DONE**. See Phase 2 — gofmt/vet/test-race/build on push/PR.
- [x] ~~**GoReleaser**~~ — **DONE**. See Phase 2 — `.goreleaser.yaml` + release workflow on `v*` tags.
- [x] ~~**Homebrew formula**~~ — **DONE**. See Phase 2 — Homebrew cask via GoReleaser to the `homebrew-departai` tap.
- [x] ~~**Shell completions**~~ — **DONE**. See Phase 2 — `departai completion <shell>` for bash/zsh/fish.
- [x] ~~**Man page**~~ — **DONE**. See Phase 2 — static `man/departai.1`.
- [ ] **`--verbose` / `--debug` flag** — the `--verbose` flag exists (currently it only expands `--version` into full build info). Still TODO: have `--verbose`/`--debug` dump the full prompt sent to each agent for debugging (currently only in raw logs).
- [x] ~~**`--version` flag**~~ — **DONE**. See Phase 1 / `internal/version`.

## Testing

All four testing items are now **DONE** — the package coverage needed to refactor Phase 1 safely is in place (tasklog 77%, orchestrator 60%, codex 55%, claude 57%).

- [x] ~~**tasklog package tests**~~ — **DONE**. Existing `tasklog_test.go` (spec primitives, windowing, scope, compaction, relocate-safety) extended with `tasklog_more_test.go`: `AppendUserDirective`, `Load` (prompt + turns), `ListTasks` (ordering, summaries, skips non-task dirs), `BothAgentsAgreeComplete`, `ParseLastWorkingDir`, `Relocate` (happy path + no-op), `CountSpecDecisions`.
- [x] ~~**orchestrator package tests**~~ — **DONE**. `orchestrator_test.go` covers pure helpers (`setOverlap`/Jaccard, `countCheckedCriteria`, `backendOrDefault`/`modelOrDefault`, `buildOneAgent`, `buildAgents`) plus E2E `Run()` flows. Added a test seam: `Orchestrator.runView` (nil → real `tui.RunAgentView`; tests inject a headless drainer) so turns run without launching bubbletea.
- [x] ~~**codex package tests**~~ — **DONE**. `stream_test.go` covers `ParseStreamLine` (agent_message, command_execution started/completed, ignored/malformed lines), `unwrapShellCommand` (all shell wrappers + escaped quotes + no-wrapper), and `singleLine`. `codex_test.go` covers constructors, streaming callbacks, and `ValidateModel` (skips if codex CLI unavailable).
- [x] ~~**End-to-end smoke test**~~ — **DONE**. A scripted fake `agent.StreamingAgent` drives the full relay: spec pre-turns → alternating turns → consensus stop. Cases: reaches consensus, exhausts max-turns, surfaces `ErrAgentBlocked`, and fails when pre-turns leave the spec DRAFT.

## Security — hard enforcement of blocklist

The current `blocked_commands` config is **soft enforcement** (prompt-injected). Future hard enforcement options:

- [ ] **Translate to claude `--disallowedTools`** — when the active backend is claude, also pass the blocklist via the native CLI flag for hard enforcement.
- [ ] **Codex stream-watching** — for codex (no native flag), parse the live stream and kill the agent if a blocked tool/command appears mid-turn. Best-effort but better than prompt-only.
- [ ] **Sandboxed execution** — run agents inside a container or with restricted filesystem/network access for true isolation.
- [ ] **Audit log** — record which commands the agent attempted vs which were blocked.

## Polish / reliability

- [x] ~~**Graceful handling of corrupt task log**~~ — **DONE**. See Phase 1 — `Load` backs up + rebuilds a malformed log and warns, preserving recoverable turns.
- [x] ~~**Better error messages on missing CLI**~~ — **DONE**. `agent.CheckCLI` + per-backend `EnsureAvailable()`; proactive check in `cli.Run` and `exec.ErrNotFound` mapping in `RunTurn` surface a clear "Install with `npm install -g …`" message.
- [x] ~~**Stream buffering tuning**~~ — **DONE**. See Phase 1 — 16 MB default, `DEPARTAI_MAX_STREAM_LINE_MB` override, and `scanner.Err()` now surfaced instead of swallowed.
- [x] ~~**Context window awareness**~~ — **DONE**. See Phase 1 — per-turn prompt-size estimate vs. model window, one-shot warning at ≥80% suggesting `log_window`.
- [x] ~~**Handle network/API failures**~~ — **DONE**. See Phase 1 — transient-error retry with backoff, configurable via `max_retries`.
- [x] ~~**Migrate from `c-bata/go-prompt` to a maintained input library**~~ — **DONE**. Migrated to `knz/bubbline` (bubbletea-based, multi-line editing, persistent history, smart Up/Down at line boundaries). Eliminated the panic class (no zombie goroutines) and added multi-line input wrap.

## Configuration UX

- [ ] **`/config unset <key>`** — symmetric with `/config set` for clearing any key (not just models).
- [ ] **`/config reset`** — restore defaults in session (without touching saved files).
- [ ] **Config file validation** — on load, warn about unknown keys or invalid values instead of silently ignoring.
- [ ] **Env var overrides** — `DEPARTAI_MODEL`, `DEPARTAI_BACKEND`, etc. for scripting.
- [ ] **`spec_preturn_effort` config (Claude only)** — pass `--effort <low|medium|high|xhigh|max>` to the Claude CLI for spec pre-turns. Pre-turns mostly edit `spec.md` rather than reason about new code; reducing the thinking budget (e.g. default `medium`) should cut wall-clock time on `/respec` over long tasks without losing fidelity. Requires extending the agent API with a `RunOptions{Effort string}` so the orchestrator can pass it only on pre-turn calls; main turns stay at the CLI default. Codex has no equivalent flag — ignored on that backend.

## Documentation

- [x] ~~**Contributing guide**~~ — **DONE**. See `CONTRIBUTING.md` (build/test, conventions, add-a-backend guide, PR process).
- [ ] **Example use cases** — README section with 2-3 real workflow examples (e.g. "migrate a schema", "add tests to a module").
- [ ] **Troubleshooting FAQ** — common errors (CLI not installed, model rejected, terminal issues with alt-screen).
