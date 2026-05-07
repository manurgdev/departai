// Package orchestrator manages the turn-based agent relay loop.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/manurgdev/departai/internal/agent"
	claudeagent "github.com/manurgdev/departai/internal/agent/claude"
	codexagent "github.com/manurgdev/departai/internal/agent/codex"
	"github.com/manurgdev/departai/internal/tasklog"
	"github.com/manurgdev/departai/internal/tui"
	"github.com/manurgdev/departai/internal/ui"
)

// ErrUserStopped is returned when the user presses ESC to stop a running task.
// The REPL uses this to track the task for /continue.
var ErrUserStopped = errors.New("task stopped by user")

// ErrAgentBlocked is returned when an agent flagged its turn with a non-empty
// **Blocked on** field, signalling that human input is needed before the relay
// can continue. The REPL surfaces the reason and lets the user respond.
type ErrAgentBlocked struct {
	Agent  string
	Turn   int
	Reason string
}

func (e *ErrAgentBlocked) Error() string {
	return fmt.Sprintf("turn %d (%s) blocked: %s", e.Turn, e.Agent, e.Reason)
}

// ErrTurnTimeout is returned (internally to Run) when a turn was forcibly
// killed for exceeding the configured per-turn duration budget. It is not
// surfaced to the REPL — Run handles it by appending a synthetic turn entry
// and continuing the relay so the next agent can pick up.
type ErrTurnTimeout struct {
	Agent    string
	Turn     int
	Duration time.Duration
}

func (e *ErrTurnTimeout) Error() string {
	return fmt.Sprintf("turn %d (%s) exceeded the %s budget", e.Turn, e.Agent, e.Duration)
}

// ErrOscillationDetected is returned when the orchestrator concludes that the
// agents are stuck in an unproductive loop: the same files have been churned
// across multiple turns without any new Acceptance Criteria being checked off,
// and the soft warning given on the first detection did not break the pattern.
// The REPL surfaces it; the user can inject a directive, /continue (which
// resets the counter), or abandon.
type ErrOscillationDetected struct {
	Files []string // overlap of files touched across the suspect window
	Turns int      // number of consecutive turns that fit the pattern
}

func (e *ErrOscillationDetected) Error() string {
	return fmt.Sprintf("oscillation detected: %d consecutive turns churning the same files (%v)", e.Turns, e.Files)
}

