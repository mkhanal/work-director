package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"wd/internal/brief"
	"wd/internal/context"
	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/open"
	"wd/internal/project"
	"wd/internal/promotion"
	"wd/internal/runner"
	"wd/internal/serve"
)

func (c *Cli) projects(rest []string) error {
	a := c.Args
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	if sub == "add" {
		if len(rest) < 3 {
			return fail("usage: wd projects add <name> <path> [--runner claude|opencode|codex] [--mode ask|auto] [--model id] [--lazyspec y|n] [--verify cmd]")
		}
		name, path := rest[1], rest[2]
		if _, ok := c.Projects[name]; ok {
			return fail("project %s already exists", name)
		}
		path, err := project.ExpandHome(path)
		if err != nil {
			return err
		}
		opts := project.NewProjectOptions{
			Runner:    strOr(a, "runner", ""),
			Mode:      strOr(a, "mode", ""),
			Model:     strOr(a, "model", ""),
			Stack:     csvFlags(a, "stack"),
			Workflows: csvFlags(a, "workflow"),
			Verify:    csvFlags(a, "verify"),
		}
		p, err := c.registerProjectFile(name, path, opts)
		if err != nil {
			return err
		}
		ls := str(a, "lazyspec")
		answer := "n"
		if ls != nil {
			answer = *ls
		} else if isTTY(os.Stdin) {
			answer, err = c.ask(fmt.Sprintf("Use the director's preferred lazyspec for %s? [y/N] ", name))
			if err != nil {
				return err
			}
		}
		if !strings.HasPrefix(strings.ToLower(answer), "y") {
			return c.out(map[string]any{"project": p, "evolution": nil},
				fmt.Sprintf("project %s created (%s); no lazyspec. Install later by adding an evolution work item.", name, path))
		}
		title, detail := project.InstallLazyspec("Adopt the director's preferred lazyspec")
		w, err := c.Ledger.Add(name, title, ledger.AddOptions{Kind: core.WorkEvolution, Detail: detail})
		if err != nil {
			return err
		}
		return c.out(map[string]any{"project": p, "evolution": w},
			fmt.Sprintf("project %s created (%s); lazyspec install queued as %s (evolution — wd spawn %s)", name, path, w.ID, w.ID))
	}
	if sub == "policy" {
		return c.projectPolicy(rest)
	}
	if sub != "" && sub != "list" {
		return fail("usage: wd projects (list | add <name> <path> | policy <name>)")
	}
	list := []*project.Project{}
	for _, name := range c.projectNames() {
		list = append(list, c.Projects[name])
	}
	table := make([]string, 0, len(list))
	for _, p := range list {
		table = append(table, fmt.Sprintf("%s\t%s\t%s\t%s", p.Name, p.Runner, p.Mode, p.Path))
	}
	return c.out(list, strings.Join(table, "\n"))
}

// projectValues lists each value a project file holds as (what sets it,
// value), in a fixed order; an unset value is "".
func projectValues(path string, o project.NewProjectOptions) [][2]string {
	out := [][2]string{{"path", path}, {"--runner", o.Runner}, {"--mode", o.Mode}, {"--model", o.Model}}
	for _, l := range []struct {
		flag   string
		values []string
	}{{"--stack", o.Stack}, {"--workflow", o.Workflows}, {"--verify", o.Verify}} {
		for _, v := range l.values {
			out = append(out, [2]string{l.flag, v})
		}
	}
	return out
}

func (c *Cli) models(rest []string) error {
	var names []string
	if len(rest) == 0 {
		avail, err := runner.Availabilities()
		if err != nil {
			return err
		}
		for _, a := range avail {
			if a.Detected {
				names = append(names, a.Runner)
			}
		}
	} else {
		known, err := runner.AllRunnerNames()
		if err != nil {
			return err
		}
		name, err := oneOf(known, rest[0], "runner")
		if err != nil {
			return err
		}
		names = []string{name}
	}
	rows := map[string][]string{}
	for _, r := range names {
		rn, err := runner.DetectedRunner(r)
		if err != nil {
			return err
		}
		models, err := rn.Models()
		if err != nil {
			return err
		}
		rows[r] = append([]string{}, models...)
	}
	var textParts []string
	for _, r := range names {
		lines := rows[r]
		if len(lines) > 0 {
			textParts = append(textParts, r+":\n  "+strings.Join(lines, "\n  "))
		} else {
			textParts = append(textParts, r+":\n  (no CLI list — pick from the provider's own picker)")
		}
	}
	return c.out(rows, strings.Join(textParts, "\n"))
}

func (c *Cli) runner(rest []string) error {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "list":
		avail, err := runner.Availabilities()
		if err != nil {
			return err
		}
		specDir, err := runner.SpecDir()
		if err != nil {
			return err
		}
		list := make([]RunnerListing, 0, len(avail))
		table := make([]string, 0, len(avail))
		for _, a := range avail {
			l := RunnerListing{Availability: a, Builtin: runner.IsBuiltin(a.Runner)}
			list = append(list, l)
			table = append(table, renderRunnerListing(l, specDir))
		}
		return c.out(list, strings.Join(table, "\n"))
	case "add":
		if len(rest) < 3 {
			return fail("usage: wd runner add <name> <file.toml>")
		}
		name, file := rest[1], rest[2]
		text, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		path, err := runner.WriteSpec(name, string(text))
		if err != nil {
			return err
		}
		return c.out(map[string]any{"runner": name, "path": path}, fmt.Sprintf("runner %s added (%s); try wd spawn <id> --runner %s", name, path, name))
	case "init":
		if len(rest) < 2 {
			return fail("usage: wd runner init <name>")
		}
		name := rest[1]
		if err := runner.CheckName(name); err != nil {
			return fail("usage: wd runner init <name> — %s", err)
		}
		dir, err := runner.SpecDir()
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name+".toml")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(runner.SpecTemplate(name)), 0o644); err != nil {
			return err
		}
		return c.out(map[string]any{"runner": name, "path": path}, fmt.Sprintf("runner spec written to %s; edit its commands — it is registered as %s", path, name))
	}
	return fail("usage: wd runner (list | add <name> <file.toml> | init <name>)")
}

// detectedRunners resolves every named runner to spawn to, so an undetected
// one fails before any worktree or ledger change.
func detectedRunners(names []string) (map[string]runner.Runner, error) {
	out := make(map[string]runner.Runner, len(names))
	for _, name := range names {
		r, err := runner.DetectedRunner(name)
		if err != nil {
			return nil, err
		}
		out[name] = r
	}
	return out, nil
}

// RunnerListing is one row of `wd runner list`: a registered runner's
// availability and whether it is built in or a spec file.
type RunnerListing struct {
	runner.Availability
	Builtin bool `json:"builtin"`
}

func renderRunnerListing(l RunnerListing, specDir string) string {
	where := specDir
	if l.Builtin {
		where = "built-in"
	}
	if l.Detected {
		return l.Runner + "\t" + where + "\tdetected " + *l.Path
	}
	return l.Runner + "\t" + where + "\tnot detected (" + l.Command + " not on PATH)"
}

func (c *Cli) add(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd add <project> <title> [--kind task|evolution|workflow|goal|roadmap|item] " +
			"[--goal <id>] [--roadmap <id>] [--type query|build|fix|change|review] [--heading <label>] [--detail text]")
	}
	name, title := rest[0], rest[1]
	if _, err := c.project(name); err != nil {
		return err
	}
	// epic was goal's old name, so --kind epic still writes a goal and
	// --epic still names the same parent --goal does.
	kindWord := strOr(c.Args, "kind", "task")
	if kindWord == "epic" {
		kindWord = "goal"
	}
	kind, err := oneOf(core.WorkKinds, kindWord, "kind")
	if err != nil {
		return err
	}
	parent := str(c.Args, "goal")
	if parent == nil {
		parent = str(c.Args, "epic")
	}
	roadmap := str(c.Args, "roadmap")
	if parent != nil && roadmap != nil {
		return fail("a work item sits under a goal or under a roadmap, not both")
	}
	if roadmap != nil {
		parent = roadmap
	}
	heading := str(c.Args, "heading")
	if parent != nil && (core.IsGoal(kind) || kind == core.WorkRoadmap) {
		return fail("a goal or roadmap cannot sit under another work item")
	}
	var goalType *core.GoalType
	if t := strOr(c.Args, "type", ""); t != "" {
		gt, err := core.ParseGoalType(t)
		if err != nil {
			return fail("%v", err)
		}
		goalType = &gt
	}
	w, err := c.Ledger.Add(name, title, ledger.AddOptions{
		Kind:     kind,
		Detail:   strOr(c.Args, "detail", ""),
		Parent:   parent,
		Heading:  heading,
		GoalType: goalType,
	})
	if err != nil {
		return err
	}
	return c.out(w, w.ID)
}

func (c *Cli) tasks(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd tasks <goal> [--all]")
	}
	id := rest[0]
	goal, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if !core.IsGoal(goal.Kind) {
		return fail("%s is not a goal", id)
	}
	all, err := c.Ledger.Tasks(id)
	if err != nil {
		return err
	}
	tasks := []core.Work{}
	for _, t := range all {
		if flag(c.Args, "all") || (t.State != core.StateDone && t.State != core.StateDropped) {
			tasks = append(tasks, t)
		}
	}
	return c.out(tasks, brief.RenderTaskBlock(tasks))
}

