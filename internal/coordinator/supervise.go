package coordinator

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"wd/internal/brief"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
)

// Picture is what a goal's supervisor is shown at each look: the goal, what was
// decided on it, what the person wrote that it has not read yet, its sub-goals
// as a tree, those a course change superseded, and what is still unsettled.
type Picture struct {
	Goal       core.Work
	Decisions  []string
	Unread     []string
	Subs       []SubPicture
	Superseded []SubPicture
	Concerns   []core.Concern
	Checks     []string
	Worktree   string
	// Done and Refused are what came of the supervisor's last reply: the
	// actions carried out, and those that did not happen with why.
	Done    []string
	Refused []string
	// Chores are the chores started on the goal, oldest first.
	Chores []ChorePicture
}

// ChorePicture is one chore as its supervisor sees it: what it is for, and
// what it said when it finished, empty while it works.
type ChorePicture struct {
	What     string
	Finished string
}

// SubPicture is one sub-goal as its supervisor sees it: its row, what it waits
// on, its agent's steps, its latest report, and, when its session stopped
// without reporting, what it last said; Superseded is why a course change
// abandoned it. Children are the sub-goals under it.
type SubPicture struct {
	Work    core.Work
	After   []string
	Steps   []core.Step
	Report  string
	Stopped string
	// Told is what the person last told its agent, and Replied what the
	// agent answered, while that reply is its latest exchange.
	Told       string
	Replied    string
	Superseded string
	// Held is why closing it stopped, while it is in review since.
	Held     string
	Children []SubPicture
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
		if s.Held != "" {
			fmt.Fprintf(b, "%scould not close: %s\n", more, strings.ReplaceAll(clip(s.Held, reportClip), "\n", nl))
		}
		if s.Replied != "" {
			fmt.Fprintf(b, "%sthe person told its agent: %s\n", more, strings.ReplaceAll(clip(s.Told, reportClip), "\n", nl))
			fmt.Fprintf(b, "%sit replied: %s\n", more, strings.ReplaceAll(clip(s.Replied, reportClip), "\n", nl))
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
	if len(p.Unread) > 0 {
		b.WriteString("\nThe person wrote to you since you last looked, oldest first. Reply to each of them in words before your COURSE line, then act on them:\n")
		for _, m := range p.Unread {
			fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(strings.TrimSpace(m), "\n", "\n  "))
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
	if len(p.Done)+len(p.Refused) > 0 {
		b.WriteString("\nYour last reply, as carried out:\n")
		for _, d := range p.Done {
			fmt.Fprintf(&b, "- done: %s\n", clip(d, 300))
		}
		for _, r := range p.Refused {
			fmt.Fprintf(&b, "- not done: %s\n", r)
		}
	}
	if len(p.Chores) > 0 {
		b.WriteString("\nChores you started, oldest first; one runs at a time:\n")
		for _, ch := range p.Chores {
			said := "still working"
			if ch.Finished != "" {
				said = "finished, saying: " + strings.ReplaceAll(clip(ch.Finished, reportClip), "\n", "\n  ")
			}
			fmt.Fprintf(&b, "- %s — %s\n", clip(ch.What, 300), said)
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

The person reads your COURSE why, every PROPOSE title, done when, found and why, every ASK question and its choices, your DONE summary, and every GOAL, EDIT and SUPERSEDE line, shown to them as the plan change. %s

End every reply with one line
COURSE: on course | correct | pivot — <why>
then a line ACTIONS: and then one action per line:
%s`, checks, brief.ForThePerson, actionList)
	return b.String()
}

// actionList is every action a supervisor may take, one per line, as the
// brief teaches them.
const actionList = `SEND <id>: <what its agent does next>
GOAL <parent>: <an outcome a person would say> | done when: <checkable from a report, verify or PR> [| after: <id>,<id>] [| split]  (a new sub-goal under the goal or one of its sub-goals; split plans it further)
START <id>  (a sub-goal filed and never started)
SUPERSEDE <id>: <why the course no longer needs it>  (stops its agents)
PAUSE <id>: <why>  (holds a sub-goal the person told to stop or wait, and stops its agents; resuming it carries on)
EDIT <id>: [title: <title>] [| done when: <done when>]
AFTER <id>: <id>,<id> | none
MET <id>: <why its done-when holds>  (closes a sub-goal in review; nothing closes on its report alone)
MISSED <id>: <what is missing>  (sends it back to its agent)
CHORE: <what the director needs done that is no sub-goal, run as a session on the goal>
PROPOSE: <title> | done when: <done when> | finish: <discovery, research, local build, complete PR, merged or released> [| found: <what was found that calls for it>] — <why>  (a change to the top goal; it waits for the person)
RESOLVE <concern number>: <decision>
ASK: <a question only the person can answer>
- <a choice, your recommendation first, marked (recommended)>
- <another choice>  (offer choices whenever the answer is one of a few; the person can always answer in words)
DONE: <one line: every part of the goal is built, checked and fits together>
WAIT  (nothing to do until a sub-goal reports)
`

// FinishWords names a finish line in a sentence for the person.
func FinishWords(line core.FinishLine) string {
	switch line {
	case core.FinishLocalBuild:
		return "a local build"
	case core.FinishPR:
		return "a complete pull request"
	case core.FinishDiscovery:
		return "a discovery report"
	}
	return string(line)
}

// PlanWords says one plan action line as a sentence for the person, naming
// work by the title title gives its id; a line that is not one plan action
// reads as written.
func PlanWords(line string, title func(id string) (string, error)) (string, error) {
	acts, _ := ParseSupervision([]string{"ACTIONS:", line})
	if len(acts) != 1 {
		return line, nil
	}
	a := acts[0]
	titles := func(ids []string) (string, error) {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			t, err := title(id)
			if err != nil {
				return "", err
			}
			out = append(out, t)
		}
		return strings.Join(out, ", "), nil
	}
	switch a.Kind {
	case ActGoal, ActStart, ActSupersede, ActEdit, ActAfter:
	default:
		return line, nil
	}
	name, err := title(a.Work)
	if err != nil {
		return "", err
	}
	switch a.Kind {
	case ActGoal:
		said := fmt.Sprintf("Add “%s” under %s, done when %s", a.Title, name, a.DoneWhen)
		if len(a.After) > 0 {
			after, err := titles(a.After)
			if err != nil {
				return "", err
			}
			said += ", after " + after
		}
		return said, nil
	case ActStart:
		return "Start " + name, nil
	case ActSupersede:
		return fmt.Sprintf("Drop %s: %s", name, a.Text), nil
	case ActEdit:
		var parts []string
		if a.Title != "" {
			parts = append(parts, fmt.Sprintf("call it “%s”", a.Title))
		}
		if a.DoneWhen != "" {
			parts = append(parts, "done when "+a.DoneWhen)
		}
		return fmt.Sprintf("Change %s: %s", name, strings.Join(parts, "; ")), nil
	}
	after, err := titles(a.After)
	if err != nil {
		return "", err
	}
	if after == "" {
		return name + " waits on nothing", nil
	}
	return name + " waits on " + after, nil
}

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
	ActSend      ActionKind = "send"
	ActGoal      ActionKind = "goal"
	ActStart     ActionKind = "start"
	ActSupersede ActionKind = "supersede"
	ActPause     ActionKind = "pause"
	ActEdit      ActionKind = "edit"
	ActAfter     ActionKind = "after"
	ActMet       ActionKind = "met"
	ActMissed    ActionKind = "missed"
	ActChore     ActionKind = "chore"
	ActPropose   ActionKind = "propose"
	ActResolve   ActionKind = "resolve"
	ActAsk       ActionKind = "ask"
	ActDone      ActionKind = "done"
	ActWait      ActionKind = "wait"
)

// Action is one parsed line of a supervisor's reply. Work is the sub-goal it
// names (a GOAL's parent), Concern the concern it settles, Text what it says.
type Action struct {
	Kind    ActionKind `json:"kind"`
	Work    string     `json:"work,omitempty"`
	Concern int        `json:"concern,omitempty"`
	Text    string     `json:"text,omitempty"`
	// Options are the choices an ASK offers, in the order given.
	Options []string `json:"options,omitempty"`
	// Title and DoneWhen are what a GOAL files, an EDIT changes (empty is
	// unchanged) and a PROPOSE proposes; After is what a GOAL or AFTER waits
	// on; Split files a GOAL to be planned further; Finish is a PROPOSE's
	// finish line, and Found what was found that calls for it.
	Title    string          `json:"title,omitempty"`
	DoneWhen string          `json:"done_when,omitempty"`
	After    []string        `json:"after,omitempty"`
	Split    bool            `json:"split,omitempty"`
	Finish   core.FinishLine `json:"finish,omitempty"`
	Found    string          `json:"found,omitempty"`
}

// Line is the action as the supervisor wrote it.
func (a Action) Line() string {
	switch a.Kind {
	case ActSend:
		return fmt.Sprintf("SEND %s: %s", a.Work, a.Text)
	case ActGoal:
		parts := []string{a.Title, "done when: " + a.DoneWhen}
		if len(a.After) > 0 {
			parts = append(parts, "after: "+strings.Join(a.After, ","))
		}
		if a.Split {
			parts = append(parts, "split")
		}
		return fmt.Sprintf("GOAL %s: %s", a.Work, strings.Join(parts, " | "))
	case ActStart:
		return "START " + a.Work
	case ActSupersede:
		return fmt.Sprintf("SUPERSEDE %s: %s", a.Work, a.Text)
	case ActPause:
		return fmt.Sprintf("PAUSE %s: %s", a.Work, a.Text)
	case ActEdit:
		var parts []string
		if a.Title != "" {
			parts = append(parts, "title: "+a.Title)
		}
		if a.DoneWhen != "" {
			parts = append(parts, "done when: "+a.DoneWhen)
		}
		return fmt.Sprintf("EDIT %s: %s", a.Work, strings.Join(parts, " | "))
	case ActAfter:
		waits := "none"
		if len(a.After) > 0 {
			waits = strings.Join(a.After, ",")
		}
		return fmt.Sprintf("AFTER %s: %s", a.Work, waits)
	case ActMet:
		return fmt.Sprintf("MET %s: %s", a.Work, a.Text)
	case ActMissed:
		return fmt.Sprintf("MISSED %s: %s", a.Work, a.Text)
	case ActChore:
		return "CHORE: " + a.Text
	case ActPropose:
		found := ""
		if a.Found != "" {
			found = " | found: " + a.Found
		}
		return fmt.Sprintf("PROPOSE: %s | done when: %s | finish: %s%s — %s", a.Title, a.DoneWhen, a.Finish, found, a.Text)
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
	actionsRe    = regexp.MustCompile(`^\s*\**ACTIONS:?\**\s*$`)
	sendRe       = regexp.MustCompile(`^SEND\s+(\S+?):\s*(\S.*)$`)
	goalActRe    = regexp.MustCompile(`^GOAL\s+(\S+?):\s*(\S.*)$`)
	startRe      = regexp.MustCompile(`^START\s+(\S+)$`)
	supersedeRe  = regexp.MustCompile(`^SUPERSEDE\s+(\S+?):\s*(\S.*)$`)
	pauseActRe   = regexp.MustCompile(`^PAUSE\s+(\S+?):\s*(\S.*)$`)
	editRe       = regexp.MustCompile(`^EDIT\s+(\S+?):\s*(\S.*)$`)
	afterActRe   = regexp.MustCompile(`^AFTER\s+(\S+?):\s*(\S.*)$`)
	metRe        = regexp.MustCompile(`^MET\s+(\S+?):\s*(\S.*)$`)
	missedRe     = regexp.MustCompile(`^MISSED\s+(\S+?):\s*(\S.*)$`)
	choreRe      = regexp.MustCompile(`^CHORE:\s*(\S.*)$`)
	proposeActRe = regexp.MustCompile(`^PROPOSE:\s*(\S.*?)\s+—\s+(\S.*)$`)
	resolveRe    = regexp.MustCompile(`^RESOLVE\s+#?(\d+):\s*(\S.*)$`)
	askActRe     = regexp.MustCompile(`^ASK:\s*(\S.*)$`)
	doneRe       = regexp.MustCompile(`^DONE:\s*(\S.*)$`)
	waitRe       = regexp.MustCompile(`^WAIT\b`)
	bulletRe     = regexp.MustCompile("^(?:[-*]\\s+|\\d+\\.\\s+)?`?")
	listRe       = regexp.MustCompile(`^(?:[-*]\s+|\d+\.\s+)`)
)

// fields reads the ` | `-separated parts of an action after its first: each
// `<name>: <value>` by its name, and a bare word as a flag. ok is false when
// a part is not one of names.
func fields(parts []string, names ...string) (map[string]string, bool) {
	out := map[string]string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		name, value, has := strings.Cut(p, ":")
		name = strings.ToLower(strings.TrimSpace(name))
		if !slices.Contains(names, name) {
			return nil, false
		}
		if _, dup := out[name]; dup {
			return nil, false
		}
		if has {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, false
			}
		}
		out[name] = value
	}
	return out, true
}

// ids reads a comma-separated list of work ids; "none" is none.
func ids(raw string) []string {
	out := []string{}
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" && s != "none" {
			out = append(out, s)
		}
	}
	return out
}

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

