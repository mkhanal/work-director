// Package cli is the director's command line: every wd command over the core
// and the ledger, printing either human text or the --json rail.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	tastecards "wd/taste/cards"

	"wd/internal/brief"
	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
	"wd/internal/taste"
)

// valueFlags are the CLI's --key value flags, given as --key value or
// --key=value; every other --key is a switch and takes no value.
var valueFlags = map[string]bool{
	"runner": true, "model": true, "mode": true, "agent": true, "count": true,
	"tail": true, "stack": true, "workflow": true, "verify": true, "lazyspec": true,
	"kind": true, "goal": true, "epic": true, "roadmap": true, "type": true, "reason": true,
	"question": true, "answer": true, "tokens": true, "effective": true,
	"since": true, "since-feedback": true, "work": true,
	"heading": true, "detail": true, "project": true,
	"branch": true, "adopt": true, "source": true, "card": true, "only": true,
	// name labels a workspace. It is a value, not a switch: a workspace is
	// something a person names, and the label is what they would type back.
	"name":    true,
	"timeout": true, "port": true, "ref": true, "cwd": true, "cancelled": true,
	"turns": true, "judgements": true, "stalled": true, "poll-seconds": true,
	"deadline": true,
}

// Args is one parsed command line: positionals, value flags (never empty)
// and switches.
type Args struct {
	Positional []string
	Values     map[string]string
	Switches   map[string]bool
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

// parse splits argv into positionals, value flags and switches. A value flag
// with no value, or an empty one, and a switch given a value both fail.
func parse(argv []string) (Args, error) {
	a := Args{Values: map[string]string{}, Switches: map[string]bool{}}
	after := false
	toks := normalize(argv)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if after {
			a.Positional = append(a.Positional, t)
			continue
		}
		if t == "--" {
			after = true
			continue
		}
		if !strings.HasPrefix(t, "--") {
			a.Positional = append(a.Positional, t)
			continue
		}
		key, val, hasVal := strings.Cut(t[2:], "=")
		if !valueFlags[key] {
			if hasVal {
				return Args{}, fail("--%s takes no value", key)
			}
			a.Switches[key] = true
			continue
		}
		if !hasVal && i+1 < len(toks) && !strings.HasPrefix(toks[i+1], "--") {
			i++
			val = toks[i]
		}
		if val == "" {
			return Args{}, fail("--%s needs a value", key)
		}
		a.Values[key] = val
	}
	return a, nil
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
// Version is the release this binary was built from, set by the release
// build with -ldflags. A binary that cannot say what it is is a binary nobody
// can file a bug against, so an unstamped build reads as dev rather than
// pretending to be a release.
var Version = "dev"

// Commit is the git revision the release was cut from, stamped the same way.
var Commit = ""

func Run(args []string) error {
	parsed, err := parse(args)
	if err != nil {
		return err
	}
	// version answers before anything is opened: a person asking what they
	// installed does not need a ledger, a projects directory or a worktrees
	// directory created on their disk to be told what they have.
	if parsed.Switches["version"] || (len(parsed.Positional) == 1 && parsed.Positional[0] == "version") {
		out := fmt.Sprintf("wd %s", Version)
		if Commit != "" {
			out += " (" + Commit + ")"
		}
		fmt.Fprintln(os.Stdout, out)
		return nil
	}
	if parsed.Switches["help"] || (len(parsed.Positional) == 1 && parsed.Positional[0] == "help") {
		fmt.Fprintln(os.Stdout, usage)
		return nil
	}
	wdHome, err := runner.WDHome()
	if err != nil {
		return err
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
		Home:        wdHome,
		ProjectsDir: projectsDir,
		Worktrees:   worktreesDir,
		Ledger:      l,
		Projects:    projects,
		Args:        parsed,
		JSON:        flag(parsed, "json"),
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}
	return c.dispatch()
}

// loadTasteCards reads the rule cards of a work-director checkout when there
// is one, so an edited card reaches the next brief without a rebuild, and
// the cards this binary was built with otherwise. The checkout is
// $WD_ROOT/taste/cards, else taste/cards beside the executable or in the
// working directory; checkout is its cards directory, empty for the
// built-in cards.
func loadTasteCards() (cards []taste.Card, checkout string, err error) {
	checkout, err = tasteCheckout()
	if err != nil {
		return nil, "", err
	}
	if checkout == "" {
		cards, err = taste.LoadCards(tastecards.Snapshot, "built-in taste/cards")
		return cards, "", err
	}
	cards, err = taste.LoadCards(os.DirFS(checkout), checkout)
	return cards, checkout, err
}

func tasteCheckout() (string, error) {
	if r := os.Getenv("WD_ROOT"); r != "" {
		return filepath.Join(r, "taste", "cards"), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, base := range []string{filepath.Dir(exe), wd} {
		dir := filepath.Join(base, "taste", "cards")
		st, err := os.Stat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if st.IsDir() {
			return dir, nil
		}
	}
	return "", nil
}

const usage = "wd <projects|workspace|add|tasks|brief|spawn|models|runner|roadmap|goal|drive|send|attach|report|verify|decide|pr|soft-done|set|done|status|context|open|claim|impact|conflict|worktree|merge|concern|scan|events|review|abandon|release|reopen|feedback|distill|tui|serve|doctor> [--json]\n\nwd --version   what release this binary is\nwd serve       the web UI and the JSON API; open the address it prints\nwd doctor     which runner CLIs are detected, and whether this is a repo"

// commands maps each wd command to its handler, given the arguments after it.
var commands = map[string]func(c *Cli, rest []string) error{
	"projects": (*Cli).projects,
	"models":   (*Cli).models,
	"runner":   (*Cli).runner,
	"add":      (*Cli).add,
	"tasks":    (*Cli).tasks,
	"brief":    (*Cli).brief,
	"spawn":    (*Cli).spawn,
	"goal":     func(c *Cli, rest []string) error { return c.goalLike("goal", rest) },
	// epic was goal's old name. It reaches the same code so anything written
	// before the rename keeps working.
	"epic":      func(c *Cli, rest []string) error { return c.goalLike("epic", rest) },
	"roadmap":   (*Cli).roadmap,
	"workspace": (*Cli).workspace,
	"drive":     (*Cli).drive,
	"abandon":   (*Cli).abandon,
	"release":   (*Cli).release,
	"reopen":    (*Cli).reopen,
	"review":    (*Cli).review,
	"send":      (*Cli).send,
	"attach":    (*Cli).attach,
	"report":    (*Cli).report,
	"verify":    (*Cli).verify,
	"decide":    (*Cli).decide,
	"pr":        (*Cli).pr,
	"soft-done": (*Cli).softDone,
	"set":       (*Cli).set,
	"done":      (*Cli).done,
	"status":    (*Cli).status,
	"context":   (*Cli).contextCmd,
	"open":      (*Cli).open,
	"claim":     (*Cli).claim,
	"impact":    (*Cli).impact,
	"conflict":  (*Cli).conflict,
	"worktree":  (*Cli).worktree,
	"merge":     (*Cli).merge,
	"concern":   (*Cli).concern,
	"scan":      (*Cli).scan,
	"events":    (*Cli).events,
	"feedback":  (*Cli).feedback,
	"distill":   (*Cli).distill,
	"tui":       (*Cli).tui,
	"serve":     (*Cli).serve,
	"doctor":    (*Cli).doctor,
}

func (c *Cli) dispatch() error {
	if len(c.Args.Positional) == 0 {
		return fail("%s", usage)
	}
	run, ok := commands[c.Args.Positional[0]]
	if !ok {
		return fail("%s", usage)
	}
	return run(c, c.Args.Positional[1:])
}

// out prints the command's result: indented JSON on the --json rail, else text.
func (c *Cli) out(v any, text string) error {
	if c.JSON {
		enc := json.NewEncoder(c.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	_, err := fmt.Fprintln(c.Stdout, text)
	return err
}

// str is the --k value flag, nil when absent.
func str(a Args, k string) *string {
	if v, ok := a.Values[k]; ok {
		return &v
	}
	return nil
}

func strOr(a Args, k, def string) string {
	if s := str(a, k); s != nil {
		return *s
	}
	return def
}

// positiveInt is the --k flag as a positive integer, def when absent.
func positiveInt(a Args, k string, def int) (int, error) {
	s := str(a, k)
	if s == nil {
		return def, nil
	}
	n, err := strconv.Atoi(*s)
	if err != nil || n < 1 {
		return 0, fail("--%s must be a positive integer", k)
	}
	return n, nil
}

// intOr reads a flag that may be absent and is a number when present. Zero is
// the answer for absent, because every int flag here counts something where
// zero means none of it.
func intOr(a Args, k string, def int) int {
	s := str(a, k)
	if s == nil {
		return def
	}
	n, err := strconv.Atoi(*s)
	if err != nil {
		return def
	}
	return n
}

func flag(a Args, k string) bool { return a.Switches[k] }

// oneOf parses x as one of values, failing with the list.
func oneOf[T ~string](values []T, x, what string) (T, error) {
	names := make([]string, len(values))
	for i, v := range values {
		if string(v) == x {
			return v, nil
		}
		names[i] = string(v)
	}
	return "", fail("unknown %s %s; one of %s", what, x, strings.Join(names, ", "))
}

// settableStates are the states `wd set` accepts, in the order it lists them.
var settableStates = []core.State{
	core.StateQueued, core.StateBriefed, core.StateRunning, core.StateNeedsInput, core.StateReview,
	core.StateSoftDone, core.StateDone, core.StateBlocked, core.StateDropped,
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
	h, ok, err := coordinator.Handle(c.Ledger, w, p)
	if err != nil {
		return runner.Handle{}, err
	}
	if !ok {
		return runner.Handle{}, fail("work %s has no session or claim", id)
	}
	return h, nil
}

// sendTo continues work's recorded session with text and moves the work to
// running. Work that cannot return to running is refused before anything is
// sent.
func (c *Cli) sendTo(id, text string) error {
	w, err := c.Ledger.Get(id)
	if err != nil {
		return err
	}
	// Finished work is refused here rather than downstream: sending to it
	// records the message before the state check would run, so a goal someone is
	// asking a question of would end up carrying a sent event it never accepted.
	// The check is on being finished, not on the state machine, because the
	// machine now has a done → running edge that only Reopen may take and a
	// message is not a reason.
	if core.Reopenable(w.State) {
		return fail("%s is done; a message is not a reason to work on it again — wd reopen %s \"<what is being worked on>\", or wd context %s to ask it something", w.ID, w.ID, w.ID)
	}
	if w.State != core.StateRunning && !slices.Contains(core.Transitions[w.State], core.StateRunning) {
		return fail("%s", core.IllegalTransition{From: w.State, To: core.StateRunning}.Error())
	}
	h, err := c.handle(id)
	if err != nil {
		return err
	}
	r, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return err
	}
	if err := coordinator.Send(c.Ledger, id, r, h, text); err != nil {
		return err
	}
	if _, err := c.Ledger.AddEvent(id, core.EventSent, text); err != nil {
		return err
	}
	if w.State != core.StateRunning {
		if _, err := c.Ledger.Transition(id, core.StateRunning); err != nil {
			return err
		}
	}
	return nil
}

// cycle gathers the brief context for work: decisions, history, roadmap.
func (c *Cli) cycle(id string) (brief.Context, error) {
	decided, err := c.Ledger.Events(id, kindPtr(core.EventDecision))
	if err != nil {
		return brief.Context{}, err
	}
	concerns, err := c.Ledger.Concerns(&id)
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
	for _, e := range decided {
		decisions = append(decisions, e.Body)
	}
	for _, cx := range concerns {
		if cx.Decision != nil {
			decisions = append(decisions, *cx.Decision)
		}
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
	cards, _, err := loadTasteCards()
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
	if core.IsGoal(w.Kind) {
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
		return brief.ComposeGoal(w, brief.RenderTaskBlock(openTasks), p, cards, ctx)
	}
	if w.Parent != nil {
		goal, err := c.Ledger.Get(*w.Parent)
		if err != nil {
			return "", err
		}
		claims, err := c.Ledger.Tasks(goal.ID)
		if err != nil {
			return "", err
		}
		var claimList []core.Work
		for _, t := range claims {
			if t.ID != id && t.Claim != nil && t.State != core.StateDone && t.State != core.StateDropped {
				claimList = append(claimList, t)
			}
		}
		return brief.ComposeSlice(w, goal, claimList, p, cards)
	}
	return brief.Compose(w, p, cards, ctx)
}

func (c *Cli) runGit(args []string, cwd string) (runner.RunResult, error) {
	return runner.Run(append([]string{"git"}, args...), cwd)
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

// ask prompts on stderr, so stdout stays the command's one result, and reads
// one line of stdin; a last line without a newline is still the answer.
func (c *Cli) ask(q string) (string, error) {
	if _, err := fmt.Fprint(c.Stderr, q); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return strings.TrimRight(line, "\n"), nil
}
