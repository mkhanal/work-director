package serve

import (
	"encoding/json"
	"fmt"
	"strings"

	"wd/internal/coordinator"
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
// by parent, the events filed at any depth under it, the question each work
// in needs-input waits on, and the question event behind it when one was
// filed.
type tree struct {
	root     core.Work
	byID     map[string]core.Work
	kids     map[string][]core.Work
	order    []core.Work
	events   []core.Event
	question map[string]runner.Question
	asked    map[string]core.Event
}

// readTree reads goal's subtree with the events under it.
func (s *Server) readTree(goal core.Work, events []core.Event) (tree, error) {
	all, err := s.ledger.Under(goal.ID)
	if err != nil {
		return tree{}, err
	}
	t := tree{root: goal, byID: map[string]core.Work{}, kids: map[string][]core.Work{}, order: all, events: events, question: map[string]runner.Question{}, asked: map[string]core.Event{}}
	for _, w := range all {
		t.byID[w.ID] = w
		if w.Parent != nil && w.ID != goal.ID {
			t.kids[*w.Parent] = append(t.kids[*w.Parent], w)
		}
		if w.State != core.StateNeedsInput {
			continue
		}
		q, ok := waitingOn(events, w.ID)
		if !ok {
			continue
		}
		t.question[w.ID] = q
		if e, ok := coordinator.OpenQuestion(events, w.ID); ok && coordinator.AskedQuestion(e).ID == q.ID {
			t.asked[w.ID] = e
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

// proposedWhy is why a sub-goal of a breakdown awaiting approval is not moving.
const proposedWhy = "Proposed, waiting for your approval"

// mergedWords names the sub-goal a merge kept by its title: the ledger files
// a merged-away sub-goal's why as "merged into <id>".
func (t tree) mergedWords(why string) string {
	id, ok := strings.CutPrefix(why, "merged into ")
	if !ok {
		return why
	}
	if w, ok := t.byID[id]; ok {
		return "merged into “" + w.Title + "”"
	}
	return "merged into another sub-goal"
}

// whyNotMoving says why work is not moving, or nil when it is or has finished.
func (t tree) whyNotMoving(w core.Work, waitingOn []string) *string {
	var why string
	switch {
	case w.State == core.StateSuperseded:
		why = "superseded"
		if e, ok := t.last(w.ID, core.EventNote, "superseded: "); ok {
			why = "superseded: " + t.mergedWords(strings.TrimPrefix(e.Body, "superseded: "))
		}
	case w.State == core.StateNeedsInput:
		why = "waits for you"
	case (w.State == core.StateQueued || w.State == core.StateBriefed) && w.Parent != nil && core.Proposed(*w.Parent, t.events):
		why = proposedWhy
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

// finish is where a goal finishes and where that was decided.
type finish struct {
	Line   core.FinishLine   `json:"line"`
	Source core.FinishSource `json:"source"`
}

// finishOf is top's finish line, or nil while it is undecided.
func finishOf(top core.Work) *finish {
	if top.FinishLine == nil || top.FinishSource == nil {
		return nil
	}
	return &finish{Line: *top.FinishLine, Source: *top.FinishSource}
}

// notDoneBecause says what holds the root back from done, in order: its open
// sub-goals, its done-when not judged met, and, for a top goal, its finish
// line not reached. A goal's done-when counts as met by a judgement or by a
// DONE report in its current run.
func (s *Server) notDoneBecause(t tree, top bool) ([]string, error) {
	out := []string{}
	goal := t.root
	if goal.State == core.StateDone {
		return out, nil
	}
	open := 0
	for _, c := range t.kids[goal.ID] {
		if !core.Settled(c.State) {
			open++
		}
	}
	switch open {
	case 0:
	case 1:
		out = append(out, "1 sub-goal is still open")
	default:
		out = append(out, fmt.Sprintf("%d sub-goals are still open", open))
	}
	run, err := s.ledger.CurrentRun(goal.ID)
	if err != nil {
		return nil, err
	}
	if goal.DoneWhen != nil {
		met := false
		if j := t.judged(goal.ID); j != nil {
			met = j.Verdict == "met"
		}
		for _, e := range run {
			met = met || (e.Kind == core.EventReport && strings.HasPrefix(e.Body, "DONE"))
		}
		if !met {
			out = append(out, "It hasn't been checked against what done means yet")
		}
	}
	if top && goal.FinishLine != nil {
		if ok, missing := core.FinishReached(*goal.FinishLine, run); !ok {
			out = append(out, "Not finished yet: "+missingWords(missing))
		}
	}
	return out, nil
}

// missingPlain says each part core.FinishReached finds missing as the person
// would: what has not happened to the work yet.
var missingPlain = map[string]string{
	"DONE report":    "the work isn't reported done",
	"passing verify": "its checks haven't passed",
	"pull request":   "there's no pull request",
	"commit landing": "it isn't merged",
	"released note":  "it isn't released",
}

// missingWords lists what core.FinishReached finds missing as one clause.
func missingWords(missing string) string {
	parts := strings.Split(missing, ", ")
	for i, m := range parts {
		if plain, ok := missingPlain[m]; ok {
			parts[i] = plain
		}
	}
	return andList(parts)
}

// andList joins phrases as a sentence lists them: "a, b, and c".
func andList(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// needYou is a question on work in the tree that waits on the person, and
// what answering it with its first choice would change.
type needYou struct {
	Work     string          `json:"work"`
	Path     string          `json:"path"`
	Question runner.Question `json:"question"`
	Change   *change         `json:"change"`
}

// needsYou lists every open question in the tree, in tree order; the root's
// own path is its title.
func (s *Server) needsYou(t tree, paths map[string]string) ([]needYou, error) {
	out := []needYou{}
	for _, w := range t.order {
		q, ok := t.question[w.ID]
		if !ok {
			continue
		}
		path, ok := paths[w.ID]
		if !ok {
			path = w.Title
		}
		ch, err := s.changeOf(t, w, q)
		if err != nil {
			return nil, err
		}
		out = append(out, needYou{Work: w.ID, Path: path, Question: q, Change: ch})
	}
	return out, nil
}

// Kinds of change a question waiting on the person carries.
const (
	changePlan   = "plan"
	changeCourse = "course"
	changeFinish = "finish"
)

// shape is a goal's title, done-when and finish line.
type shape struct {
	Title    string           `json:"title"`
	DoneWhen *string          `json:"done_when"`
	Finish   *core.FinishLine `json:"finish"`
}

// change is what a question would change: a plan change's actions and the
// same in sentences; a top goal now and as proposed, and why; or the finish
// lines offered. A question carrying both a course and a plan change is a
// course change with the plan's actions and lines.
type change struct {
	Kind     string   `json:"kind"`
	Actions  []string `json:"actions,omitempty"`
	Lines    []string `json:"lines,omitempty"`
	Now      *shape   `json:"now,omitempty"`
	Proposed *shape   `json:"proposed,omitempty"`
	Why      string   `json:"why,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	// Sources is what each source of a finish line said, served on a
	// finish-line question and null there when the planner gave none.
	Sources *core.FinishSaid `json:"-"`
	// Found is what was found that calls for a plan or course change, and
	// SubGoals what the change does to each sub-goal; both served on those
	// kinds only.
	Found    *string       `json:"-"`
	SubGoals []subGoalFate `json:"-"`
}

// Fates of a sub-goal under a change.
const (
	fateKept       = "kept"
	fateNew        = "new"
	fateSuperseded = "superseded"
)

// subGoalFate is one sub-goal as a change would leave it: an existing one
// kept or superseded with its state, or a new one with no work yet.
type subGoalFate struct {
	Work   *string     `json:"work"`
	Title  string      `json:"title"`
	Status string      `json:"status"`
	State  *core.State `json:"state"`
	Why    *string     `json:"why"`
}

// MarshalJSON serves the fields a kind always carries, null when unknown.
func (c change) MarshalJSON() ([]byte, error) {
	type fields change
	switch c.Kind {
	case changeFinish:
		return json.Marshal(struct {
			fields
			Sources *core.FinishSaid `json:"sources"`
		}{fields(c), c.Sources})
	case changePlan, changeCourse:
		return json.Marshal(struct {
			fields
			Found    *string       `json:"found"`
			SubGoals []subGoalFate `json:"sub_goals"`
		}{fields(c), c.Found, c.SubGoals})
	}
	return json.Marshal(fields(c))
}

// subGoalFates is what actions do to w's sub-goals: each open one, in tree
// order, superseded with why when an action supersedes it and kept with its
// state otherwise; then each sub-goal an action adds, as new.
func subGoalFates(t tree, w core.Work, actions []string) []subGoalFate {
	acts := []coordinator.Action{}
	for _, line := range actions {
		if parsed, _ := coordinator.ParseSupervision([]string{"ACTIONS:", line}); len(parsed) == 1 {
			acts = append(acts, parsed[0])
		}
	}
	out := []subGoalFate{}
	for _, k := range t.kids[w.ID] {
		var why *string
		for _, a := range acts {
			if a.Kind == coordinator.ActSupersede && a.Work == k.ID {
				text := a.Text
				why = &text
			}
		}
		if why == nil && (k.State == core.StateSuperseded || k.State == core.StateDropped) {
			continue
		}
		id, state := k.ID, k.State
		fate := subGoalFate{Work: &id, Title: k.Title, Status: fateKept, State: &state}
		if why != nil {
			fate.Status, fate.Why = fateSuperseded, why
		}
		out = append(out, fate)
	}
	for _, a := range acts {
		if a.Kind == coordinator.ActGoal {
			out = append(out, subGoalFate{Title: a.Title, Status: fateNew})
		}
	}
	return out
}

// changeOf is what answering q, the question w waits on, would change, or
// nil when it changes nothing by itself.
func (s *Server) changeOf(t tree, w core.Work, q runner.Question) (*change, error) {
	e, ok := t.asked[w.ID]
	if ok && e.Change != nil && (e.Change.Title != "" || len(e.Change.Actions) > 0) {
		ch := &change{Kind: changePlan, SubGoals: subGoalFates(t, w, e.Change.Actions)}
		if e.Change.Found != "" {
			found := e.Change.Found
			ch.Found = &found
		}
		if len(e.Change.Actions) > 0 {
			ch.Actions = e.Change.Actions
			lines, err := s.planLines(t, e.Change.Actions)
			if err != nil {
				return nil, err
			}
			ch.Lines = lines
		}
		if e.Change.Title != "" {
			ch.Kind = changeCourse
			ch.Now = &shape{Title: w.Title, DoneWhen: w.DoneWhen, Finish: w.FinishLine}
			proposed := shape{Title: e.Change.Title, DoneWhen: &e.Change.DoneWhen}
			if e.Change.Finish != "" {
				proposed.Finish = &e.Change.Finish
			}
			ch.Proposed = &proposed
			ch.Why = proposedWhyOf(q)
		}
		return ch, nil
	}
	for _, it := range q.Items {
		head, _, _ := strings.Cut(it.Question, "\n")
		if head != coordinator.FinishQuestion {
			continue
		}
		ch := &change{Kind: changeFinish, Choices: []string{}}
		if ok && e.Change != nil {
			ch.Sources = e.Change.Sources
		}
		for _, o := range it.Options {
			ch.Choices = append(ch.Choices, o.Label)
		}
		return ch, nil
	}
	return nil, nil
}

// proposedWhyOf is the reason a top-goal change gives, from the "Why:" line
// of the question item that proposes it.
func proposedWhyOf(q runner.Question) string {
	for _, it := range q.Items {
		if !strings.Contains(it.Question, "\nProposed: ") {
			continue
		}
		for _, line := range strings.Split(it.Question, "\n") {
			if why, ok := strings.CutPrefix(strings.TrimSpace(line), "Why: "); ok {
				return why
			}
		}
	}
	return ""
}

// planLines says each of a supervisor's plan actions as a sentence, naming
// work by its title.
func (s *Server) planLines(t tree, actions []string) ([]string, error) {
	title := func(id string) (string, error) {
		if w, ok := t.byID[id]; ok {
			return w.Title, nil
		}
		known, err := s.ledger.Has(id)
		if err != nil || !known {
			return "a sub-goal that no longer exists", err
		}
		w, err := s.ledger.Get(id)
		return w.Title, err
	}
	out := make([]string, 0, len(actions))
	for _, line := range actions {
		said, err := coordinator.PlanWords(line, title)
		if err != nil {
			return nil, err
		}
		out = append(out, said)
	}
	return out, nil
}

// course is a supervisor's latest verdict on whether its goal is on course.
type course struct {
	Verdict string `json:"verdict"`
	Why     string `json:"why"`
	At      string `json:"at"`
}

// courseOf reads the latest course event on goal in events, or nil.
func courseOf(events []core.Event, goal string) *course {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != core.EventCourse || e.Work != goal {
			continue
		}
		verdict, why, _ := strings.Cut(e.Body, ": ")
		return &course{Verdict: verdict, Why: why, At: e.At}
	}
	return nil
}

// supersededWork is a branch of the tree a course change superseded.
type supersededWork struct {
	Work  string `json:"work"`
	Title string `json:"title"`
	Why   string `json:"why"`
	At    string `json:"at"`
}

// superseded lists the roots of the superseded branches under the root.
func (t tree) superseded() []supersededWork {
	out := []supersededWork{}
	for _, w := range t.order[1:] {
		if w.State != core.StateSuperseded || t.byID[*w.Parent].State == core.StateSuperseded {
			continue
		}
		sw := supersededWork{Work: w.ID, Title: w.Title}
		if e, ok := t.last(w.ID, core.EventNote, "superseded: "); ok {
			sw.Why, sw.At = t.mergedWords(strings.TrimPrefix(e.Body, "superseded: ")), e.At
		}
		out = append(out, sw)
	}
	return out
}

// boardGoal is a top goal as the board shows it.
func (s *Server) boardGoal(goal core.Work) (boardGoal, error) {
	tasks, err := s.ledger.Tasks(goal.ID)
	if err != nil {
		return boardGoal{}, err
	}
	band, err := s.band(goal, tasks)
	if err != nil {
		return boardGoal{}, err
	}
	events, err := s.ledger.EventsUnder(goal.ID)
	if err != nil {
		return boardGoal{}, err
	}
	t, err := s.readTree(goal, events)
	if err != nil {
		return boardGoal{}, err
	}
	g := boardGoal{Work: goal, Rollup: goalRollup(tasks), Band: band, Progress: progressOf(t.kids[goal.ID]), NeedsYou: len(t.question), Finish: finishOf(goal),
		Proposed: core.Proposed(goal.ID, events)}
	// A breakdown waiting for approval waits on the person.
	if g.Proposed {
		g.Band = BandNeedsYou
	}
	if goal.State == core.StateDone && goal.FinishLine != nil {
		run, err := s.ledger.CurrentRun(goal.ID)
		if err != nil {
			return boardGoal{}, err
		}
		f := finishedAt(*goal.FinishLine, run)
		g.Finished = &f
	}
	return g, nil
}

// finishedAt names where a goal finished: its finish line, capitalised, with
// the latest landing in run as a pull request number or a short commit.
func finishedAt(line core.FinishLine, run []core.Event) string {
	out := strings.ToUpper(string(line[:1])) + string(line[1:])
	for i := len(run) - 1; i >= 0; i-- {
		if run[i].Kind != core.EventPr {
			continue
		}
		url := core.ParseLanding(run[i].Body).URL
		if _, n, ok := strings.Cut(url, "/pull/"); ok && n != "" {
			return out + " #" + strings.TrimSuffix(n, "/")
		}
		if _, sha, ok := strings.Cut(url, "/commit/"); ok && sha != "" {
			return out + " " + sha[:min(7, len(sha))]
		}
		return out
	}
	return out
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
