// Custom REPL bubbletea Model: a multi-line textarea for input plus an
// inline popover that shows filtered slash-command suggestions live as
// the user types. Replaces bubbline's modal autocomplete menu with a
// non-intrusive popover (Claude Code / Codex style): the input stays
// active while the popover is visible, and Tab / arrows navigate the
// popover only when the user opts in.

package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// errCanceled is returned by runREPL when the user pressed Ctrl+C while a
// line was in-flight. The caller treats it like ErrInterrupted: print "^C",
// continue the REPL.
var errCanceled = errors.New("input canceled")

// replModel is the bubbletea Model driving each read cycle of the REPL.
type replModel struct {
	textarea    textarea.Model
	prompt      string
	promptWidth int // visible width of the prompt prefix; subtracted from textarea Width for wrap calculations

	// Popover state.
	popoverVisible  bool
	popoverItems    []suggestion
	popoverSelected int // -1 = visible but no row highlighted; >=0 = active selection
	popoverScroll   int // index of the first visible item (window of popoverMaxItems)
	popoverWordFrom int // byte offset in the current line where the suggestion replaces from
	popoverWordTo   int // byte offset where it replaces to

	// History navigation. historyIdx == -1 means we are not currently
	// navigating history (the buffer holds the user's typed draft).
	history      []string
	historyIdx   int
	historyDraft string

	// Result flags consumed by runREPL after the program returns.
	submitted bool
	canceled  bool
	eof       bool

	// Layout.
	width int
}

// newREPLModel constructs a model ready to be passed to tea.NewProgram.
// `history` is the snapshot of past entries (oldest first); the caller is
// responsible for persisting any new entries after the line is submitted.
func newREPLModel(history []string, prompt string) *replModel {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.Focus()
	// Hide the textarea's own placeholder/styles cruft — we handle visuals.
	ta.Placeholder = ""
	// Start as a single-line input. The textarea grows vertically as the user
	// types content that overflows the width (default MaxHeight = 99 keeps
	// the growth bounded). Without this, textarea.New() defaults to 6 visible
	// rows and the prompt appears stacked.
	ta.SetHeight(1)
	// Cyan prompt prefix. Applied via the textarea's prompt style instead of
	// embedded ANSI in the prompt string itself — otherwise the textarea's
	// width measurement (uniseg.StringWidth) counts the ANSI bytes and breaks
	// layout when content wraps.
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	ta.FocusedStyle.Prompt = cyan
	ta.BlurredStyle.Prompt = cyan
	// Render the prompt prefix only on the first visual line. Continuation
	// lines (after a soft wrap or a Shift+Enter newline) get an aligned pad
	// of equal width so the input column lines up with the first row.
	promptW := len(prompt)
	pad := strings.Repeat(" ", promptW)
	ta.SetPromptFunc(promptW, func(line int) string {
		if line == 0 {
			return prompt
		}
		return pad
	})

	return &replModel{
		textarea:        ta,
		prompt:          prompt,
		promptWidth:     promptW,
		popoverSelected: -1,
		history:         history,
		historyIdx:      -1,
	}
}

func (m *replModel) Init() tea.Cmd {
	// Enable bracketed paste so multi-character pastes arrive as a single
	// tea.KeyMsg with Paste=true, instead of one event per character. This
	// keeps newlines inside pasted text from accidentally submitting the
	// input.
	return tea.Batch(textarea.Blink, tea.EnableBracketedPaste)
}