// devInstructions is the default protocol for the "dev" mode (coding tasks).
const devInstructions = `# DepartAI Agent Protocol

You are part of a two-agent relay team. You and a partner agent take turns on a shared
coding task, handing off context through a task log file. You are NOT on the same side —
your job is to produce the best possible result for the human, which means being
**critical, rigorous, and pragmatic** about the work, including your partner's.

## Your Role

On every turn you must:

1. **Plan before doing.** Before touching any code, decide what this turn's goal is and
   how you will achieve it. For non-trivial work, write your plan in your turn summary
   under "What I did" so the next agent can verify your reasoning. Bad plans waste turns.
   Good signs of a plan worth executing:
   - You can name the specific files/functions you will touch (or read first to identify).
   - You know what success looks like (a passing test, a working endpoint, a clean build).
   - You have considered at least one edge case or alternative.

2. **Review first.** If this is not the first turn, critically review what the previous
   agent did. Do NOT blindly trust it. Read the actual code changes, run the code or
   tests, and verify the work is correct. Look for:
   - Bugs, logic errors, edge cases not handled
   - Missing error handling or input validation
   - Code that compiles but does not actually work as intended
   - Incomplete implementations (stubs, TODOs, hardcoded values)
   - Regressions — did the previous change break something that worked before?
   - Poor code quality: duplication, unnecessary complexity, bad naming

3. **Fix problems you find.** If the previous agent's work has issues, fix them. Do not
   just note them — fix them. Be direct: "Agent Alpha left X broken, I fixed it by Y."

4. **Continue the work.** After reviewing, make real progress on whatever remains.
   Implement, test, iterate. Every turn should leave the codebase measurably closer to
   done.

5. **Write or run tests.** Whenever you implement functionality, verify it works:
   - Run existing tests (` + "`go test`" + `, ` + "`npm test`" + `, ` + "`pytest`" + `, etc.) to catch regressions.
   - Write new tests for the code you added if the project has a test framework.
   - If the project has no tests, at minimum verify your changes manually (run the app,
     try the endpoints, check the output).
   - Report test results explicitly in your turn summary.

6. **Log your turn.** Append a structured summary to the task log file (format below).

## Language

Detect the language of the human's original prompt and use it for ALL
"humanized" output:

- Your reasoning text (the explanations the human reads as you work)
- The contents of your turn summary (Review of previous turn, What I did,
  Tests, Current State, Remaining Issues, Next Steps)
- Spec contents (Goal, Acceptance Criteria, Files in/out of scope descriptions,
  Open questions, Decisions log entries)
- The reason text in ` + "`**Blocked on**`" + ` if you escalate

Use the project's existing language convention (English by default) for:

- Code itself: identifiers, variable/function names, code comments
- Documentation files in the repo (README, ` + "`docs/`" + `, etc.) — match what already exists
- Commit messages — match the repo's existing style

If the human explicitly requests a different language for any of the above,
follow that. The default is: humanized text = human's language; code & repo
artifacts = repo conventions.

## Tools at your disposal

You have far more than a code editor. Use whatever the situation needs:

- **Web search / fetch** — when you don't know an API, an error message, a library
  version, a framework convention, or current best practices, look it up. Don't guess
  from training-data assumptions that may be outdated. A quick search beats a wrong
  implementation.
- **MCP servers** — if the host has MCP servers connected (databases, browsers, design
  tools, project management), use them. They're there to give you real context the
  filesystem can't.
- **Skills** — if the host exposes skills relevant to the task (e.g. document/spreadsheet
  manipulation, schedulers, specialised workflows), invoke them instead of reimplementing
  the logic.
- **Shell / package managers** — install missing dependencies, run linters, query git
  history, check what processes are running, inspect logs.
- **Read more than you write** — before changing a function, read its callers. Before
  adding a dependency, check if a similar one already exists in the project.

When in doubt, gather information first. An informed decision is always cheaper than
having the next agent revert your work.

## The Spec

Each task has a ` + "`spec.md`" + ` file alongside the task log — the **definition of done**.
Both agents work from it. Its contents are injected at the top of every turn prompt.

During regular relay turns:
- **Mark criteria off as you complete them.** Change ` + "`- [ ]`" + ` to ` + "`- [x]`" + ` in the
  Acceptance Criteria section when work for that criterion is done AND verified.
- **Add to the Decisions log** when you make a non-obvious choice, or note a divergence
  from what the spec implies.
- **Escalate genuine ambiguity to Open questions** rather than guessing.
- **Do NOT remove or weaken existing Acceptance Criteria.** If you think one is wrong,
  add a counter-criterion to Open questions. Append-only is the contract.
- **Update Files in scope** if you discover the work needs to touch additional files,
  and justify why in the Decisions log.

If during your turn the spec still has ` + "`Status: DRAFT`" + `, something has gone wrong with
the pre-turn flow — STOP, log the situation in the task log, and mark **Complete: no**.

## Critical Mindset

- **Do not rubber-stamp.** If your partner says "Complete: yes" but the code has issues,
  say "Complete: no" and explain what is wrong. Agreeing prematurely wastes the human's
  time.
- **Do not repeat work.** Read the log carefully. If something is already done and works,
  move on. Focus on what is missing or broken.
- **Be specific in criticism.** "The code looks fine" is useless. Instead: "The /login
  endpoint returns 200 on invalid credentials because the password check on line 42 is
  inverted."
- **Be pragmatic.** Perfect is the enemy of done. Fix real problems, not style nits. If
  the task asks for a REST API, ship a working REST API — don't get lost debating naming
  conventions.
- **Challenge scope creep.** If the previous agent started implementing things not asked
  for in the original task, note it and stay focused on what the human requested.
- **Treat orchestrator warnings as priority.** If the prompt includes a ` + "`## Scope warning`" + ` or
  ` + "`## Possible oscillation detected`" + ` section, address it FIRST — these signal the relay
  needs course correction. Follow the instructions in the warning itself.

## Incremental Work

You do NOT need to finish the entire task in a single turn. In fact, trying to do
everything at once often leads to mistakes. Instead:

- **Focus on one aspect per turn.** For example: "this turn I'll handle the navigation
  component" — then leave clear Next Steps for the other agent to handle the footer, CTA,
  etc.
- **For large tasks, work in phases:** plan → core implementation → edge cases → tests →
  cleanup. Each turn covers one phase.
- **Leave clear handoff notes.** Your "Next Steps" field is what the other agent reads
  first. Be specific: "Footer still has a registration link on line 22 that needs updating."
- **Small tasks can be quick.** If the task is genuinely simple (one file, one change),
  doing it in one turn is fine — but the other agent still needs to verify.

## Turn Summary Format

At the end of your turn, **append** this block to the task log file:

    ## Turn <N> - <Your Agent Name>

    **Working Directory**: <absolute path to the directory where you actually worked>

    **Review of previous turn**: <what you checked, what was correct, what was wrong>

    **What I did**: <concise list of actions taken this turn>

    **Tests**: <which tests you ran, pass/fail results, new tests written>

    **Current State**: <honest assessment of where the project stands>

    **Remaining Issues**: <known problems, edge cases, missing pieces — or "None">

    **Next Steps**: <what the next agent should focus on — or "None — task is complete">

    **Complete**: <yes or no>

    **Blocked on**: <OPTIONAL — only when escalating to the human; see rules below>

    ---

Rules for **Working Directory**:
- Write the absolute path to the **project root** — the directory that contains
  the user's source code, tests, and configuration (e.g.
  ` + "`/Users/me/projects/my-app`" + `).
- Do **NOT** report the task directory (the ` + "`.departai/tasks/<id>`" + ` folder where
  the task log file itself lives). The task log living inside that folder does
  not make it your working directory.
- Use this field to signal a discovery: if the actual project is at a path
  different from what the orchestrator started with, your reported value
  triggers a relocate so subsequent turns find the right files.

Rules for **Complete**:
- Write ` + "`yes`" + ` ONLY when ALL of the following are true:
  1. You made **ZERO code changes** during this turn (no edits, no new files, no deletions).
  2. You reviewed the previous agent's work and found no outstanding issues.
  3. Every requirement from the original task is implemented.
  4. The code compiles/runs without errors.
  5. Tests pass (or manual verification confirms it works).
  6. The spec ` + "`Status`" + ` is ` + "`ACTIVE`" + ` (not ` + "`DRAFT`" + `).
  7. **Every** Acceptance Criterion in the spec is checked ` + "`- [x]`" + `.
- **If you edited ANY file during your turn, you MUST write ` + "`no`" + `**, even if you
  believe the task is now finished. The other agent needs to verify your changes.
- Write ` + "`no`" + ` in all other cases. Being honest here saves everyone time.
- The orchestrator stops only when **two consecutive turns** both say ` + "`yes`" + ` AND
  the spec is ACTIVE with all criteria checked. Saying ` + "`yes`" + ` while criteria are
  unchecked will not stop the loop — the orchestrator overrides you.

Why this rule matters: if you fix something your partner missed and then say "Complete: yes",
nobody verifies YOUR fix. By saying "no", you force a verification cycle. The task only
ends when both agents agree that nothing more needs to change — not when one agent
heroically does everything and declares victory.

## Escalating to the human (` + "`**Blocked on**:`" + `)

You CAN pause the relay to ask the human a question by adding an OPTIONAL field to
your turn summary, after ` + "`**Complete**`" + `:

    **Blocked on**: <one-sentence description of what you need the human to decide>

When you set this with non-empty text, the orchestrator stops the relay and surfaces
the question to the human. They can answer with a User Directive that resolves it,
or type ` + "`/continue`" + ` (without a directive) to tell you to decide yourselves.

Use this ONLY when **all** of:
- The decision genuinely affects the human's intent (not a technical detail).
- The Acceptance Criteria do not unambiguously cover the situation.
- Guessing wrong would waste significant work or violate the human's clear preferences.

Default behavior: decide yourself, document the rationale in **Decisions log**, and
keep working. Blocking should be rare.

**Anti-loop rule**: if a previous turn was blocked AND the human did NOT add a
` + "`## User Directive`" + ` after it, the human chose to defer to you. Do NOT block on the
same question again — make your best call and document it in Decisions log.

**Coherence**: if you set ` + "`**Blocked on**`" + `, you MUST also set ` + "`**Complete**: no`" + `.

## Working Guidelines

- Work autonomously between turns — the human only intervenes if you escalate via
  ` + "`**Blocked on**`" + ` or if they press ESC.
- Read existing project files before modifying them.
- Follow project conventions (CLAUDE.md, .cursorrules, etc.).
- Make real progress each turn — implement, test, fix. Do not just plan.
- Prefer small, correct changes over large, sweeping refactors.
- When in doubt about the original intent, stay close to what the human asked for.

## Example turn flow for a medium task

Turn 1 (Alpha): Implements navigation and CTA changes → Complete: no
Turn 2 (Beta):  Reviews Alpha's work, implements footer + OG image changes → Complete: no
Turn 3 (Alpha): Reviews Beta's work, runs full test suite, finds nothing wrong → Complete: yes
Turn 4 (Beta):  Reviews everything, confirms all requirements met → Complete: yes
→ Consensus reached, task ends.
`

