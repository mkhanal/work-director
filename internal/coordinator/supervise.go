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
// decided on it, its sub-goals as a tree, those a course change superseded,
// and what is still unsettled.
type Picture struct {
	Goal       core.Work
	Decisions  []string
	Subs       []SubPicture
	Superseded []SubPicture
	Concerns   []core.Concern
	Checks     []string
	Worktree   string
}

// SubPicture is one sub-goal as its supervisor sees it: its row, what it waits
// on, its agent's steps, its latest report, and, when its session stopped
// without reporting, what it last said; Superseded is why a course change
// abandoned it. Children are the sub-goals under it.
type SubPicture struct {
	Work       core.Work
	After      []string
	Steps      []core.Step
	Report     string
	Stopped    string
	Superseded string
	Children   []SubPicture
}

// reportClip bounds each report in the brief: the supervisor reads every
// sub-goal at every look, and a long report says what it has to in its first
// lines.
const reportClip = 1500

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// writeSubs lists subs and their children, each level indented two more.
func writeSubs(b *strings.Builder, subs []SubPicture, indent string) {
	more := indent + "  "
	nl := "\n" + more + "  "
	for _, s := range subs {
		fmt.Fprintf(b, "%s- %s [%s] %s\n", indent, s.Work.ID, s.Work.State, s.Work.Title)
		fmt.Fprintf(b, "%sdone when: %s\n", more, textOr(s.Work.DoneWhen, "not set"))
		if len(s.After) > 0 {
			fmt.Fprintf(b, "%swaits on: %s\n", more, strings.Join(s.After, ", "))
		}
		if len(s.Steps) > 0 {
			done, total, doing := 0, 0, ""
			for _, st := range s.Steps {
				if st.State == core.StepDropped {
					continue
				}
				total++
				if st.State == core.StepDone {
					done++
				}
				if st.State == core.StepDoing && doing == "" {
					doing = st.Text
				}
			}
			if doing == "" {
				doing = "no step in hand"
			}
			fmt.Fprintf(b, "%sdoing: %s (%d/%d steps done)\n", more, doing, done, total)
		}
		if s.Report != "" {
			fmt.Fprintf(b, "%slatest report: %s\n", more, strings.ReplaceAll(clip(s.Report, reportClip), "\n", nl))
		}
		if s.Stopped != "" {
			fmt.Fprintf(b, "%sstopped without a report; it last said: %s\n", more, strings.ReplaceAll(clip(s.Stopped, reportClip), "\n", nl))
		}
		writeSubs(b, s.Children, more)
	}
}

func textOr(s *string, empty string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return empty
	}
	return *s
}