func (m *replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		// Leave room for the prompt prefix and a small margin.
		w := msg.Width - 2
		if w < 20 {
			w = 20
		}
		m.textarea.SetWidth(w)
		m.syncHeight()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// handleKey routes keystrokes between popover navigation, history navigation,
// and the underlying textarea.
func (m *replModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.canceled = true
		return m, tea.Quit

	case tea.KeyCtrlD:
		if m.textarea.Value() == "" {
			m.eof = true
			return m, tea.Quit
		}
		// Non-empty buffer: let textarea handle (delete-forward).
		return m.delegate(msg)

	case tea.KeyEsc:
		if m.popoverVisible {
			m.popoverVisible = false
			m.popoverSelected = -1
			return m, nil
		}
		return m.delegate(msg)

	case tea.KeyTab:
		if !m.popoverVisible || len(m.popoverItems) == 0 {
			return m, nil // swallow tab when nothing to complete
		}
		if m.popoverSelected < 0 {
			m.popoverSelected = 0
			return m, nil
		}
		m.acceptSelectedSuggestion()
		return m, nil

	case tea.KeyEnter:
		// Multi-line via Shift+Enter when the terminal distinguishes it via
		// the kitty keyboard protocol (kitty, iTerm2 with CSI u, alacritty,
		// ghostty, WezTerm) or via Alt+Enter (universal fallback).
		if msg.Alt || msg.String() == "shift+enter" {
			m.textarea.InsertRune('\n')
			m.syncHeight()
			m.refreshPopover()
			return m, nil
		}
		if m.popoverVisible && m.popoverSelected >= 0 {
			m.acceptSelectedSuggestion()
			return m, nil
		}
		// Plain Enter → submit. Trim the buffer for the empty-line case.
		m.submitted = true
		return m, tea.Quit

	case tea.KeyCtrlJ:
		// Warp (and some other terminals) send LF (\n / Ctrl+J) for
		// Shift+Enter instead of the kitty `\x1b[13;2u` sequence. bubbletea
		// reports LF as KeyCtrlJ. Treat it as the same multi-line action.
		m.textarea.InsertRune('\n')
		m.syncHeight()
		m.refreshPopover()
		return m, nil

	case tea.KeyUp:
		if m.popoverVisible && m.popoverSelected >= 0 {
			if m.popoverSelected > 0 {
				m.popoverSelected--
				if m.popoverSelected < m.popoverScroll {
					m.popoverScroll = m.popoverSelected
				}
			}
			return m, nil
		}
		if m.textarea.Line() == 0 {
			m.historyPrev()
			return m, nil
		}
		return m.delegate(msg)

	case tea.KeyDown:
		if m.popoverVisible && m.popoverSelected >= 0 {
			if m.popoverSelected < len(m.popoverItems)-1 {
				m.popoverSelected++
				if m.popoverSelected >= m.popoverScroll+popoverMaxItems {
					m.popoverScroll = m.popoverSelected - popoverMaxItems + 1
				}
			}
			return m, nil
		}
		if m.textarea.Line() == m.textarea.LineCount()-1 {
			m.historyNext()
			return m, nil
		}
		return m.delegate(msg)
	}

	// Default: pass to textarea, then re-filter suggestions.
	return m.delegate(msg)
}

// delegate forwards the message to the textarea and refreshes popover state
// based on the (possibly updated) buffer.
func (m *replModel) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.syncHeight()
	m.refreshPopover()
	// Any edit invalidates history navigation — return to "draft" mode.
	if _, ok := msg.(tea.KeyMsg); ok {
		if m.historyIdx != -1 {
			m.historyIdx = -1
		}
	}
	return m, cmd
}

// syncHeight grows or shrinks the textarea's visible height so it matches
// the number of visual rows the current content needs after wrapping. Without
// this, a fixed Height = 1 causes the textarea to scroll horizontally when
// content overflows the width — defeating the purpose of multi-line input.
//
// The wrap point inside the textarea is `Width - promptWidth` (content area
// excludes the prompt prefix), so we subtract promptWidth here too — using
// the full Width would underestimate the row count for content that fits in
// the outer width but overflows the inner content area.
//
// Capped at 20 visual rows; very long pastes still scroll vertically inside
// the textarea but the prompt area doesn't take over the whole screen.
func (m *replModel) syncHeight() {
	contentWidth := m.textarea.Width() - m.promptWidth
	if contentWidth <= 0 {
		return
	}
	h := visualHeight(m.textarea.Value(), contentWidth)
	if h < 1 {
		h = 1
	}
	if h > 20 {
		h = 20
	}
	if m.textarea.Height() != h {
		m.textarea.SetHeight(h)
	}
}

// visualHeight returns the number of terminal rows the value will occupy at
// the given width, accounting for wrapping inside each logical line. ASCII-
// width approximation: counts runes, doesn't handle double-width characters.
func visualHeight(value string, width int) int {
	if width < 1 {
		width = 1
	}
	total := 0
	for _, line := range strings.Split(value, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			total++
			continue
		}
		total += (len(runes) + width - 1) / width
	}
	if total == 0 {
		return 1
	}
	return total
}

