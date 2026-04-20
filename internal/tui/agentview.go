package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/manurgdev/departai/internal/agent"
)

// ── public API ──────────────────────────────────────────────────────────────

// AutoContinueDelay is how long the TUI waits after the agent finishes before
// auto-continuing to the next turn. Press any key to enter review mode instead.
const AutoContinueDelay = 5 * time.Second

// RunAgentView launches a bubbletea program (alt-screen) that displays streaming
// agent events with a pinned header. It blocks until the agent finishes AND
// either the auto-continue countdown expires or the user exits review mode.
//
// After bubbletea exits, a compact summary is printed to the normal terminal
// so the turn activity persists in scroll-back history.
func RunAgentView(
	eventCh <-chan agent.StreamEvent,
	cancelAgent context.CancelFunc,
	agentName, model string,
	turn, maxTurns int,
	taskStart time.Time,
) (result string, stopped bool) {
	m := newModel(eventCh, cancelAgent, agentName, model, turn, maxTurns, taskStart)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, _ := p.Run()
	fm := final.(Model)

	// Print persistent summary to normal terminal after alt-screen closes.
	printFinalSummary(fm)

	return fm.result, fm.stopped
}

// ── model ───────────────────────────────────────────────────────────────────

type phase int

const (
	phaseStreaming  phase = iota // agent is working
	phaseCountdown              // agent done, counting down to auto-continue
	phaseReview                 // user interrupted countdown, browsing events
)

type entry struct {
	kind     string // "text" or "tool"
	title    string // one-line display text
	detail   string // expandable content (diff for Edit)
	expanded bool
}

// Model is the bubbletea model for the agent turn view.
type Model struct {
	entries   []entry
	toolIdx   []int // indices into entries that are tool entries (for cursor nav)
	cursor    int   // position in toolIdx
	phase     phase
	viewport  viewport.Model
	ready     bool // viewport initialised after first WindowSizeMsg

	eventCh   <-chan agent.StreamEvent
	agentName string
	model     string
	turn      int
	maxTurns  int
	result      string
	startTime   time.Time // when this turn started
	elapsed     time.Duration
	taskStart   time.Time // when the entire task started (across all turns)
	cancelAgent context.CancelFunc
	stopped     bool // true if user pressed ESC to stop

	countdownLeft time.Duration
	spinnerFrame  int
}

// spinnerChars are the frames for the footer spinner animation.
var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func newModel(
	ch <-chan agent.StreamEvent,
	cancel context.CancelFunc,
	agentName, model string,
	turn, maxTurns int,
	taskStart time.Time,
) Model {
	return Model{
		eventCh:     ch,
		cancelAgent: cancel,
		agentName:   agentName,
		model:       model,
		turn:        turn,
		maxTurns:    maxTurns,
		phase:       phaseStreaming,
		cursor:      0,
		startTime:   time.Now(),
		taskStart:   taskStart,
	}
}

// ── messages ────────────────────────────────────────────────────────────────

type eventMsg agent.StreamEvent
type channelClosedMsg struct{}
type countdownTickMsg struct{}
type elapsedTickMsg struct{}

func waitForEvent(ch <-chan agent.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return channelClosedMsg{}
		}
		return eventMsg(evt)
	}
}

func countdownTick() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg { return countdownTickMsg{} })
}

func elapsedTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(_ time.Time) tea.Msg { return elapsedTickMsg{} })
}

