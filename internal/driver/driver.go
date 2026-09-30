// Package driver runs a goal's loop with nobody watching. It is what replaces
// supervision: instead of a person answering the questions an executor asks and
// deciding when to stop, the driver spends a bounded number of model judgements
// on the questions and then stops on a condition it can state.
//
// The stop condition is the whole point. Autonomy without one is a loop that
// runs until something is killed, which is not autonomy, it is an absence of
// attention. Every stop here is a fact the ledger already holds — no open task
// left, a bound spent, no movement across several turns — and the reason is
// recorded, so a run that ends without shipping says so in one line rather than
// going quiet.
package driver

import (
	"fmt"
	"strings"
	"time"

	"wd/internal/core"
	"wd/internal/ledger"
)

// Budget bounds one run. Every field is a stop, and every field exists because
// an unattended loop needs a way to stop that is not a person deciding to stop
// it. Zero means "not this bound", so a run is bounded by the fields that are
// set rather than by an invisible ceiling.
type Budget struct {
	// Turns bounds coordination passes. A turn is one look at every open task,
	// so this bounds how many times the loop asks "what changed".
	Turns int
	// Judgements bounds model calls made to answer executors' questions. This
	// is the one that most directly replaces a person, so it is the tightest
	// bound by default.
	Judgements int
	// Tokens bounds what the judgements may cost in total, across calls. A
	// per-call cost is already in each decision; this is the ceiling under the
	// sum, which is what a bill cares about.
	Tokens int
	// Stalled bounds turns with no movement at all. A task that is running and
	// never reports is stuck, and waiting on it forever is not patience.
	Stalled int
	// Deadline bounds wall-clock time, measured by the injected clock so a test
	// does not have to wait for one.
	Deadline time.Time
}

// Default is the budget a run gets when none is given: enough to be worth
// running, tight enough that it cannot run forever. The judgement bound is the
// one that matters most — it counts how often the loop is allowed to act in a
// person's place — and it is deliberately small, because a run that needs a
// hundred human decisions was never autonomous.
func Default() Budget {
	return Budget{Turns: 20, Judgements: 5, Tokens: 200_000, Stalled: 3}
}

// Stop is why a run ended. Every value is a fact about the ledger rather than a
// judgement about the work, which is what makes a stop condition a condition
// and not an opinion.
type Stop string

const (
	// StopComplete: nothing is open. Every task closed.
	StopComplete Stop = "complete"
	// StopBudget: a bound ran out with work still open.
	StopBudget Stop = "budget"
	// StopStalled: nothing moved for Stalled turns.
	StopStalled Stop = "stalled"
	// StopFailed: the loop could not run.
	StopFailed Stop = "failed"
)

// Shipped reports whether a stop means the goal's work reached done. Only
// StopComplete does; the rest are a goal ending without shipping, and the reason
// belongs on the goal rather than left to be inferred from silence.
func (s Stop) Shipped() bool { return s == StopComplete }

// Question is a question an executor asked that the ledger could not answer,
// paired with the work waiting on it — so the driver can judge the question
// without guessing which task it belongs to.
type Question struct {
	Work string `json:"work"`
	Text string `json:"question"`
}

// Turn is one coordination pass's report: what the pass itself answered, what
// it could not answer, and — when it could not run at all — why, which is the
// loop failing rather than the goal failing and is carried into the result
// rather than logged and dropped.
//
// A pass does not say what it closed, because a pass moves work between states
// rather than finishing it. The run knows what closed by watching the open set,
// so Closed is computed from the work rather than reported by the thing that
// cannot see it.
type Turn struct {
	Answered   []string   `json:"answered"`
	Escalated  []string   `json:"escalated"`
	Unanswered []Question `json:"unanswered"`
	// Failed is why the turn could not run. Empty is a turn that ran.
	Failed string `json:"failed"`
}

// Verdict is what a judge concluded, and who concluded it.
type Verdict struct {
	Answer string
	// Decline is what the model said it could not settle. It is a real answer
	// and not an error: the driver's job is to notice and stop, not to retry
	// harder on a question the model has already refused.
	Decline string
	Tokens  int
	Runner  string
	Model   string
}

// Decided reports whether the model answered rather than declining.
func (v Verdict) Decided() bool { return v.Answer != "" && v.Decline == "" }