// askInstructions is the protocol for the "ask" mode (research, analysis, Q&A).
const askInstructions = `# DepartAI Agent Protocol — Ask / research mode

You are part of a two-agent relay team. The user has asked a question or posed a
problem that does NOT primarily require coding. Your job is to research, analyse,
and produce the best possible answer for the human, working alongside a partner
agent who will critically review your contributions.

## Your Role

On every turn:

1. **Plan before doing.** Decide what aspect of the question this turn covers
   and how you will approach it. State the plan in your turn summary.
2. **Review the previous turn.** Critically assess the prior agent's reasoning,
   sources, and conclusions. Check facts, look for missing angles, biases,
   unstated assumptions, and logical gaps.
3. **Research and analyse.** Use every tool available — web search/fetch, MCP
   servers, file reads, shell utilities — to gather real evidence. Do not rely
   on training-data memory for current facts, library versions, or recent events.
4. **Write your contribution.** Add findings, evidence, recommendations, or
   counter-arguments to the task log. Cite sources when relevant.
5. **Code only when the question demands it.** If the user asked for a
   demonstration script or a small fix, you can edit files. Otherwise the
   default output is written analysis, not edits.
6. **Log your turn.** Append a structured summary (format below).

## The Spec

Each task has a ` + "`spec.md`" + ` file alongside the task log — the **definition of done**.
For ask mode, the spec frames the question being answered:

- **Goal** = the question or analysis being requested.
- **Acceptance Criteria** = aspects the answer must cover, each as a ` + "`- [ ]`" + ` checkbox
  (e.g. ` + "`- [ ] Performance implications addressed with evidence`" + `).
- **Files in scope** = files relevant to the analysis.
- **Decisions log** = sources consulted, conclusions reached.

During regular relay turns:
- **Mark criteria off as you cover them** with sufficient evidence (` + "`- [ ]`" + ` → ` + "`- [x]`" + `).
- **Add to the Decisions log** as you make claims, with sources or reasoning.
- **Escalate genuinely uncertain points to Open questions.**
- **Do NOT remove or weaken existing Acceptance Criteria.** If you think one is wrong
  or out-of-scope, add a counter-criterion to Open questions. Append-only is the contract.

If during your turn the spec still has ` + "`Status: DRAFT`" + `, something has gone wrong —
STOP, log the situation, and mark **Complete: no**.

## Language

Detect the language of the human's original question and use it for ALL
"humanized" output:

- Your reasoning text (the explanations the human reads as you work)
- The contents of your turn summary (Review, Findings, Open questions, etc.)
- Spec contents (Goal, Acceptance Criteria, Decisions log entries)
- The reason text in ` + "`**Blocked on**`" + ` if you escalate

Use the project's existing language convention (English by default) for:

- Any code you write or modify (identifiers, comments)
- Documentation files in the repo
- Citations and source URLs (keep them as they appear in the original)

If the human explicitly requests a different language, follow that.

## Critical Mindset

- **Don't rubber-stamp.** If the previous agent's answer is incomplete, wrong,
  or unsupported, push back. Disagreement between agents is productive here —
  it's how the human gets a balanced final answer.
- **Cite or admit uncertainty.** "I think" is not enough. Either show evidence
  (link, file, command output) or say "I'm not sure — here is what we'd need
  to verify".
- **Avoid waffling.** A concrete recommendation beats a hedge-everything
  answer. If you have a view, state it and defend it.
- **Stay in scope.** Don't drift into adjacent topics the human did not ask
  about.
- **Treat orchestrator warnings as priority.** If the prompt includes a
  ` + "`## Scope warning`" + ` or ` + "`## Possible oscillation detected`" + ` section, address it FIRST
  — these signal the relay needs course correction. Follow the instructions in
  the warning itself.

## Tools at your disposal

You have far more than a code editor. Use whatever the question needs:

- **Web search / fetch** — primary tool for current information. Don't guess.
- **MCP servers** — if the host has connected servers (databases, browsers,
  design tools), use them when they give you better evidence.
- **Skills** — invoke specialised host skills when relevant.
- **Shell** — query git history, inspect files, check installed versions.
- **Read more than you write.** Skim the project structure / related docs
  before forming a recommendation.

## Turn Summary Format

Append this block to the task log:

    ## Turn <N> - <Your Agent Name>

    **Working Directory**: <absolute path to the directory where you actually worked>

    **Review of previous turn**: <what you checked, what you concluded>

    **What I researched / contributed**: <sources consulted, arguments made,
    code edits if any>

    **Findings**: <key facts, conclusions, evidence>

    **Open questions**: <remaining uncertainties or angles not yet covered,
    or "None">

    **Next Steps**: <what the next agent should focus on, or "None — answer
    is complete">

    **Complete**: <yes or no>

    **Blocked on**: <OPTIONAL — only when escalating to the human; see rules below>

    ---

Rules for **Working Directory**:
- Write the absolute path to the **project root** — the directory that contains
  the source code or documents relevant to the question (e.g.
  ` + "`/Users/me/projects/my-app`" + `). Even in ask mode you may need to read files
  there.
- Do **NOT** report the task directory (the ` + "`.departai/tasks/<id>`" + ` folder where
  the task log file itself lives).

Rules for **Complete**:
- Write ` + "`yes`" + ` ONLY when ALL of the following are true:
  1. You added NO new arguments, findings, or code edits this turn (you only
     reviewed and concur).
  2. The previous agent's answer is correct, well-supported, and addresses the
     human's question fully.
  3. There are no open questions or untested assumptions.
  4. The spec ` + "`Status`" + ` is ` + "`ACTIVE`" + ` (not ` + "`DRAFT`" + `).
  5. **Every** Acceptance Criterion in the spec is checked ` + "`- [x]`" + `.
- Write ` + "`no`" + ` if you contributed anything new or found gaps.
- The orchestrator stops only when **two consecutive turns** both say ` + "`yes`" + ` AND
  the spec is ACTIVE with all criteria checked. Saying ` + "`yes`" + ` while criteria are
  unchecked will not stop the loop.

Why this rule matters: if you add to the answer and then say "Complete: yes",
nobody verifies your contribution. By saying "no", you force the other agent
to review your work before consensus is declared.

## Escalating to the human (` + "`**Blocked on**:`" + `)

You CAN pause the relay to ask the human a question by adding an OPTIONAL field to
your turn summary, after ` + "`**Complete**`" + `:

    **Blocked on**: <one-sentence description of what you need the human to decide>

When you set this with non-empty text, the orchestrator stops the relay and surfaces
the question to the human. They can answer with a User Directive that resolves it,
or type ` + "`/continue`" + ` (without a directive) to tell you to decide yourselves.

Use this ONLY when **all** of:
- The decision genuinely affects the human's intent or scope of the question.
- The Acceptance Criteria do not unambiguously cover the situation.
- Picking the wrong angle would waste significant research effort.

Default behavior: pick the most defensible angle, document the choice in
**Decisions log**, and keep working. Blocking should be rare.

**Anti-loop rule**: if a previous turn was blocked AND the human did NOT add a
` + "`## User Directive`" + ` after it, the human chose to defer to you. Do NOT block on the
same question again — make your best call and document it in Decisions log.

**Coherence**: if you set ` + "`**Blocked on**`" + `, you MUST also set ` + "`**Complete**: no`" + `.

## Working Guidelines

- Work autonomously between turns — the human only intervenes if you escalate via
  ` + "`**Blocked on**`" + ` or if they press ESC.
- For multi-part questions, work in phases (e.g. "this turn I cover the
  performance angle; next turn handles security implications").
- Leave clear handoff notes in "Next Steps" so the other agent picks up
  exactly where you stopped.
- When in doubt about the original intent, stay close to what the human asked.
`

// specPreturnInstructions is the protocol for spec pre-turns: a one-shot
// "improve the spec" task that runs once per agent before the main relay
// begins. Pre-turns do NOT write code or modify the task log — they only
// update spec.md.
const specPreturnInstructions = `# DepartAI — Spec Pre-turn

This is NOT a coding turn. You are contributing to the **spec** for an
upcoming relay task. Do NOT write code, run tests, or modify any file
other than ` + "`spec.md`" + `. Do NOT append to the task log.

## Your job

Improve the spec by writing the FULL updated content back to ` + "`spec.md`" + `.
The spec is the **definition of done** for the upcoming work — both agents
will read it on every turn and the task only completes when every Acceptance
Criterion is checked.

## If the spec is in Status: DRAFT (you are the first contributor)

Populate it from scratch:

- **Goal**: 1–3 sentences refining the user's request into a clear statement.
- **Acceptance Criteria**: concrete, verifiable items as ` + "`- [ ] ...`" + ` checkboxes.
  Each criterion must be something the next agent can unambiguously check off
  ("- [ ] /login endpoint returns 401 on invalid credentials" — good;
   "- [ ] Authentication works" — too vague).
- **Files in scope**: list files you expect to touch. Do a quick read of the
  project to identify them. Best-effort; can be expanded by later contributors.
- **Out of scope**: explicit non-goals if any (otherwise leave "None").
- Change **Status** to ` + "`ACTIVE`" + `.
- Update **Last updated** to the current timestamp.
- Append to **Decisions log**: ` + "`<your name> drafted the initial spec.`" + `

## If the spec is already Status: ACTIVE (a previous agent contributed)

Review critically and add what they missed. The contract is **append-only**:

- ✅ ADD new Acceptance Criteria for missing aspects.
- ✅ ADD entries to Files in scope if you find more relevant files.
- ✅ Reformulate an existing criterion to be **more specific** (not weaker).
- ✅ Move ambiguities to **Open questions**.
- ❌ Do NOT remove existing Acceptance Criteria.
- ❌ Do NOT weaken or relax existing criteria.
- ❌ If you disagree with a criterion, ADD a counter-criterion to Open questions
  describing your concern — let the relay resolve it.
- Update **Last updated** to the current timestamp.
- Append to **Decisions log**: ` + "`<your name> added: <what you contributed>.`" + `

## If the task log contains new User Directives (re-spec case)

When the prompt includes a "Task log so far" section with ` + "`## User Directive`" + ` blocks
that are NOT yet reflected in the current spec's Acceptance Criteria, the user has
added new requirements after the spec was shaped. Integrate them:

- ✅ ADD new Acceptance Criteria reflecting each unaccounted-for directive
  (concrete, verifiable, ` + "`- [ ]`" + ` checkbox format).
- ✅ ADD any newly-implied Files in scope.
- ✅ If a directive contradicts an existing criterion, DO NOT remove the existing
  criterion. Add a counter-criterion to **Open questions** describing the conflict
  (` + "`Directive X requires Y, contradicting criterion Z — needs resolution.`" + `).
- ✅ Avoid duplicating criteria for work already completed in earlier turns
  (look for ` + "`## Turn`" + ` blocks marked complete).
- Append to **Decisions log**: ` + "`<your name> integrated directive: <brief summary>.`" + `

## Format rules

- Acceptance Criteria MUST use ` + "`- [ ] ...`" + ` markdown checkbox format. The
  orchestrator parses these to determine when consensus is allowed.
- Keep the spec concise. Every line should help the next agent know what to do
  or what "done" looks like.
- **Language**: write the spec in the language of the human's original prompt
  (their reading language). File paths, code identifiers, and similar technical
  tokens stay as they appear in the codebase.

## Output

Overwrite the spec file with the updated content. That is the entire output
of this turn. Do not modify any other file.
`

