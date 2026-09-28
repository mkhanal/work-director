// Package core holds the director's domain types and rules: the work state
// machine, the ledger's value types and the errors the ledger can raise. It
// has no storage and no I/O; the ledger is its only interpreter.
package core

import (
	"fmt"
	"strings"
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
	ID      string
	Project string
	Title   string
	Detail  string
	Kind    WorkKind
	State   State
	Runner  *string
	Session *string
	Ref     *string
	Cwd     *string
	Created string
	Updated string
	Parent  *string
	Heading *string
	Claim   *string
	Impact  *string
}

type Event struct {
	ID   int
	Work string
	Kind EventKind
	Body string
	At   string
}

type Feedback struct {
	ID      int
	Text    string
	Project *string
	Card    *string
	Source  FeedbackSource
	At      string
}

type Candidate struct {
	Key   string
	Count int
	Texts []string
}

type Concern struct {
	ID         int
	Work       string
	Text       string
	Resolved   int
	Decision   *string
	At         string
	ResolvedAt *string
}

type Worktree struct {
	ID      int
	Work    string
	Path    string
	Branch  *string
	Kind    WorktreeKind
	State   WorktreeState
	Created string
}

type Conflict struct {
	A     string
	B     string
	Paths []string
}

var Transitions = map[State][]State{
	StateQueued:     {StateBriefed, StateRunning, StateBlocked, StateDropped},
	StateBriefed:    {StateRunning, StateQueued, StateBlocked, StateDropped},
	StateRunning:    {StateNeedsInput, StateReview, StateBlocked, StateDropped},
	StateNeedsInput: {StateRunning, StateBlocked, StateDropped},
	StateReview:     {StateSoftDone, StateRunning, StateBlocked, StateDropped},
	StateSoftDone:   {StateDone, StateRunning, StateDropped},
	StateBlocked:    {StateQueued, StateRunning, StateDropped},
	StateDone:       {},
	StateDropped:    {},
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
