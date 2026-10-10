// Package core holds the director's domain types and rules: the work state
// machine, the ledger's value types and the errors the ledger can raise. It
// has no storage and no I/O; the ledger is its only interpreter.
package core

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type State string

const (
	StateQueued     State = "queued"
	StateBriefed    State = "briefed"
	StateRunning    State = "running"
	StateNeedsInput State = "needs-input"
	StateReview     State = "review"
	StateSoftDone   State = "soft-done"
	StateDone       State = "done"
	StateBlocked    State = "blocked"
	StateDropped    State = "dropped"
	// StateAbandoned is work that stopped without shipping: a PR was never
	// raised, or one was raised and never merged. It is not done and not
	// dropped — the attempt was real and the result is not in the product.
	StateAbandoned State = "abandoned"
	// StatePaused is work a person stopped on purpose, with its session's
	// conversation kept. It is not blocked: nothing is waiting on an answer,
	// someone chose to hold it.
	StatePaused State = "paused"
)

type WorkKind string

const (
	WorkTask      WorkKind = "task"
	WorkEvolution WorkKind = "evolution"
	WorkWorkflow  WorkKind = "workflow"
	WorkGoal      WorkKind = "goal"
	WorkRoadmap   WorkKind = "roadmap"
	// WorkItem is a roadmap item: a goal in waiting. It is the same row as the
	// goal it becomes, so the id an item was filed under is the id its goal
	// keeps; wd roadmap plan translates the kind rather than copying the row.
	WorkItem WorkKind = "item"
	// WorkEpic is the pre-rename spelling of WorkGoal. It is read, never
	// written, so a ledger from before the rename still loads.
	WorkEpic WorkKind = "epic"
)

// GoalType says what kind of thing a goal was, so a reader can tell a question
// that was answered from work that shipped without reading either.
type GoalType string

const (
	// GoalQuery is a goal that only asked something: the conversation wanted
	// to know, and knowing changed nothing. It is not abandonment — nothing was
	// attempted and nothing failed to land.
	GoalQuery GoalType = "query"
	// GoalBuild made something that did not exist.
	GoalBuild GoalType = "build"
	// GoalFix repaired something that was broken.
	GoalFix GoalType = "fix"
	// GoalChange modified what already existed.
	GoalChange GoalType = "change"
	// GoalReview examined without editing. The judgement shipped; the code did
	// not move.
	GoalReview GoalType = "review"
)

type EventKind string

const (
	EventState    EventKind = "state"
	EventReport   EventKind = "report"
	EventVerify   EventKind = "verify"
	EventPr       EventKind = "pr"
	EventNote     EventKind = "note"
	EventSent     EventKind = "sent"
	EventSpawn    EventKind = "spawn"
	EventAttach   EventKind = "attach"
	EventQuestion EventKind = "question"
	EventAnswer   EventKind = "answer"
	EventDecision EventKind = "decision"
	// EventAbandon records why work stopped without shipping. The body is
	// "<reason>: <detail>" so a reader sees the reason and the evidence in one
	// line, and the reason is one of AbandonReasons.
	EventAbandon EventKind = "abandon"
)

type FeedbackSource string

const (
	FeedbackDirector FeedbackSource = "director"
	FeedbackNote     FeedbackSource = "note"
	FeedbackAttached FeedbackSource = "attached"
)

// LandingKind is how a change reached somewhere a person can read it. Both
// satisfy the gate, because the gate asks whether the work landed, not how many
// people looked at it on the way — but they are different facts, and an audit
// that cannot tell them apart cannot say whether a change was ever reviewed.
type LandingKind string

const (
	// LandingCommit is a commit already pushed: the change is in the product.
	LandingCommit LandingKind = "commit"
	// LandingPullRequest is a pull request waiting on a merge.
	LandingPullRequest LandingKind = "pull-request"
)

// Landing is a filed landing read back.
type Landing struct {
	Kind LandingKind
	URL  string
}

