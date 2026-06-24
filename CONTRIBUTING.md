# Contributing to DepartAI

Thanks for your interest in DepartAI! Contributions of all kinds are welcome — bug reports, ideas, docs, and code.

DepartAI is free and open-source (MIT). There's no CLA and no paywalled features; the goal is a focused, reliable, community-driven tool.

## Where to start

- **[ROADMAP.md](ROADMAP.md)** is the map. Check **🎯 Up next** for the near-term plan and **Good first issues** for well-scoped entry points.
- For anything non-trivial, open an issue first so we can agree on the approach before you invest time.

## Prerequisites

- **Go 1.25+** (the version is pinned in [go.mod](go.mod)).
- Optional for full functionality / integration tests: the **`claude`** and/or **`codex`** CLIs, installed and authenticated. Tests that exercise a real CLI **auto-skip** when it isn't installed, so you can develop and run the suite without them.

## Build & run

```bash
go build -o departai .
./departai              # interactive REPL
./departai "a prompt"   # direct mode
```

## Testing & checks

These are exactly what CI runs ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) — please make sure they pass before opening a PR:

```bash
gofmt -l .        # must print nothing (run `gofmt -w .` to fix)
go vet ./...
go test ./... -race
go build ./...
```

### Testing conventions

- **Standard library `testing` only** — no testify or other assertion libraries. Use `if got != want { t.Errorf(...) }`.
- **Table-driven tests** where it fits, with `t.Run(name, ...)` subtests.
- Use **`t.TempDir()`** for filesystem isolation and **`t.Setenv()`** for env/`HOME` (e.g. when touching global config paths).
- **Skip, don't fail, when a real CLI is required**: `if !claudeAvailable() { t.Skip("claude CLI not installed") }`.
- **Separate pure logic from I/O so it's testable.** The codebase favors small pure functions (e.g. `isTransientError`, `contextBudgetExceeded`, `onboardingConfig`) plus injectable seams for the side-effecting parts (e.g. `Orchestrator.runView` swaps the bubbletea TUI for a headless drainer in tests; `Orchestrator.sleep` makes the retry loop instant). Prefer this pattern over spinning up real processes or TUIs in tests.

## Code conventions

- **`gofmt` is mandatory** (CI enforces it) and `go vet` must be clean.
- Idiomatic Go: clear names, small functions, errors wrapped with `%w` and context.
- **Comments explain the *why***, not the *what*. Match the comment density and style of the surrounding code.
- Keep changes focused — one logical change per PR.

## Commit messages

The project uses **[Conventional Commits](https://www.conventionalcommits.org/)**. Prefixes seen in history: `feat:`, `fix:`, `test:`, `docs:`, `chore:`, `ci:`. Example:

```
feat: add per-turn retry on transient backend failures
```

The GoReleaser changelog groups releases by these prefixes, so they matter.

## Adding a new agent backend

Backends live under `internal/agent/<name>/` and implement the `agent.StreamingAgent` interface. To add one (say, `gemini`):

1. **`internal/agent/gemini/gemini.go`** — implement `agent.Agent` (`Name`, `RunTurn`) and the streaming hooks (`SetOnEvent`, `SetOnStreamDone`), plus `ValidateModel(ctx, model)`. Spawn the CLI, scan its output with `agent.StreamBufferBytes()`, and check `scanner.Err()` (wrap with `agent.StreamReadError`).
2. **`internal/agent/gemini/stream.go`** — a `ParseStreamLine` (or stateful parser) converting the CLI's native output into `agent.StreamEvent`s (`text`, `tool`/`tool_start`, `block_end`, `result`).
3. **Availability:** export `BinaryName`, `InstallHint`, and `EnsureAvailable()` (wrap `agent.CheckCLI`).
4. **Wire it up:** add a case in `buildOneAgent()` (`internal/orchestrator/orchestrator.go`), in `ensureBackendsAvailable()` and the model-validation switch (`internal/cli/cli.go`).
5. **Tests:** add `stream_test.go` for the parser and a `*_test.go` for constructors / `ValidateModel` (skipping when the CLI is absent).

See the existing `claude` and `codex` packages as references, and the **Architecture** section of the [README](README.md#architecture) for the bigger picture.

## Pull request process

1. Fork and create a feature branch off `main`.
2. Make your change with tests; ensure the checks above pass.
3. Open a PR describing **what** changed and **why**. Link any related issue.
4. CI must be green. A maintainer will review.

## Code of Conduct

By participating, you agree to abide by the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
