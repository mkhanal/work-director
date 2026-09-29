// Package cli is the director's command line: every wd command over the core
// and the ledger, printing either human text or the --json rail. It is a port
// of packages/wd/src/cli.ts with the same commands, flags, output and error
// messages.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wd/internal/brief"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
	"wd/internal/taste"
)

// stringFlags are the CLI's --key value flags. A bare --key consumes the next
// token as its value, matching Node's parseArgs with strict: false.
var stringFlags = map[string]bool{
	"runner": true, "model": true, "mode": true, "agent": true, "count": true,
	"tail": true, "stack": true, "workflow": true, "verify": true, "lazyspec": true,
	"kind": true, "epic": true, "heading": true, "detail": true, "project": true,
	"branch": true, "adopt": true, "source": true, "card": true, "only": true,
	"timeout": true, "port": true,
}

// Args is one parsed command line: positionals and flags.
type Args struct {
	Positional []string
	Flags      map[string]any
}

// normalize moves single-dash tokens past --, where parseArgs keeps them
// positional (wd has no short options; `wd impact <id> -src/…` passes a path).
func normalize(argv []string) []string {
	var rest, singles []string
	for _, t := range argv {
		if t != "--" && strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") {
			singles = append(singles, t)
		} else {
			rest = append(rest, t)
		}
	}
	if len(singles) == 0 {
		return rest
	}
	return append(rest, append([]string{"--"}, singles...)...)
}

func parse(argv []string) Args {
	var positional []string
	flags := map[string]any{}
	after := false
	toks := normalize(argv)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if after {
			positional = append(positional, t)
			continue
		}
		if t == "--" {
			after = true
			continue
		}
		if strings.HasPrefix(t, "--") {
			key, val, hasVal := strings.Cut(t[2:], "=")
			if hasVal {
				flags[key] = val
			} else if stringFlags[key] && i+1 < len(toks) {
				i++
				flags[key] = toks[i]
			} else {
				flags[key] = true
			}
			continue
		}
		positional = append(positional, t)
	}
	return Args{Positional: positional, Flags: flags}
}

// failError is a user-facing failure: the message is the whole output.
type failError struct{ msg string }

func (e *failError) Error() string { return e.msg }

func fail(format string, args ...any) error {
	return &failError{msg: fmt.Sprintf(format, args...)}
}

// Cli holds one invocation's state: the parsed args, the ledger, the projects
// and the directories the CLI owns.
type Cli struct {
	Root        string
	Home        string
	ProjectsDir string
	Worktrees   string
	Ledger      *ledger.Ledger
	Projects    map[string]*project.Project
	Args        Args
	JSON        bool
	Stdout      io.Writer
	Stderr      io.Writer
}

// Run parses args, opens the ledger and projects, and dispatches the command.
func Run(args []string) error {
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	wdHome := os.Getenv("WD_HOME")
	if wdHome == "" {
		wdHome = filepath.Join(home, ".work-director")
	}
	if err := os.MkdirAll(wdHome, 0o755); err != nil {
		return err
	}
	l, err := ledger.New(filepath.Join(wdHome, "ledger.db"))
	if err != nil {
		return err
	}
	defer l.Close()
	projectsDir := os.Getenv("WD_PROJECTS")
	if projectsDir == "" {
		projectsDir = filepath.Join(wdHome, "projects")
	}
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		return err
	}
	projects, err := project.LoadProjects(projectsDir)
	if err != nil {
		return err
	}
	worktreesDir := filepath.Join(wdHome, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		return err
	}
	c := &Cli{
		Root:        findRoot(),
		Home:        wdHome,
		ProjectsDir: projectsDir,
		Worktrees:   worktreesDir,
		Ledger:      l,
		Projects:    projects,
		Args:        parse(args),
		JSON:        flag(parse(args), "json"),
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}
	return c.dispatch()
}

