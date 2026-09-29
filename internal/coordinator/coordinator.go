// Package coordinator drives an epic's planning and task sessions: the
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

// EpicPlanBrief is the planning model's brief: analyse the goal and reply
// with only a headed task list, editing nothing.
func EpicPlanBrief(epic core.Work, p *project.Project) string {
	verify := "none listed"
	if len(p.Verify) > 0 {
		quoted := make([]string, len(p.Verify))
		for i, v := range p.Verify {
			quoted[i] = "`" + v + "`"
		}
		verify = strings.Join(quoted, ", ")
	}
	detail := ""
	if epic.Detail != "" {
		detail = "\n" + epic.Detail
	}
	return fmt.Sprintf(`You are the director's planning model for the project %s (%s).

Analyse this goal and decompose it into concrete, independent tasks. Read the repo to ground yourself, but do NOT edit, create or open any files; analysis only. Reply with ONLY the task list in exactly this shape — one line per task, grouped under a heading:

## <heading>
- [ ] <task title>
- [ ] <task title>

Cover what must change and be verified; keep tasks small enough that one session can finish each. Project verify commands: %s. The goal: %s%s`,
		p.Name, p.Path, verify, epic.Title, detail)
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
}

var askRe = regexp.MustCompile(`ASK:\s*(\S.*)`)
var statusRe = regexp.MustCompile(`STATUS:\s*(DONE|BLOCKED|NEEDS-INPUT)`)

// Handle is work's live session: its runner (else its epic's, else the
// project's), its session (else its claim), and its directory (its own cwd,
// else its epic's active shared worktree, else the project's path). ok is
// false when work has neither session nor claim.
func Handle(l *ledger.Ledger, w core.Work, p *project.Project) (runner.Handle, bool, error) {
	session := w.Session
	if session == nil {
		session = w.Claim
	}
	if session == nil {
		return runner.Handle{}, false, nil
	}
	runnerName := p.Runner
	cwd := p.Path
	if w.Parent != nil {
		epic, err := l.Get(*w.Parent)
		if err != nil {
			return runner.Handle{}, false, err
		}
		if epic.Runner != nil {
			runnerName = *epic.Runner
		}
		wts, err := l.Worktrees(epic.ID)
		if err != nil {
			return runner.Handle{}, false, err
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
	return runner.Handle{Runner: runnerName, Session: *session, Ref: w.Ref, Cwd: cwd}, true, nil
}

// Send continues work's session and records the ref the runner returns, so a
// status check reaches the process now serving the session. Only the ref is
// recorded: a runner and directory work does not set itself keep following
// its epic.
func Send(l *ledger.Ledger, id string, r runner.Runner, h runner.Handle, text string) error {
	if err := r.Send(&h, text); err != nil {
		return err
	}
	return l.SetRef(id, h.Ref)
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

// Known is the work whose decisions can answer w's questions: its epic and
// the epic's tasks, or w alone when it has no epic.
func Known(l *ledger.Ledger, w core.Work) ([]core.Work, error) {
	if w.Parent == nil {
		return []core.Work{w}, nil
	}
	epic, err := l.Get(*w.Parent)
	if err != nil {
		return nil, err
	}
	children, err := l.Tasks(epic.ID)
	if err != nil {
		return nil, err
	}
	return append([]core.Work{epic}, children...), nil
}

// CoordinateOnce runs one coordination pass over the epic's children. It
// does not spawn.
func CoordinateOnce(epic core.Work, p *project.Project, l *ledger.Ledger, resolve func(name string) (runner.Runner, error)) (PassResult, error) {
	children, err := l.Tasks(epic.ID)
	if err != nil {
		return PassResult{}, err
	}
	known := append([]core.Work{epic}, children...)
	res := PassResult{
		Answered:  []string{},
		Escalated: []string{},
		Reviewed:  []string{},
		Blocked:   []string{},
		Waiting:   []string{},
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
		texts, err := r.Transcript(h)
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
	if w.State == core.StateNeedsInput {
		if _, err := l.Transition(w.ID, core.StateRunning); err != nil {
			return "", err
		}
	}
	if _, err := l.AddEvent(w.ID, core.EventReport, report); err != nil {
		return "", err
	}
	if status[1] == "BLOCKED" {
		if _, err := l.Transition(w.ID, core.StateBlocked); err != nil {
			return "", err
		}
		return Blocked, nil
	}
	if _, err := l.Transition(w.ID, core.StateReview); err != nil {
		return "", err
	}
	return Reviewed, nil
}
