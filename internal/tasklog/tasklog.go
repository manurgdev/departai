// Package tasklog manages the shared task directory and turn log file
// that agents use to hand off context between turns.
package tasklog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	logFileName  = "task-log.md"
	specFileName = "spec.md"
)

// TaskLog manages the shared markdown log file inside the task directory.
type TaskLog struct {
	TaskID string
	Dir    string // absolute path to the task directory
	Prompt string

	// Recovered holds non-fatal recovery notes produced by Load when it had to
	// repair a corrupt task log. Empty on a clean load. The caller (orchestrator)
	// surfaces these to the user so they know the original was backed up.
	Recovered []string
}

// New creates the task directory under baseDir/.departai/tasks/<taskID>
// and writes the initial log file with the task prompt.
func New(baseDir, prompt string) (*TaskLog, error) {
	taskID := generateTaskID(prompt)
	taskDir := filepath.Join(baseDir, ".departai", "tasks", taskID)

	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return nil, fmt.Errorf("creating task directory %s: %w", taskDir, err)
	}

	tl := &TaskLog{
		TaskID: taskID,
		Dir:    taskDir,
		Prompt: prompt,
	}

	if err := tl.initialize(); err != nil {
		return nil, fmt.Errorf("initializing task log: %w", err)
	}

	if err := tl.initializeSpec(); err != nil {
		return nil, fmt.Errorf("initializing spec: %w", err)
	}

	return tl, nil
}

// Load opens an existing task directory and returns its TaskLog.
// It reads the task-log.md to extract the original prompt. If the task
// predates the spec.md feature, a DRAFT spec template is created so the
// pre-turn loop can populate it on the next run.
func Load(taskDir string) (*TaskLog, error) {
	taskID := filepath.Base(taskDir)
	logPath := filepath.Join(taskDir, logFileName)

	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, fmt.Errorf("reading task log %s: %w", logPath, err)
	}
	content := string(data)

	tl := &TaskLog{
		TaskID: taskID,
		Dir:    taskDir,
	}

	// Integrity check: if the log is malformed (truncated header, lost
	// Original Task, invalid UTF-8), back up the original and rebuild a usable
	// version instead of proceeding with garbage or failing opaquely.
	if note, repaired, rerr := tl.recoverIfCorrupt(content); rerr != nil {
		return nil, rerr
	} else if repaired {
		tl.Recovered = append(tl.Recovered, note)
		if data, err = os.ReadFile(logPath); err != nil {
			return nil, fmt.Errorf("re-reading recovered task log %s: %w", logPath, err)
		}
		content = string(data)
	}

	tl.Prompt = extractPrompt(content)

	if err := tl.initializeSpec(); err != nil {
		return nil, fmt.Errorf("initializing spec on load: %w", err)
	}

	return tl, nil
}

// logLooksValid reports whether content has the minimal structure of a healthy
// task log: valid UTF-8, the `# Task Log` header, and an extractable
// `## Original Task` section.
func logLooksValid(content string) bool {
	if !utf8.ValidString(content) {
		return false
	}
	if !strings.Contains(content, "# Task Log") {
		return false
	}
	return extractPrompt(content) != "(unknown)"
}

// recoverableSectionRe finds the first preservable section (a turn entry or a
// user directive) so rebuildLog can keep the real work while replacing a
// damaged header.
var recoverableSectionRe = regexp.MustCompile(`(?m)^## (?:Turn \d+|User Directive)\b`)

// recoverIfCorrupt validates content and, when malformed, backs up the original
// to <log>.corrupt-<timestamp> and rewrites a usable log in place. Returns a
// human-readable note and repaired=true when recovery happened.
func (tl *TaskLog) recoverIfCorrupt(content string) (note string, repaired bool, err error) {
	if logLooksValid(content) {
		return "", false, nil
	}

	logPath := tl.Path()
	backup := fmt.Sprintf("%s.corrupt-%s", logPath, time.Now().Format("20060102-150405"))
	if werr := os.WriteFile(backup, []byte(content), 0644); werr != nil {
		return "", false, fmt.Errorf("backing up corrupt task log: %w", werr)
	}

	if werr := os.WriteFile(logPath, []byte(tl.rebuildLog(content)), 0644); werr != nil {
		return "", false, fmt.Errorf("writing recovered task log: %w", werr)
	}

	note = fmt.Sprintf(
		"task log %q was malformed; recovered a usable version (original backed up to %s)",
		logFileName, filepath.Base(backup),
	)
	return note, true, nil
}

