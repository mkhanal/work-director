package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func agents(cwd string) ([]agentRow, error) {
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
	permissionMode := "bypassPermissions"
	if o.PermissionMode != nil {
		permissionMode = *o.PermissionMode
	}
	args := []string{"claude", "--bg", "-n", o.Name, "--permission-mode", permissionMode, "--settings", thinkingSummaries}
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
		// A stopped session keeps its conversation and resumes on the next send.
		if a.State == "done" || a.State == "stopped" {
			return StatusIdle, nil
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
	files, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", session+".jsonl"))
	if err != nil {
		return "", err
	}
	switch len(files) {
	case 0:
		return "", nil
	case 1:
		return files[0], nil
	}
	return "", &RunnerError{Runner: "claude", Detail: fmt.Sprintf("session %s has %d transcripts: %s", session, len(files), strings.Join(files, ", "))}
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
	text, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	calls := map[string]int{}
	// open is the question still waiting for its reply, or -1: claude records
	// an answer typed in the session as the tool's result, and a session
	// stopped and resumed with an answer as the prompt that follows.
	open := -1
	prompt := func(at *string, s string) {
		if strings.TrimSpace(s) == "" {
			return
		}
		if open >= 0 {
			q := out[open].Question
			q.settle(s)
			open = -1
			// A set of answers is shown on its question; it is not said twice.
			if q.Answers != nil {
				return
			}
		}
		out = append(out, Entry{Kind: EntryPrompt, At: at, Text: s})
	}
	for i, line := range jsonlRecords(string(text)) {
		if line == "" {
			continue
		}
		var rec claudeRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		if (rec.Type != "user" && rec.Type != "assistant") || rec.IsSidechain || rec.IsMeta || len(rec.Message.Content) == 0 {
			continue
		}
		if !bytes.HasPrefix(bytes.TrimSpace(rec.Message.Content), []byte("[")) {
			var s string
			if err := json.Unmarshal(rec.Message.Content, &s); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
			}
			if rec.Type == "user" {
				prompt(rec.Timestamp, s)
			}
			continue
		}
		var parts []claudePart
		if err := json.Unmarshal(rec.Message.Content, &parts); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		for _, p := range parts {
			switch {
			case rec.Type == "user" && p.Type == "text":
				prompt(rec.Timestamp, p.Text)
			case rec.Type == "user" && p.Type == "tool_result":
				at, ok := calls[p.ToolUseID]
				if !ok {
					continue
				}
				if q := out[at].Question; q != nil {
					// An interrupted question is still waiting: the reply
					// arrives as the next prompt.
					if p.IsError || !q.Pending() {
						continue
					}
					var res claudeQuestions
					if len(rec.ToolUseResult) > 0 && json.Unmarshal(rec.ToolUseResult, &res) == nil && len(res.Answers) > 0 {
						answers := make([]string, len(q.Items))
						for k, it := range q.Items {
							answers[k] = res.Answers[it.Question]
						}
						q.Answers = answers
					} else {
						q.settle(resultText(p.Content))
					}
					open = -1
					continue
				}
				r := clipped(resultText(p.Content))
				out[at].Tool.Result = &r
				out[at].Tool.Failed = p.IsError
			case rec.Type == "assistant" && p.Type == "text" && p.Text != "":
				out = append(out, Entry{Kind: EntryText, At: rec.Timestamp, Text: p.Text})
			case rec.Type == "assistant" && p.Type == "thinking" && strings.TrimSpace(p.Thinking) != "":
				out = append(out, Entry{Kind: EntryThinking, At: rec.Timestamp, Text: strings.TrimSpace(p.Thinking)})
			case rec.Type == "assistant" && p.Type == "tool_use" && p.Name == askUserQuestion:
				var in claudeQuestions
				if err := json.Unmarshal(p.Input, &in); err != nil {
					return nil, fmt.Errorf("%s:%d: %s input: %w", file, i+1, askUserQuestion, err)
				}
				q := &Question{ID: p.ID, Items: []QuestionItem{}}
				for _, item := range in.Questions {
					it := QuestionItem{Header: item.Header, Question: item.Question, Multi: item.MultiSelect, Options: []Option{}}
					for _, o := range item.Options {
						it.Options = append(it.Options, Option{Label: o.Label, Description: o.Description})
					}
					q.Items = append(q.Items, it)
				}
				calls[p.ID] = len(out)
				open = len(out)
				out = append(out, Entry{Kind: EntryQuestion, At: rec.Timestamp, Question: q})
			case rec.Type == "assistant" && p.Type == "tool_use":
				input := map[string]any{}
				if len(p.Input) > 0 {
					if err := json.Unmarshal(p.Input, &input); err != nil {
						return nil, fmt.Errorf("%s:%d: %s input: %w", file, i+1, p.Name, err)
					}
				}
				calls[p.ID] = len(out)
				out = append(out, Entry{Kind: EntryTool, At: rec.Timestamp, Tool: &ToolCall{
					ID: p.ID, Name: p.Name, Summary: toolSummary(p.Name, input, h.Cwd), Input: toolInput(input),
				}})
			}
		}
	}
	return out, nil
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
