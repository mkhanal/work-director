package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/driver"
	"wd/internal/judge"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
)

// judgeTimeout bounds one judgement call. It is one bounded question, so it
// gets a short deadline: a model that never answers leaves no verdict, which
// stops the run the same way a decline does and is a safer failure than
// waiting.
const judgeTimeout = 120 * time.Second

// drive is the default poll between turns. Long enough not to hammer the
// ledger or the runners, short enough that a run notices an executor finishing
// while a person is still watching it.
const drivePoll = 3 * time.Second

// Drive runs a goal's loop with nobody watching. It is the whole replacement
// for supervision: each turn coordinates the goal's open work, spends a bounded
// number of model judgements on the questions the ledger could not answer, and
// stops on a condition it can state.
//
// A run that stopped with work open ends the goal abandoned, and says why in
// the reason. That is the stop condition doing its job rather than a verdict on
// the work: a goal that ran out of budget did not ship, and leaving it open
// would say otherwise.
func (c *Cli) drive(rest []string) error {
	if len(rest) == 0 {
		return fail("which goal: wd drive <goal-id>")
	}
	// The bounds are read first, so a command that cannot be understood is
	// reported as such whatever state the goal is in.
	b, err := driveBudget(c.Args)
	if err != nil {
		return err
	}
	goal, err := c.Ledger.Get(rest[0])
	if err != nil {
		return err
	}
	if !core.IsGoal(goal.Kind) {
		return fail("work %s is a %s, not a goal: only a goal has a loop to run", goal.ID, goal.Kind)
	}
	switch goal.State {
	case core.StateDone, core.StateDropped, core.StateAbandoned:
		// A goal that has come to rest has no loop to run, and running one
		// anyway would report a run that never happened as a run that finished.
		return fail("goal %s is %s: there is nothing left to run", goal.ID, goal.State)
	}
	p, err := c.project(goal.Project)
	if err != nil {
		return err
	}
	d := &driver.Driver{
		Ledger:     c.Ledger,
		Coordinate: c.driveTurn(goal, p),
		Judge:      c.judgeOnce(goal, p),
		Spend:      c.spendJudgement,
		Send:       c.sendAnswer(p),
		Poll:       time.Duration(intOr(c.Args, "poll-seconds", int(drivePoll/time.Second))) * time.Second,
		OnTurn:     c.turnLine,
	}
	res, err := d.Drive(goal, b)
	if err != nil {
		return err
	}
	// A run that stopped because a bound ran out or nothing moved did not ship,
	// and the goal comes to rest saying so. A run that stopped because the loop
	// itself could not run is a different thing: the work is untouched, and
	// ending a goal because a runner could not read a transcript would throw away
	// real work over a failure that says nothing about it. That one is reported.
	if res.Stop == driver.StopBudget || res.Stop == driver.StopStalled {
		if err := c.endUnshipped(goal, res); err != nil {
			return err
		}
	}
	return c.printDrive(goal, res)
}

// driveBudget reads the run's bounds. Every one that is left unset is not a
// bound, and the defaults are small on purpose: a run that needs a hundred
// human decisions was never autonomous, and a bound nobody set is a bound
// nobody can reason about.
func driveBudget(a Args) (driver.Budget, error) {
	b := driver.Default()
	for _, f := range []struct {
		flag string
		dst  *int
	}{
		{"turns", &b.Turns},
		{"judgements", &b.Judgements},
		{"tokens", &b.Tokens},
		{"stalled", &b.Stalled},
	} {
		v, err := intFlag(a, f.flag, f.dst)
		if err != nil {
			return b, err
		}
		*f.dst = *v
	}
	if s := str(a, "deadline"); s != nil {
		d, err := time.ParseDuration(*s)
		if err != nil {
			return b, fail("--deadline %q is not a duration", *s)
		}
		b.Deadline = time.Now().Add(d)
	}
	return b, nil
}

// driveTurn is one coordination pass, plus the questions it could not answer.
// The question is read back from the ledger rather than returned by the pass,
// because the pass filed it there and the ledger is where it lives from then
// on: a question nobody recorded is a question that will be asked again.
func (c *Cli) driveTurn(goal core.Work, p *project.Project) func(core.Work) (driver.Turn, error) {
	return func(core.Work) (driver.Turn, error) {
		res, err := coordinator.CoordinateOnce(goal, p, c.Ledger, detectedRunner)
		if err != nil {
			return driver.Turn{}, err
		}
		t := driver.Turn{Answered: res.Answered, Escalated: res.Escalated}
		for _, id := range res.Escalated {
			q, err := openQuestion(c.Ledger, id)
			if err != nil {
				return t, err
			}
			if q == "" {
				// Escalated with nothing to judge is a state the loop cannot
				// act on and must not pretend to have settled. It stays
				// escalated and the run stops on its own bound.
				continue
			}
			t.Unanswered = append(t.Unanswered, driver.Question{Work: id, Text: q})
		}
		return t, nil
	}
}

