package serve

import (
	"fmt"
	"slices"
	"strings"
	"sync"
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
	// Agent is the subagent whose conversation this is, or empty for the
	// session's own.
	Agent     string            `json:"agent,omitempty"`
	Usage     *runner.Usage     `json:"usage,omitempty"`
	Subagents []runner.Subagent `json:"subagents,omitempty"`
}

// statusNone is the status of work that has no session to ask.
const statusNone runner.RunnerStatus = "none"

// conversation returns work's session steps from index from on. A step before
// from never changes once a later step exists, except a tool call still
// waiting for its result or a question still waiting for its answer, so a
// client re-reads from the first of those.
func (s *Server) conversation(id string, from int, agent string) (conversationView, error) {
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
	inspect, inspects := r.(runner.Inspector)
	var entries []runner.Entry
	switch {
	case agent == "":
		entries, err = r.Conversation(h)
	case inspects:
		entries, err = inspect.SubagentConversation(h, agent)
		if err != nil {
			return conversationView{}, notFound{err.Error()}
		}
	default:
		return conversationView{}, notFound{fmt.Sprintf("%s runs on %s, which keeps no subagents", w.ID, h.Runner)}
	}
	if err != nil {
		return conversationView{}, err
	}
	view.Agent = agent
	if inspects {
		u, subs, err := inspected(inspect, h)
		if err != nil {
			return conversationView{}, err
		}
		view.Usage, view.Subagents = &u, subs
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
	// Status is the session's own: whether it works, rests or has stopped.
	Status    runner.RunnerStatus `json:"status,omitempty"`
	Usage     *runner.Usage       `json:"usage,omitempty"`
	Subagents []runner.Subagent   `json:"subagents,omitempty"`
}

// inspected is what a session spent and the subagents it started.
func inspected(r runner.Inspector, h runner.Handle) (runner.Usage, []runner.Subagent, error) {
	u, err := r.Usage(h)
	if err != nil {
		return runner.Usage{}, nil, err
	}
	subs, err := r.Subagents(h)
	return u, subs, err
}

// activities reads the session of every task, keyed by task. Sessions are
// read at once, a few at a time: a goal's tasks are many, and a runner may
// answer from another process.
func (s *Server) activities(tasks []core.Work) map[string]activity {
	out := map[string]activity{}
	if s.sessions == nil {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, sessionReads)
	for _, t := range tasks {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			if a, ok := s.activity(t); ok {
				mu.Lock()
				out[t.ID] = a
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// sessionReads is how many sessions one view reads at once.
const sessionReads = 8

// activity reads one work's session; ok is false when it has none.
func (s *Server) activity(t core.Work) (activity, bool) {
	r, h, ok, err := s.sessions(t)
	if err != nil {
		return activity{Error: err.Error()}, true
	}
	if !ok {
		return activity{}, false
	}
	entries, err := r.Conversation(h)
	if err != nil {
		return activity{Error: err.Error()}, true
	}
	a := activityOf(entries)
	if a.Status, err = r.Status(h); err != nil {
		a.Status = runner.StatusUnknown
	}
	if inspect, ok := r.(runner.Inspector); ok {
		u, subs, err := inspected(inspect, h)
		if err != nil {
			return activity{Error: err.Error()}, true
		}
		a.Usage, a.Subagents = &u, subs
	}
	return a, true
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

// sessionWatch is how often running sessions are read for a question or a report.
const sessionWatch = 3 * time.Second

// tidyEvery is how often finished work's worktrees are released while
// serving: a worktree costs disk, not attention, so the sweep is rare.
const tidyEvery = time.Hour

// tidyWorktrees releases finished work's worktrees now and then every
// tidyEvery, until stop closes or a release fails.
func (s *Server) tidyWorktrees(stop <-chan struct{}) error {
	tick := time.NewTicker(tidyEvery)
	defer tick.Stop()
	for {
		if err := s.tidy(); err != nil {
			return err
		}
		select {
		case <-stop:
			return nil
		case <-tick.C:
		}
	}
}

// Supervise is one supervise pass over a running goal.
type Supervise func(goal core.Work) error

// watchSessions files the question or report a running task's session ends
// on, notes one that stopped without either, and moves its work on, until stop
// closes, so a question reaches a person and finished work reaches review
// without a coordination pass. A goal's own session is its supervisor's, read
// by the supervise pass instead. A session that cannot be read is passed over:
// the conversation and goal views report that read's error to whoever opens
// the work.
func (s *Server) watchSessions(stop <-chan struct{}) error {
	if s.sessions == nil {
		return nil
	}
	supervised := make(chan error, 2)
	if s.supervise != nil {
		go func() { supervised <- s.superviseGoals(stop) }()
	}
	if s.tidy != nil {
		go func() {
			if err := s.tidyWorktrees(stop); err != nil {
				supervised <- err
			}
		}()
	}
	tick := time.NewTicker(sessionWatch)
	defer tick.Stop()
	onBoard := false
	for {
		select {
		case <-stop:
			return nil
		case err := <-supervised:
			return err
		case <-tick.C:
		}
		running, err := s.ledger.List(ledger.ListFilter{States: []core.State{core.StateRunning}, Archived: &onBoard})
		if err != nil {
			return err
		}
		for _, w := range running {
			if core.IsGoal(w.Kind) {
				continue
			}
			r, h, ok, err := s.sessions(w)
			if err != nil || !ok {
				continue
			}
			texts, err := runner.Transcript(r, h)
			if err != nil {
				continue
			}
			filed, err := coordinator.Notice(s.ledger, w, h, texts)
			if err != nil {
				return err
			}
			if filed {
				continue
			}
			status, err := r.Status(h)
			if err != nil || status == runner.StatusRunning {
				continue
			}
			if _, err := coordinator.Stopped(s.ledger, w, h, texts); err != nil {
				return err
			}
		}
		if err := s.deliverWaiting(); err != nil {
			return err
		}
	}
}

// deliverWaiting hands each work item's queued messages to its session once
// it is not working, whatever state the item or its goal is in: a message
// queued while the session was busy has nothing else to deliver it. Paused
// work keeps its messages until it is resumed, and a goal's supervisor is
// given what waits for it by a look. Work delivered to goes back to running,
// so its reply is read as any running session's is.
func (s *Server) deliverWaiting() error {
	waiting, err := s.ledger.MessagesWaiting()
	if err != nil {
		return err
	}
	for _, w := range waiting {
		if w.State == core.StatePaused || core.AtRest(w.State) || (core.IsGoal(w.Kind) && w.Session == nil && w.Claim == nil) {
			continue
		}
		r, h, ok, err := s.sessions(w)
		if err != nil || !ok {
			continue
		}
		sent, err := coordinator.Deliver(s.ledger, w, r, h)
		if err != nil {
			// A session that cannot be reached now is tried again next tick.
			continue
		}
		if sent > 0 && w.State != core.StateRunning && slices.Contains(core.Transitions[w.State], core.StateRunning) {
			if _, err := s.ledger.Transition(w.ID, core.StateRunning); err != nil {
				return err
			}
		}
	}
	return nil
}

// superviseGoals supervises every running top goal on the board every few
// seconds, until stop closes. A sub-goal is its top goal's supervisor's. A pass that fails is filed on its goal as a
// note, once for each different failure, and the goal is tried again later.
func (s *Server) superviseGoals(stop <-chan struct{}) error {
	tick := time.NewTicker(sessionWatch)
	defer tick.Stop()
	onBoard := false
	failed := map[string]string{}
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
			if !core.IsGoal(w.Kind) {
				continue
			}
			if top, err := s.topGoal(w); err != nil {
				return err
			} else if !top {
				continue
			}
			if err := s.supervise(w); err != nil {
				if failed[w.ID] != err.Error() {
					failed[w.ID] = err.Error()
					if _, err := s.ledger.AddEvent(w.ID, core.EventNote, "supervising failed: "+err.Error()); err != nil {
						return err
					}
				}
				continue
			}
			delete(failed, w.ID)
		}
	}
}

// topGoal reports whether goal has no goal above it.
func (s *Server) topGoal(goal core.Work) (bool, error) {
	if goal.Parent == nil {
		return true, nil
	}
	p, err := s.ledger.Get(*goal.Parent)
	if err != nil {
		return false, err
	}
	return !core.IsGoal(p.Kind), nil
}
