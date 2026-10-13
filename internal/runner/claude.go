package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// agentRow is one entry of `claude agents --json --all`.
type agentRow struct {
	ID        string `json:"id"`
	Pid       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Status    string `json:"status"`
	State     string `json:"state"`
}

// agentListing is the last `claude agents` listing, shared for ttl by a
// process that reads many sessions' status at once: one listing a few times a
// second rather than one per session per read. Zero ttl shares nothing. Past
// ttl, and up to staleFor ttls old, a read is answered with the listing it has
// while one listing runs in the background: a person opening a page never
// waits on claude listing its agents, and the page's next read sees the new
// listing.
var agentListing struct {
	sync.Mutex
	ttl     time.Duration
	at      time.Time
	rows    []agentRow
	reading bool
}

// staleFor is how many ttls old a shared listing may be and still answer.
const staleFor = 10

// ShareAgentListing has every status read within ttl of a listing use it.
func ShareAgentListing(ttl time.Duration) {
	agentListing.Lock()
	defer agentListing.Unlock()
	agentListing.ttl = ttl
}

// forgetAgents drops the shared listing: what this process just did to a
// session is read fresh.
func forgetAgents() {
	agentListing.Lock()
	defer agentListing.Unlock()
	agentListing.at = time.Time{}
}

func agents(cwd string) ([]agentRow, error) {
	agentListing.Lock()
	defer agentListing.Unlock()
	age := time.Since(agentListing.at)
	ttl := agentListing.ttl
	if ttl > 0 && age < ttl {
		return agentListing.rows, nil
	}
	if ttl > 0 && age < staleFor*ttl {
		if !agentListing.reading {
			agentListing.reading = true
			at := agentListing.at
			go relist(cwd, at)
		}
		return agentListing.rows, nil
	}
	rows, err := listAgents(cwd)
	if err != nil {
		return nil, err
	}
	agentListing.rows, agentListing.at = rows, time.Now()
	return rows, nil
}

// relist lists the agents again in the background, keeping the listing read
// at at unless something read or forgot it since.
func relist(cwd string, at time.Time) {
	rows, err := listAgents(cwd)
	agentListing.Lock()
	defer agentListing.Unlock()
	agentListing.reading = false
	if err == nil && agentListing.at.Equal(at) {
		agentListing.rows, agentListing.at = rows, time.Now()
	}
}