// findRoot locates the repo root (where taste/cards lives): $WD_ROOT, else
// the executable's directory, else the working directory.
func findRoot() string {
	if r := os.Getenv("WD_ROOT"); r != "" {
		return r
	}
	for _, base := range []string{executableDir(), mustGetwd()} {
		if base == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(base, "taste", "cards")); err == nil && st.IsDir() {
			return base
		}
	}
	return "."
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func (c *Cli) dispatch() error {
	a := c.Args
	if len(a.Positional) == 0 {
		return fail("wd <projects|add|tasks|brief|spawn|models|runner|epic|goal|send|attach|report|verify|pr|soft-done|set|done|status|claim|impact|conflict|worktree|merge|concern|scan|events|feedback|distill|tui|serve|doctor> [--json]")
	}
	cmd, rest := a.Positional[0], a.Positional[1:]
	switch cmd {
	case "projects":
		return c.projects(rest)
	case "models":
		return c.models(rest)
	case "runner":
		return c.runner(rest)
	case "add":
		return c.add(rest)
	case "tasks":
		return c.tasks(rest)
	case "brief":
		return c.brief(rest)
	case "spawn":
		return c.spawn(rest)
	case "epic", "goal":
		return c.epicLike(cmd, rest)
	case "send":
		return c.send(rest)
	case "attach":
		return c.attach(rest)
	case "report":
		return c.report(rest)
	case "verify":
		return c.verify(rest)
	case "pr":
		return c.pr(rest)
	case "soft-done":
		return c.softDone(rest)
	case "set":
		return c.set(rest)
	case "done":
		return c.done(rest)
	case "status":
		return c.status(rest)
	case "context":
		return c.contextCmd(rest)
	case "open":
		return c.open(rest)
	case "claim":
		return c.claim(rest)
	case "impact":
		return c.impact(rest)
	case "conflict":
		return c.conflict(rest)
	case "worktree":
		return c.worktree(rest)
	case "merge":
		return c.merge(rest)
	case "concern":
		return c.concern(rest)
	case "scan":
		return c.scan(rest)
	case "events":
		return c.events(rest)
	case "feedback":
		return c.feedback(rest)
	case "distill":
		return c.distill(rest)
	case "tui":
		return c.tui(rest)
	case "serve":
		return c.serve(rest)
	case "doctor":
		return c.doctor(rest)
	}
	return fail("wd <projects|add|tasks|brief|spawn|models|runner|epic|goal|send|attach|report|verify|pr|soft-done|set|done|status|claim|impact|conflict|worktree|merge|concern|scan|events|feedback|distill|tui|serve|doctor> [--json]")
}

// out prints the command's result: indented JSON on the --json rail, else text.
func (c *Cli) out(v any, text string) {
	if c.JSON {
		c.jsonOut(v)
		return
	}
	fmt.Fprintln(c.Stdout, text)
}

