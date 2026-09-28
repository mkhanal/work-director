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
		t.Run("queued to done fails naming both states", func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			var it core.IllegalTransition
			_, err := l.Transition(w.ID, core.StateDone)
			if !errors.As(err, &it) {
				t.Fatalf("error = %v, want IllegalTransition", err)
			}
			if it.From != core.StateQueued || it.To != core.StateDone {
				t.Fatalf("IllegalTransition = %v, want queued to done", it)
			}
			wantErr(t, err, "illegal transition queued → done")
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
		})
		t.Run("done and dropped are terminal", func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			move(t, l, w.ID, core.StateDropped)
			_, err := l.Transition(w.ID, core.StateQueued)
			wantErr(t, err, "illegal transition dropped → queued")
		})
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

	t.Run("Epics Hold Tasks And Nothing Sits Under An Epic", func(t *testing.T) {
		l := newTestLedger(t)
		epic := add(t, l, "p", "E", AddOptions{Kind: core.WorkEpic})
		task := add(t, l, "p", "T", AddOptions{Parent: &epic.ID})
		if ts := allTasks(t, l, epic.ID); len(ts) != 1 || ts[0].ID != task.ID {
			t.Fatalf("tasks = %v, want [%s]", ts, task.ID)
		}
		_, err := l.Add("p", "E2", AddOptions{Kind: core.WorkEpic, Parent: &epic.ID})
		wantErr(t, err, "an epic cannot sit under another work item")
		plain := add(t, l, "p", "P", AddOptions{})
		_, err = l.Add("p", "T2", AddOptions{Parent: &plain.ID})
		wantErr(t, err, "parent "+plain.ID+" is not an epic")
		_, err = l.Add("p", "T3", AddOptions{Parent: strPtr("nope")})
		wantErr(t, err, "no work nope; wd status for known work items")
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
	})

	t.Run("Worktrees Track Path Branch And State", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		branch := "feature/x"
		wt, err := l.AddWorktree(w.ID, WorktreeInfo{Path: "/tmp/wt", Branch: &branch, Kind: core.WorktreePrivate})
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
		for _, s := range []core.State{core.StateBriefed, core.StateRunning, core.StateReview, core.StateSoftDone, core.StateDone} {
			move(t, l, b.ID, s)
		}
		if got := allConflicts(t, l, epic.ID); len(got) != 0 {
			t.Fatalf("conflicts = %v, want none once a task is done", got)
		}
	})

	t.Run("Soft Done Requires Epic Tasks Done", func(t *testing.T) {
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
		move(t, l, task.ID, core.StateSoftDone)
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

		// The schema grew in place: four nullable columns, two new tables.
		after := workColumns(t, openDirect(t, path))
		if len(after) != 16 {
			t.Fatalf("migrated schema has %d work columns, want 16", len(after))
		}
		for _, col := range []string{"parent", "heading", "claim", "impact"} {
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
		for _, tbl := range []string{"concern", "worktree"} {
			if _, err := openDirect(t, path).Query("SELECT COUNT(*) FROM " + tbl); err != nil {
				t.Fatalf("table %s missing after migration: %v", tbl, err)
			}
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
		if _, err := l.AddWorktree(epic.ID, WorktreeInfo{Path: "/tmp/wt", Kind: core.WorktreePrivate}); err != nil {
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

		// Opening an already-migrated ledger changes nothing.
		l, err = New(path)
		wantNoErr(t, err)
		wantNoErr(t, l.Close())
		if again := dump(t, openDirect(t, path)); again != migrated {
			t.Fatalf("second open changed the ledger:\n%s\nvs\n%s", again, migrated)
		}
	})
}
