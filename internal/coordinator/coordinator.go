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
// decision or a `decision` event on the epic or its children that shares
// ≥2 significant words with the question (exact containment wins). It
// returns "" when nothing is known.
func KnownAnswer(question string, epic core.Work, children []core.Work, l *ledger.Ledger) (string, error) {
	var candidates []string
	works := make([]core.Work, 0, len(children)+1)
	works = append(works, epic)
	works = append(works, children...)
	for _, w := range works {
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

var askRe = regexp.MustCompile(`ASK:\s*(.+)`)
var statusRe = regexp.MustCompile(`STATUS:\s*(DONE|BLOCKED)`)

// CoordinateOnce runs one coordination pass over the epic's children:
// answer known questions, escalate unknown ones to needs-input, harvest
// DONE/BLOCKED reports. It does not spawn. A nil resolve uses the runner
// registry.
func CoordinateOnce(epic core.Work, l *ledger.Ledger, resolve func(name string) (runner.Runner, error)) (PassResult, error) {
	if resolve == nil {
		resolve = runner.RunnerNamed
	}
	children, err := l.Tasks(epic.ID)
	if err != nil {
		return PassResult{}, err
	}
	res := PassResult{
		Answered:  []string{},
		Escalated: []string{},
		Reviewed:  []string{},
		Blocked:   []string{},
		Waiting:   []string{},
	}
	for _, w := range children {
		if w.State != core.StateRunning && w.State != core.StateNeedsInput {
			continue
		}
		session := w.Session
		if session == nil {
			session = w.Claim
		}
		if session == nil {
			res.Waiting = append(res.Waiting, w.ID)
			continue
		}
		cwd, err := childCwd(w, epic, l)
		if err != nil {
			return PassResult{}, err
		}
		runnerName := "claude"
		if w.Runner != nil {
			runnerName = *w.Runner
		} else if epic.Runner != nil {
			runnerName = *epic.Runner
		}
		r, err := resolve(runnerName)
		if err != nil {
			return PassResult{}, err
		}
		h := runner.Handle{Runner: runnerName, Session: *session, Ref: w.Ref, Cwd: cwd}
		texts, err := r.Transcript(h)
		if err != nil {
			return PassResult{}, err
		}
		last := ""
		if len(texts) > 0 {
			last = texts[len(texts)-1]
		}
		if ask := askRe.FindStringSubmatch(last); ask != nil {
			question := strings.TrimSpace(ask[1])
			if err := l.AddEvent(w.ID, core.EventQuestion, question); err != nil {
				return PassResult{}, err
			}
			answer, err := KnownAnswer(question, epic, children, l)
			if err != nil {
				return PassResult{}, err
			}
			if answer != "" {
				if err := r.Send(&h, answer); err != nil {
					return PassResult{}, err
				}
				if err := l.AddEvent(w.ID, core.EventAnswer, answer); err != nil {
					return PassResult{}, err
				}
				if w.State == core.StateNeedsInput {
					if _, err := l.Transition(w.ID, core.StateRunning); err != nil {
						return PassResult{}, err
					}
				}
				res.Answered = append(res.Answered, w.ID)
				continue
			}
			if w.State != core.StateNeedsInput {
				if _, err := l.Transition(w.ID, core.StateNeedsInput); err != nil {
					return PassResult{}, err
				}
			}
			res.Escalated = append(res.Escalated, w.ID)
			continue
		}
		if status := statusRe.FindStringSubmatch(last); status != nil {
			if w.State == core.StateNeedsInput {
				if _, err := l.Transition(w.ID, core.StateRunning); err != nil {
					return PassResult{}, err
				}
			}
			if err := l.AddEvent(w.ID, core.EventReport, status[1]+"\n"+last); err != nil {
				return PassResult{}, err
			}
			to := core.StateReview
			if status[1] == "BLOCKED" {
				to = core.StateBlocked
			}
			if _, err := l.Transition(w.ID, to); err != nil {
				return PassResult{}, err
			}
			if status[1] == "DONE" {
				res.Reviewed = append(res.Reviewed, w.ID)
			} else {
				res.Blocked = append(res.Blocked, w.ID)
			}
			continue
		}
		res.Waiting = append(res.Waiting, w.ID)
	}
	return res, nil
}

// childCwd is the child's own cwd, else the epic's active shared worktree,
// else ".".
func childCwd(w core.Work, epic core.Work, l *ledger.Ledger) (string, error) {
	if w.Cwd != nil {
		return *w.Cwd, nil
	}
	worktrees, err := l.Worktrees(epic.ID)
	if err != nil {
		return "", err
	}
	for _, wt := range worktrees {
		if wt.Kind == core.WorktreeShared && wt.State == core.WorktreeActive {
			return wt.Path, nil
		}
	}
	return ".", nil
}
