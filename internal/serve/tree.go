package serve

import (
	"fmt"
	"strings"

	"wd/internal/core"
	"wd/internal/runner"
)

// link is one level of a goal's breadcrumb.
type link struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	State core.State `json:"state"`
}

// progress counts direct sub-goals: done of total, with superseded and
// dropped ones out of the total and superseded counted apart.
type progress struct {
	Done       int `json:"done"`
	Total      int `json:"total"`
	Superseded int `json:"superseded"`
}

// owner is the agent working a task: its runner, and the named agent when
// the runner has one.
type owner struct {
	Runner string  `json:"runner"`
	Agent  *string `json:"agent"`
}

// judgement is the latest judgement of a sub-goal's done-when.
type judgement struct {
	Verdict string `json:"verdict"`
	Why     string `json:"why"`
}

// node is one sub-goal in a goal's tree. A group node is the tasks of an
// older plan that share a heading, gathered under a synthetic goal row.
type node struct {
	Work         core.Work   `json:"work"`
	Group        bool        `json:"group"`
	DoneWhen     *string     `json:"done_when"`
	After        []string    `json:"after"`
	WaitingOn    []string    `json:"waiting_on"`
	WhyNotMoving *string     `json:"why_not_moving"`
	Owner        *owner      `json:"owner"`
	PlannedInto  *int        `json:"planned_into"`
	Progress     *progress   `json:"progress"`
	Doing        *string     `json:"doing"`
	Steps        []core.Step `json:"steps"`
	Judged       *judgement  `json:"judged"`
	NeedsYou     int         `json:"needs_you"`
	Children     []node      `json:"children"`
}

// pathSep joins the titles of a path through the tree.
const pathSep = " › "

// noHeading is the heading an older plan gave tasks it did not group.
const noHeading = "(no heading)"

// tree is a goal's whole subtree read once: every work under it by id and
// by parent, the events filed at any depth under it, and the question each
// work in needs-input waits on.
type tree struct {
	root     core.Work
	byID     map[string]core.Work
	kids     map[string][]core.Work
	order    []core.Work
	events   []core.Event
	question map[string]runner.Question
}

// readTree reads goal's subtree with the events under it.
func (s *Server) readTree(goal core.Work, events []core.Event) (tree, error) {
	all, err := s.ledger.Under(goal.ID)
	if err != nil {
		return tree{}, err
	}
	t := tree{root: goal, byID: map[string]core.Work{}, kids: map[string][]core.Work{}, order: all, events: events, question: map[string]runner.Question{}}
	for _, w := range all {
		t.byID[w.ID] = w
		if w.Parent != nil && w.ID != goal.ID {
			t.kids[*w.Parent] = append(t.kids[*w.Parent], w)
		}
		if w.State == core.StateNeedsInput {
			if q, ok := waitingOn(events, w.ID); ok {
				t.question[w.ID] = q
			}
		}
	}
	return t, nil
}

// chain is goal's breadcrumb: its top goal first, ending at goal.
func (s *Server) chain(goal core.Work) ([]core.Work, error) {
	out := []core.Work{goal}
	for w := goal; w.Parent != nil; {
		p, err := s.ledger.Get(*w.Parent)
		if err != nil {
			return nil, err
		}
		if !core.IsGoal(p.Kind) {
			break
		}
		out = append([]core.Work{p}, out...)
		w = p
	}
	return out, nil
}

func links(chain []core.Work) []link {
	out := make([]link, 0, len(chain))
	for _, w := range chain {
		out = append(out, link{ID: w.ID, Title: w.Title, State: w.State})
	}
	return out
}

// paths names every work under the root by the titles from just below the
// root down to it.
func (t tree) paths() map[string]string {
	out := map[string]string{}
	var walk func(parent, prefix string)
	walk = func(parent, prefix string) {
		for _, w := range t.kids[parent] {
			p := w.Title
			if prefix != "" {
				p = prefix + pathSep + w.Title
			}
			out[w.ID] = p
			walk(w.ID, p)
		}
	}
	walk(t.root.ID, "")
	return out
}

// progressOf counts children as progress.
func progressOf(children []core.Work) progress {
	var p progress
	for _, c := range children {
		switch c.State {
		case core.StateSuperseded:
			p.Superseded++
		case core.StateDropped:
		case core.StateDone:
			p.Done++
			p.Total++
		default:
			p.Total++
		}
	}
	return p
}

