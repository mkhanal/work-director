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
)

type WorkKind string

const (
	WorkTask      WorkKind = "task"
	WorkEvolution WorkKind = "evolution"
	WorkWorkflow  WorkKind = "workflow"
	WorkGoal      WorkKind = "goal"
	WorkEpic      WorkKind = "epic"
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
)

// A goal is an epic-like: work broken into tasks under one branch. Goals stay a
// distinct kind so `wd status` and the UI can surface them as the entry points
// they are.
func IsEpic(kind WorkKind) bool { return kind == WorkGoal || kind == WorkEpic }

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
	ID      int           `json:"id"`
	Work    string        `json:"work"`
	Path    string        `json:"path"`
	Branch  *string       `json:"branch"`
	Kind    WorktreeKind  `json:"kind"`
	State   WorktreeState `json:"state"`
	Created string        `json:"created"`
}

type Conflict struct {
	A     string   `json:"a"`
	B     string   `json:"b"`
	Paths []string `json:"paths"`
}

var Transitions = map[State][]State{
	StateQueued:     {StateBriefed, StateRunning, StateBlocked, StateDone, StateDropped},
	StateBriefed:    {StateRunning, StateQueued, StateBlocked, StateDone, StateDropped},
	StateRunning:    {StateNeedsInput, StateReview, StateBlocked, StateDropped},
	StateNeedsInput: {StateRunning, StateBlocked, StateDropped},
	StateReview:     {StateSoftDone, StateRunning, StateBlocked, StateDropped},
	StateSoftDone:   {StateDone, StateRunning, StateBlocked, StateDropped},
	StateBlocked:    {StateQueued, StateRunning, StateDone, StateDropped},
	StateDone:       {},
	StateDropped:    {},
}

var (
	WorkKinds       = []WorkKind{WorkTask, WorkEvolution, WorkWorkflow, WorkGoal, WorkEpic}
	EventKinds      = []EventKind{EventState, EventReport, EventVerify, EventPr, EventNote, EventSent, EventSpawn, EventAttach, EventQuestion, EventAnswer, EventDecision}
	FeedbackSources = []FeedbackSource{FeedbackDirector, FeedbackNote, FeedbackAttached}
	WorktreeKinds   = []WorktreeKind{WorktreeShared, WorktreePrivate}
	WorktreeStates  = []WorktreeState{WorktreeActive, WorktreeMerged, WorktreeAbandoned}
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

func ParseFeedbackSource(s string) (FeedbackSource, error) {
	return parse("feedback source", FeedbackSources, s)
}

func ParseWorktreeKind(s string) (WorktreeKind, error) {
	return parse("worktree kind", WorktreeKinds, s)
}

func ParseWorktreeState(s string) (WorktreeState, error) {
	return parse("worktree state", WorktreeStates, s)
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
