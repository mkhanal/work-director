package cli

import (
	"fmt"
	"slices"
	"strings"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
)

// goalTree is `wd goal edit|add|drop|merge|split|after|approve`: a person
// reshaping a goal's sub-goals. Each change is recorded as a decision on the
// goal whose sub-goals changed. It reports whether sub named one of them.
func (c *Cli) goalTree(sub string, rest []string) (bool, error) {
	switch sub {
	case "edit":
		return true, c.goalEdit(rest)
	case "add":
		if len(rest) < 2 {
			return false, nil
		}
		parent, err := c.Ledger.Get(rest[1])
		if err != nil || !core.IsGoal(parent.Kind) {
			return false, nil
		}
		return true, c.goalAddSub(parent, rest)
	case "drop":
		return true, c.goalDrop(rest)
	case "merge":
		return true, c.goalMerge(rest)
	case "split":
		return true, c.goalSplit(rest)
	case "after":
		return true, c.goalAfter(rest)
	case "approve":
		return true, c.goalApprove(rest)
	case "finish":
		return true, c.goalFinish(rest)
	}
	return false, nil
}

// goalFinish is `wd goal finish <goal> "<line>"`: the person sets where a top
// goal finishes.
// quoted names work in a sentence for the person: its title in quotes.
func quoted(w core.Work) string { return "“" + w.Title + "”" }

// waitsWords says what w now waits on, naming each by its title.
func (c *Cli) waitsWords(w core.Work, waits []string) (string, error) {
	if len(waits) == 0 {
		return quoted(w) + " waits on nothing", nil
	}
	names := make([]string, 0, len(waits))
	for _, id := range waits {
		o, err := c.Ledger.Get(id)
		if err != nil {
			return "", err
		}
		names = append(names, quoted(o))
	}
	return quoted(w) + " waits on " + strings.Join(names, ", "), nil
}

func (c *Cli) goalFinish(rest []string) error {
	if len(rest) != 3 {
		return fail("usage: wd goal finish <goal> \"<finish line>\"")
	}
	goal, err := c.Ledger.Get(rest[1])
	if err != nil {
		return err
	}
	if !core.IsGoal(goal.Kind) {
		return fail("%s is a %s; only a goal has a finish line", goal.ID, goal.Kind)
	}
	top, err := c.topGoal(goal)
	if err != nil {
		return err
	}
	if top.ID != goal.ID {
		return fail("%s finishes where its top goal %s does; set it there: wd goal finish %s \"<line>\"", goal.ID, top.ID, top.ID)
	}
	line, err := core.ParseFinishLine(rest[2])
	if err != nil {
		names := make([]string, len(core.FinishLines))
		for i, f := range core.FinishLines {
			names[i] = string(f)
		}
		return fail("%q is not a finish line; one of: %s", rest[2], strings.Join(names, ", "))
	}
	if err := c.Ledger.SetFinish(goal.ID, line, core.FinishFromPerson); err != nil {
		return err
	}
	if goal, err = c.Ledger.Get(goal.ID); err != nil {
		return err
	}
	return c.out(map[string]any{"work": goal, "finish": line, "source": core.FinishFromPerson}, fmt.Sprintf("%s finishes at %s", quoted(goal), coordinator.FinishWords(line)))
}

// reshaped records text as a decision made by by on the goal w sits under, or
// on w when it sits under no goal.
func (c *Cli) reshaped(w core.Work, text, by string) error {
	on := w.ID
	if w.Parent != nil {
		p, err := c.Ledger.Get(*w.Parent)
		if err != nil {
			return err
		}
		if core.IsGoal(p.Kind) {
			on = p.ID
		}
	}
	_, err := c.Ledger.Decide(on, text, core.Decision{Question: "the sub-goals of " + on, Answer: text, Source: by}, nil)
	return err
}

// subGoal is id when it is work under a goal.
func (c *Cli) subGoal(id string) (core.Work, error) {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return w, err
	}
	if w.Parent == nil {
		return w, fail("%s is not a sub-goal: it sits under no goal", id)
	}
	p, err := c.Ledger.Get(*w.Parent)
	if err != nil {
		return w, err
	}
	if !core.IsGoal(p.Kind) {
		return w, fail("%s is not a sub-goal: it sits under no goal", id)
	}
	return w, nil
}

// idList reads a comma-separated list of work ids; "none" or nothing is none.
func idList(raw string) []string {
	out := []string{}
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" && s != "none" {
			out = append(out, s)
		}
	}
	return out
}