// refreshPopover recomputes the suggestion list based on the cursor's current
// position. Always resets the selection to "no row highlighted" so the menu
// doesn't auto-grab focus while the user types.
func (m *replModel) refreshPopover() {
	items, wordFrom, wordTo := m.computeSuggestions()
	if len(items) == 0 {
		m.popoverVisible = false
		m.popoverItems = nil
		m.popoverSelected = -1
		return
	}
	m.popoverVisible = true
	m.popoverItems = items
	m.popoverSelected = -1
	m.popoverScroll = 0 // reset window when the filter changes
	m.popoverWordFrom = wordFrom
	m.popoverWordTo = wordTo
}

// computeSuggestions inspects the current line and decides which suggestion
// list to filter, returning the matched candidates along with the byte range
// in the current line that an accepted suggestion would replace.
func (m *replModel) computeSuggestions() (items []suggestion, wordFrom, wordTo int) {
	line := m.currentLine()
	col := m.textarea.LineInfo().ColumnOffset
	if col > len(line) {
		col = len(line)
	}
	textBefore := line[:col]

	if !strings.HasPrefix(textBefore, "/") {
		return nil, 0, 0
	}

	// Locate the "word" under the cursor: contiguous non-space chars up to col.
	wstart := col
	for wstart > 0 && line[wstart-1] != ' ' {
		wstart--
	}
	wend := col
	for wend < len(line) && line[wend] != ' ' {
		wend++
	}
	word := line[wstart:wend]

	switch {
	case strings.HasPrefix(textBefore, "/config save "):
		return filterByPrefix(configSaveTargets, word), wstart, wend

	case strings.HasPrefix(textBefore, "/config set "):
		rest := strings.TrimPrefix(textBefore, "/config set ")
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) == 1 {
			return filterByPrefix(configSetKeys, word), wstart, wend
		}
		switch parts[0] {
		case "model", "model.alpha", "model.beta":
			return filterByPrefix(modelValueSuggestions, word), wstart, wend
		}
		return nil, 0, 0

	case strings.HasPrefix(textBefore, "/config "):
		return filterByPrefix(configSubcommands, word), wstart, wend

	case strings.HasPrefix(textBefore, "/model alpha "), strings.HasPrefix(textBefore, "/model beta "):
		return filterByPrefix(modelValueSuggestions, word), wstart, wend

	case strings.HasPrefix(textBefore, "/model "):
		return filterByPrefix(modelSubcommands, word), wstart, wend
	}

	// Top-level: match the entire textBefore (which always starts with `/`)
	// against full command names. Replacement covers the whole token.
	return filterByPrefix(topLevelCommands, textBefore), 0, wend
}

// filterByPrefix returns the candidates whose Title has the given prefix
// (case-insensitive).
func filterByPrefix(items []suggestion, prefix string) []suggestion {
	if prefix == "" {
		out := make([]suggestion, len(items))
		copy(out, items)
		return out
	}
	lp := strings.ToLower(prefix)
	var out []suggestion
	for _, s := range items {
		if strings.HasPrefix(strings.ToLower(s.text), lp) {
			out = append(out, s)
		}
	}
	return out
}

// acceptSelectedSuggestion splices the highlighted suggestion into the buffer,
// replacing the word at the cursor. After insertion, the popover dismisses;
// the user can either continue editing or press Enter to submit.
func (m *replModel) acceptSelectedSuggestion() {
	if m.popoverSelected < 0 || m.popoverSelected >= len(m.popoverItems) {
		return
	}
	picked := m.popoverItems[m.popoverSelected].text

	value := m.textarea.Value()
	// Locate the start of the current line within the full value to translate
	// our (line-local) wordFrom/wordTo offsets to global offsets.
	lineStart, lineEnd := m.currentLineBounds(value)
	line := value[lineStart:lineEnd]
	from := lineStart + m.popoverWordFrom
	to := lineStart + m.popoverWordTo
	if to > lineStart+len(line) {
		to = lineStart + len(line)
	}

	newValue := value[:from] + picked + " " + value[to:]
	m.textarea.SetValue(newValue)

	// Place the cursor right after the inserted token + space.
	// textarea doesn't expose a direct "set absolute cursor" method, so we
	// settle for the cursor being at end-of-line which is generally correct
	// here because suggestions are inserted at or near the cursor.
	m.textarea.CursorEnd()

	m.popoverVisible = false
	m.popoverSelected = -1
	m.syncHeight()
}

