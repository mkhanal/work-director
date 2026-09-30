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
}

type Event struct {
	ID   int       `json:"id"`
	Work string    `json:"work"`
	Kind EventKind `json:"kind"`
	Body string    `json:"body"`
	At   string    `json:"at"`
}

type Feedback struct {
	ID      int            `json:"id"`
	Text    string         `json:"text"`
	Project *string        `json:"project"`
	Card    *string        `json:"card"`
	Source  FeedbackSource `json:"source"`
	At      string         `json:"at"`
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

type Conflict struct {
	A     string   `json:"a"`
	B     string   `json:"b"`
	Paths []string `json:"paths"`
}

var Transitions = map[State][]State{
	StateQueued:     {StateBriefed, StateRunning, StateBlocked, StateDone, StateDropped, StateAbandoned},
	StateBriefed:    {StateRunning, StateQueued, StateBlocked, StateDone, StateDropped, StateAbandoned},
	StateRunning:    {StateNeedsInput, StateReview, StateBlocked, StateDropped, StateAbandoned},
	StateNeedsInput: {StateRunning, StateBlocked, StateDropped, StateAbandoned},
	StateReview:     {StateSoftDone, StateRunning, StateBlocked, StateDropped, StateAbandoned},
	StateSoftDone:   {StateDone, StateRunning, StateBlocked, StateDropped, StateAbandoned},
	StateBlocked:    {StateQueued, StateRunning, StateDone, StateDropped, StateAbandoned},
	StateDone:       {},
	StateDropped:    {},
	// Abandoned is terminal like done and dropped: work comes to rest there.
	// A reversal is a new decision event against the same goal, not a
	// transition back out.
	StateAbandoned: {},
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
