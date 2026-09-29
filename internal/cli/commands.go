package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	"wd/internal/taste"
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
		csv := func(k string) []string {
			if v := str(a, k); v != nil {
				var out []string
				for _, s := range strings.Split(*v, ",") {
					if t := strings.TrimSpace(s); t != "" {
						out = append(out, t)
					}
				}
				return out
			}
			return nil
		}
		opts := project.NewProjectOptions{
			Runner:    strOr(a, "runner", ""),
			Mode:      strOr(a, "mode", ""),
			Model:     strOr(a, "model", ""),
			Stack:     csv("stack"),
			Workflows: csv("workflow"),
			Verify:    csv("verify"),
		}
		given := projectValues(path, opts)
		// The file is one `key: value` line per field.
		for _, g := range given {
			if strings.ContainsAny(g[1], "\r\n") {
				return fail("%s %q cannot be held in a project file: it breaks the line", g[0], g[1])
			}
		}
		text := project.ProjectTemplate(name, path, opts)
		file := filepath.Join(c.ProjectsDir, name+".md")
		p, err := project.ParseProject(text, file)
		if err != nil {
			return err
		}
		back := projectValues(p.Path, project.NewProjectOptions{
			Runner: p.Runner, Mode: string(p.Mode), Model: strOrEmpty(p.Model),
			Stack: p.Stack, Workflows: p.Workflows, Verify: p.Verify,
		})
		for i, g := range given {
			if g[1] != "" && (i >= len(back) || back[i] != g) {
				return fail("%s %q cannot be held in a project file: it reads back changed", g[0], g[1])
			}
		}
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
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
	if sub != "" && sub != "list" {
		return fail("usage: wd projects (list | add <name> <path>)")
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
		return fail("usage: wd add <project> <title> [--kind task|evolution|workflow|goal|epic] [--epic <id>] [--heading <label>] [--detail text]")
	}
	name, title := rest[0], rest[1]
	if _, err := c.project(name); err != nil {
		return err
	}
	kind, err := oneOf(core.WorkKinds, strOr(c.Args, "kind", "task"), "kind")
	if err != nil {
		return err
	}
	parent := str(c.Args, "epic")
	heading := str(c.Args, "heading")
	if parent != nil && core.IsEpic(kind) {
		return fail("an epic cannot sit under another work item")
	}
	w, err := c.Ledger.Add(name, title, ledger.AddOptions{
		Kind:    kind,
		Detail:  strOr(c.Args, "detail", ""),
		Parent:  parent,
		Heading: heading,
	})
	if err != nil {
		return err
	}
	return c.out(w, w.ID)
}