// ReplyOutcome is what a supervised event says came of the reply it files: the
// lines under its Done: and Refused: headings.
func ReplyOutcome(body string) (done, refused []string) {
	var into *[]string
	for _, line := range strings.Split(body, "\n") {
		switch line {
		case "Done:":
			into = &done
			continue
		case "Refused:":
			into = &refused
			continue
		}
		if item, ok := strings.CutPrefix(line, "- "); ok && into != nil {
			*into = append(*into, item)
		}
	}
	return done, refused
}

// action is the one action line l says, if it says one.
func action(l string) (Action, bool) {
	if m := sendRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActSend, Work: m[1], Text: strings.TrimSpace(m[2])}, true
	}
	if m := goalActRe.FindStringSubmatch(l); m != nil {
		parts := strings.Split(m[2], "|")
		f, ok := fields(parts[1:], "done when", "after", "split")
		title := strings.TrimSpace(parts[0])
		if !ok || title == "" || f["done when"] == "" || f["split"] != "" {
			return Action{}, false
		}
		_, split := f["split"]
		a := Action{Kind: ActGoal, Work: m[1], Title: title, DoneWhen: f["done when"], Split: split}
		if after := ids(f["after"]); len(after) > 0 {
			a.After = after
		}
		return a, true
	}
	if m := startRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActStart, Work: m[1]}, true
	}
	if m := supersedeRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActSupersede, Work: m[1], Text: strings.TrimSpace(m[2])}, true
	}
	if m := pauseActRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActPause, Work: m[1], Text: strings.TrimSpace(m[2])}, true
	}
	if m := editRe.FindStringSubmatch(l); m != nil {
		f, ok := fields(strings.Split(m[2], "|"), "title", "done when")
		if !ok || (f["title"] == "" && f["done when"] == "") {
			return Action{}, false
		}
		return Action{Kind: ActEdit, Work: m[1], Title: f["title"], DoneWhen: f["done when"]}, true
	}
	if m := afterActRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActAfter, Work: m[1], After: ids(m[2])}, true
	}
	if m := metRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActMet, Work: m[1], Text: strings.TrimSpace(m[2])}, true
	}
	if m := missedRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActMissed, Work: m[1], Text: strings.TrimSpace(m[2])}, true
	}
	if m := choreRe.FindStringSubmatch(l); m != nil {
		return Action{Kind: ActChore, Text: strings.TrimSpace(m[1])}, true
	}
	if m := proposeActRe.FindStringSubmatch(l); m != nil {
		parts := strings.Split(m[1], "|")
		f, ok := fields(parts[1:], "done when", "finish", "found")
		title := strings.TrimSpace(parts[0])
		if !ok || title == "" || f["done when"] == "" {
			return Action{}, false
		}
		finish, err := core.ParseFinishLine(f["finish"])
		if err != nil {
			return Action{}, false
		}
		return Action{Kind: ActPropose, Title: title, DoneWhen: f["done when"], Finish: finish, Found: f["found"], Text: strings.TrimSpace(m[2])}, true
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

// stoppedClip is how much of what a stopped session last said its note keeps.
const stoppedClip = 4000

// repliedPrefix opens the note filed on work whose session answered the
// person's message with neither a question nor a report.
const repliedPrefix = "replied: "

// Stopped is the note filed on work whose session went quiet without a
// question or a report, with what it last said, so it is a fact on the record
// and its goal's supervisor looks. After a message from the person it is a
// reply: the work waits on the person when the reply asks something, and
// otherwise returns to review or blocked when it was there before the message,
// else is held paused. Any other quiet is a stop: nothing works on it any
// more, so it waits on the person in needs-input. Running work whose latest
// exchange is such a note already moves the same way. It files once at a point
// of the transcript and reports whether it filed.
func Stopped(l *ledger.Ledger, w core.Work, h runner.Handle, texts []string) (bool, error) {
	if w.State != core.StateRunning || len(texts) == 0 {
		return false, nil
	}
	last := texts[len(texts)-1]
	if askRe.MatchString(last) || statusRe.MatchString(last) {
		return false, nil
	}
	events, err := l.Events(w.ID, nil)
	if err != nil {
		return false, err
	}
	msg, replying := personMessage(events)
	prefix := stoppedPrefix
	if replying {
		prefix = repliedPrefix
	}
	filed, err := l.File(w.ID, core.EventNote, prefix+clip(last, stoppedClip), core.TranscriptMark{Session: h.Session, Entries: len(texts)})
	if err != nil {
		return false, err
	}
	if !filed {
		if events, err = l.Events(w.ID, nil); err != nil {
			return false, err
		}
		note, ok := quietNote(events)
		if !ok {
			return false, nil
		}
		replying = strings.HasPrefix(events[note].Body, repliedPrefix)
		msg, _ = personMessage(events[:note])
	}
	target := core.StateNeedsInput
	if replying {
		target = replyState(last, events[:msg])
	}
	if _, err := l.Transition(w.ID, target); err != nil {
		// Another pass reading the same quiet may have moved it first.
		if now, gerr := l.Get(w.ID); gerr != nil || now.State == core.StateRunning {
			return false, err
		}
	}
	return filed, nil
}

// personMessage is the index in events, one work's, of the person's latest
// message to it since its last report, question, stop or reply.
func personMessage(events []core.Event) (int, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		switch e.Kind {
		case core.EventSent:
			if e.SentBy == core.SentByPerson {
				return i, true
			}
		case core.EventReport, core.EventQuestion:
			return 0, false
		case core.EventNote:
			if strings.HasPrefix(e.Body, stoppedPrefix) || strings.HasPrefix(e.Body, repliedPrefix) {
				return 0, false
			}
		}
	}
	return 0, false
}

