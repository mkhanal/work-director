// Package core holds the director's domain types and rules: the work state
// machine, the ledger's value types and the errors the ledger can raise. It
// has no storage and no I/O; the ledger is its only interpreter.
package core

import (
	"fmt"
	"regexp"
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
	// StateSuperseded is work a course change abandoned: the plan moved on
	// without it. Its history and anything it produced stay readable.
	StateSuperseded State = "superseded"
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
	// EventLook is the director asking a goal's supervisor to look; the body
	// says what changed since it last looked.
	EventLook EventKind = "look"
	// EventSupervised is a supervisor's reply carried out: what it said, what
	// was done and what was refused.
	EventSupervised EventKind = "supervised"
	// EventCourse is a supervisor's verdict on whether its goal is on course:
	// "<verdict>: <why>", or "unstated" when its reply gave none.
	EventCourse EventKind = "course"
	// EventJudged is the director's judgement of a sub-goal's done-when:
	// "met: <why>" or "missed: <what is missing>".
	EventJudged EventKind = "judged"
	// EventAttachment is a file a person attached to what they sent: the body
	// is the stored copy's absolute path, the payload its Attachment.
	EventAttachment EventKind = "attachment"
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

// FinishSource says where a goal's finish line came from: the goal's own
// meaning, the repo it runs in, the person's taste, or the person asked.
type FinishSource string

const (
	FinishFromGoal   FinishSource = "goal"
	FinishFromRepo   FinishSource = "repo"
	FinishFromTaste  FinishSource = "taste"
	FinishFromPerson FinishSource = "person"
)

// FinishLine is what a goal hands back when it is done.
type FinishLine string

const (
	// FinishDiscovery is a findings report; no code changes.
	FinishDiscovery FinishLine = "discovery"
	// FinishResearch is a written comparison and a pick; no code changes.
	FinishResearch FinishLine = "research"
	// FinishLocalBuild is built and runnable, with how to run it; no PR.
	FinishLocalBuild FinishLine = "local build"
	// FinishPR is a PR open with checks green and the done-when met.
	FinishPR       FinishLine = "complete PR"
	FinishMerged   FinishLine = "merged"
	FinishReleased FinishLine = "released"
)

// ChangesCode reports whether reaching f changes the repository's code.
func (f FinishLine) ChangesCode() bool { return f != FinishDiscovery && f != FinishResearch }

// FinishReached says whether this run's events show the finish line reached, and if not, what is missing.
// Discovery and research need a DONE report; a local build also a passing
// verify; a complete PR also a landing; merged a commit landing; released
// also a note starting "released:". Report and verify are the latest of
// their kind.
func FinishReached(f FinishLine, run []Event) (bool, string) {
	var report, verify *Event
	var landings []Landing
	released := false
	for i := range run {
		switch e := run[i]; e.Kind {
		case EventReport:
			report = &run[i]
		case EventVerify:
			verify = &run[i]
		case EventPr:
			landings = append(landings, ParseLanding(e.Body))
		case EventNote:
			released = released || strings.HasPrefix(e.Body, "released:")
		}
	}
	landed := func(k LandingKind) bool {
		return slices.ContainsFunc(landings, func(l Landing) bool { return l.Kind == k })
	}
	var missing []string
	if report == nil || !strings.HasPrefix(report.Body, "DONE") {
		missing = append(missing, "DONE report")
	}
	if f.ChangesCode() && (verify == nil || !strings.HasPrefix(verify.Body, "pass")) {
		missing = append(missing, "passing verify")
	}
	switch f {
	case FinishPR:
		if !landed(LandingPullRequest) && !landed(LandingCommit) {
			missing = append(missing, "pull request")
		}
	case FinishMerged, FinishReleased:
		if !landed(LandingCommit) {
			missing = append(missing, "commit landing")
		}
	}
	if f == FinishReleased && !released {
		missing = append(missing, "released note")
	}
	return len(missing) == 0, strings.Join(missing, ", ")
}

// NeedsPR reports whether reaching f takes a pull request.
func (f FinishLine) NeedsPR() bool { return f == FinishPR || f == FinishMerged || f == FinishReleased }

// StepState is where one of an agent's own steps stands.
type StepState string

const (
	StepTodo    StepState = "todo"
	StepDoing   StepState = "doing"
	StepDone    StepState = "done"
	StepDropped StepState = "dropped"
)

// Step is one step of an agent's own plan for its work, numbered from 1. Steps
// are never handed out as work: each plan or report replaces the whole list.
type Step struct {
	N     int       `json:"n"`
	Text  string    `json:"text"`
	State StepState `json:"state"`
}

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
	// DoneWhen is what makes the work done, checkable from a report, verify
	// output or a PR. Nil until set. It and the finish line stay off the work
	// object's wire schema, whose keys are fixed; views carry them.
	DoneWhen *string `json:"-"`
	// FinishLine is what is handed back, and FinishSource where that came
	// from; both are nil until decided, and set together.
	FinishLine   *FinishLine   `json:"-"`
	FinishSource *FinishSource `json:"-"`
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
	// Change is what a question the director filed carries out when it is
	// answered with its first choice; nil on any other event. It stays off the
	// event's wire schema, whose keys are fixed.
	Change *Change `json:"-"`
	// Reasoning is why a planner broke a goal down as it did, on the note
	// planning files; nil on any other event.
	Reasoning *Reasoning `json:"-"`
	// Attachment is the file an attachment event stored; nil on any other
	// event.
	Attachment *Attachment `json:"-"`
}