func (c *Cli) brief(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd brief <id>")
	}
	id := rest[0]
	text, err := c.briefFor(id)
	if err != nil {
		return err
	}
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if w.State == core.StateQueued {
		if _, err := c.Ledger.Transition(id, core.StateBriefed); err != nil {
			return err
		}
	}
	return c.out(map[string]any{"id": id, "brief": text}, text)
}

func (c *Cli) spawn(rest []string) error {
	a := c.Args
	if len(rest) == 0 {
		return fail("usage: wd spawn <id> [--runner claude|opencode|codex|myagent…] [--model id] [--worktree]")
	}
	id := rest[0]
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return err
	}
	runnerName := strOr(a, "runner", p.Runner)
	rn, err := runner.DetectedRunner(runnerName)
	if err != nil {
		return err
	}
	cwd := p.Path
	var runnerWorktree *bool
	if w.Parent != nil {
		goal, err := c.Ledger.Get(*w.Parent)
		if err != nil {
			return err
		}
		if flag(a, "worktree") {
			cwd, err = c.ensurePrivateWorktree(w, goal, p)
		} else {
			cwd, err = c.ensureSharedWorktree(goal, p)
		}
		if err != nil {
			return err
		}
	} else {
		// standalone: let the runner manage its own worktree (claude --worktree)
		wt := flag(a, "worktree")
		runnerWorktree = &wt
	}
	briefText, err := c.briefFor(id)
	if err != nil {
		return err
	}
	if w.State == core.StateQueued {
		if _, err := c.Ledger.Transition(id, core.StateBriefed); err != nil {
			return err
		}
	}
	agent := str(a, "agent")
	if agent == nil {
		agent = p.Agent
	}
	model := str(a, "model")
	if model == nil {
		model = p.Model
	}
	h, err := rn.Spawn(runner.SpawnOptions{
		Cwd:      cwd,
		Name:     slice60(fmt.Sprintf("wd-%s %s", id, w.Title)),
		Brief:    briefText,
		Agent:    agent,
		Worktree: runnerWorktree,
		Model:    model,
	})
	if err != nil {
		return err
	}
	if err := c.Ledger.SetSession(id, ledger.SessionInfo{
		Runner:  runnerName,
		Session: h.Session,
		Ref:     h.Ref,
		Cwd:     h.Cwd,
	}); err != nil {
		return err
	}
	if w.Claim == nil {
		if _, err := c.Ledger.SetClaim(id, &h.Session); err != nil {
			return err
		}
	}
	if _, err := c.Ledger.Transition(id, core.StateRunning); err != nil {
		return err
	}
	hint := rn.AttachHint(h)
	return c.out(handleWithAttach{Handle: h, Attach: hint}, fmt.Sprintf("running · %s · %s\nattach: %s", runnerName, h.Session, hint))
}

type handleWithAttach struct {
	runner.Handle
	Attach string `json:"attach"`
}

// goalLike is the goal command. `wd goal` reaches the same code so anything
// written before the rename keeps working; only the word a reader sees changed.
func (c *Cli) goalLike(cmd string, rest []string) error {
	a := c.Args
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	if cmd == "goal" && sub == "add" {
		if len(rest) < 3 {
			return fail("usage: wd goal add <project> <title> [--detail text] [--type query|build|fix|change|review]")
		}
		name, title := rest[1], rest[2]
		if _, err := c.project(name); err != nil {
			return err
		}
		opts := ledger.AddOptions{Kind: core.WorkGoal, Detail: strOr(a, "detail", "")}
		if t := strOr(a, "type", ""); t != "" {
			gt, err := core.ParseGoalType(t)
			if err != nil {
				return fail("%v", err)
			}
			opts.GoalType = &gt
		}
		w, err := c.Ledger.Add(name, title, opts)
		if err != nil {
			return err
		}
		return c.out(w, fmt.Sprintf("goal %s queued — decompose it: wd goal plan %s", w.ID, w.ID))
	}
	var goal *core.Work
	if len(rest) > 1 {
		w, err := c.Ledger.Get(rest[1])
		if err != nil {
			return err
		}
		if !core.IsGoal(w.Kind) {
			return fail("%s is a %s, not a goal", rest[1], w.Kind)
		}
		goal = &w
	}
	// wd epic is the old spelling of the same command; only the word a reader
	// sees differs, so a goal opened through either says the same thing.
	kindWord := "epic"
	if cmd == "goal" {
		kindWord = "goal"
	}
	usage := fmt.Sprintf("wd %s (plan <id> | spawn <id> | run <id> [--only|--heading] [--wait] | review <id> | status <id> | classify <id> <type> | abandon <id> <reason> | reopen <id> \"<what is being worked on>\" | release <id> \"<what was true instead>\")", cmd)
	switch sub {
	case "plan":
		if goal == nil {
			return fail("usage: wd %s plan <id> [--runner <runner>] [--model <id>]", cmd)
		}
		return c.goalPlan(cmd, *goal)
	case "spawn":
		if goal == nil {
			return fail("usage: wd %s spawn <id> [--count n] [--model id] [--runner claude|opencode|codex|claude,opencode,…]", cmd)
		}
		return c.goalSpawn(cmd, *goal)
	case "run":
		if goal == nil {
			return fail("usage: wd %s run <id> [--only id,id] [--heading label] [--runner list] [--wait] [--timeout s]", cmd)
		}
		return c.goalRun(cmd, kindWord, *goal)
	case "review":
		if goal == nil {
			return fail("usage: wd %s review <id>", cmd)
		}
		return c.goalReview(*goal)
	case "status":
		if goal == nil {
			return fail("usage: wd %s status <id>", cmd)
		}
		return c.goalStatus(kindWord, *goal)
	case "classify":
		return c.goalClassify(goal, rest)
	case "abandon":
		return c.goalAbandon(goal, rest)
	case "reopen":
		return c.goalReopen(goal, rest)
	case "release":
		return c.goalRelease(goal, rest)
	}
	return fail("%s", usage)
}

// abandon ends work that stopped without shipping, whatever kind it is: a
// task's pull request can go unmerged as easily as a goal's. The reason is one
// the ledger can check, and with none given it is computed from what the ledger
// already knows — a PR that was never raised, or one that was raised and never
// merged. Abandonment is detected, not declared.
func (c *Cli) abandon(rest []string) error {
	if len(rest) < 1 {
		return fail("usage: wd abandon <id> [no-pr|unmerged] [--reason \"<detail>\"]")
	}
	return c.abandonWork(rest[0], rest[1:])
}

func (c *Cli) goalAbandon(goal *core.Work, rest []string) error {
	if goal == nil {
		return fail("usage: wd goal abandon <id> [no-pr|unmerged] [--reason \"<detail>\"]")
	}
	return c.abandonWork(goal.ID, rest[1:])
}

// reopen works more on top of finished work, and says what is being worked on.
//
// The reason is not decoration. A finished goal that someone asks a question
// about has not been reopened, and a state change cannot tell that apart from
// someone building on it — both look like a person arriving at a goal that says
// done. So the command refuses without a reason and points at the reads that
// answer a question, and a goal left done and a goal picked back up stay
// different facts on the board because someone said which one happened.
func (c *Cli) reopen(rest []string) error {
	if len(rest) < 1 {
		return fail("usage: wd reopen <id> \"<what is being worked on>\"")
	}
	// One argument reaches Reopen rather than the usage line, because the reader
	// who typed it has forgotten the reason and not the command, and the reason
	// is what they need told.
	return c.reopenWork(rest[0], strings.Join(rest[1:], " "))
}

func (c *Cli) goalReopen(goal *core.Work, rest []string) error {
	if goal == nil {
		return fail("usage: wd goal reopen <id> \"<what is being worked on>\"")
	}
	return c.reopenWork(goal.ID, strings.Join(rest[2:], " "))
}

func (c *Cli) reopenWork(id, why string) error {
	w, err := c.Ledger.Reopen(id, why)
	if err != nil {
		return err
	}
	return c.out(w, fmt.Sprintf("%s reopened: %s", w.ID, why))
}

// release says a stop was a choice. Work filed as abandoned records that it
// stopped without shipping, and for a duplicate whose work landed elsewhere or a
// probe that was never meant to ship that sentence is simply false. Release
// corrects the row to dropped and files why, so the record stops claiming a
// failure.
func (c *Cli) release(rest []string) error {
	if len(rest) < 1 {
		return fail("usage: wd release <id> \"<what was true instead>\"")
	}
	// One argument reaches Release rather than the usage line, because the reader
	// who typed it has forgotten the reason and not the command.
	return c.releaseWork(rest[0], strings.Join(rest[1:], " "))
}

func (c *Cli) goalRelease(goal *core.Work, rest []string) error {
	if goal == nil {
		return fail("usage: wd goal release <id> \"<what was true instead>\"")
	}
	// rest[0] is the subcommand and rest[1] the goal, so the reason starts after
	// both — the same slice abandonWork reads its own reason from.
	return c.releaseWork(goal.ID, strings.Join(rest[2:], " "))
}

