package coordinator

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
)

// Picture is what a goal's supervisor is shown at each look: the goal, what was
// decided on it, where each task stands and what is still unsettled.
type Picture struct {
	Goal      core.Work
	Decisions []string
	Tasks     []TaskPicture
	Concerns  []core.Concern
	Checks    []string
	Worktree  string
}

// TaskPicture is one task as its supervisor sees it: its row, its latest
// report, and, when its session stopped without reporting, what it last said.
type TaskPicture struct {
	Work    core.Work
	Report  string
	Stopped string
}

// reportClip bounds each report in the brief: the supervisor reads every task
// at every look, and a long report says what it has to in its first lines.
const reportClip = 1500

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// SupervisionBrief is what the goal's supervisor is given at each look.
func SupervisionBrief(p Picture) string {
	var b strings.Builder
	g := p.Goal
	fmt.Fprintf(&b, "You supervise goal %s for the director, as its head of engineering. The person asked:\n\n%s\n", g.ID, g.Title)
	if g.Detail != "" && g.Detail != g.Title {
		fmt.Fprintf(&b, "%s\n", g.Detail)
	}
	if len(p.Decisions) > 0 {
		b.WriteString("\nDecided since, newest last:\n")
		for _, d := range p.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	}
	where := "the project's checkout"
	if p.Worktree != "" {
		where = p.Worktree
	}
	fmt.Fprintf(&b, "\nTasks, each its own agent session working in %s:\n", where)
	for _, t := range p.Tasks {
		fmt.Fprintf(&b, "- %s [%s] %s\n", t.Work.ID, t.Work.State, t.Work.Title)
		if t.Report != "" {
			fmt.Fprintf(&b, "  latest report: %s\n", strings.ReplaceAll(clip(t.Report, reportClip), "\n", "\n  "))
		}
		if t.Stopped != "" {
			fmt.Fprintf(&b, "  stopped without a report; it last said: %s\n", strings.ReplaceAll(clip(t.Stopped, reportClip), "\n", "\n  "))
		}
	}
	if len(p.Concerns) > 0 {
		b.WriteString("\nOpen concerns:\n")
		for _, c := range p.Concerns {
			fmt.Fprintf(&b, "- %d (on %s): %s\n", c.ID, c.Work, c.Text)
		}
	}
	checks := "none: the repository defines no checks"
	if len(p.Checks) > 0 {
		checks = "`" + strings.Join(p.Checks, "`, `") + "`"
	}
	fmt.Fprintf(&b, `
Your job is the goal as the person asked it, not each task on its own. Read the reports, and read the work itself in the worktree (git status, git diff) to check it; do not edit, create or delete files. Each task's agent keeps to its own files and leaves work between tasks to you: a missing wire-up, a gap no task owns, a concern one task raised about another. Settle concerns yourself unless only the person can: a business fact, or something irreversible or seen outside the repository, such as pushing a tag. Prefer telling an existing task over adding one. The director runs the checks itself (%s), and landing (commits, pull requests, tags, pushes) waits for the person.

End every reply with a line ACTIONS: and then one action per line:
SEND <task>: <what to do next>
TASK: <title of a new task, started at once>
START <task>  (a task filed and never started)
DROP <task>: <why it is not needed>  (a task filed and never started)
RESOLVE <concern number>: <decision>
ASK: <a question only the person can answer, with your recommendation>
DONE: <one line: every part of the goal is built, checked and fits together>
WAIT  (nothing to do until a task reports)
`, checks)
	return b.String()
}

// ActionKind is one thing a supervisor can decide.
type ActionKind string

const (
	ActSend    ActionKind = "send"
	ActTask    ActionKind = "task"
	ActStart   ActionKind = "start"
	ActDrop    ActionKind = "drop"
	ActResolve ActionKind = "resolve"
	ActAsk     ActionKind = "ask"
	ActDone    ActionKind = "done"
	ActWait    ActionKind = "wait"
)

// Action is one parsed line of a supervisor's reply. Work is the task it names,
// Concern the concern it settles, Text what it says.
type Action struct {
	Kind    ActionKind `json:"kind"`
	Work    string     `json:"work,omitempty"`
	Concern int        `json:"concern,omitempty"`
	Text    string     `json:"text,omitempty"`
}

// Line is the action as the supervisor wrote it.
func (a Action) Line() string {
	switch a.Kind {
	case ActSend:
		return fmt.Sprintf("SEND %s: %s", a.Work, a.Text)
	case ActTask:
		return "TASK: " + a.Text
	case ActStart:
		return "START " + a.Work
	case ActDrop:
		return fmt.Sprintf("DROP %s: %s", a.Work, a.Text)
	case ActResolve:
		return fmt.Sprintf("RESOLVE %d: %s", a.Concern, a.Text)
	case ActAsk:
		return "ASK: " + a.Text
	case ActDone:
		return "DONE: " + a.Text
	}
	return "WAIT"
}