// currentLine returns the text of the line the textarea cursor is on.
func (m *replModel) currentLine() string {
	value := m.textarea.Value()
	start, end := m.currentLineBounds(value)
	return value[start:end]
}

// currentLineBounds returns the [start, end) byte range of the current line
// within the full textarea value.
func (m *replModel) currentLineBounds(value string) (start, end int) {
	lineNum := m.textarea.Line()
	cur := 0
	for i := 0; i < lineNum; i++ {
		nl := strings.IndexByte(value[cur:], '\n')
		if nl < 0 {
			cur = len(value)
			break
		}
		cur += nl + 1
	}
	start = cur
	end = len(value)
	if nl := strings.IndexByte(value[cur:], '\n'); nl >= 0 {
		end = cur + nl
	}
	return start, end
}

// historyPrev replaces the buffer with the previous history entry (older).
func (m *replModel) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIdx == -1 {
		m.historyDraft = m.textarea.Value()
		m.historyIdx = len(m.history) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	} else {
		return // already at oldest
	}
	m.textarea.SetValue(m.history[m.historyIdx])
	m.textarea.CursorEnd()
	m.syncHeight()
	m.refreshPopover()
}

// historyNext moves toward newer entries; falls off the end into the saved
// draft.
func (m *replModel) historyNext() {
	if m.historyIdx == -1 {
		return
	}
	if m.historyIdx < len(m.history)-1 {
		m.historyIdx++
		m.textarea.SetValue(m.history[m.historyIdx])
	} else {
		m.historyIdx = -1
		m.textarea.SetValue(m.historyDraft)
	}
	m.textarea.CursorEnd()
	m.syncHeight()
	m.refreshPopover()
}

// ── render ──────────────────────────────────────────────────────────────────

// Popover styling (declared at package level so they're constructed once).
var (
	popoverBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("8")).
				Padding(0, 1)
	popoverItemStyle     = lipgloss.NewStyle()
	popoverSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("6")).
				Bold(true)
	popoverDescStyle = lipgloss.NewStyle().Faint(true)
)

const popoverMaxItems = 8

func (m *replModel) View() string {
	out := m.textarea.View()
	if m.popoverVisible && len(m.popoverItems) > 0 {
		out += "\n" + m.renderPopover()
	}
	return out
}

func (m *replModel) renderPopover() string {
	total := len(m.popoverItems)
	start := m.popoverScroll
	end := start + popoverMaxItems
	if end > total {
		end = total
	}
	visible := m.popoverItems[start:end]

	// Compute the longest text column among visible items for alignment.
	maxText := 0
	for _, s := range visible {
		if len(s.text) > maxText {
			maxText = len(s.text)
		}
	}

	var rows []string

	// Top scroll indicator: shown when there are items above the visible window.
	if start > 0 {
		rows = append(rows, popoverDescStyle.Render(fmt.Sprintf(" ↑ %d more above", start)))
	}

	for i, s := range visible {
		text := s.text
		for len(text) < maxText {
			text += " "
		}
		actualIdx := start + i
		var row string
		if actualIdx == m.popoverSelected {
			row = popoverSelectedStyle.Render(fmt.Sprintf(" %s   %s ", text, s.desc))
		} else {
			row = popoverItemStyle.Render(fmt.Sprintf(" %s   %s ", text, popoverDescStyle.Render(s.desc)))
		}
		rows = append(rows, row)
	}

	// Bottom scroll indicator: items below the visible window.
	if remainder := total - end; remainder > 0 {
		rows = append(rows, popoverDescStyle.Render(fmt.Sprintf(" ↓ %d more below", remainder)))
	}

	return popoverBorderStyle.Render(strings.Join(rows, "\n"))
}

// ── runner ─────────────────────────────────────────────────────────────────

// runREPL launches a single read cycle on the model and returns the submitted
// line. Returns io.EOF when the user pressed Ctrl+D on an empty buffer, or
// errCanceled when they pressed Ctrl+C.
func runREPL(history []string, prompt string) (string, error) {
	m := newREPLModel(history, prompt)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", err
	}
	rm, ok := final.(*replModel)
	if !ok {
		return "", fmt.Errorf("unexpected model type from tea.Program")
	}
	if rm.eof {
		return "", io.EOF
	}
	if rm.canceled {
		return "", errCanceled
	}
	if !rm.submitted {
		// Defensive: shouldn't happen.
		return "", io.EOF
	}
	return rm.textarea.Value(), nil
}