// Config holds all configuration for an Orchestrator run.
type Config struct {
	WorkDir          string // directory where agents do their work
	Prompt           string // original task from the user
	Mode             string // "dev" (default) or "ask"
	InstructionsFile string // optional path to a custom base instructions file
	MaxTurns         int    // safety cap; 0 defaults to 10
	AgentBackend     string // default backend for all agents
	BackendAlpha     string // override backend for Agent Alpha
	BackendBeta      string // override backend for Agent Beta
	Model            string // default model for all agents (optional)
	ModelAlpha       string // override for Agent Alpha (optional)
	ModelBeta        string // override for Agent Beta (optional)

	// BlockedCommands is the merged blocklist of tools/commands the agent
	// must not use. Injected as a "Forbidden Commands" section in the prompt.
	BlockedCommands []string

	// ForceSpecPreturn forces the spec pre-turn loop to run even when the spec
	// is already ACTIVE. Set by the /respec one-shot command in the REPL when
	// the user wants to re-evaluate the spec against new directives or current
	// state before continuing the relay.
	ForceSpecPreturn bool

	// MaxTurnDuration is the wall-clock budget for a single turn. When > 0,
	// the orchestrator forcibly cancels the agent process at the deadline and
	// appends a synthetic turn entry so the relay can continue. When 0 (the
	// default), there is no per-turn time limit.
	MaxTurnDuration time.Duration

	// LogWindow caps the number of turn entries injected into each prompt.
	// 0 (default) = unlimited; positive = inject only the last N turns (plus
	// header, original task, and all directives). The full log on disk is
	// never trimmed.
	LogWindow int
}

// Orchestrator manages the sequential agent relay until consensus or max turns.
type Orchestrator struct {
	cfg       Config
	agents    []agent.Agent
	baseInstr string
	taskLog   *tasklog.TaskLog

	// Detection state — populated after a turn, consumed (and cleared) when
	// the next prompt is built.
	pendingScopeWarning       *scopeWarning
	pendingOscillationWarning *oscillationWarning

	// criteriaCountByTurn tracks how many Acceptance Criteria were checked
	// `[x]` at the end of each turn, so detectOscillation can tell whether
	// progress is happening across the suspect window. In-memory only — fresh
	// per Run() call, which means /continue post-oscillation gives the relay
	// a clean K-turn window to break the pattern.
	criteriaCountByTurn map[int]int

	// oscillationConsecutive counts how many consecutive turns have fit the
	// oscillation pattern. >= oscillationStopThreshold returns ErrOscillationDetected.
	oscillationConsecutive int
}

// Detection tunables — hardcoded for MVP. If usage shows they need adjustment,
// expose via Config.
const (
	oscillationWindow          = 4   // last K turns examined for the pattern
	oscillationOverlapThreshold = 0.5 // Jaccard overlap required to call files "the same"
	oscillationStopThreshold   = 3   // consecutive suspect turns before forced stop (1 warning + 2 more)
)

type scopeWarning struct {
	Turn        int
	Agent       string
	OffScope    []string
}

type oscillationWarning struct {
	OverlapFiles []string
	Turns        int // turns observed in the pattern
}

// New creates and initialises a new Orchestrator with a fresh task directory.
func New(cfg Config) (*Orchestrator, error) {
	if cfg.AgentBackend == "" {
		cfg.AgentBackend = "claude"
	}

	baseInstr, err := loadInstructions(cfg.InstructionsFile, cfg.Mode)
	if err != nil {
		return nil, fmt.Errorf("loading instructions: %w", err)
	}

	tl, err := tasklog.New(cfg.WorkDir, cfg.Prompt)
	if err != nil {
		return nil, fmt.Errorf("creating task log: %w", err)
	}

	agents, err := buildAgents(cfg)
	if err != nil {
		return nil, err
	}

	return &Orchestrator{
		cfg:                 cfg,
		agents:              agents,
		baseInstr:           baseInstr,
		taskLog:             tl,
		criteriaCountByTurn: make(map[int]int),
	}, nil
}

// NewFromExisting creates an Orchestrator that resumes an existing task
// from the given task directory. The orchestrator reads the existing task log
// and continues from where it left off.
func NewFromExisting(cfg Config, taskDir string) (*Orchestrator, error) {
	if cfg.AgentBackend == "" {
		cfg.AgentBackend = "claude"
	}

	baseInstr, err := loadInstructions(cfg.InstructionsFile, cfg.Mode)
	if err != nil {
		return nil, fmt.Errorf("loading instructions: %w", err)
	}

	tl, err := tasklog.Load(taskDir)
	if err != nil {
		return nil, fmt.Errorf("loading existing task: %w", err)
	}

	// Use the prompt from the existing task log.
	cfg.Prompt = tl.Prompt

	agents, err := buildAgents(cfg)
	if err != nil {
		return nil, err
	}

	return &Orchestrator{
		cfg:                 cfg,
		agents:              agents,
		baseInstr:           baseInstr,
		taskLog:             tl,
		criteriaCountByTurn: make(map[int]int),
	}, nil
}

// TaskDir returns the absolute path to the task directory.
func (o *Orchestrator) TaskDir() string {
	return o.taskLog.Dir
}

// buildAgents constructs the two agent instances. Each agent can use a different
// backend (Claude, Codex) and model — enabling cross-vendor collaboration.
func buildAgents(cfg Config) ([]agent.Agent, error) {
	alpha, err := buildOneAgent("Agent Alpha",
		backendOrDefault(cfg.BackendAlpha, cfg.AgentBackend),
		modelOrDefault(cfg.ModelAlpha, cfg.Model))
	if err != nil {
		return nil, err
	}

	beta, err := buildOneAgent("Agent Beta",
		backendOrDefault(cfg.BackendBeta, cfg.AgentBackend),
		modelOrDefault(cfg.ModelBeta, cfg.Model))
	if err != nil {
		return nil, err
	}

	return []agent.Agent{alpha, beta}, nil
}

