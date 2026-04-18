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
)

const logFileName = "task-log.md"

// TaskLog manages the shared markdown log file inside the task directory.
type TaskLog struct {
	TaskID string
	Dir    string // absolute path to the task directory
	Prompt string
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

	return tl, nil
}

// Load opens an existing task directory and returns its TaskLog.
// It reads the task-log.md to extract the original prompt.
func Load(taskDir string) (*TaskLog, error) {
	taskID := filepath.Base(taskDir)
	logPath := filepath.Join(taskDir, logFileName)

	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, fmt.Errorf("reading task log %s: %w", logPath, err)
	}

	prompt := extractPrompt(string(data))

	return &TaskLog{
		TaskID: taskID,
		Dir:    taskDir,
		Prompt: prompt,
	}, nil
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
func (tl *TaskLog) Relocate(newBaseDir string) error {
	newTaskDir := filepath.Join(newBaseDir, ".departai", "tasks", tl.TaskID)
	newTaskDir = filepath.Clean(newTaskDir)

	if newTaskDir == filepath.Clean(tl.Dir) {
		return nil // already in the right place
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

// TurnEntry is a parsed representation of a single agent turn in the log.
type TurnEntry struct {
	TurnNumber int
	AgentName  string
	Complete   bool // agent's self-reported completion status
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

		turnNum := 0
		fmt.Sscanf(turnNumStr, "%d", &turnNum)

		entries = append(entries, TurnEntry{
			TurnNumber: turnNum,
			AgentName:  agentName,
			Complete:   complete,
		})
	}
	return entries
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
