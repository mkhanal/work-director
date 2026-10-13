package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// transcript is one claude store read so far: the conversation and the usage
// of every complete line up to offset. A store only grows, so the next read
// parses only what was added; a store that shrank is read again from the start.
type transcript struct {
	mu        sync.Mutex
	file, cwd string
	sidechain bool
	offset    int64
	lines     int
	// tail is the last bytes read; a store whose bytes there differ was
	// rewritten, not grown, and is read again from the start.
	tail []byte

	out   []Entry
	calls map[string]int
	// open is the question still waiting for its reply, or -1: claude records
	// an answer typed in the session as the tool's result, and a session
	// stopped and resumed with an answer as the prompt that follows.
	open int

	usage Usage
	ended bool
	said  string
}

type transcriptKey struct {
	file, cwd string
	sidechain bool
}

// keptTranscripts is how many stores stay parsed; the least recently read
// beyond it is let go and parsed again if it is read again. A goal's view
// reads every store of its tasks and their subagents, so this holds the
// stores of the several goals a person moves between: fewer, and moving
// between goals parses every store again on every read.
const keptTranscripts = 128

var transcripts = struct {
	sync.Mutex
	m    map[transcriptKey]*transcript
	used map[transcriptKey]uint64
	tick uint64
}{m: map[transcriptKey]*transcript{}, used: map[transcriptKey]uint64{}}

// readTranscript is file's store as read now, sharing the parse of every
// earlier read.
func readTranscript(file, cwd string, sidechain bool) (*transcript, error) {
	key := transcriptKey{file, cwd, sidechain}
	transcripts.Lock()
	t, ok := transcripts.m[key]
	if !ok {
		t = &transcript{file: file, cwd: cwd, sidechain: sidechain}
		t.reset()
		transcripts.m[key] = t
	}
	transcripts.tick++
	transcripts.used[key] = transcripts.tick
	if len(transcripts.m) > keptTranscripts {
		oldest, at := key, transcripts.tick
		for k, u := range transcripts.used {
			if u < at {
				oldest, at = k, u
			}
		}
		delete(transcripts.m, oldest)
		delete(transcripts.used, oldest)
	}
	transcripts.Unlock()

	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < t.offset {
		t.reset()
	} else if len(t.tail) > 0 {
		had := make([]byte, len(t.tail))
		if _, err := f.ReadAt(had, t.offset-int64(len(t.tail))); err != nil || !bytes.Equal(had, t.tail) {
			t.reset()
		}
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil, err
	}
	added, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// Only complete lines are read: the last may still be being written.
	end := bytes.LastIndexByte(added, '\n')
	if end < 0 {
		return t, nil
	}
	for _, line := range strings.Split(string(added[:end]), "\n") {
		t.lines++
		if line == "" {
			continue
		}
		if err := t.feed(line); err != nil {
			// A line that does not parse is read again next time, not skipped.
			t.reset()
			return nil, err
		}
	}
	t.offset += int64(end + 1)
	t.tail = append([]byte(nil), added[max(0, end+1-256):end+1]...)
	return t, nil
}

func (t *transcript) reset() {
	t.offset, t.lines, t.tail = 0, 0, nil
	t.out, t.calls, t.open = []Entry{}, map[string]int{}, -1
	t.usage, t.ended, t.said = Usage{}, false, ""
}

// entries is a copy of the conversation, so a later read filling in a tool's
// result or a question's answer never changes what a caller already holds.
func (t *transcript) entries() []Entry {
	out := make([]Entry, len(t.out))
	for i, e := range t.out {
		if e.Tool != nil {
			tool := *e.Tool
			e.Tool = &tool
		}
		if e.Question != nil {
			q := *e.Question
			q.Answers = append([]string(nil), q.Answers...)
			if e.Question.Answers == nil {
				q.Answers = nil
			}
			e.Question = &q
		}
		out[i] = e
	}
	return out
}

func (t *transcript) feed(line string) error {
	var rec claudeRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return fmt.Errorf("%s:%d: %w", t.file, t.lines, err)
	}
	if err := t.spend(rec); err != nil {
		return err
	}
	if (rec.Type != "user" && rec.Type != "assistant") || rec.IsSidechain != t.sidechain || rec.IsMeta || len(rec.Message.Content) == 0 {
		return nil
	}
	if !bytes.HasPrefix(bytes.TrimSpace(rec.Message.Content), []byte("[")) {
		var s string
		if err := json.Unmarshal(rec.Message.Content, &s); err != nil {
			return fmt.Errorf("%s:%d: %w", t.file, t.lines, err)
		}
		if rec.Type == "user" {
			t.prompt(rec.Timestamp, s)
		}
		return nil
	}
	var parts []claudePart
	if err := json.Unmarshal(rec.Message.Content, &parts); err != nil {
		return fmt.Errorf("%s:%d: %w", t.file, t.lines, err)
	}
	for _, p := range parts {
		if err := t.part(rec, p); err != nil {
			return err
		}
	}
	return nil
}