func (c *Cli) releaseWork(id, why string) error {
	w, err := c.Ledger.Release(id, why)
	if err != nil {
		return err
	}
	return c.out(w, fmt.Sprintf("%s released: %s", w.ID, why))
}

func (c *Cli) abandonWork(id string, args []string) error {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	reason := ""
	if len(args) > 0 {
		if _, err := core.ParseAbandonReason(args[0]); err != nil {
			return fail("%v", err)
		}
		reason = args[0]
	} else {
		reason, err = c.abandonReason(w)
		if err != nil {
			return err
		}
	}
	out, err := c.Ledger.Abandon(w.ID, reason, strOr(c.Args, "reason", ""))
	if err != nil {
		return err
	}
	return c.out(out, fmt.Sprintf("%s abandoned (%s): work that stopped without shipping", w.ID, reason))
}

// goalClassify says what kind of thing a goal was: a question answered or work
// done. It is recorded as a decision, so the classification is a claim someone
// or something made and can be seen and changed — not a field that quietly
// fills itself in.
func (c *Cli) goalClassify(goal *core.Work, rest []string) error {
	if goal == nil || len(rest) < 3 {
		return fail("usage: wd goal classify <id> <query|build|fix|change|review>")
	}
	gt, err := core.ParseGoalType(rest[2])
	if err != nil {
		return fail("%v", err)
	}
	w, err := c.Ledger.SetGoalType(goal.ID, gt)
	if err != nil {
		return err
	}
	return c.out(w, fmt.Sprintf("%s is a %s", w.ID, gt))
}

// abandonReason computes why a goal counts as abandoned, from what the ledger
// already holds. A goal with a landing of its own is not abandoned, whatever the
// reason offered: the change reached the product.
//
// Only the goal's own landing counts, and that is the fact this asks for — a
// commit on main and a pull request waiting to merge are both "the change left
// here", and a goal with either did not stop at nothing. The kind is not
// consulted because it does not change the answer, only who reads the record.
func (c *Cli) abandonReason(w core.Work) (string, error) {
	events, err := c.Ledger.Events(w.ID, nil)
	if err != nil {
		return "", err
	}
	for _, e := range events {
		if e.Kind == core.EventPr {
			return core.AbandonUnmerged, nil
		}
	}
	return core.AbandonNoPR, nil
}

func (c *Cli) goalPlan(cmd string, goal core.Work) error {
	a := c.Args
	p, err := c.project(goal.Project)
	if err != nil {
		return err
	}
	runnerName := strOr(a, "runner", p.Runner)
	rn, err := runner.DetectedRunner(runnerName)
	if err != nil {
		return err
	}
	agent := str(a, "agent")
	if agent == nil {
		agent = p.Agent
	}
	model := str(a, "model")
	if model == nil {
		model = p.Model
	}
	h, err := rn.Spawn(runner.SpawnOptions{
		Cwd:   p.Path,
		Name:  slice60(fmt.Sprintf("wd-%s plan", goal.ID)),
		Brief: coordinator.GoalPlanBrief(goal, p),
		Agent: agent,
		Model: model,
	})
	if err != nil {
		return err
	}
	deadline := time.Now().Add(120 * time.Second)
	var tasks []coordinator.PlanTask
	for time.Now().Before(deadline) {
		texts, err := rn.Transcript(h)
		if err != nil {
			return err
		}
		if ts := coordinator.ParsePlan(texts); len(ts) > 0 {
			tasks = ts
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if tasks == nil {
		return fail("plan session %s produced no task list", h.Session)
	}
	rows := make([]core.Work, 0, len(tasks))
	for _, t := range tasks {
		w, err := c.Ledger.Add(goal.Project, t.Title, ledger.AddOptions{
			Parent:  &goal.ID,
			Heading: &t.Heading,
		})
		if err != nil {
			return err
		}
		rows = append(rows, w)
	}
	if _, err := c.Ledger.AddEvent(goal.ID, core.EventNote, fmt.Sprintf("plan: %d tasks", len(rows))); err != nil {
		return err
	}
	if goal.State == core.StateQueued {
		if _, err := c.Ledger.Transition(goal.ID, core.StateBriefed); err != nil {
			return err
		}
	}
	type planRow struct {
		ID      string  `json:"id"`
		Heading *string `json:"heading"`
		Title   string  `json:"title"`
	}
	out := make([]planRow, 0, len(rows))
	table := make([]string, 0, len(rows))
	for _, w := range rows {
		out = append(out, planRow{ID: w.ID, Heading: w.Heading, Title: w.Title})
		table = append(table, fmt.Sprintf("%s\t%s\t%s", w.ID, strOrEmpty(w.Heading), w.Title))
	}
	return c.out(out, strings.Join(table, "\n"))
}

func (c *Cli) goalSpawn(cmd string, goal core.Work) error {
	a := c.Args
	p, err := c.project(goal.Project)
	if err != nil {
		return err
	}
	runners, err := project.ParseRunnerList(strOr(a, "runner", p.Runner), p.Runner, runner.AllRunnerNames)
	if err != nil {
		return err
	}
	detected, err := detectedRunners(runners)
	if err != nil {
		return err
	}
	count, err := positiveInt(a, "count", 1)
	if err != nil {
		return err
	}
	cwd, err := c.ensureSharedWorktree(goal, p)
	if err != nil {
		return err
	}
	briefText, err := c.briefFor(goal.ID)
	if err != nil {
		return err
	}
	if goal.State == core.StateQueued {
		if _, err := c.Ledger.Transition(goal.ID, core.StateBriefed); err != nil {
			return err
		}
	}
	sessions := []string{}
	handles := []handleWithAttach{}
	var lines []string
	var last *runner.Handle
	for i := 0; i < count; i++ {
		runnerName := runners[i%len(runners)]
		name := "solo"
		if count > 1 {
			if i == 0 {
				name = fmt.Sprintf("lead %dsessions", count)
			} else {
				name = fmt.Sprintf("worker %d/%d", i, count)
			}
		}
		rn := detected[runnerName]
		agent := str(a, "agent")
		if agent == nil {
			agent = p.Agent
		}
		model := str(a, "model")
		if model == nil {
			model = p.Model
		}
		h, err := rn.Spawn(runner.SpawnOptions{
			Cwd:   cwd,
			Name:  slice60(fmt.Sprintf("wd-%s %s", goal.ID, name)),
			Brief: briefText,
			Agent: agent,
			Model: model,
		})
		if err != nil {
			return err
		}
		last = &h
		if _, err := c.Ledger.AddEvent(goal.ID, core.EventSpawn, fmt.Sprintf("%s:%s", runnerName, h.Session)); err != nil {
			return err
		}
		sessions = append(sessions, h.Session)
		hint := rn.AttachHint(h)
		handles = append(handles, handleWithAttach{Handle: h, Attach: hint})
		lines = append(lines, fmt.Sprintf("%s  %s", h.Session, hint))
	}
	if last != nil {
		if err := c.Ledger.SetSession(goal.ID, ledger.SessionInfo{
			Runner:  last.Runner,
			Session: last.Session,
			Ref:     last.Ref,
			Cwd:     cwd,
		}); err != nil {
			return err
		}
	}
	if goal.State != core.StateRunning {
		if _, err := c.Ledger.Transition(goal.ID, core.StateRunning); err != nil {
			return err
		}
	}
	return c.out(map[string]any{"sessions": sessions, "handles": handles}, strings.Join(lines, "\n"))
}

func (c *Cli) goalRun(cmd, kindWord string, goal core.Work) error {
	a := c.Args
	p, err := c.project(goal.Project)
	if err != nil {
		return err
	}
	runners, err := project.ParseRunnerList(strOr(a, "runner", p.Runner), p.Runner, runner.AllRunnerNames)
	if err != nil {
		return err
	}
	detected, err := detectedRunners(runners)
	if err != nil {
		return err
	}
	var only []string
	if v := str(a, "only"); v != nil {
		for _, s := range strings.Split(*v, ",") {
			if t := strings.TrimSpace(s); t != "" {
				if _, err := c.Ledger.Get(t); err != nil {
					return err
				}
				only = append(only, t)
			}
		}
	}
	cwd, err := c.ensureSharedWorktree(goal, p)
	if err != nil {
		return err
	}
	heading := str(a, "heading")
	all, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return err
	}
	var children []core.Work
	for _, t := range all {
		if t.State == core.StateDone || t.State == core.StateDropped {
			continue
		}
		if len(only) > 0 && !contains(only, t.ID) {
			continue
		}
		if heading != nil && (t.Heading == nil || *t.Heading != *heading) {
			continue
		}
		children = append(children, t)
	}
	if len(children) == 0 {
		detail := ""
		if len(only) > 0 {
			detail = fmt.Sprintf(" (%s)", strings.Join(only, ", "))
		} else if heading != nil {
			detail = fmt.Sprintf(" (heading %s)", *heading)
		}
		return fail("no open tasks matched%s — wd %s plan <id> first", detail, cmd)
	}
	timeout, err := positiveInt(a, "timeout", 300)
	if err != nil {
		return err
	}
	spawned := []handleWithAttach{}
	var lines []string
	for i, child := range children {
		// Work past briefed has a session of its own, a claimed one, or is
		// waiting on a human; spawning would orphan it.
		spawnable := child.State == core.StateQueued || child.State == core.StateBriefed ||
			(child.State == core.StateRunning && child.Session == nil && child.Claim == nil)
		if !spawnable {
			continue
		}
		runnerName := runners[i%len(runners)]
		rn := detected[runnerName]
		if err := c.Ledger.SetCwd(child.ID, cwd); err != nil {
			return err
		}
		briefText, err := c.briefFor(child.ID)
		if err != nil {
			return err
		}
		agent := str(a, "agent")
		if agent == nil {
			agent = p.Agent
		}
		model := str(a, "model")
		if model == nil {
			model = p.Model
		}
		h, err := rn.Spawn(runner.SpawnOptions{
			Cwd:   cwd,
			Name:  slice60(fmt.Sprintf("wd-%s %s", child.ID, child.Title)),
			Brief: briefText,
			Agent: agent,
			Model: model,
		})
		if err != nil {
			return err
		}
		if err := c.Ledger.SetSession(child.ID, ledger.SessionInfo{
			Runner:  runnerName,
			Session: h.Session,
			Ref:     h.Ref,
			Cwd:     cwd,
		}); err != nil {
			return err
		}
		if child.Claim == nil {
			if _, err := c.Ledger.SetClaim(child.ID, &h.Session); err != nil {
				return err
			}
		}
		if child.State == core.StateQueued {
			if _, err := c.Ledger.Transition(child.ID, core.StateBriefed); err != nil {
				return err
			}
		}
		if child.State != core.StateRunning {
			if _, err := c.Ledger.Transition(child.ID, core.StateRunning); err != nil {
				return err
			}
		}
		if _, err := c.Ledger.AddEvent(goal.ID, core.EventSpawn, fmt.Sprintf("%s:%s", runnerName, h.Session)); err != nil {
			return err
		}
		hint := rn.AttachHint(h)
		spawned = append(spawned, handleWithAttach{Handle: h, Attach: hint})
		lines = append(lines, fmt.Sprintf("%s  %s", h.Session, hint))
	}
	if len(spawned) > 0 && goal.State != core.StateRunning {
		if _, err := c.Ledger.Transition(goal.ID, core.StateRunning); err != nil {
			return err
		}
	}
	if !flag(a, "wait") {
		lines = append(lines, fmt.Sprintf("%d task(s) spawned on %s", len(spawned), cwd))
		return c.out(map[string]any{"ok": true, "spawned": spawned}, strings.Join(lines, "\n"))
	}
	if !c.JSON && len(lines) > 0 {
		fmt.Fprintln(c.Stdout, strings.Join(lines, "\n"))
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	escalated := false
	passes := []coordinator.PassResult{}
	for time.Now().Before(deadline) {
		res, err := coordinator.CoordinateOnce(goal, p, c.Ledger, runner.RunnerNamed)
		if err != nil {
			return err
		}
		for _, k := range []struct {
			name string
			ids  []string
		}{{"answered", res.Answered}, {"escalated", res.Escalated}, {"reviewed", res.Reviewed}, {"blocked", res.Blocked}} {
			if len(k.ids) > 0 && !c.JSON {
				fmt.Fprintf(c.Stdout, "  %s: %s\n", k.name, strings.Join(k.ids, ", "))
			}
		}
		passes = append(passes, res)
		escalated = len(res.Escalated) > 0
		if escalated {
			break
		}
		open := 0
		tasks, err := c.Ledger.Tasks(goal.ID)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if t.State == core.StateRunning || t.State == core.StateNeedsInput {
				open++
			}
		}
		if open == 0 {
			break
		}
		time.Sleep(time.Second)
	}
	openCt := 0
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.State == core.StateRunning || t.State == core.StateNeedsInput {
			openCt++
		}
	}
	result := map[string]any{"open": openCt, "spawned": spawned, "passes": passes}
	if openCt == 0 {
		return c.out(result, fmt.Sprintf("%s driven to completion — every open task closed", kindWord))
	}
	human := "needs a human"
	if escalated {
		human += " (a question was escalated)"
	}
	return c.out(result, fmt.Sprintf("%s still has %d open task(s) — %s or another wd %s run --wait", kindWord, openCt, human, cmd))
}