func listAgents(cwd string) ([]agentRow, error) {
	r, err := Run([]string{"claude", "agents", "--json", "--all"}, cwd)
	if err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, &RunnerError{Runner: "claude", Detail: "agents failed: " + stderrOrStdout(r)}
	}
	var rows []agentRow
	if err := json.Unmarshal([]byte(r.Stdout), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// claudeRefRe captures the background id from `claude --bg` output:
// "backgrounded · b765c0a2 · <name>".
var claudeRefRe = regexp.MustCompile(`backgrounded\s*·\s*([0-9a-f]{8})`)

// thinkingSummaries has claude record its thinking as readable summaries
// rather than empty blocks. A resumed session keeps the settings it was
// spawned with, so it is passed once, at spawn.
const thinkingSummaries = `{"showThinkingSummaries":true}`

type claudeRunner struct{}

// claude is the built-in claude adapter: spawn resolves the session id via
// `claude agents`, transcript reads the session's jsonl under ~/.claude/projects.
var claude = claudeRunner{}

func (claudeRunner) Name() string { return "claude" }

func (claudeRunner) Command() string { return "claude" }

func (claudeRunner) Spawn(o SpawnOptions) (Handle, error) {
	defer forgetAgents()
	permissionMode := "bypassPermissions"
	if o.PermissionMode != nil {
		permissionMode = *o.PermissionMode
	}
	args := []string{"claude", "--bg", "-n", sessionName(o.Name), "--permission-mode", permissionMode, "--settings", thinkingSummaries}
	if o.Model != nil {
		args = append(args, "--model", *o.Model)
	}
	if o.Worktree != nil && *o.Worktree {
		args = append(args, "--worktree", worktreeSlug(o.Name))
	}
	args = append(args, o.Brief)
	r, err := Run(args, o.Cwd)
	if err != nil {
		return Handle{}, err
	}
	m := claudeRefRe.FindStringSubmatch(r.Stdout)
	if r.Code != 0 || m == nil {
		return Handle{}, &RunnerError{Runner: "claude", Detail: "spawn failed: " + stderrOrStdout(r)}
	}
	ref := m[1]
	row, ok, err := waitFor(func() (*agentRow, bool, error) {
		rows, err := agents(o.Cwd)
		if err != nil {
			return nil, false, err
		}
		for i, a := range rows {
			if a.ID == ref {
				return &rows[i], true, nil
			}
		}
		return nil, false, nil
	}, 15*time.Second, 500*time.Millisecond)
	if err != nil {
		return Handle{}, err
	}
	if !ok {
		return Handle{}, &RunnerError{Runner: "claude", Detail: fmt.Sprintf("session %s not listed by claude agents", ref)}
	}
	return Handle{Runner: "claude", Session: row.SessionID, Ref: &ref, Cwd: row.Cwd}, nil
}

// Send continues the session. Resuming a session whose process is still up,
// even idle or waiting on a question, starts a copy of it, so a live session
// is stopped first; one still working is refused, because stopping it would
// cut its work short.
func (c claudeRunner) Send(h *Handle, text string) error {
	defer forgetAgents()
	rows, err := agents(h.Cwd)
	if err != nil {
		return err
	}
	for _, a := range rows {
		if a.SessionID != h.Session {
			continue
		}
		switch a.Status {
		case "busy", "running":
			return &RunnerError{Runner: "claude", Detail: "session " + h.Session + " is working; a message to it waits until it is idle"}
		case "idle", "waiting":
			if err := c.Stop(*h); err != nil {
				return err
			}
			// claude stop returns before the process has gone, and a resume
			// while it is still going starts a copy.
			if a.Pid != 0 {
				_, gone, _ := waitFor(func() (bool, bool, error) { return true, !alive(a.Pid), nil }, 15*time.Second, 100*time.Millisecond)
				if !gone {
					return &RunnerError{Runner: "claude", Detail: fmt.Sprintf("session %s (pid %d) did not exit within 15s of stopping; nothing was sent", h.Session, a.Pid)}
				}
			}
		}
	}
	r, err := Run([]string{"claude", "--bg", "--resume", h.Session, text}, h.Cwd)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return &RunnerError{Runner: "claude", Detail: "send failed: " + stderrOrStdout(r)}
	}
	return nil
}

// Stop stops the background session by the id claude printed at spawn;
// claude keeps its conversation for a later resume.
func (claudeRunner) Stop(h Handle) error {
	defer forgetAgents()
	if h.Ref == nil {
		return &RunnerError{Runner: "claude", Detail: "session " + h.Session + " has no background id to stop"}
	}
	r, err := Run([]string{"claude", "stop", *h.Ref}, h.Cwd)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return &RunnerError{Runner: "claude", Detail: "stop failed: " + stderrOrStdout(r)}
	}
	return nil
}

func (claudeRunner) Status(h Handle) (RunnerStatus, error) {
	rows, err := agents(h.Cwd)
	if err != nil {
		return "", err
	}
	for _, a := range rows {
		if a.SessionID != h.Session {
			continue
		}
		switch a.Status {
		case "waiting":
			return StatusWaiting, nil
		case "busy", "running":
			return StatusRunning, nil
		case "idle":
			return StatusIdle, nil
		}
		// A session listed with no status has no process; it keeps its
		// conversation and resumes on the next send.
		if a.Status == "" {
			return StatusStopped, nil
		}
		return StatusUnknown, nil
	}
	return StatusExited, nil
}