// rebuildLog produces a structurally valid task log: a fresh header (preserving
// the original prompt when still extractable) followed by any recoverable turn
// and directive sections, verbatim.
func (tl *TaskLog) rebuildLog(content string) string {
	prompt := extractPrompt(content)
	if prompt == "(unknown)" {
		prompt = "(original task description was lost to corruption; see the .corrupt backup)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, `# Task Log

**Task ID**: %s
**Started**: %s

## Original Task

%s

---

`, tl.TaskID, time.Now().Format("2006-01-02 15:04:05"), prompt)

	if loc := recoverableSectionRe.FindStringIndex(content); loc != nil {
		b.WriteString(strings.TrimRight(content[loc[0]:], "\n"))
		b.WriteString("\n")
	}

	return b.String()
}

// promptRe extracts text between "## Original Task" and the next "---".
var promptRe = regexp.MustCompile(`(?s)## Original Task\s*\n(.*?)\n---`)

func extractPrompt(content string) string {
	m := promptRe.FindStringSubmatch(content)
	if len(m) < 2 {
		return "(unknown)"
	}
	return strings.TrimSpace(m[1])
}

// ── task listing ────────────────────────────────────────────────────────────

// TaskSummary is a brief overview of a task for display in /resume.
type TaskSummary struct {
	TaskID    string
	Dir       string
	Prompt    string
	TurnCount int
}

// ListTasks scans <workDir>/.departai/tasks/ and returns summaries of all
// existing tasks, sorted by most recent first.
func ListTasks(workDir string) ([]TaskSummary, error) {
	tasksDir := filepath.Join(workDir, ".departai", "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tasks: %w", err)
	}

	var summaries []TaskSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, e.Name())
		logPath := filepath.Join(dir, logFileName)
		data, err := os.ReadFile(logPath)
		if err != nil {
			continue // skip dirs without a valid task log
		}
		content := string(data)
		prompt := extractPrompt(content)
		turns := parseTurns(content)

		summaries = append(summaries, TaskSummary{
			TaskID:    e.Name(),
			Dir:       dir,
			Prompt:    prompt,
			TurnCount: len(turns),
		})
	}

	// Sort newest first (task IDs start with YYYYMMDD-HHMMSS).
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].TaskID > summaries[j].TaskID
	})

	return summaries, nil
}

