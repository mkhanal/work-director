package cli

import (
	"fmt"
	"strings"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/runner"
)

// answer replies to the question work's session is waiting on. A question with
// options takes one answer per item, in the order asked; a question asked in
// words takes the words. The answer is recorded on the work and reaches the
// session the way any message does.
func (c *Cli) answer(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd answer <id> <answer> [<answer>...], one answer per question asked, in order")
	}
	w, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	h, err := c.handle(w.ID)
	if err != nil {
		return err
	}
	r, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return err
	}
	entries, err := r.Conversation(h)
	if err != nil {
		return err
	}
	var text, asked string
	var filed *core.Event
	if q, ok := runner.Asking(entries); ok {
		if text, err = runner.AnswerText(*q, rest[1:]); err != nil {
			return fail("%s: %v", w.ID, err)
		}
		asked = q.Text()
	} else if w.State == core.StateNeedsInput {
		text = strings.Join(rest[1:], "\n")
		events, err := c.Ledger.Events(w.ID, nil)
		if err != nil {
			return err
		}
		// A question the director filed takes one answer per item, or words.
		if e, ok := coordinator.OpenQuestion(events, w.ID); ok {
			filed = &e
			asked = e.Body
			if q := coordinator.AskedQuestion(e); len(rest) > 2 || len(q.Items) == 1 {
				if text, err = runner.AnswerText(q, rest[1:]); err != nil {
					return fail("%s: %v", w.ID, err)
				}
			}
		}
	} else {
		return fail("%s is not asking anything; tell it something with wd send %s \"<text>\"", w.ID, w.ID)
	}
	if _, err := c.Ledger.Answer(w.ID, core.Decision{Question: asked, Answer: text, Source: core.AnsweredByPerson}); err != nil {
		return err
	}
	applied := []string{}
	if filed != nil {
		if applied, err = c.answered(w, *filed, rest[1:]); err != nil {
			return err
		}
		// Carrying out a change can have moved the work on.
		if w, err = c.Ledger.Get(w.ID); err != nil {
			return err
		}
	}
	where, err := c.tellTask(w, text)
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
	return c.out(map[string]any{"ok": true, "told": where, "answer": text, "applied": applied}, human)
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
		case head == finishQuestion:
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
			if err := c.Ledger.SetDoneWhen(w.ID, ch.DoneWhen); err != nil {
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
