// Package ledger is the sqlite-backed store of the director's truth: work
// items, their events, feedback, concerns and worktrees. Rows are parsed into core domain types at this boundary and never
// cast afterwards.
package ledger

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"wd/internal/core"
)

const schema = `
CREATE TABLE IF NOT EXISTS work (id TEXT PRIMARY KEY, project TEXT NOT NULL, title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL, state TEXT NOT NULL, runner TEXT, session TEXT, ref TEXT, cwd TEXT, created TEXT NOT NULL, updated TEXT NOT NULL,
  parent TEXT, heading TEXT, claim TEXT, impact TEXT);
CREATE TABLE IF NOT EXISTS event (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS feedback (id INTEGER PRIMARY KEY, text TEXT NOT NULL, project TEXT, card TEXT, source TEXT NOT NULL, at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS concern (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), text TEXT NOT NULL, resolved INTEGER NOT NULL DEFAULT 0, decision TEXT, at TEXT NOT NULL, resolved_at TEXT);
CREATE TABLE IF NOT EXISTS worktree (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), path TEXT NOT NULL, branch TEXT, kind TEXT NOT NULL, state TEXT NOT NULL, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS filed (work TEXT PRIMARY KEY REFERENCES work(id), session TEXT NOT NULL, entries INTEGER NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS worktree_one_active_shared ON worktree(work) WHERE kind = 'shared' AND state = 'active';
`

// Additive migration for ledgers created before epics existed.
var addedColumns = []struct{ col, decl string }{
	{"parent", "TEXT"}, {"heading", "TEXT"}, {"claim", "TEXT"}, {"impact", "TEXT"},
}

type Ledger struct {
	db *sql.DB
}

// Pragmas are per connection, so they ride on the DSN and apply to every
// connection the pool opens. busy_timeout lets parallel wd processes wait for
// the write lock; immediate transactions take it up front, so a
// read-then-write never fails on a snapshot another writer moved.
const dsnParams = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"

func New(path string) (*Ledger, error) {
	db, err := sql.Open("sqlite", path+dsnParams)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Ledger{db: db}, nil
}

