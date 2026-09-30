package cli

import (
	"fmt"
	"strings"

	"wd/internal/core"
	"wd/internal/ledger"
)

// roadmap is the level above a goal. It holds items, each of which becomes a
// goal: the same row, so the id an item was filed under is the id its goal
// keeps. A roadmap is a wishlist and a goal is a commitment, so an item can be
// abandoned before it was ever work.
func (c *Cli) roadmap(rest []string) error {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "add":
		return c.roadmapAdd(rest)
	case "item":
		return c.roadmapItem(rest)
	case "plan":
		return c.roadmapPlan(rest)
	case "show", "status":
		return c.roadmapShow(rest, sub)
	case "":
		return c.roadmapList()
	default:
		return fail("usage: wd roadmap (add <project> <title> | item <roadmap> <title> | plan <roadmap> | show <id> | status)")
	}
}

func (c *Cli) roadmapAdd(rest []string) error {
	if len(rest) < 3 {
		return fail("usage: wd roadmap add <project> <title> [--detail text]")
	}
	name, title := rest[1], rest[2]
	if _, err := c.project(name); err != nil {
		return err
	}
	w, err := c.Ledger.Add(name, title, ledger.AddOptions{Kind: core.WorkRoadmap, Detail: strOr(c.Args, "detail", "")})
	if err != nil {
		return err
	}
	return c.out(w, fmt.Sprintf("roadmap %s · add what it should produce: wd roadmap item %s <title>", w.ID, w.ID))
}

func (c *Cli) roadmapItem(rest []string) error {
	if len(rest) < 3 {
		return fail("usage: wd roadmap item <roadmap> <title> [--detail text]")
	}
	roadmap, err := c.Ledger.Get(rest[1])
	if err != nil {
		return err
	}
	if roadmap.Kind != core.WorkRoadmap {
		return fail("%s is a %s, not a roadmap", roadmap.ID, roadmap.Kind)
	}
	w, err := c.Ledger.Add(roadmap.Project, rest[2], ledger.AddOptions{
		Kind: core.WorkItem, Parent: &roadmap.ID, Detail: strOr(c.Args, "detail", ""),
	})
	if err != nil {
		return err
	}
	return c.out(w, fmt.Sprintf("item %s on roadmap %s — commit it: wd roadmap plan %s", w.ID, roadmap.ID, roadmap.ID))
}

// roadmapPlan translates every item still waiting into a goal, in the order
// they were filed. An item already translated is left alone, so planning a
// roadmap twice is not an error and does not reset a goal that has begun.
func (c *Cli) roadmapPlan(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd roadmap plan <roadmap> [--type query|build|fix|change|review]")
	}
	roadmap, err := c.Ledger.Get(rest[1])
	if err != nil {
		return err
	}
	if roadmap.Kind != core.WorkRoadmap {
		return fail("%s is a %s, not a roadmap", roadmap.ID, roadmap.Kind)
	}
	var gt *core.GoalType
	if t := strOr(c.Args, "type", ""); t != "" {
		parsed, err := core.ParseGoalType(t)
		if err != nil {
			return fail("%v", err)
		}
		gt = &parsed
	}
	items, err := c.Ledger.Items(roadmap.ID)
	if err != nil {
		return err
	}
	translated := make([]core.Work, 0, len(items))
	for _, it := range items {
		if !core.IsRoadmapItem(it.Kind) {
			continue
		}
		w, err := c.Ledger.Promote(it.ID, gt)
		if err != nil {
			return err
		}
		translated = append(translated, w)
	}
	table := make([]string, 0, len(translated))
	out := make([]map[string]any, 0, len(translated))
	for _, w := range translated {
		out = append(out, map[string]any{"id": w.ID, "title": w.Title, "goal_type": w.GoalType})
		table = append(table, fmt.Sprintf("%s\t%s", w.ID, w.Title))
	}
	if len(translated) == 0 {
		return c.out(out, "nothing waiting: every item is already a goal")
	}
	return c.out(out, strings.Join(table, "\n"))
}

// roadmapShow reads the roadmap and everything under it: items still waiting,
// goals already committed, and each goal's tasks and how it ended. This is the
// top-down view the UI renders.
func (c *Cli) roadmapShow(rest []string, word string) error {
	if len(rest) < 2 {
		return fail("usage: wd roadmap %s <id>", word)
	}
	w, err := c.Ledger.Get(rest[1])
	if err != nil {
		return err
	}
	if w.Kind != core.WorkRoadmap {
		return fail("%s is a %s, not a roadmap", w.ID, w.Kind)
	}
	items, err := c.Ledger.Items(w.ID)
	if err != nil {
		return err
	}
	type goalRow struct {
		Goal      core.Work   `json:"goal"`
		Tasks     []core.Work `json:"tasks"`
		Open      int         `json:"open"`
		Abandoned bool        `json:"abandoned"`
	}
	goals := make([]goalRow, 0, len(items))
	waiting := make([]core.Work, 0)
	table := []string{fmt.Sprintf("%s (%s) · %s", w.Title, w.State, w.Project)}
	for _, it := range items {
		if core.IsRoadmapItem(it.Kind) {
			waiting = append(waiting, it)
			table = append(table, fmt.Sprintf("  item    %s\t%s", it.ID, it.Title))
			continue
		}
		tasks, err := c.Ledger.Tasks(it.ID)
		if err != nil {
			return err
		}
		open := 0
		for _, t := range tasks {
			if t.State != core.StateDone && t.State != core.StateDropped {
				open++
			}
		}
		goals = append(goals, goalRow{Goal: it, Tasks: tasks, Open: open, Abandoned: it.State == core.StateAbandoned})
		table = append(table, fmt.Sprintf("  goal    %s\t%s\t%s", it.ID, goalStateWord(it), it.Title))
	}
	return c.out(map[string]any{"roadmap": w, "items": items, "waiting": waiting, "goals": goals},
		strings.Join(table, "\n"))
}

// goalStateWord says how a goal ended, in the word a reader wants: shipped,
// abandoned and why, or still under way.
func goalStateWord(w core.Work) string {
	if w.GoalType != nil && *w.GoalType == core.GoalQuery {
		return "answered"
	}
	switch w.State {
	case core.StateDone:
		return "shipped"
	case core.StateAbandoned:
		return "abandoned"
	case core.StateDropped:
		return "dropped"
	default:
		return string(w.State)
	}
}

func (c *Cli) roadmapList() error {
	all, err := c.Ledger.List(ledger.ListFilter{Kinds: []core.WorkKind{core.WorkRoadmap}})
	if err != nil {
		return err
	}
	table := make([]string, 0, len(all))
	for _, w := range all {
		table = append(table, fmt.Sprintf("%s\t%s\t%s", w.ID, w.Project, w.Title))
	}
	if len(all) == 0 {
		return c.out(all, "no roadmaps — create one: wd roadmap add <project> <title>")
	}
	return c.out(all, strings.Join(table, "\n"))
}
