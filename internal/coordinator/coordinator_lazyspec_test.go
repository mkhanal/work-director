package coordinator

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
)

func newTestLedger(t *testing.T) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(":memory:")
	if err != nil {
		t.Fatalf("open memory ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func add(t *testing.T, l *ledger.Ledger, project, title string, opts ledger.AddOptions) core.Work {
	t.Helper()
	w, err := l.Add(project, title, opts)
	if err != nil {
		t.Fatalf("add %s: %v", title, err)
	}
	return w
}

func move(t *testing.T, l *ledger.Ledger, id string, to core.State) {
	t.Helper()
	if _, err := l.Transition(id, to); err != nil {
		t.Fatalf("transition %s to %s: %v", id, to, err)
	}
}

func setSession(t *testing.T, l *ledger.Ledger, id, session string) {
	t.Helper()
	if err := l.SetSession(id, ledger.SessionInfo{Runner: "fake", Session: session}); err != nil {
		t.Fatalf("set session %s: %v", id, err)
	}
}

func eventBodies(t *testing.T, l *ledger.Ledger, work string, kind core.EventKind) []string {
	t.Helper()
	evs, err := l.Events(work, &kind)
	if err != nil {
		t.Fatalf("events %s: %v", work, err)
	}
	var out []string
	for _, e := range evs {
		out = append(out, e.Body)
	}
	return out
}

// fakeRunner is a Runner with a canned transcript that records its sends.
type fakeRunner struct {
	busy     bool
	texts    []string
	asking   *runner.Question
	sends    []string
	sentTo   []runner.Handle
	resolved []string
	cwds     []string
	sessions []string
}

func (f *fakeRunner) Name() string { return "fake" }

func (f *fakeRunner) Command() string { return "fake" }
func (f *fakeRunner) Spawn(o runner.SpawnOptions) (runner.Handle, error) {
	return runner.Handle{Runner: "fake"}, nil
}

// Send numbers its refs: each send starts a new process.
func (f *fakeRunner) Send(h *runner.Handle, text string) error {
	f.sends = append(f.sends, text)
	f.sentTo = append(f.sentTo, *h)
	ref := fmt.Sprintf("pid:%d", len(f.sends))
	h.Ref = &ref
	return nil
}
func (f *fakeRunner) Status(h runner.Handle) (runner.RunnerStatus, error) {
	if f.busy {
		return runner.StatusRunning, nil
	}
	return runner.StatusIdle, nil
}
func (f *fakeRunner) Conversation(h runner.Handle) ([]runner.Entry, error) {
	f.cwds = append(f.cwds, h.Cwd)
	f.sessions = append(f.sessions, h.Session)
	out := make([]runner.Entry, 0, len(f.texts)+1)
	for _, t := range f.texts {
		out = append(out, runner.Entry{Kind: runner.EntryText, Text: t})
	}
	if f.asking != nil {
		out = append(out, runner.Entry{Kind: runner.EntryQuestion, Question: f.asking})
	}
	return out, nil
}
func (f *fakeRunner) Models() ([]string, error)         { return nil, nil }
func (f *fakeRunner) AttachHint(h runner.Handle) string { return "fake attach" }
func (f *fakeRunner) Stop(h runner.Handle) error        { return nil }

func resolveFake(fr *fakeRunner) func(name string) (runner.Runner, error) {
	return func(name string) (runner.Runner, error) {
		fr.resolved = append(fr.resolved, name)
		return fr, nil
	}
}

// proj is the project every test goal belongs to.
var proj = &project.Project{Name: "proj", Path: "/repo/proj", Runner: "project-runner"}

func TestCoordinator(t *testing.T) {
	t.Run("The Goal Plan Brief Names The Project And Verify Commands", func(t *testing.T) {
		goal := core.Work{ID: "e1", Title: "Rewrite the core", Detail: "in Go", Kind: core.WorkEpic}
		p := &project.Project{Name: "wd", Path: "/repo", Verify: []string{"go test", "go vet"}}
		brief := GoalPlanBrief(goal, p)
		for _, want := range []string{
			"wd (/repo)", "`go test`", "`go vet`", "Rewrite the core", "in Go",
			"## <heading>", "- [ ] <task title>",
		} {
			if !strings.Contains(brief, want) {
				t.Fatalf("brief missing %q:\n%s", want, brief)
			}
		}
		none := &project.Project{Name: "wd", Path: "/repo"}
		if brief := GoalPlanBrief(goal, none); !strings.Contains(brief, "none listed") {
			t.Fatalf("brief without verify missing %q:\n%s", "none listed", brief)
		}
	})

	t.Run("A Heading Line Opens A Group", func(t *testing.T) {
		tasks := ParsePlan([]string{"## Core\n- [ ] port the ledger"})
		if len(tasks) != 1 || tasks[0].Heading != "Core" || tasks[0].Title != "port the ledger" {
			t.Fatalf("tasks = %+v, want one Core/port the ledger task", tasks)
		}
	})

	t.Run("A Task Line Is A Task", func(t *testing.T) {
		tasks := ParsePlan([]string{"- [ ] one\n- [x] two\n* [X] three\nnot a task\n- four"})
		want := []PlanTask{
			{Heading: "(no heading)", Title: "one"},
			{Heading: "(no heading)", Title: "two"},
			{Heading: "(no heading)", Title: "three"},
		}
		if !slices.Equal(tasks, want) {
			t.Fatalf("tasks = %+v, want %+v", tasks, want)
		}
	})

	t.Run("Tasks Before Any Heading Fall Under A Default Heading", func(t *testing.T) {
		tasks := ParsePlan([]string{"- [ ] early\n## Late\n- [ ] after"})
		want := []PlanTask{
			{Heading: "(no heading)", Title: "early"},
			{Heading: "Late", Title: "after"},
		}
		if !slices.Equal(tasks, want) {
			t.Fatalf("tasks = %+v, want %+v", tasks, want)
		}
	})

	t.Run("A Resolved Concern's Decision Can Answer", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		c, err := l.AddConcern(goal.ID, "which database driver?")
		if err != nil {
			t.Fatalf("add concern: %v", err)
		}
		if _, err := l.ResolveConcern(c.ID, "use modernc.org/sqlite as the database driver"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		answer, err := KnownAnswer("which database driver should we use?", []core.Work{goal, child}, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "use modernc.org/sqlite as the database driver" {
			t.Fatalf("answer = %q, want the concern decision", answer)
		}
	})

	t.Run("A Decision Event Can Answer", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		if _, err := l.AddEvent(child.ID, core.EventDecision, "the verify command is go test ./..."); err != nil {
			t.Fatalf("add event: %v", err)
		}
		answer, err := KnownAnswer("which verify command should we run?", []core.Work{goal, child}, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "the verify command is go test ./..." {
			t.Fatalf("answer = %q, want the decision event body", answer)
		}
	})

	t.Run("Exact Containment Wins", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		if _, err := l.AddConcern(goal.ID, "q1"); err != nil {
			t.Fatalf("add concern: %v", err)
		}
		cs, err := l.Concerns(&goal.ID)
		if err != nil {
			t.Fatalf("concerns: %v", err)
		}
		if _, err := l.ResolveConcern(cs[0].ID, "the database driver uses sqlite everywhere"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		if _, err := l.AddEvent(goal.ID, core.EventDecision, "use the sqlite driver for the database"); err != nil {
			t.Fatalf("add event: %v", err)
		}
		answer, err := KnownAnswer("sqlite driver", []core.Work{goal}, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "use the sqlite driver for the database" {
			t.Fatalf("answer = %q, want the containing candidate", answer)
		}
	})

	t.Run("Two Shared Significant Words Answer", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		c, err := l.AddConcern(goal.ID, "q1")
		if err != nil {
			t.Fatalf("add concern: %v", err)
		}
		if _, err := l.ResolveConcern(c.ID, "the formatter and linter are go fmt and go vet"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		answer, err := KnownAnswer("which formatter and linter do we use?", []core.Work{goal}, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "the formatter and linter are go fmt and go vet" {
			t.Fatalf("answer = %q, want the two-shared-word candidate", answer)
		}
		c2, err := l.AddConcern(goal.ID, "q2")
		if err != nil {
			t.Fatalf("add concern: %v", err)
		}
		if _, err := l.ResolveConcern(c2.ID, "the formatter is go fmt"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		answer, err = KnownAnswer("which formatter do we use?", []core.Work{goal}, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "" {
			t.Fatalf("answer = %q, want none for a single shared word", answer)
		}
	})

	t.Run("A Known Question Is Answered", func(t *testing.T) {
		t.Run("a running child stays running", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			c, err := l.AddConcern(goal.ID, "which database driver?")
			if err != nil {
				t.Fatalf("add concern: %v", err)
			}
			if _, err := l.ResolveConcern(c.ID, "use modernc.org/sqlite as the database driver"); err != nil {
				t.Fatalf("resolve concern: %v", err)
			}
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"ASK: which database driver should we use?"}}
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("coordinateOnce: %v", err)
			}
			if !slices.Equal(res.Answered, []string{child.ID}) {
				t.Fatalf("answered = %v, want [%s]", res.Answered, child.ID)
			}
			if !slices.Equal(fr.sends, []string{"use modernc.org/sqlite as the database driver"}) {
				t.Fatalf("sends = %v, want the answer", fr.sends)
			}
			if got := eventBodies(t, l, child.ID, core.EventQuestion); !slices.Equal(got, []string{"which database driver should we use?"}) {
				t.Fatalf("question events = %v", got)
			}
			if got := eventBodies(t, l, child.ID, core.EventAnswer); !slices.Equal(got, []string{"use modernc.org/sqlite as the database driver"}) {
				t.Fatalf("answer events = %v", got)
			}
			w, err := l.Get(child.ID)
			if err != nil {
				t.Fatalf("get child: %v", err)
			}
			if w.State != core.StateRunning {
				t.Fatalf("state = %s, want running", w.State)
			}
		})
		t.Run("a needs-input child returns to running", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			c, err := l.AddConcern(goal.ID, "which database driver?")
			if err != nil {
				t.Fatalf("add concern: %v", err)
			}
			if _, err := l.ResolveConcern(c.ID, "use modernc.org/sqlite as the database driver"); err != nil {
				t.Fatalf("resolve concern: %v", err)
			}
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			move(t, l, child.ID, core.StateNeedsInput)
			fr := &fakeRunner{texts: []string{"ASK: which database driver should we use?"}}
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("coordinateOnce: %v", err)
			}
			if !slices.Equal(res.Answered, []string{child.ID}) {
				t.Fatalf("answered = %v, want [%s]", res.Answered, child.ID)
			}
			w, err := l.Get(child.ID)
			if err != nil {
				t.Fatalf("get child: %v", err)
			}
			if w.State != core.StateRunning {
				t.Fatalf("state = %s, want running", w.State)
			}
		})
	})

	t.Run("An Unknown Question Escalates", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"ASK: what is the meaning of life?"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Escalated, []string{child.ID}) {
			t.Fatalf("escalated = %v, want [%s]", res.Escalated, child.ID)
		}
		w, err := l.Get(child.ID)
		if err != nil {
			t.Fatalf("get child: %v", err)
		}
		if w.State != core.StateNeedsInput {
			t.Fatalf("state = %s, want needs-input", w.State)
		}
	})

	t.Run("A Done Report Moves The Child To Review", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"STATUS: DONE"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Reviewed, []string{child.ID}) {
			t.Fatalf("reviewed = %v, want [%s]", res.Reviewed, child.ID)
		}
		if got := eventBodies(t, l, child.ID, core.EventReport); !slices.Equal(got, []string{"DONE\nSTATUS: DONE"}) {
			t.Fatalf("report events = %v", got)
		}
		w, err := l.Get(child.ID)
		if err != nil {
			t.Fatalf("get child: %v", err)
		}
		if w.State != core.StateReview {
			t.Fatalf("state = %s, want review", w.State)
		}
	})

	t.Run("A Blocked Report Moves The Child To Blocked", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"STATUS: BLOCKED"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Blocked, []string{child.ID}) {
			t.Fatalf("blocked = %v, want [%s]", res.Blocked, child.ID)
		}
		w, err := l.Get(child.ID)
		if err != nil {
			t.Fatalf("get child: %v", err)
		}
		if w.State != core.StateBlocked {
			t.Fatalf("state = %s, want blocked", w.State)
		}
	})

	t.Run("A Queued Message Reaches Its Session Once The Session Is Idle", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkGoal})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		move(t, l, child.ID, core.StateNeedsInput)
		for _, text := range []string{"use postgres", "keep the old API"} {
			if _, err := l.QueueMessage(child.ID, text); err != nil {
				t.Fatalf("queue: %v", err)
			}
		}
		fr := &fakeRunner{busy: true, texts: []string{"working"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if len(fr.sends) != 0 || len(res.Delivered) != 0 {
			t.Fatalf("a busy session was sent %v (delivered %v), want nothing yet", fr.sends, res.Delivered)
		}
		fr.busy = false
		res, err = CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(fr.sends, []string{"use postgres\n\nkeep the old API"}) || !slices.Equal(res.Delivered, []string{child.ID}) {
			t.Fatalf("sends = %q, delivered = %v, want both messages as one send, in order, to %s", fr.sends, res.Delivered, child.ID)
		}
		left, err := l.Undelivered(child.ID)
		if err != nil || len(left) != 0 {
			t.Fatalf("undelivered = %v (%v), want none", left, err)
		}
		w, err := l.Get(child.ID)
		if err != nil || w.State != core.StateRunning {
			t.Fatalf("child = %s (%v), want running once it was told something", w.State, err)
		}
	})

	t.Run("A Child Without A Session Waits", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"READY"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Waiting, []string{child.ID}) {
			t.Fatalf("waiting = %v, want [%s]", res.Waiting, child.ID)
		}
	})

	t.Run("A Child With Nothing New Waits", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"READY"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Waiting, []string{child.ID}) {
			t.Fatalf("waiting = %v, want [%s]", res.Waiting, child.ID)
		}
	})

	t.Run("A Child With A Claimed Session Uses The Claim", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		if _, err := l.SetClaim(child.ID, strPtr("ses_claim")); err != nil {
			t.Fatalf("set claim: %v", err)
		}
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"STATUS: DONE"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Reviewed, []string{child.ID}) {
			t.Fatalf("reviewed = %v, want [%s]", res.Reviewed, child.ID)
		}
	})

	t.Run("A Needs-Input Report Escalates", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"STATUS: NEEDS-INPUT\nNOTES: pick a queue"}}
		res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Escalated, []string{child.ID}) {
			t.Fatalf("escalated = %v, want [%s]", res.Escalated, child.ID)
		}
		if got := eventBodies(t, l, child.ID, core.EventReport); !slices.Equal(got, []string{"NEEDS-INPUT\nSTATUS: NEEDS-INPUT\nNOTES: pick a queue"}) {
			t.Fatalf("report events = %v", got)
		}
		w, err := l.Get(child.ID)
		if err != nil {
			t.Fatalf("get child: %v", err)
		}
		if w.State != core.StateNeedsInput {
			t.Fatalf("state = %s, want needs-input", w.State)
		}
	})

	t.Run("A Question Already Filed Is Not Filed Again", func(t *testing.T) {
		t.Run("an escalated question stays escalated", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"ASK: what is the meaning of life?"}}
			for pass := 0; pass < 2; pass++ {
				res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
				if err != nil {
					t.Fatalf("pass %d: %v", pass, err)
				}
				if !slices.Equal(res.Escalated, []string{child.ID}) {
					t.Fatalf("pass %d escalated = %v, want [%s]", pass, res.Escalated, child.ID)
				}
			}
			if got := eventBodies(t, l, child.ID, core.EventQuestion); len(got) != 1 {
				t.Fatalf("question events = %v, want one", got)
			}
		})
		t.Run("an unanswered needs-input report stays escalated", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"STATUS: NEEDS-INPUT\nNOTES: pick a queue"}}
			for pass := 0; pass < 2; pass++ {
				res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
				if err != nil {
					t.Fatalf("pass %d: %v", pass, err)
				}
				if !slices.Equal(res.Escalated, []string{child.ID}) {
					t.Fatalf("pass %d escalated = %v, want [%s]", pass, res.Escalated, child.ID)
				}
			}
			if got := eventBodies(t, l, child.ID, core.EventReport); len(got) != 1 {
				t.Fatalf("report events = %v, want one", got)
			}
			w, err := l.Get(child.ID)
			if err != nil {
				t.Fatalf("get child: %v", err)
			}
			if w.State != core.StateNeedsInput {
				t.Fatalf("state = %s, want needs-input", w.State)
			}
		})
		t.Run("an answered question waits", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			if _, err := l.AddEvent(goal.ID, core.EventDecision, "use modernc.org/sqlite as the database driver"); err != nil {
				t.Fatalf("decision: %v", err)
			}
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"ASK: which database driver should we use?"}}
			if _, err := CoordinateOnce(goal, proj, l, resolveFake(fr)); err != nil {
				t.Fatalf("first pass: %v", err)
			}
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("second pass: %v", err)
			}
			if !slices.Equal(res.Waiting, []string{child.ID}) {
				t.Fatalf("waiting = %v, want [%s]", res.Waiting, child.ID)
			}
			if len(fr.sends) != 1 {
				t.Fatalf("sends = %v, want the answer once", fr.sends)
			}
			if got := eventBodies(t, l, child.ID, core.EventQuestion); len(got) != 1 {
				t.Fatalf("question events = %v, want one", got)
			}
		})
		t.Run("a needs-input report answered by a human waits", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"STATUS: NEEDS-INPUT"}}
			if _, err := CoordinateOnce(goal, proj, l, resolveFake(fr)); err != nil {
				t.Fatalf("first pass: %v", err)
			}
			move(t, l, child.ID, core.StateRunning)
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("second pass: %v", err)
			}
			if !slices.Equal(res.Waiting, []string{child.ID}) {
				t.Fatalf("waiting = %v, want [%s]", res.Waiting, child.ID)
			}
			if got := eventBodies(t, l, child.ID, core.EventReport); len(got) != 1 {
				t.Fatalf("report events = %v, want one", got)
			}
		})
	})

	t.Run("A Question Asked Again Is Filed Again", func(t *testing.T) {
		t.Run("an answered question asked again is answered again", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			if _, err := l.AddEvent(goal.ID, core.EventDecision, "use modernc.org/sqlite as the database driver"); err != nil {
				t.Fatalf("decision: %v", err)
			}
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			ask := "ASK: which database driver should we use?"
			fr := &fakeRunner{texts: []string{ask}}
			for pass := 0; pass < 2; pass++ {
				if _, err := CoordinateOnce(goal, proj, l, resolveFake(fr)); err != nil {
					t.Fatalf("pass %d: %v", pass, err)
				}
			}
			fr.texts = append(fr.texts, ask)
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("pass after asking again: %v", err)
			}
			if !slices.Equal(res.Answered, []string{child.ID}) {
				t.Fatalf("answered = %v, want [%s]", res.Answered, child.ID)
			}
			if len(fr.sends) != 2 {
				t.Fatalf("sends = %v, want the answer twice", fr.sends)
			}
			if got := eventBodies(t, l, child.ID, core.EventQuestion); len(got) != 2 {
				t.Fatalf("question events = %v, want two", got)
			}
		})
		t.Run("a needs-input report made again escalates again", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"STATUS: NEEDS-INPUT"}}
			if _, err := CoordinateOnce(goal, proj, l, resolveFake(fr)); err != nil {
				t.Fatalf("first pass: %v", err)
			}
			move(t, l, child.ID, core.StateRunning)
			if _, err := CoordinateOnce(goal, proj, l, resolveFake(fr)); err != nil {
				t.Fatalf("second pass: %v", err)
			}
			fr.texts = append(fr.texts, "STATUS: NEEDS-INPUT")
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("pass after reporting again: %v", err)
			}
			if !slices.Equal(res.Escalated, []string{child.ID}) {
				t.Fatalf("escalated = %v, want [%s]", res.Escalated, child.ID)
			}
			if got := eventBodies(t, l, child.ID, core.EventReport); len(got) != 2 {
				t.Fatalf("report events = %v, want two", got)
			}
			w, err := l.Get(child.ID)
			if err != nil {
				t.Fatalf("get child: %v", err)
			}
			if w.State != core.StateNeedsInput {
				t.Fatalf("state = %s, want needs-input", w.State)
			}
		})
		t.Run("a new session asking the same question is answered", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			if _, err := l.AddEvent(goal.ID, core.EventDecision, "use modernc.org/sqlite as the database driver"); err != nil {
				t.Fatalf("decision: %v", err)
			}
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"ASK: which database driver should we use?"}}
			if _, err := CoordinateOnce(goal, proj, l, resolveFake(fr)); err != nil {
				t.Fatalf("first pass: %v", err)
			}
			setSession(t, l, child.ID, "ses_2")
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("pass in the new session: %v", err)
			}
			if !slices.Equal(res.Answered, []string{child.ID}) {
				t.Fatalf("answered = %v, want [%s]", res.Answered, child.ID)
			}
			if got := eventBodies(t, l, child.ID, core.EventQuestion); len(got) != 2 {
				t.Fatalf("question events = %v, want two", got)
			}
			if len(fr.sentTo) != 2 || fr.sentTo[1].Session != "ses_2" {
				t.Fatalf("sent to %+v, want the answer sent again, to ses_2", fr.sentTo)
			}
		})
		t.Run("a new session reporting needs-input escalates again", func(t *testing.T) {
			l := newTestLedger(t)
			goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"STATUS: NEEDS-INPUT"}}
			if _, err := CoordinateOnce(goal, proj, l, resolveFake(fr)); err != nil {
				t.Fatalf("first pass: %v", err)
			}
			setSession(t, l, child.ID, "ses_2")
			res, err := CoordinateOnce(goal, proj, l, resolveFake(fr))
			if err != nil {
				t.Fatalf("pass in the new session: %v", err)
			}
			if !slices.Equal(res.Escalated, []string{child.ID}) {
				t.Fatalf("escalated = %v, want [%s]", res.Escalated, child.ID)
			}
			if got := eventBodies(t, l, child.ID, core.EventReport); len(got) != 2 {
				t.Fatalf("report events = %v, want two", got)
			}
			w, err := l.Get(child.ID)
			if err != nil {
				t.Fatalf("get child: %v", err)
			}
			if w.State != core.StateNeedsInput {
				t.Fatalf("state = %s, want needs-input", w.State)
			}
		})
	})

	t.Run("A Child Without Its Own Runner Or Directory Uses Its Goal's Then Its Project's", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		if _, err := l.SetClaim(child.ID, strPtr("ses_claim")); err != nil {
			t.Fatalf("set claim: %v", err)
		}
		move(t, l, child.ID, core.StateRunning)
		reaches := func(stage, runnerName, session, cwd string) {
			t.Helper()
			e, err := l.Get(goal.ID)
			if err != nil {
				t.Fatalf("get goal: %v", err)
			}
			fr := &fakeRunner{texts: []string{"READY"}}
			if _, err := CoordinateOnce(e, proj, l, resolveFake(fr)); err != nil {
				t.Fatalf("%s: coordinateOnce: %v", stage, err)
			}
			if !slices.Equal(fr.resolved, []string{runnerName}) || !slices.Equal(fr.sessions, []string{session}) || !slices.Equal(fr.cwds, []string{cwd}) {
				t.Fatalf("%s: reached %v %v in %v, want %s %s in %s", stage, fr.resolved, fr.sessions, fr.cwds, runnerName, session, cwd)
			}
		}
		reaches("no goal runner, no worktree", "project-runner", "ses_claim", "/repo/proj")
		if err := l.SetSession(goal.ID, ledger.SessionInfo{Runner: "goal-runner", Session: "ses_e", Cwd: "/goal/own"}); err != nil {
			t.Fatalf("goal session: %v", err)
		}
		reaches("goal runner, no worktree", "goal-runner", "ses_claim", "/repo/proj")
		wt, err := l.AddWorktree(goal.ID, ledger.WorktreeInfo{Path: "/wt/shared", Kind: core.WorktreeShared, Origin: core.OriginDirector})
		if err != nil {
			t.Fatalf("shared worktree: %v", err)
		}
		reaches("active shared worktree", "goal-runner", "ses_claim", "/wt/shared")
		if _, err := l.SetWorktreeState(wt.ID, core.WorktreeMerged); err != nil {
			t.Fatalf("merge worktree: %v", err)
		}
		reaches("merged shared worktree", "goal-runner", "ses_claim", "/repo/proj")
		if err := l.SetSession(child.ID, ledger.SessionInfo{Runner: "own-runner", Session: "ses_own", Cwd: "/own"}); err != nil {
			t.Fatalf("child session: %v", err)
		}
		reaches("its own runner and directory", "own-runner", "ses_own", "/own")
	})

	t.Run("Sending Records The Ref The Runner Returns", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "proj", "goal", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &goal.ID})
		if _, err := l.SetClaim(child.ID, strPtr("ses_claim")); err != nil {
			t.Fatalf("set claim: %v", err)
		}
		if err := l.SetSession(goal.ID, ledger.SessionInfo{Runner: "goal-runner", Session: "ses_e", Cwd: "/goal/own"}); err != nil {
			t.Fatalf("goal session: %v", err)
		}
		first, err := l.AddWorktree(goal.ID, ledger.WorktreeInfo{Path: "/wt/first", Kind: core.WorktreeShared, Origin: core.OriginDirector})
		if err != nil {
			t.Fatalf("shared worktree: %v", err)
		}
		w, err := l.Get(child.ID)
		if err != nil {
			t.Fatalf("get child: %v", err)
		}
		h, _, err := Handle(l, w, proj)
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		fr := &fakeRunner{}
		if err := Send(l, child.ID, fr, h, "more"); err != nil {
			t.Fatalf("send: %v", err)
		}
		if len(fr.sentTo) != 1 || fr.sentTo[0].Runner != "goal-runner" || fr.sentTo[0].Session != "ses_claim" || fr.sentTo[0].Cwd != "/wt/first" || fr.sends[0] != "more" {
			t.Fatalf("sent %v to %+v, want more to goal-runner ses_claim in /wt/first", fr.sends, fr.sentTo)
		}
		w, err = l.Get(child.ID)
		if err != nil {
			t.Fatalf("get child: %v", err)
		}
		if w.Ref == nil || *w.Ref != "pid:1" || w.Runner != nil || w.Cwd != nil {
			t.Fatalf("child = ref %v runner %v cwd %v, want ref pid:1 and no runner or cwd of its own", w.Ref, w.Runner, w.Cwd)
		}
		if err := l.SetSession(goal.ID, ledger.SessionInfo{Runner: "next-runner", Session: "ses_e2", Cwd: "/goal/own"}); err != nil {
			t.Fatalf("goal session: %v", err)
		}
		if _, err := l.SetWorktreeState(first.ID, core.WorktreeMerged); err != nil {
			t.Fatalf("merge worktree: %v", err)
		}
		if _, err := l.AddWorktree(goal.ID, ledger.WorktreeInfo{Path: "/wt/second", Kind: core.WorktreeShared, Origin: core.OriginDirector}); err != nil {
			t.Fatalf("shared worktree: %v", err)
		}
		h, _, err = Handle(l, w, proj)
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if h.Runner != "next-runner" || h.Session != "ses_claim" || h.Cwd != "/wt/second" || h.Ref == nil || *h.Ref != "pid:1" {
			t.Fatalf("handle after send = %+v, want next-runner ses_claim in /wt/second at pid:1", h)
		}
	})
}

func strPtr(s string) *string { return &s }