func (c *Cli) goalEdit(rest []string) error {
	title, doneWhen := str(c.Args, "title"), str(c.Args, "done-when")
	if len(rest) != 2 || (title == nil && doneWhen == nil) {
		return fail("usage: wd goal edit <id> [--title \"<title>\"] [--done-when \"<done when>\"]")
	}
	w, err := c.Ledger.Get(rest[1])
	if err != nil {
		return err
	}
	what, told, err := c.editSubGoal(w, title, doneWhen, core.AnsweredByPerson)
	if err != nil {
		return err
	}
	if w, err = c.Ledger.Get(w.ID); err != nil {
		return err
	}
	text := fmt.Sprintf("Changed %s: %s", quoted(w), what)
	if len(told) > 0 {
		text += fmt.Sprintf("; its %d working agent(s) were told", len(told))
	}
	return c.out(map[string]any{"work": w, "told": told}, text)
}

// editSubGoal changes open work's title, done-when or both (nil is
// unchanged), records it on its goal and tells every agent working under it.
// It returns what changed and who was told.
func (c *Cli) editSubGoal(w core.Work, title, doneWhen *string, by string) (string, []string, error) {
	if core.AtRest(w.State) {
		return "", nil, fail("%s is %s; only open work is edited", w.ID, w.State)
	}
	var changes []string
	if title != nil {
		t := strings.TrimSpace(*title)
		if t == "" {
			return "", nil, fail("a title says what the sub-goal is")
		}
		if err := c.Ledger.SetTitle(w.ID, t); err != nil {
			return "", nil, err
		}
		changes = append(changes, "title: "+t)
	}
	if doneWhen != nil {
		if err := c.Ledger.SetDoneWhen(w.ID, *doneWhen, by); err != nil {
			return "", nil, err
		}
		changes = append(changes, "done when: "+textOr(doneWhen, "not set"))
	}
	what := strings.Join(changes, "; ")
	if err := c.reshaped(w, fmt.Sprintf("edited %s: %s", w.ID, what), by); err != nil {
		return "", nil, err
	}
	told, err := c.tellWorking(w.ID, "Your sub-goal changed. "+what)
	return what, told, err
}

// tellWorking tells every agent working under id, id included, text; it
// returns the work it told.
func (c *Cli) tellWorking(id, text string) ([]string, error) {
	tree, err := c.Ledger.Under(id)
	if err != nil {
		return nil, err
	}
	told := []string{}
	for _, t := range tree {
		if t.Kind != core.WorkTask || t.Session == nil || (t.State != core.StateRunning && t.State != core.StateNeedsInput) {
			continue
		}
		if _, err := c.tellTask(t, text); err != nil {
			return nil, err
		}
		told = append(told, t.ID)
	}
	return told, nil
}

func (c *Cli) goalAddSub(parent core.Work, rest []string) error {
	doneWhen := str(c.Args, "done-when")
	if len(rest) != 3 || strings.TrimSpace(rest[2]) == "" || doneWhen == nil || strings.TrimSpace(*doneWhen) == "" {
		return fail("usage: wd goal add <parent goal> \"<title>\" --done-when \"<done when>\" [--after <id>,<id>] [--split]")
	}
	w, err := c.addSubGoal(parent, rest[2], *doneWhen, idList(strOr(c.Args, "after", "")), flag(c.Args, "split"), core.AnsweredByPerson)
	if err != nil {
		return err
	}
	return c.out(w, fmt.Sprintf("Added %s under %s, as %s", quoted(w), quoted(parent), w.ID))
}

// addSubGoal files a sub-goal under parent with its done-when and what it
// waits on, as a goal to plan further when split, and records it.
func (c *Cli) addSubGoal(parent core.Work, title, doneWhen string, after []string, split bool, by string) (core.Work, error) {
	if !core.IsGoal(parent.Kind) {
		return core.Work{}, fail("%s is a %s; a sub-goal goes under a goal", parent.ID, parent.Kind)
	}
	if core.AtRest(parent.State) {
		return core.Work{}, fail("%s is %s; a sub-goal goes under open work", parent.ID, parent.State)
	}
	opts := ledger.AddOptions{Parent: &parent.ID}
	if split {
		opts.Kind = core.WorkGoal
	}
	w, err := c.Ledger.Add(parent.Project, strings.TrimSpace(title), opts)
	if err != nil {
		return w, err
	}
	if err := c.Ledger.SetDoneWhen(w.ID, doneWhen, by); err != nil {
		return w, err
	}
	if len(after) > 0 {
		if err := c.Ledger.SetAfter(w.ID, after); err != nil {
			return w, fail("%v", err)
		}
	}
	if err := c.reshaped(w, fmt.Sprintf("added %s: %s", w.ID, w.Title), by); err != nil {
		return w, err
	}
	return c.Ledger.Get(w.ID)
}

