// Package ledger is the sqlite-backed store of the director's truth: work
// items, their events, feedback, concerns and worktrees. Rows are parsed into core domain types at this boundary and never
// cast afterwards.
package ledger

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"wd/internal/core"
)

const schema = `
CREATE TABLE IF NOT EXISTS work (id TEXT PRIMARY KEY, project TEXT NOT NULL, title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL, state TEXT NOT NULL, runner TEXT, session TEXT, ref TEXT, cwd TEXT, created TEXT NOT NULL, updated TEXT NOT NULL,
  parent TEXT, heading TEXT, claim TEXT, impact TEXT, goal_type TEXT, archived TEXT);
CREATE TABLE IF NOT EXISTS event (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL, effective TEXT, payload TEXT);
CREATE TABLE IF NOT EXISTS feedback (id INTEGER PRIMARY KEY, text TEXT NOT NULL, project TEXT, card TEXT, source TEXT NOT NULL, at TEXT NOT NULL, work TEXT REFERENCES work(id));
CREATE TABLE IF NOT EXISTS concern (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), text TEXT NOT NULL, resolved INTEGER NOT NULL DEFAULT 0, decision TEXT, at TEXT NOT NULL, resolved_at TEXT);
CREATE TABLE IF NOT EXISTS worktree (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), path TEXT NOT NULL, branch TEXT, kind TEXT NOT NULL, state TEXT NOT NULL, created TEXT NOT NULL, origin TEXT NOT NULL DEFAULT 'attached');
CREATE TABLE IF NOT EXISTS filed (work TEXT PRIMARY KEY REFERENCES work(id), session TEXT NOT NULL, entries INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS cursor (project TEXT NOT NULL, name TEXT NOT NULL, value INTEGER NOT NULL, PRIMARY KEY (project, name));
CREATE UNIQUE INDEX IF NOT EXISTS worktree_one_active_shared ON worktree(work) WHERE kind = 'shared' AND state = 'active';
CREATE TABLE IF NOT EXISTS message (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), text TEXT NOT NULL, at TEXT NOT NULL, delivered TEXT);
CREATE TABLE IF NOT EXISTS workspace (id TEXT PRIMARY KEY, name TEXT NOT NULL, path TEXT NOT NULL UNIQUE, creates INTEGER NOT NULL DEFAULT 0, created TEXT NOT NULL, updated TEXT NOT NULL);
`

// Additive migration for ledgers created before parents, worktree origins,
// goal types, structured decisions or the epic rename existed. A worktree row
// with no recorded origin reads as attached, so wd never removes a directory it
// cannot prove it made. A work row whose kind reads as epic is goal under its
// old name: the rename happens in Go, not by rewriting rows, so a ledger
// written before the rename still opens and still says what it meant.
var addedColumns = []struct{ table, col, decl string }{
	{"work", "parent", "TEXT"}, {"work", "heading", "TEXT"}, {"work", "claim", "TEXT"}, {"work", "impact", "TEXT"},
	{"work", "goal_type", "TEXT"}, {"work", "archived", "TEXT"},
	{"worktree", "origin", "TEXT NOT NULL DEFAULT 'attached'"},
	// effective is null on an event whose effective moment is its recorded
	// moment, and payload is null on an event with no structured claim. Both
	// are absent rather than defaulted so a reader can tell "not separated"
	// from "recorded the same way".
	{"event", "effective", "TEXT"}, {"event", "payload", "TEXT"},
	// work on feedback is null on a note somebody typed by hand. Attached
	// evidence came out of a session and knows the work it came from, because
	// evidence with no pointer back to the thing that produced it cannot be
	// audited against it — and a card promoted on that evidence records its
	// decision on that work, so the two have to meet somewhere.
	{"feedback", "work", "TEXT REFERENCES work(id)"},
}

// workspaceSchema is the additive statement for ledgers created before
// workspaces existed. It is a whole table rather than columns in addedColumns
// because a PRIMARY KEY cannot be added with ALTER TABLE ADD COLUMN, and the
// uniqueness on path is the whole point: one directory is one workspace.
const workspaceSchema = `CREATE TABLE IF NOT EXISTS workspace (id TEXT PRIMARY KEY, name TEXT NOT NULL, path TEXT NOT NULL UNIQUE, creates INTEGER NOT NULL DEFAULT 0, created TEXT NOT NULL, updated TEXT NOT NULL);`

type Ledger struct {
	db   *sql.DB
	path string
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
	if _, err := db.Exec(workspaceSchema); err != nil {
		return err
	}
	for _, c := range addedColumns {
		cols, err := columns(db, c.table)
		if err != nil {
			return err
		}
		if !slices.Contains(cols, c.col) {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.col, c.decl)); err != nil {
				return err
			}
		}
	}
	return nil
}