// AppendUserDirective appends a new user instruction to the task log.
// Agents read this as part of the task log and act on it in subsequent turns.
func (tl *TaskLog) AppendUserDirective(text string) error {
	directive := fmt.Sprintf("\n## User Directive\n\n%s\n\n---\n\n", text)

	f, err := os.OpenFile(tl.Path(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening task log for append: %w", err)
	}
	defer f.Close()

	_, err = f.WriteString(directive)
	return err
}

// Path returns the absolute path to the task log markdown file.
func (tl *TaskLog) Path() string {
	return filepath.Join(tl.Dir, logFileName)
}

// initialize writes the initial task log header. Called once at task start.
func (tl *TaskLog) initialize() error {
	content := fmt.Sprintf(`# Task Log

**Task ID**: %s
**Started**: %s

## Original Task

%s

---

`, tl.TaskID, time.Now().Format("2006-01-02 15:04:05"), tl.Prompt)

	return os.WriteFile(tl.Path(), []byte(content), 0644)
}

// Read returns the full current contents of the task log file.
func (tl *TaskLog) Read() (string, error) {
	data, err := os.ReadFile(tl.Path())
	if err != nil {
		return "", fmt.Errorf("reading task log: %w", err)
	}
	return string(data), nil
}

// WindowedContent returns the task log content with mid-range turns elided to
// keep prompt size bounded as tasks grow long. Always preserved:
//   - The header (everything before the first ## heading)
//   - The `## Original Task` section
//   - All `## User Directive` sections
//   - The last `window` turn entries (## Turn N - Agent)
//
// When `window <= 0` or the total turn count is at or below `window`, the full
// content is returned unchanged. Otherwise an omission marker is inserted just
// before the first kept turn.
//
// Trade-off: User Directives that originally appeared between mid-range turns
// end up grouped with the other non-turn sections (chronological position
// relative to dropped turns is lost). Their content is preserved, which is
// what matters for downstream prompts.
func (tl *TaskLog) WindowedContent(window int) (string, error) {
	content, err := tl.Read()
	if err != nil {
		return "", err
	}
	if window <= 0 {
		return content, nil
	}

	sections := parseLogSections(content)

	var turnIdx []int
	for i, s := range sections {
		if s.Kind == "turn" {
			turnIdx = append(turnIdx, i)
		}
	}
	if len(turnIdx) <= window {
		return content, nil
	}

	// Determine the omitted range using actual turn numbers parsed from the log.
	turns, err := tl.ParseTurns()
	if err != nil {
		return "", err
	}
	if len(turns) <= window {
		return content, nil
	}
	omittedCount := len(turns) - window
	firstOmitted := turns[0].TurnNumber
	lastOmitted := turns[omittedCount-1].TurnNumber
	firstKeptSectionIdx := turnIdx[omittedCount]

	var b strings.Builder
	omissionInserted := false
	for i, s := range sections {
		if s.Kind == "turn" {
			if i < firstKeptSectionIdx {
				continue
			}
			if !omissionInserted {
				if firstOmitted == lastOmitted {
					fmt.Fprintf(&b, "> _Turn %d omitted to keep context bounded — full history in `%s`._\n\n---\n\n", firstOmitted, logFileName)
				} else {
					fmt.Fprintf(&b, "> _Turns %d–%d omitted to keep context bounded — full history in `%s`._\n\n---\n\n", firstOmitted, lastOmitted, logFileName)
				}
				omissionInserted = true
			}
		}
		b.WriteString(s.Body)
	}
	return b.String(), nil
}

// logSection is one chunk of the task log: the preamble (everything before the
// first ## heading), or a single section starting at a `## ...` heading and
// extending to the next heading or end of file.
type logSection struct {
	Kind string // "preamble" | "task" | "directive" | "turn" | "other"
	Body string
}

// sectionHeadingRe matches every line starting with `## `.
var sectionHeadingRe = regexp.MustCompile(`(?m)^## .+$`)

// parseLogSections splits the log by `## ` headings, tagging each section.
func parseLogSections(content string) []logSection {
	matches := sectionHeadingRe.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return []logSection{{Kind: "preamble", Body: content}}
	}

	var sections []logSection
	if matches[0][0] > 0 {
		sections = append(sections, logSection{Kind: "preamble", Body: content[:matches[0][0]]})
	}
	for i, m := range matches {
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := content[m[0]:end]
		heading := content[m[0]:m[1]]
		sections = append(sections, logSection{Kind: classifyHeading(heading), Body: body})
	}
	return sections
}

// classifyHeading inspects a `## ...` line and returns the section kind.
func classifyHeading(heading string) string {
	heading = strings.TrimSpace(heading)
	switch {
	case strings.HasPrefix(heading, "## Original Task"):
		return "task"
	case strings.HasPrefix(heading, "## User Directive"):
		return "directive"
	case strings.HasPrefix(heading, "## Turn "):
		return "turn"
	default:
		return "other"
	}
}

// WriteRawLog saves the activity (tool calls), output, and stderr for a turn
// to a dedicated file named turn-N-<agent>-raw.log in the task directory.
func (tl *TaskLog) WriteRawLog(turnNumber int, agentName string, activity []string, output, stderr string) error {
	filename := fmt.Sprintf("turn-%d-%s-raw.log", turnNumber, sanitizeName(agentName))
	path := filepath.Join(tl.Dir, filename)

	var b strings.Builder
	fmt.Fprintf(&b, "DepartAI Raw Turn Log\n")
	fmt.Fprintf(&b, "=====================\n")
	fmt.Fprintf(&b, "Turn    : %d\n", turnNumber)
	fmt.Fprintf(&b, "Agent   : %s\n", agentName)
	fmt.Fprintf(&b, "Time    : %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "--- ACTIVITY ---\n\n")
	if len(activity) == 0 {
		b.WriteString("  (no tool calls)\n")
	} else {
		for _, entry := range activity {
			fmt.Fprintf(&b, "  → %s\n", entry)
		}
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "--- OUTPUT ---\n\n")
	if output == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(output)
	}
	fmt.Fprintf(&b, "\n\n")

	fmt.Fprintf(&b, "--- STDERR ---\n\n")
	if stderr == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(stderr)
	}
	fmt.Fprintf(&b, "\n")

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// ── per-turn touched-files tracking ────────────────────────────────────────
//
// We persist the set of files an agent modified each turn to a sidecar file
// so the orchestrator can detect (a) writes that fall outside the spec's
// `Files in scope` and (b) oscillation patterns where multiple turns churn
// the same files without progress.
//
// LIMITATION: extraction relies on Claude's per-tool stream events
// (Edit/Write/MultiEdit/NotebookEdit each carry the file path explicitly).
// Codex agents only expose `Bash` with the raw command, so file modifications
// inside bash (cat >, sed -i, etc.) are not captured. Detection works when
// at least one agent is Claude; degrades to no-op when both are Codex.