// nodes builds the tree under parent; under a goal, tasks sharing a real
// heading gather into one group node where the first of them stands.
func (s *Server) nodes(t tree, parent string) ([]node, error) {
	out := []node{}
	groups := map[string]int{}
	for _, w := range t.kids[parent] {
		h := ""
		if w.Kind == core.WorkTask && w.Heading != nil && *w.Heading != noHeading {
			h = strings.TrimSpace(*w.Heading)
		}
		n, err := s.node(t, w)
		if err != nil {
			return nil, err
		}
		if h == "" {
			out = append(out, n)
			continue
		}
		i, ok := groups[h]
		if !ok {
			i = len(out)
			groups[h] = i
			out = append(out, node{Group: true, After: []string{}, WaitingOn: []string{}, Steps: []core.Step{}, Children: []node{}})
		}
		out[i].Children = append(out[i].Children, n)
	}
	n := 0
	for i := range out {
		if !out[i].Group {
			continue
		}
		n++
		g := &out[i]
		states := make([]core.State, 0, len(g.Children))
		works := make([]core.Work, 0, len(g.Children))
		for _, c := range g.Children {
			states = append(states, c.Work.State)
			works = append(works, c.Work)
			g.NeedsYou += c.NeedsYou
		}
		first := g.Children[0].Work
		g.Work = core.Work{ID: fmt.Sprintf("group:%s:%d", parent, n), Project: first.Project, Title: *first.Heading, Kind: core.WorkGoal, State: core.RollupState(states), Parent: &parent}
		p := progressOf(works)
		g.Progress = &p
	}
	return out, nil
}

// node builds one work's node with everything under it.
func (s *Server) node(t tree, w core.Work) (node, error) {
	n := node{Work: w, DoneWhen: w.DoneWhen, WaitingOn: []string{}, Children: []node{}}
	after, err := s.ledger.After(w.ID)
	if err != nil {
		return n, err
	}
	n.After = after
	for _, id := range after {
		o, ok := t.byID[id]
		if !ok {
			if o, err = s.ledger.Get(id); err != nil {
				return n, err
			}
		}
		if !core.Settled(o.State) {
			n.WaitingOn = append(n.WaitingOn, o.Title)
		}
	}
	n.WhyNotMoving = t.whyNotMoving(w, n.WaitingOn)
	if w.Kind == core.WorkTask && w.Session != nil && w.Runner != nil {
		n.Owner = &owner{Runner: *w.Runner}
	}
	if core.IsGoal(w.Kind) {
		planned := len(t.kids[w.ID])
		n.PlannedInto = &planned
		p := progressOf(t.kids[w.ID])
		n.Progress = &p
		if n.Children, err = s.nodes(t, w.ID); err != nil {
			return n, err
		}
	}
	if n.Steps, err = s.ledger.Steps(w.ID); err != nil {
		return n, err
	}
	for _, st := range n.Steps {
		if st.State == core.StepDoing {
			text := st.Text
			n.Doing = &text
			break
		}
	}
	n.Judged = t.judged(w.ID)
	if _, ok := t.question[w.ID]; ok {
		n.NeedsYou = 1
	}
	for _, c := range n.Children {
		n.NeedsYou += c.NeedsYou
	}
	return n, nil
}

// whyNotMoving says why work is not moving, or nil when it is or has finished.
func (t tree) whyNotMoving(w core.Work, waitingOn []string) *string {
	var why string
	switch {
	case w.State == core.StateSuperseded:
		why = "superseded"
		if e, ok := t.last(w.ID, core.EventNote, "superseded: "); ok {
			why = e.Body
		}
	case w.State == core.StateNeedsInput:
		why = "waits for you"
	case (w.State == core.StateQueued || w.State == core.StateBriefed) && len(waitingOn) > 0:
		why = "waits on " + strings.Join(waitingOn, ", ")
	default:
		return nil
	}
	return &why
}

// judged is work's latest done-when judgement, or nil.
func (t tree) judged(work string) *judgement {
	e, ok := t.last(work, core.EventJudged, "")
	if !ok {
		return nil
	}
	verdict, why, _ := strings.Cut(e.Body, ": ")
	return &judgement{Verdict: verdict, Why: why}
}

// last is the latest event of kind on work whose body opens with prefix.
func (t tree) last(work string, kind core.EventKind, prefix string) (core.Event, bool) {
	for i := len(t.events) - 1; i >= 0; i-- {
		e := t.events[i]
		if e.Work == work && e.Kind == kind && strings.HasPrefix(e.Body, prefix) {
			return e, true
		}
	}
	return core.Event{}, false
}