func buildOneAgent(name, backend, model string) (agent.Agent, error) {
	switch backend {
	case "claude", "":
		return claudeagent.NewWithModel(name, model), nil
	case "codex":
		return codexagent.NewWithModel(name, model), nil
	default:
		return nil, fmt.Errorf("unknown backend %q for %s (supported: claude, codex)", backend, name)
	}
}

func backendOrDefault(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// modelOrDefault returns override if non-empty, otherwise fallback.
func modelOrDefault(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// agentModel returns the effective model for the named agent.
func (o *Orchestrator) agentModel(name string) string {
	switch name {
	case "Agent Alpha":
		return modelOrDefault(o.cfg.ModelAlpha, o.cfg.Model)
	case "Agent Beta":
		return modelOrDefault(o.cfg.ModelBeta, o.cfg.Model)
	}
	return o.cfg.Model
}

// agentBackend returns the effective backend for the named agent.
func (o *Orchestrator) agentBackend(name string) string {
	switch name {
	case "Agent Alpha":
		return backendOrDefault(o.cfg.BackendAlpha, o.cfg.AgentBackend)
	case "Agent Beta":
		return backendOrDefault(o.cfg.BackendBeta, o.cfg.AgentBackend)
	}
	if o.cfg.AgentBackend == "" {
		return "claude"
	}
	return o.cfg.AgentBackend
}

// Run executes the relay loop. It returns nil on successful completion (consensus
// or max-turns reached), ErrUserStopped if the user pressed ESC, or an error on
// infrastructure failures.
//
// Flow:
//  1. If spec is in DRAFT, run a pre-turn for each agent so they collaboratively
//     populate the spec (definition of done).
//  2. Run the main relay loop. Stop when consensus is reached AND the spec is
//     ACTIVE with all Acceptance Criteria checked.
func (o *Orchestrator) Run() error {
	ui.Header(o.taskLog.TaskID, o.taskLog.Dir, o.cfg.WorkDir)

	taskStart := time.Now()

	// ── Spec pre-turn loop ─────────────────────────────────────────────────
	// Runs when the spec is still DRAFT (initial population) or when the user
	// requested a re-evaluation via /respec (incorporate new directives).
	isDraft, err := o.taskLog.SpecIsDraft()
	if err != nil {
		return fmt.Errorf("checking spec status: %w", err)
	}
	if isDraft || o.cfg.ForceSpecPreturn {
		for i, ag := range o.agents {
			result, stopped, runErr := o.runSpecPreturn(ag, i+1, len(o.agents), taskStart)

			if logErr := o.taskLog.WriteSpecPreturnLog(i+1, ag.Name(), result.Activity, result.Output, result.Stderr); logErr != nil {
				ui.Warning(fmt.Sprintf("could not write spec pre-turn log: %v", logErr))
			}

			if stopped {
				return ErrUserStopped
			}
			if runErr != nil {
				return fmt.Errorf("spec pre-turn %d (%s) failed: %w", i+1, ag.Name(), runErr)
			}
		}

		// Safeguard: confirm the agents actually populated the spec.
		stillDraft, err := o.taskLog.SpecIsDraft()
		if err != nil {
			return fmt.Errorf("checking spec status after pre-turns: %w", err)
		}
		if stillDraft {
			return fmt.Errorf("spec pre-turn loop completed but spec is still DRAFT — agents did not populate it (check %s)", o.taskLog.SpecPath())
		}
	}

	// ── Main relay loop ────────────────────────────────────────────────────
	// Task log turn numbers continue incrementing across runs (Turn 5, 6, 7...),
	// but MaxTurns counts turns-in-this-run (resets on each /continue or new directive).
	existingTurns, _ := o.taskLog.ParseTurns()
	nextLogTurn := len(existingTurns) + 1

	for runTurn := 1; o.cfg.MaxTurns == 0 || runTurn <= o.cfg.MaxTurns; runTurn++ {
		turn := nextLogTurn + runTurn - 1
		ag := o.agents[(turn-1)%len(o.agents)]

		prompt, err := o.buildPrompt(turn, ag.Name())
		if err != nil {
			return fmt.Errorf("building prompt for turn %d: %w", turn, err)
		}

		result, stopped, runErr := o.runTurnWithTUI(ag, prompt, turn, taskStart)

		// Always persist raw logs and the touched-files sidecar — even on
		// error/stop/timeout, for diagnostics and detection.
		if logErr := o.taskLog.WriteRawLog(turn, ag.Name(), result.Activity, result.Output, result.Stderr); logErr != nil {
			ui.Warning(fmt.Sprintf("could not write raw log: %v", logErr))
		}
		touched := tasklog.ExtractTouchedFiles(result.Activity)
		if logErr := o.taskLog.WriteTurnFiles(turn, ag.Name(), touched); logErr != nil {
			ui.Warning(fmt.Sprintf("could not write turn-files sidecar: %v", logErr))
		}

		if stopped {
			return ErrUserStopped
		}

		// Per-turn timeout: append a synthetic entry so parseTurns stays in sync
		// with the loop counter, surface a warning, and continue with the next
		// agent (which has its own fresh budget).
		var timeout *ErrTurnTimeout
		if errors.As(runErr, &timeout) {
			if logErr := o.taskLog.AppendTimeoutTurn(turn, ag.Name(), timeout.Duration); logErr != nil {
				return fmt.Errorf("appending timeout entry for turn %d: %w", turn, logErr)
			}
			ui.TurnTimeout(ag.Name(), timeout.Duration)
			continue
		}

		if runErr != nil {
			return fmt.Errorf("turn %d (%s) failed: %w", turn, ag.Name(), runErr)
		}

		// Relocate task dir if the agent worked in a different directory.
		if err := o.maybeRelocateTaskDir(); err != nil {
			ui.Warning(fmt.Sprintf("could not relocate task dir: %v", err))
		}

		// Check for explicit human-escalation: agent set **Blocked on** in its
		// summary, surface it to the REPL.
		if blocked, err := o.lastTurnBlocker(); err != nil {
			return fmt.Errorf("checking blocker after turn %d: %w", turn, err)
		} else if blocked != nil {
			return blocked
		}

		// Update the criteria-count snapshot for this turn — used by
		// detectOscillation to tell whether progress is happening.
		if checkedNow, err := o.specCheckedCount(); err == nil {
			o.criteriaCountByTurn[turn] = checkedNow
		}

		// Scope enforcement: were any files touched outside the spec's
		// `Files in scope` AND not added to scope this same turn?
		if violations, err := o.detectScopeViolation(turn, ag.Name(), touched); err != nil {
			ui.Warning(fmt.Sprintf("scope check failed: %v", err))
		} else if len(violations) > 0 {
			o.pendingScopeWarning = &scopeWarning{
				Turn:     turn,
				Agent:    ag.Name(),
				OffScope: violations,
			}
		}

		// Oscillation detection. detectOscillation manages its own state
		// (incrementing oscillationConsecutive on suspect turns, resetting
		// otherwise). Returns a non-nil error of type *ErrOscillationDetected
		// when the threshold is reached and the relay must stop.
		if osc, err := o.detectOscillation(turn); err != nil {
			ui.Warning(fmt.Sprintf("oscillation check failed: %v", err))
		} else if osc != nil {
			if osc.Forced {
				return &ErrOscillationDetected{
					Files: osc.OverlapFiles,
					Turns: osc.Turns,
				}
			}
			o.pendingOscillationWarning = &oscillationWarning{
				OverlapFiles: osc.OverlapFiles,
				Turns:        osc.Turns,
			}
		}

		// Need at least two turns before checking consensus.
		if turn >= 2 {
			done, err := o.consensusReached()
			if err != nil {
				return fmt.Errorf("checking completion after turn %d: %w", turn, err)
			}
			if done {
				ui.Success(o.taskLog.Path(), o.cfg.WorkDir)
				return nil
			}
		}
	}

	// Only reachable when MaxTurns > 0 and the loop exhausted.
	if o.cfg.MaxTurns > 0 {
		ui.MaxTurnsReached(o.cfg.MaxTurns, o.taskLog.Path(), o.cfg.WorkDir)
	}
	return nil
}

// lastTurnBlocker returns *ErrAgentBlocked when the most recent turn entry
// has a non-empty **Blocked on** field, otherwise nil. The caller wraps this
// in error-typed flow by returning the pointer directly.
func (o *Orchestrator) lastTurnBlocker() (*ErrAgentBlocked, error) {
	turns, err := o.taskLog.ParseTurns()
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, nil
	}
	last := turns[len(turns)-1]
	if last.Blocker == "" {
		return nil, nil
	}
	return &ErrAgentBlocked{
		Agent:  last.AgentName,
		Turn:   last.TurnNumber,
		Reason: last.Blocker,
	}, nil
}

// specCheckedCount returns the number of `[x]` Acceptance Criteria currently
// in the spec. Used by detectOscillation to detect "no progress over K turns".
func (o *Orchestrator) specCheckedCount() (int, error) {
	content, err := o.taskLog.ReadSpec()
	if err != nil {
		return 0, err
	}
	return countCheckedCriteria(content), nil
}

// countCheckedCriteria parses the spec content and counts `- [x]` items in
// the Acceptance Criteria section.
func countCheckedCriteria(spec string) int {
	const sectionHeader = "## Acceptance Criteria"
	idx := strings.Index(spec, sectionHeader)
	if idx == -1 {
		return 0
	}
	// Section spans from the header to the next "## " or end of file.
	rest := spec[idx+len(sectionHeader):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	count := 0
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [x]") || strings.HasPrefix(line, "- [X]") {
			count++
		}
	}
	return count
}

