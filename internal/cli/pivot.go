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
	// toldRecorded is an answer kept on a goal with no supervisor yet: the
	// supervisor reads it when it first looks.
	toldRecorded told = "recorded"
)

// resumeText opens every message that continues paused work.
const resumeText = "Resume where you left off."

// send says something to work while it runs. A goal keeps it as a decision and
// the director routes it; a task gets it now when its session is idle,
// and when it is ready otherwise.
func (c *Cli) send(rest []string) error {
	files, err := c.attachedFiles()
	if err != nil {
		return err
	}
	if len(rest) < 2 && (len(rest) != 1 || len(files) == 0) {
		return fail("usage: wd send <id> <text> [--attach <path>]...")
	}
	text := ""
	if len(rest) > 1 {
		text = rest[1]
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	atts, err := c.store(w.ID, files)
	if err != nil {
		return err
	}
	if core.IsGoal(w.Kind) {
		out, err := c.tellGoal(w, text, atts)
		if err != nil {
			discard(atts)
			return err
		}
		text := fmt.Sprintf("Kept as a decision on %s; the director is finishing the last one and reads it as soon as it is free", quoted(w))
		if why, ok := out["not_told"]; ok {
			text = fmt.Sprintf("Kept as a decision on %s; the director couldn't be told now (%s) and reads it when it next starts", quoted(w), why)
		} else if out["looked"] == true {
			text = fmt.Sprintf("Kept as a decision on %s; the director is reading it now", quoted(w))
		}
		return c.out(out, text)
	}
	where, err := c.tellTask(w, core.WithAttached(text, atts))
	if err != nil {
		discard(atts)
		return err
	}
	if _, err := c.Ledger.Attach(w.ID, atts); err != nil {
		return err
	}
	said := "delivered"
	if where == toldQueued {
		said = fmt.Sprintf("The agent on %s is busy; your message is queued and reaches it as soon as it is free", quoted(w))
	}
	return c.out(map[string]any{"ok": true, "told": where, "attachments": atts}, said)
}

// sendTo is send for callers that only need to know it worked: a goal keeps
// it as a decision for its supervisor, as wd send does.
func (c *Cli) sendTo(id, text string) error {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if core.IsGoal(w.Kind) {
		_, err = c.tellGoal(w, text, nil)
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
	return c.tellVia(w, h, text)
}

// tellVia queues text for w and delivers it through h, the session that is
// to read it. A goal's supervisor is never h: only a claimed look sends to
// it, so two processes never send to it at once.
func (c *Cli) tellVia(w core.Work, h runner.Handle, text string) (told, error) {
	if core.IsGoal(w.Kind) {
		p, err := c.project(w.Project)
		if err != nil {
			return "", err
		}
		sh, has, err := coordinator.Supervisor(c.Ledger, w, p)
		if err != nil {
			return "", err
		}
		if has && sh.Session == h.Session {
			return "", fmt.Errorf("%s's supervisor is told only by a look: wd send %s", w.ID, w.ID)
		}
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
// judgement reads, and as a message waiting for the goal's supervisor, which
// is asked to look at once, so it reaches the tasks it is for rather than
// every task. A supervisor that is working is given it at its next look,
// with whatever else the person wrote meanwhile. Once kept, the decision and
// its files stay even when the supervisor cannot look, and not_told says why
// it was not told now.
func (c *Cli) tellGoal(goal core.Work, text string, atts []core.Attachment) (map[string]any, error) {
	if core.Reopenable(goal.State) {
		return nil, fail("%s is done; a message is not a reason to work on it again — wd reopen %s \"<what is being worked on>\"", goal.ID, goal.ID)
	}
	if core.AtRest(goal.State) {
		return nil, fail("goal %s is %s: there is nothing running to tell", goal.ID, goal.State)
	}
	// The files follow the decision they came with, which is how the
	// supervisor reads them as part of it.
	decision, err := c.Ledger.AddEvent(goal.ID, core.EventDecision, text, atts...)
	if err != nil {
		return nil, err
	}
	if _, err := c.Ledger.QueueForSupervisor(goal.ID, decision.ID, core.WithAttached(text, atts)); err != nil {
		return nil, err
	}
	// A person writing to a goal waiting on them has answered it.
	if goal.State == core.StateNeedsInput {
		if goal, err = c.Ledger.Transition(goal.ID, core.StateRunning); err != nil {
			return nil, err
		}
	}
	out := map[string]any{"decision": decision, "looked": false, "attachments": atts}
	s, err := c.supervise(goal)
	if err != nil {
		out["not_told"] = strings.TrimSpace(err.Error())
		return out, nil
	}
	out["looked"] = s.Looked
	return out, nil
}

// stop interrupts what a task's session is doing and takes back the messages
// it has not read yet, so they can be said again differently. The work keeps
// its state: the next message continues the same session.
func (c *Cli) stop(rest []string) error {
	if len(rest) != 1 {
		return fail("usage: wd stop <id>")
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	targets := []core.Work{w}
	if core.IsGoal(w.Kind) {
		tasks, err := c.Ledger.Tasks(w.ID)
		if err != nil {
			return err
		}
		// The goal's own session, while it is planned, stops with its tasks.
		targets = append(targets, tasks...)
	}
	stopped, withdrawn, names := []string{}, []string{}, []string{}
	for _, t := range targets {
		if core.AtRest(t.State) {
			continue
		}
		was, err := c.stopSession(t)
		if err != nil {
			return err
		}
		if was {
			stopped = append(stopped, t.ID)
			names = append(names, quoted(t))
		}
		back, err := c.Ledger.Withdraw(t.ID)
		if err != nil {
			return err
		}
		for _, m := range back {
			withdrawn = append(withdrawn, m.Text)
		}
	}
	if len(stopped) == 0 && len(withdrawn) == 0 {
		return fail("%s has nothing to stop: no agent is working on it and no message is waiting", quoted(w))
	}
	var text string
	switch len(names) {
	case 0:
		text = "No agent was working"
	case 1:
		text = "Stopped the agent on " + names[0] + "; your next message continues it"
	default:
		text = "Stopped the agents on " + core.AndList(names) + "; your next message continues them"
	}
	if len(withdrawn) > 0 {
		text += "\nTaken back, unread:\n" + strings.Join(withdrawn, "\n\n")
	}
	return c.out(map[string]any{"ok": true, "stopped": stopped, "withdrawn": withdrawn}, text)
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
		return fail("%s isn't running: there is nothing to pause", quoted(w))
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
		if _, err := c.stopSession(t); err != nil {
			failed = append(failed, fmt.Sprintf("the agent on %s (%v)", quoted(t), err))
			continue
		}
		if _, err := c.Ledger.Transition(t.ID, core.StatePaused); err != nil {
			return err
		}
		paused = append(paused, t.ID)
	}
	if len(failed) > 0 {
		return fail("Couldn't stop %s; the rest are paused and %s is not", core.AndList(failed), quoted(w))
	}
	if core.IsGoal(w.Kind) {
		if _, err := c.Ledger.Transition(w.ID, core.StatePaused); err != nil {
			return err
		}
	}
	return c.out(map[string]any{"paused": paused}, "Paused "+withAgents(w, paused)+"; resume it to carry on")
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
	resumed, err := c.resumeWork(w, strings.Join(rest[1:], " "), nil)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"resumed": resumed}, "Resumed "+withAgents(w, resumed))
}

// resumeWork returns paused work to where it was and tells each task that was
// working to carry on, with the note and its files; a goal's note also waits
// for its supervisor, as wd send to a goal does. It returns the ids it
// resumed.
func (c *Cli) resumeWork(w core.Work, note string, atts []core.Attachment) ([]string, error) {
	note = strings.TrimSpace(note)
	message := resumeText
	if note != "" || len(atts) > 0 {
		message += "\n\n" + core.WithAttached(note, atts)
	}
	if w.State == core.StateDropped {
		return c.restore(w, message)
	}
	targets := []core.Work{w}
	var err error
	if core.IsGoal(w.Kind) {
		if targets, err = c.Ledger.Tasks(w.ID); err != nil {
			return nil, err
		}
	} else if w.State != core.StatePaused {
		return nil, fail("%s isn't paused", quoted(w))
	}
	if core.IsGoal(w.Kind) && w.State == core.StatePaused {
		if err := c.unpause(w); err != nil {
			return nil, err
		}
	}
	resumed := []string{}
	for _, t := range targets {
		if t.State != core.StatePaused {
			continue
		}
		if err := c.unpause(t); err != nil {
			return nil, err
		}
		resumed = append(resumed, t.ID)
		if t, err = c.Ledger.Get(t.ID); err != nil {
			return nil, err
		}
		if t.State != core.StateRunning && t.State != core.StateNeedsInput {
			continue
		}
		p, err := c.project(t.Project)
		if err != nil {
			return nil, err
		}
		if _, ok, err := coordinator.Handle(c.Ledger, t, p); err != nil {
			return nil, err
		} else if ok {
			if _, err := c.tellTask(t, message); err != nil {
				return nil, err
			}
		}
	}
	if core.IsGoal(w.Kind) && (note != "" || len(atts) > 0) {
		if w, err = c.Ledger.Get(w.ID); err != nil {
			return nil, err
		}
		if _, err := c.tellGoal(w, note, atts); err != nil {
			return nil, err
		}
	}
	return resumed, nil
}

// restore brings cancelled work back with everything cancelled alongside it,
// and tells each task with a session to carry on.
func (c *Cli) restore(w core.Work, message string) ([]string, error) {
	back, err := c.Ledger.Restore(w.ID)
	if err != nil {
		return nil, fail("%v", err)
	}
	restored := []string{}
	for _, t := range back {
		restored = append(restored, t.ID)
		if core.IsGoal(t.Kind) || (t.State != core.StateRunning && t.State != core.StateNeedsInput) {
			continue
		}
		p, err := c.project(t.Project)
		if err != nil {
			return nil, err
		}
		if _, ok, err := coordinator.Handle(c.Ledger, t, p); err != nil {
			return nil, err
		} else if ok {
			if _, err := c.tellTask(t, message); err != nil {
				return nil, err
			}
		}
	}
	return restored, nil
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
	if len(rest) < 1 {
		return fail("usage: wd cancel <id> [\"<why>\"]")
	}
	why := strings.TrimSpace(strings.Join(rest[1:], " "))
	if why == "" {
		why = "cancelled"
	}
	if err := c.stopUnder(rest[0]); err != nil {
		return err
	}
	dropped, err := c.Ledger.Drop(rest[0], why)
	if err != nil {
		return fail("%v", err)
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	ids := make([]string, len(dropped))
	for i, d := range dropped {
		ids[i] = d.ID
	}
	back := "them"
	if len(dropped) <= 1 {
		back = "it"
	}
	return c.out(dropped, "Cancelled "+withAgents(w, ids)+"; resume it to bring "+back+" back")
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
		if _, err := c.stopSession(t); err != nil {
			failed = append(failed, fmt.Sprintf("the agent on %s (%v)", quoted(t), err))
		}
	}
	if len(failed) > 0 {
		return fail("Couldn't stop %s; nothing was dropped", core.AndList(failed))
	}
	return nil
}

// stopSession stops work's session while its process is up, working, idle or
// waiting. Work with no session, or one already stopped, has nothing to stop.
func (c *Cli) stopSession(w core.Work) (bool, error) {
	p, err := c.project(w.Project)
	if err != nil {
		return false, err
	}
	h, ok, err := coordinator.Handle(c.Ledger, w, p)
	if err != nil || !ok {
		return false, err
	}
	r, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return false, err
	}
	return runner.StopIfLive(r, h)
}

// goalContinue picks a goal up where it stands: an open goal is told the text,
// a paused one is resumed with it, and a done one is reopened with it as the
// reason, planned and started again. A goal let go of is not continued.
func (c *Cli) goalContinue(goal core.Work, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fail("usage: wd goal continue <id> \"<what is being worked on>\" [--attach <path>]...")
	}
	files, err := c.attachedFiles()
	if err != nil {
		return err
	}
	if goal.State == core.StateDropped || goal.State == core.StateAbandoned || goal.State == core.StateSuperseded {
		return fail("goal %s is %s and is not picked up again; start a new one: wd goal start %s \"<what you want done>\"", goal.ID, goal.State, goal.Project)
	}
	atts, err := c.store(goal.ID, files)
	if err != nil {
		return err
	}
	var lines []string
	switch {
	case goal.State == core.StatePaused:
		if _, err := c.resumeWork(goal, text, atts); err != nil {
			discard(atts)
			return err
		}
	case core.Reopenable(goal.State):
		if goal.Archived != nil {
			if _, err := c.Ledger.Unarchive(goal.ID); err != nil {
				return err
			}
		}
		reopened, err := c.Ledger.Reopen(goal.ID, text)
		if err != nil {
			discard(atts)
			return err
		}
		if _, err := c.Ledger.Attach(goal.ID, atts); err != nil {
			return err
		}
		p, err := c.project(goal.Project)
		if err != nil {
			return err
		}
		if _, _, lines, err = c.planAndSpawn(reopened, p); err != nil {
			return fmt.Errorf("goal %s reopened; %w", goal.ID, err)
		}
	default:
		told, err := c.tellGoal(goal, text, atts)
		if err != nil {
			discard(atts)
			return err
		}
		if why, ok := told["not_told"]; ok {
			lines = append(lines, fmt.Sprintf("The director couldn't be told now (%s) and reads it when it next starts", why))
		}
	}
	goal, err = c.Ledger.Get(goal.ID)
	if err != nil {
		return err
	}
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"goal": goal, "tasks": tasks, "attachments": atts},
		strings.TrimSpace(fmt.Sprintf("%s continues (%s)\n%s", quoted(goal), goal.State, strings.Join(lines, "\n"))))
}

// withAgents names w by its title with how many agents' work under it changed
// alongside it: ids are what changed, w itself among them or not.
func withAgents(w core.Work, ids []string) string {
	n := 0
	for _, id := range ids {
		if id != w.ID {
			n++
		}
	}
	switch n {
	case 0:
		return quoted(w)
	case 1:
		return quoted(w) + " and its agent"
	}
	return fmt.Sprintf("%s and its %d agents", quoted(w), n)
}
