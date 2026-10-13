package cli

import (
	"fmt"
	"slices"
	"strings"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/project"
	"wd/internal/runner"
)

// supervision is one supervise pass: whether the supervisor was asked to look,
// whether a reply of its was carried out, and what that did and refused.
type supervision struct {
	Looked  bool     `json:"looked"`
	Applied bool     `json:"applied"`
	Actions []string `json:"actions"`
	Refused []string `json:"refused"`
}

// goalSupervise is `wd goal supervise <goal>`.
func (c *Cli) goalSupervise(id string) error {
	goal, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if !core.IsGoal(goal.Kind) || goal.State != core.StateRunning {
		return fail("%s is a %s %s; only a running goal is supervised", goal.ID, goal.State, goal.Kind)
	}
	s, err := c.supervise(goal)
	if err != nil {
		return err
	}
	text := "nothing to look at: a task is working, the supervisor is, or nothing changed since it last looked"
	switch {
	case s.Applied:
		text = fmt.Sprintf("the supervisor's reply was carried out: %d done, %d refused", len(s.Actions), len(s.Refused))
	case s.Looked:
		text = "the supervisor is looking"
	}
	return c.out(s, text)
}

// Supervise is one supervise pass over a running goal, for a server that
// supervises every running goal while it serves.
func (c *Cli) Supervise(goal core.Work) error {
	_, err := c.supervise(goal)
	return err
}

// supervise moves a running goal on: its tasks' queued messages reach idle
// sessions, a task that stopped without a report is noted, a supervisor reply
// not yet read is carried out, and the supervisor is asked to look when no task
// is working and something changed since it last looked, or at once when the
// goal was decided on.
func (c *Cli) supervise(goal core.Work) (supervision, error) {
	out := supervision{Actions: []string{}, Refused: []string{}}
	if goal.State != core.StateRunning {
		return out, nil
	}
	p, err := c.project(goal.Project)
	if err != nil {
		return out, err
	}
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return out, err
	}
	working, err := c.tend(tasks, p)
	if err != nil {
		return out, err
	}

	sh, has, err := coordinator.Supervisor(c.Ledger, goal, p)
	if err != nil {
		return out, err
	}
	var sr runner.Runner
	said := []string{}
	if has {
		if sr, err = runner.RunnerNamed(sh.Runner); err != nil {
			return out, err
		}
		status, err := sr.Status(sh)
		if err != nil {
			return out, err
		}
		if status == runner.StatusRunning {
			return out, nil
		}
		if said, err = runner.Transcript(sr, sh); err != nil {
			return out, err
		}
		mark, ok, err := c.Ledger.Filed(goal.ID)
		if err != nil {
			return out, err
		}
		if ok && mark.Session == sh.Session && len(said) > mark.Entries {
			read := core.TranscriptMark{Session: sh.Session, Entries: len(said)}
			mine, err := c.Ledger.Advance(goal.ID, mark, read)
			if err != nil || !mine {
				return out, err
			}
			return c.carryOut(goal, p, said[mark.Entries:])
		}
	}

	why, decided, last, err := c.sinceLook(goal, tasks)
	if err != nil {
		return out, err
	}
	if len(why) == 0 || (working && !decided) {
		return out, nil
	}
	if _, mine, err := c.Ledger.ClaimLook(goal.ID, last, strings.Join(why, "; ")); err != nil || !mine {
		return out, err
	}
	// Another pass may have started the supervisor since it was read above.
	if sh, has, err = coordinator.Supervisor(c.Ledger, goal, p); err != nil {
		return out, err
	}
	if has && sr == nil {
		if sr, err = runner.RunnerNamed(sh.Runner); err != nil {
			return out, err
		}
		if said, err = runner.Transcript(sr, sh); err != nil {
			return out, err
		}
	}
	picture, err := c.picture(goal, p)
	if err != nil {
		return out, err
	}
	brief := coordinator.SupervisionBrief(picture)
	if has {
		before := sh.Ref
		if err := sr.Send(&sh, brief); err != nil {
			return out, err
		}
		if !slices.Equal(refOf(before), refOf(sh.Ref)) {
			if _, err := c.Ledger.AddEvent(goal.ID, core.EventSpawn, coordinator.SuperviseEvent(sh)); err != nil {
				return out, err
			}
		}
	} else {
		name := strOr(c.Args, "runner", p.Runner)
		if goal.Runner != nil && str(c.Args, "runner") == nil {
			name = *goal.Runner
		}
		rn, err := runner.DetectedRunner(name)
		if err != nil {
			return out, err
		}
		cwd, err := coordinator.SharedWorktree(c.Ledger, goal.ID, p.Path)
		if err != nil {
			return out, err
		}
		if sh, err = rn.Spawn(runner.SpawnOptions{
			Cwd:   cwd,
			Name:  fmt.Sprintf("%s supervise %s", goal.ID, goal.Title),
			Brief: brief,
			Model: p.Model,
		}); err != nil {
			return out, err
		}
		sh.Runner = name
		if _, err := c.Ledger.AddEvent(goal.ID, core.EventSpawn, coordinator.SuperviseEvent(sh)); err != nil {
			return out, err
		}
		said = nil
	}
	if err := c.Ledger.SetFiled(goal.ID, core.TranscriptMark{Session: sh.Session, Entries: len(said)}); err != nil {
		return out, err
	}
	out.Looked = true
	return out, nil
}

