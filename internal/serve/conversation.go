package serve

import (
	"strings"
	"time"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/runner"
)

// Sessions reaches the session work is worked in: its runner and handle. ok is
// false when the work has no session.
type Sessions func(w core.Work) (r runner.Runner, h runner.Handle, ok bool, err error)

// conversationView is a work item's session from one step on: the steps, how
// many there are in all, whether the session is working, and the question it
// is waiting on.
type conversationView struct {
	Work    string              `json:"work"`
	Runner  string              `json:"runner,omitempty"`
	Session string              `json:"session,omitempty"`
	Status  runner.RunnerStatus `json:"status"`
	From    int                 `json:"from"`
	Total   int                 `json:"total"`
	Entries []runner.Entry      `json:"entries"`
	Asking  *runner.Question    `json:"asking"`
}

// statusNone is the status of work that has no session to ask.
const statusNone runner.RunnerStatus = "none"

// conversation returns work's session steps from index from on. A step before
// from never changes once a later step exists, except a tool call still
// waiting for its result or a question still waiting for its answer, so a
// client re-reads from the first of those.
func (s *Server) conversation(id string, from int) (conversationView, error) {
	w, err := s.work(id)
	if err != nil {
		return conversationView{}, err
	}
	if from < 0 {
		return conversationView{}, badRequest{"from is a step index, 0 or more"}
	}
	view := conversationView{Work: w.ID, Status: statusNone, From: from, Entries: []runner.Entry{}}
	if s.sessions == nil {
		return view, nil
	}
	r, h, ok, err := s.sessions(w)
	if err != nil || !ok {
		return view, err
	}
	entries, err := r.Conversation(h)
	if err != nil {
		return conversationView{}, err
	}
	status, err := r.Status(h)
	if err != nil {
		status = runner.StatusUnknown
	}
	view.Runner, view.Session, view.Status, view.Total = h.Runner, h.Session, status, len(entries)
	if q, ok := runner.Asking(entries); ok {
		view.Asking = q
	}
	if from < len(entries) {
		view.Entries = entries[from:]
	}
	return view, nil
}

// activity is what a task's session is doing now, in a line, and the question
// it is waiting on. Error is why the session could not be read.
type activity struct {
	Now    string           `json:"now,omitempty"`
	Kind   runner.EntryKind `json:"kind,omitempty"`
	At     *string          `json:"at,omitempty"`
	Asking *runner.Question `json:"asking,omitempty"`
	Error  string           `json:"error,omitempty"`
}

// activities reads the session of every task still working, keyed by task.
func (s *Server) activities(tasks []core.Work) map[string]activity {
	out := map[string]activity{}
	if s.sessions == nil {
		return out
	}
	for _, t := range tasks {
		if t.State != core.StateRunning && t.State != core.StateNeedsInput {
			continue
		}
		r, h, ok, err := s.sessions(t)
		if err != nil {
			out[t.ID] = activity{Error: err.Error()}
			continue
		}
		if !ok {
			continue
		}
		entries, err := r.Conversation(h)
		if err != nil {
			out[t.ID] = activity{Error: err.Error()}
			continue
		}
		out[t.ID] = activityOf(entries)
	}
	return out
}

// activityOf is the latest step of a conversation as a line.
func activityOf(entries []runner.Entry) activity {
	if len(entries) == 0 {
		return activity{}
	}
	e := entries[len(entries)-1]
	a := activity{Kind: e.Kind, At: e.At}
	switch e.Kind {
	case runner.EntryTool:
		a.Now = e.Tool.Name
		if e.Tool.Summary != "" {
			a.Now += " · " + e.Tool.Summary
		}
	case runner.EntryQuestion:
		a.Now = e.Question.Text()
	default:
		a.Now = firstLine(e.Text)
	}
	if q, ok := runner.Asking(entries); ok {
		a.Asking = q
	}
	return a
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// questionWatch is how often running sessions are read for a question.
const questionWatch = 3 * time.Second

// watchQuestions files the question a running session ends on and moves its
// work to needs-input, until stop closes, so a question reaches a person
// without a coordination pass. A session that cannot be read is passed over:
// the conversation and goal views report that read's error to whoever opens
// the work.
func (s *Server) watchQuestions(stop <-chan struct{}) error {
	if s.sessions == nil {
		return nil
	}
	tick := time.NewTicker(questionWatch)
	defer tick.Stop()
	onBoard := false
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
		}
		running, err := s.ledger.List(ledger.ListFilter{States: []core.State{core.StateRunning}, Archived: &onBoard})
		if err != nil {
			return err
		}
		for _, w := range running {
			r, h, ok, err := s.sessions(w)
			if err != nil || !ok {
				continue
			}
			texts, err := runner.Transcript(r, h)
			if err != nil {
				continue
			}
			if _, err := coordinator.Notice(s.ledger, w, h, texts); err != nil {
				return err
			}
		}
	}
}