func contains(list []string, x string) bool {
	for _, s := range list {
		if s == x {
			return true
		}
	}
	return false
}

func (c *Cli) goalReview(goal core.Work) error {
	p, err := c.project(goal.Project)
	if err != nil {
		return err
	}
	res, err := coordinator.CoordinateOnce(goal, p, c.Ledger, runner.RunnerNamed)
	if err != nil {
		return err
	}
	// The children that just reported DONE taught something or did not, and
	// this is the second place their transcript is fresh. Reflecting here
	// catches a whole pass in one command instead of one report at a time.
	reflected := []reflectResult{}
	if !flag(c.Args, "no-reflect") {
		for _, id := range res.Reviewed {
			w, err := c.Ledger.Get(id)
			if err != nil {
				return err
			}
			h, ok, err := coordinator.Handle(c.Ledger, w, p)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			r, err := runner.DetectedRunner(h.Runner)
			if err != nil {
				return err
			}
			texts, err := r.Transcript(h)
			if err != nil {
				return err
			}
			one, err := c.Reflect(w, texts)
			if err != nil {
				// A reflection that cannot run is recorded on the child
				// and does not lose the coordination pass.
				one = reflectResult{Body: "reflect failed: " + err.Error()}
			}
			reflected = append(reflected, one)
		}
	}
	return c.out(map[string]any{"pass": res, "reflect": reflected},
		fmt.Sprintf("answered: %s\nescalated: %s\nreviewed: %s\nblocked: %s\nwaiting: %s",
			joinOrNone(res.Answered), joinOrNone(res.Escalated), joinOrNone(res.Reviewed), joinOrNone(res.Blocked), joinOrNone(res.Waiting)))
}

func joinOrNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

func (c *Cli) goalStatus(kindWord string, goal core.Work) error {
	all, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return err
	}
	tasks := []core.Work{}
	for _, t := range all {
		if t.State != core.StateDone && t.State != core.StateDropped {
			tasks = append(tasks, t)
		}
	}
	return c.out(map[string]any{"goal": goal, "open": len(tasks), "tasks": tasks},
		fmt.Sprintf("%s (%s) · %d open\n%s", goal.Title, goal.State, len(tasks), brief.RenderTaskBlock(tasks)))
}

func (c *Cli) send(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd send <id> <text>")
	}
	if err := c.sendTo(rest[0], rest[1]); err != nil {
		return err
	}
	return c.out(map[string]any{"ok": true}, "sent")
}

func (c *Cli) attach(rest []string) error {
	a := c.Args
	if len(rest) < 2 {
		return fail("usage: wd attach <id> <session> [--runner <runner>] [--model <id>] [--ref <ref>] [--cwd <dir>]")
	}
	id, session := rest[0], rest[1]
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return err
	}
	runnerName := strOr(a, "runner", strOrEmpty(w.Runner))
	if runnerName == "" {
		runnerName = p.Runner
	}
	ref := str(a, "ref")
	cwd := str(a, "cwd")
	if cwd == nil {
		cwd = w.Cwd
	}
	if cwd == nil && w.Parent != nil {
		wts, err := c.Ledger.Worktrees(*w.Parent)
		if err != nil {
			return err
		}
		for _, wt := range wts {
			if wt.Kind == core.WorktreeShared && wt.State == core.WorktreeActive {
				cwd = &wt.Path
			}
		}
	}
	if cwd == nil {
		cwd = &p.Path
	}
	rn, err := runner.RunnerNamed(runnerName)
	if err != nil {
		return err
	}
	if err := c.Ledger.SetSession(id, ledger.SessionInfo{
		Runner:  runnerName,
		Session: session,
		Ref:     ref,
		Cwd:     *cwd,
	}); err != nil {
		return err
	}
	if w.Claim == nil {
		if _, err := c.Ledger.SetClaim(id, &session); err != nil {
			return err
		}
	}
	attachEvent := fmt.Sprintf("%s:%s", runnerName, session)
	if ref != nil {
		attachEvent += " ref=" + *ref
	}
	if _, err := c.Ledger.AddEvent(id, core.EventAttach, attachEvent); err != nil {
		return err
	}
	if w.State == core.StateQueued || w.State == core.StateBriefed {
		if _, err := c.Ledger.Transition(id, core.StateRunning); err != nil {
			return err
		}
	}
	after, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	hint := rn.AttachHint(runner.Handle{Runner: runnerName, Session: session, Ref: ref, Cwd: *cwd})
	return c.out(map[string]any{"id": id, "runner": runnerName, "session": session, "ref": ref, "cwd": *cwd},
		fmt.Sprintf("attached %s:%s → %s (now %s)\nattach: %s", runnerName, session, w.Title, after.State, hint))
}