// quietNote is the index of the stop or reply note that is the latest
// exchange in events, one work's.
func quietNote(events []core.Event) (int, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		switch e.Kind {
		case core.EventNote:
			if strings.HasPrefix(e.Body, stoppedPrefix) || strings.HasPrefix(e.Body, repliedPrefix) {
				return i, true
			}
		case core.EventReport, core.EventSent, core.EventQuestion, core.EventAnswer:
			return 0, false
		}
	}
	return 0, false
}

// replyState is where work goes once its agent replied said to the person:
// needs-input when the reply asks something, else review or blocked when the
// work was there before the person's message, else paused. before is the
// work's events up to that message.
func replyState(said string, before []core.Event) core.State {
	lines := strings.Split(strings.TrimSpace(said), "\n")
	if strings.HasSuffix(lastSentence(lines[len(lines)-1]), "?") {
		return core.StateNeedsInput
	}
	for i := len(before) - 1; i >= 0; i-- {
		if before[i].Kind != core.EventState {
			continue
		}
		if s := core.State(before[i].Body); s == core.StateReview || s == core.StateBlocked {
			return s
		}
		break
	}
	return core.StatePaused
}

// ReplyExchange is what the person last told work's agent and what it
// replied, while that reply is the work's latest exchange. events are one
// work's.
func ReplyExchange(events []core.Event) (told, replied string, ok bool) {
	note, ok := quietNote(events)
	if !ok {
		return "", "", false
	}
	replied, ok = strings.CutPrefix(events[note].Body, repliedPrefix)
	if !ok {
		return "", "", false
	}
	if msg, sent := personMessage(events[:note]); sent {
		told = events[msg].Body
	}
	return told, replied, true
}