// Result is one run, and why it stopped.
type Result struct {
	Goal string `json:"goal"`
	Stop Stop   `json:"stop"`
	// Why is the stop in words, the way a reader wants it: which bound ran
	// out, or that nothing moved.
	Why string `json:"why"`
	// Shipped says whether the goal's work reached done. It is false for every
	// stop but complete, and the caller ends the goal abandoned on a false.
	Shipped    bool `json:"shipped"`
	Turns      int  `json:"turns"`
	Judgements int  `json:"judgements"`
	Tokens     int  `json:"tokens"`
	// Answered and Unanswered are the questions this run settled and the ones it
	// could not. Unanswered is the honest list of where a person is still
	// needed, and it is empty only when the goal shipped.
	Answered   []string   `json:"answered"`
	Unanswered []Question `json:"unanswered"`
	// Closed is what the run's turns finished, and Open is what was still
	// running or waiting when the run stopped. Both are ids, and Closed is
	// computed from the open set rather than reported, so it cannot disagree
	// with the work.
	Closed []string `json:"closed"`
	Open   []string `json:"open"`
	// Unfinished is every task that has not come to rest, so a run that finds
	// nothing running can still tell a finished goal from an untouched one.
	Unfinished []string `json:"unfinished"`
}

// Judge answers one question, given the work that is settled. It returns the
// verdict and the runner and model that gave it, so the answer is recorded with
// its provenance rather than as anonymous truth.
type Judge func(question string, settled []core.Work) (Verdict, error)

// Driver runs one goal's loop. Everything it does that reaches out — the
// coordination pass, the judgement, recording the judgement, delivering the
// answer — is injected, so the thing that decides when to stop contains no I/O
// and can be reasoned about on its own.
type Driver struct {
	Ledger *ledger.Ledger
	// Coordinate runs one pass over the goal's open work. It reports what it
	// could not answer rather than stopping the loop itself: what to do about
	// an unanswered question is the driver's decision, not the pass's.
	Coordinate func(goal core.Work) (Turn, error)
	// Judge answers a question. A driver with no judge leaves every question
	// unanswered, which is the old supervised behaviour: it works, and it stops
	// at the first question.
	Judge Judge
	// Spend records a judgement as a structured decision on the work. It is
	// required whenever there is a judge, because an unrecorded decision is
	// one the review surface cannot show a reader.
	Spend func(work, question string, v Verdict) error
	// Send hands an answer to the executor waiting on it. Without it an answer
	// is decided and never delivered, which is worse than not deciding.
	Send func(work, answer string) error
	// Now is the clock, so a deadline is testable without waiting for one.
	Now func() time.Time
	// Poll is how long a turn waits before looking again. Zero polls as fast as
	// the ledger reads, which is right in a test and wrong in a run.
	Poll time.Duration
	// OnTurn is called after each turn, so a caller can print progress without
	// the driver knowing about printing.
	OnTurn func(turn int, t Turn, used Budget)
}