// openQuestion is the question a work item is waiting on: the last question it
// asked, or the report that stopped it. A report is read as a question because
// NEEDS-INPUT means exactly that — an executor saying it cannot go on without an
// answer — and reading it as anything else would let the loop answer a
// transcript instead of a question.
func openQuestion(l *ledger.Ledger, id string) (string, error) {
	evs, err := l.Events(id, nil)
	if err != nil {
		return "", err
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == core.EventQuestion {
			return evs[i].Body, nil
		}
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == core.EventReport && strings.HasPrefix(evs[i].Body, "NEEDS-INPUT") {
			return evs[i].Body, nil
		}
	}
	return "", nil
}

// judgeOnce is one bounded model call. It runs where the work runs and on the
// project's model, so a judgement is made by something that can read the code
// the question is about.
func (c *Cli) judgeOnce(goal core.Work, p *project.Project) driver.Judge {
	return func(question string, settled []core.Work) (driver.Verdict, error) {
		brief := judge.Brief(goal, p, question, settled)
		runnerName, cwd, err := coordinator.Where(c.Ledger, goal, p)
		if err != nil {
			return driver.Verdict{}, err
		}
		rn, err := detectedRunner(runnerName)
		if err != nil {
			return driver.Verdict{}, err
		}
		h, err := rn.Spawn(runner.SpawnOptions{
			Cwd:   cwd,
			Name:  slice60(fmt.Sprintf("wd-%s judge", goal.ID)),
			Brief: brief,
			Model: p.Model,
		})
		if err != nil {
			return driver.Verdict{}, err
		}
		v := awaitJudgement(rn, h)
		return driver.Verdict{
			Answer:  v.Answer,
			Decline: v.Decline,
			Tokens:  v.Tokens,
			Runner:  rn.Name(),
			Model:   strOrEmpty(p.Model),
		}, nil
	}
}

