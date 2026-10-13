package cli

import (
	"fmt"
	"slices"
	"strings"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
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
		if status == runner.StatusRunning {
			working = true
			continue
		}
		texts, err := runner.Transcript(r, h)
		if err != nil {
			return false, err
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
	pic := coordinator.Picture{Goal: goal, Decisions: []string{}, Tasks: []coordinator.TaskPicture{}, Concerns: []core.Concern{}}
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
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return pic, err
	}
	ids := []string{goal.ID}
	for _, t := range tasks {
		ids = append(ids, t.ID)
		evs, err := c.Ledger.Events(t.ID, nil)
		if err != nil {
			return pic, err
		}
		tp := coordinator.TaskPicture{Work: t, Stopped: coordinator.StoppedSaying(evs)}
		for _, e := range evs {
			if e.Kind == core.EventReport {
				tp.Report = e.Body
			}
		}
		pic.Tasks = append(pic.Tasks, tp)
	}
	for _, id := range ids {
		concerns, err := c.Ledger.Concerns(&id)
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

// carryOut does what the supervisor's reply says, each action on its own, and
// files the reply with what was done and refused as one supervised event.
func (c *Cli) carryOut(goal core.Work, p *project.Project, reply []string) (supervision, error) {
	out := supervision{Applied: true, Actions: []string{}, Refused: []string{}}
	actions, unread := coordinator.ParseSupervision(reply)
	for _, line := range unread {
		out.Refused = append(out.Refused, line+" — not an action")
	}
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return out, err
	}
	tree, err := c.Ledger.Under(goal.ID)
	if err != nil {
		return out, err
	}
	child := func(id string) (core.Work, error) {
		for _, t := range tree[1:] {
			if t.ID == id {
				return c.Ledger.Get(id)
			}
		}
		return core.Work{}, fmt.Errorf("%s is not a task of %s", id, goal.ID)
	}
	var asked []coordinator.Action
	var done *coordinator.Action
	for _, a := range actions {
		err := func() error {
			switch a.Kind {
			case coordinator.ActSend:
				t, err := child(a.Work)
				if err != nil {
					return err
				}
				_, err = c.tellTask(t, a.Text)
				return err
			case coordinator.ActTask:
				t, err := c.Ledger.Add(goal.Project, a.Text, ledger.AddOptions{Parent: &goal.ID})
				if err != nil {
					return err
				}
				return c.startUnder(goal, p, t)
			case coordinator.ActStart:
				t, err := child(a.Work)
				if err != nil {
					return err
				}
				if t.State != core.StateQueued && t.State != core.StateBriefed {
					return fmt.Errorf("%s is %s, already under way", t.ID, t.State)
				}
				if core.IsGoal(t.Kind) {
					_, err := c.startGoalRow(goal, p, t, true)
					return err
				}
				return c.startUnder(goal, p, t)
			case coordinator.ActDrop:
				t, err := child(a.Work)
				if err != nil {
					return err
				}
				if t.State != core.StateQueued && t.State != core.StateBriefed {
					return fmt.Errorf("%s is %s, already under way", t.ID, t.State)
				}
				if _, err := c.Ledger.AddEvent(t.ID, core.EventNote, "dropped by its goal's supervisor: "+a.Text); err != nil {
					return err
				}
				_, err = c.Ledger.Transition(t.ID, core.StateDropped)
				return err
			case coordinator.ActResolve:
				cx, err := c.concernUnder(goal, tasks, a.Concern)
				if err != nil {
					return err
				}
				_, err = c.Ledger.ResolveConcern(cx.ID, a.Text)
				return err
			case coordinator.ActAsk:
				asked = append(asked, a)
			case coordinator.ActDone:
				done = &a
			}
			return nil
		}()
		if err != nil {
			out.Refused = append(out.Refused, a.Line()+" — "+err.Error())
			continue
		}
		if a.Kind != coordinator.ActDone && a.Kind != coordinator.ActWait {
			out.Actions = append(out.Actions, a.Line())
		}
	}
	if done != nil {
		if open, err := c.unfinished(goal); err != nil {
			return out, err
		} else if open != "" {
			out.Refused = append(out.Refused, done.Line()+" — "+open)
			done = nil
		}
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
		if _, err := c.Ledger.AddEvent(goal.ID, core.EventQuestion, coordinator.QuestionBody(asked)); err != nil {
			return out, err
		}
		if _, err := c.Ledger.Transition(goal.ID, core.StateNeedsInput); err != nil {
			return out, err
		}
		for _, q := range asked {
			out.Actions = append(out.Actions, q.Line())
		}
	}
	return out, nil
}

// unfinished says which task of goal holds it back from done: one not started
// yet, or one running with a session that is; else "".
func (c *Cli) unfinished(goal core.Work) (string, error) {
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return "", err
	}
	p, err := c.project(goal.Project)
	if err != nil {
		return "", err
	}
	for _, t := range tasks {
		if t.State == core.StateQueued || t.State == core.StateBriefed {
			return t.ID + " is not started yet: START it, or DROP it with why", nil
		}
		if t.State != core.StateRunning {
			continue
		}
		h, ok, err := coordinator.Handle(c.Ledger, t, p)
		if err != nil || !ok {
			return t.ID + " is still working", err
		}
		r, err := runner.RunnerNamed(h.Runner)
		if err != nil {
			return "", err
		}
		if n, err := c.Ledger.Undelivered(t.ID); err != nil {
			return "", err
		} else if len(n) > 0 {
			return t.ID + " is still working", nil
		}
		status, err := r.Status(h)
		if err != nil {
			return "", err
		}
		if status == runner.StatusRunning {
			return t.ID + " is still working", nil
		}
	}
	return "", nil
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

// startUnder starts task t of goal on the goal's shared worktree, on the
// project's runners.
func (c *Cli) startUnder(goal core.Work, p *project.Project, t core.Work) error {
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