// Attachment is one file a person attached, as copied into the director's
// own store: the name it was given, the copy's absolute path, its size in
// bytes and its media type.
type Attachment struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
}

// AttachedBlock is what an agent reads after a message that carried files:
// each stored path with its media type and size. Without files it is "".
func AttachedBlock(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	lines := []string{"Attached files (read them from these paths):"}
	for _, a := range atts {
		lines = append(lines, fmt.Sprintf("- %s (%s, %d bytes)", a.Path, a.MediaType, a.Size))
	}
	return strings.Join(lines, "\n")
}

// WithAttached is text as an agent reads it: the person's words, then the
// block naming what they attached.
func WithAttached(text string, atts []Attachment) string {
	block := AttachedBlock(atts)
	switch {
	case block == "":
		return text
	case strings.TrimSpace(text) == "":
		return block
	}
	return text + "\n\n" + block
}

// Reasoning is a planner's account of a breakdown: why it is broken down so,
// what the goal does not ask for and was left out, and how a person would
// judge it done. A part the planner did not give is nil.
type Reasoning struct {
	Why     *string `json:"why"`
	LeftOut *string `json:"left_out"`
	JudgeBy *string `json:"judge_by"`
}

// ReasoningPrefix opens the note planning files with its planner's reasoning.
const ReasoningPrefix = "reasoning: "

// Change is what answering a director's question with its first choice
// carries out: a supervisor's plan changes, as its action lines, and a
// proposed top goal, when Title is set.
type Change struct {
	Actions  []string   `json:"actions,omitempty"`
	Title    string     `json:"title,omitempty"`
	DoneWhen string     `json:"done_when,omitempty"`
	Finish   FinishLine `json:"finish,omitempty"`
	// Found is what was found that calls for a proposed top-goal change.
	Found string `json:"found,omitempty"`
	// Sources is what each source of a finish line said, on the question a
	// planner unsure where a goal finishes leaves.
	Sources *FinishSaid `json:"sources,omitempty"`
}