func refOf(p *string) []string {
	if p == nil {
		return nil
	}
	return []string{*p}
}

// tend delivers each open task's queued messages to a session that is not
// running, files what a stopped session ends on, and notes one that stopped
// without a report. It reports whether any task is still working.
func (c *Cli) tend(tasks []core.Work, p *project.Project) (bool, error) {
	working := false
	for _, t := range tasks {
		if t.State != core.StateRunning && t.State != core.StateNeedsInput {
			continue
		}
		h, ok, err := coordinator.Handle(c.Ledger, t, p)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		r, err := runner.RunnerNamed(h.Runner)
		if err != nil {
			return false, err
		}
		sent, err := coordinator.Deliver(c.Ledger, t, r, h)
		if err != nil {
			return false, err
		}
		if sent > 0 {
			if t.State == core.StateNeedsInput {
				if _, err := c.Ledger.Transition(t.ID, core.StateRunning); err != nil {
					return false, err
				}
			}
			working = true
			continue
		}
		if t.State != core.StateRunning {
			continue
		}
		status, err := r.Status(h)
		if err != nil {
			return false, err
		}
		texts, err := runner.Transcript(r, h)
		if err != nil {
			return false, err
		}
		if err := c.keepCheckpoints(t, texts); err != nil {
			return false, err
		}
		if status == runner.StatusRunning {
			working = true
			continue
		}
		if _, err := coordinator.Notice(c.Ledger, t, h, texts); err != nil {
			return false, err
		}
		if _, err := coordinator.Stopped(c.Ledger, t, h, texts); err != nil {
			return false, err
		}
	}
	return working, nil
}