func (c *Cli) goalDrop(rest []string) error {
	if len(rest) < 3 || strings.TrimSpace(strings.Join(rest[2:], " ")) == "" {
		return fail("usage: wd goal drop <id> \"<why>\"")
	}
	w, err := c.subGoal(rest[1])
	if err != nil {
		return err
	}
	gone, err := c.supersedeSubGoal(w, strings.Join(rest[2:], " "), core.AnsweredByPerson)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Dropped %s; its work stays readable", quoted(w))
	switch under := len(gone) - 1; {
	case under == 1:
		text = fmt.Sprintf("Dropped %s and the sub-goal under it; their work stays readable", quoted(w))
	case under > 1:
		text = fmt.Sprintf("Dropped %s and the %d sub-goals under it; their work stays readable", quoted(w), under)
	}
	return c.out(gone, text)
}

// supersedeSubGoal stops every session under w, supersedes it and what is open
// under it with why, and records it. It returns the work superseded.
func (c *Cli) supersedeSubGoal(w core.Work, why, by string) ([]core.Work, error) {
	why = strings.TrimSpace(why)
	if err := c.stopUnder(w.ID); err != nil {
		return nil, err
	}
	gone, err := c.Ledger.Supersede(w.ID, why)
	if err != nil {
		return nil, fail("%v", err)
	}
	return gone, c.reshaped(w, fmt.Sprintf("dropped %s: %s", w.ID, why), by)
}

func (c *Cli) goalMerge(rest []string) error {
	if len(rest) != 3 {
		return fail("usage: wd goal merge <keep> <other>")
	}
	keep, err := c.subGoal(rest[1])
	if err != nil {
		return err
	}
	other, err := c.subGoal(rest[2])
	if err != nil {
		return err
	}
	if keep.ID == other.ID || *keep.Parent != *other.Parent {
		return fail("%s and %s are not two sub-goals of one goal", keep.ID, other.ID)
	}
	if core.AtRest(keep.State) || core.AtRest(other.State) {
		return fail("%s and %s must both be open to merge", keep.ID, other.ID)
	}
	if err := c.moveWaiters(other.ID, keep.ID); err != nil {
		return err
	}
	if err := c.stopUnder(other.ID); err != nil {
		return err
	}
	if _, err := c.Ledger.Supersede(other.ID, "merged into "+keep.ID); err != nil {
		return err
	}
	var parts []string
	for _, d := range []*string{keep.DoneWhen, other.DoneWhen} {
		if d != nil && *d != "" {
			parts = append(parts, *d)
		}
	}
	if err := c.Ledger.SetDoneWhen(keep.ID, strings.Join(parts, "; "), core.AnsweredByPerson); err != nil {
		return err
	}
	if err := c.reshaped(keep, fmt.Sprintf("merged %s into %s", other.ID, keep.ID), core.AnsweredByPerson); err != nil {
		return err
	}
	if keep, err = c.Ledger.Get(keep.ID); err != nil {
		return err
	}
	return c.out(keep, fmt.Sprintf("Merged %s into %s, now done when %s", quoted(other), quoted(keep), textOr(keep.DoneWhen, "not set")))
}

