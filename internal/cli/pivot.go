package cli

import (
	"fmt"
	"slices"
	"strings"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/runner"
)

// told is where one message to a task went.
type told string

const (
	toldDelivered told = "delivered"
	toldQueued    told = "queued"
)

// resumeText opens every message that continues paused work.
const resumeText = "Resume where you left off."

// send says something to work while it runs. A goal keeps it as a decision and
// passes it to every open task; a task gets it now when its session is idle,
// and when it is ready otherwise.
func (c *Cli) send(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd send <id> <text>")
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	if core.IsGoal(w.Kind) {
		return c.tellGoal(w, rest[1])
	}
	where, err := c.tellTask(w, rest[1])
	if err != nil {
		return err
	}
	text := "delivered"
	if where == toldQueued {
		text = fmt.Sprintf("queued: %s is busy and reads this when it is ready; the next wd drive or wd goal review pass delivers it", w.ID)
	}
	return c.out(map[string]any{"ok": true, "told": where}, text)
}

// sendTo is send for one task, for callers that only need to know it worked.
func (c *Cli) sendTo(id, text string) error {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	_, err = c.tellTask(w, text)
	return err
}

// tellTask queues text for a task with a session and delivers everything
// waiting when the session is not running. A running session keeps it queued:
// resuming a session that is working starts a copy of it instead.
func (c *Cli) tellTask(w core.Work, text string) (told, error) {
	// Finished work is refused before anything is stored: a goal someone is
	// asking a question of must not end up carrying a message it never
	// accepted, and a message is not a reason to work on it again.
	if core.Reopenable(w.State) {
		return "", fail("%s is done; a message is not a reason to work on it again — wd reopen %s \"<what is being worked on>\", or wd context %s to ask it something", w.ID, w.ID, w.ID)
	}
	if w.State != core.StateRunning && w.State != core.StatePaused && !slices.Contains(core.Transitions[w.State], core.StateRunning) {
		return "", fail("%s", core.IllegalTransition{From: w.State, To: core.StateRunning}.Error())
	}
	h, err := c.handle(w.ID)
	if err != nil {
		return "", err
	}
	if _, err := c.Ledger.QueueMessage(w.ID, text); err != nil {
		return "", err
	}
	if w.State == core.StatePaused {
		return toldQueued, nil
	}
	r, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return "", err
	}
	n, err := coordinator.Deliver(c.Ledger, w, r, h)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return toldQueued, nil
	}
	if w.State != core.StateRunning {
		if _, err := c.Ledger.Transition(w.ID, core.StateRunning); err != nil {
			return "", err
		}
	}
	return toldDelivered, nil
}

// tellGoal keeps text as a decision on the goal, which every later brief and
// judgement reads, and passes it to every open task that has a session.
func (c *Cli) tellGoal(goal core.Work, text string) error {
	if core.Reopenable(goal.State) {
		return fail("%s is done; a message is not a reason to work on it again — wd reopen %s \"<what is being worked on>\"", goal.ID, goal.ID)
	}
	if core.AtRest(goal.State) {
		return fail("goal %s is %s: there is nothing running to tell", goal.ID, goal.State)
	}
	if _, err := c.Ledger.AddEvent(goal.ID, core.EventDecision, text); err != nil {
		return err
	}
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return err
	}
	out := map[string][]string{"delivered": {}, "queued": {}, "brief": {}}
	for _, t := range tasks {
		if core.AtRest(t.State) {
			continue
		}
		p, err := c.project(t.Project)
		if err != nil {
			return err
		}
		if _, ok, err := coordinator.Handle(c.Ledger, t, p); err != nil {
			return err
		} else if !ok {
			out["brief"] = append(out["brief"], t.ID)
			continue
		}
		where, err := c.tellTask(t, text)
		if err != nil {
			return err
		}
		out[string(where)] = append(out[string(where)], t.ID)
	}
	return c.out(out, fmt.Sprintf("kept as a decision on %s; delivered to %d task(s), queued for %d, in the brief of %d",
		goal.ID, len(out["delivered"]), len(out["queued"]), len(out["brief"])))
}