// sinceLook is what changed under goal since its supervisor last looked or was
// last read, whether the goal itself was decided on since, and the id of that
// last look or reading.
func (c *Cli) sinceLook(goal core.Work, tasks []core.Work) ([]string, bool, int, error) {
	events, err := c.Ledger.Events(goal.ID, nil)
	if err != nil {
		return nil, false, 0, err
	}
	var ref core.Event
	for _, e := range events {
		if e.Kind == core.EventLook || e.Kind == core.EventSupervised {
			ref = e
		}
	}
	counts := map[string]int{}
	decided := false
	for _, e := range events {
		if e.ID > ref.ID && (e.Kind == core.EventDecision || e.Kind == core.EventAnswer) {
			decided = true
			counts["the goal was decided on"]++
		}
	}
	moved := map[core.EventKind]string{
		core.EventReport: "a task reported", core.EventQuestion: "a task asked", core.EventAnswer: "a task was answered",
		core.EventState: "a task changed state", core.EventNote: "a task was noted on",
	}
	ids := []string{goal.ID}
	for _, t := range tasks {
		ids = append(ids, t.ID)
		evs, err := c.Ledger.Events(t.ID, nil)
		if err != nil {
			return nil, false, 0, err
		}
		for _, e := range evs {
			if why, ok := moved[e.Kind]; ok && e.ID > ref.ID {
				counts[why]++
			}
		}
	}
	for _, id := range ids {
		concerns, err := c.Ledger.Concerns(&id)
		if err != nil {
			return nil, false, 0, err
		}
		for _, cx := range concerns {
			if cx.Resolved == 0 && (ref.ID == 0 || cx.At > ref.At) {
				counts["a concern was raised"]++
			}
		}
	}
	why := []string{}
	for k, n := range counts {
		why = append(why, fmt.Sprintf("%s (%d)", k, n))
	}
	slices.Sort(why)
	if ref.ID == 0 && len(tasks) > 0 && len(why) > 0 {
		why = []string{"the first look"}
	}
	return why, decided, ref.ID, nil
}

// picture is the goal as its supervisor is shown it.
func (c *Cli) picture(goal core.Work, p *project.Project) (coordinator.Picture, error) {
	pic := coordinator.Picture{Goal: goal, Decisions: []string{}, Subs: []coordinator.SubPicture{}, Superseded: []coordinator.SubPicture{}, Concerns: []core.Concern{}}
	kit, err := project.ReadToolkit(p.Path)
	if err != nil {
		return pic, err
	}
	if pic.Checks, err = project.Checks(p, kit); err != nil {
		return pic, err
	}
	if pic.Worktree, err = coordinator.SharedWorktree(c.Ledger, goal.ID, ""); err != nil {
		return pic, err
	}
	events, err := c.Ledger.Events(goal.ID, nil)
	if err != nil {
		return pic, err
	}
	for _, e := range events {
		if e.Kind == core.EventDecision || e.Kind == core.EventAnswer {
			pic.Decisions = append(pic.Decisions, e.Body)
		}
	}
	if pic.Subs, err = c.subPictures(goal.ID, &pic.Superseded); err != nil {
		return pic, err
	}
	tree, err := c.Ledger.Under(goal.ID)
	if err != nil {
		return pic, err
	}
	for _, w := range tree {
		concerns, err := c.Ledger.Concerns(&w.ID)
		if err != nil {
			return pic, err
		}
		for _, cx := range concerns {
			if cx.Resolved == 0 {
				pic.Concerns = append(pic.Concerns, cx)
			}
		}
	}
	return pic, nil
}

