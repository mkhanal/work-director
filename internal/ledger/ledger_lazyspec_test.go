package ledger

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"wd/internal/core"
)

func newTestLedger(t *testing.T) *Ledger {
	t.Helper()
	l, err := New(":memory:")
	if err != nil {
		t.Fatalf("open memory ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func strPtr(s string) *string { return &s }

func goalTypePtr(gt core.GoalType) *core.GoalType { return &gt }

func wantErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q, got nil", msg)
	}
	if err.Error() != msg {
		t.Fatalf("expected error %q, got %q", msg, err.Error())
	}
}

func wantNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func add(t *testing.T, l *Ledger, project, title string, opts AddOptions) core.Work {
	t.Helper()
	w, err := l.Add(project, title, opts)
	wantNoErr(t, err)
	return w
}

func move(t *testing.T, l *Ledger, id string, to core.State) core.Work {
	t.Helper()
	w, err := l.Transition(id, to)
	wantNoErr(t, err)
	return w
}

func softDone(t *testing.T, l *Ledger, id string, codeChanged bool) core.Work {
	t.Helper()
	w, err := l.SoftDone(id, codeChanged)
	wantNoErr(t, err)
	return w
}

func allEvents(t *testing.T, l *Ledger, work string, kind *core.EventKind) []core.Event {
	t.Helper()
	evs, err := l.Events(work, kind)
	wantNoErr(t, err)
	return evs
}

func allTasks(t *testing.T, l *Ledger, epicID string) []core.Work {
	t.Helper()
	ts, err := l.Tasks(epicID)
	wantNoErr(t, err)
	return ts
}

func allOpenConcerns(t *testing.T, l *Ledger, epicID string) []core.Concern {
	t.Helper()
	cs, err := l.OpenConcerns(epicID)
	wantNoErr(t, err)
	return cs
}

func allWorktrees(t *testing.T, l *Ledger, work string) []core.Worktree {
	t.Helper()
	ws, err := l.Worktrees(work)
	wantNoErr(t, err)
	return ws
}

func allConflicts(t *testing.T, l *Ledger, epicID string) []core.Conflict {
	t.Helper()
	cs, err := l.Conflicts(epicID)
	wantNoErr(t, err)
	return cs
}

func allDistill(t *testing.T, l *Ledger) []core.Candidate {
	t.Helper()
	cs, err := l.Distill()
	wantNoErr(t, err)
	return cs
}

func TestLedger(t *testing.T) {
	t.Run("New Work Starts Queued", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "Do thing", AddOptions{})
		if w.State != core.StateQueued {
			t.Fatalf("state = %q, want queued", w.State)
		}
		stateKind := core.EventState
		bodies := []string{}
		for _, e := range allEvents(t, l, w.ID, &stateKind) {
			bodies = append(bodies, e.Body)
		}
		if !slices.Equal(bodies, []string{"queued"}) {
			t.Fatalf("state events = %v, want [queued]", bodies)
		}
	})

	t.Run("Only Listed Transitions Are Allowed", func(t *testing.T) {
		t.Run("happy path walks every state", func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			for _, s := range []core.State{core.StateBriefed, core.StateRunning, core.StateReview} {
				move(t, l, w.ID, s)
			}
			l.AddEvent(w.ID, core.EventReport, "DONE")
			l.AddEvent(w.ID, core.EventVerify, "pass")
			if got := softDone(t, l, w.ID, false); got.State != core.StateSoftDone {
				t.Fatalf("state = %q, want soft-done", got.State)
			}
			if got := move(t, l, w.ID, core.StateDone); got.State != core.StateDone {
				t.Fatalf("state = %q, want done", got.State)
			}
		})
		t.Run("running to done fails naming both states", func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			move(t, l, w.ID, core.StateRunning)
			var it core.IllegalTransition
			_, err := l.Transition(w.ID, core.StateDone)
			if !errors.As(err, &it) {
				t.Fatalf("error = %v, want IllegalTransition", err)
			}
			if it.From != core.StateRunning || it.To != core.StateDone {
				t.Fatalf("IllegalTransition = %v, want running to done", it)
			}
			wantErr(t, err, "illegal transition running → done")
		})
		t.Run("queued briefed and blocked go straight to done", func(t *testing.T) {
			l := newTestLedger(t)
			for _, path := range [][]core.State{
				{},
				{core.StateBriefed},
				{core.StateBlocked},
			} {
				w := add(t, l, "p", "t", AddOptions{})
				for _, s := range path {
					move(t, l, w.ID, s)
				}
				if got := move(t, l, w.ID, core.StateDone); got.State != core.StateDone {
					t.Fatalf("after %v: state = %q, want done", path, got.State)
				}
			}
		})
		t.Run("queued to running is the attached-outside-conversation path", func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			if got := move(t, l, w.ID, core.StateRunning); got.State != core.StateRunning {
				t.Fatalf("state = %q, want running", got.State)
			}
		})
		t.Run("blocked is reachable from open states and returns", func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			move(t, l, w.ID, core.StateRunning)
			if got := move(t, l, w.ID, core.StateBlocked); got.State != core.StateBlocked {
				t.Fatalf("state = %q, want blocked", got.State)
			}
			if got := move(t, l, w.ID, core.StateRunning); got.State != core.StateRunning {
				t.Fatalf("state = %q, want running", got.State)
			}
			move(t, l, w.ID, core.StateReview)
			l.AddEvent(w.ID, core.EventReport, "DONE")
			l.AddEvent(w.ID, core.EventVerify, "pass")
			softDone(t, l, w.ID, false)
			if got := move(t, l, w.ID, core.StateBlocked); got.State != core.StateBlocked {
				t.Fatalf("soft-done: state = %q, want blocked", got.State)
			}
		})
		t.Run("done and dropped are terminal", func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			move(t, l, w.ID, core.StateDropped)
			_, err := l.Transition(w.ID, core.StateQueued)
			wantErr(t, err, "illegal transition dropped → queued")
		})
	})

	t.Run("Any Event Counts As Activity", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		if _, err := l.db.Exec(`UPDATE work SET updated = '2026-01-01T00:00:00.000Z' WHERE id = ?`, w.ID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		_, err := l.AddEvent(w.ID, core.EventNote, "still alive")
		wantNoErr(t, err)
		ev, err := l.Events(w.ID, nil)
		wantNoErr(t, err)
		got, err := l.Get(w.ID)
		wantNoErr(t, err)
		if last := ev[len(ev)-1]; got.Updated != last.At {
			t.Fatalf("updated = %q, want the event's time %q", got.Updated, last.At)
		}
	})

	t.Run("Soft Done Requires A Done Report And A Passing Verify", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		for _, s := range []core.State{core.StateBriefed, core.StateRunning, core.StateReview} {
			move(t, l, w.ID, s)
		}
		var nr core.NotReady
		if _, err := l.SoftDone(w.ID, false); !errors.As(err, &nr) {
			t.Fatalf("error = %v, want NotReady", err)
		} else if !slices.Equal(nr.Missing, []string{"DONE report", "passing verify"}) {
			t.Fatalf("missing = %v, want [DONE report passing verify]", nr.Missing)
		}
		l.AddEvent(w.ID, core.EventReport, "BLOCKED\nx")
		l.AddEvent(w.ID, core.EventVerify, "fail\nx")
		if _, err := l.SoftDone(w.ID, false); !errors.As(err, &nr) {
			t.Fatalf("error = %v, want NotReady", err)
		}
		l.AddEvent(w.ID, core.EventReport, "DONE\nok")
		l.AddEvent(w.ID, core.EventVerify, "pass\nok")
		if got := softDone(t, l, w.ID, false); got.State != core.StateSoftDone {
			t.Fatalf("state = %q, want soft-done", got.State)
		}
	})

	t.Run("Soft Done Is Reached Only Through Its Gate", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		for _, s := range []core.State{core.StateBriefed, core.StateRunning, core.StateReview} {
			move(t, l, w.ID, s)
		}
		before := allEvents(t, l, w.ID, nil)
		if _, err := l.Transition(w.ID, core.StateSoftDone); err == nil {
			t.Fatal("Transition moved work to soft-done without its gate")
		}
		got, err := l.Get(w.ID)
		wantNoErr(t, err)
		if got.State != core.StateReview || len(allEvents(t, l, w.ID, nil)) != len(before) {
			t.Fatalf("state = %q with %d events, want review unchanged", got.State, len(allEvents(t, l, w.ID, nil)))
		}
		l.AddEvent(w.ID, core.EventReport, "DONE")
		l.AddEvent(w.ID, core.EventVerify, "pass")
		if got := softDone(t, l, w.ID, false); got.State != core.StateSoftDone {
			t.Fatalf("state = %q, want soft-done", got.State)
		}
	})

	t.Run("Code Changes Need A Pull Request Before Soft Done", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		for _, s := range []core.State{core.StateBriefed, core.StateRunning, core.StateReview} {
			move(t, l, w.ID, s)
		}
		l.AddEvent(w.ID, core.EventReport, "DONE")
		l.AddEvent(w.ID, core.EventVerify, "pass")
		var nr core.NotReady
		if _, err := l.SoftDone(w.ID, true); !errors.As(err, &nr) {
			t.Fatalf("error = %v, want NotReady", err)
		} else if !slices.Equal(nr.Missing, []string{"pull request"}) {
			t.Fatalf("missing = %v, want [pull request]", nr.Missing)
		}
		l.AddEvent(w.ID, core.EventPr, "https://github.com/x/y/pull/1")
		if got := softDone(t, l, w.ID, true); got.State != core.StateSoftDone {
			t.Fatalf("state = %q, want soft-done", got.State)
		}
	})

	t.Run("Feedback Seen Twice Becomes A Distill Candidate", func(t *testing.T) {
		l := newTestLedger(t)
		l.AddFeedback("a1", FeedbackOptions{Card: strPtr("parse-at-boundary")})
		l.AddFeedback("a2", FeedbackOptions{Card: strPtr("parse-at-boundary")})
		l.AddFeedback("b1", FeedbackOptions{Project: strPtr("send-frugal")})
		l.AddFeedback("b2", FeedbackOptions{Project: strPtr("send-frugal")})
		l.AddFeedback("only once", FeedbackOptions{Card: strPtr("fail-loud")})
		l.AddFeedback("g1", FeedbackOptions{})
		l.AddFeedback("g2", FeedbackOptions{})
		got := allDistill(t, l)
		want := []core.Candidate{
			{Key: "card:parse-at-boundary", Count: 2, Texts: []string{"a1", "a2"}},
			{Key: "project:send-frugal", Count: 2, Texts: []string{"b1", "b2"}},
			{Key: "global", Count: 2, Texts: []string{"g1", "g2"}},
		}
		if len(got) != len(want) {
			t.Fatalf("distill = %v, want %v", got, want)
		}
		for i := range want {
			if got[i].Key != want[i].Key || got[i].Count != want[i].Count || !slices.Equal(got[i].Texts, want[i].Texts) {
				t.Fatalf("distill[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("A Roadmap Holds Items And A Goal Holds Tasks", func(t *testing.T) {
		l := newTestLedger(t)
		roadmap := add(t, l, "p", "R", AddOptions{Kind: core.WorkRoadmap})
		item := add(t, l, "p", "I", AddOptions{Kind: core.WorkItem, Parent: &roadmap.ID})
		if is := allTasks(t, l, roadmap.ID); len(is) != 1 || is[0].ID != item.ID {
			t.Fatalf("items = %v, want [%s]", is, item.ID)
		}
		goal := add(t, l, "p", "G", AddOptions{Kind: core.WorkGoal})
		task := add(t, l, "p", "T", AddOptions{Parent: &goal.ID})
		if ts := allTasks(t, l, goal.ID); len(ts) != 1 || ts[0].ID != task.ID {
			t.Fatalf("tasks = %v, want [%s]", ts, task.ID)
		}
		// The two levels do not cross, and the refusal says which.
		_, err := l.Add("p", "T2", AddOptions{Parent: &roadmap.ID})
		wantErr(t, err, "under "+roadmap.ID+": a roadmap holds items, not task")
		_, err = l.Add("p", "I2", AddOptions{Kind: core.WorkItem, Parent: &goal.ID})
		wantErr(t, err, "under "+goal.ID+": a goal holds tasks, not item")
		// A goal or a roadmap cannot sit under anything.
		_, err = l.Add("p", "G2", AddOptions{Kind: core.WorkGoal, Parent: &roadmap.ID})
		wantErr(t, err, "a goal or roadmap cannot sit under another work item")
		_, err = l.Add("p", "R2", AddOptions{Kind: core.WorkRoadmap, Parent: &roadmap.ID})
		wantErr(t, err, "a goal or roadmap cannot sit under another work item")
		// An item with no roadmap has nowhere to live.
		_, err = l.Add("p", "I3", AddOptions{Kind: core.WorkItem})
		wantErr(t, err, "a roadmap item belongs to a roadmap")
		// Nothing but the two levels holds children.
		plain := add(t, l, "p", "P", AddOptions{})
		_, err = l.Add("p", "T3", AddOptions{Parent: &plain.ID})
		wantErr(t, err, "under "+plain.ID+": work of kind task holds no children")
		_, err = l.Add("p", "T4", AddOptions{Parent: strPtr("nope")})
		wantErr(t, err, "no work nope; wd status for known work items")
		// A goal type is not spellable on anything but a goal.
		_, err = l.Add("p", "T5", AddOptions{GoalType: goalTypePtr(core.GoalBuild)})
		wantErr(t, err, "a goal type applies to a goal, not task")
	})

	t.Run("A Roadmap Item Becomes A Goal Without Changing Its Id", func(t *testing.T) {
		l := newTestLedger(t)
		roadmap := add(t, l, "p", "R", AddOptions{Kind: core.WorkRoadmap})
		item := add(t, l, "p", "I", AddOptions{Kind: core.WorkItem, Parent: &roadmap.ID})
		// Work filed against the item before it was committed stays with it.
		_, err := l.AddEvent(item.ID, core.EventDecision, "asked about the shape")
		wantNoErr(t, err)
		got, err := l.Promote(item.ID, goalTypePtr(core.GoalBuild))
		wantNoErr(t, err)
		if got.ID != item.ID {
			t.Fatalf("promoted id = %s, want the item's own id %s", got.ID, item.ID)
		}
		if got.Kind != core.WorkGoal {
			t.Fatalf("kind = %s, want goal", got.Kind)
		}
		if got.GoalType == nil || *got.GoalType != core.GoalBuild {
			t.Fatalf("goal_type = %v, want build", got.GoalType)
		}
		// The translation is on the record.
		evs, err := l.Events(item.ID, nil)
		wantNoErr(t, err)
		found := false
		for _, e := range evs {
			if e.Body == "promoted to goal" {
				found = true
			}
		}
		if !found {
			t.Fatalf("events = %v, want the translation recorded", evs)
		}
		// The decision filed before the promotion is still there.
		decisionKind := core.EventDecision
		evs, err = l.Events(item.ID, &decisionKind)
		wantNoErr(t, err)
		if len(evs) != 1 || evs[0].Body != "asked about the shape" {
			t.Fatalf("decisions = %v, want the one filed before the promotion", evs)
		}
		// Promoting what is not an item is refused.
		_, err = l.Promote(item.ID, nil)
		wantErr(t, err, "work "+item.ID+" is a goal, not a roadmap item")
	})

	t.Run("A Goal Type Is Set Deliberately Or Not At All", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "p", "G", AddOptions{Kind: core.WorkGoal})
		if goal.GoalType != nil {
			t.Fatalf("goal_type = %v, want none: a type is never defaulted", *goal.GoalType)
		}
		got, err := l.SetGoalType(goal.ID, core.GoalQuery)
		wantNoErr(t, err)
		if got.GoalType == nil || *got.GoalType != core.GoalQuery {
			t.Fatalf("goal_type = %v, want query", got.GoalType)
		}
		// The claim is on the record as a decision.
		decisionKind := core.EventDecision
		evs, err := l.Events(goal.ID, &decisionKind)
		wantNoErr(t, err)
		if len(evs) != 1 || evs[0].Body != "classified query" {
			t.Fatalf("decisions = %v, want the classification recorded", evs)
		}
		// A type outside the set is not spellable.
		_, err = l.SetGoalType(goal.ID, "invented")
		wantErr(t, err, `unknown goal type "invented"`)
		// A type on work that is not a goal is refused.
		task := add(t, l, "p", "T", AddOptions{Parent: &goal.ID})
		_, err = l.SetGoalType(task.ID, core.GoalBuild)
		wantErr(t, err, "work "+task.ID+" is a task, not a goal")
	})

	t.Run("Work Can End Abandoned And Says Why", func(t *testing.T) {
		l := newTestLedger(t)
		goal := add(t, l, "p", "G", AddOptions{Kind: core.WorkGoal})
		// Abandon is only allowed from states where work was attempted.
		move(t, l, goal.ID, core.StateRunning)
		got, err := l.Abandon(goal.ID, core.AbandonNoPR, "no branch was ever pushed")
		wantNoErr(t, err)
		if got.State != core.StateAbandoned {
			t.Fatalf("state = %s, want abandoned", got.State)
		}
		abandonKind := core.EventAbandon
		evs, err := l.Events(goal.ID, &abandonKind)
		wantNoErr(t, err)
		if len(evs) != 1 || evs[0].Body != "no-pr: no branch was ever pushed" {
			t.Fatalf("abandon events = %v, want the reason and the detail", evs)
		}
		// A reason nobody can verify is not spellable.
		other := add(t, l, "p", "G2", AddOptions{Kind: core.WorkGoal})
		move(t, l, other.ID, core.StateRunning)
		_, err = l.Abandon(other.ID, "ran-out-of-enthusiasm", "")
		wantErr(t, err, `unknown abandon reason "ran-out-of-enthusiasm"`)
		// Abandoned is terminal: it does not transition back out.
		_, err = l.Transition(goal.ID, core.StateRunning)
		wantErr(t, err, "illegal transition abandoned → running")
		// The other machine-checkable reason.
		third := add(t, l, "p", "G3", AddOptions{Kind: core.WorkGoal})
		move(t, l, third.ID, core.StateRunning)
		got, err = l.Abandon(third.ID, core.AbandonUnmerged, "PR 12 never merged")
		wantNoErr(t, err)
		if got.State != core.StateAbandoned {
			t.Fatalf("state = %s, want abandoned", got.State)
		}
	})

	t.Run("Concerns Resolve With A Decision", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "p", "E", AddOptions{Kind: core.WorkEpic})
		task := add(t, l, "p", "T", AddOptions{Parent: &epic.ID})
		c, err := l.AddConcern(task.ID, "overlap risk")
		wantNoErr(t, err)
		if c.Resolved != 0 || c.ResolvedAt != nil {
			t.Fatalf("concern = %+v, want unresolved", c)
		}
		if cs := allOpenConcerns(t, l, epic.ID); len(cs) != 1 || cs[0].ID != c.ID {
			t.Fatalf("open concerns = %v, want [%d]", cs, c.ID)
		}
		decision := "serialize both"
		r, err := l.ResolveConcern(c.ID, decision)
		wantNoErr(t, err)
		if r.Resolved != 1 || r.Decision == nil || *r.Decision != decision || r.ResolvedAt == nil {
			t.Fatalf("concern = %+v, want resolved with decision", r)
		}
		noteKind := core.EventNote
		notes := allEvents(t, l, task.ID, &noteKind)
		if len(notes) != 1 || notes[0].Body != "concern "+strconv.Itoa(c.ID)+" resolved: "+decision {
			t.Fatalf("notes = %v, want concern resolved note", notes)
		}
		if cs := allOpenConcerns(t, l, epic.ID); len(cs) != 0 {
			t.Fatalf("open concerns = %v, want none", cs)
		}
		if _, err := l.ResolveConcern(c.ID, "undo it"); err == nil {
			t.Fatal("resolving a resolved concern succeeded")
		}
		cs, err := l.Concerns(&task.ID)
		wantNoErr(t, err)
		if len(cs) != 1 || cs[0].Decision == nil || *cs[0].Decision != decision {
			t.Fatalf("concerns = %+v, want the first decision kept", cs)
		}
	})

	t.Run("Events Concerns And Worktrees Belong To Existing Work", func(t *testing.T) {
		l := newTestLedger(t)
		const noWork = "no work nope; wd status for known work items"
		_, err := l.AddEvent("nope", core.EventNote, "x")
		wantErr(t, err, noWork)
		_, err = l.AddConcern("nope", "x")
		wantErr(t, err, noWork)
		_, err = l.AddWorktree("nope", WorktreeInfo{Path: "/tmp/wt", Kind: core.WorktreePrivate, Origin: core.OriginAttached})
		wantErr(t, err, noWork)
		var rows int
		wantNoErr(t, l.db.QueryRow(`SELECT (SELECT COUNT(*) FROM event) + (SELECT COUNT(*) FROM concern) + (SELECT COUNT(*) FROM worktree)`).Scan(&rows))
		if rows != 0 {
			t.Fatalf("%d rows stored for unknown work, want 0", rows)
		}
	})

	t.Run("Evidence From A Session Knows The Work It Came From", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		proj, card := "p", "no-branch-chains"
		// Evidence from a session points at the work it came out of, so a card
		// promoted on it records its decision there and a reader can audit the
		// promotion against the thing that showed the pattern.
		f, err := l.AddFeedback("wd send raced a busy task in a shared worktree",
			FeedbackOptions{Project: &proj, Card: &card, Source: core.FeedbackAttached, Work: &w.ID})
		wantNoErr(t, err)
		if f.Work == nil || *f.Work != w.ID {
			t.Errorf("feedback work = %v, want %s", f.Work, w.ID)
		}
		all, err := l.Feedback()
		wantNoErr(t, err)
		if len(all) != 1 || all[0].Work == nil || *all[0].Work != w.ID {
			t.Errorf("feedback read back as %+v, want the work kept", all)
		}
		after, err := l.FeedbackAfter(0)
		wantNoErr(t, err)
		if len(after) != 1 || after[0].Work == nil || *after[0].Work != w.ID {
			t.Errorf("feedback after 0 = %+v, want the work kept: a review reads this", after)
		}
		// A note somebody typed by hand has no work behind it, and says so
		// rather than guessing at one.
		n, err := l.AddFeedback("a preference stated once", FeedbackOptions{Project: &proj})
		wantNoErr(t, err)
		if n.Work != nil {
			t.Errorf("a hand-typed note recorded work %v, want none", *n.Work)
		}
		// The reference is a relation like every other, so evidence pointed at
		// work that is not there fails rather than becoming an orphan nothing
		// can be audited against.
		_, err = l.AddFeedback("evidence for nothing", FeedbackOptions{Work: strPtr("nope")})
		wantErr(t, err, noWork("nope").Error())
		var rows int
		wantNoErr(t, l.db.QueryRow(`SELECT COUNT(*) FROM feedback WHERE work = ?`, "nope").Scan(&rows))
		if rows != 0 {
			t.Fatalf("%d rows stored for unknown work, want 0", rows)
		}
	})

	t.Run("Worktrees Track Path Branch And State", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		branch := "feature/x"
		wt, err := l.AddWorktree(w.ID, WorktreeInfo{Path: "/tmp/wt", Branch: &branch, Kind: core.WorktreePrivate, Origin: core.OriginAttached})
		wantNoErr(t, err)
		if wt.State != core.WorktreeActive || wt.Kind != core.WorktreePrivate || wt.Branch == nil || *wt.Branch != branch {
			t.Fatalf("worktree = %+v, want active private with branch", wt)
		}
		if got, err := l.SetWorktreeState(wt.ID, core.WorktreeMerged); err != nil || got.State != core.WorktreeMerged {
			t.Fatalf("state = %v, %v, want merged", got.State, err)
		}
		if ws := allWorktrees(t, l, w.ID); len(ws) != 1 || ws[0].ID != wt.ID {
			t.Fatalf("worktrees = %v, want [%d]", ws, wt.ID)
		}
	})

	t.Run("A Goal Has At Most One Active Shared Worktree", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "p", "epic", AddOptions{Kind: core.WorkEpic})
		other := add(t, l, "p", "other", AddOptions{Kind: core.WorkEpic})
		first, err := l.AddWorktree(epic.ID, WorktreeInfo{Path: "/wt/first", Kind: core.WorktreeShared, Origin: core.OriginDirector})
		wantNoErr(t, err)
		if _, err := l.AddWorktree(epic.ID, WorktreeInfo{Path: "/wt/second", Kind: core.WorktreeShared, Origin: core.OriginDirector}); err == nil {
			t.Fatal("a second active shared worktree was stored")
		}
		if _, err := l.AddWorktree(epic.ID, WorktreeInfo{Path: "/wt/private", Kind: core.WorktreePrivate, Origin: core.OriginAttached}); err != nil {
			t.Fatalf("private worktree beside the shared one: %v", err)
		}
		if _, err := l.AddWorktree(other.ID, WorktreeInfo{Path: "/wt/other", Kind: core.WorktreeShared, Origin: core.OriginDirector}); err != nil {
			t.Fatalf("another epic's shared worktree: %v", err)
		}
		_, err = l.SetWorktreeState(first.ID, core.WorktreeMerged)
		wantNoErr(t, err)
		second, err := l.AddWorktree(epic.ID, WorktreeInfo{Path: "/wt/second", Kind: core.WorktreeShared, Origin: core.OriginDirector})
		if err != nil {
			t.Fatalf("shared worktree after the first merged: %v", err)
		}
		if _, err := l.SetWorktreeState(first.ID, core.WorktreeActive); err == nil {
			t.Fatal("a merged shared worktree became active beside another")
		}
		if ws := allWorktrees(t, l, epic.ID); len(ws) != 3 || ws[2].ID != second.ID {
			t.Fatalf("worktrees = %v, want first, private and second", ws)
		}
	})

	t.Run("Conflicts Surface When Claimed Tasks Overlap", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "p", "E", AddOptions{Kind: core.WorkEpic})
		a := add(t, l, "p", "A", AddOptions{Parent: &epic.ID})
		b := add(t, l, "p", "B", AddOptions{Parent: &epic.ID})
		l.SetClaim(a.ID, strPtr("ses_a"))
		l.SetClaim(b.ID, strPtr("ses_b"))
		l.SetImpact(a.ID, []string{"a/b"})
		l.SetImpact(b.ID, []string{"a/b/c"})
		got := allConflicts(t, l, epic.ID)
		if len(got) != 1 || got[0].A != a.ID || got[0].B != b.ID || !slices.Equal(got[0].Paths, []string{"a/b"}) {
			t.Fatalf("conflicts = %v, want one overlap on a/b", got)
		}
		for _, s := range []core.State{core.StateBriefed, core.StateRunning, core.StateReview} {
			move(t, l, b.ID, s)
		}
		l.AddEvent(b.ID, core.EventReport, "DONE")
		softDone(t, l, b.ID, false)
		move(t, l, b.ID, core.StateDone)
		if got := allConflicts(t, l, epic.ID); len(got) != 0 {
			t.Fatalf("conflicts = %v, want none once a task is done", got)
		}
	})

	t.Run("Soft Done Requires Goal Tasks Done", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "p", "E", AddOptions{Kind: core.WorkEpic})
		task := add(t, l, "p", "T", AddOptions{Parent: &epic.ID})
		move(t, l, epic.ID, core.StateBriefed)
		move(t, l, epic.ID, core.StateRunning)
		move(t, l, epic.ID, core.StateReview)
		l.AddEvent(epic.ID, core.EventReport, "DONE")
		l.AddEvent(epic.ID, core.EventVerify, "pass")
		l.AddEvent(epic.ID, core.EventPr, "https://github.com/x/y/pull/1")
		var nr core.NotReady
		if _, err := l.SoftDone(epic.ID, true); !errors.As(err, &nr) {
			t.Fatalf("error = %v, want NotReady", err)
		} else if !slices.Equal(nr.Missing, []string{"1 task(s) not done"}) {
			t.Fatalf("missing = %v, want [1 task(s) not done]", nr.Missing)
		}
		move(t, l, task.ID, core.StateBriefed)
		move(t, l, task.ID, core.StateRunning)
		move(t, l, task.ID, core.StateReview)
		l.AddEvent(task.ID, core.EventReport, "DONE")
		softDone(t, l, task.ID, false)
		move(t, l, task.ID, core.StateDone)
		if got := softDone(t, l, epic.ID, true); got.State != core.StateSoftDone {
			t.Fatalf("state = %q, want soft-done", got.State)
		}
	})

	t.Run("The Ledger Persists Across Reopen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ledger.db")
		l, err := New(path)
		wantNoErr(t, err)
		w := add(t, l, "p", "t", AddOptions{})
		wantNoErr(t, l.Close())
		l, err = New(path)
		wantNoErr(t, err)
		defer l.Close()
		got, err := l.Get(w.ID)
		wantNoErr(t, err)
		if got.Title != "t" {
			t.Fatalf("title = %q, want t", got.Title)
		}
	})

	// The schema as the first TypeScript wd wrote it: a 12-column work table,
	// event and feedback only. Every ledger of this generation must stay readable.
	const oldSchema = `
CREATE TABLE work (id TEXT PRIMARY KEY, project TEXT NOT NULL, title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL, state TEXT NOT NULL, runner TEXT, session TEXT, ref TEXT, cwd TEXT, created TEXT NOT NULL, updated TEXT NOT NULL);
CREATE TABLE event (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL);
CREATE TABLE feedback (id INTEGER PRIMARY KEY, text TEXT NOT NULL, project TEXT, card TEXT, source TEXT NOT NULL, at TEXT NOT NULL);`

	// writeOldLedger creates a pre-epic ledger with the rows the TypeScript wd
	// would have stored, and returns the path.
	writeOldLedger := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "ledger.db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("open old ledger: %v", err)
		}
		defer db.Close()
		if _, err := db.Exec(oldSchema); err != nil {
			t.Fatalf("write old schema: %v", err)
		}
		t0 := "2026-09-01T10:00:00.000Z"
		if _, err := db.Exec(`INSERT INTO work (id, project, title, detail, kind, state, runner, session, ref, cwd, created, updated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"oldaaaa1", "proj", "First task", "d1", "task", "running", "claude", "ses_old1", "ref1", "/tmp/wd", t0, t0); err != nil {
			t.Fatalf("seed work 1: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO work (id, project, title, detail, kind, state, created, updated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"oldbbbb2", "proj", "Second task", "", "evolution", "done", t0, t0); err != nil {
			t.Fatalf("seed work 2: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO event (work, kind, body, at) VALUES (?, ?, ?, ?)`,
			"oldaaaa1", "state", "running", t0); err != nil {
			t.Fatalf("seed event: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO feedback (text, project, card, source, at) VALUES (?, ?, ?, ?, ?)`,
			"be succinct", "proj", nil, "director", t0); err != nil {
			t.Fatalf("seed feedback: %v", err)
		}
		return path
	}

	// workColumns returns name -> (notNull, pk) per column of the work table.
	workColumns := func(t *testing.T, db *sql.DB) map[string][2]int {
		t.Helper()
		rows, err := db.Query("PRAGMA table_info(work)")
		if err != nil {
			t.Fatalf("table_info: %v", err)
		}
		defer rows.Close()
		out := map[string][2]int{}
		for rows.Next() {
			var cid, notNull, pk int
			var name, typ string
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
				t.Fatalf("scan table_info: %v", err)
			}
			out[name] = [2]int{notNull, pk}
		}
		return out
	}

	// dump every row of every table, deterministically ordered.
	dump := func(t *testing.T, db *sql.DB) string {
		t.Helper()
		var b strings.Builder
		for _, tbl := range []string{"work", "event", "feedback", "concern", "worktree"} {
			rows, err := db.Query(fmt.Sprintf("SELECT * FROM %s ORDER BY 1", tbl))
			if err != nil {
				t.Fatalf("dump %s: %v", tbl, err)
			}
			cols, err := rows.Columns()
			if err != nil {
				t.Fatalf("columns %s: %v", tbl, err)
			}
			b.WriteString(tbl + " " + strings.Join(cols, ",") + "\n")
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatalf("scan %s: %v", tbl, err)
				}
				parts := make([]string, len(cols))
				for i, v := range vals {
					parts[i] = fmt.Sprintf("%v", v)
				}
				b.WriteString(strings.Join(parts, ",") + "\n")
			}
			rows.Close()
		}
		return b.String()
	}

	openDirect := func(t *testing.T, path string) *sql.DB {
		t.Helper()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("reopen direct: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}

	t.Run("Old Ledgers Stay Readable", func(t *testing.T) {
		path := writeOldLedger(t)
		before := workColumns(t, openDirect(t, path))
		if len(before) != 12 {
			t.Fatalf("old schema has %d work columns, want 12", len(before))
		}

		l, err := New(path)
		wantNoErr(t, err)

		// Every stored row reads back with its values untouched.
		w1, err := l.Get("oldaaaa1")
		wantNoErr(t, err)
		if w1.Project != "proj" || w1.Title != "First task" || w1.Detail != "d1" ||
			w1.Kind != core.WorkTask || w1.State != core.StateRunning ||
			w1.Runner == nil || *w1.Runner != "claude" || w1.Session == nil || *w1.Session != "ses_old1" ||
			w1.Ref == nil || *w1.Ref != "ref1" || w1.Cwd == nil || *w1.Cwd != "/tmp/wd" ||
			w1.Created != "2026-09-01T10:00:00.000Z" || w1.Updated != "2026-09-01T10:00:00.000Z" {
			t.Fatalf("work 1 = %+v, want the seeded row", w1)
		}
		w2, err := l.Get("oldbbbb2")
		wantNoErr(t, err)
		if w2.Title != "Second task" || w2.Kind != core.WorkEvolution || w2.State != core.StateDone || w2.Runner != nil {
			t.Fatalf("work 2 = %+v, want the seeded row", w2)
		}
		stateKind := core.EventState
		evs, err := l.Events("oldaaaa1", &stateKind)
		wantNoErr(t, err)
		if len(evs) != 1 || evs[0].Body != "running" {
			t.Fatalf("events = %v, want the seeded state event", evs)
		}
		fb, err := l.Feedback()
		wantNoErr(t, err)
		if len(fb) != 1 || fb[0].Text != "be succinct" || fb[0].Project == nil || *fb[0].Project != "proj" {
			t.Fatalf("feedback = %v, want the seeded row", fb)
		}

		// The schema grew in place: five nullable columns and new tables.
		after := workColumns(t, openDirect(t, path))
		if len(after) != 18 {
			t.Fatalf("migrated schema has %d work columns, want 18", len(after))
		}
		for _, col := range []string{"parent", "heading", "claim", "impact", "goal_type", "archived"} {
			if _, ok := after[col]; !ok {
				t.Fatalf("column %s missing after migration", col)
			}
		}
		for name, want := range before {
			got := after[name]
			if got != want {
				t.Fatalf("column %s changed: %v -> %v", name, want, got)
			}
		}
		for _, tbl := range []string{"concern", "worktree", "filed", "cursor"} {
			if _, err := openDirect(t, path).Query("SELECT COUNT(*) FROM " + tbl); err != nil {
				t.Fatalf("table %s missing after migration: %v", tbl, err)
			}
		}
		// An event written before effective and payload existed reads with both
		// absent, not defaulted: only one of "not separated" and "recorded the
		// same way" is true of an old row.
		evs, err = l.Events("oldaaaa1", nil)
		wantNoErr(t, err)
		if len(evs) != 1 || evs[0].Effective != nil || evs[0].Decision != nil {
			t.Fatalf("old event = %+v, want it read with no effective time and no claim", evs[0])
		}

		// New features write into the migrated ledger.
		epic, err := l.Add("proj", "Epic", AddOptions{Kind: core.WorkEpic})
		wantNoErr(t, err)
		if _, err := l.Add("proj", "T", AddOptions{Parent: &epic.ID}); err != nil {
			t.Fatalf("add task under epic: %v", err)
		}
		if _, err := l.AddConcern(epic.ID, "risk"); err != nil {
			t.Fatalf("add concern: %v", err)
		}
		if _, err := l.AddWorktree(epic.ID, WorktreeInfo{Path: "/tmp/wt", Kind: core.WorktreePrivate, Origin: core.OriginAttached}); err != nil {
			t.Fatalf("add worktree: %v", err)
		}
		wantNoErr(t, l.Close())

		// And the whole thing, old rows and new, survives a reopen.
		l, err = New(path)
		wantNoErr(t, err)
		defer l.Close()
		if got := allTasks(t, l, epic.ID); len(got) != 1 {
			t.Fatalf("tasks after reopen = %v, want 1", got)
		}
		if got := allOpenConcerns(t, l, epic.ID); len(got) != 1 {
			t.Fatalf("concerns after reopen = %v, want 1", got)
		}
		if got := allWorktrees(t, l, epic.ID); len(got) != 1 {
			t.Fatalf("worktrees after reopen = %v, want 1", got)
		}
	})

	t.Run("A Cursor Says How Far A Sweep Has Read, And Only Forwards", func(t *testing.T) {
		l := newTestLedger(t)
		add(t, l, "p", "Work", AddOptions{})

		// A cursor that does not exist reads as 0, so a first sweep sees
		// everything rather than nothing.
		v, err := l.Cursor("p", "review.events")
		wantNoErr(t, err)
		if v != 0 {
			t.Fatalf("cursor = %d, want 0 on a ledger that has never swept", v)
		}

		// Forward is the only direction a cursor moves.
		wantNoErr(t, l.SetCursor("p", "review.events", 12))
		v, err = l.Cursor("p", "review.events")
		wantNoErr(t, err)
		if v != 12 {
			t.Fatalf("cursor = %d, want 12", v)
		}
		wantNoErr(t, l.SetCursor("p", "review.events", 12))
		wantNoErr(t, l.SetCursor("p", "review.events", 13))
		err = l.SetCursor("p", "review.events", 12)
		wantErr(t, err, "cursor p/review.events is at 13; refusing to move it back to 12")

		// Names are independent within a project, and so are projects, so a
		// sweep that keeps two streams keeps two independent marks.
		v, err = l.Cursor("p", "review.feedback")
		wantNoErr(t, err)
		if v != 0 {
			t.Errorf("a second cursor = %d, want 0: names are independent", v)
		}
		wantNoErr(t, l.SetCursor("other", "review.events", 3))
		v, err = l.Cursor("other", "review.events")
		wantNoErr(t, err)
		if v != 3 {
			t.Errorf("another project's cursor = %d, want 3: projects are independent", v)
		}
	})

	t.Run("Migration Is Additive Only", func(t *testing.T) {
		path := writeOldLedger(t)
		l, err := New(path)
		wantNoErr(t, err)
		wantNoErr(t, l.Close())

		db := openDirect(t, path)
		migrated := dump(t, db)
		cols := workColumns(t, db)
		for _, col := range []string{"parent", "heading", "claim", "impact"} {
			if cols[col][0] != 0 {
				t.Fatalf("added column %s is NOT NULL; TypeScript wd inserts would break", col)
			}
		}
		// A worktree insert that names no origin, as the TypeScript wd writes it.
		if _, err := db.Exec(`INSERT INTO worktree (work, path, branch, kind, state, created) VALUES (?, ?, ?, ?, ?, ?)`,
			"oldaaaa1", "/wt/ts", nil, "private", "active", "2026-09-01T10:00:00.000Z"); err != nil {
			t.Fatalf("TypeScript-shaped worktree insert: %v", err)
		}
		migrated = dump(t, db)

		// Opening an already-migrated ledger changes nothing.
		l, err = New(path)
		wantNoErr(t, err)
		wantNoErr(t, l.Close())
		if again := dump(t, openDirect(t, path)); again != migrated {
			t.Fatalf("second open changed the ledger:\n%s\nvs\n%s", again, migrated)
		}
	})

	t.Run("A Worktree Records Whether The Director Made It", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ledger.db")
		l, err := New(path)
		wantNoErr(t, err)
		w := add(t, l, "p", "t", AddOptions{})
		made, err := l.AddWorktree(w.ID, WorktreeInfo{Path: "/wt/made", Kind: core.WorktreePrivate, Origin: core.OriginDirector})
		wantNoErr(t, err)
		attached, err := l.AddWorktree(w.ID, WorktreeInfo{Path: "/wt/attached", Kind: core.WorktreePrivate, Origin: core.OriginAttached})
		wantNoErr(t, err)
		if made.Origin != core.OriginDirector || attached.Origin != core.OriginAttached {
			t.Fatalf("origins = %q/%q, want director/attached", made.Origin, attached.Origin)
		}
		if got, err := l.SetWorktreeState(made.ID, core.WorktreeRemoved); err != nil || got.State != core.WorktreeRemoved || got.Origin != core.OriginDirector {
			t.Fatalf("removed = %+v, %v, want removed director worktree", got, err)
		}
		wantNoErr(t, l.Close())

		// A worktree table from before origins were recorded.
		db := openDirect(t, path)
		for _, stmt := range []string{
			`ALTER TABLE worktree DROP COLUMN origin`,
			`INSERT INTO worktree (work, path, branch, kind, state, created) VALUES ('` + w.ID + `', '/wt/old', 'wd-old', 'shared', 'active', '2026-09-01T10:00:00.000Z')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
		wantNoErr(t, db.Close())
		l, err = New(path)
		wantNoErr(t, err)
		t.Cleanup(func() { l.Close() })
		for _, wt := range allWorktrees(t, l, w.ID) {
			if wt.Origin != core.OriginAttached {
				t.Fatalf("worktree %s origin = %q after migration, want attached", wt.Path, wt.Origin)
			}
		}
	})
}

// A goal's story happens on its tasks, so the one set a surface needs is the
// goal's row plus everything under it — read once, in event order, rather than
// as two lists a reader has to put together and could put together wrongly.
func TestAGoalsEventsAreItsOwnAndItsTasks(t *testing.T) {
	l := newTestLedger(t)
	goal := add(t, l, "p", "Autonomous factory", AddOptions{Kind: core.WorkEpic})
	t1 := add(t, l, "p", "Drive the loop", AddOptions{Parent: &goal.ID})
	t2 := add(t, l, "p", "Show the goal in context", AddOptions{Parent: &goal.ID})
	elsewhere := add(t, l, "p", "Another goal", AddOptions{Kind: core.WorkEpic})

	move(t, l, goal.ID, core.StateRunning)
	claim, err := l.Decide(t1.ID, "the loop decided", core.Decision{Question: "which port?", Answer: "8080", Source: "judge"}, nil)
	wantNoErr(t, err)
	if _, err := l.AddEvent(t2.ID, core.EventPr, "commit https://x.test/c/1"); err != nil {
		t.Fatalf("pr: %v", err)
	}
	if _, err := l.AddEvent(elsewhere.ID, core.EventState, "running"); err != nil {
		t.Fatalf("other goal event: %v", err)
	}

	events, err := l.EventsUnder(goal.ID)
	wantNoErr(t, err)
	// Exactly the goal's own row and its two tasks', in event order: the spine is
	// in the set, and so is every decision, landing and report filed on a task,
	// because that is what makes a goal readable as a whole rather than as a row
	// that looks like it decided nothing.
	rows := map[string]int{}
	for _, e := range events {
		rows[e.Work]++
	}
	if len(rows) != 3 || rows[goal.ID] == 0 || rows[t1.ID] == 0 || rows[t2.ID] == 0 {
		t.Errorf("rows under goal = %v, want the goal's own row and both its tasks'", rows)
	}
	if !slices.IsSortedFunc(events, func(a, b core.Event) int { return a.ID - b.ID }) {
		t.Errorf("events under goal are not in id order, so the log reads out of order")
	}
	var sawClaim, sawLanding bool
	for _, e := range events {
		if e.ID == claim.ID {
			sawClaim = true
		}
		if strings.HasPrefix(e.Body, "commit https://x.test/c/1") {
			sawLanding = true
		}
	}
	if !sawClaim || !sawLanding {
		t.Errorf("events under goal carry the decision = %v and the landing = %v, want both", sawClaim, sawLanding)
	}
	// Another goal's events are not in this one: a subtree that leaked would show
	// two goals' decisions as if they were one run.
	for _, e := range events {
		if e.Work == elsewhere.ID {
			t.Fatalf("events under goal include another goal's event %d", e.ID)
		}
	}

	// A task has nothing under it, so for a task this is its own events — the
	// same set `Events` gives, not the whole goal's log.
	own, err := l.EventsUnder(t1.ID)
	wantNoErr(t, err)
	same, err := l.Events(t1.ID, nil)
	wantNoErr(t, err)
	if len(own) != len(same) {
		t.Errorf("events under task = %d, want its own %d, not the whole goal's log", len(own), len(same))
	}

	// The spine still reads on its own, for a surface that wants one row's log.
	solo, err := l.Events(goal.ID, nil)
	wantNoErr(t, err)
	for _, e := range solo {
		if e.Work != goal.ID {
			t.Fatalf("Events on the goal row returned an event on %s", e.Work)
		}
	}
	if len(solo) >= len(events) {
		t.Errorf("the goal row alone has %d events and the subtree %d, want the subtree to be more", len(solo), len(events))
	}
}
func TestFinishedWorkIsReopenedWithAReasonOrNotAtAll(t *testing.T) {
	l := newTestLedger(t)
	w := add(t, l, "p", "A goal somebody keeps extending", AddOptions{Kind: core.WorkGoal})
	task := add(t, l, "p", "The first half", AddOptions{Parent: &w.ID})
	closeGoal(t, l, w.ID, task.ID)

	// A reopen with no reason is refused, and the refusal says what to do
	// instead, because the two reasons someone arrives at a finished goal —
	// asking it something, and building on it — look identical in the state.
	_, err := l.Reopen(w.ID, "")
	if err == nil || !strings.Contains(err.Error(), "wd context "+w.ID) {
		t.Fatalf("Reopen with no reason = %v, want it refused naming the reads that answer a question", err)
	}
	if got := mustGet(t, l, w.ID); got.State != core.StateDone {
		t.Fatalf("state after the refusal = %q, want the goal left done", got.State)
	}

	opened, err := l.Reopen(w.ID, "a second half on the driver")
	wantNoErr(t, err)
	if opened.State != core.StateRunning {
		t.Fatalf("state = %q, want running: someone working more on it is on it now, not waiting to be picked up", opened.State)
	}
	// The reopen is a claim, not a bare state change: what is being worked on
	// travels with it, and a review can read it as one.
	evs, err := l.Events(w.ID, nil)
	wantNoErr(t, err)
	last := evs[len(evs)-1]
	if last.Kind != core.EventDecision || last.Decision == nil {
		t.Fatalf("last event = %+v, want a decision", last)
	}
	if !strings.Contains(last.Decision.Answer, "a second half on the driver") {
		t.Errorf("the claim answers %q, want it to say what is being worked on", last.Decision.Answer)
	}
	// The first run's history is not erased — it is what the second one reads
	// against, and the evidence of the first close is still here to read.
	closing := allEvents(t, l, w.ID, nil)
	if len(closing) <= 3 {
		t.Errorf("events = %d, want the first run's events kept alongside the reopen", len(closing))
	}

	// Dropped and abandoned are the two ways of saying a thing is finished with,
	// on purpose. Neither comes back to life: a state change should not be able
	// to undo a deliberate ending by accident.
	for _, ending := range []struct {
		name string
		to   core.State
	}{
		{"dropped", core.StateDropped},
		{"abandoned", core.StateAbandoned},
	} {
		d := add(t, l, "p", "Finished with, on purpose "+ending.name, AddOptions{})
		// Dropped/abandoned are only reachable from states where work was attempted.
		move(t, l, d.ID, core.StateRunning)
		move(t, l, d.ID, ending.to)
		_, err := l.Reopen(d.ID, "on reflection")
		if err == nil || !strings.Contains(err.Error(), "only done work is reopened") {
			t.Errorf("Reopen of %s work = %v, want it refused: that ending is deliberate", ending.name, err)
		}
	}

	// The machine has the edge — done work can run again — but Transition will
	// not take it, so nothing but Reopen can reach it with a reason attached.
	other := add(t, l, "p", "Another finished thing", AddOptions{})
	move(t, l, other.ID, core.StateDone)
	_, err = l.Transition(other.ID, core.StateRunning)
	if err == nil || !strings.Contains(err.Error(), "wd reopen "+other.ID) {
		t.Errorf("Transition done → running = %v, want it refused naming Reopen", err)
	}
	if got := mustGet(t, l, other.ID); got.State != core.StateDone {
		t.Errorf("state after the refusal = %q, want it left done", got.State)
	}
	if !slices.Contains(core.Transitions[core.StateDone], core.StateRunning) {
		t.Error("the machine does not list done → running, so Reopen could not take it either")
	}
	if core.Reopenable(core.StateAbandoned) || core.Reopenable(core.StateDropped) {
		t.Error("Reopenable says a deliberate ending can come back to life")
	}
}

// A reopened goal still holds the report, verify and pull request of the run
// that closed it. Judging readiness on those would let a goal walk straight
// back to soft-done on a completion nobody did.
func TestReadinessIsJudgedOnThisRunsEvidence(t *testing.T) {
	l := newTestLedger(t)
	g := add(t, l, "p", "The loop runs itself", AddOptions{Kind: core.WorkGoal})
	first := add(t, l, "p", "The first pass", AddOptions{Parent: &g.ID})
	closeGoal(t, l, g.ID, first.ID)

	opened, err := l.Reopen(g.ID, "and now the promotion step")
	wantNoErr(t, err)
	second := add(t, l, "p", "The promotion step", AddOptions{Parent: &opened.ID})

	// Every gate the first run satisfied is unsatisfied again: the goal's own
	// DONE report, its verify, and its pull request are all read from before the
	// reopen.
	var nr core.NotReady
	if _, err := l.SoftDone(g.ID, false); !errors.As(err, &nr) {
		t.Fatalf("SoftDone after reopen = %v, want NotReady", err)
	} else if !slices.Equal(nr.Missing, []string{"1 task(s) not done", "DONE report", "passing verify", "pull request"}) {
		t.Errorf("missing = %v, want the second run to prove all four again", nr.Missing)
	}

	// The second run files its own evidence and the gate opens. What closed the
	// first run took back; it did not become a wall.
	if _, err := l.AddEvent(g.ID, core.EventReport, "DONE\nSTATUS: DONE"); err != nil {
		t.Fatalf("report: %v", err)
	}
	if _, err := l.AddEvent(g.ID, core.EventVerify, "pass"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := l.AddEvent(g.ID, core.EventPr, "commit https://github.test/o/r/commit/5a5f3bb"); err != nil {
		t.Fatalf("pr: %v", err)
	}
	closeOnlyTask(t, l, second.ID)
	move(t, l, g.ID, core.StateReview)
	if got := softDone(t, l, g.ID, false); got.State != core.StateSoftDone {
		t.Fatalf("state = %q, want soft-done once the second run proved itself", got.State)
	}

	// Work never reopened is judged on everything it has, which is what makes
	// the reopen the only thing that changes.
	plain := add(t, l, "p", "Never reopened", AddOptions{})
	for _, s := range []core.State{core.StateRunning, core.StateReview} {
		move(t, l, plain.ID, s)
	}
	l.AddEvent(plain.ID, core.EventReport, "DONE\nSTATUS: DONE")
	l.AddEvent(plain.ID, core.EventVerify, "pass")
	if got := softDone(t, l, plain.ID, false); got.State != core.StateSoftDone {
		t.Fatalf("state = %q, want soft-done on its own evidence", got.State)
	}
}

// A task under a finished goal would sit there with nothing to run it: the goal
// is not open, so no pass drives it, and the rollup would show open work under
// finished work.
func TestATaskCannotBeAddedUnderAGoalThatHasComeToRest(t *testing.T) {
	l := newTestLedger(t)
	g := add(t, l, "p", "A goal that finished", AddOptions{Kind: core.WorkGoal})
	task := add(t, l, "p", "Its only task", AddOptions{Parent: &g.ID})
	closeGoal(t, l, g.ID, task.ID)

	_, err := l.Add("p", "A second task nobody will run", AddOptions{Parent: &g.ID})
	if err == nil || !strings.Contains(err.Error(), "wd reopen "+g.ID) {
		t.Fatalf("Add under a done goal = %v, want it refused naming reopen", err)
	}
	tasks, err := l.Tasks(g.ID)
	wantNoErr(t, err)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want the refused task not to exist", len(tasks))
	}

	// Reopened, the same task is exactly the thing the reopen was for.
	opened, err := l.Reopen(g.ID, "there is more to do here")
	wantNoErr(t, err)
	if _, err := l.Add("p", "A second task", AddOptions{Parent: &opened.ID}); err != nil {
		t.Fatalf("Add after reopen: %v", err)
	}
}

// closeGoal drives a goal and its only task all the way to done: the task
// through the report and verify gates, the goal through those and a pull
// request, which is what a goal needs before soft-done will open.
func closeGoal(t *testing.T, l *Ledger, goal, task string) {
	t.Helper()
	closeOnlyTask(t, l, task)
	for _, s := range []core.State{core.StateRunning, core.StateReview} {
		move(t, l, goal, s)
	}
	if _, err := l.AddEvent(goal, core.EventReport, "DONE\nSTATUS: DONE"); err != nil {
		t.Fatalf("goal report: %v", err)
	}
	if _, err := l.AddEvent(goal, core.EventVerify, "pass"); err != nil {
		t.Fatalf("goal verify: %v", err)
	}
	if _, err := l.AddEvent(goal, core.EventPr, "commit https://github.test/o/r/commit/5a5f3bb"); err != nil {
		t.Fatalf("goal pr: %v", err)
	}
	softDone(t, l, goal, false)
	got, err := l.Transition(goal, core.StateDone)
	wantNoErr(t, err)
	if got.State != core.StateDone {
		t.Fatalf("state = %q, want done", got.State)
	}
}

// closeOnlyTask takes a task all the way to done on its own evidence, because a
// goal's gate counts its tasks and an unfinished one holds the goal open.
func closeOnlyTask(t *testing.T, l *Ledger, task string) {
	t.Helper()
	for _, s := range []core.State{core.StateRunning, core.StateReview} {
		move(t, l, task, s)
	}
	if _, err := l.AddEvent(task, core.EventReport, "DONE\nSTATUS: DONE"); err != nil {
		t.Fatalf("task report: %v", err)
	}
	if _, err := l.AddEvent(task, core.EventVerify, "pass"); err != nil {
		t.Fatalf("task verify: %v", err)
	}
	softDone(t, l, task, true)
	move(t, l, task, core.StateDone)
}

func mustGet(t *testing.T, l *Ledger, id string) core.Work {
	t.Helper()
	w, err := l.Get(id)
	wantNoErr(t, err)
	return w
}

// Abandoning records that a thing stopped without shipping. For a duplicate
// whose work landed under another id, that sentence is false, and the row cannot
// be left saying it forever — it is the one thing on a board that contradicts
// work that demonstrably shipped.
func TestAnAbandonedRowCanBeReleasedIntoAChoice(t *testing.T) {
	l := newTestLedger(t)
	dup := add(t, l, "p", "The same work shipped as another task", AddOptions{})
	for _, s := range []core.State{core.StateRunning, core.StateReview} {
		move(t, l, dup.ID, s)
	}
	if _, err := l.Abandon(dup.ID, core.AbandonNoPR, "never started"); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	// The reason is the whole content of the command, so it is required and its
	// absence is refused with the command that takes one.
	if _, err := l.Release(dup.ID, ""); err == nil || !strings.Contains(err.Error(), "wd release "+dup.ID) {
		t.Fatalf("Release with no reason = %v, want it refused naming the command", err)
	}
	if got := mustGet(t, l, dup.ID); got.State != core.StateAbandoned {
		t.Fatalf("state = %q, want the refusal to leave it abandoned", got.State)
	}

	released, err := l.Release(dup.ID, "the same work shipped as 17b4a94b, which is done")
	wantNoErr(t, err)
	if released.State != core.StateDropped {
		t.Fatalf("state = %q, want dropped: the work was let go of, never failed", released.State)
	}
	evs, err := l.Events(dup.ID, nil)
	wantNoErr(t, err)
	last := evs[len(evs)-1]
	if last.Kind != core.EventDecision || last.Decision == nil {
		t.Fatalf("last event = %+v, want the correction filed as a decision", last)
	}
	if !strings.Contains(last.Decision.Answer, "17b4a94b") {
		t.Errorf("the claim answers %q, want it to name what was true instead", last.Decision.Answer)
	}

	// History is not rewritten. The abandon event stays, because work really did
	// not ship, and a reader sees the stop and then the correction — which is the
	// only way the row can stop contradicting the work without erasing it.
	var stops int
	for _, e := range evs {
		if e.Kind == core.EventAbandon && strings.HasPrefix(e.Body, "no-pr:") {
			stops++
		}
	}
	if stops != 1 {
		t.Errorf("abandon events = %d, want the original stop kept alongside the release", stops)
	}

	// A bare transition cannot do it: the reason is the content, not a formality.
	other := add(t, l, "p", "Another stop", AddOptions{})
	for _, s := range []core.State{core.StateRunning, core.StateReview} {
		move(t, l, other.ID, s)
	}
	if _, err := l.Abandon(other.ID, core.AbandonNoPR, "never started"); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if _, err := l.Transition(other.ID, core.StateDropped); err == nil || !strings.Contains(err.Error(), "wd release "+other.ID) {
		t.Errorf("Transition abandoned → dropped = %v, want it refused naming Release", err)
	}
	if got := mustGet(t, l, other.ID); got.State != core.StateAbandoned {
		t.Errorf("state = %q, want it left abandoned", got.State)
	}
	if !slices.Contains(core.Transitions[core.StateAbandoned], core.StateDropped) {
		t.Error("the machine does not list abandoned → dropped, so Release could not take it either")
	}

	// Release is not a second name for reopening. Work that never shipped does
	// not become work in progress by being relabelled, and only abandoned work
	// has a stop to correct.
	for _, w := range []struct {
		name string
		work core.Work
		to   core.State
	}{
		{"done", finish(t, l, "Shipped"), core.StateDone},
		{"dropped", released, core.StateDropped},
	} {
		if _, err := l.Release(w.work.ID, "on reflection"); err == nil || !strings.Contains(err.Error(), "only abandoned work is released") {
			t.Errorf("Release of %s work = %v, want it refused", w.name, err)
		}
	}
	if _, err := l.Release(dup.ID, "again"); err == nil {
		t.Error("releasing an already-released row succeeded, want a refusal")
	}
}

// finish closes work through every gate it needs and returns it done, so a test
// about states it must not be able to leave can start from a real one.
func finish(t *testing.T, l *Ledger, title string) core.Work {
	t.Helper()
	w := add(t, l, "p", title, AddOptions{})
	for _, s := range []core.State{core.StateRunning, core.StateReview} {
		move(t, l, w.ID, s)
	}
	l.AddEvent(w.ID, core.EventReport, "DONE\nSTATUS: DONE")
	l.AddEvent(w.ID, core.EventVerify, "pass")
	softDone(t, l, w.ID, false)
	move(t, l, w.ID, core.StateDone)
	return mustGet(t, l, w.ID)
}

// A client names a workspace and never sends a path, so the containment check is
// the whole of what stands between a token from a phone and a directory
// somewhere else on the machine.
func TestWorkspacesAreRegisteredAndNewOnesAreOnlyMadeUnderAParentThatAllowsIt(t *testing.T) {
	l := newTestLedger(t)

	parent, err := l.AddWorkspace("workspaces", t.TempDir(), true)
	wantNoErr(t, err)
	if !parent.Creates {
		t.Fatal("parent = a plain workspace, want it registered as one")
	}

	// The ordinary path: a name under a parent that allows it.
	path, err := l.CreateWorkspacePath(parent.ID, "new-idea")
	wantNoErr(t, err)
	if !strings.HasPrefix(path, parent.Path+string(filepath.Separator)) || !strings.HasSuffix(path, "new-idea") {
		t.Errorf("path = %q, want it inside %q", path, parent.Path)
	}

	// A workspace is not a parent. This is the rule that keeps "create" from
	// meaning "create anywhere" one flag at a time.
	plain, err := l.AddWorkspace("a-repo", t.TempDir(), false)
	wantNoErr(t, err)
	_, err = l.CreateWorkspacePath(plain.ID, "sneaky")
	if err == nil || !strings.Contains(err.Error(), "--creates") {
		t.Errorf("create under a plain workspace = %v, want it refused naming how to make a parent", err)
	}

	// The name comes from a client, so containment is checked rather than
	// assumed. Each of these escapes a naive join.
	for _, name := range []string{"../elsewhere", "a/../../b", "/etc", "..", ".", "sub/dir"} {
		if got, err := l.CreateWorkspacePath(parent.ID, name); err == nil {
			t.Errorf("name %q was accepted as %q, want it refused: it is not one directory inside the parent", name, got)
		}
	}
	if _, err := l.CreateWorkspacePath(parent.ID, ""); err == nil {
		t.Error("an empty name was accepted, want it refused")
	}

	// Two of these three are about the ledger refusing rather than the
	// filesystem, and a directory that does not exist yet is not a refusal: a
	// workspace can be registered before anything is in it.
	again, err := l.AddWorkspace("workspaces", parent.Path, true)
	if err == nil {
		t.Errorf("the same path registered twice as %s, want the second refused: one directory is one workspace", again.ID)
	}

	listed, err := l.Workspaces()
	wantNoErr(t, err)
	if len(listed) != 2 {
		t.Fatalf("workspaces = %d, want the parent and the plain one", len(listed))
	}
	if !listed[0].Creates {
		t.Errorf("list starts with %s, want parents first: a client rendering a picker shows where new work can go above where it is", listed[0].Name)
	}

	// A person types a name; a client holds an id. Both must land on one row.
	byID, err := l.Workspace(parent.ID)
	wantNoErr(t, err)
	byPath, err := l.Workspace(parent.Path)
	wantNoErr(t, err)
	byName, err := l.Workspace("workspaces")
	wantNoErr(t, err)
	if byID.ID != byPath.ID || byID.ID != byName.ID {
		t.Errorf("id %s, path %s and name %s did not all find the same workspace", byID.ID, byPath.ID, byName.ID)
	}

	// A name that matches two is refused rather than resolved to whichever
	// sorted first: picking one silently is how work ends up on the wrong one.
	for _, dir := range []string{t.TempDir(), t.TempDir()} {
		_, err := l.AddWorkspace("shared", dir, false)
		wantNoErr(t, err)
	}
	_, err = l.Workspace("shared")
	if err == nil || !strings.Contains(err.Error(), "names 2 workspaces") {
		t.Errorf("ambiguous name = %v, want it refused naming the ids to choose between", err)
	}
}

// wantErrNaming fails unless err is an error whose message contains part.
func wantErrNaming(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), part) {
		t.Fatalf("error = %v, want one naming %q", err, part)
	}
}

func TestArchivingPutsWorkOutOfSightWithEverythingUnderIt(t *testing.T) {
	l := newTestLedger(t)
	goal := add(t, l, "p", "Goal", AddOptions{Kind: core.WorkGoal})
	shipped := add(t, l, "p", "Shipped", AddOptions{Parent: &goal.ID})
	move(t, l, shipped.ID, core.StateDone)
	running := add(t, l, "p", "Running", AddOptions{Parent: &goal.ID})
	move(t, l, running.ID, core.StateRunning)
	loose := add(t, l, "p", "Loose", AddOptions{})

	_, err := l.Archive(goal.ID, "  ")
	wantErrNaming(t, err, goal.ID)
	wantErrNaming(t, err, running.ID)
	for _, id := range []string{goal.ID, shipped.ID, running.ID} {
		if w, _ := l.Get(id); w.Archived != nil {
			t.Fatalf("%s archived by a refused archive", id)
		}
	}

	archived, err := l.Archive(goal.ID, "the mobile app is out of scope")
	wantNoErr(t, err)
	if len(archived) != 3 {
		t.Fatalf("archived %d works, want the goal and both tasks", len(archived))
	}
	want := map[string]core.State{goal.ID: core.StateDropped, shipped.ID: core.StateDone, running.ID: core.StateDropped}
	note := core.EventNote
	decision := core.EventDecision
	for id, state := range want {
		w, err := l.Get(id)
		wantNoErr(t, err)
		if w.Archived == nil || w.State != state {
			t.Errorf("%s = %s archived %v, want %s and archived", id, w.State, w.Archived, state)
		}
		notes := allEvents(t, l, id, &note)
		if len(notes) == 0 || notes[len(notes)-1].Body != "archived" {
			t.Errorf("%s notes = %v, want one saying archived", id, notes)
		}
		decisions := allEvents(t, l, id, &decision)
		dropped := state == core.StateDropped && id != shipped.ID
		if dropped && (len(decisions) == 0 || !strings.Contains(decisions[len(decisions)-1].Body, "the mobile app is out of scope")) {
			t.Errorf("%s decisions = %v, want the reason it was dropped", id, decisions)
		}
		if !dropped && len(decisions) != 0 {
			t.Errorf("%s at rest gained decisions %v", id, decisions)
		}
	}
	if w, _ := l.Get(loose.ID); w.Archived != nil {
		t.Errorf("work outside the goal was archived")
	}
	_, err = l.Archive(goal.ID, "again")
	wantErrNaming(t, err, "already archived")
}

func TestUnarchivingBringsWorkBackAsItWas(t *testing.T) {
	l := newTestLedger(t)
	goal := add(t, l, "p", "Goal", AddOptions{Kind: core.WorkGoal})
	task := add(t, l, "p", "Task", AddOptions{Parent: &goal.ID})
	move(t, l, task.ID, core.StateDone)
	move(t, l, goal.ID, core.StateDone)
	_, err := l.Archive(goal.ID, "")
	wantNoErr(t, err)

	back, err := l.Unarchive(goal.ID)
	wantNoErr(t, err)
	if len(back) != 2 {
		t.Fatalf("unarchived %d works, want the goal and its task", len(back))
	}
	note := core.EventNote
	for _, id := range []string{goal.ID, task.ID} {
		w, err := l.Get(id)
		wantNoErr(t, err)
		if w.Archived != nil || w.State != core.StateDone {
			t.Errorf("%s = %s archived %v, want done and on the board", id, w.State, w.Archived)
		}
		notes := allEvents(t, l, id, &note)
		if len(notes) == 0 || notes[len(notes)-1].Body != "unarchived" {
			t.Errorf("%s notes = %v, want one saying unarchived", id, notes)
		}
	}
	_, err = l.Unarchive(goal.ID)
	wantErrNaming(t, err, "not archived")
}

func TestAListCanLeaveArchivedWorkOutOrShowOnlyIt(t *testing.T) {
	l := newTestLedger(t)
	kept := add(t, l, "p", "Kept", AddOptions{})
	gone := add(t, l, "p", "Gone", AddOptions{})
	move(t, l, gone.ID, core.StateDone)
	_, err := l.Archive(gone.ID, "")
	wantNoErr(t, err)
	ids := func(f ListFilter) []string {
		items, err := l.List(f)
		wantNoErr(t, err)
		out := []string{}
		for _, w := range items {
			out = append(out, w.ID)
		}
		return out
	}
	no, yes := false, true
	if got := ids(ListFilter{}); len(got) != 2 {
		t.Errorf("unset = %v, want both", got)
	}
	if got := ids(ListFilter{Archived: &no}); len(got) != 1 || got[0] != kept.ID {
		t.Errorf("archived false = %v, want only %s", got, kept.ID)
	}
	if got := ids(ListFilter{Archived: &yes}); len(got) != 1 || got[0] != gone.ID {
		t.Errorf("archived true = %v, want only %s", got, gone.ID)
	}
}