func (c *Cli) report(rest []string) error {
	a := c.Args
	if len(rest) == 0 {
		return fail("usage: wd report <id> [<text>] [--tail n]")
	}
	n, err := positiveInt(a, "tail", 1)
	if err != nil {
		return err
	}
	id := rest[0]
	if len(rest) > 1 {
		// The report is the work's own account of what it did. Work the
		// director builds in its own session has no executor to read one from,
		// and the report is not a gate on honesty — it is the author's own
		// verdict, which a person filing it by hand is equally entitled to
		// give. Verify and pull request stay exactly as they were.
		return c.filedReport(id, strings.Join(rest[1:], " "))
	}
	h, err := c.reportHandle(id)
	if err != nil {
		return err
	}
	r, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return err
	}
	texts, err := r.Transcript(h)
	if err != nil {
		return err
	}
	last := ""
	if len(texts) > 0 {
		last = texts[len(texts)-1]
	}
	status := matchStatus(last)
	runnerStatus, err := r.Status(h)
	if err != nil {
		return err
	}
	if status == "" {
		return fail("no report filed for %s: the last message of %s session %s has no STATUS line (runner %s). "+
			"The report is the executor's done checkpoint; wait for it, then run wd report again.",
			id, h.Runner, h.Session, runnerStatus)
	}
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if w.State == core.StateRunning || w.State == core.StateNeedsInput {
		known, err := coordinator.Known(c.Ledger, w)
		if err != nil {
			return err
		}
		if _, err := coordinator.Coordinate(w, known, c.Ledger, r, h, texts); err != nil {
			return err
		}
	}
	// Reflection rides on the report, where the transcript is fresh and the
	// session has just said its piece. It files feedback and cannot touch the
	// work's state, so a wrong lesson never moves a gate. A reflection that
	// cannot run is recorded, not fatal: the report is the valuable result and
	// must not be lost to a model that would not start.
	reflected := reflectResult{}
	if status == "DONE" && !flag(c.Args, "no-reflect") {
		if r, rerr := c.Reflect(w, texts); rerr != nil {
			reflected = reflectResult{Body: "reflect failed: " + rerr.Error()}
		} else {
			reflected = r
		}
	}
	messages := sliceLastN(texts, n)
	if messages == nil {
		messages = []string{}
	}
	return c.out(map[string]any{"status": runnerStatus, "report": status, "messages": messages, "reflect": reflected},
		fmt.Sprintf("%s · %s\n%s", runnerStatus, status, strings.Join(messages, "\n---\n")))
}

// filedReport files a report whose text is given rather than read from a
// session, and moves the work to the state the report's status names. It is the
// same gate by a different road: the report is still the work author's own
// account, the verify and pull-request gates are untouched, and a text with no
// STATUS line files nothing at all.
//
// The provenance line is on the report because a reader comparing two reports
// has to be able to tell one read from a session from one handed over, and
// guessing is worse than knowing.
func (c *Cli) filedReport(id, text string) error {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	status := matchStatus(text)
	if status == "" {
		return fail("no report filed for %s: the text has no STATUS line, so it says nothing about what happened. "+
			"Give the report a line of the form STATUS: DONE (or BLOCKED).", id)
	}
	from := "filed as text, not read from a session: this work has no executor session"
	if session := workSession(w); session != "" {
		from = "filed as text, not read from session " + session
	}
	report := status + "\n" + from + "\n" + text
	if _, err := coordinator.FileReport(c.Ledger, w, status, report); err != nil {
		return err
	}
	// Reflection rides on the report, and the report is the text here: it is the
	// finished session's own account either way, and reflecting on a supplied
	// report teaches the same thing reflecting on a transcript does.
	reflected := reflectResult{}
	if status == "DONE" && !flag(c.Args, "no-reflect") {
		if r, rerr := c.Reflect(w, []string{text}); rerr != nil {
			reflected = reflectResult{Body: "reflect failed: " + rerr.Error()}
		} else {
			reflected = r
		}
	}
	return c.out(map[string]any{"status": "filed", "report": status, "source": "text",
		"messages": []string{text}, "reflect": reflected},
		fmt.Sprintf("filed · %s\n%s", status, text))
}

// workSession is the session or claim serving work, or "" when nothing is: work
// the director builds in its own session has neither, and that is a fact about
// the work worth saying out loud rather than leaving a reader to infer.
func workSession(w core.Work) string {
	if w.Session != nil && *w.Session != "" {
		return *w.Session
	}
	if w.Claim != nil {
		return *w.Claim
	}
	return ""
}

// reportHandle resolves the session a report is read from, and when there is
// none it names the way out instead of only the wall.
func (c *Cli) reportHandle(id string) (runner.Handle, error) {
	h, err := c.handle(id)
	if err == nil {
		return h, nil
	}
	w, gerr := c.Ledger.Get(id)
	if gerr == nil && workSession(w) == "" {
		return runner.Handle{}, fail("work %s has no session to read a report from: it was built in the director's own session, "+
			"so file the report as text — wd report %s \"DONE\nSTATUS: DONE\n<what changed and how it was verified>\"", id, id)
	}
	return h, err
}

var statusRe = regexp.MustCompile(`STATUS:\s*(DONE|BLOCKED|NEEDS-INPUT)`)

func matchStatus(last string) string {
	m := statusRe.FindStringSubmatch(last)
	if m == nil {
		return ""
	}
	return m[1]
}

func (c *Cli) verify(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd verify <id>")
	}
	id := rest[0]
	pass, body, results, err := c.runVerify(id)
	if err != nil {
		return err
	}
	if err := c.out(map[string]any{"pass": pass, "results": results}, body); err != nil {
		return err
	}
	if !pass {
		return fail("verify failed for %s", id)
	}
	return nil
}

// runVerify runs the project's verify commands in the work's own directory and
// records the result. It is separate from the command so a driven run can put a
// gate through it without printing a second document into the middle of its own
// output, which on the JSON rail would be two documents where there must be one.
func (c *Cli) runVerify(id string) (pass bool, body string, results []string, err error) {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return false, "", nil, err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return false, "", nil, err
	}
	cwd, err := c.workDir(w, p)
	if err != nil {
		return false, "", nil, err
	}
	if len(p.Verify) == 0 {
		return false, "", nil, fail("project %s lists no verify commands; add them under verify: in its project file", p.Name)
	}
	results = []string{}
	pass = true
	for _, cmdline := range p.Verify {
		r, rerr := runner.Run([]string{"bash", "-lc", cmdline}, cwd)
		if rerr != nil {
			return false, "", nil, rerr
		}
		if r.Code != 0 {
			pass = false
		}
		results = append(results, fmt.Sprintf("%s → %d\n%s", cmdline, r.Code, lastLines(strings.TrimSpace(r.Stdout+r.Stderr), 5)))
	}
	body = "fail\n"
	if pass {
		body = "pass\n"
	}
	body += strings.Join(results, "\n")
	if _, err := c.Ledger.AddEvent(id, core.EventVerify, body); err != nil {
		return false, "", nil, err
	}
	return pass, body, results, nil
}

func lastLines(text string, n int) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// decide records a decision. With no flags it is one line of prose, which is
// what a person types. With --question it is a structured claim a review can
// read, and --effective says when the answer took hold as distinct from when it
// was written: the two genuinely differ when a model answered in one session
// and the answer settled in a later one.
func (c *Cli) decide(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd decide <id> <text> [--question q --answer a --source s --runner r --model m --tokens n --effective <when>]")
	}
	id, text := rest[0], strings.Join(rest[1:], " ")
	question := strOr(c.Args, "question", "")
	answer := strOr(c.Args, "answer", "")
	if question == "" && answer == "" {
		e, err := c.Ledger.AddEvent(id, core.EventDecision, text)
		if err != nil {
			return err
		}
		return c.out(e, "decided: "+e.Body)
	}
	d := core.Decision{
		Question: question,
		Answer:   answer,
		Source:   strOr(c.Args, "source", ""),
		Runner:   strOr(c.Args, "runner", ""),
		Model:    strOr(c.Args, "model", ""),
		Tokens:   intOr(c.Args, "tokens", 0),
	}
	effective := str(c.Args, "effective")
	e, err := c.Ledger.Decide(id, text, d, effective)
	if err != nil {
		return err
	}
	return c.out(e, "decided: "+e.Body)
}

// pr records where the work landed. With a URL that is the answer. With no URL
// it works out the answer: the commit the work's own branch is at, on the
// project's remote.
//
// The gate's real question is "did this land somewhere a person can see it", and
// a commit the director pushed to main answers it as well as a pull request does
// — indeed better, because it is already true. Making the tool derive it is what
// lets the gate be checked without a person present to type a link, which is the
// only way a loop that runs unattended can pass it.
func (c *Cli) pr(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd pr <id> [<url>]")
	}
	url, err := c.recordPR(rest[0], rest[1:])
	if err != nil {
		return err
	}
	return c.out(map[string]any{"ok": true, "url": url}, "recorded "+url)
}

// recordPR files where the work landed and returns the url, so a driven run can
// put the gate through it without a second document on its own output.
//
// The kind is recorded because the CLI is the only thing that knows it: a url
// handed to wd pr is a pull request, and a link worked out from the work's own
// branch is a commit that is already in the product. An audit that cannot tell
// them apart cannot say whether a change was ever reviewed, and inferring it
// from the shape of a url would be guessing at a fact the tool was told.
func (c *Cli) recordPR(id string, rest []string) (string, error) {
	kind, url := core.LandingPullRequest, ""
	if len(rest) > 0 {
		url = strings.Join(rest, " ")
	} else {
		landed, err := c.landedAt(id)
		if err != nil {
			return "", err
		}
		kind, url = core.LandingCommit, landed
	}
	if _, err := c.Ledger.AddEvent(id, core.EventPr, string(kind)+" "+url); err != nil {
		return "", err
	}
	return url, nil
}

