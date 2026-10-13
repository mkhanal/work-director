package serve

import (
	"errors"
	"fmt"
	"strconv"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/delivery"
	"wd/internal/ledger"
	"wd/internal/review"
	"wd/internal/runner"
)

// Band is where an item sits on the board: what it is waiting on.
type Band string

const (
	BandNeedsYou     Band = "needs-you"
	BandInFlight     Band = "in-flight"
	BandPaused       Band = "paused"
	BandReadyToPush  Band = "ready-to-push"
	BandInReview     Band = "in-review"
	BandReadyToClose Band = "ready-to-close"
	BandAtRest       Band = "at-rest"
)

// rollup is a goal's status rolled up from its children.
type rollup struct {
	Counts map[core.State]int `json:"counts"`
	Done   int                `json:"done"`
	Total  int                `json:"total"`
}

type boardGoal struct {
	Work   core.Work `json:"work"`
	Rollup rollup    `json:"rollup"`
	Band   Band      `json:"band"`
}

type boardItem struct {
	Work core.Work `json:"work"`
	Band Band      `json:"band"`
}

// boardView is every goal with its rollup and every open standalone work item.
type boardView struct {
	Goals      []boardGoal `json:"goals"`
	Standalone []boardItem `json:"standalone"`
}

// goalView is one goal with its children, rollup and events, plus the two facts
// that make a goal legible on its own: what the loop decided in a person's place,
// and where the work reached. They are read out of the events rather than fetched
// again, because a client rendering the goal's decisions and its event log must
// not be looking at two reads of the same ledger.
type goalView struct {
	Goal     core.Work       `json:"goal"`
	Tasks    []core.Work     `json:"tasks"`
	Rollup   rollup          `json:"rollup"`
	Events   []core.Event    `json:"events"`
	Claims   []review.Claim  `json:"claims"`
	Landings []review.Landed `json:"landings"`
	Delivery *delivery.State `json:"delivery,omitempty"`
	// Activity is what each task still working is doing now, by task id.
	Activity map[string]activity `json:"activity"`
	// Asking is the question the goal waits on the person for, with its
	// choices, while it does.
	Asking *runner.Question `json:"asking,omitempty"`
	// Reports are the goal's and its tasks' reports read into parts, by the
	// id of the event that filed each.
	Reports map[string]core.Report `json:"reports"`
	// Waiting is each task in needs-input whose session asks nothing, with
	// what it waits on as a question.
	Waiting []waitingTask `json:"waiting"`
	// Chain is the breadcrumb, top goal first, ending at this goal.
	Chain []link `json:"chain"`
	// Tree is the goal's sub-goals, and Paths names every work under the
	// goal by where it sits in that tree.
	Tree  []node            `json:"tree"`
	Paths map[string]string `json:"paths"`
	// DoneWhen is what makes the goal done; Finish is where its top goal
	// finishes.
	DoneWhen *string  `json:"done_when"`
	Finish   *finish  `json:"finish"`
	Progress progress `json:"progress"`
	// NotDoneBecause is what holds the goal back from done, empty once done.
	NotDoneBecause []string `json:"not_done_because"`
}

// waitingTask is a task waiting on the person, and what it waits on.
type waitingTask struct {
	Task     string          `json:"task"`
	Question runner.Question `json:"question"`
}

// notFound is a view asked of work that does not exist, or of the wrong kind.
type notFound struct{ msg string }

func (e notFound) Error() string { return e.msg }

// badRequest is a view asked for without what it needs.
type badRequest struct{ msg string }

func (e badRequest) Error() string { return e.msg }

// board returns every goal with its rollup and every open standalone work item,
// each in its band.
func (s *Server) board() (boardView, error) {
	onBoard := false
	items, err := s.ledger.List(ledger.ListFilter{Archived: &onBoard})
	if err != nil {
		return boardView{}, err
	}
	b := boardView{Goals: []boardGoal{}, Standalone: []boardItem{}}
	for _, w := range items {
		if core.IsGoal(w.Kind) {
			children, err := s.ledger.Tasks(w.ID)
			if err != nil {
				return boardView{}, err
			}
			band, err := s.band(w, children)
			if err != nil {
				return boardView{}, err
			}
			b.Goals = append(b.Goals, boardGoal{Work: w, Rollup: goalRollup(children), Band: band})
		} else if w.Parent == nil && !core.Settled(w.State) {
			band, err := s.band(w, nil)
			if err != nil {
				return boardView{}, err
			}
			b.Standalone = append(b.Standalone, boardItem{Work: w, Band: band})
		}
	}
	return b, nil
}

// band places one item: a task that needs a person lifts its goal into
// needs-you, and review splits on whether this run has landed anywhere yet.
func (s *Server) band(w core.Work, tasks []core.Work) (Band, error) {
	for _, t := range tasks {
		if needsYou(t.State) {
			return BandNeedsYou, nil
		}
	}
	switch {
	case needsYou(w.State):
		return BandNeedsYou, nil
	case w.State == core.StatePaused:
		return BandPaused, nil
	case w.State == core.StateSoftDone:
		return BandReadyToClose, nil
	case core.AtRest(w.State):
		return BandAtRest, nil
	case w.State == core.StateReview:
		evs, err := s.ledger.CurrentRun(w.ID)
		if err != nil {
			return "", err
		}
		for _, e := range evs {
			if e.Kind == core.EventPr {
				return BandInReview, nil
			}
		}
		return BandReadyToPush, nil
	default:
		return BandInFlight, nil
	}
}

