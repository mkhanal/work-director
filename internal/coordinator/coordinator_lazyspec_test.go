package coordinator

import (
	"fmt"
	"reflect"
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
	t.Run("The Goal Plan Brief Asks For Changes, Not Checks", func(t *testing.T) {
		goal := core.Work{ID: "e1", Title: "Rewrite the core", Detail: "in Go", Kind: core.WorkEpic}
		p := &project.Project{Name: "wd", Path: "/repo"}
		kit := project.Toolkit{PackageManager: "npm", Lockfile: "package-lock.json", Checks: []string{"npm run lint", "npm run validate"}, Specs: []string{"SPEC.md"}}
		brief, err := GoalPlanBrief(goal, p, kit)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"wd (/repo)", "`npm run lint`", "`npm run validate`", "Rewrite the core", "in Go",
			"## <heading>", "- [ ] <task title>",
			"director runs the checks itself", "no task only runs checks, plans, sets up a worktree or finds out what the goal is",
			"Specifications this repository keeps: SPEC.md", "ASK: <", "NAME: <",
		} {
			if !strings.Contains(brief, want) {
				t.Fatalf("brief missing %q:\n%s", want, brief)
			}
		}
		bare, err := GoalPlanBrief(goal, p, project.Toolkit{})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"none: the repository defines no checks", "Specifications this repository keeps: none; no task adds"} {
			if !strings.Contains(bare, want) {
				t.Fatalf("brief for a bare repository missing %q:\n%s", want, bare)
			}
		}
		if _, err := GoalPlanBrief(goal, &project.Project{Name: "wd", Verify: []string{"pnpm lint"}}, kit); err == nil {
			t.Fatalf("a pnpm check in an npm repository was planned with")
		}
	})

	t.Run("The Supervision Brief Gives A Goal's Supervisor The Whole Picture", func(t *testing.T) {
		pic := Picture{
			Goal:      core.Work{ID: "g1", Title: "Release notes", Detail: "for engineers and customers", Kind: core.WorkGoal},
			Decisions: []string{"keep the js bump script"},
			Tasks: []TaskPicture{
				{Work: core.Work{ID: "t1", Title: "Collector", State: core.StateReview}, Report: "STATUS: DONE\nFILES: collect.ts"},
				{Work: core.Work{ID: "t2", Title: "Impacts", State: core.StateRunning}, Stopped: "wiring it in belongs to the collector task"},
			},
			Concerns: []core.Concern{{ID: 18, Work: "t2", Text: "the collector never calls the detector"}},
			Checks:   []string{"npm run lint"},
			Worktree: "/wt/g1",
		}
		brief := SupervisionBrief(pic)
		for _, want := range []string{
			"g1", "Release notes", "for engineers and customers", "keep the js bump script",
			"t1 [review] Collector", "STATUS: DONE", "t2 [running] Impacts",
			"stopped without a report; it last said: wiring it in belongs to the collector task",
			"18 (on t2): the collector never calls the detector", "`npm run lint`",
			"do not edit, create or delete files", "landing (commits, pull requests, tags, pushes) waits for the person",
			"offer choices whenever the answer is one of a few",
			"ACTIONS:", "SEND <task>", "TASK: <", "START <task>", "RESOLVE <", "ASK: <", "DONE: <", "WAIT",
		} {
			if !strings.Contains(brief, want) {
				t.Fatalf("brief missing %q:\n%s", want, brief)
			}
		}
		if bare := SupervisionBrief(Picture{Goal: pic.Goal}); !strings.Contains(bare, "none: the repository defines no checks") {
			t.Fatalf("brief with no checks does not say so:\n%s", bare)
		}
	})

	t.Run("A Question The Director Files Keeps Its Choices", func(t *testing.T) {
		body := QuestionBody([]Action{
			{Kind: ActAsk, Text: "Tag v1.0.0?", Options: []string{"yes (recommended)", "no"}},
			{Kind: ActAsk, Text: "Who cherry-picks the fixes?"},
		})
		if body != "Tag v1.0.0?\n- yes (recommended)\n- no\nWho cherry-picks the fixes?" {
			t.Fatalf("body = %q", body)
		}
		q := AskedQuestion(core.Event{ID: 7, Body: body})
		if q.ID != "event-7" || len(q.Items) != 2 || q.Items[0].Question != "Tag v1.0.0?" || q.Items[1].Question != "Who cherry-picks the fixes?" ||
			len(q.Items[0].Options) != 2 || q.Items[0].Options[0].Label != "yes (recommended)" || q.Items[0].Options[1].Label != "no" || len(q.Items[1].Options) != 0 {
			t.Fatalf("question = %+v, want two items, the first with its two choices", q)
		}
		events := []core.Event{
			{ID: 1, Work: "g", Kind: core.EventQuestion, Body: "first?"},
			{ID: 2, Work: "g", Kind: core.EventAnswer, Body: "yes"},
			{ID: 3, Work: "g", Kind: core.EventQuestion, Body: "second?"},
			{ID: 4, Work: "t", Kind: core.EventDecision, Body: "on another work"},
			{ID: 5, Work: "t", Kind: core.EventQuestion, Body: "a task's"},
		}
		if e, ok := OpenQuestion(events, "g"); !ok || e.ID != 3 {
			t.Fatalf("open question = %+v, %v; want the second", e, ok)
		}
		yn := AskedQuestion(core.Event{ID: 8, Body: "Should v1.0.0 go on e328cf57?\nWho cherry-picks the fixes?\nIs it A or B?\n- A\n- B"})
		if got := yn.Items[0].Options; len(got) != 2 || got[0].Label != "Yes" || got[1].Label != "No" {
			t.Fatalf("a yes-or-no question offers %+v, want Yes and No", got)
		}
		if len(yn.Items[1].Options) != 0 || len(yn.Items[2].Options) != 2 || yn.Items[2].Options[0].Label != "A" {
			t.Fatalf("questions = %+v, want an open question left open and offered choices kept", yn.Items)
		}
		rec := AskedQuestion(core.Event{ID: 9, Body: "Confirm `v1.0.0` goes on e328cf57. I recommend yes: it is the last deployment."})
		if got := rec.Items[0].Options; len(got) != 2 || got[0].Label != "Yes (recommended)" || got[1].Label != "No" {
			t.Fatalf("a confirmation recommending yes offers %+v", got)
		}
		proposed := ReportQuestion(core.Event{ID: 10, Body: "NEEDS-INPUT\nSTATUS: NEEDS-INPUT Pushing needs a force-with-lease; I propose allowing it."})
		if got := proposed.Items[0].Options; len(got) != 2 || got[0].Label != "Yes (recommended)" {
			t.Fatalf("a proposal offers %+v, want Yes recommended and No", got)
		}
		if _, ok := OpenQuestion(append(events, core.Event{ID: 6, Work: "g", Kind: core.EventDecision, Body: "settled"}), "g"); ok {
			t.Fatal("a question decided on since is still open")
		}
	})

	t.Run("A Supervision Reply Parses Into Actions", func(t *testing.T) {
		reply := []string{
			"The collector never calls the detector. ACTIONS: are below.",
			"ACTIONS:\n- SEND t1: call detectOperationalImpacts and print impacts\nTASK: weekly running note",
			"START t9\nDROP t8: the skill already does it\n`RESOLVE #18: the collector task wires it in`\nASK: tag v1.0.0 at 7720d09? I recommend yes\n- yes (recommended)\n* no, wait\nDONE: all parts fit\nWAIT\n\nthen I will look again",
		}
		actions, refused := ParseSupervision(reply)
		want := []Action{
			{Kind: ActSend, Work: "t1", Text: "call detectOperationalImpacts and print impacts"},
			{Kind: ActTask, Text: "weekly running note"},
			{Kind: ActStart, Work: "t9"},
			{Kind: ActDrop, Work: "t8", Text: "the skill already does it"},
			{Kind: ActResolve, Concern: 18, Text: "the collector task wires it in"},
			{Kind: ActAsk, Text: "tag v1.0.0 at 7720d09? I recommend yes", Options: []string{"yes (recommended)", "no, wait"}},
			{Kind: ActDone, Text: "all parts fit"},
			{Kind: ActWait},
		}
		if !reflect.DeepEqual(actions, want) {
			t.Fatalf("actions = %+v\nwant %+v", actions, want)
		}
		if !slices.Equal(refused, []string{"then I will look again"}) {
			t.Fatalf("refused = %q, want the line that is no action", refused)
		}
		none, unread := ParseSupervision([]string{"I looked; nothing to do."})
		if len(none) != 0 || len(unread) != 0 {
			t.Fatalf("a reply with no ACTIONS line = %+v, %q; want nothing", none, unread)
		}
	})

	t.Run("A Plan Reply Names Its Goal", func(t *testing.T) {
		if name, ok := PlanName([]string{"Read the repo.", "**NAME:** \"Login page fixes\"\n## Core\n- [ ] fix it"}); !ok || name != "Login page fixes" {
			t.Fatalf("name = %q, %v", name, ok)
		}
		long := "NAME: " + strings.Repeat("word ", 30)
		if name, _ := PlanName([]string{long}); len([]rune(name)) > 72 || strings.HasSuffix(name, " ") || !strings.HasPrefix(name, "word") {
			t.Fatalf("a long name = %q, want it cut at a word to 72", name)
		}
		if name, ok := PlanName([]string{"## Core\n- [ ] port the ledger"}); ok {
			t.Fatalf("a plan with no NAME line named its goal %q", name)
		}
	})

	t.Run("A Plan Reply Asking A Question Is A Question", func(t *testing.T) {
		q, ok := PlanQuestion([]string{"Looking at the repo.", "ASK: What should change in the new worktree?"})
		if !ok || q != "What should change in the new worktree?" {
			t.Fatalf("question = %q, %v", q, ok)
		}
		if q, ok := PlanQuestion([]string{"## Core\n- [ ] port the ledger"}); ok {
			t.Fatalf("a plan read as a question: %q", q)
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
			evs, err := l.Events(child.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range evs {
				if e.Kind == core.EventAnswer && (e.Decision == nil || e.Decision.Question != "which database driver should we use?" || e.Decision.Source != core.AnsweredByDirector) {
					t.Fatalf("answer = %+v, want it with its question, answered by the director", e.Decision)
				}
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