// landedAt is the permalink for the commit the work landed on. It is a real
// place the change can be read, which is the whole of what the pull-request gate
// asks, and it is only given when the commit is on a remote branch: a permalink
// to a commit nobody has pushed is a link that does not open, and a gate that
// can be satisfied by a dead link is not a gate.
func (c *Cli) landedAt(id string) (string, error) {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return "", err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return "", err
	}
	dir, err := c.workDir(w, p)
	if err != nil {
		return "", err
	}
	head, err := c.runGit([]string{"-C", dir, "rev-parse", "HEAD"}, dir)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(head.Stdout)
	if head.Code != 0 || sha == "" {
		return "", fail("work %s has no commit to point at: %s has nothing at HEAD", id, dir)
	}
	// The remote first, then what it has: "not pushed" and "no remote to push
	// to" are different problems, and a repository with no remote would
	// otherwise be told to push somewhere it has never heard of.
	remote, err := c.runGit([]string{"-C", dir, "remote"}, dir)
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(remote.Stdout)
	if len(remotes) == 0 {
		return "", fail("work %s has no git remote, so there is nowhere to point at. Pass the url: wd pr %s <url>", id, id)
	}
	name := remotes[0]
	if slices.Contains(remotes, "origin") {
		name = "origin"
	}
	// Contained in a remote branch, or not a link anybody can open.
	contains, err := c.runGit([]string{"-C", dir, "branch", "-r", "--contains", sha}, dir)
	if err != nil {
		return "", err
	}
	if contains.Code != 0 || strings.TrimSpace(contains.Stdout) == "" {
		return "", fail("work %s is not pushed: %s is on no remote branch, so there is nowhere to point at. "+
			"Push it, or pass the url: wd pr %s <url>", id, slice60(sha), id)
	}
	url, err := c.runGit([]string{"-C", dir, "remote", "get-url", name}, dir)
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(url.Stdout)
	if base == "" {
		return "", fail("work %s: remote %s has no url. Pass the url: wd pr %s <url>", id, name, id)
	}
	return commitLink(base, sha), nil
}

// commitLink turns a remote url and a sha into a link a person can open.
// git@github.com:owner/repo.git and https://github.com/owner/repo both have to
// work, because which one a person has is not something the tool should care
// about, and a gate that only reads one form is a gate that fails for half the
// people using it.
func commitLink(remote, sha string) string {
	base := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	switch {
	case strings.HasPrefix(base, "git@"):
		return "https://" + strings.Replace(base[len("git@"):], ":", "/", 1) + "/commit/" + sha
	case strings.HasPrefix(base, "ssh://git@"):
		rest := strings.TrimPrefix(base, "ssh://git@")
		rest = strings.Replace(rest, ":", "/", 1)
		return "https://" + rest + "/commit/" + sha
	case strings.HasPrefix(base, "http://"), strings.HasPrefix(base, "https://"):
		return base + "/commit/" + sha
	}
	return base + "/commit/" + sha
}

// workDir is the directory work is verified and landed from: its own, else the
// shared worktree under a goal, else the project. Verify and the landing link
// must agree about this, or a goal could be verified in one tree and linked from
// another.
func (c *Cli) workDir(w core.Work, p *project.Project) (string, error) {
	if w.Cwd != nil {
		return *w.Cwd, nil
	}
	if core.IsGoal(w.Kind) {
		wts, err := c.Ledger.Worktrees(w.ID)
		if err != nil {
			return "", err
		}
		for _, wt := range wts {
			if wt.Kind == core.WorktreeShared {
				return wt.Path, nil
			}
		}
	}
	return p.Path, nil
}

func (c *Cli) softDone(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd soft-done <id> [--no-code]")
	}
	id := rest[0]
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	codeChanged := true
	if w.Parent != nil {
		codeChanged = false
	} else if !core.IsGoal(w.Kind) {
		codeChanged = !flag(c.Args, "no-code")
	}
	after, err := c.Ledger.SoftDone(id, codeChanged)
	if err != nil {
		if nr, ok := err.(core.NotReady); ok {
			return fail("%s", nr.Error())
		}
		return err
	}
	return c.out(after, "soft-done")
}

func (c *Cli) set(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd set <id> <state>")
	}
	id, to := rest[0], rest[1]
	state, err := oneOf(settableStates, to, "state")
	if err != nil {
		return err
	}
	if state == core.StateSoftDone {
		return c.softDone(rest[:1])
	}
	if state == core.StateDone {
		return c.done(rest[:1])
	}
	after, err := c.Ledger.Transition(id, state)
	if err != nil {
		if _, ok := err.(core.IllegalTransition); ok {
			return fail("%s", err.Error())
		}
		return err
	}
	return c.out(after, to)
}

func (c *Cli) done(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd done <id> [--cancelled <reason>]")
	}
	id := rest[0]
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	// Closing straight from queued, briefed or blocked skips the report, verify
	// and pull-request gate on purpose: the work is abandoned, not completed.
	// That makes the reason the only thing distinguishing it from a real
	// completion later on, so it is required rather than optional.
	reason := str(c.Args, "cancelled")
	direct := core.ClosesDirectly(w.State)
	if direct && (reason == nil || *reason == "") {
		return fail("closing %s from %s skips the report, verify and pull-request gate: pass --cancelled \"<reason>\"", id, w.State)
	}
	after, err := c.Ledger.Transition(id, core.StateDone)
	if err != nil {
		if _, ok := err.(core.IllegalTransition); ok {
			return fail("%s", err.Error())
		}
		return err
	}
	if direct {
		if _, err := c.Ledger.AddEvent(id, core.EventDecision, "cancelled: "+*reason); err != nil {
			return err
		}
	}
	removed, kept, err := c.releaseWorktrees(after)
	if err != nil {
		return err
	}
	lines := []string{"done"}
	for _, wt := range removed {
		lines = append(lines, "removed worktree "+wt.Path)
	}
	for _, k := range kept {
		if _, err := c.Ledger.AddConcern(id, fmt.Sprintf("%s. Run wd worktree remove %s once that is resolved.", k, id)); err != nil {
			return err
		}
		lines = append(lines, k.String())
	}
	return c.out(after, strings.Join(lines, "\n"))
}

// statusRow is one wd status row: the work item and whether it is stale.
type statusRow struct {
	core.Work
	Stale bool `json:"stale"`
}

func (c *Cli) status(rest []string) error {
	a := c.Args
	filter := ledger.ListFilter{}
	if v := str(a, "project"); v != nil {
		filter.Project = v
	}
	items, err := c.Ledger.List(filter)
	if err != nil {
		return err
	}
	open := []statusRow{}
	at := time.Now()
	for _, w := range items {
		if flag(a, "all") || (w.State != core.StateDone && w.State != core.StateDropped) {
			stale, err := core.Stale(w, at)
			if err != nil {
				return err
			}
			open = append(open, statusRow{Work: w, Stale: stale})
		}
	}
	table := make([]string, 0, len(open))
	for _, r := range open {
		w := r.Work
		line := fmt.Sprintf("%s\t%-11s\t%s\t%s\t%s\t%s", w.ID, w.State, w.Project, w.Kind, strOrEmpty(w.Heading), w.Title)
		if w.Runner != nil {
			ref := w.Ref
			if ref == nil {
				ref = w.Session
			}
			line += fmt.Sprintf("\t%s:%s", *w.Runner, strOrEmpty(ref))
		}
		if r.Stale {
			line += fmt.Sprintf("\tstale since %s", w.Updated[:len("2006-01-02")])
		}
		table = append(table, line)
	}
	tableText := strings.Join(table, "\n")
	if tableText == "" {
		tableText = "nothing open"
	}
	if !c.JSON {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		head, err := context.WorkspaceContext(wd)
		if err != nil {
			return err
		}
		lines := []string{context.ContextLine(head)}
		seen := map[string]bool{}
		for _, r := range open {
			w := r.Work
			if seen[w.Project] {
				continue
			}
			seen[w.Project] = true
			p, ok := c.Projects[w.Project]
			if !ok {
				continue
			}
			c, err := context.WorkspaceContext(p.Path)
			if err != nil {
				return err
			}
			branch := "no commits"
			if c.Branch != nil {
				branch = *c.Branch
			}
			line := fmt.Sprintf("  %s: %s", w.Project, branch)
			if c.Linked && c.Repo != nil {
				line += " @ " + *c.Repo
			}
			if len(c.Changed) > 0 {
				line += fmt.Sprintf(" (%d changed)", len(c.Changed))
			}
			lines = append(lines, line)
		}
		fmt.Fprintln(c.Stdout, strings.Join(lines, "\n"))
	}
	return c.out(open, tableText)
}