// FinishSaid is what the goal's wording, the repository and the person's
// taste each said about where a goal finishes; one that said nothing is nil.
type FinishSaid struct {
	Goal  *string `json:"goal"`
	Repo  *string `json:"repo"`
	Taste *string `json:"taste"`
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

// Who answered a question, as an answer event's decision records it: the
// director, from what was already decided or by its judgement, or the person.
const (
	AnsweredByDirector = "director"
	AnsweredByPerson   = "person"
)

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
	// Dropped has no edge a bare transition may take: work let go of comes
	// back only through Restore, to the state it was dropped from, with
	// everything the same drop let go of.
	StateDropped: {},
	// Abandoned is not terminal, but it is nearly so: it records a thing that
	// stopped without shipping, and the only way out is into dropped, because
	// sometimes a thing filed as a failure was a choice all along — a duplicate
	// whose work landed elsewhere, a probe that was never meant to ship. Release
	// is what takes that edge, because reclassifying a failure as a choice is a
	// claim and has to be said out loud. Reopening is not on this list: a goal
	// that never shipped is not a goal being worked on again.
	StateAbandoned: {StateDropped},
	// Superseded is terminal. It is reached only through Supersede, which
	// files why the course changed; a later course that wants the work again
	// plans it anew.
	StateSuperseded: {},
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

// AtRest reports whether work has come to rest: done, dropped, abandoned or
// superseded.
func AtRest(s State) bool {
	return s == StateDone || s == StateDropped || s == StateAbandoned || s == StateSuperseded
}

// Settled reports whether work no longer counts as open to its parent: done,
// dropped or superseded. Abandoned is not settled: it stopped without
// shipping, and its parent still owes that work.
func Settled(s State) bool {
	return s == StateDone || s == StateDropped || s == StateSuperseded
}

// Judgement is the body of an EventJudged: "met: <text>" or "missed: <text>".
func Judgement(met bool, text string) string {
	if met {
		return "met: " + text
	}
	return "missed: " + text
}

// JudgedMet reports whether the latest EventJudged in evs judged met.
func JudgedMet(evs []Event) bool {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == EventJudged {
			return strings.HasPrefix(evs[i].Body, "met: ")
		}
	}
	return false
}

// ProposedPrefix opens the note a breakdown awaiting approval leaves on its
// goal, and ApprovedPrefix the decision that approves it.
const (
	ProposedPrefix = "proposed: "
	ApprovedPrefix = "approved: "
)

// Proposed reports whether goal's latest breakdown, read from events, waits
// for approval: a proposed note not followed by a plan note or an approval.
func Proposed(goal string, events []Event) bool {
	pending := false
	for _, e := range events {
		if e.Work != goal {
			continue
		}
		switch {
		case e.Kind == EventNote && strings.HasPrefix(e.Body, ProposedPrefix):
			pending = true
		case e.Kind == EventNote && strings.HasPrefix(e.Body, "plan: "),
			e.Kind == EventDecision && strings.HasPrefix(e.Body, ApprovedPrefix):
			pending = false
		}
	}
	return pending
}

