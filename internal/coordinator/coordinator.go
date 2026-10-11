// Package coordinator drives a goal's planning and task sessions: the
// planning brief, parsing the planner's reply, answering questions from
// what the ledger already knows, and one coordination pass over the
// children.
package coordinator

import (
	"fmt"
	"regexp"
	"strings"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
)

// GoalPlanBrief is the planning model's brief: analyse the goal and reply
// with only a headed task list, editing nothing.
func GoalPlanBrief(goal core.Work, p *project.Project) string {
	verify := "none listed"
	if len(p.Verify) > 0 {
		quoted := make([]string, len(p.Verify))
		for i, v := range p.Verify {
			quoted[i] = "`" + v + "`"
		}
		verify = strings.Join(quoted, ", ")
	}
	detail := ""
	if goal.Detail != "" {
		detail = "\n" + goal.Detail
	}
	return fmt.Sprintf(`You are the director's planning model for the project %s (%s).

Analyse this goal and decompose it into concrete, independent tasks. Read the repo to ground yourself, but do NOT edit, create or open any files; analysis only. Reply with ONLY the task list in exactly this shape — one line per task, grouped under a heading:

## <heading>
- [ ] <task title>
- [ ] <task title>

Cover what must change and be verified; keep tasks small enough that one session can finish each. Project verify commands: %s. The goal: %s%s`,
		p.Name, p.Path, verify, goal.Title, detail)
}

// PlanTask is one parsed planner reply line: the heading group it sits
// under and its title.
type PlanTask struct {
	Heading string
	Title   string
}

var headingRe = regexp.MustCompile(`^##\s+(.+)`)
var taskRe = regexp.MustCompile(`^[-*]\s+\[[ xX]\]\s*(.+)`)

// ParsePlan parses the planner session's reply into tasks. Heading lines
// open a group; `- [ ]` lines are tasks.
func ParsePlan(texts []string) []PlanTask {
	const noHeading = "(no heading)"
	var out []PlanTask
	heading := noHeading
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			if h := headingRe.FindStringSubmatch(line); h != nil {
				heading = strings.TrimSpace(h[1])
				continue
			}
			if t := taskRe.FindStringSubmatch(line); t != nil {
				out = append(out, PlanTask{Heading: heading, Title: strings.TrimSpace(t[1])})
			}
		}
	}
	return out
}

var nonTokenRe = regexp.MustCompile(`[^a-z0-9\s-]`)

// tokens are the significant words of a string: lowercase, punctuation
// dropped, words of four or more characters.
func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.Fields(nonTokenRe.ReplaceAllString(s, " ")) {
		if len(t) >= 4 {
			out[t] = true
		}
	}
	return out
}

func countShared(a, b map[string]bool) int {
	n := 0
	for t := range a {
		if b[t] {
			n++
		}
	}
	return n
}

// KnownAnswer finds a known answer to a question: a resolved concern's
// decision or a `decision` event on the known work that shares ≥2
// significant words with the question (exact containment wins). It returns
// "" when nothing is known.
func KnownAnswer(question string, known []core.Work, l *ledger.Ledger) (string, error) {
	var candidates []string
	for _, w := range known {
		concerns, err := l.Concerns(&w.ID)
		if err != nil {
			return "", err
		}
		for _, c := range concerns {
			if c.Resolved == 1 && c.Decision != nil {
				candidates = append(candidates, *c.Decision)
			}
		}
		events, err := l.Events(w.ID, nil)
		if err != nil {
			return "", err
		}
		for _, e := range events {
			if e.Kind == core.EventDecision {
				candidates = append(candidates, e.Body)
			}
		}
	}
	lower := strings.ToLower(question)
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c), lower) {
			return c, nil
		}
	}
	q := tokens(lower)
	best := ""
	bestN := 1
	for _, c := range candidates {
		if n := countShared(tokens(strings.ToLower(c)), q); n > bestN {
			bestN = n
			best = c
		}
	}
	return best, nil
}