func (c *Cli) contextCmd(rest []string) error {
	target := ""
	if len(rest) > 0 {
		target = rest[0]
	}
	if target != "" {
		if _, ok := c.Projects[target]; !ok {
			has, err := c.Ledger.Has(target)
			if err != nil {
				return err
			}
			if has {
				return c.contextWork(target)
			}
		}
		p, err := c.project(target)
		if err != nil {
			return err
		}
		ws, err := context.WorkspaceContext(p.Path)
		if err != nil {
			return err
		}
		return c.out(map[string]any{"project": p.Name, "path": p.Path, "workspace": ws, "runner": p.Runner, "mode": p.Mode},
			fmt.Sprintf("project %s\n%s\nrunner: %s · mode: %s", p.Name, context.ContextLine(ws), p.Runner, p.Mode))
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := context.WorkspaceContext(wd)
	if err != nil {
		return err
	}
	return c.out(ws, context.ContextLine(ws))
}

func (c *Cli) contextWork(id string) error {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	h, err := c.handle(id)
	if err != nil {
		return err
	}
	ws, err := context.WorkspaceContext(h.Cwd)
	if err != nil {
		return err
	}
	link := open.OpenLink(open.OpenTarget{Path: h.Cwd})
	return c.out(map[string]any{"id": id, "runner": h.Runner, "session": h.Session, "ref": h.Ref, "cwd": h.Cwd, "workspace": ws},
		fmt.Sprintf("%s (%s)\n%s\nrunner: %s · session: %s\n%s", w.Title, w.State, context.ContextLine(ws), h.Runner, h.Session, link))
}

func (c *Cli) open(rest []string) error {
	var base, fileArg string
	switch len(rest) {
	case 1:
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		base, fileArg = wd, rest[0]
	case 2:
		h, err := c.handle(rest[0])
		if err != nil {
			return err
		}
		base, fileArg = h.Cwd, rest[1]
	default:
		return fail("usage: wd open [<id>] <path>[:<line>]")
	}
	target := open.ParseTarget(fileArg, base)
	link := open.OpenLink(target)
	result := map[string]any{"path": target.Path, "line": target.Line, "editor": nil}
	editor, err := open.DetectEditor()
	if err != nil {
		return err
	}
	if editor == nil {
		return c.out(result, fmt.Sprintf("%s\nno editor on this host — click the link or open %s manually", link, target.Path))
	}
	ok, err := open.OpenInEditor(*editor, target)
	if err != nil {
		return err
	}
	if !ok {
		return fail("%s failed to open %s", editor.Label, target.Path)
	}
	result["editor"] = editor.Label
	return c.out(result, fmt.Sprintf("opened in %s: %s\n%s", editor.Label, open.FileLabel(target), link))
}

func (c *Cli) claim(rest []string) error {
	a := c.Args
	if len(rest) == 0 {
		return fail("usage: wd claim <id> [<who>] [--drop]")
	}
	id := rest[0]
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if flag(a, "drop") {
		after, err := c.Ledger.SetClaim(id, nil)
		if err != nil {
			return err
		}
		return c.out(after, "dropped")
	}
	who := w.Session
	if len(rest) > 1 {
		who = &rest[1]
	}
	if who == nil {
		return fail("usage: wd claim <id> <who> — %s has no session to claim for", id)
	}
	if *who == "" {
		return fail("usage: wd claim <id> <who> — who cannot be empty")
	}
	after, err := c.Ledger.SetClaim(id, who)
	if err != nil {
		return err
	}
	return c.out(after, "claimed by "+nullStr(who))
}

// nullStr renders a nullable string the way JavaScript's template literals do:
// nil becomes "null", not "".
func nullStr(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}

func (c *Cli) impact(rest []string) error {
	a := c.Args
	if len(rest) == 0 {
		return fail("usage: wd impact <id> <+path|-path> [--clear]")
	}
	id := rest[0]
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if flag(a, "clear") {
		after, err := c.Ledger.SetImpact(id, []string{})
		if err != nil {
			return err
		}
		return c.out(after, "cleared")
	}
	if len(rest) < 2 || len(rest[1]) < 2 || (rest[1][0] != '+' && rest[1][0] != '-') {
		return fail("usage: wd impact <id> <+path|-path>")
	}
	op, path := rest[1][0], rest[1][1:]
	cur := core.SplitImpact(w.Impact)
	var next []string
	if op == '+' {
		next = append(next, cur...)
		if !contains(cur, path) {
			next = append(next, path)
		}
	} else {
		for _, x := range cur {
			if x != path {
				next = append(next, x)
			}
		}
	}
	if _, err := c.Ledger.SetImpact(id, next); err != nil {
		return err
	}
	fresh, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	impact := core.SplitImpact(fresh.Impact)
	if len(impact) == 0 {
		return c.out(fresh, "no impact")
	}
	return c.out(fresh, strings.Join(impact, "\n"))
}

func (c *Cli) conflict(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd conflict <goal>")
	}
	id := rest[0]
	goal, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if !core.IsGoal(goal.Kind) {
		return fail("%s is not a goal", id)
	}
	cs, err := c.Ledger.Conflicts(id)
	if err != nil {
		return err
	}
	text := "no conflicts among active claims"
	if len(cs) > 0 {
		var lines []string
		for _, c := range cs {
			lines = append(lines, fmt.Sprintf("%s ↔ %s on %s", c.A, c.B, strings.Join(c.Paths, ", ")))
		}
		text = strings.Join(lines, "\n")
	}
	return c.out(cs, text)
}

func (c *Cli) worktree(rest []string) error {
	a := c.Args
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	if sub == "attach" {
		if len(rest) < 3 {
			return fail("usage: wd worktree attach <id> <path> [--branch <b>]")
		}
		id := rest[1]
		path, err := project.ExpandHome(rest[2])
		if err != nil {
			return err
		}
		wt, err := c.Ledger.AddWorktree(id, ledger.WorktreeInfo{
			Path:   path,
			Branch: str(a, "branch"),
			Kind:   core.WorktreePrivate,
			Origin: core.OriginAttached,
		})
		if err != nil {
			return err
		}
		return c.out(wt, fmt.Sprintf("attached private worktree %d → %s", wt.ID, wt.Path))
	}
	if len(rest) < 2 {
		return fail("usage: wd worktree (attach <id> <path> | list <id> | remove <id>)")
	}
	id := rest[1]
	if sub == "remove" {
		w, err := c.Ledger.Get(id)
		if err != nil {
			return err
		}
		if w.State != core.StateDone {
			return fail("work %s is %s; wd removes its worktrees only once it is done", id, w.State)
		}
		removed, kept, err := c.releaseWorktrees(w)
		if err != nil {
			return err
		}
		var lines []string
		for _, wt := range removed {
			lines = append(lines, "removed worktree "+wt.Path)
		}
		if len(kept) > 0 {
			for _, k := range kept {
				lines = append(lines, k.String())
			}
			return fail("%s", strings.Join(lines, "\n"))
		}
		if len(lines) == 0 {
			lines = append(lines, "no worktrees to remove")
		}
		return c.out(removed, strings.Join(lines, "\n"))
	}
	if sub == "list" {
		if _, err := c.Ledger.Get(id); err != nil {
			return err
		}
		wts, err := c.Ledger.Worktrees(id)
		if err != nil {
			return err
		}
		text := "no worktrees"
		if len(wts) > 0 {
			var lines []string
			for _, wt := range wts {
				lines = append(lines, fmt.Sprintf("%d\t%s\t%s\t%s\t%s", wt.ID, wt.Kind, wt.State, strOrEmpty(wt.Branch), wt.Path))
			}
			text = strings.Join(lines, "\n")
		}
		return c.out(wts, text)
	}
	return fail("usage: wd worktree <attach|list|remove>")
}

func (c *Cli) merge(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd merge <id>")
	}
	id := rest[0]
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if w.Parent == nil {
		return fail("merge is only for tasks under a goal")
	}
	goal, err := c.Ledger.Get(*w.Parent)
	if err != nil {
		return err
	}
	if _, err := c.project(w.Project); err != nil {
		return err
	}
	shared, err := c.Ledger.Worktrees(goal.ID)
	if err != nil {
		return err
	}
	var sharedWt *core.Worktree
	for i := range shared {
		if shared[i].Kind == core.WorktreeShared && shared[i].State == core.WorktreeActive {
			sharedWt = &shared[i]
		}
	}
	if sharedWt == nil {
		return fail("goal %s has no active shared worktree", goal.ID)
	}
	wts, err := c.Ledger.Worktrees(id)
	if err != nil {
		return err
	}
	var wt *core.Worktree
	for i := range wts {
		if wts[i].Kind == core.WorktreePrivate && wts[i].State == core.WorktreeActive {
			wt = &wts[i]
		}
	}
	if wt == nil {
		return fail("work %s has no active private worktree; register one with wd worktree attach", id)
	}
	verifies, err := c.Ledger.Events(id, kindPtr(core.EventVerify))
	if err != nil {
		return err
	}
	if len(verifies) == 0 || !strings.HasPrefix(verifies[len(verifies)-1].Body, "pass") {
		return fail("refusing merge: run wd verify <id> until it passes")
	}
	cs, err := c.Ledger.Conflicts(goal.ID)
	if err != nil {
		return err
	}
	var mine []string
	for _, c := range cs {
		other := c.A
		if c.A == id {
			other = c.B
		}
		if c.A == id || c.B == id {
			mine = append(mine, other)
		}
	}
	if len(mine) > 0 {
		return fail("refusing merge: conflicts with %s; raise or resolve a concern first", strings.Join(mine, ", "))
	}
	branch := branchFor(w)
	if wt.Branch != nil {
		branch = *wt.Branch
	}
	r, err := c.runGit([]string{"merge", "--no-edit", "--no-ff", branch}, sharedWt.Path)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		if _, err := c.Ledger.AddEvent(id, core.EventNote, fmt.Sprintf("merge of %s conflicted in %s; resolve then merge again", branch, nullStr(sharedWt.Branch))); err != nil {
			return err
		}
		return fail("merge conflicted: %s", sliceFirst500(strings.TrimSpace(r.Stdout+r.Stderr)))
	}
	if _, err := c.Ledger.SetWorktreeState(wt.ID, core.WorktreeMerged); err != nil {
		return err
	}
	if _, err := c.Ledger.AddEvent(id, core.EventNote, fmt.Sprintf("merged %s into %s", branch, nullStr(sharedWt.Branch))); err != nil {
		return err
	}
	return c.out(map[string]any{"merged": branch}, fmt.Sprintf("merged %s into %s", branch, nullStr(sharedWt.Branch)))
}