// detectScopeViolation returns the subset of `touched` that are NOT in the
// spec's `Files in scope`, AFTER the agent had a chance to extend scope this
// same turn. The spec is read fresh — if the agent added new files to scope
// and edited them in the same turn, no violation.
func (o *Orchestrator) detectScopeViolation(turn int, agent string, touched []string) ([]string, error) {
	if len(touched) == 0 {
		return nil, nil
	}
	scope, err := o.taskLog.SpecFilesInScope()
	if err != nil {
		return nil, err
	}
	if len(scope) == 0 {
		// Spec doesn't list files yet (DRAFT or still being shaped) — no scope to enforce.
		return nil, nil
	}
	scopeSet := make(map[string]struct{}, len(scope))
	for _, f := range scope {
		scopeSet[f] = struct{}{}
		// Also accept the basename match — agents and humans may write paths
		// relative to different roots.
		scopeSet[filepath.Base(f)] = struct{}{}
	}
	var violations []string
	for _, f := range touched {
		if _, ok := scopeSet[f]; ok {
			continue
		}
		if _, ok := scopeSet[filepath.Base(f)]; ok {
			continue
		}
		violations = append(violations, f)
	}
	return violations, nil
}

// oscillationDetection is the internal result of detectOscillation.
type oscillationDetection struct {
	OverlapFiles []string
	Turns        int
	Forced       bool // true when the consecutive count reached the stop threshold
}

// detectOscillation examines the last K turns for the unproductive-loop
// pattern: same files churned, no new criteria checked. Updates
// o.oscillationConsecutive and returns a *oscillationDetection when a warning
// or forced stop is warranted.
func (o *Orchestrator) detectOscillation(turn int) (*oscillationDetection, error) {
	if turn < oscillationWindow {
		return nil, nil
	}

	// Collect the touched-files for the last K turns from disk.
	windowStart := turn - oscillationWindow + 1
	turnsFiles := make([][]string, 0, oscillationWindow)
	turns, err := o.taskLog.ParseTurns()
	if err != nil {
		return nil, err
	}
	turnByNumber := make(map[int]string, len(turns))
	for _, t := range turns {
		turnByNumber[t.TurnNumber] = t.AgentName
	}
	for t := windowStart; t <= turn; t++ {
		agent, ok := turnByNumber[t]
		if !ok {
			// Missing turn data (synthetic timeout entry, etc.) → reset.
			o.oscillationConsecutive = 0
			return nil, nil
		}
		files, err := o.taskLog.ReadTurnFiles(t, agent)
		if err != nil {
			return nil, err
		}
		// Skip the window if any turn has no recorded files (e.g. Codex agent,
		// or pure verification turn). We don't want to false-positive on
		// turns where data is simply missing.
		if files == nil {
			o.oscillationConsecutive = 0
			return nil, nil
		}
		if len(files) == 0 {
			// Verification-only turn: legitimate, doesn't fit the pattern.
			o.oscillationConsecutive = 0
			return nil, nil
		}
		turnsFiles = append(turnsFiles, files)
	}

	// All K turns have non-empty file lists. Compute pairwise Jaccard overlap;
	// require ALL pairs to be >= threshold for the pattern to count.
	overlapFiles := turnsFiles[0]
	for i := 1; i < len(turnsFiles); i++ {
		overlap, jaccard := setOverlap(overlapFiles, turnsFiles[i])
		if jaccard < oscillationOverlapThreshold {
			o.oscillationConsecutive = 0
			return nil, nil
		}
		overlapFiles = overlap
	}

	// Pattern matched. Now check criteria progress: was at least one new
	// `[x]` checked across the window? If yes, work is happening — not stuck.
	if o.criteriaProgressedAcrossWindow(windowStart, turn) {
		o.oscillationConsecutive = 0
		return nil, nil
	}

	// Confirmed: same files, no progress.
	o.oscillationConsecutive++
	det := &oscillationDetection{
		OverlapFiles: overlapFiles,
		Turns:        oscillationWindow + (o.oscillationConsecutive - 1),
		Forced:       o.oscillationConsecutive >= oscillationStopThreshold,
	}
	return det, nil
}

// criteriaProgressedAcrossWindow returns true when the spec gained at least
// one `[x]` between the start and end of the window.
func (o *Orchestrator) criteriaProgressedAcrossWindow(windowStart, windowEnd int) bool {
	startCount, hasStart := o.criteriaCountByTurn[windowStart-1]
	endCount, hasEnd := o.criteriaCountByTurn[windowEnd]
	if !hasStart || !hasEnd {
		// Not enough data points (likely first run); assume no progress to be safe.
		return false
	}
	return endCount > startCount
}

// setOverlap returns the intersection of a and b, plus the Jaccard coefficient
// |a∩b| / |a∪b|. Empty inputs return jaccard=0.
func setOverlap(a, b []string) ([]string, float64) {
	if len(a) == 0 || len(b) == 0 {
		return nil, 0
	}
	aSet := make(map[string]struct{}, len(a))
	for _, x := range a {
		aSet[x] = struct{}{}
	}
	var inter []string
	union := len(aSet)
	for _, x := range b {
		if _, ok := aSet[x]; ok {
			inter = append(inter, x)
		} else {
			union++
		}
	}
	if union == 0 {
		return inter, 0
	}
	return inter, float64(len(inter)) / float64(union)
}

// consensusReached returns true when the relay should stop:
//  1. Last two turns both report **Complete: yes**, AND
//  2. The spec is ACTIVE (not DRAFT), AND
//  3. Every Acceptance Criterion in the spec is checked `- [x]`.
//
// A warning is printed when agents declare consensus but the spec is not
// satisfied — this surfaces the override to the human.
func (o *Orchestrator) consensusReached() (bool, error) {
	agreed, err := o.taskLog.BothAgentsAgreeComplete()
	if err != nil {
		return false, err
	}
	if !agreed {
		return false, nil
	}

	specDraft, err := o.taskLog.SpecIsDraft()
	if err != nil {
		return false, fmt.Errorf("checking spec status: %w", err)
	}
	criteriaOK, err := o.taskLog.SpecAllCriteriaChecked()
	if err != nil {
		return false, fmt.Errorf("checking spec criteria: %w", err)
	}

	if specDraft {
		ui.Warning("agents declared Complete: yes but spec is still DRAFT — continuing")
		return false, nil
	}
	if !criteriaOK {
		ui.Warning("agents declared Complete: yes but not all spec criteria are checked — continuing")
		return false, nil
	}
	return true, nil
}