func columns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
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
	created, updated, parent, heading, claim, impact, goal_type, archived`

const eventColumns = `id, work, kind, body, at, effective, payload`

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
	var goalType, archived sql.NullString
	err := s.Scan(&w.ID, &w.Project, &w.Title, &w.Detail, &kind, &state,
		&runner, &session, &ref, &cwd, &w.Created, &w.Updated,
		&parent, &heading, &claim, &impact, &goalType, &archived)
	if err != nil {
		return core.Work{}, err
	}
	// An old row's kind reads as epic, which is goal under its former name:
	// the rename lives here rather than in a row rewrite, so a ledger written
	// before it still opens and still means what it meant.
	if kind == string(core.WorkEpic) {
		kind = string(core.WorkGoal)
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
	if goalType.Valid {
		gt, err := core.ParseGoalType(goalType.String)
		if err != nil {
			return core.Work{}, fmt.Errorf("work %s: %w", w.ID, err)
		}
		w.GoalType = &gt
	}
	w.Archived = nullStr(archived)
	return w, nil
}

func scanEvent(s rowScanner) (core.Event, error) {
	var e core.Event
	var kind string
	var effective, payload sql.NullString
	err := s.Scan(&e.ID, &e.Work, &kind, &e.Body, &e.At, &effective, &payload)
	if err != nil {
		return core.Event{}, err
	}
	if e.Kind, err = core.ParseEventKind(kind); err != nil {
		return core.Event{}, fmt.Errorf("event %d: %w", e.ID, err)
	}
	e.Effective = nullStr(effective)
	if payload.Valid && payload.String != "" {
		var d core.Decision
		if err := json.Unmarshal([]byte(payload.String), &d); err != nil {
			return core.Event{}, fmt.Errorf("event %d: decision payload: %w", e.ID, err)
		}
		e.Decision = &d
	}
	return e, nil
}

func scanFeedback(s rowScanner) (core.Feedback, error) {
	var f core.Feedback
	var source string
	var project, card, work sql.NullString
	err := s.Scan(&f.ID, &f.Text, &project, &card, &source, &f.At, &work)
	if err != nil {
		return core.Feedback{}, err
	}
	f.Project = nullStr(project)
	f.Card = nullStr(card)
	f.Work = nullStr(work)
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
	var kind, state, origin string
	var branch sql.NullString
	err := s.Scan(&w.ID, &w.Work, &w.Path, &branch, &kind, &state, &origin, &w.Created)
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
	if w.Origin, err = core.ParseWorktreeOrigin(origin); err != nil {
		return core.Worktree{}, fmt.Errorf("worktree %d: %w", w.ID, err)
	}
	return w, nil
}

type AddOptions struct {
	Kind     core.WorkKind
	Detail   string
	Parent   *string
	Heading  *string
	GoalType *core.GoalType
}

// The two levels that organise work: a roadmap holds items, a goal holds
// tasks. Anything else refuses a parent, so the shape of the ledger cannot
// drift into something no reader could draw.
func childKind(parentKind, childKind core.WorkKind) error {
	switch {
	case parentKind == core.WorkRoadmap:
		if childKind != core.WorkItem {
			return fmt.Errorf("a roadmap holds items, not %s", childKind)
		}
		return nil
	case core.IsGoal(parentKind):
		if childKind != core.WorkTask {
			return fmt.Errorf("a goal holds tasks, not %s", childKind)
		}
		return nil
	default:
		return fmt.Errorf("work of kind %s holds no children", parentKind)
	}
}

func (l *Ledger) Add(project, title string, opts AddOptions) (core.Work, error) {
	kind := opts.Kind
	if kind == "" {
		kind = core.WorkTask
	}
	if kind == core.WorkEpic {
		// epic was goal's old name. A write always uses the current one; an old
		// row still reads as goal, so nothing needs rewriting to migrate.
		kind = core.WorkGoal
	}
	var parent *string
	if opts.Parent != nil {
		p := *opts.Parent
		// A goal or a roadmap is a level, not a child: checked before the
		// parent's own rule so the refusal says what is wrong rather than what
		// the parent happened to hold.
		if kind == core.WorkRoadmap || core.IsGoal(kind) {
			return core.Work{}, fmt.Errorf("a goal or roadmap cannot sit under another work item")
		}
		pw, err := l.Get(p)
		if err != nil {
			return core.Work{}, err
		}
		if err := childKind(pw.Kind, kind); err != nil {
			return core.Work{}, fmt.Errorf("under %s: %w", p, err)
		}
		// A task under a goal that has come to rest would sit there with nobody
		// to run it: the goal is not open, so the coordinator never drives it,
		// and the rollup would show an open task under finished work. Reopening
		// first is what puts the goal back in the loop, and it costs one command.
		if pw.State == core.StateDone {
			return core.Work{}, fmt.Errorf("%s is done; work on it again reopens it first: wd reopen %s \"<what is being worked on>\"", p, p)
		}
		parent = &p
	} else if kind == core.WorkItem {
		return core.Work{}, fmt.Errorf("a roadmap item belongs to a roadmap")
	}
	if opts.GoalType != nil && !core.IsGoal(kind) {
		return core.Work{}, fmt.Errorf("a goal type applies to a goal, not %s", kind)
	}
	id, err := newId()
	if err != nil {
		return core.Work{}, err
	}
	t := now()
	err = l.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO work (id, project, title, detail, kind, state, parent, heading, created, updated, goal_type)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, project, title, opts.Detail, string(kind), string(core.StateQueued), parent, opts.Heading, t, t, goalTypeArg(opts.GoalType)); err != nil {
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

func goalTypeArg(gt *core.GoalType) any {
	if gt == nil {
		return nil
	}
	return string(*gt)
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
	Kinds   []core.WorkKind
	// Archived unset lists all work; false leaves archived work out; true lists
	// only archived work.
	Archived *bool
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
		if filter.Kinds != nil && !slices.Contains(filter.Kinds, w.Kind) {
			continue
		}
		if filter.Archived != nil && *filter.Archived != (w.Archived != nil) {
			continue
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Children of a goal, oldest first.
func (l *Ledger) Tasks(goalID string) ([]core.Work, error) {
	return l.children(goalID)
}

// RoadmapItems of a roadmap, oldest first: the items still waiting, and the
// goals already translated out of them, in the order they were filed.
func (l *Ledger) Items(roadmapID string) ([]core.Work, error) {
	return l.children(roadmapID)
}

func (l *Ledger) children(parentID string) ([]core.Work, error) {
	rows, err := l.db.Query(`SELECT `+workColumns+` FROM work WHERE parent = ? ORDER BY created`, parentID)
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
// Promote translates a roadmap item into a goal. It is the same row, not a
// copy: the id an item was filed under is the id its goal keeps, so a
// conversation, a decision or an event recorded against the item before it was
// committed stays attached to the goal it became.
func (l *Ledger) Promote(itemID string, gt *core.GoalType) (core.Work, error) {
	err := l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, itemID)
		if err != nil {
			return err
		}
		if !core.IsRoadmapItem(w.Kind) {
			return fmt.Errorf("work %s is a %s, not a roadmap item", itemID, w.Kind)
		}
		if gt != nil {
			if _, err := tx.Exec(`UPDATE work SET kind = ?, goal_type = ? WHERE id = ?`,
				string(core.WorkGoal), string(*gt), itemID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(`UPDATE work SET kind = ? WHERE id = ?`, string(core.WorkGoal), itemID); err != nil {
			return err
		}
		_, err = addEvent(tx, itemID, core.EventState, "promoted to goal", now())
		return err
	})
	if err != nil {
		return core.Work{}, err
	}
	return l.Get(itemID)
}

// SetGoalType classifies a goal as a question answered or work done, and
// records the decision. An unset type stays unset: a wrong type is a lie the
// reader cannot see, so nothing is defaulted.
func (l *Ledger) SetGoalType(id string, gt core.GoalType) (core.Work, error) {
	if _, err := core.ParseGoalType(string(gt)); err != nil {
		return core.Work{}, err
	}
	err := l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, id)
		if err != nil {
			return err
		}
		if !core.IsGoal(w.Kind) {
			return fmt.Errorf("work %s is a %s, not a goal", id, w.Kind)
		}
		if _, err := tx.Exec(`UPDATE work SET goal_type = ? WHERE id = ?`, string(gt), id); err != nil {
			return err
		}
		_, err = addEvent(tx, id, core.EventDecision, "classified "+string(gt), now())
		return err
	})
	if err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

// Abandon records that work stopped without shipping and why. The reason is
// one the ledger can verify — no PR was ever raised, or one was raised and
// never merged — because a reason nobody can check is not a reason.
func (l *Ledger) Abandon(id, reason, detail string) (core.Work, error) {
	if _, err := core.ParseAbandonReason(reason); err != nil {
		return core.Work{}, err
	}
	body := reason
	if detail != "" {
		body = reason + ": " + detail
	}
	err := l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(core.Transitions[w.State], core.StateAbandoned) {
			return core.IllegalTransition{From: w.State, To: core.StateAbandoned}
		}
		if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(core.StateAbandoned), id); err != nil {
			return err
		}
		if _, err := addEvent(tx, id, core.EventAbandon, body, now()); err != nil {
			return err
		}
		_, err = addEvent(tx, id, core.EventState, string(core.StateAbandoned), now())
		return err
	})
	if err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

// Reopen returns finished work to life and records what is being worked on.
//
// The reason is required, and that is the whole difference between this and a
// question. Someone asking a finished goal what it decided, or what is still
// open, has not asked for more work — the answer is in the ledger and asking
// costs nothing. Someone working more on top of a finished goal has, and the
// ledger has no way to tell the two apart from a state change alone: both look
// like a person arriving at a goal that says done. So the arrival has to say
// which it is, and a reopen with no stated work is refused rather than allowed
// to pass for a question.
//
// The work goes to running rather than queued, because reopening means someone
// is on it now: queued work is waiting to be picked up, and this was not.
func (l *Ledger) Reopen(id, why string) (core.Work, error) {
	if why == "" {
		return core.Work{}, fmt.Errorf("work %s: reopening says what is being worked on: wd reopen %s \"<what>\"; "+
			"to ask a finished goal something, read it — wd context %s, wd events %s", id, id, id, id)
	}
	err := l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, id)
		if err != nil {
			return err
		}
		if !core.Reopenable(w.State) {
			return fmt.Errorf("work %s is %s, and only done work is reopened: %s", id, w.State, core.IllegalTransition{From: w.State, To: core.StateRunning})
		}
		if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(core.StateRunning), id); err != nil {
			return err
		}
		// A decision, not a bare state event: reopening claims something is
		// being worked on, and a claim is what a review reads and a reversal
		// undoes. The first run's events stay exactly where they were, which is
		// what makes a reopened goal's second history legible against the first.
		_, err = addClaim(tx, id, core.EventDecision, "reopened: "+why, now(), nil, &core.Decision{
			Question: "what is being worked on in " + id + " now it is done?",
			Answer:   why,
		})
		return err
	})
	if err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

// Release moves work that came to rest abandoned into dropped: work that was
// filed as a failure and turns out to have been a choice. A duplicate whose
// work landed under another id, a probe that was never meant to ship — both were
// written down as "stopped without shipping", and that sentence is false of them.
//
// The reason is required for the same reason Reopen's is: this reclassifies a
// failure as a choice, and the only thing standing between the two is what the
// person filing it says. So it is said, and it is filed as a decision rather than
// an edit. The abandon event stays exactly where it was, because it did happen —
// work that never shipped — and a reader sees the failure and then the correction
// that the failure was the wrong word for it. History is not rewritten; the row
// simply stops claiming the work stopped.
//
// Releasing is the only way out of abandoned. It does not lead back to running:
// work that never shipped does not become work in progress by being relabelled.
func (l *Ledger) Release(id, why string) (core.Work, error) {
	if why == "" {
		return core.Work{}, fmt.Errorf("work %s: releasing says why the stop was a choice: wd release %s \"<what was true instead>\"", id, id)
	}
	err := l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, id)
		if err != nil {
			return err
		}
		if w.State != core.StateAbandoned {
			return fmt.Errorf("work %s is %s, and only abandoned work is released: %s", id, w.State, core.IllegalTransition{From: w.State, To: core.StateDropped})
		}
		if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(core.StateDropped), id); err != nil {
			return err
		}
		_, err = addClaim(tx, id, core.EventDecision, "released: "+why, now(), nil, &core.Decision{
			Question: "was the stop recorded against " + id + " a failure or a choice?",
			Answer:   why,
		})
		return err
	})
	if err != nil {
		return core.Work{}, err
	}
	return l.Get(id)
}

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
		// Reopening is a claim with a reason attached, so the machine has the
		// edge but this refuses to take it: done → running is reachable only
		// through Reopen, so no caller can bring a finished goal back to life
		// without saying what for. Checked here, where the row is already read.
		if to == core.StateRunning && core.Reopenable(w.State) {
			return fmt.Errorf("work %s is done; work on it again goes through Reopen, which says what is being worked on: wd reopen %s \"<what>\"", id, id)
		}
		// The same shape as reopening, and the same reason: abandoned → dropped
		// reclassifies a stop as a choice, so the machine has the edge and only
		// Release may take it, with the reason attached.
		if to == core.StateDropped && w.State == core.StateAbandoned {
			return fmt.Errorf("work %s is abandoned; saying the stop was a choice goes through Release: wd release %s \"<what was true instead>\"", id, id)
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

// Archive puts work and everything under it out of sight. Work at rest keeps
// its state; open work is dropped first, and dropping is a choice, so it takes
// a reason that is filed as a decision. It returns every work it archived.
func (l *Ledger) Archive(id, why string) ([]core.Work, error) {
	why = strings.TrimSpace(why)
	var tree []core.Work
	err := l.inTx(func(tx *sql.Tx) error {
		var err error
		if tree, err = subtree(tx, id); err != nil {
			return err
		}
		if tree[0].Archived != nil {
			return fmt.Errorf("work %s is already archived; wd unarchive %s brings it back", id, id)
		}
		var open []string
		for _, w := range tree {
			if !core.AtRest(w.State) {
				open = append(open, fmt.Sprintf("%s (%s)", w.ID, w.State))
			}
		}
		if len(open) > 0 && why == "" {
			return fmt.Errorf("archiving %s drops open work: %s; say why: wd archive %s \"<why>\"", id, strings.Join(open, ", "), id)
		}
		at := now()
		if _, err := dropOpen(tx, tree, why, at); err != nil {
			return err
		}
		for _, w := range tree {
			if _, err := tx.Exec(`UPDATE work SET archived = ? WHERE id = ?`, at, w.ID); err != nil {
				return err
			}
			if _, err := addEvent(tx, w.ID, core.EventNote, "archived", at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return l.reread(tree)
}

// Drop lets go of work and everything open under it, on purpose: each moves
// to dropped with why filed as a decision. Work at rest under it is left as it
// is. It returns the work it dropped.
func (l *Ledger) Drop(id, why string) ([]core.Work, error) {
	why = strings.TrimSpace(why)
	var dropped []core.Work
	err := l.inTx(func(tx *sql.Tx) error {
		tree, err := subtree(tx, id)
		if err != nil {
			return err
		}
		if core.AtRest(tree[0].State) {
			return fmt.Errorf("work %s is %s: it is already at rest", id, tree[0].State)
		}
		if why == "" {
			return fmt.Errorf("dropping %s needs a reason: say why", id)
		}
		dropped, err = dropOpen(tx, tree, why, now())
		return err
	})
	if err != nil {
		return nil, err
	}
	return l.reread(dropped)
}

// dropOpen moves every open work in tree to dropped, filing why as a decision
// on each, and returns them.
func dropOpen(tx *sql.Tx, tree []core.Work, why, at string) ([]core.Work, error) {
	var dropped []core.Work
	for _, w := range tree {
		if core.AtRest(w.State) {
			continue
		}
		if !slices.Contains(core.Transitions[w.State], core.StateDropped) {
			return nil, core.IllegalTransition{From: w.State, To: core.StateDropped}
		}
		if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(core.StateDropped), w.ID); err != nil {
			return nil, err
		}
		if _, err := addEvent(tx, w.ID, core.EventState, string(core.StateDropped), at); err != nil {
			return nil, err
		}
		if _, err := addEvent(tx, w.ID, core.EventDecision, "dropped: "+why, at); err != nil {
			return nil, err
		}
		dropped = append(dropped, w)
	}
	return dropped, nil
}

// QueueMessage stores something said to work, undelivered.
func (l *Ledger) QueueMessage(work, text string) (core.Message, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return core.Message{}, fmt.Errorf("a message to %s is empty", work)
	}
	var m core.Message
	err := l.inTx(func(tx *sql.Tx) error {
		if _, err := getWork(tx, work); err != nil {
			return err
		}
		at := now()
		res, err := tx.Exec(`INSERT INTO message (work, text, at) VALUES (?, ?, ?)`, work, text, at)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		m = core.Message{ID: int(id), Work: work, Text: text, At: at}
		return nil
	})
	return m, err
}

// Undelivered lists work's messages not yet delivered, oldest first.
func (l *Ledger) Undelivered(work string) ([]core.Message, error) {
	rows, err := l.db.Query(`SELECT id, work, text, at FROM message WHERE work = ? AND delivered IS NULL ORDER BY id`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Message{}
	for rows.Next() {
		var m core.Message
		if err := rows.Scan(&m.ID, &m.Work, &m.Text, &m.At); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkDelivered stamps a message delivered and files a sent event with its text.
func (l *Ledger) MarkDelivered(id int) error {
	return l.inTx(func(tx *sql.Tx) error {
		var work, text string
		var delivered sql.NullString
		err := tx.QueryRow(`SELECT work, text, delivered FROM message WHERE id = ?`, id).Scan(&work, &text, &delivered)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no message %d", id)
		}
		if err != nil {
			return err
		}
		if delivered.Valid {
			return fmt.Errorf("message %d was already delivered at %s", id, delivered.String)
		}
		at := now()
		if _, err := tx.Exec(`UPDATE message SET delivered = ? WHERE id = ?`, at, id); err != nil {
			return err
		}
		_, err = addEvent(tx, work, core.EventSent, text, at)
		return err
	})
}

// Unarchive brings work and everything under it back into sight, in the states
// it rested in. It returns every work it brought back.
func (l *Ledger) Unarchive(id string) ([]core.Work, error) {
	var tree []core.Work
	err := l.inTx(func(tx *sql.Tx) error {
		var err error
		if tree, err = subtree(tx, id); err != nil {
			return err
		}
		if tree[0].Archived == nil {
			return fmt.Errorf("work %s is not archived", id)
		}
		at := now()
		for _, w := range tree {
			if _, err := tx.Exec(`UPDATE work SET archived = NULL WHERE id = ?`, w.ID); err != nil {
				return err
			}
			if _, err := addEvent(tx, w.ID, core.EventNote, "unarchived", at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return l.reread(tree)
}

// Under returns work and everything under it, parents before children.
func (l *Ledger) Under(id string) ([]core.Work, error) {
	var tree []core.Work
	err := l.inTx(func(tx *sql.Tx) error {
		var err error
		tree, err = subtree(tx, id)
		return err
	})
	return tree, err
}

// subtree is a work item followed by everything under it, parents before
// children.
func subtree(tx *sql.Tx, id string) ([]core.Work, error) {
	root, err := getWork(tx, id)
	if err != nil {
		return nil, err
	}
	out := []core.Work{root}
	for i := 0; i < len(out); i++ {
		rows, err := tx.Query(`SELECT `+workColumns+` FROM work WHERE parent = ? ORDER BY created`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			w, err := scanWork(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, w)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (l *Ledger) reread(works []core.Work) ([]core.Work, error) {
	out := make([]core.Work, 0, len(works))
	for _, w := range works {
		fresh, err := l.Get(w.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, fresh)
	}
	return out, nil
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
	return addClaim(tx, work, kind, body, at, nil, nil)
}

// addClaim records an event that may carry a structured claim and an effective
// moment distinct from when it was written.
func addClaim(tx *sql.Tx, work string, kind core.EventKind, body, at string, effective *string, d *core.Decision) (core.Event, error) {
	if err := updateWork(tx, work, `UPDATE work SET updated = ? WHERE id = ?`, at, work); err != nil {
		return core.Event{}, err
	}
	return scanEvent(tx.QueryRow(`INSERT INTO event (work, kind, body, at, effective, payload) VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, work, kind, body, at, effective, payload`,
		work, string(kind), body, at, effective, marshalDecision(d)))
}

// marshalDecision renders a claim for storage, or nil when there is none: a
// decision recorded as one line of prose has no payload and says so.
func marshalDecision(d *core.Decision) any {
	if d == nil {
		return nil
	}
	// Round-tripping through the same struct the reader parses is what keeps a
	// stored claim and a read claim the same shape by construction.
	raw, err := json.Marshal(d)
	if err != nil {
		return nil
	}
	return string(raw)
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

// Decide records a decision with its parts and, optionally, the moment it took
// effect. The effective moment is separate from when it was written because
// the two genuinely differ: a model can answer a question now and the answer
// take hold later, and a review needs to know which date a claim is judged
// against.
func (l *Ledger) Decide(work, body string, d core.Decision, effective *string) (core.Event, error) {
	if d.Question == "" || d.Answer == "" {
		return core.Event{}, fmt.Errorf("a structured decision needs both a question and an answer")
	}
	var e core.Event
	err := l.inTx(func(tx *sql.Tx) error {
		var err error
		e, err = addClaim(tx, work, core.EventDecision, body, now(), effective, &d)
		return err
	})
	if err != nil {
		return core.Event{}, err
	}
	return e, nil
}

// Reverse records that a decision no longer stands, as a new decision naming
// the one it undoes. The original is left exactly as it was made, and is
// marked with what reversed it, so a reader sees both the claim and the
// correction in the order they happened. Editing history would be the other
// way round and would lose the claim entirely.
func (l *Ledger) Reverse(work string, target int, reason string) (core.Event, error) {
	if reason == "" {
		return core.Event{}, fmt.Errorf("a reversal says why: wd review --reverse <event-id> \"<reason>\"")
	}
	var e core.Event
	err := l.inTx(func(tx *sql.Tx) error {
		reversed, err := getEvent(tx, target)
		if err != nil {
			return err
		}
		if reversed.Kind != core.EventDecision {
			return fmt.Errorf("event %d is a %s, not a decision", target, reversed.Kind)
		}
		d := core.Decision{
			Question: "should decision " + strconv.Itoa(target) + " still stand?",
			Answer:   "no — " + reason,
			Reverses: target,
		}
		if reversed.Decision != nil {
			d.Source, d.Runner, d.Model, d.Tokens = reversed.Decision.Source, reversed.Decision.Runner, reversed.Decision.Model, reversed.Decision.Tokens
		}
		body := "reverses " + strconv.Itoa(target) + ": " + reason
		e, err = addClaim(tx, work, core.EventDecision, body, now(), nil, &d)
		if err != nil {
			return err
		}
		// Mark the original so a review reading only the old event still learns
		// it no longer stands. The payload is rewritten in place; the body, the
		// recorded time and the claim itself are not.
		if reversed.Decision == nil {
			reversed.Decision = &core.Decision{Question: reversed.Body, Answer: reversed.Body}
		}
		marked := *reversed.Decision
		marked.ReversedBy = e.ID
		_, err = tx.Exec(`UPDATE event SET payload = ? WHERE id = ?`, marshalDecision(&marked), target)
		return err
	})
	if err != nil {
		return core.Event{}, err
	}
	return e, nil
}

func getEvent(q queryer, id int) (core.Event, error) {
	e, err := scanEvent(q.QueryRow(`SELECT `+eventColumns+` FROM event WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Event{}, fmt.Errorf("no event %d; wd review --since 0 lists the whole ledger", id)
	}
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
	rows, err := l.db.Query(`SELECT `+eventColumns+` FROM event WHERE work = ? ORDER BY id`, work)
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

