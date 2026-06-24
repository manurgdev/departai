package orchestrator

import (
	"context"
	"errors"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manurgdev/departai/internal/agent"
	"github.com/manurgdev/departai/internal/tasklog"
)

// floatNear reports whether two floats are within a small epsilon.
func floatNear(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// ─────────────────────────────────────────────────────────────────────────────
// Pure-function unit tests
// ─────────────────────────────────────────────────────────────────────────────

func TestSetOverlap(t *testing.T) {
	cases := []struct {
		name        string
		a, b        []string
		wantInter   int
		wantJaccard float64
	}{
		{"both empty", nil, nil, 0, 0},
		{"a empty", nil, []string{"x"}, 0, 0},
		{"b empty", []string{"x"}, nil, 0, 0},
		{"disjoint", []string{"a", "b"}, []string{"c", "d"}, 0, 0},
		{"identical", []string{"a", "b"}, []string{"a", "b"}, 2, 1.0},
		// |∩|=1 (a), |∪|=3 (a,b,c) → 1/3
		{"partial", []string{"a", "b"}, []string{"a", "c"}, 1, 1.0 / 3.0},
		// |∩|=2 (a,b), |∪|=3 (a,b,c) → 2/3
		{"subset", []string{"a", "b", "c"}, []string{"a", "b"}, 2, 2.0 / 3.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inter, jaccard := setOverlap(tc.a, tc.b)
			if len(inter) != tc.wantInter {
				t.Errorf("intersection size = %d, want %d (%v)", len(inter), tc.wantInter, inter)
			}
			if !floatNear(jaccard, tc.wantJaccard) {
				t.Errorf("jaccard = %v, want %v", jaccard, tc.wantJaccard)
			}
		})
	}
}

func TestCountCheckedCriteria(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want int
	}{
		{"no section", "# Spec\n\n## Goal\nfoo\n", 0},
		{
			"none checked",
			"## Acceptance Criteria\n\n- [ ] one\n- [ ] two\n",
			0,
		},
		{
			"mixed",
			"## Acceptance Criteria\n\n- [x] one\n- [ ] two\n- [X] three\n",
			2,
		},
		{
			"stops at next section",
			"## Acceptance Criteria\n\n- [x] one\n\n## Files in scope\n\n- [x] not-a-criterion\n",
			1,
		},
		{
			"all checked",
			"## Acceptance Criteria\n\n- [x] one\n- [x] two\n",
			2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countCheckedCriteria(tc.spec); got != tc.want {
				t.Errorf("countCheckedCriteria = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBackendOrDefault(t *testing.T) {
	if got := backendOrDefault("codex", "claude"); got != "codex" {
		t.Errorf("override should win, got %q", got)
	}
	if got := backendOrDefault("", "claude"); got != "claude" {
		t.Errorf("empty override should fall back, got %q", got)
	}
}

func TestModelOrDefault(t *testing.T) {
	if got := modelOrDefault("opus", "sonnet"); got != "opus" {
		t.Errorf("override should win, got %q", got)
	}
	if got := modelOrDefault("", "sonnet"); got != "sonnet" {
		t.Errorf("empty override should fall back, got %q", got)
	}
}

func TestBuildOneAgent(t *testing.T) {
	cases := []struct {
		backend string
		wantErr bool
	}{
		{"claude", false},
		{"", false}, // empty defaults to claude
		{"codex", false},
		{"nonsense", true},
	}
	for _, tc := range cases {
		t.Run(tc.backend, func(t *testing.T) {
			ag, err := buildOneAgent("Agent Alpha", tc.backend, "")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for backend %q, got nil", tc.backend)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ag.Name() != "Agent Alpha" {
				t.Errorf("agent name = %q, want %q", ag.Name(), "Agent Alpha")
			}
		})
	}
}

func TestBuildAgents(t *testing.T) {
	agents, err := buildAgents(Config{AgentBackend: "claude"})
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}
	if agents[0].Name() != "Agent Alpha" || agents[1].Name() != "Agent Beta" {
		t.Errorf("agent names = %q, %q; want Agent Alpha, Agent Beta", agents[0].Name(), agents[1].Name())
	}
}

func TestBuildAgentsUnknownBackend(t *testing.T) {
	if _, err := buildAgents(Config{AgentBackend: "nope"}); err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
}

func TestIsTransientError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		stderr string
		want   bool
	}{
		{"nil error", nil, "", false},
		{"rate limit", errors.New("agent failed"), "Error: rate limit exceeded", true},
		{"429", errors.New("exited: HTTP 429"), "", true},
		{"overloaded 529", errors.New("api error"), "529 overloaded_error", true},
		{"503", errors.New("503 Service Unavailable"), "", true},
		{"connection reset", errors.New("read tcp: connection reset by peer"), "", true},
		{"io timeout", errors.New("dial tcp: i/o timeout"), "", true},
		{"plain unknown error", errors.New("something odd happened"), "", false},
		{"invalid model (permanent)", errors.New("invalid model foo-bar"), "", false},
		{"401 unauthorized (permanent)", errors.New("401 unauthorized"), "", false},
		{"cli not installed (permanent)", errors.New("executable file not found in $PATH"), "", false},
		// Permanent marker wins even when a transient one is also present.
		{"permanent beats transient", errors.New("429 rate limit"), "invalid api key", false},
		{"context canceled", context.Canceled, "rate limit", false},
		{"deadline exceeded", context.DeadlineExceeded, "timeout", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientError(tc.err, tc.stderr); got != tc.want {
				t.Errorf("isTransientError = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens(""); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	if got := estimateTokens(strings.Repeat("x", 400)); got != 100 {
		t.Errorf("400 chars = %d, want 100", got)
	}
}

func TestContextWindowFor(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"claude-opus-4-8[1m]", oneMillionContextWindow},
		{"some-model-1m", oneMillionContextWindow},
		{"claude-opus-4-8", defaultContextWindow},
		{"gpt-5.3-codex", defaultContextWindow},
		{"", defaultContextWindow},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if got := contextWindowFor(tc.model); got != tc.want {
				t.Errorf("contextWindowFor(%q) = %d, want %d", tc.model, got, tc.want)
			}
		})
	}
}