// Known says which kind it was. A body filed before the kind was recorded is a
// bare url: a real link of unknown kind, which is not the same as knowing it
// was neither.
func (l Landing) Known() bool { return l.Kind != "" }

// ParseLanding reads a landing body, which is "<kind> <url>" once kinds were
// recorded and a bare url before. An unrecognised kind reads as unknown rather
// than as a guess, because a review surface that invented one would be worse
// than one that admitted it did not look.
func ParseLanding(body string) Landing {
	body = strings.TrimSpace(body)
	kind, url, ok := strings.Cut(body, " ")
	url = strings.TrimSpace(url)
	if !ok {
		return Landing{URL: body}
	}
	switch LandingKind(kind) {
	case LandingCommit, LandingPullRequest:
		return Landing{Kind: LandingKind(kind), URL: url}
	}
	return Landing{URL: body}
}

type WorktreeKind string

const (
	WorktreeShared  WorktreeKind = "shared"
	WorktreePrivate WorktreeKind = "private"
)

type WorktreeState string

const (
	WorktreeActive    WorktreeState = "active"
	WorktreeMerged    WorktreeState = "merged"
	WorktreeAbandoned WorktreeState = "abandoned"
	WorktreeRemoved   WorktreeState = "removed"
)

// WorktreeOrigin says who made a worktree. Only a director-made worktree is
// ever removed by wd: an attached one belongs to whoever created it.
type WorktreeOrigin string

const (
	OriginDirector WorktreeOrigin = "director"
	OriginAttached WorktreeOrigin = "attached"
)

// IsGoal says whether a kind is a goal, reading the pre-rename epic spelling
// too so a ledger from before the rename still loads. A roadmap is not a goal:
// it is the level above, and it holds items rather than tasks.
func IsGoal(kind WorkKind) bool { return kind == WorkGoal || kind == WorkEpic }

// IsRoadmapItem says whether a kind is a goal still in waiting. An item is not
// a third thing: it is the row a goal starts as, and wd roadmap plan
// translates its kind rather than copying the row, so the id stays the same.
func IsRoadmapItem(kind WorkKind) bool { return kind == WorkItem }

// ClosesWithoutShipping says whether a state means work that stopped without
// its change reaching anywhere. Done shipped; dropped was removed on purpose;
// abandoned was attempted and did not land.
func ClosesWithoutShipping(s State) bool { return s == StateAbandoned }

type Work struct {
	ID      string   `json:"id"`
	Project string   `json:"project"`
	Title   string   `json:"title"`
	Detail  string   `json:"detail"`
	Kind    WorkKind `json:"kind"`
	State   State    `json:"state"`
	Runner  *string  `json:"runner"`
	Session *string  `json:"session"`
	Ref     *string  `json:"ref"`
	Cwd     *string  `json:"cwd"`
	Created string   `json:"created"`
	Updated string   `json:"updated"`
	Parent  *string  `json:"parent"`
	Heading *string  `json:"heading"`
	Claim   *string  `json:"claim"`
	Impact  *string  `json:"impact"`
	// GoalType classifies a goal as a question answered or work done. It is
	// null until classified; a goal is never silently given a default, because
	// a wrong type is a lie a reader cannot see.
	GoalType *GoalType `json:"goal_type"`
	// Archived is when the work was put out of sight, or null while it is on
	// the board. It is not a state: archived work keeps the state it rests in.
	Archived *string `json:"archived"`
}

type Event struct {
	ID   int       `json:"id"`
	Work string    `json:"work"`
	Kind EventKind `json:"kind"`
	Body string    `json:"body"`
	// At is when the event was recorded: the moment the ledger learned it. It
	// is a fact about the write, and it is always there.
	At string `json:"at"`
	// Effective is when the event became the thing in force. It is null when
	// the two are the same moment, because a null reads as "not separated" and
	// a reader can tell the two cases apart. Recorded and effective differ when
	// a decision was made in one session and filed in a later one, and when a
	// reversal takes effect on a different day from the decision it undoes.
	Effective *string `json:"effective"`
	// Decision is the structured claim behind a decision event, parsed at the
	// boundary. It is null for a decision recorded as one line of prose: the
	// one-line form is what a person types, and a review that needs structure
	// can still read the body.
	Decision *Decision `json:"decision"`
}