// pause stops running sessions, keeping their conversations, and holds the
// work in paused: a task itself, or every open task of a goal and then the goal.
func (c *Cli) pause(rest []string) error {
	if len(rest) != 1 {
		return fail("usage: wd pause <id>")
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	if core.AtRest(w.State) || w.State == core.StatePaused {
		return fail("%s is %s: there is nothing running to pause", w.ID, w.State)
	}
	targets := []core.Work{w}
	if core.IsGoal(w.Kind) {
		if targets, err = c.Ledger.Tasks(w.ID); err != nil {
			return err
		}
	}
	var paused, failed []string
	for _, t := range targets {
		if core.AtRest(t.State) || t.State == core.StatePaused {
			continue
		}
		if err := c.stopSession(t); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", t.ID, err))
			continue
		}
		if _, err := c.Ledger.Transition(t.ID, core.StatePaused); err != nil {
			return err
		}
		paused = append(paused, t.ID)
	}
	if len(failed) > 0 {
		return fail("could not stop %s; the rest are paused and %s is not", strings.Join(failed, "; "), w.ID)
	}
	if core.IsGoal(w.Kind) {
		if _, err := c.Ledger.Transition(w.ID, core.StatePaused); err != nil {
			return err
		}
	}
	return c.out(map[string]any{"paused": paused}, fmt.Sprintf("%s paused with %d task(s); wd resume %s carries on", w.ID, len(paused), w.ID))
}

// resume returns paused work to the state it was paused from, and sends every
// task that was working "Resume where you left off." with the note.
func (c *Cli) resume(rest []string) error {
	if len(rest) < 1 {
		return fail("usage: wd resume <id> [\"<note>\"]")
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	note := strings.TrimSpace(strings.Join(rest[1:], " "))
	message := resumeText
	if note != "" {
		message += "\n\n" + note
	}
	targets := []core.Work{w}
	if core.IsGoal(w.Kind) {
		if targets, err = c.Ledger.Tasks(w.ID); err != nil {
			return err
		}
	} else if w.State != core.StatePaused {
		return fail("%s is %s, not paused", w.ID, w.State)
	}
	if core.IsGoal(w.Kind) && w.State == core.StatePaused {
		if err := c.unpause(w); err != nil {
			return err
		}
	}
	resumed := []string{}
	for _, t := range targets {
		if t.State != core.StatePaused {
			continue
		}
		if err := c.unpause(t); err != nil {
			return err
		}
		resumed = append(resumed, t.ID)
		if t, err = c.Ledger.Get(t.ID); err != nil {
			return err
		}
		if t.State != core.StateRunning && t.State != core.StateNeedsInput {
			continue
		}
		p, err := c.project(t.Project)
		if err != nil {
			return err
		}
		if _, ok, err := coordinator.Handle(c.Ledger, t, p); err != nil {
			return err
		} else if ok {
			if _, err := c.tellTask(t, message); err != nil {
				return err
			}
		}
	}
	return c.out(map[string]any{"resumed": resumed}, fmt.Sprintf("%s resumed with %d task(s)", w.ID, len(resumed)))
}

// unpause moves paused work back to the state its ledger says it was paused from.
func (c *Cli) unpause(w core.Work) error {
	evs, err := c.Ledger.Events(w.ID, kindPtr(core.EventState))
	if err != nil {
		return err
	}
	before := core.StateQueued
	for i := len(evs) - 1; i > 0; i-- {
		if evs[i].Body == string(core.StatePaused) {
			if before, err = core.ParseState(evs[i-1].Body); err != nil {
				return err
			}
			break
		}
	}
	_, err = c.Ledger.Transition(w.ID, before)
	return err
}

// cancel stops every running session under work and drops it and its open
// work with the reason given.
func (c *Cli) cancel(rest []string) error {
	if len(rest) < 2 || strings.TrimSpace(strings.Join(rest[1:], " ")) == "" {
		return fail("usage: wd cancel <id> \"<why>\" — cancelling drops open work, so it says why")
	}
	if err := c.stopUnder(rest[0]); err != nil {
		return err
	}
	dropped, err := c.Ledger.Drop(rest[0], strings.Join(rest[1:], " "))
	if err != nil {
		return fail("%v", err)
	}
	return c.out(dropped, fmt.Sprintf("cancelled %s: %d work item(s) dropped", rest[0], len(dropped)))
}

// stopUnder stops the running session of every open work under id, itself
// included, and fails naming each that would not stop.
func (c *Cli) stopUnder(id string) error {
	tree, err := c.Ledger.Under(id)
	if err != nil {
		return err
	}
	var failed []string
	for _, t := range tree {
		if core.AtRest(t.State) {
			continue
		}
		if err := c.stopSession(t); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", t.ID, err))
		}
	}
	if len(failed) > 0 {
		return fail("could not stop %s; nothing was dropped", strings.Join(failed, "; "))
	}
	return nil
}

// stopSession stops work's session when its runner reports it running. Work
// with no session, or a session that is idle, has nothing to stop.
func (c *Cli) stopSession(w core.Work) error {
	p, err := c.project(w.Project)
	if err != nil {
		return err
	}
	h, ok, err := coordinator.Handle(c.Ledger, w, p)
	if err != nil || !ok {
		return err
	}
	r, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return err
	}
	status, err := r.Status(h)
	if err != nil {
		return err
	}
	if status != runner.StatusRunning {
		return nil
	}
	return r.Stop(h)
}
