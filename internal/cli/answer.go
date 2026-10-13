package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/judge"
	"wd/internal/project"
	"wd/internal/runner"
)

// answer replies to the question work's session is waiting on. A question with
// options takes one answer per item, in the order asked; a question asked in
// words takes the words. The answer is recorded on the work and reaches the
// session the way any message does. --director has the judgement model decide
// every item in the person's place instead.
func (c *Cli) answer(rest []string) error {
	director := flag(c.Args, "director")
	if director && len(rest) != 1 {
		return fail("usage: wd answer <id> --director; the director decides every item of the question")
	}
	if !director && len(rest) < 2 {
		return fail("usage: wd answer <id> <answer> [<answer>...], one answer per question asked, in order")
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	answers := rest[1:]
	by := core.Decision{Source: core.AnsweredByPerson}
	var decided []directorChoice
	if director {
		q, ok, err := c.openQuestion(w)
		if err != nil {
			return err
		}
		if !ok {
			return fail("%s is not waiting on a question the director can decide", w.ID)
		}
		var declined []directorChoice
		if decided, declined, by, err = c.directorDecides(w, q); err != nil {
			return err
		}
		if len(declined) > 0 {
			return c.directorDeclined(w, decided, declined)
		}
		answers = make([]string, len(decided))
		for i, d := range decided {
			answers[i] = d.Answer
		}
	}
	h, has, err := c.answerHandle(w)
	if err != nil {
		return err
	}
	var entries []runner.Entry
	if has {
		r, err := runner.RunnerNamed(h.Runner)
		if err != nil {
			return err
		}
		if entries, err = r.Conversation(h); err != nil {
			return err
		}
	}
	var text, asked string
	var filed *core.Event
	if q, ok := runner.Asking(entries); ok {
		if text, err = runner.AnswerText(*q, answers); err != nil {
			return fail("%s: %v", w.ID, err)
		}
		asked = q.Text()
	} else if w.State == core.StateNeedsInput {
		text = strings.Join(answers, "\n")
		events, err := c.Ledger.Events(w.ID, nil)
		if err != nil {
			return err
		}
		// A question the director filed takes one answer per item, or words.
		if e, ok := coordinator.OpenQuestion(events, w.ID); ok {
			filed = &e
			asked = e.Body
			if q := coordinator.AskedQuestion(e); len(answers) > 1 || len(q.Items) == 1 {
				if text, err = runner.AnswerText(q, answers); err != nil {
					return fail("%s: %v", w.ID, err)
				}
			}
		}
	} else {
		return fail("%s is not asking anything; tell it something with wd send %s \"<text>\"", w.ID, w.ID)
	}
	var setting *finishSetting
	if flag(c.Args, "remember") {
		if setting, err = c.finishToRemember(w, filed, answers); err != nil {
			return err
		}
	}
	by.Question, by.Answer = asked, text
	if _, err := c.Ledger.Answer(w.ID, by); err != nil {
		return err
	}
	for _, d := range decided {
		said := fmt.Sprintf("the director decided %q: %s, because %s", d.Question, d.Answer, d.Why)
		if _, err := c.Ledger.Decide(w.ID, said, core.Decision{Question: d.Question, Answer: d.Answer, Source: core.AnsweredByDirector, Runner: by.Runner, Model: by.Model}, nil); err != nil {
			return err
		}
	}
	applied := []string{}
	if filed != nil {
		if applied, err = c.answered(w, *filed, answers); err != nil {
			return err
		}
		if setting != nil {
			if err := os.WriteFile(setting.path, []byte(setting.text), 0o644); err != nil {
				return err
			}
			said := "finish setting: " + string(setting.line)
			if _, err := c.Ledger.Decide(w.ID, said, core.Decision{Question: "where " + w.Project + "'s goals finish", Answer: string(setting.line), Source: core.AnsweredByPerson}, nil); err != nil {
				return err
			}
			applied = append(applied, said)
		}
		// Carrying out a change can have moved the work on.
		if w, err = c.Ledger.Get(w.ID); err != nil {
			return err
		}
	}
	where, err := c.deliverAnswer(w, h, has, text)
	if err != nil {
		return err
	}
	human := "answered"
	if where == toldQueued {
		human = fmt.Sprintf("answer queued: %s reads it when it is ready", w.ID)
	}
	if len(applied) > 0 {
		human += "\n" + strings.Join(applied, "\n")
	}
	out := map[string]any{"ok": true, "told": where, "answer": text, "applied": applied}
	if director {
		out["answered"], out["decided"], out["declined"] = true, decided, []directorChoice{}
		for _, d := range decided {
			human = fmt.Sprintf("the director decided %q: %s, because %s\n", d.Question, d.Answer, d.Why) + human
		}
	}
	return c.out(out, human)
}

// directorChoice is the director's judgement on one item of a question: its
// answer and why, or why it declined.
type directorChoice struct {
	Question string `json:"question"`
	Answer   string `json:"answer,omitempty"`
	Why      string `json:"why"`
}

// answerHandle is the session an answer to w goes to. A goal's is its own
// session or its supervisor's, never the session that planned it, and a goal
// may have none yet; any other work must have one.
func (c *Cli) answerHandle(w core.Work) (runner.Handle, bool, error) {
	if !core.IsGoal(w.Kind) || w.Session != nil || w.Claim != nil {
		h, err := c.handle(w.ID)
		return h, err == nil, err
	}
	p, err := c.project(w.Project)
	if err != nil {
		return runner.Handle{}, false, err
	}
	return coordinator.Supervisor(c.Ledger, w, p)
}

// deliverAnswer sends text through h when there is a session to read it; a
// goal with none keeps it recorded and goes back to work.
func (c *Cli) deliverAnswer(w core.Work, h runner.Handle, has bool, text string) (told, error) {
	if !core.IsGoal(w.Kind) {
		return c.tellTask(w, text)
	}
	if has {
		return c.tellVia(w, h, text)
	}
	if w.State == core.StateNeedsInput {
		if _, err := c.Ledger.Transition(w.ID, core.StateRunning); err != nil {
			return "", err
		}
	}
	return toldRecorded, nil
}

// openQuestion is the question w waits on: its session's, or one the director
// filed on it while it needs input.
func (c *Cli) openQuestion(w core.Work) (runner.Question, bool, error) {
	h, has, err := c.answerHandle(w)
	if err != nil {
		return runner.Question{}, false, err
	}
	if has {
		r, err := runner.RunnerNamed(h.Runner)
		if err != nil {
			return runner.Question{}, false, err
		}
		entries, err := r.Conversation(h)
		if err != nil {
			return runner.Question{}, false, err
		}
		if q, asking := runner.Asking(entries); asking {
			return *q, true, nil
		}
	}
	if w.State != core.StateNeedsInput {
		return runner.Question{}, false, nil
	}
	events, err := c.Ledger.Events(w.ID, nil)
	if err != nil {
		return runner.Question{}, false, err
	}
	e, ok := coordinator.OpenQuestion(events, w.ID)
	if !ok {
		return runner.Question{}, false, nil
	}
	return coordinator.AskedQuestion(e), true, nil
}

// whyRe reads the reason a judgement reply gives for its answer.
var whyRe = regexp.MustCompile(`^\s*WHY:\s*(\S.*)$`)

// directorDecides puts each item of q to the judgement model with w's goal
// as context. An answer to an item with choices must be one of them; any
// other is a decline. It returns the decided and declined items, and the
// decision an answer is recorded with: the director, the model and what it
// cost.
func (c *Cli) directorDecides(w core.Work, q runner.Question) ([]directorChoice, []directorChoice, core.Decision, error) {
	by := core.Decision{Source: core.AnsweredByDirector}
	p, err := c.project(w.Project)
	if err != nil {
		return nil, nil, by, err
	}
	goal, err := c.topGoal(w)
	if err != nil {
		return nil, nil, by, err
	}
	known, err := coordinator.Known(c.Ledger, w)
	if err != nil {
		return nil, nil, by, err
	}
	decided, declined := []directorChoice{}, []directorChoice{}
	for _, it := range q.Items {
		head, _, _ := strings.Cut(it.Question, "\n")
		choices := make([]string, len(it.Options))
		for i, o := range it.Options {
			choices[i] = o.Label
		}
		v, reply, err := c.judgeBrief(goal, p, judge.QuestionBrief(w, goal, p, it.Question, choices, known))
		if err != nil {
			return nil, nil, by, err
		}
		by.Runner, by.Model, by.Tokens = v.Runner, v.Model, by.Tokens+v.Tokens
		why := ""
		for _, text := range reply {
			for _, line := range strings.Split(text, "\n") {
				if m := whyRe.FindStringSubmatch(strings.Trim(strings.TrimSpace(line), "`")); m != nil {
					why = strings.TrimSpace(m[1])
				}
			}
		}
		answer := v.Answer
		if v.Answer != "" && v.Decline == "" && len(choices) > 0 {
			answer = ""
			for _, ch := range choices {
				if pickedFirst(v.Answer, ch) {
					answer = ch
				}
			}
			if answer == "" {
				v.Decline = fmt.Sprintf("answered %q, which is none of the choices", v.Answer)
			}
		}
		switch {
		case v.Decline != "":
			declined = append(declined, directorChoice{Question: head, Why: v.Decline})
		case answer == "":
			declined = append(declined, directorChoice{Question: head, Why: "the judgement model gave no answer"})
		default:
			decided = append(decided, directorChoice{Question: head, Answer: answer, Why: why})
		}
	}
	return decided, declined, by, nil
}

// directorDeclined leaves w's question open when the director declined any of
// it, recording each decline and saying why.
func (c *Cli) directorDeclined(w core.Work, decided, declined []directorChoice) error {
	lines := []string{}
	for _, d := range declined {
		said := fmt.Sprintf("the director declined %q: %s", d.Question, d.Why)
		if _, err := c.Ledger.AddEvent(w.ID, core.EventNote, said); err != nil {
			return err
		}
		lines = append(lines, said)
	}
	for _, d := range decided {
		lines = append(lines, fmt.Sprintf("the director would answer %q: %s, because %s", d.Question, d.Answer, d.Why))
	}
	lines = append(lines, "the question stays open for you: wd answer "+w.ID+" <answer>...")
	return c.out(map[string]any{"ok": true, "answered": false, "told": nil, "answer": nil, "applied": []string{}, "decided": decided, "declined": declined}, strings.Join(lines, "\n"))
}

// finishSetting is a project file rewritten to finish its goals at line.
type finishSetting struct {
	path string
	text string
	line core.FinishLine
}

// finishToRemember is the project file of w with its finish setting set to
// the finish line answers pick on filed's finish-line question; anything
// else is refused before the answer is recorded.
func (c *Cli) finishToRemember(w core.Work, filed *core.Event, answers []string) (*finishSetting, error) {
	if filed != nil {
		q := coordinator.AskedQuestion(*filed)
		for i, it := range q.Items {
			head, _, _ := strings.Cut(it.Question, "\n")
			if head != coordinator.FinishQuestion || len(answers) != len(q.Items) {
				continue
			}
			line, ok := coordinator.FinishChoice(answers[i])
			if !ok {
				return nil, fail("--remember keeps a finish line, and %q is not one", answers[i])
			}
			path := filepath.Join(c.ProjectsDir, w.Project+".md")
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			text, err := project.WithFinish(string(raw), line)
			if err != nil {
				return nil, fail("%s: %v; nothing was answered", path, err)
			}
			return &finishSetting{path: path, text: text, line: line}, nil
		}
	}
	return nil, fail("--remember keeps a finish-line choice, and %s is not asking where it finishes", w.ID)
}

// answered carries out what the person's answers to a question the director
// filed on w decide: a finish line chosen is stored, from the person; Apply
// carries out the plan changes the question holds; Change course sets the top
// goal as proposed. Each is recorded as a decision. It returns what it did
// and what it could not.
func (c *Cli) answered(w core.Work, e core.Event, answers []string) ([]string, error) {
	q := coordinator.AskedQuestion(e)
	if len(answers) != len(q.Items) {
		return nil, nil
	}
	did := []string{}
	for i, it := range q.Items {
		head, _, _ := strings.Cut(it.Question, "\n")
		first := len(it.Options) > 0 && pickedFirst(answers[i], it.Options[0].Label)
		switch {
		case head == coordinator.FinishQuestion:
			line, ok := coordinator.FinishChoice(answers[i])
			if !ok {
				continue
			}
			if err := c.Ledger.SetFinish(w.ID, line, core.FinishFromPerson); err != nil {
				return nil, err
			}
			did = append(did, "finishes at: "+string(line))
		case head == planChangeQuestion && e.Change != nil:
			if !first {
				if _, err := c.Ledger.AddEvent(w.ID, core.EventDecision, "kept the plan as it is"); err != nil {
					return nil, err
				}
				continue
			}
			p, err := c.project(w.Project)
			if err != nil {
				return nil, err
			}
			if _, err := c.Ledger.AddEvent(w.ID, core.EventDecision, "applied the plan change:\n"+strings.Join(e.Change.Actions, "\n")); err != nil {
				return nil, err
			}
			for _, line := range e.Change.Actions {
				acts, _ := coordinator.ParseSupervision([]string{"ACTIONS:", line})
				if len(acts) != 1 {
					did = append(did, line+" — not an action")
					continue
				}
				if err := c.applyAction(w, p, acts[0]); err != nil {
					did = append(did, line+" — "+err.Error())
					continue
				}
				did = append(did, line)
			}
		case head == courseQuestion && e.Change != nil && e.Change.Title != "":
			if !first {
				if _, err := c.Ledger.AddEvent(w.ID, core.EventDecision, "kept the goal as it is"); err != nil {
					return nil, err
				}
				continue
			}
			ch := e.Change
			if ch.Title != w.Title {
				if err := c.Ledger.SetTitle(w.ID, ch.Title); err != nil {
					return nil, err
				}
				if _, err := c.Ledger.AddEvent(w.ID, core.EventNote, renamedPrefix+w.Title); err != nil {
					return nil, err
				}
			}
			if err := c.Ledger.SetDoneWhen(w.ID, ch.DoneWhen, core.AnsweredByPerson); err != nil {
				return nil, err
			}
			if err := c.Ledger.SetFinish(w.ID, ch.Finish, core.FinishFromPerson); err != nil {
				return nil, err
			}
			decided := fmt.Sprintf("changed course: %s | done when: %s | finish: %s", ch.Title, ch.DoneWhen, ch.Finish)
			if _, err := c.Ledger.AddEvent(w.ID, core.EventDecision, decided); err != nil {
				return nil, err
			}
			did = append(did, decided)
		}
	}
	return did, nil
}

// pickedFirst reports whether answer picks choice, with or without its
// "(recommended)" mark.
func pickedFirst(answer, choice string) bool {
	bare := func(s string) string {
		return strings.ToLower(strings.TrimSpace(strings.Replace(s, "(recommended)", "", 1)))
	}
	return bare(answer) == bare(choice)
}
