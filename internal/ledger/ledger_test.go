package ledger

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wd/internal/core"
)

func failEventInserts(t *testing.T, l *Ledger) {
	t.Helper()
	if _, err := l.db.Exec(`CREATE TRIGGER fail_event BEFORE INSERT ON event BEGIN SELECT RAISE(ABORT, 'event refused'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

func TestWritesAndTheirEventsAreAtomic(t *testing.T) {
	t.Run("add", func(t *testing.T) {
		l := newTestLedger(t)
		failEventInserts(t, l)
		if _, err := l.Add("p", "t", AddOptions{}); err == nil {
			t.Fatal("add succeeded without its event")
		}
		ws, err := l.List(ListFilter{})
		wantNoErr(t, err)
		if len(ws) != 0 {
			t.Fatalf("work = %v, want none after a failed event", ws)
		}
	})
	t.Run("transition", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		failEventInserts(t, l)
		if _, err := l.Transition(w.ID, core.StateBriefed); err == nil {
			t.Fatal("transition succeeded without its event")
		}
		got, err := l.Get(w.ID)
		wantNoErr(t, err)
		if got.State != core.StateQueued {
			t.Fatalf("state = %q, want queued after a failed event", got.State)
		}
	})
	t.Run("resolve concern", func(t *testing.T) {
		l := newTestLedger(t)
		w := add(t, l, "p", "t", AddOptions{})
		c, err := l.AddConcern(w.ID, "risk")
		wantNoErr(t, err)
		failEventInserts(t, l)
		if _, err := l.ResolveConcern(c.ID, "fine"); err == nil {
			t.Fatal("resolve succeeded without its event")
		}
		cs, err := l.Concerns(&w.ID)
		wantNoErr(t, err)
		if cs[0].Resolved != 0 || cs[0].Decision != nil {
			t.Fatalf("concern = %+v, want unresolved after a failed event", cs[0])
		}
	})
}

func TestNewEnforcesForeignKeysAndWaitsOnLocks(t *testing.T) {
	l := newTestLedger(t)
	var fk, timeout int
	wantNoErr(t, l.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk))
	wantNoErr(t, l.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout))
	if fk != 1 || timeout != 5000 {
		t.Fatalf("foreign_keys = %d, busy_timeout = %d, want 1 and 5000", fk, timeout)
	}
	if _, err := l.AddEvent("nope", core.EventNote, "x"); err == nil {
		t.Fatal("event for unknown work was stored")
	}
	if _, err := l.AddConcern("nope", "x"); err == nil {
		t.Fatal("concern for unknown work was stored")
	}
	if _, err := l.AddWorktree("nope", WorktreeInfo{Path: "/tmp/wt", Kind: core.WorktreePrivate, Origin: core.OriginAttached}); err == nil {
		t.Fatal("worktree for unknown work was stored")
	}
	if err := l.File("nope", core.EventQuestion, "x", core.TranscriptMark{Session: "s", Entries: 1}); err == nil {
		t.Fatal("filed event for unknown work was stored")
	}
}

func TestReadThenWriteTransactionsOnOneFileBothCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	a, err := New(path)
	wantNoErr(t, err)
	defer a.Close()
	b, err := New(path)
	wantNoErr(t, err)
	defer b.Close()
	w := add(t, a, "p", "t", AddOptions{})
	readThenWrite := func(tx *sql.Tx, title string) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&n); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE work SET title = ? WHERE id = ?`, title, w.ID)
		return err
	}
	aRead := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		<-aRead
		bDone <- b.inTx(func(tx *sql.Tx) error { return readThenWrite(tx, "b") })
	}()
	var bErr error
	bWaited := true
	aErr := a.inTx(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&n); err != nil {
			return err
		}
		close(aRead)
		select {
		case bErr = <-bDone:
			bWaited = false
		case <-time.After(300 * time.Millisecond):
		}
		_, err := tx.Exec(`UPDATE work SET title = 'a' WHERE id = ?`, w.ID)
		return err
	})
	if bWaited {
		bErr = <-bDone
	}
	if aErr != nil || bErr != nil {
		t.Fatalf("a = %v, b = %v, want both committed", aErr, bErr)
	}
	got, err := a.Get(w.ID)
	wantNoErr(t, err)
	if got.Title != "b" {
		t.Fatalf("title = %q, want b: the second transaction waits for the first", got.Title)
	}
}

func TestLedgerWithOrphanRowsStillOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	db, err := sql.Open("sqlite", path)
	wantNoErr(t, err)
	_, err = db.Exec(schema + `INSERT INTO event (work, kind, body, at) VALUES ('gone', 'note', 'orphan', '2026-09-01T10:00:00.000Z');`)
	wantNoErr(t, err)
	wantNoErr(t, db.Close())
	l, err := New(path)
	wantNoErr(t, err)
	defer l.Close()
	evs, err := l.Events("gone", nil)
	wantNoErr(t, err)
	if len(evs) != 1 || evs[0].Body != "orphan" {
		t.Fatalf("events = %v, want the orphan row", evs)
	}
}

