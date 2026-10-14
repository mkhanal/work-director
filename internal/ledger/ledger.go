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
  parent TEXT, heading TEXT, claim TEXT, impact TEXT, goal_type TEXT, archived TEXT, done_when TEXT, finish_line TEXT, finish_source TEXT);
CREATE TABLE IF NOT EXISTS event (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL, effective TEXT, payload TEXT);
CREATE TABLE IF NOT EXISTS feedback (id INTEGER PRIMARY KEY, text TEXT NOT NULL, project TEXT, card TEXT, source TEXT NOT NULL, at TEXT NOT NULL, work TEXT REFERENCES work(id));
CREATE TABLE IF NOT EXISTS concern (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), text TEXT NOT NULL, resolved INTEGER NOT NULL DEFAULT 0, decision TEXT, at TEXT NOT NULL, resolved_at TEXT);
CREATE TABLE IF NOT EXISTS worktree (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), path TEXT NOT NULL, branch TEXT, kind TEXT NOT NULL, state TEXT NOT NULL, created TEXT NOT NULL, origin TEXT NOT NULL DEFAULT 'attached');
CREATE TABLE IF NOT EXISTS filed (work TEXT PRIMARY KEY REFERENCES work(id), session TEXT NOT NULL, entries INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS cursor (project TEXT NOT NULL, name TEXT NOT NULL, value INTEGER NOT NULL, PRIMARY KEY (project, name));
CREATE UNIQUE INDEX IF NOT EXISTS worktree_one_active_shared ON worktree(work) WHERE kind = 'shared' AND state = 'active';
CREATE TABLE IF NOT EXISTS message (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), text TEXT NOT NULL, at TEXT NOT NULL, delivered TEXT,
  event INTEGER REFERENCES event(id), look INTEGER REFERENCES event(id));
CREATE TABLE IF NOT EXISTS workspace (id TEXT PRIMARY KEY, name TEXT NOT NULL, path TEXT NOT NULL UNIQUE, creates INTEGER NOT NULL DEFAULT 0, created TEXT NOT NULL, updated TEXT NOT NULL);
` + goalTreeSchema

// Additive migration for ledgers created before parents, worktree origins,
// goal types, structured decisions or the epic rename existed. A worktree row
// with no recorded origin reads as attached, so wd never removes a directory it
// cannot prove it made. A work row whose kind reads as epic is goal under its
// old name: the rename happens in Go, not by rewriting rows, so a ledger
// written before the rename still opens and still says what it meant.
var addedColumns = []struct{ table, col, decl string }{
	{"work", "parent", "TEXT"}, {"work", "heading", "TEXT"}, {"work", "claim", "TEXT"}, {"work", "impact", "TEXT"},
	{"work", "goal_type", "TEXT"}, {"work", "archived", "TEXT"},
	{"work", "done_when", "TEXT"}, {"work", "finish_line", "TEXT"}, {"work", "finish_source", "TEXT"},
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
	// event is what a message to a goal's supervisor was filed as, and look
	// the look that delivered it; both are null on a message to a task.
	{"message", "event", "INTEGER REFERENCES event(id)"}, {"message", "look", "INTEGER REFERENCES event(id)"},
}

// workspaceSchema is the additive statement for ledgers created before
// workspaces existed. It is a whole table rather than columns in addedColumns
// because a PRIMARY KEY cannot be added with ALTER TABLE ADD COLUMN, and the
// uniqueness on path is the whole point: one directory is one workspace.
const workspaceSchema = `CREATE TABLE IF NOT EXISTS workspace (id TEXT PRIMARY KEY, name TEXT NOT NULL, path TEXT NOT NULL UNIQUE, creates INTEGER NOT NULL DEFAULT 0, created TEXT NOT NULL, updated TEXT NOT NULL);`

// goalTreeSchema is the additive statement for ledgers created before steps
// and sibling order existed. Both are whole tables: a composite primary key
// cannot be added with ALTER TABLE ADD COLUMN.
const goalTreeSchema = `CREATE TABLE IF NOT EXISTS step (work TEXT NOT NULL REFERENCES work(id), n INTEGER NOT NULL,
  text TEXT NOT NULL, state TEXT NOT NULL, PRIMARY KEY (work, n));