// moveWaiters has every sibling waiting on from wait on to instead.
func (c *Cli) moveWaiters(from, to string) error {
	w, err := c.Ledger.Get(from)
	if err != nil {
		return err
	}
	sibs, err := c.Ledger.Children(*w.Parent)
	if err != nil {
		return err
	}
	for _, s := range sibs {
		waits, err := c.Ledger.After(s.ID)
		if err != nil {
			return err
		}
		if !slices.Contains(waits, from) {
			continue
		}
		next := slices.DeleteFunc(waits, func(id string) bool { return id == from })
		if s.ID != to && !slices.Contains(next, to) {
			next = append(next, to)
		}
		if err := c.Ledger.SetAfter(s.ID, next); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cli) goalSplit(rest []string) error {
	if len(rest) != 2 {
		return fail("usage: wd goal split <id>")
	}
	t, err := c.subGoal(rest[1])
	if err != nil {
		return err
	}
	if t.Kind != core.WorkTask {
		return fail("%s is already a goal; plan it with wd goal plan %s", t.ID, t.ID)
	}
	if core.AtRest(t.State) {
		return fail("%s is %s; only open work is split", t.ID, t.State)
	}
	g, err := c.Ledger.Add(t.Project, t.Title, ledger.AddOptions{Kind: core.WorkGoal, Parent: t.Parent})
	if err != nil {
		return err
	}
	if t.DoneWhen != nil {
		if err := c.Ledger.SetDoneWhen(g.ID, *t.DoneWhen, core.AnsweredByPerson); err != nil {
			return err
		}
	}
	waits, err := c.Ledger.After(t.ID)
	if err != nil {
		return err
	}
	if err := c.Ledger.SetAfter(t.ID, nil); err != nil {
		return err
	}
	if err := c.Ledger.SetAfter(g.ID, waits); err != nil {
		return err
	}
	if err := c.moveWaiters(t.ID, g.ID); err != nil {
		return err
	}
	if err := c.Ledger.Reparent(t.ID, g.ID); err != nil {
		return err
	}
	if err := c.reshaped(t, fmt.Sprintf("split %s: %s is now a sub-goal holding it", t.ID, g.ID), core.AnsweredByPerson); err != nil {
		return err
	}
	if g, err = c.Ledger.Get(g.ID); err != nil {
		return err
	}
	return c.out(g, fmt.Sprintf("%s is now a sub-goal of its own, %s; add more beside it with wd goal add %s \"<title>\" --done-when \"<…>\"", quoted(t), g.ID, g.ID))
}

func (c *Cli) goalAfter(rest []string) error {
	if len(rest) < 2 || len(rest) > 3 {
		return fail("usage: wd goal after <id> [<id>,<id>…]")
	}
	w, err := c.subGoal(rest[1])
	if err != nil {
		return err
	}
	waits := []string{}
	if len(rest) == 3 {
		waits = idList(rest[2])
	}
	if _, err := c.orderSubGoal(w, waits, core.AnsweredByPerson); err != nil {
		return err
	}
	said, err := c.waitsWords(w, waits)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"work": w.ID, "after": waits}, said)
}

// orderSubGoal replaces what w waits on and records it; it returns the order
// as read.
func (c *Cli) orderSubGoal(w core.Work, waits []string, by string) (string, error) {
	if err := c.Ledger.SetAfter(w.ID, waits); err != nil {
		return "", fail("%v", err)
	}
	what := "none"
	if len(waits) > 0 {
		what = strings.Join(waits, ", ")
	}
	return what, c.reshaped(w, fmt.Sprintf("%s waits on: %s", w.ID, what), by)
}

// proposed reports whether goal's latest breakdown waits for approval.
func (c *Cli) proposed(goal string) (bool, error) {
	events, err := c.Ledger.Events(goal, nil)
	if err != nil {
		return false, err
	}
	return core.Proposed(goal, events), nil
}

// topGoal is the goal at the top of w's chain: w itself when no goal is above.
func (c *Cli) topGoal(w core.Work) (core.Work, error) {
	chain, err := c.chain(w)
	if err != nil || len(chain.Ancestors) == 0 {
		return w, err
	}
	return c.Ledger.Get(chain.Ancestors[0].ID)
}

func (c *Cli) goalApprove(rest []string) error {
	if len(rest) != 2 {
		return fail("usage: wd goal approve <goal>")
	}
	goal, err := c.Ledger.Get(rest[1])
	if err != nil {
		return err
	}
	pending, err := c.proposed(goal.ID)
	if err != nil {
		return err
	}
	if !pending {
		return fail("%s has no proposed breakdown to approve", goal.ID)
	}
	subs, err := c.Ledger.Children(goal.ID)
	if err != nil {
		return err
	}
	open := slices.DeleteFunc(slices.Clone(subs), func(w core.Work) bool { return core.AtRest(w.State) })
	if _, err := c.Ledger.AddEvent(goal.ID, core.EventDecision, fmt.Sprintf("%s%d sub-goals", core.ApprovedPrefix, len(open))); err != nil {
		return err
	}
	p, err := c.project(goal.Project)
	if err != nil {
		return err
	}
	top, err := c.topGoal(goal)
	if err != nil {
		return err
	}
	lines, err := c.startSubGoals(top, p, open, true)
	if err != nil {
		return err
	}
	all, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"goal": goal.ID, "tasks": all}, strings.TrimSpace(fmt.Sprintf("Approved the plan for %s: %d sub-goals\n%s", quoted(goal), len(open), strings.Join(lines, "\n"))))
}