func TestSoftDoneFailsWhenEventsCannotBeRead(t *testing.T) {
	l := newTestLedger(t)
	w := add(t, l, "p", "t", AddOptions{})
	if _, err := l.db.Exec(`ALTER TABLE event RENAME TO event_gone`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	_, err := l.SoftDone(w.ID, false)
	var nr core.NotReady
	if err == nil || errors.As(err, &nr) {
		t.Fatalf("error = %v, want the query failure", err)
	}
}

func TestResolveConcernErrors(t *testing.T) {
	l := newTestLedger(t)
	w := add(t, l, "p", "t", AddOptions{})
	c, err := l.AddConcern(w.ID, "risk")
	wantNoErr(t, err)
	_, err = l.ResolveConcern(c.ID, "first")
	wantNoErr(t, err)
	_, err = l.ResolveConcern(c.ID, "second")
	wantErr(t, err, fmt.Sprintf("concern %d already resolved", c.ID))
	cs, err := l.Concerns(&w.ID)
	wantNoErr(t, err)
	if *cs[0].Decision != "first" {
		t.Fatalf("decision = %q, want the first one kept", *cs[0].Decision)
	}
	_, err = l.ResolveConcern(99, "x")
	wantErr(t, err, "no concern 99")
	if _, err := l.db.Exec(`ALTER TABLE concern RENAME TO concern_gone`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := l.ResolveConcern(c.ID, "x"); err == nil || strings.HasPrefix(err.Error(), "no concern") {
		t.Fatalf("error = %v, want the query failure", err)
	}
}

func TestSetWorktreeStateErrors(t *testing.T) {
	l := newTestLedger(t)
	_, err := l.SetWorktreeState(99, core.WorktreeMerged)
	wantErr(t, err, "no worktree 99")
	if _, err := l.db.Exec(`ALTER TABLE worktree RENAME TO worktree_gone`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := l.SetWorktreeState(1, core.WorktreeMerged); err == nil || strings.HasPrefix(err.Error(), "no worktree") {
		t.Fatalf("error = %v, want the query failure", err)
	}
}

func TestStoredValuesOutsideTheDomainFailNamingTheValue(t *testing.T) {
	cases := []struct {
		name, corrupt, bad string
		read               func(l *Ledger, w core.Work) error
	}{
		{"work kind", `UPDATE work SET kind = 'chore'`, "chore", func(l *Ledger, w core.Work) error { _, err := l.Get(w.ID); return err }},
		{"work state", `UPDATE work SET state = 'hibernating'`, "hibernating", func(l *Ledger, w core.Work) error { _, err := l.Get(w.ID); return err }},
		{"event kind", `UPDATE event SET kind = 'shout'`, "shout", func(l *Ledger, w core.Work) error { _, err := l.Events(w.ID, nil); return err }},
		{"feedback source", `UPDATE feedback SET source = 'rumour'`, "rumour", func(l *Ledger, w core.Work) error { _, err := l.Feedback(); return err }},
		{"worktree kind", `UPDATE worktree SET kind = 'borrowed'`, "borrowed", func(l *Ledger, w core.Work) error { _, err := l.Worktrees(w.ID); return err }},
		{"worktree state", `UPDATE worktree SET state = 'lost'`, "lost", func(l *Ledger, w core.Work) error { _, err := l.Worktrees(w.ID); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := newTestLedger(t)
			w := add(t, l, "p", "t", AddOptions{})
			_, err := l.AddFeedback("f", FeedbackOptions{})
			wantNoErr(t, err)
			_, err = l.AddWorktree(w.ID, WorktreeInfo{Path: "/tmp/wt", Kind: core.WorktreePrivate, Origin: core.OriginAttached})
			wantNoErr(t, err)
			if _, err := l.db.Exec(c.corrupt); err != nil {
				t.Fatalf("corrupt: %v", err)
			}
			err = c.read(l, w)
			if err == nil || !strings.Contains(err.Error(), `"`+c.bad+`"`) {
				t.Fatalf("error = %v, want one naming %q", err, c.bad)
			}
		})
	}
}

func TestOpenConcernsOfUnknownWorkFails(t *testing.T) {
	l := newTestLedger(t)
	_, err := l.OpenConcerns("nope")
	wantErr(t, err, "no work nope; wd status for known work items")
}

func TestSessionWritesToUnknownWorkFail(t *testing.T) {
	l := newTestLedger(t)
	wantErr(t, l.SetSession("nope", SessionInfo{Runner: "claude", Session: "s", Cwd: "/tmp"}), "no work nope; wd status for known work items")
	wantErr(t, l.SetCwd("nope", "/tmp"), "no work nope; wd status for known work items")
	wantErr(t, l.SetRef("nope", nil), "no work nope; wd status for known work items")
}

func TestAddEventReturnsTheEventItStored(t *testing.T) {
	l := newTestLedger(t)
	w := add(t, l, "p", "t", AddOptions{})
	got, err := l.AddEvent(w.ID, core.EventDecision, "use sqlite")
	wantNoErr(t, err)
	evs := allEvents(t, l, w.ID, nil)
	if last := evs[len(evs)-1]; got != last {
		t.Fatalf("AddEvent = %+v, want the stored %+v", got, last)
	}
}