// EventsUnder returns the events of a work item and everything under it, in
// event order.
//
// A goal's story happens on its tasks. The goal's own row carries the spine —
// it was promoted, it went running, it will end — and every decision, landing
// and report in between is filed on the task that caused it, because that is
// what the row is for. So a goal read from its own row looks like a goal that
// decided nothing no matter how much it decided, and a goal read from its tasks
// looks like tasks with no parent. One set is the whole subtree, which is what
// both surfaces show on a goal and what keeps them showing the same thing.
//
// A task has nothing under it, so for a task this is its own events.
func (l *Ledger) EventsUnder(work string) ([]core.Event, error) {
	rows, err := l.db.Query(`SELECT `+eventColumns+` FROM event
		WHERE work = ? OR work IN (SELECT id FROM work WHERE parent = ?)
		ORDER BY id`, work, work)
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

// EventsAfter returns every event, across all work, with an id above after,
// in id order.
func (l *Ledger) EventsAfter(after int) ([]core.Event, error) {
	rows, err := l.db.Query(`SELECT `+eventColumns+` FROM event WHERE id > ? ORDER BY id`, after)
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

// Cursor is how far a per-project sweep has read. A periodic review is a diff
// rather than a dump because the cursor remembers where the last pass stopped,
// and acknowledging a pass moves it. A cursor that does not exist yet reads as
// 0, so the first pass sees everything rather than nothing.
func (l *Ledger) Cursor(project, name string) (int, error) {
	var v int
	err := l.db.QueryRow(`SELECT value FROM cursor WHERE project = ? AND name = ?`, project, name).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// SetCursor moves a per-project cursor forward. It refuses to move backwards:
// a cursor is what a sweep has seen, and a sweep that forgets is a sweep that
// shows the same thing twice and hides what changed since.
func (l *Ledger) SetCursor(project, name string, value int) error {
	return l.inTx(func(tx *sql.Tx) error {
		var cur int
		err := tx.QueryRow(`SELECT value FROM cursor WHERE project = ? AND name = ?`, project, name).Scan(&cur)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && value < cur {
			return fmt.Errorf("cursor %s/%s is at %d; refusing to move it back to %d", project, name, cur, value)
		}
		_, err = tx.Exec(`INSERT INTO cursor (project, name, value) VALUES (?, ?, ?)
			ON CONFLICT (project, name) DO UPDATE SET value = excluded.value`, project, name, value)
		return err
	})
}

// FeedbackAfter returns feedback rows with an id above after, oldest first. A
// review needs this so a taste card promoted since the last pass ships with
// the decisions made under it: a card that changed what the loop thinks is
// part of what the loop decided.
func (l *Ledger) FeedbackAfter(after int) ([]core.Feedback, error) {
	rows, err := l.db.Query(`SELECT id, text, project, card, source, at, work FROM feedback WHERE id > ? ORDER BY id`, after)
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

// LastFeedbackID returns the highest feedback id, or 0 when there is none.
func (l *Ledger) LastFeedbackID() (int, error) {
	var id int
	err := l.db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM feedback`).Scan(&id)
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
	Origin core.WorktreeOrigin
}

func (l *Ledger) AddWorktree(work string, w WorktreeInfo) (core.Worktree, error) {
	var wt core.Worktree
	err := l.inTx(func(tx *sql.Tx) error {
		if _, err := getWork(tx, work); err != nil {
			return err
		}
		var err error
		wt, err = scanWorktree(tx.QueryRow(
			`INSERT INTO worktree (work, path, branch, kind, state, origin, created) VALUES (?, ?, ?, ?, ?, ?, ?)
			 RETURNING id, work, path, branch, kind, state, origin, created`,
			work, w.Path, w.Branch, string(w.Kind), string(core.WorktreeActive), string(w.Origin), now()))
		return err
	})
	if err != nil {
		return core.Worktree{}, err
	}
	return wt, nil
}

func (l *Ledger) Worktrees(work string) ([]core.Worktree, error) {
	rows, err := l.db.Query(`SELECT id, work, path, branch, kind, state, origin, created FROM worktree WHERE work = ? ORDER BY id`, work)
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
		 RETURNING id, work, path, branch, kind, state, origin, created`, string(state), id))
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
	evs, err := l.CurrentRun(id)
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
	if core.IsGoal(w.Kind) {
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

// CurrentRun returns a work item's events since its most recent reopening, or
// all of them when it was never reopened. A reopened goal still holds the
// report, verify and landing that closed its first run; the events stay as
// history, but only this run's count as evidence of where the work stands now.
func (l *Ledger) CurrentRun(id string) ([]core.Event, error) {
	evs, err := l.Events(id, nil)
	if err != nil {
		return nil, err
	}
	if since := reopenedAt(evs); since > 0 {
		evs = slices.DeleteFunc(evs, func(e core.Event) bool { return e.ID <= since })
	}
	return evs, nil
}

// reopenedAt is the event id of the most recent reopening, or 0 when the work has
// never been reopened. Readiness is read from just after it.
func reopenedAt(evs []core.Event) int {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == core.EventDecision && strings.HasPrefix(evs[i].Body, "reopened: ") {
			return evs[i].ID
		}
	}
	return 0
}

type FeedbackOptions struct {
	Project *string
	Card    *string
	Source  core.FeedbackSource
	// Work is the piece of work the evidence came from, when it came from one.
	// A note typed by hand has none and says so rather than guessing.
	Work *string
}

func (l *Ledger) AddFeedback(text string, o FeedbackOptions) (core.Feedback, error) {
	source := o.Source
	if source == "" {
		source = core.FeedbackDirector
	}
	// The work is checked before the insert rather than left to the constraint,
	// because a bare constraint failure does not say which work was missing and
	// the person filing evidence is the one who has to fix it.
	if o.Work != nil {
		if _, err := l.Get(*o.Work); err != nil {
			return core.Feedback{}, noWork(*o.Work)
		}
	}
	f, err := scanFeedback(l.db.QueryRow(
		`INSERT INTO feedback (text, project, card, source, at, work) VALUES (?, ?, ?, ?, ?, ?)
		 RETURNING id, text, project, card, source, at, work`,
		text, o.Project, o.Card, string(source), now(), o.Work))
	if err != nil {
		return core.Feedback{}, fmt.Errorf("feedback insert failed: %w", err)
	}
	return f, nil
}

func (l *Ledger) Feedback() ([]core.Feedback, error) {
	rows, err := l.db.Query(`SELECT id, text, project, card, source, at, work FROM feedback ORDER BY id`)
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

// EventWork is the work an event belongs to, so a reversal can be filed against
// the same stream the claim it undoes is in.
func (l *Ledger) EventWork(eventID int) (string, error) {
	var work string
	err := l.db.QueryRow(`SELECT work FROM event WHERE id = ?`, eventID).Scan(&work)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("no event %d; wd events <work> lists ids", eventID)
	}
	return work, err
}

// Dir returns the directory containing the ledger database.
func (l *Ledger) Dir() string {
	if l.path == "" {
		return ""
	}
	return filepath.Dir(l.path)
}

// Workspaces lists the directories this director serves, parents first, so a
// client rendering a picker shows the place new work can go above the places it
// already is.
func (l *Ledger) Workspaces() ([]core.Workspace, error) {
	rows, err := l.db.Query(`SELECT id, name, path, creates, created, updated FROM workspace ORDER BY creates DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Workspace{}
	for rows.Next() {
		var w core.Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.Path, &w.Creates, &w.Created, &w.Updated); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Workspace returns one registered workspace by id, by path, or by name.
//
// The three are tried in that order because a person types a name and a client
// holds an id, and both must land on the same row. A name that matches more than
// one is refused rather than resolved to whichever sorted first: picking one
// silently is how work ends up attached to the wrong workspace, and this is the
// lookup that decides where a phone's instructions go.
func (l *Ledger) Workspace(idOrPathOrName string) (core.Workspace, error) {
	const cols = `id, name, path, creates, created, updated`
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`SELECT ` + cols + ` FROM workspace WHERE id = ?`, []any{idOrPathOrName}},
		{`SELECT ` + cols + ` FROM workspace WHERE path = ?`, []any{idOrPathOrName}},
		{`SELECT ` + cols + ` FROM workspace WHERE name = ? ORDER BY created`, []any{idOrPathOrName}},
	} {
		rows, err := l.db.Query(q.sql, q.args...)
		if err != nil {
			return core.Workspace{}, err
		}
		found := []core.Workspace{}
		for rows.Next() {
			var w core.Workspace
			if err := rows.Scan(&w.ID, &w.Name, &w.Path, &w.Creates, &w.Created, &w.Updated); err != nil {
				rows.Close()
				return core.Workspace{}, err
			}
			found = append(found, w)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return core.Workspace{}, err
		}
		if len(found) == 1 {
			return found[0], nil
		}
		if len(found) > 1 {
			return core.Workspace{}, fmt.Errorf("%s names %d workspaces (%s): name it by id or path", idOrPathOrName, len(found), joinIds(found))
		}
	}
	return core.Workspace{}, fmt.Errorf("no workspace is called %q: wd workspace list", idOrPathOrName)
}

// joinIds names the rows an ambiguous lookup matched, so the message can say
// which ones to choose between rather than just that there are several.
func joinIds(ws []core.Workspace) string {
	ids := make([]string, 0, len(ws))
	for _, w := range ws {
		ids = append(ids, w.ID)
	}
	return strings.Join(ids, ", ")
}

// AddWorkspace registers a directory this director will serve. The path is
// stored as given but resolved, so "~/work/foo" and "/Users/me/work/foo" are
// one workspace rather than two rows that disagree about what they point at.
func (l *Ledger) AddWorkspace(name, path string, creates bool) (core.Workspace, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return core.Workspace{}, err
	}
	id, err := newId()
	if err != nil {
		return core.Workspace{}, err
	}
	stamp := now()
	if _, err := l.db.Exec(`INSERT INTO workspace (id, name, path, creates, created, updated) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, abs, creates, stamp, stamp); err != nil {
		return core.Workspace{}, err
	}
	return l.Workspace(id)
}

// CreateWorkspacePath is where a new workspace would live under parent for the
// given name, and the only place one may ever be made.
//
// The name comes from a client — often a phone, on a different network, over a
// token — so the containment is checked rather than assumed. Constructing the
// path ourselves is not the same as it being inside the parent: "..",
// "a/../../b" and an absolute name all escape a naive join. So the joined path
// is cleaned and then required to still be under the parent, and the parent
// itself has to be one that was registered as a parent. A name that escapes is
// refused by name, the way every other refusal here names what would fix it.
func (l *Ledger) CreateWorkspacePath(parentID, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("a new workspace needs a name")
	}
	parent, err := l.Workspace(parentID)
	if err != nil {
		return "", err
	}
	if !parent.Creates {
		return "", fmt.Errorf("workspace %s is a workspace, not a parent: new ones are created under a parent — wd workspace add <path> --creates", parent.ID)
	}
	// A name is one directory, so anything that is not one segment is not a
	// name. This is checked before the join so the message names the real
	// problem rather than reporting an escape.
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || filepath.IsAbs(name) {
		return "", fmt.Errorf("%q is not a workspace name: it is one directory name, not a path", name)
	}
	child := filepath.Clean(filepath.Join(parent.Path, name))
	if !strings.HasPrefix(child, parent.Path+string(filepath.Separator)) {
		return "", fmt.Errorf("%q would land outside %s", name, parent.Path)
	}
	return child, nil
}