CREATE TABLE IF NOT EXISTS after (work TEXT NOT NULL REFERENCES work(id),
  waits_on TEXT NOT NULL REFERENCES work(id), PRIMARY KEY (work, waits_on));`

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
	if _, err := db.Exec(goalTreeSchema); err != nil {
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
	created, updated, parent, heading, claim, impact, goal_type, archived, done_when, finish_line, finish_source`

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
	var goalType, archived, doneWhen, finishLine, finishSource sql.NullString
	err := s.Scan(&w.ID, &w.Project, &w.Title, &w.Detail, &kind, &state,
		&runner, &session, &ref, &cwd, &w.Created, &w.Updated,
		&parent, &heading, &claim, &impact, &goalType, &archived,
		&doneWhen, &finishLine, &finishSource)
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
	w.DoneWhen = nullStr(doneWhen)
	if finishLine.Valid {
		fl, err := core.ParseFinishLine(finishLine.String)
		if err != nil {
			return core.Work{}, fmt.Errorf("work %s: %w", w.ID, err)
		}
		w.FinishLine = &fl
	}
	if finishSource.Valid {
		fs, err := core.ParseFinishSource(finishSource.String)
		if err != nil {
			return core.Work{}, fmt.Errorf("work %s: %w", w.ID, err)
		}
		w.FinishSource = &fs
	}
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
	if payload.Valid && payload.String != "" && e.Kind == core.EventQuestion {
		var ch core.Change
		if err := json.Unmarshal([]byte(payload.String), &ch); err != nil {
			return core.Event{}, fmt.Errorf("event %d: change payload: %w", e.ID, err)
		}
		e.Change = &ch
	} else if payload.Valid && payload.String != "" && e.Kind == core.EventNote {
		var r core.Reasoning
		if err := json.Unmarshal([]byte(payload.String), &r); err != nil {
			return core.Event{}, fmt.Errorf("event %d: reasoning payload: %w", e.ID, err)
		}
		e.Reasoning = &r
	} else if payload.Valid && payload.String != "" && e.Kind == core.EventAttachment {
		var a core.Attachment
		if err := json.Unmarshal([]byte(payload.String), &a); err != nil {
			return core.Event{}, fmt.Errorf("event %d: attachment payload: %w", e.ID, err)
		}
		e.Attachment = &a
	} else if payload.Valid && payload.String != "" {
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
// goals and tasks. Anything else refuses a parent, so the shape of the ledger
// cannot drift into something no reader could draw.
func childKind(parentKind, childKind core.WorkKind) error {
	switch {
	case parentKind == core.WorkRoadmap:
		if childKind != core.WorkItem {
			return fmt.Errorf("a roadmap holds items, not %s", childKind)
		}
		return nil
	case core.IsGoal(parentKind):
		if childKind != core.WorkTask && !core.IsGoal(childKind) {
			return fmt.Errorf("a goal holds goals and tasks, not %s", childKind)
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
		// A roadmap is the top level, not a child: checked before the parent's
		// own rule so the refusal says what is wrong rather than what the parent
		// happened to hold.
		if kind == core.WorkRoadmap {
			return core.Work{}, fmt.Errorf("a roadmap cannot sit under another work item")
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
		if _, err := addEvent(tx, id, core.EventState, string(core.StateQueued), t); err != nil {
			return err
		}
		return rollUp(tx, parent)
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

// Tasks are every task under a goal at any depth, oldest first: the work its
// agents run, however deep its sub-goals nest.
func (l *Ledger) Tasks(goalID string) ([]core.Work, error) {
	tree, err := l.Under(goalID)
	if err != nil {
		return nil, err
	}
	out := []core.Work{}
	for _, w := range tree[1:] {
		if w.Kind == core.WorkTask {
			out = append(out, w)
		}
	}
	slices.SortStableFunc(out, func(a, b core.Work) int { return strings.Compare(a.Created, b.Created) })
	return out, nil
}

// RoadmapItems of a roadmap, oldest first: the items still waiting, and the
// goals already translated out of them, in the order they were filed.
func (l *Ledger) Items(roadmapID string) ([]core.Work, error) {
	return l.Children(roadmapID)
}

// Children are the work directly under parentID, oldest first.
func (l *Ledger) Children(parentID string) ([]core.Work, error) {
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

// SetSteps replaces work's steps with these, numbered from 1 in the order
// given. A step with no text or an unknown state refuses the whole list.
func (l *Ledger) SetSteps(work string, steps []core.Step) error {
	for i, st := range steps {
		if strings.TrimSpace(st.Text) == "" {
			return fmt.Errorf("step %d of %s has no text", i+1, work)
		}
		if _, err := core.ParseStepState(string(st.State)); err != nil {
			return err
		}
	}
	return l.inTx(func(tx *sql.Tx) error {
		if _, err := getWork(tx, work); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM step WHERE work = ?`, work); err != nil {
			return err
		}
		for i, st := range steps {
			if _, err := tx.Exec(`INSERT INTO step (work, n, text, state) VALUES (?, ?, ?, ?)`,
				work, i+1, strings.TrimSpace(st.Text), string(st.State)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Steps are work's steps in order.
func (l *Ledger) Steps(work string) ([]core.Step, error) {
	rows, err := l.db.Query(`SELECT n, text, state FROM step WHERE work = ? ORDER BY n`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Step{}
	for rows.Next() {
		var st core.Step
		var state string
		if err := rows.Scan(&st.N, &st.Text, &state); err != nil {
			return nil, err
		}
		if st.State, err = core.ParseStepState(state); err != nil {
			return nil, fmt.Errorf("step %d of %s: %w", st.N, work, err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SetAfter replaces what work waits on. Only siblings are waited on, and
// never in a loop: an order that cannot be satisfied is refused.
func (l *Ledger) SetAfter(work string, waitsOn []string) error {
	return l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, work)
		if err != nil {
			return err
		}
		for _, id := range waitsOn {
			if id == work {
				return fmt.Errorf("%s cannot wait on itself", work)
			}
			o, err := getWork(tx, id)
			if err != nil {
				return err
			}
			if w.Parent == nil || o.Parent == nil || *w.Parent != *o.Parent {
				return fmt.Errorf("%s and %s are not siblings; a sub-goal waits only on its siblings", work, id)
			}
			reaches, err := waitsOnAtAnyRemove(tx, id, work)
			if err != nil {
				return err
			}
			if reaches {
				return fmt.Errorf("%s already waits on %s", id, work)
			}
		}
		if _, err := tx.Exec(`DELETE FROM after WHERE work = ?`, work); err != nil {
			return err
		}
		for _, id := range waitsOn {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO after (work, waits_on) VALUES (?, ?)`, work, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Reparent moves a task under a goal beside it: a goal whose parent is the
// task's own. Order among siblings does not survive the move, so a task still
// ordered is refused until that order is cleared.
func (l *Ledger) Reparent(id, parent string) error {
	return l.inTx(func(tx *sql.Tx) error {
		w, err := getWork(tx, id)
		if err != nil {
			return err
		}
		if w.Kind != core.WorkTask {
			return fmt.Errorf("only a task moves under a sub-goal; %s is a %s", id, w.Kind)
		}
		p, err := getWork(tx, parent)
		if err != nil {
			return err
		}
		if !core.IsGoal(p.Kind) || p.ID == id || w.Parent == nil || p.Parent == nil || *p.Parent != *w.Parent {
			return fmt.Errorf("%s is not a goal beside %s", parent, id)
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM after WHERE work = ? OR waits_on = ?`, id, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%s is ordered among its siblings; clear that order first", id)
		}
		if err := updateWork(tx, id, `UPDATE work SET parent = ? WHERE id = ?`, parent, id); err != nil {
			return err
		}
		if err := rollUp(tx, &parent); err != nil {
			return err
		}
		return rollUp(tx, w.Parent)
	})
}

// waitsOnAtAnyRemove reports whether from waits on target, directly or
// through what it waits on.
func waitsOnAtAnyRemove(tx *sql.Tx, from, target string) (bool, error) {
	var n int
	err := tx.QueryRow(`WITH RECURSIVE reach(id) AS (
			SELECT waits_on FROM after WHERE work = ?
			UNION SELECT after.waits_on FROM after JOIN reach ON after.work = reach.id)
		SELECT COUNT(*) FROM reach WHERE id = ?`, from, target).Scan(&n)
	return n > 0, err
}

// After is the work that work waits on.
func (l *Ledger) After(work string) ([]string, error) {
	rows, err := l.db.Query(`SELECT waits_on FROM after WHERE work = ? ORDER BY waits_on`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetDoneWhen stores what makes the work done; empty text clears it to unset.
// Either way the change is filed as a decision made by by.
func (l *Ledger) SetDoneWhen(id, text, by string) error {
	text = strings.TrimSpace(text)
	var stored *string
	answer := "not set"
	if text != "" {
		stored = &text
		answer = text
	}
	return l.inTx(func(tx *sql.Tx) error {
		if err := updateWork(tx, id, `UPDATE work SET done_when = ? WHERE id = ?`, stored, id); err != nil {
			return err
		}
		d := core.Decision{Question: "what makes " + id + " done", Answer: answer, Source: by}
		_, err := addClaim(tx, id, core.EventDecision, "done when: "+answer, now(), nil, &d)
		return err
	})
}

// Judge files the director's judgement of id's done-when; a missed one puts
// id back to running.
func (l *Ledger) Judge(id string, met bool, text string) error {
	if _, err := l.AddEvent(id, core.EventJudged, core.Judgement(met, text)); err != nil {
		return err
	}
	if met {
		return nil
	}
	w, err := l.Get(id)
	if err != nil || w.State == core.StateRunning {
		return err
	}
	_, err = l.Transition(id, core.StateRunning)
	return err
}

// SetFinish stores what the work hands back and where that was decided, and
// files the change as a decision.
func (l *Ledger) SetFinish(id string, line core.FinishLine, src core.FinishSource) error {
	if strings.TrimSpace(string(line)) == "" {
		return fmt.Errorf("a finish line names what is handed back")
	}
	line, err := core.ParseFinishLine(string(line))
	if err != nil {
		return err
	}
	if _, err := core.ParseFinishSource(string(src)); err != nil {
		return err
	}
	return l.inTx(func(tx *sql.Tx) error {
		if err := updateWork(tx, id, `UPDATE work SET finish_line = ?, finish_source = ? WHERE id = ?`, string(line), string(src), id); err != nil {
			return err
		}
		_, err := addEvent(tx, id, core.EventDecision, fmt.Sprintf("finishes at: %s (from %s)", line, src), now())
		return err
	})
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
		if _, err := addEvent(tx, id, core.EventState, string(core.StateAbandoned), now()); err != nil {
			return err
		}
		return rollUp(tx, w.Parent)
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
		if _, err := addClaim(tx, id, core.EventDecision, "reopened: "+why, now(), nil, &core.Decision{
			Question: "what is being worked on in " + id + " now it is done?",
			Answer:   why,
		}); err != nil {
			return err
		}
		return rollUp(tx, w.Parent)
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
		if _, err := addClaim(tx, id, core.EventDecision, "released: "+why, now(), nil, &core.Decision{
			Question: "was the stop recorded against " + id + " a failure or a choice?",
			Answer:   why,
		}); err != nil {
			return err
		}
		return rollUp(tx, w.Parent)
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
	if to == core.StateSuperseded {
		return core.Work{}, fmt.Errorf("work %s: use Supersede to supersede work, which says why the course changed", id)
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
		if w.State == core.StateDropped {
			return fmt.Errorf("work %s is dropped; bringing it back goes through Restore, with everything dropped alongside it: wd resume %s", id, id)
		}
		sub, err := subGoal(tx, w)
		if err != nil {
			return err
		}
		if sub {
			return fmt.Errorf("%s is a sub-goal; its state follows its children", id)
		}
		if !slices.Contains(core.Transitions[w.State], to) {
			return core.IllegalTransition{From: w.State, To: to}
		}
		if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(to), id); err != nil {
			return err
		}
		if _, err := addEvent(tx, id, core.EventState, string(to), now()); err != nil {
			return err
		}
		return rollUp(tx, w.Parent)
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
		return rollUp(tx, tree[0].Parent)
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
		if dropped, err = dropOpen(tx, tree, why, now()); err != nil {
			return err
		}
		return rollUp(tx, tree[0].Parent)
	})
	if err != nil {
		return nil, err
	}
	return l.reread(dropped)
}

// Supersede marks work and everything open under it as abandoned by a course
// change, filing why on each. Work at rest under it is left as it is. It
// returns the work it superseded.
func (l *Ledger) Supersede(id, why string) ([]core.Work, error) {
	why = strings.TrimSpace(why)
	var superseded []core.Work
	err := l.inTx(func(tx *sql.Tx) error {
		tree, err := subtree(tx, id)
		if err != nil {
			return err
		}
		if core.AtRest(tree[0].State) {
			return fmt.Errorf("work %s is %s: it is already at rest", id, tree[0].State)
		}
		if why == "" {
			return fmt.Errorf("superseding %s needs a reason: say why the course changed", id)
		}
		at := now()
		for _, w := range tree {
			if core.AtRest(w.State) {
				continue
			}
			if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(core.StateSuperseded), w.ID); err != nil {
				return err
			}
			if _, err := addEvent(tx, w.ID, core.EventState, string(core.StateSuperseded), at); err != nil {
				return err
			}
			if _, err := addEvent(tx, w.ID, core.EventNote, "superseded: "+why, at); err != nil {
				return err
			}
			superseded = append(superseded, w)
		}
		return rollUp(tx, tree[0].Parent)
	})
	if err != nil {
		return nil, err
	}
	return l.reread(superseded)
}

// Restore brings dropped work back to the state it was dropped from, with
// every work under it that the same drop let go of. Work dropped on its own,
// at another moment, stays dropped. It returns the work it restored.
func (l *Ledger) Restore(id string) ([]core.Work, error) {
	var restored []core.Work
	err := l.inTx(func(tx *sql.Tx) error {
		tree, err := subtree(tx, id)
		if err != nil {
			return err
		}
		if tree[0].State != core.StateDropped {
			return fmt.Errorf("work %s is %s; only dropped work is restored", id, tree[0].State)
		}
		if tree[0].Archived != nil {
			return fmt.Errorf("work %s is archived; Unarchive it first: wd unarchive %s", id, id)
		}
		moment, _, err := droppedFrom(tx, id)
		if err != nil {
			return err
		}
		at := now()
		for _, w := range tree {
			if w.State != core.StateDropped {
				continue
			}
			when, before, err := droppedFrom(tx, w.ID)
			if err != nil {
				return err
			}
			if when != moment {
				continue
			}
			if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(before), w.ID); err != nil {
				return err
			}
			if _, err := addEvent(tx, w.ID, core.EventState, string(before), at); err != nil {
				return err
			}
			if _, err := addEvent(tx, w.ID, core.EventNote, "restored", at); err != nil {
				return err
			}
			restored = append(restored, w)
		}
		return rollUp(tx, tree[0].Parent)
	})
	if err != nil {
		return nil, err
	}
	return l.reread(restored)
}

// subGoal reports whether w is a goal under a goal, whose state is rolled up
// from its children rather than set.
func subGoal(q queryer, w core.Work) (bool, error) {
	if !core.IsGoal(w.Kind) || w.Parent == nil {
		return false, nil
	}
	p, err := getWork(q, *w.Parent)
	if err != nil {
		return false, err
	}
	return core.IsGoal(p.Kind), nil
}

// rollUp sets a sub-goal's state from its children's, filing a state event
// when it changes, and climbs while the work it reaches is a sub-goal. A top
// goal, or any work that is not a sub-goal, is left as it is.
func rollUp(tx *sql.Tx, id *string) error {
	for id != nil {
		w, err := getWork(tx, *id)
		if err != nil {
			return err
		}
		sub, err := subGoal(tx, w)
		if err != nil || !sub {
			return err
		}
		rows, err := tx.Query(`SELECT state FROM work WHERE parent = ?`, w.ID)
		if err != nil {
			return err
		}
		var states []core.State
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			st, err := core.ParseState(raw)
			if err != nil {
				rows.Close()
				return fmt.Errorf("work under %s: %w", w.ID, err)
			}
			states = append(states, st)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		to := core.RollupState(states)
		if to == w.State {
			return nil
		}
		if _, err := tx.Exec(`UPDATE work SET state = ? WHERE id = ?`, string(to), w.ID); err != nil {
			return err
		}
		if _, err := addEvent(tx, w.ID, core.EventState, string(to), now()); err != nil {
			return err
		}
		id = w.Parent
	}
	return nil
}

// droppedFrom is when work was last dropped and the state it was dropped from.
func droppedFrom(tx *sql.Tx, id string) (string, core.State, error) {
	rows, err := tx.Query(`SELECT body, at FROM event WHERE work = ? AND kind = ? ORDER BY id`, id, string(core.EventState))
	if err != nil {
		return "", "", err
	}
	type change struct{ body, at string }
	var changes []change
	for rows.Next() {
		var c change
		if err := rows.Scan(&c.body, &c.at); err != nil {
			rows.Close()
			return "", "", err
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", "", err
	}
	if err := rows.Close(); err != nil {
		return "", "", err
	}
	for i := len(changes) - 1; i > 0; i-- {
		if changes[i].body == string(core.StateDropped) {
			before, err := core.ParseState(changes[i-1].body)
			return changes[i].at, before, err
		}
	}
	return "", "", fmt.Errorf("work %s has no record of the state it was dropped from", id)
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
	return l.queue(work, text, nil)
}

// QueueForSupervisor stores something the person said to goal, filed as
// event, for the goal's supervisor: it waits until a look delivers it.
func (l *Ledger) QueueForSupervisor(goal string, event int, text string) (core.Message, error) {
	return l.queue(goal, text, &event)
}

func (l *Ledger) queue(work, text string, event *int) (core.Message, error) {
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
		res, err := tx.Exec(`INSERT INTO message (work, text, at, event) VALUES (?, ?, ?, ?)`, work, text, at, event)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		m = core.Message{ID: int(id), Work: work, Text: text, At: at, Event: event}
		return nil
	})
	return m, err
}

// Undelivered lists work's messages not yet delivered, oldest first.
func (l *Ledger) Undelivered(work string) ([]core.Message, error) {
	return undelivered(l.db, work)
}

// querier is what reads the ledger: the database or a transaction on it.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func undelivered(q querier, work string) ([]core.Message, error) {
	rows, err := q.Query(`SELECT id, work, text, at, event FROM message WHERE work = ? AND delivered IS NULL ORDER BY id`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Message{}
	for rows.Next() {
		var m core.Message
		var event sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Work, &m.Text, &m.At, &event); err != nil {
			return nil, err
		}
		if event.Valid {
			n := int(event.Int64)
			m.Event = &n
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Withdraw removes work's undelivered messages and returns them, oldest first,
// filing a withdrawn note for each.
func (l *Ledger) Withdraw(work string) ([]core.Message, error) {
	out := []core.Message{}
	err := l.inTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id, work, text, at FROM message WHERE work = ? AND delivered IS NULL ORDER BY id`, work)
		if err != nil {
			return err
		}
		for rows.Next() {
			var m core.Message
			if err := rows.Scan(&m.ID, &m.Work, &m.Text, &m.At); err != nil {
				rows.Close()
				return err
			}
			out = append(out, m)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		at := now()
		for _, m := range out {
			if _, err := tx.Exec(`DELETE FROM message WHERE id = ?`, m.ID); err != nil {
				return err
			}
			if _, err := addEvent(tx, work, core.EventNote, "withdrawn: "+m.Text, at); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
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

func (l *Ledger) SetTitle(id, title string) error {
	return updateWork(l.db, id, `UPDATE work SET title = ?, updated = ? WHERE id = ?`, title, now(), id)
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

// AddEvent records an event on work and returns it as stored, followed at the
// same moment by the files attached to it.
func (l *Ledger) AddEvent(work string, kind core.EventKind, body string, atts ...core.Attachment) (core.Event, error) {
	var e core.Event
	err := l.inTx(func(tx *sql.Tx) error {
		at := now()
		var err error
		if e, err = addEvent(tx, work, kind, body, at); err != nil {
			return err
		}
		for _, a := range atts {
			if _, err := addAttachment(tx, work, at, a); err != nil {
				return err
			}
		}
		return nil
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

// Answer records an answer on work with what it answered and who answered it,
// so the work's record reads as the question and its answer, followed by the
// files attached to it. An answer of files alone has no words.
func (l *Ledger) Answer(work string, d core.Decision, atts ...core.Attachment) (core.Event, error) {
	if (d.Answer == "" && len(atts) == 0) || d.Source == "" {
		return core.Event{}, fmt.Errorf("an answer needs its words or files, and who gave it")
	}
	var e core.Event
	err := l.inTx(func(tx *sql.Tx) error {
		at := now()
		var err error
		if e, err = addClaim(tx, work, core.EventAnswer, d.Answer, at, nil, &d); err != nil {
			return err
		}
		for _, a := range atts {
			if _, err := addAttachment(tx, work, at, a); err != nil {
				return err
			}
		}
		return nil
	})
	return e, err
}

// Attach records each file attached to work as its own event, in order.
func (l *Ledger) Attach(work string, atts []core.Attachment) ([]core.Event, error) {
	var out []core.Event
	err := l.inTx(func(tx *sql.Tx) error {
		at := now()
		if err := updateWork(tx, work, `UPDATE work SET updated = ? WHERE id = ?`, at, work); err != nil {
			return err
		}
		for _, a := range atts {
			e, err := addAttachment(tx, work, at, a)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

func addAttachment(tx *sql.Tx, work, at string, a core.Attachment) (core.Event, error) {
	if a.Path == "" || a.Name == "" || a.MediaType == "" {
		return core.Event{}, fmt.Errorf("an attachment needs its name, path and media type")
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return core.Event{}, err
	}
	return scanEvent(tx.QueryRow(`INSERT INTO event (work, kind, body, at, payload) VALUES (?, ?, ?, ?, ?)
		RETURNING id, work, kind, body, at, effective, payload`, work, string(core.EventAttachment), a.Path, at, string(raw)))
}

// Ask records a question on work that carries out change when answered with
// its first choice.
func (l *Ledger) Ask(work, body string, change core.Change) (core.Event, error) {
	raw, err := json.Marshal(change)
	if err != nil {
		return core.Event{}, err
	}
	var e core.Event
	err = l.inTx(func(tx *sql.Tx) error {
		at := now()
		if err := updateWork(tx, work, `UPDATE work SET updated = ? WHERE id = ?`, at, work); err != nil {
			return err
		}
		e, err = scanEvent(tx.QueryRow(`INSERT INTO event (work, kind, body, at, payload) VALUES (?, ?, ?, ?, ?)
			RETURNING id, work, kind, body, at, effective, payload`, work, string(core.EventQuestion), body, at, string(raw)))
		return err
	})
	return e, err
}

// Reason records on goal why its planner broke it down as it did, as a note
// carrying the reasoning.
func (l *Ledger) Reason(goal string, r core.Reasoning) (core.Event, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return core.Event{}, err
	}
	parts := []string{}
	for _, p := range []struct {
		name string
		text *string
	}{{"why", r.Why}, {"left out", r.LeftOut}, {"judge by", r.JudgeBy}} {
		if p.text != nil {
			parts = append(parts, p.name+": "+*p.text)
		}
	}
	var e core.Event
	err = l.inTx(func(tx *sql.Tx) error {
		at := now()
		if err := updateWork(tx, goal, `UPDATE work SET updated = ? WHERE id = ?`, at, goal); err != nil {
			return err
		}
		e, err = scanEvent(tx.QueryRow(`INSERT INTO event (work, kind, body, at, payload) VALUES (?, ?, ?, ?, ?)
			RETURNING id, work, kind, body, at, effective, payload`, goal, string(core.EventNote), core.ReasoningPrefix+strings.Join(parts, " | "), at, string(raw)))
		return err
	})
	return e, err
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

// ClaimLook files a look on goal, saying why, unless a look or supervised
// event newer than after is on it already: passes that decide to look at the
// same moment look once. ok is false when another pass looked first. The
// look takes every message waiting for the goal's supervisor, returned oldest
// first: they go with its brief, and come back with ReleaseLook when the
// brief cannot be delivered.
func (l *Ledger) ClaimLook(goal string, after int, why string) (e core.Event, taken []core.Message, ok bool, err error) {
	err = l.inTx(func(tx *sql.Tx) error {
		var latest sql.NullInt64
		if err := tx.QueryRow(`SELECT MAX(id) FROM event WHERE work = ? AND kind IN (?, ?)`,
			goal, core.EventLook, core.EventSupervised).Scan(&latest); err != nil {
			return err
		}
		if int(latest.Int64) != after {
			return nil
		}
		at := now()
		if e, err = addEvent(tx, goal, core.EventLook, why, at); err != nil {
			return err
		}
		if taken, err = undelivered(tx, goal); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE message SET delivered = ?, look = ? WHERE work = ? AND delivered IS NULL`, at, e.ID, goal); err != nil {
			return err
		}
		ok = true
		return nil
	})
	return e, taken, ok, err
}

// ReleaseLook takes back look, whose brief never reached the supervisor: the
// look is removed and the messages it took wait again, so the next pass looks
// with them.
func (l *Ledger) ReleaseLook(look core.Event) error {
	return l.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE message SET delivered = NULL, look = NULL WHERE look = ?`, look.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM event WHERE id = ? AND kind = ?`, look.ID, core.EventLook)
		return err
	})
}

// SetFiled records the transcript point work was last read at.
func (l *Ledger) SetFiled(work string, at core.TranscriptMark) error {
	_, err := l.db.Exec(`INSERT INTO filed (work, session, entries) VALUES (?, ?, ?)
		ON CONFLICT (work) DO UPDATE SET session = excluded.session, entries = excluded.entries`,
		work, at.Session, at.Entries)
	return err
}

// Advance moves work's transcript point from from to to, only while it is
// still from: of passes reading the same reply at once, one reads it. ok is
// false when another pass moved it first.
func (l *Ledger) Advance(work string, from, to core.TranscriptMark) (bool, error) {
	res, err := l.db.Exec(`UPDATE filed SET session = ?, entries = ? WHERE work = ? AND session = ? AND entries = ?`,
		to.Session, to.Entries, work, from.Session, from.Entries)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// File files an event on work from a point of a transcript, unless work was
// already filed from that point: of passes reading the same point at once,
// one files it. ok is false when it was filed already.
func (l *Ledger) File(work string, kind core.EventKind, body string, at core.TranscriptMark) (ok bool, err error) {
	err = l.inTx(func(tx *sql.Tx) error {
		var prev core.TranscriptMark
		err := tx.QueryRow(`SELECT session, entries FROM filed WHERE work = ?`, work).Scan(&prev.Session, &prev.Entries)
		if err == nil && prev == at {
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := addEvent(tx, work, kind, body, now()); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO filed (work, session, entries) VALUES (?, ?, ?)
			ON CONFLICT (work) DO UPDATE SET session = excluded.session, entries = excluded.entries`,
			work, at.Session, at.Entries)
		ok = err == nil
		return err
	})
	return ok, err
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
// It reaches every depth: a goal's sub-goals and their tasks are under it too.
// A task has nothing under it, so for a task this is its own events.
func (l *Ledger) EventsUnder(work string) ([]core.Event, error) {
	rows, err := l.db.Query(`WITH RECURSIVE tree(id) AS (
			SELECT ? UNION SELECT work.id FROM work JOIN tree ON work.parent = tree.id)
		SELECT `+eventColumns+` FROM event WHERE work IN (SELECT id FROM tree)
		ORDER BY id`, work)
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

// WaitingUnder is the events, at work or under it, that hold something the
// person said which no look has given a supervisor yet, in id order.
func (l *Ledger) WaitingUnder(work string) ([]int, error) {
	rows, err := l.db.Query(`WITH RECURSIVE tree(id) AS (
			SELECT ? UNION SELECT work.id FROM work JOIN tree ON work.parent = tree.id)
		SELECT event FROM message WHERE work IN (SELECT id FROM tree) AND event IS NOT NULL AND delivered IS NULL
		ORDER BY event`, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
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
	tree, err := l.Under(epicID)
	if err != nil {
		return nil, err
	}
	works := map[string]bool{}
	for _, w := range tree {
		works[w.ID] = true
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
		if t.Claim != nil && !core.Settled(t.State) {
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

// review → soft-done. Goal: needs every task under it settled, and its finish
// line reached (FinishReached), or with none a DONE report, passing verify and
// PR; what is missing is listed in a person's words. Task: needs only the DONE report. Standalone: DONE + verify, and
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
		for _, t := range open {
			if !core.Settled(t.State) {
				missing = append(missing, core.MissingTask(t.Title))
			}
		}
		if w.FinishLine != nil {
			if ok, what := core.FinishReached(*w.FinishLine, evs); !ok {
				missing = append(missing, strings.Split(what, ", ")...)
			}
		} else {
			if r := last(core.EventReport); r == nil || !strings.HasPrefix(r.Body, "DONE") {
				missing = append(missing, core.MissingReport)
			}
			if v := last(core.EventVerify); v == nil || !strings.HasPrefix(v.Body, "pass") {
				missing = append(missing, core.MissingChecks)
			}
			if last(core.EventPr) == nil {
				missing = append(missing, core.MissingPullRequest)
			}
		}
	} else if w.Parent != nil {
		if r := last(core.EventReport); r == nil || !strings.HasPrefix(r.Body, "DONE") {
			missing = append(missing, core.MissingReport)
		}
	} else {
		if r := last(core.EventReport); r == nil || !strings.HasPrefix(r.Body, "DONE") {
			missing = append(missing, core.MissingReport)
		}
		if v := last(core.EventVerify); v == nil || !strings.HasPrefix(v.Body, "pass") {
			missing = append(missing, core.MissingChecks)
		}
		if codeChanged && last(core.EventPr) == nil {
			missing = append(missing, core.MissingPullRequest)
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
