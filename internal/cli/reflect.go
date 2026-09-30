package cli

import (
	"fmt"
	"time"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/reflect"
	"wd/internal/runner"
)

// reflectTimeout bounds the reflection call. It is one bounded question, so it
// gets a short deadline: a model that never answers leaves no verdict, which is
// the same safe failure as declining.
const reflectTimeout = 120 * time.Second

// reflectResult is what one reflection call did: how many verdicts it found,
// how many were filed as feedback, and the event body recording the call.
type reflectResult struct {
	Found int    `json:"found"`
	Filed int    `json:"filed"`
	Body  string `json:"body"`
}

// Reflect asks one model whether a finished session taught anything durable,
// and files what it says as feedback. It proposes and never disposes: it
// appends feedback rows and one event, and cannot move work state, write a
// card or satisfy a gate.
//
// In mode ask the verdicts are reported but not filed, so asking for a
// conversation's confirmation means something; in auto they are filed, which is
// the only way the memory loop runs unattended.
func (c *Cli) Reflect(w core.Work, texts []string) (reflectResult, error) {
	var res reflectResult
	p, err := c.project(w.Project)
	if err != nil {
		return res, err
	}
	// The call runs where the work runs, not where the project lives: a child
	// under a goal is reflected on with that goal's runner and in its shared
	// worktree, so the model reads the same code the executor did.
	runnerName, cwd, err := coordinator.Where(c.Ledger, w, p)
	if err != nil {
		return res, err
	}
	rn, err := runner.DetectedRunner(runnerName)
	if err != nil {
		return res, err
	}
	brief := reflect.Brief(w, p, texts)
	h, err := rn.Spawn(runner.SpawnOptions{
		Cwd:   cwd,
		Name:  slice60(fmt.Sprintf("wd-%s reflect", w.ID)),
		Brief: brief,
		Model: p.Model,
	})
	if err != nil {
		return res, err
	}
	j, err := awaitReflection(rn, h)
	if err != nil {
		return res, err
	}
	res.Body = reflect.Event(brief, j, rn.Name(), strOrEmpty(p.Model))
	durable := j.Durable()
	res.Found = len(durable)
	if p.Mode == project.ModeAsk {
		// Reported, not filed: a supervised project keeps its human in the loop
		// over what becomes a durable rule.
		_, err := c.Ledger.AddEvent(w.ID, core.EventNote, res.Body)
		return res, err
	}
	for _, v := range durable {
		proj := w.Project
		card := v.Card
		opts := ledger.FeedbackOptions{Source: core.FeedbackAttached, Project: &proj}
		if card != "" {
			opts.Card = &card
		}
		if _, err := c.Ledger.AddFeedback(v.Text, opts); err != nil {
			return res, err
		}
		res.Filed++
	}
	_, err = c.Ledger.AddEvent(w.ID, core.EventNote, res.Body)
	return res, err
}

// awaitReflection polls the reflection session until it produces a verdict or
// the call is plainly finished without one, which is a declined reflection
// rather than a slow one.
func awaitReflection(rn runner.Runner, h runner.Handle) (reflect.Judgement, error) {
	deadline := time.Now().Add(reflectTimeout)
	var j reflect.Judgement
	for time.Now().Before(deadline) {
		out, err := rn.Transcript(h)
		if err != nil {
			return j, err
		}
		if parsed := reflect.Parse(out); len(parsed.Verdicts) > 0 {
			return parsed, nil
		}
		if len(out) > 0 {
			// A session no longer running has finished its answer; take it,
			// which for a reply with no DURABLE line is a declined reflection.
			if st, err := rn.Status(h); err == nil && st != runner.StatusRunning && st != runner.StatusWaiting {
				return reflect.Parse(out), nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return j, nil
}