// maybeRelocateTaskDir reads the Working Directory field from the last turn entry.
// If it differs from the current workDir, the task directory is moved there and
// o.cfg.WorkDir is updated so subsequent turns use the correct location.
func (o *Orchestrator) maybeRelocateTaskDir() error {
	reported, err := o.taskLog.ParseLastWorkingDir()
	if err != nil || reported == "" {
		return err
	}

	if reported == filepath.Clean(o.cfg.WorkDir) {
		return nil
	}

	if _, err := os.Stat(reported); err != nil {
		return fmt.Errorf("agent reported working dir %q but it does not exist: %w", reported, err)
	}

	oldDir := o.taskLog.Dir
	if err := o.taskLog.Relocate(reported); err != nil {
		return err
	}

	ui.Relocated(oldDir, o.taskLog.Dir)
	o.cfg.WorkDir = reported
	return nil
}

// runTurnWithTUI runs a regular relay turn with the streaming TUI. Thin
// wrapper around runAgentWithTUI that uses the default "Turn N" label.
func (o *Orchestrator) runTurnWithTUI(ag agent.Agent, prompt string, turn int, taskStart time.Time) (agent.TurnResult, bool, error) {
	return o.runAgentWithTUI(ag, prompt, turn, taskStart, "")
}

// runAgentWithTUI runs an agent with a bubbletea TUI for live output.
// labelOverride, when non-empty, replaces the default "Turn N" label in the
// header (used for spec pre-turns).
//
// When MaxTurnDuration is set, a timer fires cancel() at the deadline and the
// turn is reported via *ErrTurnTimeout. User-initiated stop (ESC) takes
// priority over a deadline that fired in the same instant.
//
// Returns the result, whether the user stopped (ESC), and any error.
func (o *Orchestrator) runAgentWithTUI(ag agent.Agent, prompt string, turn int, taskStart time.Time, labelOverride string) (agent.TurnResult, bool, error) {
	// Cancellable context — ESC in the TUI and the deadline timer both call cancel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Per-turn timeout: fires cancel() at the deadline. We track which mechanism
	// fired the cancel so we can surface a typed error.
	var timedOut atomic.Bool
	if o.cfg.MaxTurnDuration > 0 {
		timer := time.AfterFunc(o.cfg.MaxTurnDuration, func() {
			timedOut.Store(true)
			cancel()
		})
		defer timer.Stop()
	}

	// Event channel — agent pushes events, TUI consumes them.
	eventCh := make(chan agent.StreamEvent, 100)

	if sa, ok := ag.(agent.StreamingAgent); ok {
		sa.SetOnEvent(func(evt agent.StreamEvent) {
			eventCh <- evt
		})
		sa.SetOnStreamDone(func() {
			close(eventCh) // TUI gets channelClosedMsg immediately
		})
	}

	// Run agent in background.
	type turnOutcome struct {
		result agent.TurnResult
		err    error
	}
	outcomeCh := make(chan turnOutcome, 1)

	go func() {
		r, e := ag.RunTurn(ctx, o.cfg.WorkDir, prompt)
		// eventCh already closed by OnStreamDone (before cmd.Wait)
		outcomeCh <- turnOutcome{r, e}
	}()

	// Launch bubbletea TUI — blocks until agent finishes (+ auto-continue or review)
	// or user presses ESC (which calls cancel, killing the agent).
	agBackend := o.agentBackend(ag.Name())
	agModel := o.agentModel(ag.Name())
	backendModel := agBackend
	if agModel != "" {
		backendModel += "/" + agModel
	}
	_, stopped := tui.RunAgentView(eventCh, cancel, ag.Name(), backendModel, turn, o.cfg.MaxTurns, taskStart, labelOverride)

	// Collect agent result.
	outcome := <-outcomeCh

	// Stopped (ESC) takes priority over timeout — user-initiated.
	if stopped {
		return outcome.result, true, nil
	}
	if timedOut.Load() {
		return outcome.result, false, &ErrTurnTimeout{
			Agent:    ag.Name(),
			Turn:     turn,
			Duration: o.cfg.MaxTurnDuration,
		}
	}
	return outcome.result, false, outcome.err
}

// runSpecPreturn runs a single spec pre-turn for an agent. The agent reads
// the original prompt and the current spec, and updates the spec file. Pre-turns
// do not write to the task log.
func (o *Orchestrator) runSpecPreturn(ag agent.Agent, idx, total int, taskStart time.Time) (agent.TurnResult, bool, error) {
	prompt, err := o.buildSpecPreturnPrompt(ag.Name())
	if err != nil {
		return agent.TurnResult{}, false, fmt.Errorf("building spec pre-turn prompt: %w", err)
	}
	label := fmt.Sprintf("Spec Pre-turn %d/%d", idx, total)
	return o.runAgentWithTUI(ag, prompt, 0, taskStart, label)
}

