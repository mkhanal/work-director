package cli

import (
	"fmt"
	"strings"

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
	var text string
	if q, ok := runner.Asking(entries); ok {
		if text, err = runner.AnswerText(*q, rest[1:]); err != nil {
			return fail("%s: %v", w.ID, err)
		}
	} else if w.State == core.StateNeedsInput {
		text = strings.Join(rest[1:], "\n")
	} else {
		return fail("%s is not asking anything; tell it something with wd send %s \"<text>\"", w.ID, w.ID)
	}
	if _, err := c.Ledger.AddEvent(w.ID, core.EventAnswer, text); err != nil {
		return err
	}
	where, err := c.tellTask(w, text)
	if err != nil {
		return err
	}
	human := "answered"
	if where == toldQueued {
		human = fmt.Sprintf("answer queued: %s reads it when it is ready", w.ID)
	}
	return c.out(map[string]any{"ok": true, "told": where, "answer": text}, human)
}