func TestContextBudgetExceeded(t *testing.T) {
	// 200k window → threshold at 160k tokens → ~640k chars.
	small := strings.Repeat("x", 1000)
	if _, _, exceeded := contextBudgetExceeded(small, "claude-opus-4-8"); exceeded {
		t.Error("small prompt should not exceed the budget")
	}

	big := strings.Repeat("x", 700_000) // ~175k tokens > 160k threshold
	est, window, exceeded := contextBudgetExceeded(big, "claude-opus-4-8")
	if !exceeded {
		t.Errorf("big prompt should exceed: est=%d window=%d", est, window)
	}

	// Same big prompt under a 1M window is well within budget.
	if _, _, exceeded := contextBudgetExceeded(big, "claude-opus-4-8[1m]"); exceeded {
		t.Error("big prompt should be fine under a 1M window")
	}
}

func TestCheckContextBudgetWarnsOnce(t *testing.T) {
	o := &Orchestrator{cfg: Config{Model: "claude-opus-4-8"}}
	big := strings.Repeat("x", 700_000)

	o.checkContextBudget(big, "Agent Alpha")
	if !o.contextWarned {
		t.Fatal("expected contextWarned=true after crossing threshold")
	}
	// Idempotent: stays warned, doesn't reset.
	o.checkContextBudget(big, "Agent Alpha")
	if !o.contextWarned {
		t.Error("contextWarned should remain true")
	}
}

func TestRetryDelayIsIncreasing(t *testing.T) {
	d1, d2, d3 := retryDelay(1), retryDelay(2), retryDelay(3)
	if !(d1 < d2 && d2 < d3) {
		t.Errorf("expected increasing backoff, got %v, %v, %v", d1, d2, d3)
	}
	if d1 != 3*time.Second {
		t.Errorf("retryDelay(1) = %v, want 3s", d1)
	}
}

func TestWithJitterStaysInBand(t *testing.T) {
	base := 4 * time.Second
	for i := 0; i < 200; i++ {
		d := withJitter(base)
		// ±25% band.
		if d < 3*time.Second || d > 5*time.Second {
			t.Fatalf("jittered delay %v out of [3s,5s] band", d)
		}
	}
}

