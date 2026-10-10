package cli

import (
	"fmt"
	"strings"
	"time"

	"wd/internal/core"
	"wd/internal/find"
	"wd/internal/ledger"
	"wd/internal/runner"
)

// findTimeout bounds the call that finds the goal to continue. A model that
// never answers leaves no match, and a person can still start a new goal.
const findTimeout = 90 * time.Second

// found is one goal a request continues, as a whole row, and why.
type found struct {
	Goal core.Work `json:"goal"`
	Why  string    `json:"why"`
}

// goalFind asks one bounded model call which of a project's goals, finished
// and archived ones included, a request continues.
func (c *Cli) goalFind(name, text string) error {
	p, err := c.project(name)
	if err != nil {
		return err
	}
	goals, err := c.Ledger.List(ledger.ListFilter{Project: &name, Kinds: []core.WorkKind{core.WorkGoal}})
	if err != nil {
		return err
	}
	out := []found{}
	if len(goals) == 0 {
		return c.out(out, "no goals in "+name)
	}
	runnerName := strOr(c.Args, "runner", p.Runner)
	if err := c.restrictToProject(runnerName, p, runner.RoleInterpret); err != nil {
		return err
	}
	rn, err := runner.DetectedRunner(runnerName)
	if err != nil {
		return err
	}
	model, err := c.judgementModel(p, runner.RoleInterpret, mustPolicy(c.Home))
	if err != nil {
		return err
	}
	h, err := rn.Spawn(runner.SpawnOptions{
		Cwd:   p.Path,
		Name:  "wd find",
		Brief: find.Brief(text, goals),
		Model: optionalModel(model),
	})
	if err != nil {
		return err
	}
	reply, err := awaitFind(rn, h)
	if err != nil {
		return err
	}
	byID := map[string]core.Work{}
	for _, g := range goals {
		byID[g.ID] = g
	}
	var lines []string
	for _, m := range find.Parse(reply, goals) {
		g := byID[m.Goal]
		out = append(out, found{Goal: g, Why: m.Why})
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s — %s", g.ID, g.State, g.Title, m.Why))
	}
	if len(lines) == 0 {
		return c.out(out, "no existing goal matches; wd goal start "+name+" starts a new one")
	}
	return c.out(out, strings.Join(lines, "\n"))
}

// awaitFind polls the call until it answers, or until its session has finished
// without answering, which is no match.
func awaitFind(rn runner.Runner, h runner.Handle) ([]string, error) {
	deadline := time.Now().Add(findTimeout)
	for time.Now().Before(deadline) {
		out, err := rn.Transcript(h)
		if err != nil {
			return nil, err
		}
		if find.Answered(out) {
			return out, nil
		}
		if len(out) > 0 {
			if st, err := rn.Status(h); err == nil && st != runner.StatusRunning && st != runner.StatusWaiting {
				return out, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fail("session %s gave no answer within %s; start a new goal or try again", h.Session, findTimeout)
}