func sliceFirst500(s string) string {
	r := []rune(s)
	if len(r) > 500 {
		return string(r[:500])
	}
	return s
}

func (c *Cli) concern(rest []string) error {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "add":
		if len(rest) < 3 {
			return fail("usage: wd concern add <work> <text>")
		}
		cx, err := c.Ledger.AddConcern(rest[1], rest[2])
		if err != nil {
			return err
		}
		return c.out(cx, fmt.Sprintf("concern %d on %s", cx.ID, rest[1]))
	case "resolve":
		if len(rest) < 3 {
			return fail("usage: wd concern resolve <id> <decision>")
		}
		id, err := strconv.Atoi(rest[1])
		if err != nil {
			return fail("usage: wd concern resolve <id> <decision>")
		}
		cx, err := c.Ledger.ResolveConcern(id, rest[2])
		if err != nil {
			return err
		}
		return c.out(cx, fmt.Sprintf("concern %d resolved: %s", cx.ID, rest[2]))
	case "list":
		var goal string
		if len(rest) > 1 {
			goal = rest[1]
		}
		cs := []core.Concern{}
		var err error
		if goal == "" {
			all, err := c.Ledger.Concerns(nil)
			if err != nil {
				return err
			}
			for _, c := range all {
				if c.Resolved == 0 {
					cs = append(cs, c)
				}
			}
		} else {
			cs, err = c.Ledger.OpenConcerns(goal)
			if err != nil {
				return err
			}
		}
		text := "no open concerns"
		if len(cs) > 0 {
			var lines []string
			for _, c := range cs {
				lines = append(lines, fmt.Sprintf("%d\t%s\t%s", c.ID, c.Work, c.Text))
			}
			text = strings.Join(lines, "\n")
		}
		return c.out(cs, text)
	}
	return fail("usage: wd concern <add|resolve|list>")
}

func (c *Cli) scan(rest []string) error {
	a := c.Args
	adopt := str(a, "adopt")
	cards, cardsDir, err := loadTasteCards()
	if err != nil {
		return err
	}
	if adopt != nil && cardsDir == "" {
		return fail("adopting a card writes it into a work-director checkout, and none was found; set WD_ROOT to one")
	}
	feedback, err := c.Ledger.Feedback()
	if err != nil {
		return err
	}
	distilled, err := c.Ledger.Distill()
	if err != nil {
		return err
	}
	candidates := promotion.PromotionCandidates(cards, feedback)
	if adopt != nil {
		found := -1
		for i, c := range candidates {
			if c.Card.ID == *adopt {
				found = i
				break
			}
		}
		if found < 0 {
			return fail("no promotion candidate %s; run wd scan to see candidates", *adopt)
		}
		cand := candidates[found]
		evidence := make([]string, 0, len(cand.Evidence))
		for _, f := range cand.Evidence {
			evidence = append(evidence, strconv.Itoa(f.ID))
		}
		path, err := promotion.AdoptCard(cand.Card, evidence, cardsDir)
		if err != nil {
			return err
		}
		return c.out(map[string]any{"path": path}, fmt.Sprintf("wrote global candidate card %s from %s → %s\nreview it, then `go run ./cmd/taste` when adopted", strings.TrimSuffix(filepath.Base(path), ".md"), cand.Card.ID, path))
	}
	var b strings.Builder
	if len(distilled) > 0 {
		var parts []string
		for _, x := range distilled {
			parts = append(parts, fmt.Sprintf("%s ×%d\n  %s", x.Key, x.Count, strings.Join(x.Texts, "\n  ")))
		}
		b.WriteString(strings.Join(parts, "\n"))
	} else {
		b.WriteString("no distill candidates (need ≥2 feedback on one card or project)")
	}
	b.WriteString("\n--- promotion candidates ---\n")
	if len(candidates) > 0 {
		var parts []string
		for _, c := range candidates {
			var texts []string
			for _, f := range c.Evidence {
				texts = append(texts, f.Text)
			}
			parts = append(parts, fmt.Sprintf("%s (%s) ×%d · %d project(s) · %d attached\n  %s",
				c.Card.ID, c.Card.Category, len(c.Evidence), c.Projects, c.Attached, strings.Join(sliceFirstN(texts, 3), "\n  ")))
		}
		b.WriteString(strings.Join(parts, "\n"))
	} else {
		b.WriteString("none")
	}
	return c.out(map[string]any{"distill": distilled, "promotion": candidates}, b.String())
}

func sliceFirstN(items []string, n int) []string {
	if len(items) > n {
		return items[:n]
	}
	return items
}

func (c *Cli) events(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd events <id>")
	}
	id := rest[0]
	if _, err := c.Ledger.Get(id); err != nil {
		return err
	}
	ev, err := c.Ledger.Events(id, nil)
	if err != nil {
		return err
	}
	var lines []string
	for _, e := range ev {
		head := e.Body
		if i := strings.IndexByte(e.Body, '\n'); i >= 0 {
			head = e.Body[:i]
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s", e.At, e.Kind, head))
	}
	return c.out(ev, strings.Join(lines, "\n"))
}

func (c *Cli) feedback(rest []string) error {
	a := c.Args
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	if sub == "add" {
		if len(rest) < 2 {
			return fail("usage: wd feedback add <text> [--project p] [--card c] [--work id] [--source director|note|attached]")
		}
		text := rest[1]
		var source core.FeedbackSource
		if s := str(a, "source"); s != nil {
			parsed, err := oneOf(core.FeedbackSources, *s, "source")
			if err != nil {
				return err
			}
			source = parsed
		}
		work := str(a, "work")
		if work != nil {
			// Evidence that names a work names a real one: a note pointed at
			// work that is not there is evidence nothing can be audited against,
			// and it would be found out only when a card was promoted on it.
			if _, err := c.Ledger.Get(*work); err != nil {
				return fail("no work %s to file this evidence against", *work)
			}
		}
		f, err := c.Ledger.AddFeedback(text, ledger.FeedbackOptions{
			Project: str(a, "project"),
			Card:    str(a, "card"),
			Source:  source,
			Work:    work,
		})
		if err != nil {
			return err
		}
		return c.out(f, strconv.Itoa(f.ID))
	}
	if sub != "" && sub != "list" {
		return fail("usage: wd feedback (list | add <text> [--project p] [--card c] [--work id] [--source director|note|attached])")
	}
	all, err := c.Ledger.Feedback()
	if err != nil {
		return err
	}
	text := "no feedback"
	if len(all) > 0 {
		var lines []string
		for _, f := range all {
			what := "-"
			if f.Card != nil {
				what = *f.Card
			} else if f.Project != nil {
				what = *f.Project
			}
			lines = append(lines, fmt.Sprintf("%d\t%s\t%s\t%s", f.ID, f.Source, what, f.Text))
		}
		text = strings.Join(lines, "\n")
	}
	return c.out(all, text)
}

func (c *Cli) distill(rest []string) error {
	candidates, err := c.Ledger.Distill()
	if err != nil {
		return err
	}
	text := "no candidates (need ≥2 feedback on one card or project)"
	if len(candidates) > 0 {
		var parts []string
		for _, x := range candidates {
			parts = append(parts, fmt.Sprintf("%s ×%d\n  %s", x.Key, x.Count, strings.Join(x.Texts, "\n  ")))
		}
		text = strings.Join(parts, "\n")
	}
	return c.out(candidates, text)
}

func (c *Cli) serve(rest []string) error {
	port, err := positiveInt(c.Args, "port", serve.DefaultPort)
	if err != nil {
		return err
	}
	// Board actions run this same binary, so they can never drift from it.
	cliPath, err := os.Executable()
	if err != nil {
		return err
	}
	srv := serve.New(c.Ledger, cliPath)
	addr, failed, err := srv.Start(port)
	if err != nil {
		return err
	}
	url := "http://" + addr
	if err := c.out(map[string]any{"url": url}, "serve: "+url); err != nil {
		return err
	}
	return <-failed
}
