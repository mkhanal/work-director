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
  parent TEXT, heading TEXT, claim TEXT, impact TEXT, goal_type TEXT);
CREATE TABLE IF NOT EXISTS event (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL, effective TEXT, payload TEXT);
CREATE TABLE IF NOT EXISTS feedback (id INTEGER PRIMARY KEY, text TEXT NOT NULL, project TEXT, card TEXT, source TEXT NOT NULL, at TEXT NOT NULL, work TEXT REFERENCES work(id));
CREATE TABLE IF NOT EXISTS concern (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), text TEXT NOT NULL, resolved INTEGER NOT NULL DEFAULT 0, decision TEXT, at TEXT NOT NULL, resolved_at TEXT);
CREATE TABLE IF NOT EXISTS worktree (id INTEGER PRIMARY KEY, work TEXT NOT NULL REFERENCES work(id), path TEXT NOT NULL, branch TEXT, kind TEXT NOT NULL, state TEXT NOT NULL, created TEXT NOT NULL, origin TEXT NOT NULL DEFAULT 'attached');
CREATE TABLE IF NOT EXISTS filed (work TEXT PRIMARY KEY REFERENCES work(id), session TEXT NOT NULL, entries INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS cursor (project TEXT NOT NULL, name TEXT NOT NULL, value INTEGER NOT NULL, PRIMARY KEY (project, name));
CREATE UNIQUE INDEX IF NOT EXISTS worktree_one_active_shared ON worktree(work) WHERE kind = 'shared' AND state = 'active';
`

// Additive migration for ledgers created before parents, worktree origins,
// goal types, structured decisions or the epic rename existed. A worktree row
// with no recorded origin reads as attached, so wd never removes a directory it
// cannot prove it made. A work row whose kind reads as epic is goal under its
// old name: the rename happens in Go, not by rewriting rows, so a ledger
// written before the rename still opens and still says what it meant.
var addedColumns = []struct{ table, col, decl string }{
	{"work", "parent", "TEXT"}, {"work", "heading", "TEXT"}, {"work", "claim", "TEXT"}, {"work", "impact", "TEXT"},
	{"work", "goal_type", "TEXT"},
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
	created, updated, parent, heading, claim, impact, goal_type`

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
	var goalType sql.NullString
	err := s.Scan(&w.ID, &w.Project, &w.Title, &w.Detail, &kind, &state,
		&runner, &session, &ref, &cwd, &w.Created, &w.Updated,
		&parent, &heading, &claim, &impact, &goalType)
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
