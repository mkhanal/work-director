package runner

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// EntryKind is what one step of a session's conversation is.
type EntryKind string

const (
	// EntryPrompt is what the session was told: its brief, a message sent to it.
	EntryPrompt EntryKind = "prompt"
	// EntryText is what the executor said.
	EntryText EntryKind = "text"
	// EntryThinking is the executor's reasoning, as its provider summarises it.
	EntryThinking EntryKind = "thinking"
	// EntryTool is one tool the executor called, with its result once it has one.
	EntryTool EntryKind = "tool"
	// EntryQuestion is a question the executor put to a person, with options.
	EntryQuestion EntryKind = "question"
)

// Entry is one step of a session's conversation, oldest first.
type Entry struct {
	Kind     EntryKind `json:"kind"`
	At       *string   `json:"at,omitempty"`
	Text     string    `json:"text,omitempty"`
	Tool     *ToolCall `json:"tool,omitempty"`
	Question *Question `json:"question,omitempty"`
}

// ToolCall is one tool call: what was called on what, and what came back.
// Result is nil while the call is still running.
type ToolCall struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Summary string  `json:"summary"`
	Input   string  `json:"input"`
	Result  *string `json:"result"`
	Failed  bool    `json:"failed"`
}

// Question is a question with options the executor is waiting on a person
// for. Answers holds one answer per item once it is answered; Reply is the
// message the session got instead when that message was not a set of answers.
type Question struct {
	ID      string         `json:"id"`
	Items   []QuestionItem `json:"items"`
	Answers []string       `json:"answers"`
	Reply   *string        `json:"reply"`
}

// QuestionItem is one question of a set, as the executor asked it.
type QuestionItem struct {
	Header   string   `json:"header"`
	Question string   `json:"question"`
	Multi    bool     `json:"multi"`
	Options  []Option `json:"options"`
}

// Option is one answer the executor offered.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Pending reports whether nobody has answered the question yet.
func (q Question) Pending() bool { return q.Answers == nil && q.Reply == nil }

// Text is the question as one line: each item with its options.
func (q Question) Text() string {
	parts := make([]string, 0, len(q.Items))
	for _, it := range q.Items {
		labels := make([]string, 0, len(it.Options))
		for _, o := range it.Options {
			labels = append(labels, o.Label)
		}
		s := it.Question
		if len(labels) > 0 {
			s += " (" + strings.Join(labels, " / ") + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// Asking is the question a conversation ends waiting on, if any.
func Asking(entries []Entry) (*Question, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind == EntryQuestion {
			if e.Question.Pending() {
				return e.Question, true
			}
			return nil, false
		}
		if e.Kind == EntryPrompt {
			return nil, false
		}
	}
	return nil, false
}

// Transcript is what the executor said in a session, oldest first. A
// conversation ending on a question with options ends on that question as an
// `ASK:` line, the same line an executor without such a tool writes, so one
// path files, answers and escalates both.
func Transcript(r Runner, h Handle) ([]string, error) {
	entries, err := r.Conversation(h)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.Kind == EntryText {
			out = append(out, e.Text)
		}
	}
	if q, ok := Asking(entries); ok {
		out = append(out, "ASK: "+q.Text())
	}
	return out, nil
}

// answersHeading opens the message that answers a question with options; a
// reader of the conversation pairs the lines under it with the question's items.
const answersHeading = "Answers to your questions:"

// AnswerText is the message that answers q: one line per item, in order. It
// is refused unless there is an answer for every item.
func AnswerText(q Question, answers []string) (string, error) {
	if len(answers) != len(q.Items) {
		asked := make([]string, len(q.Items))
		for i, it := range q.Items {
			asked[i] = fmt.Sprintf("%q", it.Question)
		}
		return "", fmt.Errorf("the question has %d item(s), so it takes %d answer(s) in order: %s", len(q.Items), len(q.Items), strings.Join(asked, ", "))
	}
	lines := []string{answersHeading}
	for i, it := range q.Items {
		a := strings.TrimSpace(answers[i])
		if a == "" {
			return "", fmt.Errorf("no answer to %q", it.Question)
		}
		lines = append(lines, fmt.Sprintf("%q → %s", it.Question, a))
	}
	return strings.Join(lines, "\n"), nil
}

// answersIn reads a message written by AnswerText back into one answer per
// item. ok is false for any other message.
func answersIn(text string, q Question) ([]string, bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != len(q.Items)+1 || lines[0] != answersHeading {
		return nil, false
	}
	out := make([]string, len(q.Items))
	for i, it := range q.Items {
		prefix := fmt.Sprintf("%q → ", it.Question)
		if !strings.HasPrefix(lines[i+1], prefix) {
			return nil, false
		}
		out[i] = strings.TrimPrefix(lines[i+1], prefix)
	}
	return out, true
}

// settle records the reply a question got: its answers when the reply is a
// set of answers to it, else the reply itself.
func (q *Question) settle(reply string) {
	if a, ok := answersIn(reply, *q); ok {
		q.Answers = a
		return
	}
	q.Reply = &reply
}

// clip keeps a tool's input or result to what a reader scans; the session's
// own store keeps the rest.
const clip = 4000

func clipped(s string) string {
	if r := []rune(s); len(r) > clip {
		return string(r[:clip]) + fmt.Sprintf("\n… %d more characters", len(r)-clip)
	}
	return s
}

// toolSummary is the one thing a tool call acted on: the command, the file,
// the pattern, the address.
func toolSummary(name string, input map[string]any, cwd string) string {
	str := func(k string) string {
		s, _ := input[k].(string)
		return s
	}
	path := func(k string) string {
		p := str(k)
		if cwd != "" {
			if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
				return rel
			}
		}
		return p
	}
	firstLine := func(s string) string {
		s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
		return s
	}
	switch name {
	case "Bash":
		return firstLine(str("command"))
	case "Read", "Edit", "Write", "MultiEdit":
		return path("file_path")
	case "NotebookEdit":
		return path("notebook_path")
	case "Grep":
		if p := str("path"); p != "" {
			return str("pattern") + " in " + path("path")
		}
		return str("pattern")
	case "Glob":
		return str("pattern")
	case "WebFetch":
		return str("url")
	case "WebSearch":
		return str("query")
	case "Agent", "Task":
		return str("description")
	case "Skill":
		return str("skill")
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := input[k].(string); ok && s != "" {
			return firstLine(s)
		}
	}
	return ""
}

// toolInput is a tool's input as a reader scans it: each field on its own
// line, longest last.
func toolInput(input map[string]any) string {
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := input[k]
		s, ok := v.(string)
		if !ok {
			j, err := json.Marshal(v)
			if err != nil {
				continue
			}
			s = string(j)
		}
		if strings.Contains(s, "\n") {
			fmt.Fprintf(&b, "%s:\n%s\n", k, s)
		} else {
			fmt.Fprintf(&b, "%s: %s\n", k, s)
		}
	}
	return clipped(strings.TrimRight(b.String(), "\n"))
}

// texts is a conversation of an executor whose store holds only what it
// said.
func texts(said []string) []Entry {
	out := make([]Entry, 0, len(said))
	for _, s := range said {
		out = append(out, Entry{Kind: EntryText, Text: s})
	}
	return out
}