func (c *Cli) jsonOut(v any) {
	enc := json.NewEncoder(c.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func (c *Cli) jsonLine(v any) {
	enc := json.NewEncoder(c.Stdout)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func str(a Args, k string) *string {
	if v, ok := a.Flags[k]; ok {
		if s, ok := v.(string); ok && s != "" {
			return &s
		}
	}
	return nil
}

func strOr(a Args, k, def string) string {
	if s := str(a, k); s != nil {
		return *s
	}
	return def
}

func flag(a Args, k string) bool {
	_, ok := a.Flags[k]
	return ok
}

func oneOf(values []string, x, what string) (string, error) {
	for _, v := range values {
		if v == x {
			return x, nil
		}
	}
	return "", fail("unknown %s %s; one of %s", what, x, strings.Join(values, ", "))
}

func kindPtr(k core.EventKind) *core.EventKind { return &k }

// project resolves a project by name, failing with the known projects.
func (c *Cli) project(name string) (*project.Project, error) {
	if p, ok := c.Projects[name]; ok {
		return p, nil
	}
	known := c.projectNames()
	joined := strings.Join(known, ", ")
	if joined == "" {
		joined = "none"
	}
	return nil, fail("unknown project %s; known: %s (add %s/<name>.md, see projects/example.md)", name, joined, c.ProjectsDir)
}

// projectNames lists the known project names, sorted.
func (c *Cli) projectNames() []string {
	var out []string
	for name := range c.Projects {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// handle resolves the live session handle for work: its runner and session,
// and the directory it works in.
func (c *Cli) handle(id string) (runner.Handle, error) {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return runner.Handle{}, err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return runner.Handle{}, err
	}
	runnerName := w.Runner
	if runnerName == nil {
		runnerName = &p.Runner
	}
	session := w.Session
	if session == nil {
		session = w.Claim
	}
	if session == nil {
		return runner.Handle{}, fail("work %s has no session or claim", id)
	}
	var shared *string
	if w.Parent != nil {
		wts, err := c.Ledger.Worktrees(*w.Parent)
		if err != nil {
			return runner.Handle{}, err
		}
		for _, wt := range wts {
			if wt.Kind == core.WorktreeShared && wt.State == core.WorktreeActive {
				shared = &wt.Path
			}
		}
	}
	cwd := w.Cwd
	if cwd == nil {
		cwd = shared
	}
	if cwd == nil {
		cwd = &p.Path
	}
	return runner.Handle{Runner: *runnerName, Session: *session, Ref: w.Ref, Cwd: *cwd}, nil
}

// cycle gathers the brief context for work: decisions, history, roadmap.
func (c *Cli) cycle(id string) (brief.Context, error) {
	notes, err := c.Ledger.Events(id, kindPtr(core.EventNote))
	if err != nil {
		return brief.Context{}, err
	}
	reports, err := c.Ledger.Events(id, kindPtr(core.EventReport))
	if err != nil {
		return brief.Context{}, err
	}
	w, err := c.Ledger.Get(id)
	if err != nil {
		return brief.Context{}, err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return brief.Context{}, err
	}
	var decisions []string
	for _, e := range notes {
		decisions = append(decisions, e.Body)
	}
	var history []string
	for _, e := range reports {
		head := e.Body
		if i := strings.IndexByte(e.Body, '\n'); i >= 0 {
			head = e.Body[:i]
		}
		history = append(history, "report: "+head)
	}
	roadmap := p.Roadmap
	return brief.Context{Decisions: decisions, History: history, Roadmap: &roadmap}, nil
}

// briefFor renders the brief for work, whichever shape fits its kind.
func (c *Cli) briefFor(id string) (string, error) {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return "", err
	}
	cards, err := taste.LoadCards(filepath.Join(c.Root, "taste", "cards"))
	if err != nil {
		return "", err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return "", err
	}
	ctx, err := c.cycle(id)
	if err != nil {
		return "", err
	}
	if core.IsEpic(w.Kind) {
		open, err := c.Ledger.Tasks(id)
		if err != nil {
			return "", err
		}
		var openTasks []core.Work
		for _, t := range open {
			if t.State != core.StateDone && t.State != core.StateDropped {
				openTasks = append(openTasks, t)
			}
		}
		return brief.ComposeEpic(w, brief.RenderTaskBlock(openTasks), p, cards, ctx)
	}
	if w.Parent != nil {
		epic, err := c.Ledger.Get(*w.Parent)
		if err != nil {
			return "", err
		}
		claims, err := c.Ledger.Tasks(epic.ID)
		if err != nil {
			return "", err
		}
		var claimList []core.Work
		for _, t := range claims {
			if t.ID != id && t.Claim != nil && t.State != core.StateDone && t.State != core.StateDropped {
				claimList = append(claimList, t)
			}
		}
		return brief.ComposeSlice(w, epic, claimList, p, cards)
	}
	return brief.Compose(w, p, cards, ctx)
}

func (c *Cli) runGit(args []string, cwd string) (runner.RunResult, error) {
	return runner.Run(append([]string{"git"}, args...), cwd)
}

func sharedBranch(epic core.Work) string { return "wd-" + epic.ID }

func branchFor(w core.Work) string { return "wd-" + w.ID }

// ensureSharedWorktree creates or reuses the epic's one shared worktree on
// branch wd-<epic>, registered in the ledger.
func (c *Cli) ensureSharedWorktree(epic core.Work, p *project.Project) (string, error) {
	wts, err := c.Ledger.Worktrees(epic.ID)
	if err != nil {
		return "", err
	}
	for _, wt := range wts {
		if wt.Kind == core.WorktreeShared && wt.State == core.WorktreeActive {
			return wt.Path, nil
		}
	}
	if _, err := c.runGit([]string{"worktree", "prune"}, p.Path); err != nil {
		return "", err
	}
	path := filepath.Join(c.Worktrees, p.Name+"-epic-"+epic.ID)
	r, err := c.runGit([]string{"worktree", "add", "-b", sharedBranch(epic), path}, p.Path)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		r, err = c.runGit([]string{"worktree", "add", path, sharedBranch(epic)}, p.Path)
		if err != nil {
			return "", err
		}
	}
	if r.Code != 0 {
		return "", fail("cannot create shared worktree at %s: %s", path, strings.TrimSpace(r.Stderr))
	}
	wt, err := c.Ledger.AddWorktree(epic.ID, ledger.WorktreeInfo{
		Path:   path,
		Branch: ptr(sharedBranch(epic)),
		Kind:   core.WorktreeShared,
	})
	if err != nil {
		return "", err
	}
	return wt.Path, nil
}

// ensurePrivateWorktree creates a private worktree for a task, branched off
// the epic's shared branch.
func (c *Cli) ensurePrivateWorktree(w core.Work, epic core.Work, p *project.Project) (string, error) {
	wts, err := c.Ledger.Worktrees(w.ID)
	if err != nil {
		return "", err
	}
	for _, wt := range wts {
		if wt.Kind == core.WorktreePrivate && wt.State == core.WorktreeActive {
			return wt.Path, nil
		}
	}
	if _, err := c.runGit([]string{"worktree", "prune"}, p.Path); err != nil {
		return "", err
	}
	path := filepath.Join(c.Worktrees, p.Name+"-"+w.ID)
	r, err := c.runGit([]string{"worktree", "add", "-b", branchFor(w), path, sharedBranch(epic)}, p.Path)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fail("cannot create private worktree at %s: %s", path, strings.TrimSpace(r.Stderr))
	}
	wt, err := c.Ledger.AddWorktree(w.ID, ledger.WorktreeInfo{
		Path:   path,
		Branch: ptr(branchFor(w)),
		Kind:   core.WorktreePrivate,
	})
	if err != nil {
		return "", err
	}
	return wt.Path, nil
}

func ptr(s string) *string { return &s }

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// slice60 truncates a display name to 60 characters (JS slice(0, 60)).
func slice60(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60])
	}
	return s
}

// sliceLastN matches JavaScript's texts.slice(-n).
func sliceLastN(texts []string, n int) []string {
	if n == 0 {
		return texts
	}
	if n < 0 {
		if -n >= len(texts) {
			return []string{}
		}
		return texts[-n:]
	}
	if n >= len(texts) {
		return texts
	}
	return texts[len(texts)-n:]
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (c *Cli) ask(q string) string {
	fmt.Fprint(c.Stdout, q)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimRight(line, "\n")
}