// RollupState is a nested goal's state from its children's: needs-input if
// any child needs input; else blocked if any is blocked; else running if any
// is briefed, running, review or soft-done; else done if every child is
// settled and at least one is done; else superseded if every child is
// superseded; else queued. No children: queued.
func RollupState(children []State) State {
	has := func(states ...State) bool {
		return slices.ContainsFunc(children, func(s State) bool { return slices.Contains(states, s) })
	}
	all := func(f func(State) bool) bool {
		return len(children) > 0 && !slices.ContainsFunc(children, func(s State) bool { return !f(s) })
	}
	switch {
	case has(StateNeedsInput):
		return StateNeedsInput
	case has(StateBlocked):
		return StateBlocked
	case has(StateBriefed, StateRunning, StateReview, StateSoftDone):
		return StateRunning
	case all(Settled) && has(StateDone):
		return StateDone
	case all(func(s State) bool { return s == StateSuperseded }):
		return StateSuperseded
	default:
		return StateQueued
	}
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
	EventKinds      = []EventKind{EventState, EventReport, EventVerify, EventPr, EventNote, EventSent, EventSpawn, EventAttach, EventQuestion, EventAnswer, EventDecision, EventAbandon, EventLook, EventSupervised, EventCourse, EventJudged, EventAttachment}
	FeedbackSources = []FeedbackSource{FeedbackDirector, FeedbackNote, FeedbackAttached}
	LandingKinds    = []LandingKind{LandingCommit, LandingPullRequest}
	WorktreeKinds   = []WorktreeKind{WorktreeShared, WorktreePrivate}
	WorktreeStates  = []WorktreeState{WorktreeActive, WorktreeMerged, WorktreeAbandoned, WorktreeRemoved}
	WorktreeOrigins = []WorktreeOrigin{OriginDirector, OriginAttached}
	GoalTypes       = []GoalType{GoalQuery, GoalBuild, GoalFix, GoalChange, GoalReview}
	AbandonReasons  = []string{AbandonNoPR, AbandonUnmerged}
	FinishSources   = []FinishSource{FinishFromGoal, FinishFromRepo, FinishFromTaste, FinishFromPerson}
	StepStates      = []StepState{StepTodo, StepDoing, StepDone, StepDropped}
	FinishLines     = []FinishLine{FinishDiscovery, FinishResearch, FinishLocalBuild, FinishPR, FinishMerged, FinishReleased}
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

func ParseStepState(s string) (StepState, error) { return parse("step state", StepStates, s) }

// ParseFinishLine reads a finish line in any case, trimmed, and names the
// known lines when it refuses one.
func ParseFinishLine(s string) (FinishLine, error) {
	for _, f := range FinishLines {
		if strings.EqualFold(strings.TrimSpace(s), string(f)) {
			return f, nil
		}
	}
	names := make([]string, len(FinishLines))
	for i, f := range FinishLines {
		names[i] = string(f)
	}
	return "", fmt.Errorf("unknown finish line %q: one of %s", s, strings.Join(names, ", "))
}

func ParseFinishSource(s string) (FinishSource, error) {
	return parse("finish source", FinishSources, s)
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
	if Settled(w.State) {
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

// Clipped is the first line of text cut at a word to at most n characters.
func Clipped(text string, n int) string {
	line, _, _ := strings.Cut(text, "\n")
	runes := []rune(strings.TrimSpace(line))
	if len(runes) <= n {
		return string(runes)
	}
	cut := string(runes[:n])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}

// Report is a task's report read into its parts: the verdict and what was
// said with it, the files it names, and each named section in order.
type Report struct {
	Verdict  string          `json:"verdict"`
	Said     string          `json:"said"`
	Files    []string        `json:"files"`
	FileNote string          `json:"file_note"`
	Sections []ReportSection `json:"sections"`
}

// ReportSection is one named part of a report, such as VERIFY or NOTES.
type ReportSection struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

var (
	reportNameRe   = regexp.MustCompile(`^([A-Z][A-Z -]{1,24}):\s*(.*)$`)
	reportStatusRe = regexp.MustCompile(`^(DONE|BLOCKED|NEEDS-INPUT)\b\s*(.*)$`)
	reportPathRe   = regexp.MustCompile(`^[^\s]*[/.][^\s]*$`)
)

// ParseReport reads a report as executors write it: a STATUS line, then
// named lines (FILES, VERIFY, PR, NOTES...) each running to the next.
func ParseReport(body string) Report {
	r := Report{Files: []string{}, Sections: []ReportSection{}}
	var files string
	current := -1
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if i == 0 && reportStatusRe.MatchString(trimmed) && !strings.Contains(trimmed, ":") {
			continue
		}
		m := reportNameRe.FindStringSubmatch(trimmed)
		switch {
		case m != nil && m[1] == "STATUS":
			if s := reportStatusRe.FindStringSubmatch(m[2]); s != nil {
				r.Verdict = strings.ToLower(s[1])
				r.Said = strings.TrimSpace(s[2])
			}
			current = -1
		case m != nil && m[1] == "FILES":
			files = m[2]
			current = -2
		case m != nil:
			r.Sections = append(r.Sections, ReportSection{Name: m[1], Text: m[2]})
			current = len(r.Sections) - 1
		case current >= 0:
			r.Sections[current].Text += "\n" + line
		case current == -1 && trimmed != "":
			r.Said = strings.TrimSpace(r.Said + "\n" + line)
		case current == -2 && trimmed != "":
			files += " " + trimmed
		}
	}
	for i := range r.Sections {
		r.Sections[i].Text = strings.TrimSpace(r.Sections[i].Text)
	}
	r.Files, r.FileNote = reportFiles(files)
	return r
}

// reportFiles splits a FILES line into the paths it names and what else it
// says: a note in brackets, or words that are not a path.
func reportFiles(text string) ([]string, string) {
	paths := []string{}
	var notes []string
	if open := strings.Index(text, " ("); open >= 0 {
		notes = append(notes, strings.Trim(strings.TrimSpace(text[open:]), "()"))
		text = text[:open]
	}
	for _, part := range strings.Split(text, ",") {
		part = strings.Trim(strings.TrimSpace(part), "`")
		switch {
		case part == "":
		case reportPathRe.MatchString(part):
			paths = append(paths, part)
		default:
			notes = append(notes, part)
		}
	}
	return paths, strings.Join(notes, "; ")
}