// transcriptFile finds the session's store in every project dir: claude
// files a session under the slug of its current cwd, which moves when the
// session enters a worktree. "" means no file yet, so no messages yet.
func transcriptFile(session string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dirs, err := projectDirs(filepath.Join(home, ".claude", "projects"))
	if err != nil {
		return "", err
	}
	var files []string
	for _, dir := range dirs {
		file := filepath.Join(dir, session+".jsonl")
		if _, err := os.Lstat(file); err == nil {
			files = append(files, file)
		}
	}
	switch len(files) {
	case 0:
		return "", nil
	case 1:
		return files[0], nil
	}
	return "", &RunnerError{Runner: "claude", Detail: fmt.Sprintf("session %s has %d transcripts: %s", session, len(files), strings.Join(files, ", "))}
}

// projectListing is the last listing of claude's projects directory, with
// the directory's modification time when it was read. Adding or removing a
// project directory changes that time, so a listing read at the same time is
// still the whole list; a server reading every session of a goal on each read
// lists the directory once rather than once per session per read.
var projectListing struct {
	sync.Mutex
	root string
	at   time.Time
	dirs []string
}

// projectDirs is every entry under root, in name order; a session's store is
// looked for in each.
func projectDirs(root string) ([]string, error) {
	st, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	projectListing.Lock()
	defer projectListing.Unlock()
	if projectListing.root == root && projectListing.at.Equal(st.ModTime()) {
		return projectListing.dirs, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		dirs = append(dirs, filepath.Join(root, e.Name()))
	}
	projectListing.root, projectListing.at, projectListing.dirs = root, st.ModTime(), dirs
	return dirs, nil
}

// claudeRecord is one line of a session's store; only the fields a
// conversation reads.
type claudeRecord struct {
	Type          string          `json:"type"`
	Timestamp     *string         `json:"timestamp"`
	IsMeta        bool            `json:"isMeta"`
	IsSidechain   bool            `json:"isSidechain"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Message       struct {
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			Input         int `json:"input_tokens"`
			CacheCreation int `json:"cache_creation_input_tokens"`
			Output        int `json:"output_tokens"`
		} `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudePart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// askUserQuestion is claude's question tool: its input is the question set.
const askUserQuestion = "AskUserQuestion"

type claudeQuestions struct {
	Questions []struct {
		Header      string `json:"header"`
		Question    string `json:"question"`
		MultiSelect bool   `json:"multiSelect"`
		Options     []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
	Answers map[string]string `json:"answers"`
}

// Conversation reads the session's store: prompts, what it said, its
// summarised thinking, its tool calls with their results, and the questions it
// asked with the answers they got. Side conversations of its subagents are not
// its own.
func (claudeRunner) Conversation(h Handle) ([]Entry, error) {
	file, err := transcriptFile(h.Session)
	if err != nil || file == "" {
		return []Entry{}, err
	}
	return conversationIn(file, h.Cwd, false)
}

// conversationIn reads one claude store. A subagent's own store is all
// sidechain; a session's store skips the sidechain records it carries.
func conversationIn(file, cwd string, sidechain bool) ([]Entry, error) {
	t, err := readTranscript(file, cwd, sidechain)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.entries(), nil
}

var (
	commandRe = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	argsRe    = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
)

// typed is what a person typed, as a prompt: a slash command reads as the
// command and its arguments, and claude's own notes and output for a local
// command are no prompt at all.
func typed(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "<local-command-") {
		return "", false
	}
	if m := commandRe.FindStringSubmatch(t); m != nil && strings.HasPrefix(t, "<command-") {
		args := ""
		if a := argsRe.FindStringSubmatch(t); a != nil {
			args = strings.TrimSpace(a[1])
		}
		return strings.TrimSpace(m[1] + " " + args), true
	}
	return s, true
}

// resultText is a tool result's text: claude stores it as a string or as
// text parts.
func resultText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// claude's CLI has no non-interactive model list; its own /model picker is the list.
func (claudeRunner) Models() ([]string, error) { return []string{}, nil }

func (claudeRunner) AttachHint(h Handle) string {
	ref := h.Session
	if h.Ref != nil {
		ref = *h.Ref
	}
	return "claude attach " + ref
}