func (c *Cli) tasks(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd tasks <epic> [--all]")
	}
	id := rest[0]
	epic, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if !core.IsEpic(epic.Kind) {
		return fail("%s is not an epic", id)
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
		epic, err := c.Ledger.Get(*w.Parent)
		if err != nil {
			return err
		}
		if flag(a, "worktree") {
			cwd, err = c.ensurePrivateWorktree(w, epic, p)
		} else {
			cwd, err = c.ensureSharedWorktree(epic, p)
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

func (c *Cli) epicLike(cmd string, rest []string) error {
	a := c.Args
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	if cmd == "goal" && sub == "add" {
		if len(rest) < 3 {
			return fail("usage: wd goal add <project> <title> [--detail text]")
		}
		name, title := rest[1], rest[2]
		if _, err := c.project(name); err != nil {
			return err
		}
		w, err := c.Ledger.Add(name, title, ledger.AddOptions{Kind: core.WorkGoal, Detail: strOr(a, "detail", "")})
		if err != nil {
			return err
		}
		return c.out(w, fmt.Sprintf("goal %s queued — decompose it: wd goal plan %s", w.ID, w.ID))
	}
	var epic *core.Work
	if len(rest) > 1 {
		w, err := c.Ledger.Get(rest[1])
		if err != nil {
			return err
		}
		if !core.IsEpic(w.Kind) {
			return fail("%s is not a goal or epic", rest[1])
		}
		epic = &w
	}
	kindWord := "epic"
	if cmd == "goal" {
		kindWord = "goal"
	}
	usage := fmt.Sprintf("wd %s (plan <id> | spawn <id> | run <id> [--only|--heading] [--wait] | review <id> | status <id>)", cmd)
	switch sub {
	case "plan":
		if epic == nil {
			return fail("usage: wd %s plan <id> [--runner <runner>] [--model <id>]", cmd)
		}
		return c.epicPlan(cmd, *epic)
	case "spawn":
		if epic == nil {
			return fail("usage: wd %s spawn <id> [--count n] [--model id] [--runner claude|opencode|codex|claude,opencode,…]", cmd)
		}
		return c.epicSpawn(cmd, *epic)
	case "run":
		if epic == nil {
			return fail("usage: wd %s run <id> [--only id,id] [--heading label] [--runner list] [--wait] [--timeout s]", cmd)
		}
		return c.epicRun(cmd, kindWord, *epic)
	case "review":
		if epic == nil {
			return fail("usage: wd %s review <id>", cmd)
		}
		return c.epicReview(*epic)
	case "status":
		if epic == nil {
			return fail("usage: wd %s status <id>", cmd)
		}
		return c.epicStatus(kindWord, *epic)
	}
	return fail("%s", usage)
}

func (c *Cli) epicPlan(cmd string, epic core.Work) error {
	a := c.Args
	p, err := c.project(epic.Project)
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
		Name:  slice60(fmt.Sprintf("wd-%s plan", epic.ID)),
		Brief: coordinator.EpicPlanBrief(epic, p),
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
		w, err := c.Ledger.Add(epic.Project, t.Title, ledger.AddOptions{
			Parent:  &epic.ID,
			Heading: &t.Heading,
		})
		if err != nil {
			return err
		}
		rows = append(rows, w)
	}
	if _, err := c.Ledger.AddEvent(epic.ID, core.EventNote, fmt.Sprintf("plan: %d tasks", len(rows))); err != nil {
		return err
	}
	if epic.State == core.StateQueued {
		if _, err := c.Ledger.Transition(epic.ID, core.StateBriefed); err != nil {
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

func (c *Cli) epicSpawn(cmd string, epic core.Work) error {
	a := c.Args
	p, err := c.project(epic.Project)
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
	cwd, err := c.ensureSharedWorktree(epic, p)
	if err != nil {
		return err
	}
	briefText, err := c.briefFor(epic.ID)
	if err != nil {
		return err
	}
	if epic.State == core.StateQueued {
		if _, err := c.Ledger.Transition(epic.ID, core.StateBriefed); err != nil {
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
			Name:  slice60(fmt.Sprintf("wd-%s %s", epic.ID, name)),
			Brief: briefText,
			Agent: agent,
			Model: model,
		})
		if err != nil {
			return err
		}
		last = &h
		if _, err := c.Ledger.AddEvent(epic.ID, core.EventSpawn, fmt.Sprintf("%s:%s", runnerName, h.Session)); err != nil {
			return err
		}
		sessions = append(sessions, h.Session)
		hint := rn.AttachHint(h)
		handles = append(handles, handleWithAttach{Handle: h, Attach: hint})
		lines = append(lines, fmt.Sprintf("%s  %s", h.Session, hint))
	}
	if last != nil {
		if err := c.Ledger.SetSession(epic.ID, ledger.SessionInfo{
			Runner:  last.Runner,
			Session: last.Session,
			Ref:     last.Ref,
			Cwd:     cwd,
		}); err != nil {
			return err
		}
	}
	if epic.State != core.StateRunning {
		if _, err := c.Ledger.Transition(epic.ID, core.StateRunning); err != nil {
			return err
		}
	}
	return c.out(map[string]any{"sessions": sessions, "handles": handles}, strings.Join(lines, "\n"))
}

func (c *Cli) epicRun(cmd, kindWord string, epic core.Work) error {
	a := c.Args
	p, err := c.project(epic.Project)
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
	cwd, err := c.ensureSharedWorktree(epic, p)
	if err != nil {
		return err
	}
	heading := str(a, "heading")
	all, err := c.Ledger.Tasks(epic.ID)
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
		if _, err := c.Ledger.AddEvent(epic.ID, core.EventSpawn, fmt.Sprintf("%s:%s", runnerName, h.Session)); err != nil {
			return err
		}
		hint := rn.AttachHint(h)
		spawned = append(spawned, handleWithAttach{Handle: h, Attach: hint})
		lines = append(lines, fmt.Sprintf("%s  %s", h.Session, hint))
	}
	if len(spawned) > 0 && epic.State != core.StateRunning {
		if _, err := c.Ledger.Transition(epic.ID, core.StateRunning); err != nil {
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
		res, err := coordinator.CoordinateOnce(epic, p, c.Ledger, runner.RunnerNamed)
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
		tasks, err := c.Ledger.Tasks(epic.ID)
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
	tasks, err := c.Ledger.Tasks(epic.ID)
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

func (c *Cli) epicReview(epic core.Work) error {
	p, err := c.project(epic.Project)
	if err != nil {
		return err
	}
	res, err := coordinator.CoordinateOnce(epic, p, c.Ledger, runner.RunnerNamed)
	if err != nil {
		return err
	}
	return c.out(res, fmt.Sprintf("answered: %s\nescalated: %s\nreviewed: %s\nblocked: %s\nwaiting: %s",
		joinOrNone(res.Answered), joinOrNone(res.Escalated), joinOrNone(res.Reviewed), joinOrNone(res.Blocked), joinOrNone(res.Waiting)))
}

func joinOrNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

func (c *Cli) epicStatus(kindWord string, epic core.Work) error {
	all, err := c.Ledger.Tasks(epic.ID)
	if err != nil {
		return err
	}
	tasks := []core.Work{}
	for _, t := range all {
		if t.State != core.StateDone && t.State != core.StateDropped {
			tasks = append(tasks, t)
		}
	}
	return c.out(map[string]any{"epic": epic, "open": len(tasks), "tasks": tasks},
		fmt.Sprintf("%s (%s) · %d open\n%s", epic.Title, epic.State, len(tasks), brief.RenderTaskBlock(tasks)))
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
		return fail("usage: wd report <id> [--tail n]")
	}
	n, err := positiveInt(a, "tail", 1)
	if err != nil {
		return err
	}
	id := rest[0]
	h, err := c.handle(id)
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
	runnerStatus, err := r.Status(h)
	if err != nil {
		return err
	}
	messages := sliceLastN(texts, n)
	if messages == nil {
		messages = []string{}
	}
	report := status
	return c.out(map[string]any{"status": runnerStatus, "report": report, "messages": messages},
		fmt.Sprintf("%s%s\n%s", runnerStatus, orStatus(status), strings.Join(messages, "\n---\n")))
}

var statusRe = regexp.MustCompile(`STATUS:\s*(DONE|BLOCKED|NEEDS-INPUT)`)

func matchStatus(last string) string {
	m := statusRe.FindStringSubmatch(last)
	if m == nil {
		return ""
	}
	return m[1]
}

func orStatus(status string) string {
	if status == "" {
		return ""
	}
	return " · " + status
}

func (c *Cli) verify(rest []string) error {
	if len(rest) == 0 {
		return fail("usage: wd verify <id>")
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
	cwd := w.Cwd
	if cwd == nil && core.IsEpic(w.Kind) {
		wts, err := c.Ledger.Worktrees(id)
		if err != nil {
			return err
		}
		for _, wt := range wts {
			if wt.Kind == core.WorktreeShared {
				cwd = &wt.Path
			}
		}
	}
	if cwd == nil {
		cwd = &p.Path
	}
	if len(p.Verify) == 0 {
		return fail("project %s lists no verify commands; add them under verify: in its project file", p.Name)
	}
	results := []string{}
	pass := true
	for _, cmdline := range p.Verify {
		r, err := runner.Run([]string{"bash", "-lc", cmdline}, *cwd)
		if err != nil {
			return err
		}
		if r.Code != 0 {
			pass = false
		}
		results = append(results, fmt.Sprintf("%s → %d\n%s", cmdline, r.Code, lastLines(strings.TrimSpace(r.Stdout+r.Stderr), 5)))
	}
	body := "fail\n"
	if pass {
		body = "pass\n"
	}
	body += strings.Join(results, "\n")
	if _, err := c.Ledger.AddEvent(id, core.EventVerify, body); err != nil {
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

func (c *Cli) decide(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd decide <id> <text>")
	}
	id, text := rest[0], strings.Join(rest[1:], " ")
	e, err := c.Ledger.AddEvent(id, core.EventDecision, text)
	if err != nil {
		return err
	}
	return c.out(e, "decided: "+e.Body)
}

func (c *Cli) pr(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd pr <id> <url>")
	}
	id, url := rest[0], rest[1]
	if _, err := c.Ledger.AddEvent(id, core.EventPr, url); err != nil {
		return err
	}
	return c.out(map[string]any{"ok": true}, "recorded")
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
	} else if !core.IsEpic(w.Kind) {
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
		return fail("usage: wd done <id>")
	}
	id := rest[0]
	after, err := c.Ledger.Transition(id, core.StateDone)
	if err != nil {
		if _, ok := err.(core.IllegalTransition); ok {
			return fail("%s", err.Error())
		}
		return err
	}
	return c.out(after, "done")
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
		return fail("usage: wd conflict <epic>")
	}
	id := rest[0]
	epic, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	if !core.IsEpic(epic.Kind) {
		return fail("%s is not an epic", id)
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
		})
		if err != nil {
			return err
		}
		return c.out(wt, fmt.Sprintf("attached private worktree %d → %s", wt.ID, wt.Path))
	}
	if len(rest) < 2 {
		return fail("usage: wd worktree (attach <id> <path> | list <id>)")
	}
	id := rest[1]
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
	return fail("usage: wd worktree <attach|list>")
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
		return fail("merge is only for tasks under an epic")
	}
	epic, err := c.Ledger.Get(*w.Parent)
	if err != nil {
		return err
	}
	if _, err := c.project(w.Project); err != nil {
		return err
	}
	shared, err := c.Ledger.Worktrees(epic.ID)
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
		return fail("epic %s has no active shared worktree", epic.ID)
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
	cs, err := c.Ledger.Conflicts(epic.ID)
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
		var epic string
		if len(rest) > 1 {
			epic = rest[1]
		}
		cs := []core.Concern{}
		var err error
		if epic == "" {
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
			cs, err = c.Ledger.OpenConcerns(epic)
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
	cardsDir, err := tasteCardsDir()
	if err != nil {
		return err
	}
	cards, err := taste.LoadCards(cardsDir)
	if err != nil {
		return err
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
			return fail("usage: wd feedback add <text> [--project p] [--card c] [--source director|note|attached]")
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
		f, err := c.Ledger.AddFeedback(text, ledger.FeedbackOptions{
			Project: str(a, "project"),
			Card:    str(a, "card"),
			Source:  source,
		})
		if err != nil {
			return err
		}
		return c.out(f, strconv.Itoa(f.ID))
	}
	if sub != "" && sub != "list" {
		return fail("usage: wd feedback (list | add <text> [--project p] [--card c] [--source director|note|attached])")
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