// Decision is a decision with its parts, so a review can show what was asked,
// what was answered and what it cost rather than one line of prose. Everything
// but Question and Answer is optional because a human types one line and a
// model fills in more; a missing field says what it says and nothing more.
type Decision struct {
	// Question is what the decision settled. A claim with no question behind it
	// cannot be reviewed, because there is nothing to judge it against.
	Question string `json:"question"`
	// Answer is what was decided.
	Answer string `json:"answer"`
	// Source says who decided: a runner name, a model, or the director.
	Source string `json:"source"`
	// Runner and Model name what did the deciding, when a model did.
	Runner string `json:"runner"`
	Model  string `json:"model"`
	// Tokens is what the decision cost, when it was a model that spent them.
	Tokens int `json:"tokens"`
	// Reverses is the event id this decision undoes. A reversal is a new
	// decision rather than an edit of the old one, so the audit reads in order
	// and the original claim stays visible as it was made.
	Reverses int `json:"reverses"`
	// ReversedBy is the event id that undid this decision, or 0 while it stands.
	ReversedBy int `json:"reversed_by"`
}

type Feedback struct {
	ID      int            `json:"id"`
	Text    string         `json:"text"`
	Project *string        `json:"project"`
	Card    *string        `json:"card"`
	Source  FeedbackSource `json:"source"`
	// Work is the piece of work the evidence came from, when it came from one.
	// Evidence that cannot be pointed back at the thing that produced it cannot
	// be audited against it, and a card promoted on that evidence records its
	// decision there.
	Work *string `json:"work"`
	At   string  `json:"at"`
}

type Candidate struct {
	Key   string   `json:"key"`
	Count int      `json:"count"`
	Texts []string `json:"texts"`
}

type Concern struct {
	ID         int     `json:"id"`
	Work       string  `json:"work"`
	Text       string  `json:"text"`
	Resolved   int     `json:"resolved"`
	Decision   *string `json:"decision"`
	At         string  `json:"at"`
	ResolvedAt *string `json:"resolved_at"`
}

type Worktree struct {
	ID      int            `json:"id"`
	Work    string         `json:"work"`
	Path    string         `json:"path"`
	Branch  *string        `json:"branch"`
	Kind    WorktreeKind   `json:"kind"`
	State   WorktreeState  `json:"state"`
	Origin  WorktreeOrigin `json:"origin"`
	Created string         `json:"created"`
}

// TranscriptMark is a point in a session's transcript: the session and how
// many entries it held.
type TranscriptMark struct {
	Session string
	Entries int
}

// ClosesDirectly reports whether a state may go straight to done without passing
// the soft-done gate. Work in these states has not run, or has stopped, and is
// being abandoned rather than completed — a legitimate close that must not be
// made to fake a report, a passing verify and a pull request. Because it skips
// the gate on purpose, every direct close records why, so a closure that came
// through the gate is never later read as a cancellation.
func ClosesDirectly(s State) bool {
	switch s {
	case StateQueued, StateBriefed, StateBlocked:
		return true
	}
	return false
}