// RepliedNote is what work's agent replied to the person and the note filing
// it, when the work's latest exchange is that reply. events are one work's.
func RepliedNote(events []core.Event) (string, core.Event, bool) {
	i, ok := quietNote(events)
	if !ok {
		return "", core.Event{}, false
	}
	said, replied := strings.CutPrefix(events[i].Body, repliedPrefix)
	return said, events[i], replied
}

// StoppedQuestion is what work that stopped without a report, or replied to
// the person asking something, waits on the person for: what it last said,
// offered Yes and No when it ends asking one.
func StoppedQuestion(e core.Event) runner.Question {
	said := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(e.Body, stoppedPrefix), repliedPrefix))
	it := runner.QuestionItem{Question: said, Options: []runner.Option{}}
	lines := strings.Split(said, "\n")
	ask := runner.QuestionItem{Question: lastSentence(lines[len(lines)-1]), Options: []runner.Option{}}
	if strings.HasSuffix(ask.Question, "?") {
		offerYesNo(&ask)
		it.Options = ask.Options
	}
	return runner.Question{ID: fmt.Sprintf("event-%d", e.ID), Items: []runner.QuestionItem{it}}
}

// lastSentence is the last sentence of a line.
func lastSentence(line string) string {
	line = strings.TrimSpace(line)
	cut := 0
	for _, end := range []string{". ", "? ", "! "} {
		if i := strings.LastIndex(line, end); i >= 0 && i+2 > cut {
			cut = i + 2
		}
	}
	return line[cut:]
}

