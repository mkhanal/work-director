package coordinator

import (
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
	texts []string
	sends []string
}

func (f *fakeRunner) Name() string { return "fake" }
func (f *fakeRunner) Spawn(o runner.SpawnOptions) (runner.Handle, error) {
	return runner.Handle{Runner: "fake"}, nil
}
func (f *fakeRunner) Send(h *runner.Handle, text string) error {
	f.sends = append(f.sends, text)
	return nil
}
func (f *fakeRunner) Status(h runner.Handle) (runner.RunnerStatus, error) {
	return runner.StatusIdle, nil
}
func (f *fakeRunner) Transcript(h runner.Handle) ([]string, error) { return f.texts, nil }
func (f *fakeRunner) Models() ([]string, error)                    { return nil, nil }
func (f *fakeRunner) AttachHint(h runner.Handle) string            { return "fake attach" }

func resolveFake(fr *fakeRunner) func(name string) (runner.Runner, error) {
	return func(name string) (runner.Runner, error) { return fr, nil }
}

func TestCoordinator(t *testing.T) {
	t.Run("The Epic Plan Brief Names The Project And Verify Commands", func(t *testing.T) {
		epic := core.Work{ID: "e1", Title: "Rewrite the core", Detail: "in Go", Kind: core.WorkEpic}
		p := &project.Project{Name: "wd", Path: "/repo", Verify: []string{"go test", "go vet"}}
		brief := EpicPlanBrief(epic, p)
		for _, want := range []string{
			"wd (/repo)", "`go test`", "`go vet`", "Rewrite the core", "in Go",
			"## <heading>", "- [ ] <task title>",
		} {
			if !strings.Contains(brief, want) {
				t.Fatalf("brief missing %q:\n%s", want, brief)
			}
		}
		none := &project.Project{Name: "wd", Path: "/repo"}
		if brief := EpicPlanBrief(epic, none); !strings.Contains(brief, "none listed") {
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
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		c, err := l.AddConcern(epic.ID, "which database driver?")
		if err != nil {
			t.Fatalf("add concern: %v", err)
		}
		if _, err := l.ResolveConcern(c.ID, "use modernc.org/sqlite as the database driver"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		answer, err := KnownAnswer("which database driver should we use?", epic, []core.Work{child}, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "use modernc.org/sqlite as the database driver" {
			t.Fatalf("answer = %q, want the concern decision", answer)
		}
	})

	t.Run("A Decision Event Can Answer", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		if err := l.AddEvent(child.ID, core.EventDecision, "the verify command is go test ./..."); err != nil {
			t.Fatalf("add event: %v", err)
		}
		answer, err := KnownAnswer("which verify command should we run?", epic, []core.Work{child}, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "the verify command is go test ./..." {
			t.Fatalf("answer = %q, want the decision event body", answer)
		}
	})

	t.Run("Exact Containment Wins", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		if _, err := l.AddConcern(epic.ID, "q1"); err != nil {
			t.Fatalf("add concern: %v", err)
		}
		cs, err := l.Concerns(&epic.ID)
		if err != nil {
			t.Fatalf("concerns: %v", err)
		}
		if _, err := l.ResolveConcern(cs[0].ID, "the database driver uses sqlite everywhere"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		if err := l.AddEvent(epic.ID, core.EventDecision, "use the sqlite driver for the database"); err != nil {
			t.Fatalf("add event: %v", err)
		}
		answer, err := KnownAnswer("sqlite driver", epic, nil, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "use the sqlite driver for the database" {
			t.Fatalf("answer = %q, want the containing candidate", answer)
		}
	})

	t.Run("Two Shared Significant Words Answer", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		c, err := l.AddConcern(epic.ID, "q1")
		if err != nil {
			t.Fatalf("add concern: %v", err)
		}
		if _, err := l.ResolveConcern(c.ID, "the formatter and linter are go fmt and go vet"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		answer, err := KnownAnswer("which formatter and linter do we use?", epic, nil, l)
		if err != nil {
			t.Fatalf("knownAnswer: %v", err)
		}
		if answer != "the formatter and linter are go fmt and go vet" {
			t.Fatalf("answer = %q, want the two-shared-word candidate", answer)
		}
		c2, err := l.AddConcern(epic.ID, "q2")
		if err != nil {
			t.Fatalf("add concern: %v", err)
		}
		if _, err := l.ResolveConcern(c2.ID, "the formatter is go fmt"); err != nil {
			t.Fatalf("resolve concern: %v", err)
		}
		answer, err = KnownAnswer("which formatter do we use?", epic, nil, l)
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
			epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
			c, err := l.AddConcern(epic.ID, "which database driver?")
			if err != nil {
				t.Fatalf("add concern: %v", err)
			}
			if _, err := l.ResolveConcern(c.ID, "use modernc.org/sqlite as the database driver"); err != nil {
				t.Fatalf("resolve concern: %v", err)
			}
			setSession(t, l, child.ID, "ses_1")
			move(t, l, child.ID, core.StateRunning)
			fr := &fakeRunner{texts: []string{"ASK: which database driver should we use?"}}
			res, err := CoordinateOnce(epic, l, resolveFake(fr))
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
			epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
			child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
			c, err := l.AddConcern(epic.ID, "which database driver?")
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
			res, err := CoordinateOnce(epic, l, resolveFake(fr))
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
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"ASK: what is the meaning of life?"}}
		res, err := CoordinateOnce(epic, l, resolveFake(fr))
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
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"STATUS: DONE"}}
		res, err := CoordinateOnce(epic, l, resolveFake(fr))
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
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"STATUS: BLOCKED"}}
		res, err := CoordinateOnce(epic, l, resolveFake(fr))
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

	t.Run("A Child Without A Session Waits", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"READY"}}
		res, err := CoordinateOnce(epic, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Waiting, []string{child.ID}) {
			t.Fatalf("waiting = %v, want [%s]", res.Waiting, child.ID)
		}
	})

	t.Run("A Child With Nothing New Waits", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		setSession(t, l, child.ID, "ses_1")
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"READY"}}
		res, err := CoordinateOnce(epic, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Waiting, []string{child.ID}) {
			t.Fatalf("waiting = %v, want [%s]", res.Waiting, child.ID)
		}
	})

	t.Run("A Child With A Claimed Session Uses The Claim", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "proj", "epic", ledger.AddOptions{Kind: core.WorkEpic})
		child := add(t, l, "proj", "child", ledger.AddOptions{Parent: &epic.ID})
		if _, err := l.SetClaim(child.ID, strPtr("ses_claim")); err != nil {
			t.Fatalf("set claim: %v", err)
		}
		move(t, l, child.ID, core.StateRunning)
		fr := &fakeRunner{texts: []string{"STATUS: DONE"}}
		res, err := CoordinateOnce(epic, l, resolveFake(fr))
		if err != nil {
			t.Fatalf("coordinateOnce: %v", err)
		}
		if !slices.Equal(res.Reviewed, []string{child.ID}) {
			t.Fatalf("reviewed = %v, want [%s]", res.Reviewed, child.ID)
		}
	})
}

func strPtr(s string) *string { return &s }