var (
	actionsRe = regexp.MustCompile(`^\s*\**ACTIONS:?\**\s*$`)
	sendRe    = regexp.MustCompile(`^SEND\s+(\S+?):\s*(\S.*)$`)
	taskActRe = regexp.MustCompile(`^TASK:\s*(\S.*)$`)
	startRe   = regexp.MustCompile(`^START\s+(\S+)$`)
	dropRe    = regexp.MustCompile(`^DROP\s+(\S+?):\s*(\S.*)$`)
	resolveRe = regexp.MustCompile(`^RESOLVE\s+#?(\d+):\s*(\S.*)$`)
	askActRe  = regexp.MustCompile(`^ASK:\s*(\S.*)$`)
	doneRe    = regexp.MustCompile(`^DONE:\s*(\S.*)$`)
	waitRe    = regexp.MustCompile(`^WAIT\b`)
	bulletRe  = regexp.MustCompile("^(?:[-*]\\s+|\\d+\\.\\s+)?`?")
)

// ParseSupervision reads the actions after the last ACTIONS: line of a reply,
// and the lines that are no action.
func ParseSupervision(texts []string) (actions []Action, refused []string) {
	var lines []string
	for _, t := range texts {
		lines = append(lines, strings.Split(t, "\n")...)
	}
	start := -1
	for i, l := range lines {
		if actionsRe.MatchString(l) {
			start = i + 1
		}
	}
	actions, refused = []Action{}, []string{}
	if start < 0 {
		return actions, refused
	}
	for _, raw := range lines[start:] {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "```") {
			continue
		}
		l = strings.TrimSuffix(bulletRe.ReplaceAllString(l, ""), "`")
		if a, ok := action(l); ok {
			actions = append(actions, a)
			continue
		}
		refused = append(refused, strings.TrimSpace(raw))
	}
	return actions, refused
}

// Said is a supervisor's reply without its actions: what it said before the
// last ACTIONS: line.
func Said(texts []string) string {
	lines := strings.Split(strings.Join(texts, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if actionsRe.MatchString(lines[i]) {
			lines = lines[:i]
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// action is the one action line l says, if it says one.
func action(l string) (Action, bool) {
	if m := sendRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActSend, Work: m[1], Text: strings.TrimSpace(m[2])}, true
	}
	if m := taskActRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActTask, Text: strings.TrimSpace(m[1])}, true
	}
	if m := startRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActStart, Work: m[1]}, true
	}
	if m := dropRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActDrop, Work: m[1], Text: strings.TrimSpace(m[2])}, true
	}
	if m := resolveRe.FindStringSubmatch(l); m != nil {
		n, err := strconv.Atoi(m[1])
		return Action{Kind: ActResolve, Concern: n, Text: strings.TrimSpace(m[2])}, err == nil
	}
	if m := askActRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActAsk, Text: strings.TrimSpace(m[1])}, true
	}
	if m := doneRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActDone, Text: strings.TrimSpace(m[1])}, true
	}
	if waitRe.MatchString(l) {
		return Action{Kind: ActWait}, true
	}
	return Action{}, false
}

// supervisePrefix opens the spawn event that records a goal's supervisor.
const supervisePrefix = "supervise "

// SuperviseEvent is the spawn event body that records h as the goal's
// supervisor.
func SuperviseEvent(h runner.Handle) string {
	ref := ""
	if h.Ref != nil {
		ref = *h.Ref
	}
	return strings.TrimSpace(supervisePrefix + h.Runner + " " + h.Session + " " + ref)
}

// Supervisor is the goal's supervising session, run where it was spawned, as
// its latest supervise event records it.
func Supervisor(l *ledger.Ledger, goal core.Work, p *project.Project) (runner.Handle, bool, error) {
	return goalSession(l, goal, p, supervisePrefix)
}

// stoppedPrefix opens the note filed on work whose session stopped without
// reporting.
const stoppedPrefix = "stopped without a report: "

// Stopped is the note filed on work whose session stopped without a question
// or a report, so the stop is a fact on the record and its goal's supervisor
// looks. It files once at a point of the transcript and reports whether it
// filed.
func Stopped(l *ledger.Ledger, w core.Work, h runner.Handle, texts []string) (bool, error) {
	if w.State != core.StateRunning || len(texts) == 0 {
		return false, nil
	}
	last := texts[len(texts)-1]
	if askRe.MatchString(last) || statusRe.MatchString(last) {
		return false, nil
	}
	line, _, _ := strings.Cut(strings.TrimSpace(last), "\n")
	return l.File(w.ID, core.EventNote, stoppedPrefix+line, core.TranscriptMark{Session: h.Session, Entries: len(texts)})
}

// StoppedSaying is what work last said when its latest event is the note that
// it stopped without a report, else "".
func StoppedSaying(events []core.Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		switch e.Kind {
		case core.EventNote:
			if s, ok := strings.CutPrefix(e.Body, stoppedPrefix); ok {
				return s
			}
		case core.EventReport, core.EventSent, core.EventQuestion, core.EventAnswer:
			return ""
		}
	}
	return ""
}