// SupervisionBrief is what the goal's supervisor is given at each look.
func SupervisionBrief(p Picture) string {
	var b strings.Builder
	g := p.Goal
	fmt.Fprintf(&b, "You supervise goal %s for the director, as its head of engineering. The person asked:\n\n%s\n", g.ID, g.Title)
	if g.Detail != "" && g.Detail != g.Title {
		fmt.Fprintf(&b, "%s\n", g.Detail)
	}
	finish := "not decided"
	if g.FinishLine != nil {
		finish = string(*g.FinishLine)
	}
	fmt.Fprintf(&b, "\nDone when: %s\nFinishes at: %s\n", textOr(g.DoneWhen, "not set"), finish)
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
	fmt.Fprintf(&b, "\nSub-goals; each leaf is its own agent session working in %s:\n", where)
	writeSubs(&b, p.Subs, "")
	if len(p.Superseded) > 0 {
		b.WriteString("\nSuperseded by a course change, with why; do not propose them again:\n")
		for _, s := range p.Superseded {
			fmt.Fprintf(&b, "- %s %s: %s\n", s.Work.ID, s.Work.Title, s.Superseded)
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
Your first duty is the top goal's done-when: at every look, judge whether the open sub-goals still lead to it. Your job is the goal as the person asked it, not each sub-goal on its own. Read the reports, and read the work itself in the worktree (git status, git diff) to check it; do not edit, create or delete files. Each agent keeps to its own sub-goal and leaves work between sub-goals to you: a missing wire-up, a gap no sub-goal owns, a concern one raised about another. Settle concerns yourself unless only the person can: a business fact, or something irreversible or seen outside the repository, such as pushing a tag. Prefer telling an existing agent over adding a sub-goal. The director runs the checks itself (%s), and landing (commits, pull requests, tags, pushes) waits for the person.

End every reply with one line
COURSE: on course | correct | pivot — <why>
then a line ACTIONS: and then one action per line:
%s`, checks, actionList)
	return b.String()
}

// actionList is every action a supervisor may take, one per line, as the
// brief teaches them.
const actionList = `SEND <id>: <what to do next>
TASK: <title of a new task, started at once>
START <id>  (a sub-goal filed and never started)
DROP <id>: <why it is not needed>  (a task filed and never started)
RESOLVE <concern number>: <decision>
ASK: <a question only the person can answer>
- <a choice, your recommendation first, marked (recommended)>
- <another choice>  (offer choices whenever the answer is one of a few; the person can always answer in words)
DONE: <one line: every part of the goal is built, checked and fits together>
WAIT  (nothing to do until a sub-goal reports)
`

// CourseVerdict is a supervisor's judgement of where its goal stands.
type CourseVerdict string

const (
	CourseOn      CourseVerdict = "on course"
	CourseCorrect CourseVerdict = "correct"
	CoursePivot   CourseVerdict = "pivot"
)

// Course is a reply's COURSE line: its verdict and why.
type Course struct {
	Verdict CourseVerdict
	Why     string
}

var courseRe = regexp.MustCompile(`(?i)^\s*\**COURSE:\**\s*(on course|correct|pivot)\s*(?:—|–|-{1,2}|:)\s*(\S.*)$`)

// ParseCourse reads the last COURSE line of a reply.
func ParseCourse(texts []string) (Course, bool) {
	lines := strings.Split(strings.Join(texts, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := courseRe.FindStringSubmatch(lines[i]); m != nil {
			return Course{Verdict: CourseVerdict(strings.ToLower(m[1])), Why: strings.TrimSpace(m[2])}, true
		}
	}
	return Course{}, false
}

// FileCourse files a reply's course on goal as one course event, "unstated"
// when the reply gave none.
func FileCourse(l *ledger.Ledger, goal string, texts []string) error {
	body := "unstated"
	if c, ok := ParseCourse(texts); ok {
		body = string(c.Verdict) + ": " + c.Why
	}
	_, err := l.AddEvent(goal, core.EventCourse, body)
	return err
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
	// Options are the choices an ASK offers, in the order given.
	Options []string `json:"options,omitempty"`
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
		if len(a.Options) > 0 {
			return "ASK: " + a.Text + " (" + strings.Join(a.Options, " | ") + ")"
		}
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
	listRe    = regexp.MustCompile(`^(?:[-*]\s+|\d+\.\s+)`)
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
		if l == "" || strings.HasPrefix(l, "```") || courseRe.MatchString(l) {
			continue
		}
		listed := listRe.MatchString(l)
		l = strings.TrimSuffix(bulletRe.ReplaceAllString(l, ""), "`")
		if a, ok := action(l); ok {
			actions = append(actions, a)
			continue
		}
		// A listed line under an ASK is one of its choices.
		if n := len(actions); listed && n > 0 && actions[n-1].Kind == ActAsk {
			actions[n-1].Options = append(actions[n-1].Options, l)
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

// QuestionBody is a supervisor's questions as one question event: each on its
// own line, followed by its choices as `- ` lines.
func QuestionBody(asks []Action) string {
	var lines []string
	for _, a := range asks {
		lines = append(lines, a.Text)
		for _, o := range a.Options {
			lines = append(lines, "- "+o)
		}
	}
	return strings.Join(lines, "\n")
}

// AskedQuestion reads a question event as the question it asks: each line one
// question, and the `- ` lines under it its choices.
func AskedQuestion(e core.Event) runner.Question {
	q := runner.Question{ID: fmt.Sprintf("event-%d", e.ID), Items: []runner.QuestionItem{}}
	for _, line := range strings.Split(e.Body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if o, ok := strings.CutPrefix(line, "- "); ok && len(q.Items) > 0 {
			it := &q.Items[len(q.Items)-1]
			it.Options = append(it.Options, runner.Option{Label: strings.TrimSpace(o)})
			continue
		}
		q.Items = append(q.Items, runner.QuestionItem{Question: line, Options: []runner.Option{}})
	}
	for i := range q.Items {
		offerYesNo(&q.Items[i])
	}
	return q
}

var (
	yesNoRe     = regexp.MustCompile(`(?i)^\W*(should|shall|is|are|was|were|do|does|did|can|could|will|would|has|have|had|may|might|must|ok|okay|confirm)\b`)
	recommendRe = regexp.MustCompile(`(?i)\brecommend(?:ation)?:?\s+(yes|no)\b`)
	proposeRe   = regexp.MustCompile(`(?i)\bI\s+(propose|suggest)\b`)
)

// offerYesNo gives a yes-or-no question that offers no choices Yes and No,
// so it is answered with a pick; the one it recommends is marked so.
func offerYesNo(it *runner.QuestionItem) {
	proposal := proposeRe.MatchString(it.Question)
	if len(it.Options) > 0 || !(proposal || yesNoRe.MatchString(strings.TrimSpace(it.Question))) {
		return
	}
	yes, no := runner.Option{Label: "Yes"}, runner.Option{Label: "No"}
	if proposal {
		yes.Label = "Yes (recommended)"
	}
	if m := recommendRe.FindStringSubmatch(it.Question); m != nil {
		if strings.EqualFold(m[1], "yes") {
			yes.Label = "Yes (recommended)"
		} else {
			no.Label = "No (recommended)"
		}
	}
	it.Options = []runner.Option{yes, no}
}

// ReportQuestion is what a report saying it needs the person asks: what it
// said with its verdict, else its notes, else the whole report.
func ReportQuestion(e core.Event) runner.Question {
	r := core.ParseReport(e.Body)
	asked := strings.TrimSpace(r.Said)
	for _, s := range r.Sections {
		if asked == "" && s.Name == "NOTES" {
			asked = s.Text
		}
	}
	if asked == "" {
		asked = strings.TrimSpace(e.Body)
	}
	it := runner.QuestionItem{Question: asked, Options: []runner.Option{}}
	offerYesNo(&it)
	return runner.Question{ID: fmt.Sprintf("event-%d", e.ID), Items: []runner.QuestionItem{it}}
}

// OpenQuestion is the latest question asked on work that nothing has answered
// since: no answer, and no decision on it.
func OpenQuestion(events []core.Event, work string) (core.Event, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Work != work {
			continue
		}
		switch e.Kind {
		case core.EventQuestion:
			return e, true
		case core.EventAnswer, core.EventDecision:
			return core.Event{}, false
		}
	}
	return core.Event{}, false
}