// Workspace is one directory this director serves: a repo or a folder of
// work, identified so a client can name it instead of sending a path. A client
// never transmits a filesystem path — it names a workspace, which is what keeps
// a stolen token from becoming filesystem access on the machine.
//
// Creates says the path is a parent under which new workspaces may be made.
// It is permission, held by the machine rather than by the client: a remote
// actor can create a workspace anywhere this director is allowed to create one,
// and nowhere else.
type Workspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Creates bool   `json:"creates"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

// RunState is where a run of a goal got to. A run is a record, not a process:
// it outlives whatever was driving it, which is the only reason an interrupted
// one can be told apart from one that finished.
type RunState string

const (
	// RunRunning means a process was driving it and has not said otherwise. It
	// is a claim, not a fact: a machine that lost power leaves a row saying
	// running with nothing behind it, which is what Recovered is for.
	RunRunning RunState = "running"
	// RunInterrupted means the process went away without deciding anything. A
	// dead battery, a crash and a closed laptop all land here, and none of them
	// is the loop giving up.
	RunInterrupted RunState = "interrupted"
	// RunFinished means the run reached a stop of its own accord, with the stop
	// and the reason recorded beside it.
	RunFinished RunState = "finished"
)

type Conflict struct {
	A     string   `json:"a"`
	B     string   `json:"b"`
	Paths []string `json:"paths"`
}

var Transitions = map[State][]State{
	StateQueued:     {StateBriefed, StateRunning, StateBlocked, StateDone, StateDropped, StatePaused},
	StateBriefed:    {StateRunning, StateQueued, StateBlocked, StateDone, StateDropped, StatePaused},
	StateRunning:    {StateNeedsInput, StateReview, StateBlocked, StateDropped, StateAbandoned, StatePaused},
	StateNeedsInput: {StateRunning, StateBlocked, StateDropped, StateAbandoned, StatePaused},
	StateReview:     {StateSoftDone, StateRunning, StateBlocked, StateDropped, StateAbandoned, StatePaused},
	StatePaused:     {StateQueued, StateBriefed, StateRunning, StateNeedsInput, StateReview, StateBlocked, StateDropped, StateAbandoned},
	StateSoftDone:   {StateDone, StateRunning, StateBlocked, StateDropped, StateAbandoned},
	StateBlocked:    {StateQueued, StateRunning, StateDone, StateDropped, StateAbandoned, StatePaused},
	// Done is not terminal. Work that is finished and then built on again — a
	// goal someone keeps extending — has to come back to life rather than
	// forcing a near-copy of the same work under a new id, which loses the
	// history of what came first. Reopening says what is being worked on, so it
	// is reached through Reopen and not by a bare transition.
	StateDone: {StateRunning},
	// Dropped is terminal: work let go of on purpose stays let go of.
	StateDropped: {},
	// Abandoned is not terminal, but it is nearly so: it records a thing that
	// stopped without shipping, and the only way out is into dropped, because
	// sometimes a thing filed as a failure was a choice all along — a duplicate
	// whose work landed elsewhere, a probe that was never meant to ship. Release
	// is what takes that edge, because reclassifying a failure as a choice is a
	// claim and has to be said out loud. Reopening is not on this list: a goal
	// that never shipped is not a goal being worked on again.
	StateAbandoned: {StateDropped},
}

// Reopenable reports whether work in state s may be worked on again. Only done
// work is. Dropped is a choice and abandoned is a stop, and neither is work that
// is in progress; reopening either would make a deliberate ending reversible by
// accident.
// Message is something a person said to work while it runs. It waits in the
// ledger until the work's session is idle, because a running session handed a
// message would fork rather than read it.
type Message struct {
	ID        int     `json:"id"`
	Work      string  `json:"work"`
	Text      string  `json:"text"`
	At        string  `json:"at"`
	Delivered *string `json:"delivered"`
}

// AtRest reports whether work has come to rest: done, dropped or abandoned.
func AtRest(s State) bool {
	return s == StateDone || s == StateDropped || s == StateAbandoned
}

func Reopenable(s State) bool {
	return s == StateDone
}

// Why a goal is abandoned. Both are facts the ledger can verify, so
// abandonment is detected rather than declared.
const (
	// AbandonNoPR: nothing was ever raised. The work stopped before a PR.
	AbandonNoPR = "no-pr"
	// AbandonUnmerged: a PR was raised and never merged. The work shipped into
	// a branch and stopped there.
	AbandonUnmerged = "unmerged"
)

var (
	WorkKinds       = []WorkKind{WorkTask, WorkEvolution, WorkWorkflow, WorkGoal, WorkRoadmap, WorkItem}
	EventKinds      = []EventKind{EventState, EventReport, EventVerify, EventPr, EventNote, EventSent, EventSpawn, EventAttach, EventQuestion, EventAnswer, EventDecision, EventAbandon}
	FeedbackSources = []FeedbackSource{FeedbackDirector, FeedbackNote, FeedbackAttached}
	LandingKinds    = []LandingKind{LandingCommit, LandingPullRequest}
	WorktreeKinds   = []WorktreeKind{WorktreeShared, WorktreePrivate}
	WorktreeStates  = []WorktreeState{WorktreeActive, WorktreeMerged, WorktreeAbandoned, WorktreeRemoved}
	WorktreeOrigins = []WorktreeOrigin{OriginDirector, OriginAttached}
	GoalTypes       = []GoalType{GoalQuery, GoalBuild, GoalFix, GoalChange, GoalReview}
	AbandonReasons  = []string{AbandonNoPR, AbandonUnmerged}
)

func parse[T ~string](what string, known []T, s string) (T, error) {
	if !slices.Contains(known, T(s)) {
		return "", fmt.Errorf("unknown %s %q", what, s)
	}
	return T(s), nil
}

// ParseState accepts exactly the states Transitions lists.
func ParseState(s string) (State, error) {
	if _, ok := Transitions[State(s)]; !ok {
		return "", fmt.Errorf("unknown work state %q", s)
	}
	return State(s), nil
}

func ParseWorkKind(s string) (WorkKind, error) { return parse("work kind", WorkKinds, s) }

func ParseEventKind(s string) (EventKind, error) { return parse("event kind", EventKinds, s) }

func ParseGoalType(s string) (GoalType, error) { return parse("goal type", GoalTypes, s) }

// ParseAbandonReason accepts only the reasons the ledger can verify. A reason
// nobody can check is not a reason, so it is not spellable.
func ParseAbandonReason(s string) (string, error) {
	return parse("abandon reason", AbandonReasons, s)
}

func ParseFeedbackSource(s string) (FeedbackSource, error) {
	return parse("feedback source", FeedbackSources, s)
}

func ParseWorktreeKind(s string) (WorktreeKind, error) {
	return parse("worktree kind", WorktreeKinds, s)
}

func ParseWorktreeState(s string) (WorktreeState, error) {
	return parse("worktree state", WorktreeStates, s)
}

func ParseWorktreeOrigin(s string) (WorktreeOrigin, error) {
	return parse("worktree origin", WorktreeOrigins, s)
}

// StaleAfter is how long open work may go without a state change or event
// before it is reported stale.
const StaleAfter = 30 * 24 * time.Hour

// TimeLayout is the ledger's timestamp format: UTC with millisecond precision.
const TimeLayout = "2006-01-02T15:04:05.000Z07:00"

// Stale reports whether w is still open and its last activity is older than
// StaleAfter at now.
func Stale(w Work, now time.Time) (bool, error) {
	if w.State == StateDone || w.State == StateDropped {
		return false, nil
	}
	updated, err := time.Parse(TimeLayout, w.Updated)
	if err != nil {
		return false, fmt.Errorf("work %s: updated %q is not a ledger timestamp: %w", w.ID, w.Updated, err)
	}
	return now.Sub(updated) > StaleAfter, nil
}

type IllegalTransition struct {
	From State
	To   State
}

func (e IllegalTransition) Error() string {
	return fmt.Sprintf("illegal transition %s → %s", e.From, e.To)
}

type NotReady struct {
	Missing []string
}

func (e NotReady) Error() string {
	return "not ready for soft-done: " + strings.Join(e.Missing, ", ")
}

func SplitImpact(impact *string) []string {
	if impact == nil {
		return []string{}
	}
	var out []string
	for _, s := range strings.Split(*impact, "\n") {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Two paths clash when one is the other or one contains the other.
func PathsOverlap(a, b string) bool {
	x := strings.TrimRight(a, "/")
	y := strings.TrimRight(b, "/")
	return x == y || strings.HasPrefix(x, y+"/") || strings.HasPrefix(y, x+"/")
}