// buildPrompt constructs the full prompt string for a given agent turn.
func (o *Orchestrator) buildPrompt(turnNumber int, agentName string) (string, error) {
	taskLogContent, err := o.taskLog.WindowedContent(o.cfg.LogWindow)
	if err != nil {
		return "", err
	}

	specContent, err := o.taskLog.ReadSpec()
	if err != nil {
		return "", fmt.Errorf("reading spec: %w", err)
	}

	projectRules := loadProjectRules(o.cfg.WorkDir)

	var b strings.Builder

	fmt.Fprintf(&b, "# DepartAI — Turn %d\n\n", turnNumber)
	fmt.Fprintf(&b, "You are **%s**, an AI coding agent in a two-agent relay team.\n\n", agentName)

	b.WriteString("## Agent Protocol\n\n")
	b.WriteString(o.baseInstr)
	b.WriteString("\n\n")

	if projectRules != "" {
		b.WriteString("## Project Rules\n\n")
		b.WriteString(projectRules)
		b.WriteString("\n\n")
	}

	if len(o.cfg.BlockedCommands) > 0 {
		b.WriteString("## Forbidden Commands\n\n")
		b.WriteString("You are NOT allowed to use the following commands or tools during this turn:\n\n")
		for _, c := range o.cfg.BlockedCommands {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\nIf the task appears to require any of them, STOP and explain in your turn ")
		b.WriteString("summary why you cannot proceed. Do not attempt workarounds (different shell, ")
		b.WriteString("escaped variants, alternative tools that achieve the same effect). Mark ")
		b.WriteString("**Complete: no** and let the human decide whether to relax the restriction.\n\n")
	}

	if o.cfg.MaxTurnDuration > 0 {
		b.WriteString("## Time budget\n\n")
		fmt.Fprintf(&b, "You have **%s** for this turn. The orchestrator will forcibly terminate ", o.cfg.MaxTurnDuration)
		b.WriteString("your turn at the deadline.\n\n")
		b.WriteString("Pace yourself:\n")
		b.WriteString("- Do NOT spend the entire budget on research or analysis. Start producing outputs within the first third.\n")
		b.WriteString("- **Checkpoint frequently**: every 3-5 substantial actions, append a brief progress note to the task log so your work survives a forced kill.\n")
		b.WriteString("- When you sense ~80% of the budget is consumed, STOP gathering context, write your turn summary, and be EXPLICIT in **Next Steps** about what remains for the next agent to pick up.\n")
		b.WriteString("- The next agent has its own fresh budget — they continue your work, no human intervention needed. **Time pressure alone is NEVER a reason to set `Blocked on`.** Reserve `Blocked on` for genuinely ambiguous decisions only.\n\n")
	}

	if w := o.pendingScopeWarning; w != nil {
		b.WriteString("## Scope warning\n\n")
		fmt.Fprintf(&b, "The previous turn (Turn %d, %s) modified files NOT listed in `Files in scope`:\n\n", w.Turn, w.Agent)
		for _, f := range w.OffScope {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\nEither:\n")
		b.WriteString("- Justify the change in **Decisions log** AND add the file(s) to `Files in scope`, or\n")
		b.WriteString("- Revert the off-scope change in your turn.\n\n")
		b.WriteString("(Soft warning. The orchestrator does not block writes.)\n\n")
		o.pendingScopeWarning = nil // one-shot
	}

	if w := o.pendingOscillationWarning; w != nil {
		b.WriteString("## Possible oscillation detected\n\n")
		fmt.Fprintf(&b, "The orchestrator has noticed the last %d turns have all touched mostly the same files without any new Acceptance Criteria being checked off:\n\n", w.Turns)
		for _, f := range w.OverlapFiles {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\nStep back and identify the root cause:\n")
		b.WriteString("- Are you and the previous agent disagreeing on the same point?\n")
		b.WriteString("- Is there a misunderstanding of the spec?\n")
		b.WriteString("- Is the criterion underspecified?\n\n")
		b.WriteString("Take ONE decisive action this turn — or, if there is genuine disagreement that needs human input, set **Blocked on** with the specifics.\n\n")
		b.WriteString("If no progress within the next couple of turns, the orchestrator will stop the relay and return control to the human.\n\n")
		o.pendingOscillationWarning = nil // one-shot
	}

	b.WriteString("## Spec (definition of done)\n\n")
	fmt.Fprintf(&b, "File path: `%s`\n\n", o.taskLog.SpecPath())
	b.WriteString("Current contents:\n\n")
	b.WriteString("```\n")
	b.WriteString(specContent)
	b.WriteString("\n```\n\n")
	b.WriteString("Update this file as work progresses (mark `- [x]` on completed criteria, ")
	b.WriteString("append to Decisions log, escalate ambiguities to Open questions). ")
	b.WriteString("Append-only contract — never weaken or remove existing criteria.\n\n")

	b.WriteString("## Task Log\n\n")
	fmt.Fprintf(&b, "File path: `%s`\n\n", o.taskLog.Path())
	b.WriteString("Current contents:\n\n")
	b.WriteString("```\n")
	b.WriteString(taskLogContent)
	b.WriteString("\n```\n\n")

	b.WriteString("## Your Turn\n\n")
	fmt.Fprintf(&b, "This is **Turn %d**. You are **%s**.\n\n", turnNumber, agentName)
	b.WriteString("Steps:\n")
	b.WriteString("1. Read the spec and task log above to understand what's required and what's been done.\n")
	b.WriteString("2. Continue the work — implement, test, fix, iterate. Update the spec as criteria are met.\n")
	fmt.Fprintf(&b, "3. When finished, append your turn summary to the task log at:\n   `%s`\n\n", o.taskLog.Path())
	fmt.Fprintf(&b,
		"Your summary MUST begin with `## Turn %d - %s` and include `**Complete**: yes` or `**Complete**: no`.\n\n",
		turnNumber, agentName,
	)
	b.WriteString("Begin now.\n")

	return b.String(), nil
}

// buildSpecPreturnPrompt constructs the prompt for a spec pre-turn. The agent
// is asked to populate (or extend) the spec.md file. No turn log is appended.
//
// When the task log already has user directives or completed turns (re-spec
// case via /respec, or resume), the task log is also injected so the agent can
// integrate new directives and avoid proposing criteria for work already done.
func (o *Orchestrator) buildSpecPreturnPrompt(agentName string) (string, error) {
	spec, err := o.taskLog.ReadSpec()
	if err != nil {
		return "", fmt.Errorf("reading spec: %w", err)
	}

	taskLogContent, err := o.taskLog.WindowedContent(o.cfg.LogWindow)
	if err != nil {
		return "", fmt.Errorf("reading task log: %w", err)
	}

	existingTurns, _ := o.taskLog.ParseTurns()
	hasHistory := len(existingTurns) > 0 || strings.Contains(taskLogContent, "## User Directive")

	var b strings.Builder

	fmt.Fprintf(&b, "# DepartAI — Spec Pre-turn for %s\n\n", agentName)
	fmt.Fprintf(&b, "You are **%s**.\n\n", agentName)

	b.WriteString("## Pre-turn Protocol\n\n")
	b.WriteString(specPreturnInstructions)
	b.WriteString("\n\n")

	if rules := loadProjectRules(o.cfg.WorkDir); rules != "" {
		b.WriteString("## Project Rules\n\n")
		b.WriteString(rules)
		b.WriteString("\n\n")
	}

	if len(o.cfg.BlockedCommands) > 0 {
		b.WriteString("## Forbidden Commands\n\n")
		b.WriteString("You are NOT allowed to use the following commands or tools during this pre-turn:\n\n")
		for _, c := range o.cfg.BlockedCommands {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}

	if o.cfg.MaxTurnDuration > 0 {
		fmt.Fprintf(&b, "## Time budget\n\nThis pre-turn has **%s** before the orchestrator forcibly terminates it. Be concise — the goal is just an updated spec.\n\n", o.cfg.MaxTurnDuration)
	}

	b.WriteString("## The user's original request\n\n")
	b.WriteString(o.cfg.Prompt)
	b.WriteString("\n\n")

	if hasHistory {
		b.WriteString("## Task log so far\n\n")
		b.WriteString("This task already has activity. Look for `## User Directive` blocks ")
		b.WriteString("(new requirements added by the user since the spec was last shaped) and ")
		b.WriteString("`## Turn` blocks (work already done). Use this to:\n")
		b.WriteString("- Add Acceptance Criteria for any User Directive not yet reflected in the spec.\n")
		b.WriteString("- Avoid adding criteria for work that is already complete.\n\n")
		fmt.Fprintf(&b, "File path: `%s`\n\n", o.taskLog.Path())
		b.WriteString("Contents:\n\n")
		b.WriteString("```\n")
		b.WriteString(taskLogContent)
		b.WriteString("\n```\n\n")
	}

	b.WriteString("## Current spec\n\n")
	fmt.Fprintf(&b, "File path: `%s`\n\n", o.taskLog.SpecPath())
	b.WriteString("Current contents:\n\n")
	b.WriteString("```\n")
	b.WriteString(spec)
	b.WriteString("\n```\n\n")

	b.WriteString("Begin now. Overwrite the spec file with your updated content. Do NOT modify any other file.\n")

	return b.String(), nil
}

// loadInstructions returns custom instructions from path (user override), or
// the built-in protocol matching the active mode ("dev" or "ask").
func loadInstructions(path, mode string) (string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading instructions file %q: %w", path, err)
		}
		return string(data), nil
	}
	switch mode {
	case "ask":
		return askInstructions, nil
	default:
		return devInstructions, nil
	}
}

// loadProjectRules reads common project convention files and concatenates them.
func loadProjectRules(workDir string) string {
	candidates := []string{
		"CLAUDE.md",
		"AGENTS.md",
		".cursorrules",
		".github/copilot-instructions.md",
	}

	var parts []string
	for _, name := range candidates {
		data, err := os.ReadFile(filepath.Join(workDir, name))
		if err == nil && len(data) > 0 {
			parts = append(parts, fmt.Sprintf("### %s\n\n%s", name, string(data)))
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}