func needsYou(s core.State) bool { return s == core.StateNeedsInput || s == core.StateBlocked }

// goal returns one goal with its children, rollup, events, claims, landings and
// delivery.
func (s *Server) goal(id string) (goalView, error) {
	goal, err := s.work(id)
	if err != nil {
		return goalView{}, err
	}
	if !core.IsGoal(goal.Kind) {
		return goalView{}, notFound{fmt.Sprintf("%s is not a goal", id)}
	}
	children, err := s.ledger.Tasks(id)
	if err != nil {
		return goalView{}, err
	}
	events, err := s.ledger.EventsUnder(id)
	if err != nil {
		return goalView{}, err
	}
	// Every work this goal is made of: the goal itself and its tasks, which is
	// what a claim or a landing can name, and which the events above may be
	// filed against.
	works := map[string]core.Work{goal.ID: goal}
	for _, t := range children {
		works[t.ID] = t
	}
	view := goalView{
		Goal:     goal,
		Tasks:    children,
		Rollup:   goalRollup(children),
		Events:   events,
		Claims:   review.ClaimsUnder(events, works),
		Landings: review.LandingsUnder(events, works),
		Activity: s.activities(append([]core.Work{goal}, children...)),
	}
	view.Reports = map[string]core.Report{}
	for _, e := range events {
		if e.Kind == core.EventReport {
			view.Reports[strconv.Itoa(e.ID)] = core.ParseReport(e.Body)
		}
	}
	view.Waiting = []waitingTask{}
	for _, t := range children {
		if t.State != core.StateNeedsInput || view.Activity[t.ID].Asking != nil {
			continue
		}
		if q, ok := waitingOn(events, t.ID); ok {
			view.Waiting = append(view.Waiting, waitingTask{Task: t.ID, Question: q})
		}
	}
	if q, ok := coordinator.OpenQuestion(events, goal.ID); ok && goal.State == core.StateNeedsInput {
		asked := coordinator.AskedQuestion(q)
		view.Asking = &asked
	}
	chain, err := s.chain(goal)
	if err != nil {
		return goalView{}, err
	}
	view.Chain = links(chain)
	t, err := s.readTree(goal, events)
	if err != nil {
		return goalView{}, err
	}
	if view.Tree, err = s.nodes(t, goal.ID); err != nil {
		return goalView{}, err
	}
	view.Paths = t.paths()
	view.DoneWhen = goal.DoneWhen
	view.Finish = finishOf(chain[0])
	view.Progress = progressOf(t.kids[goal.ID])
	if view.NotDoneBecause, err = s.notDoneBecause(t, len(chain) == 1); err != nil {
		return goalView{}, err
	}
	if dir := goalDir(goal, children); dir != "" {
		d := delivery.StatusFor(dir, events, goal, children)
		view.Delivery = &d
	}
	return view, nil
}

// goalDir is where a goal's work happens: its tasks' directory, else its own,
// else none. With none, delivery is not derived at all, because git run
// anywhere else answers for somebody else's repository.
func goalDir(goal core.Work, tasks []core.Work) string {
	for _, t := range tasks {
		if t.Cwd != nil && *t.Cwd != "" {
			return *t.Cwd
		}
	}
	if goal.Cwd != nil {
		return *goal.Cwd
	}
	return ""
}

// work returns one work item, or notFound naming it.
func (s *Server) work(id string) (core.Work, error) {
	if id == "" {
		return core.Work{}, badRequest{"missing work id"}
	}
	known, err := s.ledger.Has(id)
	if err != nil {
		return core.Work{}, err
	}
	if !known {
		return core.Work{}, notFound{"no work " + id}
	}
	return s.ledger.Get(id)
}

// events returns one work item's own events.
func (s *Server) events(id string) ([]core.Event, error) {
	if _, err := s.work(id); err != nil {
		return nil, err
	}
	return s.ledger.Events(id, nil)
}

// goalRollup counts a goal's children per state.
func goalRollup(children []core.Work) rollup {
	counts := map[core.State]int{}
	for _, c := range children {
		counts[c.State]++
	}
	return rollup{Counts: counts, Done: counts[core.StateDone], Total: len(children)}
}

// errorKind names the class of a view error for a client to branch on.
func errorKind(err error) string {
	var nf notFound
	var br badRequest
	switch {
	case errors.As(err, &nf):
		return "not-found"
	case errors.As(err, &br):
		return "bad-request"
	default:
		return "internal"
	}
}

// waitingOn is what work waits on the person for: the question it last asked
// that nothing has answered, else what its last report said it needs.
func waitingOn(events []core.Event, work string) (runner.Question, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Work != work {
			continue
		}
		switch e.Kind {
		case core.EventAnswer, core.EventDecision:
			return runner.Question{}, false
		case core.EventQuestion:
			return coordinator.AskedQuestion(e), true
		case core.EventReport:
			return coordinator.ReportQuestion(e), true
		}
	}
	return runner.Question{}, false
}