// touchedFilePrefixes is the set of activity-string prefixes that mean a
// file was modified (not just read).
var touchedFilePrefixes = []string{
	"Edit ",
	"Write ",
	"MultiEdit ",
	"NotebookEdit ",
}

// ExtractTouchedFiles parses agent activity strings and returns the de-duplicated
// list of files that were modified. Activity strings have the form
// `<Tool> <Detail>`; for write-class tools the Detail is the file path.
func ExtractTouchedFiles(activity []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, entry := range activity {
		for _, prefix := range touchedFilePrefixes {
			if strings.HasPrefix(entry, prefix) {
				path := strings.TrimSpace(entry[len(prefix):])
				if path == "" {
					break
				}
				if _, dup := seen[path]; !dup {
					seen[path] = struct{}{}
					out = append(out, path)
				}
				break
			}
		}
	}
	return out
}

// WriteTurnFiles persists the list of files modified by a turn to
// turn-N-<agent>-files.txt in the task directory. Empty list still writes
// the file (zero-length) so detectors can distinguish "no writes" from
// "no data captured".
func (tl *TaskLog) WriteTurnFiles(turnNumber int, agentName string, files []string) error {
	filename := fmt.Sprintf("turn-%d-%s-files.txt", turnNumber, sanitizeName(agentName))
	path := filepath.Join(tl.Dir, filename)

	var content string
	if len(files) > 0 {
		content = strings.Join(files, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// ReadTurnFiles loads the list of files modified by a turn. Returns nil (no
// error) when the sidecar file does not exist — older tasks may predate the
// feature, or both agents may be Codex (see LIMITATION above).
func (tl *TaskLog) ReadTurnFiles(turnNumber int, agentName string) ([]string, error) {
	filename := fmt.Sprintf("turn-%d-%s-files.txt", turnNumber, sanitizeName(agentName))
	path := filepath.Join(tl.Dir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// filesInScopeSectionRe captures the body of "## Files in scope" up to the
// next "## " heading or end of file.
var filesInScopeSectionRe = regexp.MustCompile(`(?ims)^##\s+Files in scope\s*\n(.*?)(?:\n##\s+|\z)`)

// listItemRe matches markdown list items: `- path` (with optional surrounding
// whitespace, no checkbox).
var listItemRe = regexp.MustCompile(`(?m)^\s*-\s+(?:\[[ xX]\]\s+)?(.+?)\s*$`)

// SpecFilesInScope returns the file paths listed under the spec's
// `## Files in scope` section. Returns nil (no error) if the spec is missing
// or the section has no list items (typical for DRAFT specs).
func (tl *TaskLog) SpecFilesInScope() ([]string, error) {
	content, err := tl.ReadSpec()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sec := filesInScopeSectionRe.FindStringSubmatch(content)
	if len(sec) < 2 {
		return nil, nil
	}
	matches := listItemRe.FindAllStringSubmatch(sec[1], -1)
	if len(matches) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		path := strings.TrimSpace(m[1])
		if path == "" {
			continue
		}
		// Strip backticks if the spec wrapped paths in them (`foo.go`).
		path = strings.Trim(path, "`")
		out = append(out, path)
	}
	return out, nil
}

// AppendTimeoutTurn appends a synthetic turn entry to the task log marking
// that the agent's turn was forcibly terminated by the orchestrator after
// exceeding the per-turn duration budget. The synthetic entry has Complete: no
// and a clear Next Steps pointer so the next agent can pick up.
//
// Without this, parseTurns would not see a turn for the killed iteration and
// the orchestrator's turn counter would drift out of sync with the log.
func (tl *TaskLog) AppendTimeoutTurn(turnNumber int, agentName string, duration time.Duration) error {
	entry := fmt.Sprintf(`
## Turn %d - %s

**Working Directory**: (unknown — turn killed by orchestrator)

**Review of previous turn**: (turn killed before review)

**What I did**: (turn killed by orchestrator after exceeding the %s budget — see `+"`turn-%d-%s-raw.log`"+` for the partial activity captured before termination)

**Tests**: (turn killed)

**Current State**: (turn killed)

**Remaining Issues**: (turn killed by timeout — inspect the raw log and current code state to assess)

**Next Steps**: pick up from where this turn was cut off. Read the raw log to see what was done, then continue toward the spec's unchecked Acceptance Criteria.

**Complete**: no

---

`, turnNumber, agentName, duration, turnNumber, sanitizeName(agentName))

	f, err := os.OpenFile(tl.Path(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening task log for timeout entry: %w", err)
	}
	defer f.Close()

	_, err = f.WriteString(entry)
	return err
}

// WriteSpecPreturnLog saves the activity, output and stderr for a spec pre-turn
// to spec-preturn-N-<agent>-raw.log in the task directory.
func (tl *TaskLog) WriteSpecPreturnLog(idx int, agentName string, activity []string, output, stderr string) error {
	filename := fmt.Sprintf("spec-preturn-%d-%s-raw.log", idx, sanitizeName(agentName))
	path := filepath.Join(tl.Dir, filename)

	var b strings.Builder
	fmt.Fprintf(&b, "DepartAI Spec Pre-turn Raw Log\n")
	fmt.Fprintf(&b, "==============================\n")
	fmt.Fprintf(&b, "Pre-turn: %d\n", idx)
	fmt.Fprintf(&b, "Agent   : %s\n", agentName)
	fmt.Fprintf(&b, "Time    : %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "--- ACTIVITY ---\n\n")
	if len(activity) == 0 {
		b.WriteString("  (no tool calls)\n")
	} else {
		for _, entry := range activity {
			fmt.Fprintf(&b, "  → %s\n", entry)
		}
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "--- OUTPUT ---\n\n")
	if output == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(output)
	}
	fmt.Fprintf(&b, "\n\n")

	fmt.Fprintf(&b, "--- STDERR ---\n\n")
	if stderr == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(stderr)
	}
	fmt.Fprintf(&b, "\n")

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// sanitizeName converts an agent name like "Agent Alpha" to "agent-alpha"
// for use in filenames.
func sanitizeName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	s := re.ReplaceAllString(strings.ToLower(name), "-")
	return strings.Trim(s, "-")
}

// workingDirRe matches: **Working Directory**: /some/path
var workingDirRe = regexp.MustCompile(`(?im)^\*\*Working Directory\*\*:\s*(.+)$`)

// ParseLastWorkingDir returns the Working Directory reported in the most recent
// turn entry, or ("", nil) if no turn has reported one yet.
func (tl *TaskLog) ParseLastWorkingDir() (string, error) {
	content, err := tl.Read()
	if err != nil {
		return "", err
	}
	// FindAllStringSubmatch returns all matches; we want the last one.
	matches := workingDirRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return "", nil
	}
	return filepath.Clean(strings.TrimSpace(matches[len(matches)-1][1])), nil
}

// Relocate moves the entire task directory from its current location to
// newBaseDir/.departai/tasks/<taskID> and updates tl.Dir accordingly.
// It is a no-op if the computed destination equals the current location.
//
// Refuses with a clear error if the computed destination would land inside
// the current task directory — this happens when an agent misreports its
// Working Directory as the task dir itself, and would otherwise trigger an
// opaque "invalid argument" error from os.Rename.
func (tl *TaskLog) Relocate(newBaseDir string) error {
	newTaskDir := filepath.Join(newBaseDir, ".departai", "tasks", tl.TaskID)
	newTaskDir = filepath.Clean(newTaskDir)

	if newTaskDir == filepath.Clean(tl.Dir) {
		return nil // already in the right place
	}

	if isInside(tl.Dir, newTaskDir) {
		return fmt.Errorf("refusing to relocate task dir into itself: source %s would contain destination %s — likely the agent reported the task dir as Working Directory instead of the project root", tl.Dir, newTaskDir)
	}

	if err := os.MkdirAll(filepath.Dir(newTaskDir), 0755); err != nil {
		return fmt.Errorf("creating parent for new task dir: %w", err)
	}

	if err := os.Rename(tl.Dir, newTaskDir); err != nil {
		return fmt.Errorf("moving task dir from %s to %s: %w", tl.Dir, newTaskDir, err)
	}

	tl.Dir = newTaskDir
	return nil
}

// isInside returns true when child is strictly under parent in the directory
// tree (not equal to it). Resolves both to absolute paths first; returns
// false on any path resolution error.
func isInside(parent, child string) bool {
	p, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	c, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	p = filepath.Clean(p)
	c = filepath.Clean(c)
	if p == c {
		return false
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != "."
}

// TurnEntry is a parsed representation of a single agent turn in the log.
type TurnEntry struct {
	TurnNumber int
	AgentName  string
	Complete   bool   // agent's self-reported completion status
	Blocker    string // agent's optional **Blocked on** reason (empty if not blocked)
}

// ParseTurns reads and parses all turn entries from the task log.
func (tl *TaskLog) ParseTurns() ([]TurnEntry, error) {
	content, err := tl.Read()
	if err != nil {
		return nil, err
	}
	return parseTurns(content), nil
}

// BothAgentsAgreeComplete returns true when the last two consecutive turns
// both reported **Complete**: yes. This is the consensus condition for stopping.
func (tl *TaskLog) BothAgentsAgreeComplete() (bool, error) {
	turns, err := tl.ParseTurns()
	if err != nil {
		return false, err
	}
	if len(turns) < 2 {
		return false, nil
	}
	last := turns[len(turns)-1]
	secondLast := turns[len(turns)-2]
	return last.Complete && secondLast.Complete, nil
}

// turnHeaderRe matches lines like: ## Turn 3 - Agent Alpha
var turnHeaderRe = regexp.MustCompile(`(?m)^## Turn (\d+) - (.+)$`)

// completeRe matches: **Complete**: yes  or  **Complete**: no  (case-insensitive)
var completeRe = regexp.MustCompile(`(?i)\*\*Complete\*\*:\s*(yes|no)`)

// blockedOnRe matches: **Blocked on**: <reason text up to a blank line, ---,
// next field, next section heading, or end of input>. Case-insensitive.
//
// `[ \t]*` (not `\s*`) on the leading whitespace is intentional: it must NOT
// consume newlines. Otherwise an empty-value case like `**Blocked on**:\n\n---`
// would capture the `---` separator as the value.
var blockedOnRe = regexp.MustCompile(`(?ims)\*\*Blocked on\*\*:[ \t]*(.+?)(?:\n\n|\n---|\n##\s|\n\*\*|\z)`)

func parseTurns(content string) []TurnEntry {
	headers := turnHeaderRe.FindAllStringSubmatchIndex(content, -1)
	if len(headers) == 0 {
		return nil
	}

	entries := make([]TurnEntry, 0, len(headers))
	for i, h := range headers {
		turnNumStr := content[h[2]:h[3]]
		agentName := strings.TrimSpace(content[h[4]:h[5]])

		// Section spans from this header to the next (or end of file).
		sectionEnd := len(content)
		if i+1 < len(headers) {
			sectionEnd = headers[i+1][0]
		}
		section := content[h[0]:sectionEnd]

		complete := false
		if m := completeRe.FindStringSubmatch(section); m != nil {
			complete = strings.EqualFold(strings.TrimSpace(m[1]), "yes")
		}

		blocker := ""
		if m := blockedOnRe.FindStringSubmatch(section); m != nil {
			blocker = strings.TrimSpace(m[1])
		}

		turnNum := 0
		fmt.Sscanf(turnNumStr, "%d", &turnNum)

		entries = append(entries, TurnEntry{
			TurnNumber: turnNum,
			AgentName:  agentName,
			Complete:   complete,
			Blocker:    blocker,
		})
	}
	return entries
}

// ── spec.md ─────────────────────────────────────────────────────────────────

// SpecPath returns the absolute path to the spec markdown file.
func (tl *TaskLog) SpecPath() string {
	return filepath.Join(tl.Dir, specFileName)
}

// ReadSpec returns the full current contents of the spec file.
// Returns os.ErrNotExist if the spec has not been created yet.
func (tl *TaskLog) ReadSpec() (string, error) {
	data, err := os.ReadFile(tl.SpecPath())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// initializeSpec writes the initial DRAFT spec template if one does not already
// exist. Idempotent: called from both New (fresh tasks) and Load (legacy tasks
// created before the spec feature).
func (tl *TaskLog) initializeSpec() error {
	path := tl.SpecPath()
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking spec path: %w", err)
	}

	content := fmt.Sprintf(`# Spec

**Task ID**: %s
**Status**: DRAFT
**Last updated**: %s

## Goal

(To be populated in pre-turn: refine the user's request into a clear goal statement.)

## Acceptance Criteria

(To be populated in pre-turn: list concrete, verifiable criteria that define "done" as `+"`- [ ] criterion`"+` checkbox items.)

## Files in scope

(To be populated in pre-turn: list files you expect to touch.)

## Out of scope

None

## Open questions

None

## Decisions log

(empty)
`, tl.TaskID, time.Now().Format("2006-01-02 15:04:05"))

	return os.WriteFile(path, []byte(content), 0644)
}

// decisionsLogSectionRe captures the body of "## Decisions log" up to (but
// not into) the next "## " heading or end of file.
//
// Go's RE2 has no lookaheads, so group 3 captures the boundary string itself
// (either `\n## ` or empty at \z) — the body ends where group 3 starts, and
// callers slice the rest of the spec from that boundary forward.
//
// Group 1 = heading line including its trailing newline.
// Group 2 = body content.
// Group 3 = boundary (next section start or empty at EOF).
var decisionsLogSectionRe = regexp.MustCompile(`(?ims)(^##\s+Decisions log[ \t]*\n)(.*?)(\n##\s+|\z)`)

// decisionBulletStartRe matches the start of a bullet entry: a line beginning
// with "- " (with optional leading whitespace).
var decisionBulletStartRe = regexp.MustCompile(`(?m)^\s*-\s`)

// CompactDecisionsLog compresses the spec's `## Decisions log` section by
// keeping only the first keepFirst and last keepLast bullet entries; the
// middle entries are appended to <taskdir>/spec-archive.md (creating it if
// missing). Everything else in spec.md is preserved byte-for-byte.
//
// Returns the count of removed entries and the archive path. If the section
// is missing, empty, or has fewer than keepFirst+keepLast entries, returns
// (0, "", nil) without touching any file.
func (tl *TaskLog) CompactDecisionsLog(keepFirst, keepLast int) (removed int, archivePath string, err error) {
	if keepFirst <= 0 || keepLast <= 0 {
		return 0, "", fmt.Errorf("keepFirst (%d) and keepLast (%d) must both be positive", keepFirst, keepLast)
	}

	spec, err := tl.ReadSpec()
	if err != nil {
		return 0, "", fmt.Errorf("reading spec: %w", err)
	}

	loc := decisionsLogSectionRe.FindStringSubmatchIndex(spec)
	if loc == nil {
		return 0, "", nil // no Decisions log section, nothing to do
	}
	bodyStart := loc[4]
	bodyEnd := loc[5]
	body := spec[bodyStart:bodyEnd]

	bullets := splitDecisionBullets(body)
	if len(bullets) <= keepFirst+keepLast {
		return 0, "", nil
	}

	first := bullets[:keepFirst]
	middle := bullets[keepFirst : len(bullets)-keepLast]
	last := bullets[len(bullets)-keepLast:]

	var nb strings.Builder
	nb.WriteString("\n") // blank line after heading
	for _, b := range first {
		nb.WriteString(b)
		nb.WriteString("\n")
	}
	// Blockquote (not a bullet) so a re-run of CompactDecisionsLog doesn't
	// count this marker as another entry and shrink the section further.
	fmt.Fprintf(&nb, "\n> _%d entradas anteriores archivadas en spec-archive.md._\n\n", len(middle))
	for _, b := range last {
		nb.WriteString(b)
		nb.WriteString("\n")
	}

	newSpec := spec[:bodyStart] + nb.String() + spec[bodyEnd:]

	archivePath = filepath.Join(tl.Dir, "spec-archive.md")
	if err := appendDecisionsArchive(archivePath, middle); err != nil {
		return 0, "", fmt.Errorf("appending archive: %w", err)
	}

	if err := os.WriteFile(tl.SpecPath(), []byte(newSpec), 0644); err != nil {
		return 0, "", fmt.Errorf("writing spec: %w", err)
	}

	return len(middle), archivePath, nil
}

// CountSpecDecisions returns the number of bullet entries in the spec's
// `## Decisions log` section. Returns 0 when the section is missing, contains
// only the `(empty)` placeholder, or has no bullet items.
func CountSpecDecisions(spec string) int {
	loc := decisionsLogSectionRe.FindStringSubmatchIndex(spec)
	if loc == nil {
		return 0
	}
	return len(splitDecisionBullets(spec[loc[4]:loc[5]]))
}

// splitDecisionBullets parses the body of a Decisions log section into a list
// of bullet entries. Each entry includes any continuation lines that follow
// the leading `- `. Whitespace-only sections and the literal "(empty)"
// placeholder return an empty slice.
func splitDecisionBullets(body string) []string {
	if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == "(empty)" {
		return nil
	}

	starts := decisionBulletStartRe.FindAllStringIndex(body, -1)
	if len(starts) == 0 {
		return nil
	}

	out := make([]string, 0, len(starts))
	for i, s := range starts {
		end := len(body)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		entry := strings.TrimRight(body[s[0]:end], " \t\n")
		if entry != "" {
			out = append(out, entry)
		}
	}
	return out
}

// appendDecisionsArchive writes a `## Compacted at <ts>` block followed by the
// archived entries to path, appending if the file already exists. On first
// creation, prepends a header block explaining the file's purpose.
func appendDecisionsArchive(path string, entries []string) error {
	_, statErr := os.Stat(path)
	exists := statErr == nil

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	var b strings.Builder
	if !exists {
		b.WriteString("# Spec Decisions Archive\n\n")
		b.WriteString("Decisions log entries removed from spec.md by `/spec compact`. Oldest first.\n\n")
	}
	fmt.Fprintf(&b, "## Compacted at %s\n\n", time.Now().Format(time.RFC3339))
	for _, e := range entries {
		b.WriteString(e)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	_, err = f.WriteString(b.String())
	return err
}

// statusRe matches: **Status**: DRAFT  or  **Status**: ACTIVE  (case insensitive)
var statusRe = regexp.MustCompile(`(?i)\*\*Status\*\*:\s*(DRAFT|ACTIVE)`)

// SpecIsDraft returns true when the spec reports Status: DRAFT, when the file
// is missing, or when no Status field is present. Erring on the side of "still
// drafting" is safer — it forces the pre-turn loop to run.
func (tl *TaskLog) SpecIsDraft() (bool, error) {
	content, err := tl.ReadSpec()
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	m := statusRe.FindStringSubmatch(content)
	if m == nil {
		return true, nil
	}
	return strings.EqualFold(strings.TrimSpace(m[1]), "DRAFT"), nil
}

// criteriaSectionRe captures the body of "## Acceptance Criteria" up to the
// next "## " heading or end of file.
var criteriaSectionRe = regexp.MustCompile(`(?ims)^##\s+Acceptance Criteria\s*\n(.*?)(?:\n##\s+|\z)`)

// checkboxRe matches markdown checkbox items: `- [ ] ...` or `- [x] ...`.
var checkboxRe = regexp.MustCompile(`(?m)^\s*-\s*\[([ xX])\]\s+`)

// SpecAllCriteriaChecked returns true only when the Acceptance Criteria section
// contains at least one checkbox AND every checkbox is checked. Empty criteria
// or any unchecked box returns false.
func (tl *TaskLog) SpecAllCriteriaChecked() (bool, error) {
	content, err := tl.ReadSpec()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	sec := criteriaSectionRe.FindStringSubmatch(content)
	if len(sec) < 2 {
		return false, nil
	}
	boxes := checkboxRe.FindAllStringSubmatch(sec[1], -1)
	if len(boxes) == 0 {
		return false, nil
	}
	for _, b := range boxes {
		if b[1] == " " {
			return false, nil
		}
	}
	return true, nil
}

// generateTaskID creates a filesystem-safe, human-readable task ID.
func generateTaskID(prompt string) string {
	timestamp := time.Now().Format("20060102-150405")

	words := strings.Fields(prompt)
	if len(words) > 4 {
		words = words[:4]
	}

	// Strip non-alphanumeric characters and lowercase.
	sanitize := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	slug := sanitize.ReplaceAllString(strings.Join(words, "-"), "-")
	slug = strings.Trim(slug, "-")
	slug = strings.ToLower(slug)

	if len(slug) > 40 {
		slug = slug[:40]
	}

	return fmt.Sprintf("%s-%s", timestamp, slug)
}