func TestRunWithRetry(t *testing.T) {
	transient := func() (agent.TurnResult, bool, error) {
		return agent.TurnResult{Stderr: "rate limit exceeded"}, false, errors.New("turn failed: rate limit")
	}

	t.Run("succeeds first try", func(t *testing.T) {
		o := &Orchestrator{cfg: Config{MaxRetries: 2}, sleep: func(time.Duration) {}}
		calls := 0
		_, _, err := o.runWithRetry("Agent Alpha", func() (agent.TurnResult, bool, error) {
			calls++
			return agent.TurnResult{Output: "ok"}, false, nil
		})
		if err != nil || calls != 1 {
			t.Errorf("calls=%d err=%v, want 1 call no error", calls, err)
		}
	})

	t.Run("retries transient then succeeds", func(t *testing.T) {
		o := &Orchestrator{cfg: Config{MaxRetries: 2}, sleep: func(time.Duration) {}}
		calls := 0
		_, _, err := o.runWithRetry("Agent Alpha", func() (agent.TurnResult, bool, error) {
			calls++
			if calls < 3 {
				return transient()
			}
			return agent.TurnResult{Output: "ok"}, false, nil
		})
		if err != nil || calls != 3 {
			t.Errorf("calls=%d err=%v, want 3 calls no error", calls, err)
		}
	})

	t.Run("gives up after max retries", func(t *testing.T) {
		o := &Orchestrator{cfg: Config{MaxRetries: 2}, sleep: func(time.Duration) {}}
		calls := 0
		_, _, err := o.runWithRetry("Agent Alpha", func() (agent.TurnResult, bool, error) {
			calls++
			return transient()
		})
		if err == nil || calls != 3 { // 1 initial + 2 retries
			t.Errorf("calls=%d err=%v, want 3 calls with error", calls, err)
		}
	})

	t.Run("does not retry non-transient", func(t *testing.T) {
		o := &Orchestrator{cfg: Config{MaxRetries: 2}, sleep: func(time.Duration) {}}
		calls := 0
		_, _, err := o.runWithRetry("Agent Alpha", func() (agent.TurnResult, bool, error) {
			calls++
			return agent.TurnResult{}, false, errors.New("invalid model foo")
		})
		if err == nil || calls != 1 {
			t.Errorf("calls=%d err=%v, want 1 call with error", calls, err)
		}
	})

	t.Run("does not retry on stop", func(t *testing.T) {
		o := &Orchestrator{cfg: Config{MaxRetries: 2}, sleep: func(time.Duration) {}}
		calls := 0
		_, stopped, _ := o.runWithRetry("Agent Alpha", func() (agent.TurnResult, bool, error) {
			calls++
			return agent.TurnResult{}, true, nil
		})
		if !stopped || calls != 1 {
			t.Errorf("calls=%d stopped=%v, want 1 call stopped", calls, stopped)
		}
	})

	t.Run("max retries 0 disables", func(t *testing.T) {
		o := &Orchestrator{cfg: Config{MaxRetries: 0}, sleep: func(time.Duration) {}}
		calls := 0
		_, _, err := o.runWithRetry("Agent Alpha", func() (agent.TurnResult, bool, error) {
			calls++
			return transient()
		})
		if err == nil || calls != 1 {
			t.Errorf("calls=%d err=%v, want 1 call (no retries)", calls, err)
		}
	})

	t.Run("does not retry timeout", func(t *testing.T) {
		o := &Orchestrator{cfg: Config{MaxRetries: 2}, sleep: func(time.Duration) {}}
		calls := 0
		_, _, err := o.runWithRetry("Agent Alpha", func() (agent.TurnResult, bool, error) {
			calls++
			return agent.TurnResult{}, false, &ErrTurnTimeout{Agent: "Agent Alpha", Turn: 1}
		})
		if err == nil || calls != 1 {
			t.Errorf("calls=%d err=%v, want 1 call (timeout not retried)", calls, err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E Run() loop tests with a scripted fake backend
// ─────────────────────────────────────────────────────────────────────────────

// turnBehavior scripts what a fake agent does on a given main-relay turn.
type turnBehavior struct {
	markAllCriteria bool   // flip every `- [ ]` to `- [x]` in the spec this turn
	complete        bool   // value written for **Complete**
	blocker         string // optional **Blocked on** reason
}

// scriptedAgent is a fake agent.StreamingAgent that drives the orchestrator
// through a deterministic relay without invoking any real CLI. It reacts to the
// prompt: a "Spec Pre-turn" prompt triggers spec population; a main-turn prompt
// appends a turn summary per the shared script.
type scriptedAgent struct {
	name      string
	tl        *tasklog.TaskLog
	script    map[int]turnBehavior
	populates bool // whether this agent populates the spec during pre-turns
	onEvent   func(agent.StreamEvent)
	onDone    func()
}

func (a *scriptedAgent) Name() string                          { return a.name }
func (a *scriptedAgent) SetOnEvent(fn func(agent.StreamEvent)) { a.onEvent = fn }
func (a *scriptedAgent) SetOnStreamDone(fn func())             { a.onDone = fn }

var promptTurnRe = regexp.MustCompile(`Turn (\d+)`)

func (a *scriptedAgent) RunTurn(_ context.Context, _ string, prompt string) (agent.TurnResult, error) {
	// Exercise the streaming wiring with one event, then signal stream end.
	if a.onEvent != nil {
		a.onEvent(agent.StreamEvent{Kind: "text", Text: "scripted working"})
	}
	defer func() {
		if a.onDone != nil {
			a.onDone()
		}
	}()

	if strings.Contains(prompt, "Spec Pre-turn") {
		if a.populates {
			a.writeActiveSpec()
		}
		return agent.TurnResult{Output: "pre-turn done"}, nil
	}

	turn := parseTurnNumber(prompt)
	b := a.script[turn]

	var activity []string
	if b.markAllCriteria {
		a.checkAllCriteria()
		activity = []string{"Write main.go"}
	}
	a.appendTurnSummary(turn, b)

	return agent.TurnResult{Output: "turn done", Activity: activity}, nil
}

func parseTurnNumber(prompt string) int {
	m := promptTurnRe.FindStringSubmatch(prompt)
	if m == nil {
		return 0
	}
	n := 0
	for _, r := range m[1] {
		n = n*10 + int(r-'0')
	}
	return n
}

func (a *scriptedAgent) writeActiveSpec() {
	spec := `# Spec

**Task ID**: test
**Status**: ACTIVE
**Last updated**: 2026-01-01 00:00:00

## Goal

Build the thing.

## Acceptance Criteria

- [ ] The thing exists

## Files in scope

- main.go

## Out of scope

None

## Open questions

None

## Decisions log

- alpha: drafted spec
`
	if err := os.WriteFile(a.tl.SpecPath(), []byte(spec), 0644); err != nil {
		panic(err)
	}
}

func (a *scriptedAgent) checkAllCriteria() {
	data, err := os.ReadFile(a.tl.SpecPath())
	if err != nil {
		panic(err)
	}
	updated := strings.ReplaceAll(string(data), "- [ ]", "- [x]")
	if err := os.WriteFile(a.tl.SpecPath(), []byte(updated), 0644); err != nil {
		panic(err)
	}
}

func (a *scriptedAgent) appendTurnSummary(turn int, b turnBehavior) {
	complete := "no"
	if b.complete {
		complete = "yes"
	}
	var sb strings.Builder
	sb.WriteString("\n## Turn ")
	sb.WriteString(strconv.Itoa(turn))
	sb.WriteString(" - ")
	sb.WriteString(a.name)
	sb.WriteString("\n\n**Complete**: ")
	sb.WriteString(complete)
	sb.WriteString("\n")
	if b.blocker != "" {
		sb.WriteString("\n**Blocked on**: ")
		sb.WriteString(b.blocker)
		sb.WriteString("\n")
	}
	sb.WriteString("\n---\n")

	f, err := os.OpenFile(a.tl.Path(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err := f.WriteString(sb.String()); err != nil {
		panic(err)
	}
}

// headlessView drains the event channel and returns immediately — the test
// stand-in for the bubbletea TUI. Draining is required so the agent goroutine
// (which pushes events then closes the channel via onStreamDone) never blocks.
func headlessView(eventCh <-chan agent.StreamEvent, _ context.CancelFunc, _, _ string, _, _ int, _ time.Time, _ string) (string, bool) {
	for range eventCh {
	}
	return "", false
}

// newScriptedOrchestrator wires up an orchestrator with two scripted agents and
// a headless view runner. populateSpec controls whether the pre-turn loop will
// move the spec out of DRAFT.
func newScriptedOrchestrator(t *testing.T, maxTurns int, populateSpec bool, script map[int]turnBehavior) *Orchestrator {
	t.Helper()
	cfg := Config{
		WorkDir:      t.TempDir(),
		Prompt:       "build the thing",
		Mode:         "dev",
		MaxTurns:     maxTurns,
		AgentBackend: "claude",
	}
	o, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alpha := &scriptedAgent{name: "Agent Alpha", tl: o.taskLog, script: script, populates: populateSpec}
	beta := &scriptedAgent{name: "Agent Beta", tl: o.taskLog, script: script, populates: populateSpec}
	o.agents = []agent.Agent{alpha, beta}
	o.runView = headlessView
	return o
}

func TestRunReachesConsensus(t *testing.T) {
	// Turn 1 (alpha): does the work, marks criteria, not yet complete.
	// Turn 2 (beta): verifies, complete.
	// Turn 3 (alpha): confirms, complete → last two (2,3) both yes → consensus.
	script := map[int]turnBehavior{
		1: {markAllCriteria: true, complete: false},
		2: {complete: true},
		3: {complete: true},
	}
	o := newScriptedOrchestrator(t, 10, true, script)

	if err := o.Run(); err != nil {
		t.Fatalf("Run returned error, want nil (consensus): %v", err)
	}

	turns, err := o.taskLog.ParseTurns()
	if err != nil {
		t.Fatalf("ParseTurns: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("expected relay to stop at 3 turns, got %d", len(turns))
	}

	agreed, err := o.taskLog.BothAgentsAgreeComplete()
	if err != nil {
		t.Fatalf("BothAgentsAgreeComplete: %v", err)
	}
	if !agreed {
		t.Error("expected last two turns to agree complete")
	}
	checked, err := o.taskLog.SpecAllCriteriaChecked()
	if err != nil {
		t.Fatalf("SpecAllCriteriaChecked: %v", err)
	}
	if !checked {
		t.Error("expected all spec criteria checked at consensus")
	}
}

func TestRunExhaustsMaxTurns(t *testing.T) {
	// Never two consecutive "yes": alternating no/yes never lands two yes in a
	// row, so consensus is impossible and the loop must hit MaxTurns.
	script := map[int]turnBehavior{
		1: {complete: false},
		2: {complete: true},
		3: {complete: false},
		4: {complete: true},
	}
	o := newScriptedOrchestrator(t, 4, true, script)

	if err := o.Run(); err != nil {
		t.Fatalf("Run returned error, want nil (max turns): %v", err)
	}

	turns, err := o.taskLog.ParseTurns()
	if err != nil {
		t.Fatalf("ParseTurns: %v", err)
	}
	if len(turns) != 4 {
		t.Errorf("expected 4 turns (max), got %d", len(turns))
	}
}

func TestRunSurfacesBlocked(t *testing.T) {
	// Turn 1 escalates to the human via **Blocked on**.
	script := map[int]turnBehavior{
		1: {complete: false, blocker: "need a human decision on auth flow"},
	}
	o := newScriptedOrchestrator(t, 10, true, script)

	err := o.Run()
	if err == nil {
		t.Fatal("expected ErrAgentBlocked, got nil")
	}
	var blocked *ErrAgentBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *ErrAgentBlocked, got %T: %v", err, err)
	}
	if !strings.Contains(blocked.Reason, "auth flow") {
		t.Errorf("blocker reason = %q, want it to mention auth flow", blocked.Reason)
	}
}

func TestRunFailsWhenPreturnLeavesSpecDraft(t *testing.T) {
	// Agents that never populate the spec leave it DRAFT — the orchestrator must
	// refuse to proceed into the relay.
	o := newScriptedOrchestrator(t, 10, false, map[int]turnBehavior{})

	err := o.Run()
	if err == nil {
		t.Fatal("expected error when spec stays DRAFT, got nil")
	}
	if !strings.Contains(err.Error(), "DRAFT") {
		t.Errorf("error = %v, want it to mention DRAFT", err)
	}
}