// ── Init / Update / View ────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.eventCh), elapsedTick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		headerH := 3 // rule + title + rule
		footerH := 2 // blank + help line
		vpH := msg.Height - headerH - footerH
		if vpH < 1 {
			vpH = 1
		}
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpH)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpH
		}
		m.rebuildContent()
		return m, nil

	case eventMsg:
		m.addEvent(agent.StreamEvent(msg))
		m.rebuildContent()
		return m, waitForEvent(m.eventCh)

	case channelClosedMsg:
		m.elapsed = time.Since(m.startTime)
		if m.stopped {
			// User pressed ESC — exit immediately, no countdown.
			m.rebuildContent()
			return m, tea.Quit
		}
		m.phase = phaseCountdown
		m.countdownLeft = AutoContinueDelay
		if len(m.toolIdx) > 0 {
			m.cursor = 0
		}
		m.rebuildContent()
		return m, countdownTick()

	case elapsedTickMsg:
		if m.phase == phaseStreaming {
			m.elapsed = time.Since(m.startTime)
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerChars)
			return m, elapsedTick()
		}
		return m, nil

	case countdownTickMsg:
		if m.phase != phaseCountdown {
			return m, nil
		}
		m.countdownLeft -= time.Second
		if m.countdownLeft <= 0 {
			return m, tea.Quit
		}
		return m, countdownTick()

	case tea.KeyMsg:
		switch m.phase {
		case phaseStreaming:
			if msg.String() == "esc" {
				m.stopped = true
				if m.cancelAgent != nil {
					m.cancelAgent() // kill agent → eventCh will close
				}
				return m, nil // wait for channelClosedMsg
			}
			return m, nil

		case phaseCountdown:
			// Any key → enter review mode, cancel auto-continue.
			m.phase = phaseReview
			m.rebuildContent()
			return m, nil

		case phaseReview:
			switch msg.String() {
			case "q", "esc":
				return m, tea.Quit
			case "up", "k":
				m.moveCursor(-1)
				m.rebuildContent()
			case "down", "j":
				m.moveCursor(1)
				m.rebuildContent()
			case "enter", " ":
				m.toggleExpand()
				m.rebuildContent()
			}
			return m, nil
		}
	}

	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	return header + "\n" + m.viewport.View() + "\n" + footer
}

// ── content rendering ───────────────────────────────────────────────────────

func (m *Model) rebuildContent() {
	if !m.ready {
		return
	}

	var b strings.Builder

	selectedEntryIdx := -1
	if m.phase != phaseStreaming && m.cursor >= 0 && m.cursor < len(m.toolIdx) {
		selectedEntryIdx = m.toolIdx[m.cursor]
	}

	for i, e := range m.entries {
		switch e.kind {
		case "text":
			for _, line := range strings.Split(e.title, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					b.WriteString(styleFaint.Render("  "+trimmed) + "\n")
				}
			}
		case "tool":
			isSelected := i == selectedEntryIdx
			if isSelected {
				b.WriteString(styleSelectedMarker.Render("  ▹ ") + styleSelectedTool.Render(e.title) + "\n")
			} else {
				b.WriteString(styleBoldCyn.Render("  ▸ ") + styleBold.Render(e.title) + "\n")
			}
			if e.expanded && e.detail != "" {
				for _, line := range strings.Split(e.detail, "\n") {
					b.WriteString("      " + line + "\n")
				}
			}
		}
	}

	if m.result != "" && m.phase != phaseStreaming {
		b.WriteString("\n")
		b.WriteString(styleGreen.Render("  ✓ Result:") + "\n")
		for _, line := range strings.Split(m.result, "\n") {
			b.WriteString(styleFaint.Render("  "+line) + "\n")
		}
	}

	m.viewport.SetContent(b.String())
	if m.phase == phaseStreaming {
		m.viewport.GotoBottom()
	}
}

func (m *Model) addEvent(evt agent.StreamEvent) {
	switch evt.Kind {
	case "text":
		m.entries = append(m.entries, entry{kind: "text", title: evt.Text})
	case "tool":
		title := evt.Tool
		if evt.Detail != "" {
			title += " " + evt.Detail
		}
		m.entries = append(m.entries, entry{
			kind:   "tool",
			title:  title,
			detail: buildToolDetail(evt),
		})
		m.toolIdx = append(m.toolIdx, len(m.entries)-1)
	case "result":
		m.result = evt.Result
	}
}

func (m *Model) moveCursor(delta int) {
	if len(m.toolIdx) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.toolIdx) {
		m.cursor = len(m.toolIdx) - 1
	}
}

func (m *Model) toggleExpand() {
	if m.cursor < 0 || m.cursor >= len(m.toolIdx) {
		return
	}
	idx := m.toolIdx[m.cursor]
	m.entries[idx].expanded = !m.entries[idx].expanded
}