// subPictures are the sub-goals under parent as a tree; one a course change
// superseded goes to superseded instead, once, with why.
func (c *Cli) subPictures(parent string, superseded *[]coordinator.SubPicture) ([]coordinator.SubPicture, error) {
	subs, err := c.Ledger.Children(parent)
	if err != nil {
		return nil, err
	}
	out := []coordinator.SubPicture{}
	for _, w := range subs {
		evs, err := c.Ledger.Events(w.ID, nil)
		if err != nil {
			return nil, err
		}
		sp := coordinator.SubPicture{Work: w, Stopped: coordinator.StoppedSaying(evs)}
		for _, e := range evs {
			switch {
			case e.Kind == core.EventReport:
				sp.Report = e.Body
			case e.Kind == core.EventNote && strings.HasPrefix(e.Body, supersededPrefix):
				sp.Superseded = strings.TrimPrefix(e.Body, supersededPrefix)
			}
		}
		if w.State == core.StateSuperseded {
			*superseded = append(*superseded, sp)
			continue
		}
		if sp.After, err = c.Ledger.After(w.ID); err != nil {
			return nil, err
		}
		if sp.Steps, err = c.Ledger.Steps(w.ID); err != nil {
			return nil, err
		}
		if sp.Children, err = c.subPictures(w.ID, superseded); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, nil
}

// supersededPrefix opens the note Supersede files with why.
const supersededPrefix = "superseded: "

// planChange are the actions that change a goal's sub-goals: in ask mode they
// wait for the person.
var planChange = []coordinator.ActionKind{coordinator.ActGoal, coordinator.ActSupersede, coordinator.ActEdit, coordinator.ActAfter}

// carryOut does what the supervisor's reply says, each action on its own, and
// files the reply with what was done and refused as one supervised event. A
// change to the top goal, and in ask mode a change to the plan, is filed as a
// question for the person instead, with what an answer carries out.
func (c *Cli) carryOut(goal core.Work, p *project.Project, reply []string) (supervision, error) {
	out := supervision{Applied: true, Actions: []string{}, Refused: []string{}}
	actions, unread := coordinator.ParseSupervision(reply)
	for _, line := range unread {
		out.Refused = append(out.Refused, line+" — not an action")
	}
	var asked, held []coordinator.Action
	var done, proposed *coordinator.Action
	for _, a := range actions {
		switch {
		case a.Kind == coordinator.ActAsk:
			asked = append(asked, a)
			continue
		case a.Kind == coordinator.ActDone:
			done = &a
			continue
		case a.Kind == coordinator.ActWait:
			continue
		case a.Kind == coordinator.ActPropose:
			proposed = &a
			continue
		case p.Mode == project.ModeAsk && slices.Contains(planChange, a.Kind):
			held = append(held, a)
			continue
		}
		if err := c.applyAction(goal, p, a); err != nil {
			out.Refused = append(out.Refused, a.Line()+" — "+err.Error())
			continue
		}
		out.Actions = append(out.Actions, a.Line())
	}
	var change core.Change
	if len(held) > 0 {
		lines := make([]string, len(held))
		for i, a := range held {
			lines[i] = a.Line()
		}
		change.Actions = lines
		asked = append([]coordinator.Action{{Kind: coordinator.ActAsk, Text: planChangeQuestion + "\n" + strings.Join(lines, "\n"),
			Options: []string{applyChoice, keepPlanChoice}}}, asked...)
	}
	if proposed != nil {
		change.Title, change.DoneWhen, change.Finish, change.Found = proposed.Title, proposed.DoneWhen, proposed.Finish, proposed.Found
		finish := "not decided"
		if goal.FinishLine != nil {
			finish = string(*goal.FinishLine)
		}
		found := ""
		if proposed.Found != "" {
			found = "\nFound: " + proposed.Found
		}
		text := fmt.Sprintf("%s\nNow: %s | done when: %s | finish: %s\nProposed: %s | done when: %s | finish: %s%s\nWhy: %s",
			courseQuestion, goal.Title, textOr(goal.DoneWhen, "not set"), finish, proposed.Title, proposed.DoneWhen, proposed.Finish, found, proposed.Text)
		asked = append([]coordinator.Action{{Kind: coordinator.ActAsk, Text: text, Options: []string{changeCourseChoice, keepGoalChoice}}}, asked...)
	}
	if done != nil {
		open, err := c.unfinished(goal)
		if err != nil {
			return out, err
		}
		if open == "" && (len(held) > 0 || proposed != nil) {
			open = "a change waits for the person"
		}
		if open != "" {
			out.Refused = append(out.Refused, done.Line()+" — "+open)
			done = nil
		}
	}
	if err := coordinator.FileCourse(c.Ledger, goal.ID, reply); err != nil {
		return out, err
	}
	body := coordinator.Said(reply)
	if len(out.Actions) > 0 {
		body += "\n\nDone:\n- " + strings.Join(out.Actions, "\n- ")
	}
	if len(out.Refused) > 0 {
		body += "\n\nRefused:\n- " + strings.Join(out.Refused, "\n- ")
	}
	if _, err := c.Ledger.AddEvent(goal.ID, core.EventSupervised, body); err != nil {
		return out, err
	}
	switch {
	case done != nil:
		if _, err := c.Ledger.AddEvent(goal.ID, core.EventReport, "STATUS: DONE\n"+done.Text); err != nil {
			return out, err
		}
		if _, err := c.Ledger.Transition(goal.ID, core.StateReview); err != nil {
			return out, err
		}
		out.Actions = append(out.Actions, done.Line())
	case len(asked) > 0:
		qbody := coordinator.QuestionBody(asked)
		if len(held) > 0 || proposed != nil {
			if _, err := c.Ledger.Ask(goal.ID, qbody, change); err != nil {
				return out, err
			}
		} else if _, err := c.Ledger.AddEvent(goal.ID, core.EventQuestion, qbody); err != nil {
			return out, err
		}
		if _, err := c.Ledger.Transition(goal.ID, core.StateNeedsInput); err != nil {
			return out, err
		}
		for _, q := range asked {
			if first, _, more := strings.Cut(q.Text, "\n"); more {
				q = coordinator.Action{Kind: coordinator.ActAsk, Text: first, Options: q.Options}
			}
			out.Actions = append(out.Actions, q.Line())
		}
	}
	return out, nil
}

// The questions a supervisor's change is put to the person as, and their
// choices; the first carries the change out.
const (
	planChangeQuestion = "Change the plan?"
	applyChoice        = "Apply these changes (recommended)"
	keepPlanChoice     = "Keep the plan as it is"
	courseQuestion     = "Change course?"
	changeCourseChoice = "Change course (recommended)"
	keepGoalChoice     = "Keep the goal as it is"
)

// applyAction carries out one of a supervisor's actions on goal: anything but
// ASK, DONE, WAIT and PROPOSE.
func (c *Cli) applyAction(goal core.Work, p *project.Project, a coordinator.Action) error {
	tree, err := c.Ledger.Under(goal.ID)
	if err != nil {
		return err
	}
	child := func(id string) (core.Work, error) {
		for _, t := range tree[1:] {
			if t.ID == id {
				return c.Ledger.Get(id)
			}
		}
		return core.Work{}, fmt.Errorf("%s is not a sub-goal of %s", id, goal.ID)
	}
	switch a.Kind {
	case coordinator.ActSend:
		t, err := child(a.Work)
		if err != nil {
			return err
		}
		_, err = c.tellTask(t, a.Text)
		return err
	case coordinator.ActGoal:
		parent := goal
		if a.Work != goal.ID {
			var err error
			if parent, err = child(a.Work); err != nil {
				return err
			}
		}
		w, err := c.addSubGoal(parent, a.Title, a.DoneWhen, a.After, a.Split, core.AnsweredByDirector)
		if err != nil {
			return err
		}
		if waits, err := c.waitingOn(w); err != nil || waits != "" {
			return err
		}
		if core.IsGoal(w.Kind) {
			_, err := c.startGoalRow(goal, p, w, true)
			return err
		}
		return c.startUnder(goal, p, w)
	case coordinator.ActStart:
		t, err := child(a.Work)
		if err != nil {
			return err
		}
		if t.State != core.StateQueued && t.State != core.StateBriefed {
			return fmt.Errorf("%s is %s, already under way", t.ID, t.State)
		}
		if held, err := c.proposed(*t.Parent); err != nil || held {
			if err != nil {
				return err
			}
			return fmt.Errorf("%s is in a proposed breakdown; it waits for wd goal approve %s", t.ID, *t.Parent)
		}
		if core.IsGoal(t.Kind) {
			_, err := c.startGoalRow(goal, p, t, true)
			return err
		}
		return c.startUnder(goal, p, t)
	case coordinator.ActSupersede:
		t, err := child(a.Work)
		if err != nil {
			return err
		}
		_, err = c.supersedeSubGoal(t, a.Text, core.AnsweredByDirector)
		return err
	case coordinator.ActEdit:
		t, err := child(a.Work)
		if err != nil {
			return err
		}
		var title, doneWhen *string
		if a.Title != "" {
			title = &a.Title
		}
		if a.DoneWhen != "" {
			doneWhen = &a.DoneWhen
		}
		_, _, err = c.editSubGoal(t, title, doneWhen, core.AnsweredByDirector)
		return err
	case coordinator.ActAfter:
		t, err := child(a.Work)
		if err != nil {
			return err
		}
		what, err := c.orderSubGoal(t, a.After, core.AnsweredByDirector)
		if err != nil {
			return err
		}
		_, err = c.tellWorking(t.ID, "Your sub-goal now waits on: "+what)
		return err
	case coordinator.ActMet:
		t, err := c.judged(child, a)
		if err != nil {
			return err
		}
		gate, err := c.closeOne(t)
		if err != nil || gate == "" {
			return err
		}
		return fmt.Errorf("judged met, but its %s gate stopped it", gate)
	case coordinator.ActMissed:
		t, err := c.judged(child, a)
		if err != nil {
			return err
		}
		return c.tellMissed(t.ID, a.Text)
	case coordinator.ActChore:
		return c.chore(goal, p, a.Text)
	case coordinator.ActResolve:
		cx, err := c.concernUnder(goal, tree[1:], a.Concern)
		if err != nil {
			return err
		}
		_, err = c.Ledger.ResolveConcern(cx.ID, a.Text)
		return err
	}
	return fmt.Errorf("%s is not carried out here", a.Kind)
}

// judged files a MET or MISSED judgement on the sub-goal it names, which must
// be a task in review, and returns that task.
func (c *Cli) judged(child func(string) (core.Work, error), a coordinator.Action) (core.Work, error) {
	t, err := child(a.Work)
	if err != nil {
		return t, err
	}
	if t.Kind != core.WorkTask || t.State != core.StateReview {
		return t, fmt.Errorf("%s is %s; only a sub-goal in review is judged", t.ID, t.State)
	}
	return t, c.Ledger.Judge(t.ID, a.Kind == coordinator.ActMet, a.Text)
}

// tellMissed tells the agent of sub-goal id, judged missed, what its
// done-when still lacks.
func (c *Cli) tellMissed(id, missing string) error {
	t, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	_, err = c.tellTask(t, "Your sub-goal's done-when is not met yet: "+missing)
	return err
}

// chorePrefix opens the spawn event that records a chore's session on its goal.
const chorePrefix = "chore:"

// chore starts a session in goal's worktree to do what, on the project's
// runner, and files the session and what it is for on the goal.
func (c *Cli) chore(goal core.Work, p *project.Project, what string) error {
	name := p.Runner
	if goal.Runner != nil {
		name = *goal.Runner
	}
	rn, err := runner.DetectedRunner(name)
	if err != nil {
		return err
	}
	cwd, err := coordinator.SharedWorktree(c.Ledger, goal.ID, p.Path)
	if err != nil {
		return err
	}
	h, err := rn.Spawn(runner.SpawnOptions{
		Cwd:   cwd,
		Name:  goal.ID + " chore",
		Brief: fmt.Sprintf("You do one chore for goal %s (%s), for the director: %s\nDo only that, then say what you did.", goal.ID, goal.Title, what),
		Model: p.Model,
	})
	if err != nil {
		return err
	}
	if _, err := c.Ledger.AddEvent(goal.ID, core.EventSpawn, chorePrefix+name+":"+h.Session); err != nil {
		return err
	}
	_, err = c.Ledger.AddEvent(goal.ID, core.EventNote, "chore: "+what)
	return err
}

// unfinished says what holds goal back from done, else "": the first of a
// sub-goal not started or still working, any other sub-goal not settled, a finished sub-goal whose done-when was never judged
// met, and its finish line not reached as far as review goes. Landing waits
// for the person, so a finish line past a local build is held to a local
// build here and to the whole line at soft-done; the director runs the
// project's verify for it when this run has none passing.
func (c *Cli) unfinished(goal core.Work) (string, error) {
	tree, err := c.Ledger.Under(goal.ID)
	if err != nil {
		return "", err
	}
	p, err := c.project(goal.Project)
	if err != nil {
		return "", err
	}
	for _, t := range tree[1:] {
		if t.State == core.StateQueued || t.State == core.StateBriefed {
			return t.ID + " is not started yet: START it, or SUPERSEDE it with why", nil
		}
		if t.State == core.StateRunning && t.Kind == core.WorkTask {
			if working, err := c.stillWorking(t, p); err != nil || working {
				return t.ID + " is still working", err
			}
		}
	}
	for _, t := range tree[1:] {
		if !core.Settled(t.State) {
			return fmt.Sprintf("%s is %s, not settled", t.ID, t.State), nil
		}
	}
	judged := core.EventJudged
	for _, t := range tree[1:] {
		if t.Kind != core.WorkTask || t.State != core.StateDone {
			continue
		}
		evs, err := c.Ledger.Events(t.ID, &judged)
		if err != nil {
			return "", err
		}
		if !core.JudgedMet(evs) {
			return t.ID + " is done but its done-when was never judged met: MET it with why", nil
		}
	}
	if goal.FinishLine == nil {
		return "", nil
	}
	line := *goal.FinishLine
	if line.ChangesCode() {
		line = core.FinishLocalBuild
	}
	run, err := c.Ledger.CurrentRun(goal.ID)
	if err != nil {
		return "", err
	}
	report := core.Event{Kind: core.EventReport, Body: "DONE"}
	if ok, _ := core.FinishReached(line, append(run, report)); !ok && line.ChangesCode() {
		_, body, _, err := c.runVerify(goal.ID)
		if err != nil {
			return "", err
		}
		e, err := c.Ledger.AddEvent(goal.ID, core.EventVerify, body)
		if err != nil {
			return "", err
		}
		run = append(run, e)
	}
	if ok, what := core.FinishReached(line, append(run, report)); !ok {
		return fmt.Sprintf("the finish line (%s) is not reached: %s", *goal.FinishLine, what), nil
	}
	return "", nil
}

// stillWorking reports whether running task t's session is working or has
// messages waiting for it.
func (c *Cli) stillWorking(t core.Work, p *project.Project) (bool, error) {
	h, ok, err := coordinator.Handle(c.Ledger, t, p)
	if err != nil || !ok {
		return true, err
	}
	r, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return false, err
	}
	if n, err := c.Ledger.Undelivered(t.ID); err != nil || len(n) > 0 {
		return true, err
	}
	status, err := r.Status(h)
	return status == runner.StatusRunning, err
}