func migrate(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(work)")
	if err != nil {
		return err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range addedColumns {
		if !slices.Contains(cols, c.col) {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE work ADD COLUMN %s %s", c.col, c.decl)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *Ledger) Close() error { return l.db.Close() }

// now matches new Date().toISOString(): UTC with millisecond precision.
func now() string { return time.Now().UTC().Format(core.TimeLayout) }

// newId matches randomUUID().slice(0, 8): eight hex characters.
func newId() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

type rowScanner interface{ Scan(dest ...any) error }

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (l *Ledger) inTx(f func(tx *sql.Tx) error) error {
	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func noWork(id string) error {
	return fmt.Errorf("no work %s; wd status for known work items", id)
}

// updateWork runs an UPDATE of one work row and fails when id matches none.
func updateWork(e execer, id, query string, args ...any) error {
	res, err := e.Exec(query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return noWork(id)
	}
	return nil
}

const workColumns = `id, project, title, detail, kind, state, runner, session, ref, cwd,
	created, updated, parent, heading, claim, impact`

func nullStr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func scanWork(s rowScanner) (core.Work, error) {
	var w core.Work
	var kind, state string
	var runner, session, ref, cwd, parent, heading, claim, impact sql.NullString
	err := s.Scan(&w.ID, &w.Project, &w.Title, &w.Detail, &kind, &state,
		&runner, &session, &ref, &cwd, &w.Created, &w.Updated,
		&parent, &heading, &claim, &impact)
	if err != nil {
		return core.Work{}, err
	}
	if w.Kind, err = core.ParseWorkKind(kind); err != nil {
		return core.Work{}, fmt.Errorf("work %s: %w", w.ID, err)
	}
	if w.State, err = core.ParseState(state); err != nil {
		return core.Work{}, fmt.Errorf("work %s: %w", w.ID, err)
	}
	w.Runner = nullStr(runner)
	w.Session = nullStr(session)
	w.Ref = nullStr(ref)
	w.Cwd = nullStr(cwd)
	w.Parent = nullStr(parent)
	w.Heading = nullStr(heading)
	w.Claim = nullStr(claim)
	w.Impact = nullStr(impact)
	return w, nil
}

func scanEvent(s rowScanner) (core.Event, error) {
	var e core.Event
	var kind string
	err := s.Scan(&e.ID, &e.Work, &kind, &e.Body, &e.At)
	if err != nil {
		return core.Event{}, err
	}
	if e.Kind, err = core.ParseEventKind(kind); err != nil {
		return core.Event{}, fmt.Errorf("event %d: %w", e.ID, err)
	}
	return e, nil
}

func scanFeedback(s rowScanner) (core.Feedback, error) {
	var f core.Feedback
	var source string
	var project, card sql.NullString
	err := s.Scan(&f.ID, &f.Text, &project, &card, &source, &f.At)
	if err != nil {
		return core.Feedback{}, err
	}
	f.Project = nullStr(project)
	f.Card = nullStr(card)
	if f.Source, err = core.ParseFeedbackSource(source); err != nil {
		return core.Feedback{}, fmt.Errorf("feedback %d: %w", f.ID, err)
	}
	return f, nil
}

func scanConcern(s rowScanner) (core.Concern, error) {
	var c core.Concern
	var decision, resolvedAt sql.NullString
	if err := s.Scan(&c.ID, &c.Work, &c.Text, &c.Resolved, &decision, &c.At, &resolvedAt); err != nil {
		return core.Concern{}, err
	}
	c.Decision = nullStr(decision)
	c.ResolvedAt = nullStr(resolvedAt)
	return c, nil
}

func scanWorktree(s rowScanner) (core.Worktree, error) {
	var w core.Worktree
	var kind, state string
	var branch sql.NullString
	err := s.Scan(&w.ID, &w.Work, &w.Path, &branch, &kind, &state, &w.Created)
	if err != nil {
		return core.Worktree{}, err
	}
	w.Branch = nullStr(branch)
	if w.Kind, err = core.ParseWorktreeKind(kind); err != nil {
		return core.Worktree{}, fmt.Errorf("worktree %d: %w", w.ID, err)
	}
	if w.State, err = core.ParseWorktreeState(state); err != nil {
		return core.Worktree{}, fmt.Errorf("worktree %d: %w", w.ID, err)
	}
	return w, nil
}

type AddOptions struct {
	Kind    core.WorkKind
	Detail  string
	Parent  *string
	Heading *string
}

func (l *Ledger) Add(project, title string, opts AddOptions) (core.Work, error) {
	kind := opts.Kind
	if kind == "" {
		kind = core.WorkTask
	}
	var parent *string
	if opts.Parent != nil {
		p := *opts.Parent
		if core.IsEpic(kind) {
			return core.Work{}, fmt.Errorf("an epic cannot sit under another work item")
		}
		pw, err := l.Get(p)
		if err != nil {
			return core.Work{}, err
		}
		if !core.IsEpic(pw.Kind) {
			return core.Work{}, fmt.Errorf("parent %s is not an epic", p)
		}
		parent = &p
	}
	id, err := newId()
	if err != nil {
		return core.Work{}, err
	}
	t := now()
	err = l.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO work (id, project, title, detail, kind, state, parent, heading, created, updated)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, project, title, opts.Detail, string(kind), string(core.StateQueued), parent, opts.Heading, t, t); err != nil {
			return err
		}
		_, err := addEvent(tx, id, core.EventState, string(core.StateQueued), t)
		return err
	})
	if err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

func (l *Ledger) Get(id string) (core.Work, error) { return getWork(l.db, id) }

func getWork(q queryer, id string) (core.Work, error) {
	w, err := scanWork(q.QueryRow(`SELECT `+workColumns+` FROM work WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Work{}, noWork(id)
	}
	if err != nil {
		return core.Work{}, err
	}
	return w, nil
}

func (l *Ledger) Has(id string) (bool, error) {
	var n int
	if err := l.db.QueryRow(`SELECT COUNT(*) FROM work WHERE id = ?`, id).Scan(&n); err != nil {
		return false, err
	}
	return n == 1, nil
}

type ListFilter struct {
	Project *string
	States  []core.State
}

func (l *Ledger) List(filter ListFilter) ([]core.Work, error) {
	rows, err := l.db.Query(`SELECT ` + workColumns + ` FROM work ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Work{}
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return nil, err
		}
		if filter.Project != nil && w.Project != *filter.Project {
			continue
		}
		if filter.States != nil && !slices.Contains(filter.States, w.State) {
			continue
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Children of an epic, oldest first.
func (l *Ledger) Tasks(epicID string) ([]core.Work, error) {
	rows, err := l.db.Query(`SELECT `+workColumns+` FROM work WHERE parent = ? ORDER BY created`, epicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Work{}
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Transition moves work along core.Transitions. Soft-done is reached only
// through SoftDone, which checks readiness first.
func (l *Ledger) Transition(id string, to core.State) (core.Work, error) {
	if to == core.StateSoftDone {
		return core.Work{}, fmt.Errorf("work %s: soft-done is reached only through SoftDone, which checks readiness", id)
	}
	return l.transition(id, to)
}

func (l *Ledger) transition(id string, to core.State) (core.Work, error) {
	err := l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(core.Transitions[w.State], to) {
			return core.IllegalTransition{From: w.State, To: to}
		}
		if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(to), id); err != nil {
			return err
		}
		_, err = addEvent(tx, id, core.EventState, string(to), now())
		return err
	})
	if err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

type SessionInfo struct {
	Runner  string
	Session string
	Ref     *string
	Cwd     string
}

func (l *Ledger) SetSession(id string, s SessionInfo) error {
	return updateWork(l.db, id, `UPDATE work SET runner = ?, session = ?, ref = ?, cwd = ?, updated = ? WHERE id = ?`,
		s.Runner, s.Session, s.Ref, s.Cwd, now(), id)
}

func (l *Ledger) SetRef(id string, ref *string) error {
	return updateWork(l.db, id, `UPDATE work SET ref = ?, updated = ? WHERE id = ?`, ref, now(), id)
}

func (l *Ledger) SetCwd(id, cwd string) error {
	return updateWork(l.db, id, `UPDATE work SET cwd = ?, updated = ? WHERE id = ?`, cwd, now(), id)
}

func (l *Ledger) SetClaim(id string, claim *string) (core.Work, error) {
	if err := updateWork(l.db, id, `UPDATE work SET claim = ?, updated = ? WHERE id = ?`, claim, now(), id); err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

func (l *Ledger) SetImpact(id string, paths []string) (core.Work, error) {
	impact := strings.Join(paths, "\n")
	if err := updateWork(l.db, id, `UPDATE work SET impact = ?, updated = ? WHERE id = ?`, impact, now(), id); err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

// addEvent records the event and moves the work's updated to it: updated is
// the work's last activity, which staleness is measured from.
func addEvent(tx *sql.Tx, work string, kind core.EventKind, body, at string) (core.Event, error) {
	if err := updateWork(tx, work, `UPDATE work SET updated = ? WHERE id = ?`, at, work); err != nil {
		return core.Event{}, err
	}
	return scanEvent(tx.QueryRow(`INSERT INTO event (work, kind, body, at) VALUES (?, ?, ?, ?)
		RETURNING id, work, kind, body, at`, work, string(kind), body, at))
}

// AddEvent records an event on work and returns it as stored.
func (l *Ledger) AddEvent(work string, kind core.EventKind, body string) (core.Event, error) {
	var e core.Event
	err := l.inTx(func(tx *sql.Tx) error {
		var err error
		e, err = addEvent(tx, work, kind, body, now())
		return err
	})
	if err != nil {
		return core.Event{}, err
	}
	return e, nil
}

// File records an event read from a transcript together with the point it
// was read at, so the same entry is recognised as filed.
func (l *Ledger) File(work string, kind core.EventKind, body string, at core.TranscriptMark) error {
	return l.inTx(func(tx *sql.Tx) error {
		if _, err := addEvent(tx, work, kind, body, now()); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO filed (work, session, entries) VALUES (?, ?, ?)
			ON CONFLICT (work) DO UPDATE SET session = excluded.session, entries = excluded.entries`,
			work, at.Session, at.Entries)
		return err
	})
}

// Filed is the transcript point work's latest event was filed from; ok is
// false when nothing was filed from a transcript.
func (l *Ledger) Filed(work string) (at core.TranscriptMark, ok bool, err error) {
	err = l.db.QueryRow(`SELECT session, entries FROM filed WHERE work = ?`, work).Scan(&at.Session, &at.Entries)
	if errors.Is(err, sql.ErrNoRows) {
		return core.TranscriptMark{}, false, nil
	}
	if err != nil {
		return core.TranscriptMark{}, false, err
	}
	return at, true, nil
}

func (l *Ledger) Events(work string, kind *core.EventKind) ([]core.Event, error) {
	rows, err := l.db.Query(`SELECT id, work, kind, body, at FROM event WHERE work = ? ORDER BY id`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		if kind != nil && e.Kind != *kind {
			continue
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventsAfter returns every event, across all work, with an id above after,
// in id order.
func (l *Ledger) EventsAfter(after int) ([]core.Event, error) {
	rows, err := l.db.Query(`SELECT id, work, kind, body, at FROM event WHERE id > ? ORDER BY id`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LastEventID returns the highest event id, or 0 when there are no events.
func (l *Ledger) LastEventID() (int, error) {
	var id int
	err := l.db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM event`).Scan(&id)
	return id, err
}

func (l *Ledger) AddConcern(work, text string) (core.Concern, error) {
	var c core.Concern
	err := l.inTx(func(tx *sql.Tx) error {
		if _, err := getWork(tx, work); err != nil {
			return err
		}
		var err error
		c, err = scanConcern(tx.QueryRow(
			`INSERT INTO concern (work, text, at) VALUES (?, ?, ?)
			 RETURNING id, work, text, resolved, decision, at, resolved_at`, work, text, now()))
		return err
	})
	if err != nil {
		return core.Concern{}, err
	}
	return c, nil
}

func (l *Ledger) ResolveConcern(id int, decision string) (core.Concern, error) {
	var c core.Concern
	err := l.inTx(func(tx *sql.Tx) error {
		at := now()
		var err error
		c, err = scanConcern(tx.QueryRow(
			`UPDATE concern SET resolved = 1, decision = ?, resolved_at = ? WHERE id = ? AND resolved = 0
			 RETURNING id, work, text, resolved, decision, at, resolved_at`, decision, at, id))
		if errors.Is(err, sql.ErrNoRows) {
			return unresolvable(tx, id)
		}
		if err != nil {
			return err
		}
		_, err = addEvent(tx, c.Work, core.EventNote, fmt.Sprintf("concern %d resolved: %s", id, decision), at)
		return err
	})
	if err != nil {
		return core.Concern{}, err
	}
	return c, nil
}

// unresolvable says why concern id matched no open concern.
func unresolvable(q queryer, id int) error {
	var resolved int
	err := q.QueryRow(`SELECT resolved FROM concern WHERE id = ?`, id).Scan(&resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no concern %d", id)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("concern %d already resolved", id)
}

func (l *Ledger) Concerns(work *string) ([]core.Concern, error) {
	var rows *sql.Rows
	var err error
	if work == nil {
		rows, err = l.db.Query(`SELECT id, work, text, resolved, decision, at, resolved_at FROM concern ORDER BY id`)
	} else {
		rows, err = l.db.Query(`SELECT id, work, text, resolved, decision, at, resolved_at FROM concern WHERE work = ? ORDER BY id`, *work)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Concern{}
	for rows.Next() {
		c, err := scanConcern(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (l *Ledger) OpenConcerns(epicID string) ([]core.Concern, error) {
	if _, err := l.Get(epicID); err != nil {
		return nil, err
	}
	tasks, err := l.Tasks(epicID)
	if err != nil {
		return nil, err
	}
	works := map[string]bool{epicID: true}
	for _, t := range tasks {
		works[t.ID] = true
	}
	all, err := l.Concerns(nil)
	if err != nil {
		return nil, err
	}
	out := []core.Concern{}
	for _, c := range all {
		if c.Resolved == 0 && works[c.Work] {
			out = append(out, c)
		}
	}
	return out, nil
}

type WorktreeInfo struct {
	Path   string
	Branch *string
	Kind   core.WorktreeKind
}

func (l *Ledger) AddWorktree(work string, w WorktreeInfo) (core.Worktree, error) {
	var wt core.Worktree
	err := l.inTx(func(tx *sql.Tx) error {
		if _, err := getWork(tx, work); err != nil {
			return err
		}
		var err error
		wt, err = scanWorktree(tx.QueryRow(
			`INSERT INTO worktree (work, path, branch, kind, state, created) VALUES (?, ?, ?, ?, ?, ?)
			 RETURNING id, work, path, branch, kind, state, created`,
			work, w.Path, w.Branch, string(w.Kind), string(core.WorktreeActive), now()))
		return err
	})
	if err != nil {
		return core.Worktree{}, err
	}
	return wt, nil
}

func (l *Ledger) Worktrees(work string) ([]core.Worktree, error) {
	rows, err := l.db.Query(`SELECT id, work, path, branch, kind, state, created FROM worktree WHERE work = ? ORDER BY id`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Worktree{}
	for rows.Next() {
		w, err := scanWorktree(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (l *Ledger) SetWorktreeState(id int, state core.WorktreeState) (core.Worktree, error) {
	wt, err := scanWorktree(l.db.QueryRow(
		`UPDATE worktree SET state = ? WHERE id = ?
		 RETURNING id, work, path, branch, kind, state, created`, string(state), id))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Worktree{}, fmt.Errorf("no worktree %d", id)
	}
	if err != nil {
		return core.Worktree{}, err
	}
	return wt, nil
}

// Concurrently claimed tasks of an epic whose impact paths overlap.
func (l *Ledger) Conflicts(epicID string) ([]core.Conflict, error) {
	tasks, err := l.Tasks(epicID)
	if err != nil {
		return nil, err
	}
	var claimed []core.Work
	for _, t := range tasks {
		if t.Claim != nil && t.State != core.StateDone && t.State != core.StateDropped {
			claimed = append(claimed, t)
		}
	}
	out := []core.Conflict{}
	for i := 0; i < len(claimed); i++ {
		a := claimed[i]
		pa := core.SplitImpact(a.Impact)
		for j := i + 1; j < len(claimed); j++ {
			b := claimed[j]
			var overlapping []string
			for _, x := range pa {
				for _, y := range core.SplitImpact(b.Impact) {
					if core.PathsOverlap(x, y) {
						overlapping = append(overlapping, x)
						break
					}
				}
			}
			if len(overlapping) > 0 {
				out = append(out, core.Conflict{A: a.ID, B: b.ID, Paths: overlapping})
			}
		}
	}
	return out, nil
}

// review → soft-done. Epic: needs every task done/dropped, DONE report, passing
// verify, PR. Task: needs only the DONE report. Standalone: DONE + verify, and
// PR when code changed.
func (l *Ledger) SoftDone(id string, codeChanged bool) (core.Work, error) {
	w, err := l.Get(id)
	if err != nil {
		return core.Work{}, err
	}
	evs, err := l.Events(id, nil)
	if err != nil {
		return core.Work{}, err
	}
	last := func(k core.EventKind) *core.Event {
		for i := len(evs) - 1; i >= 0; i-- {
			if evs[i].Kind == k {
				return &evs[i]
			}
		}
		return nil
	}
	var missing []string
	if core.IsEpic(w.Kind) {
		open, err := l.Tasks(id)
		if err != nil {
			return core.Work{}, err
		}
		var n int
		for _, t := range open {
			if t.State != core.StateDone && t.State != core.StateDropped {
				n++
			}
		}
		if n > 0 {
			missing = append(missing, fmt.Sprintf("%d task(s) not done", n))
		}
		if r := last(core.EventReport); r == nil || !strings.HasPrefix(r.Body, "DONE") {
			missing = append(missing, "DONE report")
		}
		if v := last(core.EventVerify); v == nil || !strings.HasPrefix(v.Body, "pass") {
			missing = append(missing, "passing verify")
		}
		if last(core.EventPr) == nil {
			missing = append(missing, "pull request")
		}
	} else if w.Parent != nil {
		if r := last(core.EventReport); r == nil || !strings.HasPrefix(r.Body, "DONE") {
			missing = append(missing, "DONE report")
		}
	} else {
		if r := last(core.EventReport); r == nil || !strings.HasPrefix(r.Body, "DONE") {
			missing = append(missing, "DONE report")
		}
		if v := last(core.EventVerify); v == nil || !strings.HasPrefix(v.Body, "pass") {
			missing = append(missing, "passing verify")
		}
		if codeChanged && last(core.EventPr) == nil {
			missing = append(missing, "pull request")
		}
	}
	if len(missing) > 0 {
		return core.Work{}, core.NotReady{Missing: missing}
	}
	return l.transition(id, core.StateSoftDone)
}

type FeedbackOptions struct {
	Project *string
	Card    *string
	Source  core.FeedbackSource
}

func (l *Ledger) AddFeedback(text string, o FeedbackOptions) (core.Feedback, error) {
	source := o.Source
	if source == "" {
		source = core.FeedbackDirector
	}
	f, err := scanFeedback(l.db.QueryRow(
		`INSERT INTO feedback (text, project, card, source, at) VALUES (?, ?, ?, ?, ?)
		 RETURNING id, text, project, card, source, at`,
		text, o.Project, o.Card, string(source), now()))
	if err != nil {
		return core.Feedback{}, fmt.Errorf("feedback insert failed: %w", err)
	}
	return f, nil
}

func (l *Ledger) Feedback() ([]core.Feedback, error) {
	rows, err := l.db.Query(`SELECT id, text, project, card, source, at FROM feedback ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Feedback{}
	for rows.Next() {
		f, err := scanFeedback(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Feedback grouped by card (or project when no card) with two or more occurrences.
func (l *Ledger) Distill() ([]core.Candidate, error) {
	all, err := l.Feedback()
	if err != nil {
		return nil, err
	}
	groups := map[string][]string{}
	var order []string
	for _, f := range all {
		var key string
		switch {
		case f.Card != nil:
			key = "card:" + *f.Card
		case f.Project != nil:
			key = "project:" + *f.Project
		default:
			key = "global"
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], f.Text)
	}
	out := []core.Candidate{}
	for _, key := range order {
		if texts := groups[key]; len(texts) >= 2 {
			out = append(out, core.Candidate{Key: key, Count: len(texts), Texts: texts})
		}
	}
	return out, nil
}