// awaitJudgement polls until the reply carries a verdict or the session is
// plainly finished without one, which is a decline rather than a slow answer.
func awaitJudgement(rn runner.Runner, h runner.Handle) judge.Verdict {
	deadline := time.Now().Add(judgeTimeout)
	for time.Now().Before(deadline) {
		out, err := rn.Transcript(h)
		if err != nil {
			return judge.Verdict{}
		}
		if v := judge.Parse(out); v.Decided() || v.Decline != "" {
			return v
		}
		if len(out) > 0 {
			if st, err := rn.Status(h); err == nil && st != runner.StatusRunning && st != runner.StatusWaiting {
				return judge.Parse(out)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return judge.Verdict{}
}

// spendJudgement records what the model said, with what it cost and who said
// it, before the answer reaches the executor. An answer nobody recorded is a
// decision the review surface cannot show a reader, and a loop that cannot be
// audited is a loop nobody can supervise.
func (c *Cli) spendJudgement(work, question string, v driver.Verdict) error {
	jv := judge.Verdict{Answer: v.Answer, Decline: v.Decline, Tokens: v.Tokens}
	body := judge.Event(question, jv, v.Runner, v.Model)
	d, ok := judge.Decision(question, jv, v.Runner, v.Model)
	if !ok {
		// A decline is not a decision and the ledger must never gain one. It
		// is still recorded, marked as a decline, because a run that could not
		// settle a question is exactly what a later reader needs to see.
		_, err := c.Ledger.AddEvent(work, core.EventNote, body)
		return err
	}
	_, err := c.Ledger.Decide(work, body, d, nil)
	return err
}

// sendAnswer hands an answer to the executor waiting on it and puts it back
// to work, which is what makes a judgement worth anything: a settled question
// that never reaches the executor is a decision that changed nothing.
func (c *Cli) sendAnswer(p *project.Project) func(work, answer string) error {
	return func(work, answer string) error {
		w, err := c.Ledger.Get(work)
		if err != nil {
			return err
		}
		h, ok, err := coordinator.Handle(c.Ledger, w, p)
		if err != nil {
			return err
		}
		if !ok {
			return fail("work %s has no session to answer: the answer is recorded but nobody is waiting", w.ID)
		}
		r, err := detectedRunner(h.Runner)
		if err != nil {
			return err
		}
		if err := coordinator.Send(c.Ledger, w.ID, r, h, answer); err != nil {
			return err
		}
		if _, err := c.Ledger.AddEvent(w.ID, core.EventAnswer, answer); err != nil {
			return err
		}
		if w.State == core.StateNeedsInput {
			_, err := c.Ledger.Transition(w.ID, core.StateRunning)
			return err
		}
		return nil
	}
}

// endUnshipped ends a goal whose run stopped with work open, and the tasks
// that were still open with it. A task left running under a goal that has come
// to rest would claim work is in progress when nothing is driving it, and the
// board is read as a statement about the world.
func (c *Cli) endUnshipped(goal core.Work, res driver.Result) error {
	detail := fmt.Sprintf("stopped on %s: %s", res.Stop, res.Why)
	// The work that was left is named in the reason, so the goal and the tasks
	// under it cannot be read as telling different stories about one run. A
	// question nobody settled is named too, because a run that stopped on a
	// question a person could have answered is a different fact from one that
	// stopped on a bound, and the reason is where a reader looks for it.
	if len(res.Unanswered) > 0 {
		asked := make([]string, 0, len(res.Unanswered))
		for _, q := range res.Unanswered {
			asked = append(asked, q.Work)
		}
		detail += "; unsettled: " + strings.Join(asked, ", ")
	}
	if len(res.Unfinished) > 0 {
		detail += "; unfinished: " + strings.Join(res.Unfinished, ", ")
	}
	// Every open task ends for the same reason and with the same words, so the
	// goal and the work under it cannot be read as telling different stories
	// about one run.
	for _, id := range res.Open {
		if _, err := c.Ledger.Abandon(id, core.AbandonNoPR, detail); err != nil {
			return err
		}
	}
	_, err := c.Ledger.Abandon(goal.ID, core.AbandonNoPR, detail)
	return err
}

// turnLine prints what a turn did, so a run watched at a distance says
// something as it goes rather than only at the end.
func (c *Cli) turnLine(turn int, t driver.Turn, used driver.Budget) {
	if c.JSON {
		return
	}
	bits := []string{fmt.Sprintf("turn %d", turn)}
	if n := len(t.Answered); n > 0 {
		bits = append(bits, fmt.Sprintf("answered %d", n))
	}
	if n := len(t.Unanswered); n > 0 {
		bits = append(bits, fmt.Sprintf("unsettled %d", n))
	}
	if n := len(t.Escalated); n > 0 {
		bits = append(bits, fmt.Sprintf("escalated %d", n))
	}
	if used.Judgements > 0 {
		bits = append(bits, fmt.Sprintf("judged %d", used.Judgements))
	}
	fmt.Fprintln(c.Stderr, strings.Join(bits, " · "))
}

func (c *Cli) printDrive(goal core.Work, res driver.Result) error {
	out := []string{fmt.Sprintf("%s %s", goal.ID, goal.Title)}
	if res.Shipped {
		out = append(out, fmt.Sprintf("shipped in %d turn(s), %d judgement(s), %d tokens", res.Turns, res.Judgements, res.Tokens))
	} else {
		out = append(out, fmt.Sprintf("stopped: %s — %s", res.Stop, res.Why))
		out = append(out, fmt.Sprintf("%d turn(s), %d judgement(s), %d tokens", res.Turns, res.Judgements, res.Tokens))
		if res.Stop == driver.StopFailed {
			// The loop failed, not the work: the goal is exactly where it was,
			// and saying "abandoned" here would be a lie about it.
			out = append(out, "the goal is untouched: the loop could not run, not the work")
		}
		if len(res.Answered) > 0 {
			out = append(out, "settled: "+strings.Join(res.Answered, ", "))
		}
		if len(res.Unanswered) > 0 {
			asked := make([]string, 0, len(res.Unanswered))
			for _, q := range res.Unanswered {
				asked = append(asked, q.Work+": "+clipLine(q.Text, 90))
			}
			out = append(out, "still needing a person:")
			for _, a := range asked {
				out = append(out, "  "+a)
			}
		}
		if len(res.Closed) > 0 {
			out = append(out, "finished: "+strings.Join(res.Closed, ", "))
		}
		if res.Stop != driver.StopFailed {
			out = append(out, "the goal is abandoned: it did not ship")
		}
	}
	return c.out(res, strings.Join(out, "\n"))
}

func clipLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}

// detectedRunner is the resolver a coordination pass needs, narrowed to the
// ledger's own set of runners.
func detectedRunner(name string) (runner.Runner, error) {
	n, err := runner.DetectedRunner(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strconv.Quote(name))
	}
	return n, nil
}