// concernUnder is concern n when it was raised on goal or one of its tasks.
func (c *Cli) concernUnder(goal core.Work, tasks []core.Work, n int) (core.Concern, error) {
	ids := []string{goal.ID}
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	for _, id := range ids {
		concerns, err := c.Ledger.Concerns(&id)
		if err != nil {
			return core.Concern{}, err
		}
		for _, cx := range concerns {
			if cx.ID == n {
				return cx, nil
			}
		}
	}
	return core.Concern{}, fmt.Errorf("concern %d is not under %s", n, goal.ID)
}

// waitingOn is the first sibling w waits on that is not settled yet, or "".
func (c *Cli) waitingOn(w core.Work) (string, error) {
	ids, err := c.Ledger.After(w.ID)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		o, err := c.Ledger.Get(id)
		if err != nil {
			return "", err
		}
		if !core.Settled(o.State) {
			return id, nil
		}
	}
	return "", nil
}

// mayStart refuses to start w while it waits on an unsettled sibling.
func (c *Cli) mayStart(w core.Work) error {
	id, err := c.waitingOn(w)
	if err != nil || id == "" {
		return err
	}
	return fail("%s waits on %s, which is not settled yet", w.ID, id)
}

// startUnder starts task t of goal on the goal's shared worktree, on the
// project's runners.
func (c *Cli) startUnder(goal core.Work, p *project.Project, t core.Work) error {
	if err := c.mayStart(t); err != nil {
		return err
	}
	runners, err := project.ParseRunnerList(p.Runner, p.Runner, runner.AllRunnerNames)
	if err != nil {
		return err
	}
	detected, err := detectedRunners(runners)
	if err != nil {
		return err
	}
	cwd, err := c.ensureSharedWorktree(goal, p)
	if err != nil {
		return err
	}
	_, _, err = c.spawnTasks(goal, p, runners, detected, []core.Work{t}, cwd)
	return err
}