// Drive runs the goal's loop until a stop condition holds. It never asks
// whether to continue: that is the whole contract, and a loop that has to be
// asked whether to go on is a loop that is supervised.
func (d *Driver) Drive(goal core.Work, b Budget) (Result, error) {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	res := Result{Goal: goal.ID, Answered: []string{}, Unanswered: []Question{}, Open: []string{}}
	spent := Budget{}
	settled, err := d.settled()
	if err != nil {
		return res, err
	}
	lastFingerprint := ""
	stalled := 0
	// put is every question this run has already put to the model, so a
	// question is asked once and then waited on rather than asked again.
	put := map[string]bool{}
	// The open set is read once before the first turn so a run can say what it
	// finished, which is otherwise invisible: a pass reports states, not
	// endings.
	wasOpen, err := d.openTasks(goal.ID)
	if err != nil {
		return res, err
	}

	for turn := 1; ; turn++ {
		// Every stop is checked before a turn runs, so a budget of nothing stops
		// without spending one more pass first.
		if reason, stop := reached(b, spent, turn-1, stalled, lastFingerprint, now()); stop != "" {
			res.Stop, res.Why = stop, reason
			break
		}
		beforeJudgements := spent.Judgements
		t, used, err := d.turn(goal, b, spent, settled, put)
		spent = used
		if err != nil {
			return res, err
		}
		res.Turns = turn
		if t.Failed != "" {
			res.Stop, res.Why = StopFailed, t.Failed
			break
		}
		res.Judgements, res.Tokens = spent.Judgements, spent.Tokens
		res.Answered = append(res.Answered, t.Answered...)
		res.Unanswered = append(res.Unanswered, t.Unanswered...)
		if d.OnTurn != nil {
			d.OnTurn(turn, t, spent)
		}

		open, err := d.openTasks(goal.ID)
		if err != nil {
			return res, err
		}
		res.Closed = append(res.Closed, left(wasOpen, open)...)
		wasOpen = open
		res.Open = open
		unfinished, err := d.unfinished(goal.ID)
		if err != nil {
			return res, err
		}
		res.Unfinished = unfinished
		if len(unfinished) == 0 {
			res.Stop, res.Why = StopComplete, "every task closed"
			break
		}
		// Movement is measured on the work itself, not on what a turn reported.
		// A turn that spent a judgement moved the run even when no task changed
		// state, and a turn that reports activity without changing anything has
		// not moved it.
		fp, err := d.fingerprint(goal.ID)
		if err != nil {
			return res, err
		}
		if fp == lastFingerprint && spent.Judgements == beforeJudgements {
			stalled++
		} else {
			stalled = 0
		}
		lastFingerprint = fp
		if d.Poll > 0 {
			time.Sleep(d.Poll)
		}
	}
	res.Shipped = res.Stop.Shipped()
	res.Answered, res.Unanswered = unique(res.Answered), uniqueQuestions(res.Unanswered)
	return res, nil
}

// turn is one coordination pass, then judgement on whatever it could not
// answer, bounded by what is left of the budget. It returns the turn and the
// spend after it, so the budget is always what actually happened rather than
// what the driver meant to do.
func (d *Driver) turn(goal core.Work, b, spent Budget, settled []core.Work, put map[string]bool) (Turn, Budget, error) {
	var t Turn
	if d.Coordinate == nil {
		t.Failed = "no coordination pass wired"
		return t, spent, nil
	}
	pass, err := d.Coordinate(goal)
	if err != nil {
		t.Failed = "coordination pass: " + err.Error()
		return t, spent, nil
	}
	t.Answered, t.Escalated = pass.Answered, pass.Escalated
	if len(pass.Unanswered) == 0 {
		return t, spent, nil
	}
	if d.Judge == nil || d.Send == nil {
		// With no judge, every question stays unanswered. That is the old
		// behaviour and it is honest: the loop stops at the first question and
		// says which question, rather than pretending to have settled it.
		t.Unanswered = pass.Unanswered
		return t, spent, nil
	}
	for _, q := range pass.Unanswered {
		if put[q.Text] {
			// Already put to the model this run. A pass reports the question
			// again every turn because the executor is still asking it, and
			// asking again spends the budget on a question the model has
			// already answered or refused. It stays in the bill either way: a
			// person may still settle it, and until they do the work is
			// waiting.
			t.Unanswered = append(t.Unanswered, q)
			continue
		}
		put[q.Text] = true
		if b.Judgements > 0 && spent.Judgements >= b.Judgements {
			// Out of judgement. The remaining questions are not dropped: they
			// become the list of where a person is still needed, which is the
			// run's honest bill.
			t.Unanswered = append(t.Unanswered, q)
			continue
		}
		if b.Tokens > 0 && spent.Tokens >= b.Tokens {
			t.Unanswered = append(t.Unanswered, q)
			continue
		}
		v, err := d.Judge(q.Text, settled)
		if err != nil {
			// A judge that could not run is not a decline: it is a failure, and
			// it is reported as one rather than quietly counted as an answer.
			t.Failed = fmt.Sprintf("judging %s: %v", q.Work, err)
			return t, spent, nil
		}
		spent.Judgements++
		spent.Tokens += v.Tokens
		if d.Spend != nil {
			if err := d.Spend(q.Work, q.Text, v); err != nil {
				t.Failed = "recording a judgement: " + err.Error()
				return t, spent, nil
			}
		}
		if !v.Decided() {
			// A decline is a real answer and it counts. Retrying a question a
			// model has declined to settle spends the run's budget on a
			// question it has already said it cannot answer.
			t.Unanswered = append(t.Unanswered, q)
			continue
		}
		if err := d.Send(q.Work, v.Answer); err != nil {
			t.Failed = fmt.Sprintf("sending an answer to %s: %v", q.Work, err)
			return t, spent, nil
		}
		t.Answered = append(t.Answered, q.Work)
	}
	return t, spent, nil
}