// PassResult is one coordination pass: the children answered, escalated,
// harvested for review or blocked, and the ones still waiting.
type PassResult struct {
	Answered  []string `json:"answered"`
	Escalated []string `json:"escalated"`
	Reviewed  []string `json:"reviewed"`
	Blocked   []string `json:"blocked"`
	Waiting   []string `json:"waiting"`
	Delivered []string `json:"delivered"`
}

var askRe = regexp.MustCompile(`ASK:\s*(\S.*)`)
var statusRe = regexp.MustCompile(`STATUS:\s*(DONE|BLOCKED|NEEDS-INPUT)`)

// Handle is work's live session: its runner (else its goal's, else the
// project's), its session (else its claim), and its directory (its own cwd,
// else its goal's active shared worktree, else the project's path). ok is
// false when work has neither session nor claim.
// Where resolves the runner and directory a work item is worked in: its own
// runner else its goal's else the project's, and its own cwd else its goal's
// active shared worktree else the project path. It is how every command reaches
// the session a work item actually runs in, so a new call made on a work item's
// behalf — a reflection, a judgement — runs where that work runs, not where the
// project lives.
func Where(l *ledger.Ledger, w core.Work, p *project.Project) (string, string, error) {
	runnerName := p.Runner
	cwd := p.Path
	if w.Parent != nil {
		goal, err := l.Get(*w.Parent)
		if err != nil {
			return "", "", err
		}
		if goal.Runner != nil {
			runnerName = *goal.Runner
		}
		wts, err := l.Worktrees(goal.ID)
		if err != nil {
			return "", "", err
		}
		for _, wt := range wts {
			if wt.Kind == core.WorktreeShared && wt.State == core.WorktreeActive {
				cwd = wt.Path
			}
		}
	}
	if w.Runner != nil {
		runnerName = *w.Runner
	}
	if w.Cwd != nil {
		cwd = *w.Cwd
	}
	return runnerName, cwd, nil
}

func Handle(l *ledger.Ledger, w core.Work, p *project.Project) (runner.Handle, bool, error) {
	session := w.Session
	if session == nil {
		session = w.Claim
	}
	if session == nil {
		return runner.Handle{}, false, nil
	}
	runnerName, cwd, err := Where(l, w, p)
	if err != nil {
		return runner.Handle{}, false, err
	}
	return runner.Handle{Runner: runnerName, Session: *session, Ref: w.Ref, Cwd: cwd}, true, nil
}

// Send continues work's session and records the ref the runner returns, so a
// status check reaches the process now serving the session. Only the ref is
// recorded: a runner and directory work does not set itself keep following
// its goal.
func Send(l *ledger.Ledger, id string, r runner.Runner, h runner.Handle, text string) error {
	if err := r.Send(&h, text); err != nil {
		return err
	}
	return l.SetRef(id, h.Ref)
}

// Deliver hands work's queued messages to its session, oldest first, when the
// session is not running, and returns how many it delivered. A running session
// keeps them queued: handing it a message would start a copy rather than reach
// the session that is working.
func Deliver(l *ledger.Ledger, w core.Work, r runner.Runner, h runner.Handle) (int, error) {
	pending, err := l.Undelivered(w.ID)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	status, err := r.Status(h)
	if err != nil {
		return 0, err
	}
	if status == runner.StatusRunning {
		return 0, nil
	}
	// One send: the session is busy with the first message the moment it
	// arrives, and a second send would start a copy of it.
	texts := make([]string, 0, len(pending))
	for _, m := range pending {
		texts = append(texts, m.Text)
	}
	if err := Send(l, w.ID, r, h, strings.Join(texts, "\n\n")); err != nil {
		return 0, err
	}
	for _, m := range pending {
		if err := l.MarkDelivered(m.ID); err != nil {
			return 0, err
		}
	}
	return len(pending), nil
}

// Outcome is what one coordination pass did with a work item.
type Outcome string