// startSubGoals starts sub-goals of top goal: each task among them gets an
// agent, and each goal among them is planned and its own sub-goals started,
// unless propose holds that breakdown for approval. Runners are --runner,
// else the project's.
func (c *Cli) startSubGoals(top core.Work, p *project.Project, subs []core.Work, propose bool) ([]string, error) {
	var leaves []core.Work
	var lines []string
	for _, s := range subs {
		if waits, err := c.waitingOn(s); err != nil || waits != "" {
			if err != nil {
				return nil, err
			}
			continue
		}
		if !core.IsGoal(s.Kind) {
			leaves = append(leaves, s)
			continue
		}
		more, err := c.startGoalRow(top, p, s, propose)
		if err != nil {
			return nil, err
		}
		lines = append(lines, more...)
	}
	if len(leaves) == 0 {
		return lines, nil
	}
	runners, err := project.ParseRunnerList(strOr(c.Args, "runner", p.Runner), p.Runner, runner.AllRunnerNames)
	if err != nil {
		return nil, err
	}
	detected, err := detectedRunners(runners)
	if err != nil {
		return nil, err
	}
	cwd, err := c.ensureSharedWorktree(top, p)
	if err != nil {
		return nil, err
	}
	if top, err = c.Ledger.Get(top.ID); err != nil {
		return nil, err
	}
	_, spawned, err := c.spawnTasks(top, p, runners, detected, leaves, cwd)
	return append(lines, spawned...), err
}

