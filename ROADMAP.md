# DepartAI Roadmap

DepartAI is moving from a personal CLI tool to a **commercial product sold to third parties**. This roadmap has two layers:

1. **Go-to-market** — the phased path to a sellable product (below). This is the priority spine.
2. **Feature backlog** — the thematic list of capabilities, picked opportunistically as they fit a phase or a customer need.

---

## 🚀 Go-to-market

### Business model (decided)

- **Per-user perpetual license.** One-time payment, unlimited use afterward. License is bound to a registered user account (registration required).
- **Team licenses.** Bulk licenses for companies at a reduced per-seat price.
- **Device limit: 2 per license.** Usage is capped to 2 activated devices. Enforced via a signature derived from `user + device fingerprint`.

> **Architectural consequence:** a 2-device cap cannot be enforced purely offline — a server must track how many devices a user has activated. The model is therefore **"online activation, offline use"**: the client computes a device fingerprint and activates against a license server, which checks the device count, registers the device, and returns a **cryptographically signed license**. The client ships only the public key, verifies the signature locally, runs offline, and periodically re-validates (e.g. every N days) to honor revocations and plan changes. This drags in: accounts backend + payment gateway + license server + binary signing.

### Phase 1 — Product hardening (current focus)

The product is solid as a personal tool; a paying customer has far less tolerance for rough edges. Goal: make it boringly reliable and self-explanatory before anyone pays for it. (Detailed items live in the thematic backlog below — this is the curated phase checklist.)

- [x] ~~**`--version` flag**~~ — **DONE**. `internal/version` resolves version/commit/date from ldflags (GoReleaser-ready) with a `runtime/debug.ReadBuildInfo` fallback. Closed-source-aware output: `--version` prints a clean one-liner (`departai vX.Y.Z (os/arch)`), while internal metadata (commit, build date, Go toolchain) is gated behind `--version --verbose` for support/diagnostics only.
- [x] ~~**Clear error on missing/old backend CLI**~~ — **DONE**. `agent.CheckCLI` + per-backend `EnsureAvailable()`/`InstallHint`. `cli.Run` checks the configured backends up front (fatal in direct mode, warning in the REPL); `RunTurn` also maps `exec.ErrNotFound` to the actionable install hint as a safety net.
- [x] ~~**Graceful corrupt-task-log recovery**~~ — **DONE**. `Load` runs an integrity check (`logLooksValid`: valid UTF-8 + `# Task Log` header + extractable Original Task). On corruption it backs up the original to `task-log.md.corrupt-<ts>` and rebuilds a usable log (fresh header preserving the extractable prompt + recoverable turn/directive sections verbatim), recording a note in `TaskLog.Recovered` that the orchestrator surfaces as a warning. Never crashes, never proceeds on garbage.
- [x] ~~**Network/API failure retry**~~ — **DONE**. Transient backend failures (rate limit, 429/529, 5xx, network blips) retry the whole turn with exponential backoff + jitter; permanent failures (bad model, auth, missing CLI, cancelled context) abort immediately. Configurable via `max_retries` / `--max-retries` / `/config set max-retries` (default 2, 0 disables). `isTransientError` is a pure heuristic over the exit error + stderr; a `sleep` seam keeps the retry loop testable.
- [x] ~~**Stream buffer tuning**~~ — **DONE**. Per-line scanner cap raised 1 MB → 16 MB default, overridable via `DEPARTAI_MAX_STREAM_LINE_MB`. Both backends now check `scanner.Err()` (previously swallowed): on overflow they kill the process (avoiding a pipe-full deadlock) and return an actionable error instead of truncating the turn silently. Shared helpers `agent.StreamBufferBytes`/`StreamReadError`.
- [ ] **Context-window awareness** — warn before an agent hits its limit.
- [ ] **Orchestrator + codex package tests + E2E smoke** — the test confidence needed to ship and refactor safely.
- [ ] **First-run onboarding** — detect installed backends, guide the user through config, fail clearly if nothing is available.
- [ ] **`--verbose` / `--debug` flag** — dump full prompts; essential for diagnosing customer issues.

### Phase 2 — Distribution & packaging (signed binaries)

Selling to third parties means they can't `go build` from a public repo. They download a signed artifact.

- [ ] **Private release pipeline** — GitHub Actions builds tagged releases; artifacts are NOT public `go install`. Decide hosting (private GH releases, own download server, gated CDN).
- [ ] **GoReleaser** — reproducible cross-platform binaries (macOS arm64/amd64, Linux, Windows).
- [ ] **macOS code signing + notarization** — without this, Gatekeeper blocks the app on customer machines. Requires an Apple Developer account + signing cert in CI.
- [ ] **Windows code signing** — Authenticode cert to avoid SmartScreen warnings (if Windows is a target).
- [ ] **Auto-update mechanism** — check for + install updates, since there's no Homebrew/package-manager channel for private software.
- [ ] **Versioning & changelog discipline** — semver, signed tags, customer-facing release notes.

### Phase 3 — Licensing & commercial backend

Lives in a **separate `departai-web` project** (not this repo): backend (accounts, payments, license server), landing page, pricing + checkout, public documentation, and user dashboards (devices + license management). This repo (the CLI) only ships the client-side license verification that talks to that backend.

The "online activation, offline use" machinery described above.