const (
	Answered  Outcome = "answered"
	Escalated Outcome = "escalated"
	Reviewed  Outcome = "reviewed"
	Blocked   Outcome = "blocked"
	Waiting   Outcome = "waiting"
)

// Known is the work whose decisions can answer w's questions: its goal and
// the goal's tasks, or w alone when it has no goal.
func Known(l *ledger.Ledger, w core.Work) ([]core.Work, error) {
	if w.Parent == nil {
		return []core.Work{w}, nil
	}
	goal, err := l.Get(*w.Parent)
	if err != nil {
		return nil, err
	}
	children, err := l.Tasks(goal.ID)
	if err != nil {
		return nil, err
	}
	return append([]core.Work{goal}, children...), nil
}

// CoordinateOnce runs one coordination pass over the goal's children. It
// does not spawn.
func CoordinateOnce(goal core.Work, p *project.Project, l *ledger.Ledger, resolve func(name string) (runner.Runner, error)) (PassResult, error) {
	children, err := l.Tasks(goal.ID)
	if err != nil {
		return PassResult{}, err
	}
	known := append([]core.Work{goal}, children...)
	res := PassResult{
		Answered:  []string{},
		Escalated: []string{},
		Reviewed:  []string{},
		Blocked:   []string{},
		Waiting:   []string{},
		Delivered: []string{},
	}
	listed := map[Outcome]*[]string{
		Answered:  &res.Answered,
		Escalated: &res.Escalated,
		Reviewed:  &res.Reviewed,
		Blocked:   &res.Blocked,
		Waiting:   &res.Waiting,
	}
	for _, w := range children {
		if w.State != core.StateRunning && w.State != core.StateNeedsInput {
			continue
		}
		h, ok, err := Handle(l, w, p)
		if err != nil {
			return PassResult{}, err
		}
		if !ok {
			res.Waiting = append(res.Waiting, w.ID)
			continue
		}
		r, err := resolve(h.Runner)
		if err != nil {
			return PassResult{}, err
		}
		sent, err := Deliver(l, w, r, h)
		if err != nil {
			return PassResult{}, err
		}
		if sent > 0 {
			res.Delivered = append(res.Delivered, w.ID)
			// The executor was waiting on a person and has now heard from one.
			if w.State == core.StateNeedsInput {
				if w, err = l.Transition(w.ID, core.StateRunning); err != nil {
					return PassResult{}, err
				}
			}
			continue
		}
		texts, err := runner.Transcript(r, h)
		if err != nil {
			return PassResult{}, err
		}
		out, err := Coordinate(w, known, l, r, h, texts)
		if err != nil {
			return PassResult{}, err
		}
		*listed[out] = append(*listed[out], w.ID)
	}
	return res, nil
}

// Coordinate runs one coordination pass over running or needs-input work,
// given its session's transcript: answer a question from what the known
// work decided, escalate an unknown one and a NEEDS-INPUT report to
// needs-input, harvest a DONE/BLOCKED report. A question or report already
// filed is not acted on again.
func Coordinate(w core.Work, known []core.Work, l *ledger.Ledger, r runner.Runner, h runner.Handle, texts []string) (Outcome, error) {
	last := ""
	if len(texts) > 0 {
		last = texts[len(texts)-1]
	}
	// The transcript keeps ending in a question or report until the
	// executor writes again, so the entry already filed is not acted on
	// twice; the same words in a later entry or another session are new.
	at := core.TranscriptMark{Session: h.Session, Entries: len(texts)}
	prev, ok, err := l.Filed(w.ID)
	if err != nil {
		return "", err
	}
	filed := ok && prev == at
	seen := Waiting
	if w.State == core.StateNeedsInput {
		seen = Escalated
	}
	escalate := func() (Outcome, error) {
		if w.State != core.StateNeedsInput {
			if _, err := l.Transition(w.ID, core.StateNeedsInput); err != nil {
				return "", err
			}
		}
		return Escalated, nil
	}
	if ask := askRe.FindStringSubmatch(last); ask != nil {
		if filed {
			return seen, nil
		}
		question := strings.TrimSpace(ask[1])
		if err := l.File(w.ID, core.EventQuestion, question, at); err != nil {
			return "", err
		}
		answer, err := KnownAnswer(question, known, l)
		if err != nil {
			return "", err
		}
		if answer == "" {
			return escalate()
		}
		if err := Send(l, w.ID, r, h, answer); err != nil {
			return "", err
		}
		if _, err := l.AddEvent(w.ID, core.EventAnswer, answer); err != nil {
			return "", err
		}
		if w.State == core.StateNeedsInput {
			if _, err := l.Transition(w.ID, core.StateRunning); err != nil {
				return "", err
			}
		}
		return Answered, nil
	}
	status := statusRe.FindStringSubmatch(last)
	if status == nil {
		return Waiting, nil
	}
	report := status[1] + "\n" + last
	if status[1] == "NEEDS-INPUT" {
		if filed {
			return seen, nil
		}
		if err := l.File(w.ID, core.EventReport, report, at); err != nil {
			return "", err
		}
		return escalate()
	}
	return FileReport(l, w, status[1], report)
}