// startGoalRow starts a sub-goal stored as a goal: one not yet planned is
// planned, and its sub-goals start unless the breakdown is held for approval.
func (c *Cli) startGoalRow(top core.Work, p *project.Project, g core.Work, propose bool) ([]string, error) {
	subs, err := c.Ledger.Children(g.ID)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		if subs, err = c.planGoal(g, p, propose); err != nil {
			return nil, err
		}
		if propose && p.Mode == project.ModeAsk {
			return []string{fmt.Sprintf("%s: %d sub-goals proposed — wd goal approve %s", g.ID, len(subs), g.ID)}, nil
		}
	}
	return c.startSubGoals(top, p, subs, propose)
}

// keepCheckpoints stores the steps an agent last gave when they differ from
// those stored, and files a SPLIT or OFF-COURSE it raised as a concern on its
// work, once.
func (c *Cli) keepCheckpoints(t core.Work, texts []string) error {
	if steps, ok := coordinator.ParseSteps(texts); ok {
		stored, err := c.Ledger.Steps(t.ID)
		if err != nil {
			return err
		}
		if !slices.Equal(steps, stored) {
			if err := c.Ledger.SetSteps(t.ID, steps); err != nil {
				return err
			}
		}
	}
	var raised []string
	if outcomes, ok := coordinator.ParseSplit(texts); ok {
		lines := make([]string, 0, len(outcomes))
		for _, o := range outcomes {
			lines = append(lines, "- "+o.Title+" | done when: "+textOr(&o.DoneWhen, "not set"))
		}
		raised = append(raised, "SPLIT:\n"+strings.Join(lines, "\n"))
	}
	if found, ok := coordinator.ParseOffCourse(texts); ok {
		raised = append(raised, "OFF-COURSE: "+found)
	}
	if len(raised) == 0 {
		return nil
	}
	concerns, err := c.Ledger.Concerns(&t.ID)
	if err != nil {
		return err
	}
	for _, text := range raised {
		if slices.ContainsFunc(concerns, func(cx core.Concern) bool { return cx.Text == text }) {
			continue
		}
		if _, err := c.Ledger.AddConcern(t.ID, text); err != nil {
			return err
		}
	}
	return nil
}