- [ ] **Accounts backend** — user registration, auth, license records, team/seat management.
- [ ] **Payment integration** — one-time purchase + team purchases (Stripe or similar); webhooks issue licenses.
- [ ] **License server** — issues signed licenses, tracks device activations, enforces the 2-device cap, handles deactivation/transfer, supports revocation.
- [ ] **Device fingerprinting** — stable `user + device` signature that survives reboots but distinguishes machines. Decide inputs (hardware IDs, OS install ID) and privacy posture.
- [ ] **Client-side license verification** — embed public key, verify signed license offline, periodic re-validation, grace period for offline use, clear UX when over the device limit or expired.
- [ ] **Team admin** — seat assignment, member management, reduced bulk pricing tiers.
- [ ] **Trial / activation flow** — how a new user goes from download → register → pay → activate, ideally without friction.

### Phase 4 — Legal, support & GTM polish

Mostly delivered through **`departai-web`** (landing, pricing, docs site, support) — listed here for completeness.

- [ ] **EULA / terms of service** — commercial license terms, liability, acceptable use.
- [ ] **Privacy policy** — especially around device fingerprinting + any telemetry (GDPR if selling in EU).
- [ ] **Opt-in telemetry** — anonymous usage/error reporting with explicit consent, for product decisions + support.
- [ ] **Support channel** — issue intake, docs site, troubleshooting FAQ (see Documentation backlog).
- [ ] **Marketing surface** — landing page, pricing page, demo, purchase funnel (in `departai-web`).
- [ ] **Branding pass** — finalize name/identity; the README still advertises open `go install`, which contradicts the commercial model and must change.

### Open questions / decisions pending

- **Platforms at launch** — macOS only first, or macOS + Linux + Windows? Drives signing + testing scope.
- **Backend CLI dependency** — DepartAI requires the customer to have `claude`/`codex` installed and authenticated (their own LLM subscription). Is that acceptable for the target buyer, or does the product need to abstract/bundle that? Big positioning question.
- **Pricing numbers** — single-license price, team tier discount curve.
- **License server hosting** — managed (Vercel/Fly/Railway) vs self-hosted; cost vs control. Decided in `departai-web`.
- ~~**Where the accounts/payment/license stack lives**~~ — **DECIDED**: a separate `departai-web` project holds backend (accounts, payments, license server), landing, pricing/checkout, docs, and user dashboards. The CLI only ships client-side license verification. `departai-web` needs its own roadmap once Phase 1 is underway.

---

## Feature backlog (general)

The thematic list below predates the commercial pivot. Items feed into the phases above (e.g. testing items → Phase 1, distribution items → Phase 2) or remain optional capabilities picked when they fit a customer need.

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
- [ ] **"Connecting…" indicator before first event** — Claude CLI emits `{"type":"system","subtype":"status","status":"requesting"}` when it starts the LLM call. Surface this as a placeholder line in the TUI viewport so the first 1–3 seconds of a turn aren't a blank screen. Low priority because partial-message streaming already makes the first text/tool block appear quickly; mostly cosmetic for the very initial wait.

## CLI / distribution

- [ ] **Tag `v0.1.0` release** — so `go install github.com/manurgdev/departai@latest` works with a stable version.
- [ ] **GitHub Actions CI** — run `go test`, `go vet`, `go build` on push/PR.
- [ ] **GoReleaser** — pre-built binaries for macOS/Linux/Windows on GitHub releases.
- [ ] **Homebrew formula** — `brew install departai`.
- [ ] **Shell completions** — bash/zsh/fish completions for `--dir`, `--model`, `--backend`, etc.
- [ ] **Man page** — `man departai` with full flag documentation.
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
- [ ] **Context window awareness** — detect when an agent is approaching its context limit and log a warning.
- [x] ~~**Handle network/API failures**~~ — **DONE**. See Phase 1 — transient-error retry with backoff, configurable via `max_retries`.
- [x] ~~**Migrate from `c-bata/go-prompt` to a maintained input library**~~ — **DONE**. Migrated to `knz/bubbline` (bubbletea-based, multi-line editing, persistent history, smart Up/Down at line boundaries). Eliminated the panic class (no zombie goroutines) and added multi-line input wrap.

## Configuration UX

- [ ] **`/config unset <key>`** — symmetric with `/config set` for clearing any key (not just models).
- [ ] **`/config reset`** — restore defaults in session (without touching saved files).
- [ ] **Config file validation** — on load, warn about unknown keys or invalid values instead of silently ignoring.
- [ ] **Env var overrides** — `DEPARTAI_MODEL`, `DEPARTAI_BACKEND`, etc. for scripting.
- [ ] **`spec_preturn_effort` config (Claude only)** — pass `--effort <low|medium|high|xhigh|max>` to the Claude CLI for spec pre-turns. Pre-turns mostly edit `spec.md` rather than reason about new code; reducing the thinking budget (e.g. default `medium`) should cut wall-clock time on `/respec` over long tasks without losing fidelity. Requires extending the agent API with a `RunOptions{Effort string}` so the orchestrator can pass it only on pre-turn calls; main turns stay at the CLI default. Codex has no equivalent flag — ignored on that backend.

## Documentation

- [ ] **Contributing guide** — CONTRIBUTING.md explaining how to add a backend, how tests work, coding conventions.
- [ ] **Example use cases** — README section with 2-3 real workflow examples (e.g. "migrate a schema", "add tests to a module").
- [ ] **Troubleshooting FAQ** — common errors (CLI not installed, model rejected, terminal issues with alt-screen).