// Notice files the question a session's transcript ends on and moves its
// work to needs-input, answering nothing, so a person sees it where work waits
// on them. A question already filed at that point is not filed again, and the
// next coordination pass reads it as escalated. It reports whether it filed.
func Notice(l *ledger.Ledger, w core.Work, h runner.Handle, texts []string) (bool, error) {
	if w.State != core.StateRunning || len(texts) == 0 {
		return false, nil
	}
	ask := askRe.FindStringSubmatch(texts[len(texts)-1])
	if ask == nil {
		return false, nil
	}
	at := core.TranscriptMark{Session: h.Session, Entries: len(texts)}
	if prev, ok, err := l.Filed(w.ID); err != nil || (ok && prev == at) {
		return false, err
	}
	if err := l.File(w.ID, core.EventQuestion, strings.TrimSpace(ask[1]), at); err != nil {
		return false, err
	}
	if _, err := l.Transition(w.ID, core.StateNeedsInput); err != nil {
		return false, err
	}
	return true, nil
}

// FileReport files a STATUS report on work and moves it to the state that
// status names: DONE to review, BLOCKED to blocked. It is the one place a
// report moves work, whether the report was read from a session's transcript or
// supplied as text, so a work item built in a session that has no executor
// cannot reach a different ending than one that has.
//
// NEEDS-INPUT is not here: it is not a report that moves work forward, it is an
// executor saying it cannot go on, and it has to be filed at the transcript
// point it was found at, so the escalation is a place in the record.
func FileReport(l *ledger.Ledger, w core.Work, status, report string) (Outcome, error) {
	// A report is a thing that has finished, so the thing has run. A row that
	// still says queued or briefed is stale rather than true — nothing spawns
	// work the director builds itself, so nothing has ever moved it — and a
	// ledger that refuses to believe a report it was just handed is a ledger
	// whose state and whose record disagree. Needs-input goes back to running
	// first because it has already run and stopped, which is a different fact
	// with the same consequence.
	if w.State == core.StateNeedsInput || w.State == core.StateQueued || w.State == core.StateBriefed {
		if _, err := l.Transition(w.ID, core.StateRunning); err != nil {
			return "", err
		}
	}
	if _, err := l.AddEvent(w.ID, core.EventReport, report); err != nil {
		return "", err
	}
	target := core.StateReview
	if status == "BLOCKED" {
		target = core.StateBlocked
	}
	// A work already where the report says it should be does not move. Filing a
	// report is filing a fact, and a second one is a new fact even when it
	// changes nothing — refusing it would lose a record to save a transition
	// that has nowhere to go.
	if w.State != target {
		if _, err := l.Transition(w.ID, target); err != nil {
			return "", err
		}
	}
	if target == core.StateBlocked {
		return Blocked, nil
	}
	return Reviewed, nil
}