// spend adds a record to the usage. Tokens leave out cache reads: those are
// the same context read again, and counting them makes every long session
// look a hundred times its size.
func (t *transcript) spend(rec claudeRecord) error {
	if rec.IsSidechain != t.sidechain || rec.Timestamp == nil {
		return nil
	}
	if t.usage.Started == nil {
		t.usage.Started = rec.Timestamp
	}
	t.usage.Last = rec.Timestamp
	if rec.Type != "assistant" {
		return nil
	}
	m := rec.Message
	if m.Model != "" && !strings.HasPrefix(m.Model, "<") {
		t.usage.Model = m.Model
	}
	t.usage.Tokens += m.Usage.Input + m.Usage.CacheCreation + m.Usage.Output
	t.ended = m.StopReason == "end_turn"
	if !bytes.HasPrefix(bytes.TrimSpace(m.Content), []byte("[")) {
		return nil
	}
	var parts []claudePart
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return fmt.Errorf("%s:%d: %w", t.file, t.lines, err)
	}
	for _, p := range parts {
		switch p.Type {
		case "tool_use":
			t.usage.Tools++
		case "text":
			if line := firstLine(p.Text); line != "" {
				t.said = line
			}
		}
	}
	return nil
}

func (t *transcript) prompt(at *string, s string) {
	s, ok := typed(s)
	if !ok || strings.TrimSpace(s) == "" {
		return
	}
	if t.open >= 0 {
		q := t.out[t.open].Question
		q.settle(s)
		t.open = -1
		// A set of answers is shown on its question; it is not said twice.
		if q.Answers != nil {
			return
		}
	}
	t.out = append(t.out, Entry{Kind: EntryPrompt, At: at, Text: s})
}

func (t *transcript) part(rec claudeRecord, p claudePart) error {
	switch {
	case rec.Type == "user" && p.Type == "text":
		t.prompt(rec.Timestamp, p.Text)
	case rec.Type == "user" && p.Type == "tool_result":
		at, ok := t.calls[p.ToolUseID]
		if !ok {
			return nil
		}
		if q := t.out[at].Question; q != nil {
			// An interrupted question is still waiting: the reply arrives as
			// the next prompt.
			if p.IsError || !q.Pending() {
				return nil
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
			t.open = -1
			return nil
		}
		r := clipped(resultText(p.Content))
		t.out[at].Tool.Result = &r
		t.out[at].Tool.Failed = p.IsError
	case rec.Type == "assistant" && p.Type == "text" && p.Text != "":
		t.out = append(t.out, Entry{Kind: EntryText, At: rec.Timestamp, Text: p.Text})
	case rec.Type == "assistant" && p.Type == "thinking" && strings.TrimSpace(p.Thinking) != "":
		t.out = append(t.out, Entry{Kind: EntryThinking, At: rec.Timestamp, Text: strings.TrimSpace(p.Thinking)})
	case rec.Type == "assistant" && p.Type == "tool_use" && p.Name == askUserQuestion:
		var in claudeQuestions
		if err := json.Unmarshal(p.Input, &in); err != nil {
			return fmt.Errorf("%s:%d: %s input: %w", t.file, t.lines, askUserQuestion, err)
		}
		q := &Question{ID: p.ID, Items: []QuestionItem{}}
		for _, item := range in.Questions {
			it := QuestionItem{Header: item.Header, Question: item.Question, Multi: item.MultiSelect, Options: []Option{}}
			for _, o := range item.Options {
				it.Options = append(it.Options, Option{Label: o.Label, Description: o.Description})
			}
			q.Items = append(q.Items, it)
		}
		t.calls[p.ID] = len(t.out)
		t.open = len(t.out)
		t.out = append(t.out, Entry{Kind: EntryQuestion, At: rec.Timestamp, Question: q})
	case rec.Type == "assistant" && p.Type == "tool_use":
		input := map[string]any{}
		if len(p.Input) > 0 {
			if err := json.Unmarshal(p.Input, &input); err != nil {
				return fmt.Errorf("%s:%d: %s input: %w", t.file, t.lines, p.Name, err)
			}
		}
		t.calls[p.ID] = len(t.out)
		t.out = append(t.out, Entry{Kind: EntryTool, At: rec.Timestamp, Tool: &ToolCall{
			ID: p.ID, Name: p.Name, Summary: toolSummary(p.Name, input, t.cwd), Input: toolInput(input),
		}})
	}
	return nil
}