// ── header / footer ─────────────────────────────────────────────────────────

func (m Model) renderHeader() string {
	modelStr := m.model
	if modelStr == "" {
		modelStr = "(default)"
	}
	elapsed := m.elapsed.Round(time.Second).String()

	var turnLabel string
	if m.maxTurns > 0 {
		turnLabel = fmt.Sprintf("Turn %d/%d", m.turn, m.maxTurns)
	} else {
		turnLabel = fmt.Sprintf("Turn %d", m.turn)
	}
	title := fmt.Sprintf("  %s  •  %s  •  %s", turnLabel, m.agentName, modelStr)

	if m.phase == phaseStreaming {
		title += styleFaint.Render(fmt.Sprintf("  (%s)", elapsed))
	} else {
		title += styleGreen.Render(fmt.Sprintf("  (%s)", elapsed))
	}

	return styleRule.Render("  "+rule) + "\n" +
		styleBold.Render(title) + "\n" +
		styleRule.Render("  "+rule)
}

func (m Model) renderFooter() string {
	switch m.phase {
	case phaseStreaming:
		totalElapsed := time.Since(m.taskStart).Round(time.Second)
		spinner := styleBoldCyn.Render(spinnerChars[m.spinnerFrame])
		return fmt.Sprintf("  %s %s  %s",
			spinner,
			styleFaint.Render("working..."),
			styleFaint.Render(fmt.Sprintf("(total %s)", totalElapsed)))
	case phaseCountdown:
		secs := int(m.countdownLeft.Seconds())
		if secs < 1 {
			secs = 1
		}
		return styleFaint.Render(fmt.Sprintf(
			"  auto-continue in %ds — press any key to review", secs))
	case phaseReview:
		return styleFaint.Render("  ↑/↓ navigate • enter expand/collapse • q continue")
	}
	return ""
}

// ── post-TUI summary ────────────────────────────────────────────────────────

// printFinalSummary prints a compact record to the normal terminal after
// alt-screen closes. This persists in the user's scroll-back history.
func printFinalSummary(m Model) {
	modelStr := m.model
	if modelStr == "" {
		modelStr = "(default)"
	}
	elapsed := m.elapsed.Round(time.Second)

	fmt.Println(styleRule.Render("  " + rule))
	var turnLabel string
	if m.maxTurns > 0 {
		turnLabel = fmt.Sprintf("Turn %d/%d", m.turn, m.maxTurns)
	} else {
		turnLabel = fmt.Sprintf("Turn %d", m.turn)
	}
	fmt.Println(styleBold.Render(fmt.Sprintf(
		"  %s  •  %s  •  %s", turnLabel, m.agentName, modelStr)) +
		styleGreen.Render(fmt.Sprintf("  (%s)", elapsed)))
	fmt.Println(styleRule.Render("  " + rule))

	// Print all entries — text (reasoning) and tool calls.
	for _, e := range m.entries {
		switch e.kind {
		case "text":
			for _, line := range strings.Split(e.title, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					fmt.Println(styleFaint.Render("  " + trimmed))
				}
			}
		case "tool":
			fmt.Println(styleBoldCyn.Render("  → ") + styleFaint.Render(e.title))
		}
	}

	// Print full result.
	if m.result != "" {
		fmt.Println()
		fmt.Println(styleGreen.Render("  ✓ Result:"))
		for _, line := range strings.Split(m.result, "\n") {
			fmt.Println(styleFaint.Render("  " + line))
		}
	}
	fmt.Println()
}

// ── tool detail builders ────────────────────────────────────────────────────

func buildToolDetail(evt agent.StreamEvent) string {
	if evt.Tool == "Edit" && (evt.DiffOld != "" || evt.DiffNew != "") {
		return formatDiff(evt.DiffOld, evt.DiffNew)
	}
	return ""
}

func formatDiff(old, new string) string {
	var b strings.Builder
	for _, line := range strings.Split(old, "\n") {
		b.WriteString(styleDiffDel.Render("- "+line) + "\n")
	}
	for _, line := range strings.Split(new, "\n") {
		b.WriteString(styleDiffAdd.Render("+ "+line) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