// reached is the stop condition in one place: given the budget, what has been
// spent and what has happened, it returns the reason and the stop, or an empty
// stop to keep going. Every bound is a fact and each says which fact ran out,
// because "it stopped" is not something a person can act on.
func reached(b, spent Budget, turnsDone, stalled int, lastFingerprint string, now time.Time) (string, Stop) {
	if !b.Deadline.IsZero() && now.After(b.Deadline) {
		return fmt.Sprintf("the deadline passed at %s", b.Deadline.Format(time.RFC3339)), StopBudget
	}
	if b.Turns > 0 && turnsDone >= b.Turns {
		return fmt.Sprintf("%d turns is the bound set for this run", b.Turns), StopBudget
	}
	if b.Judgements > 0 && spent.Judgements >= b.Judgements {
		return fmt.Sprintf("%d judgements is the bound set for how often this run may act in a person's place", b.Judgements), StopBudget
	}
	if b.Tokens > 0 && spent.Tokens >= b.Tokens {
		return fmt.Sprintf("%d tokens is the bound set for what this run may spend deciding", b.Tokens), StopBudget
	}
	if b.Stalled > 0 && stalled >= b.Stalled && lastFingerprint != "" {
		return fmt.Sprintf("nothing moved for %d turns", b.Stalled), StopStalled
	}
	return "", ""
}

// settled is the work the judgements choose between: everything open, with its
// state and detail, because a judgement is supposed to be made from what the
// project has already settled rather than from a model's imagination.
func (d *Driver) settled() ([]core.Work, error) {
	all, err := d.Ledger.List(ledger.ListFilter{})
	if err != nil {
		return nil, err
	}
	out := []core.Work{}
	for _, w := range all {
		if w.State == core.StateDone || w.State == core.StateDropped || w.State == core.StateAbandoned {
			continue
		}
		out = append(out, w)
	}
	return out, nil
}

// unfinished is every task that has not come to rest. This is what "complete"
// means: not "nothing is running" but "nothing is left". A goal whose tasks are
// all still queued has not been driven, and reporting it as complete because
// nothing happened to be running would claim a run that never ran.
func (d *Driver) unfinished(goal string) ([]string, error) {
	tasks, err := d.Ledger.Tasks(goal)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, t := range tasks {
		switch t.State {
		case core.StateDone, core.StateDropped, core.StateAbandoned:
		default:
			out = append(out, t.ID)
		}
	}
	return out, nil
}

func (d *Driver) openTasks(goal string) ([]string, error) {
	tasks, err := d.Ledger.Tasks(goal)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, t := range tasks {
		if t.State == core.StateRunning || t.State == core.StateNeedsInput {
			out = append(out, t.ID)
		}
	}
	return out, nil
}

// fingerprint is what a turn changed: every open task's id, state and last
// activity. Comparing it is how the loop knows something moved, without storing
// a counter somewhere that could disagree with the work.
func (d *Driver) fingerprint(goal string) (string, error) {
	tasks, err := d.Ledger.Tasks(goal)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(tasks))
	for _, t := range tasks {
		parts = append(parts, fmt.Sprintf("%s:%s:%s", t.ID, t.State, t.Updated))
	}
	return strings.Join(parts, "|"), nil
}

// left is what was open before and is not open now. It is how the run knows
// what it finished without the pass having to know, which it cannot: a pass
// moves work between states, and only the ledger sees a task come to rest.
func left(wasOpen, open []string) []string {
	still := map[string]bool{}
	for _, id := range open {
		still[id] = true
	}
	out := []string{}
	for _, id := range wasOpen {
		if !still[id] {
			out = append(out, id)
		}
	}
	return out
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func uniqueQuestions(in []Question) []Question {
	seen := map[string]bool{}
	out := []Question{}
	for _, q := range in {
		key := q.Work + "\x00" + q.Text
		if q.Text != "" && !seen[key] {
			seen[key] = true
			out = append(out, q)
		}
	}
	return out
}