// StoppedSaying is what work last said when its latest event is the note that
// it stopped without a report, else "".
func StoppedSaying(events []core.Event) string {
	said, _, _ := StoppedNote(events)
	return said
}

// StoppedNote is what work last said and the note filing it, when the work's
// latest exchange is the note that it stopped without a report. events are
// one work's.
func StoppedNote(events []core.Event) (string, core.Event, bool) {
	i, ok := quietNote(events)
	if !ok {
		return "", core.Event{}, false
	}
	said, stopped := strings.CutPrefix(events[i].Body, stoppedPrefix)
	return said, events[i], stopped
}

// QuestionBody is a supervisor's questions as one question event: each on its
// own line, the rest of its text indented under it, followed by its choices
// as `- ` lines.
func QuestionBody(asks []Action) string {
	var lines []string
	for _, a := range asks {
		first, detail, _ := strings.Cut(a.Text, "\n")
		lines = append(lines, first)
		for _, d := range strings.Split(detail, "\n") {
			if strings.TrimSpace(d) != "" {
				lines = append(lines, "  "+strings.TrimSpace(d))
			}
		}
		for _, o := range a.Options {
			lines = append(lines, "- "+o)
		}
	}
	return strings.Join(lines, "\n")
}

// AskedQuestion reads a question event as the question it asks: each line one
// question, the indented lines under it part of it, and the `- ` lines under
// it its choices.
func AskedQuestion(e core.Event) runner.Question {
	q := runner.Question{ID: fmt.Sprintf("event-%d", e.ID), Items: []runner.QuestionItem{}}
	for _, raw := range strings.Split(e.Body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if o, ok := strings.CutPrefix(line, "- "); ok && len(q.Items) > 0 {
			it := &q.Items[len(q.Items)-1]
			it.Options = append(it.Options, runner.Option{Label: strings.TrimSpace(o)})
			continue
		}
		// An indented line under a question is part of it.
		if n := len(q.Items); n > 0 && strings.HasPrefix(raw, "  ") && len(q.Items[n-1].Options) == 0 {
			q.Items[n-1].Question += "\n" + line
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
