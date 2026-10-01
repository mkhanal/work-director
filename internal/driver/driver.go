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
	// Closed is the work this turn moved the rest of the way, and Blocked is the
	// work that was ready and stopped at a gate, naming which. Neither is a
	// judgement: both are the ledger's own answer about what it would not pass.
	Closed  []string          `json:"closed"`
	Blocked map[string]string `json:"blocked"`
	// Promoted is the taste card this turn made global, and Held is the one it
	// judged should stay where it is. Taste is the loudest thing a loop changes
	// and it is judged like everything else, so a run says what it changed about
	// what the loop believes rather than leaving that to be noticed later.
	Promoted []string `json:"promoted"`
	Held     []string `json:"held"`
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
	// Shipped says whether the goal's work landed: every task done, not merely
	// at rest. A goal with a dropped or abandoned task has come to rest without
	// shipping, and calling that shipped would let the caller close a goal whose
	// work is on the floor.
	Shipped bool `json:"shipped"`
	// Unlanded is the task that stopped a run that came to rest without
	// shipping, or nil when every task is done.
	Unlanded []string `json:"unlanded"`
	// Blocked is the work a turn found ready to close and could not close, each
	// naming the gate that stopped it. It is the honest bill of a run that did
	// everything it could: a gate that refuses is not a failure to hide, it is
	// the one thing a person is still needed for.
	Blocked map[string]string `json:"blocked"`
	// Promoted is the taste card this run made global, and Held is the one it
	// judged should stay where it is. What the loop believes about itself
	// changed during the run, and a run that does not say so leaves the biggest
	// thing it did to be found later by somebody diffing the taste.
	Promoted   []string `json:"promoted"`
	Held       []string `json:"held"`
	Turns      int      `json:"turns"`
	Judgements int      `json:"judgements"`
	Tokens     int      `json:"tokens"`
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

// Closing is what one turn did with the work that had finished. Closed is the
// work it moved the rest of the way, and Blocked is the work that is ready and
// stopped at a gate, naming which one. A gate the loop cannot pass is not a
// failure of the run: it is the honest list of what is left, and the loop that
// hid it would be reporting a clean run over work it never closed.
type Closing struct {
	Closed  []string          `json:"closed"`
	Blocked map[string]string `json:"blocked"`
}

// Close puts the goal's finished work through the gates that are left, once each.
// It is a separate step from the coordination pass because the pass reads what
// the executors said while this acts on it, and a pass that also closed work
// would be doing two things whose failures mean different things: an executor
// that has not spoken yet, and a gate that has been tried and refused.
type Close func(goal core.Work) (Closing, error)

// Candidate is a rule card waiting to be judged for the global taste, and what
// to do with the answer. The judgement is the driver's, not the step's: taste is
// the loudest thing the loop can change, so it is asked and recorded through the
// same budget and the same record as any other question, and a step that asked
// on its own would be a second, quieter budget.
type Candidate struct {
	// Work is the work the decision is recorded on: the one whose evidence put
	// the card forward. A decision needs somewhere to live so a reader can find
	// it, and the work that showed the pattern is the place a reader looks.
	Work string
	// Asked is the brief put to the model and Recorded is the one line that
	// goes on the ledger. They differ because a brief is a page of instruction
	// and a claim is a line a reader scans: recording the brief and truncating
	// it would leave the card unnamed on the very record that promoted it.
	Asked    string
	Recorded string
	// Apply puts the verdict into effect and names the card it acted on, or ""
	// when it did nothing. A decline is an answer, so Apply is called with one.
	Apply func(v Verdict) (string, error)
}

// Candidates is what is waiting to be judged about the taste.
type Candidates func() ([]Candidate, error)

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
	// TasteJudge answers a question about the taste, which is a different
	// question with a different brief, and it is a separate function so neither
	// can borrow the other's words. A driver with none judges its work and
	// leaves its taste alone, which is the smaller mistake of the two.
	TasteJudge Judge
	// Spend records a judgement as a structured decision on the work. It is
	// required whenever there is a judge, because an unrecorded decision is
	// one the review surface cannot show a reader.
	Spend func(work, question string, v Verdict) error
	// Send hands an answer to the executor waiting on it. Without it an answer
	// is decided and never delivered, which is worse than not deciding.
	Send func(work, answer string) error
	// Close puts work that has finished through the gates that are left. Without
	// it a task that reported DONE sits in review for ever, and the run that
	// watched it get there calls a goal finished while its work is unfinished.
	Close Close
	// Candidates reports the taste cards waiting to be judged. It is asked of
	// the driver's own judge rather than of its own, and it is nil-safe: a
	// driver that does not judge taste simply does not.
	Candidates Candidates
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
	res := Result{Goal: goal.ID, Answered: []string{}, Unanswered: []Question{}, Open: []string{}, Blocked: map[string]string{}}
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
		// A gate that refused is kept, not dropped on the next turn: it is the
		// same fact every turn, and the run ends holding it.
		for id, gate := range t.Blocked {
			if res.Blocked == nil {
				res.Blocked = map[string]string{}
			}
			res.Blocked[id] = gate
		}
		res.Promoted = append(res.Promoted, t.Promoted...)
		res.Held = append(res.Held, t.Held...)
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
			res.Stop, res.Why = StopComplete, "every task at rest"
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
	// Landed is asked of the ledger rather than of the run, because the run
	// watched tasks leave the open set and cannot tell a task that finished from
	// one that was dropped. A goal with a dropped piece came to rest without
	// shipping, and only the task's own state says which.
	unlanded, err := d.unlanded(goal.ID)
	if err != nil {
		return res, err
	}
	res.Unlanded = unlanded
	if len(unlanded) > 0 {
		res.Shipped = false
	}
	return res, nil
}

// unlanded is every task that came to rest without landing: dropped, or
// abandoned. Both are endings rather than successes, and a goal with one has not
// shipped, so the caller must not close it as though it had.
func (d *Driver) unlanded(goal string) ([]string, error) {
	tasks, err := d.Ledger.Tasks(goal)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, t := range tasks {
		if t.State == core.StateDropped || t.State == core.StateAbandoned {
			out = append(out, t.ID)
		}
	}
	return out, nil
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
	// Closing runs whatever the pass asked about, because the common case is a
	// task reporting DONE with no question at all, and a loop that only closed
	// work while somebody was asking it something would sit on finished work for
	// ever.
	if d.Close != nil {
		c, err := d.Close(goal)
		if err != nil {
			t.Failed = "closing finished work: " + err.Error()
			return t, spent, nil
		}
		t.Closed, t.Blocked = c.Closed, c.Blocked
	}
	if len(pass.Unanswered) > 0 && (d.Judge == nil || d.Send == nil) {
		// With no judge, every question stays unanswered. That is the old
		// behaviour and it is honest: the loop stops at the first question and
		// says which question, rather than pretending to have settled it.
		t.Unanswered = pass.Unanswered
	}
	for _, q := range pass.Unanswered {
		if d.Judge == nil || d.Send == nil {
			break
		}
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
		var v Verdict
		var asked bool
		var why string
		// Assigned rather than declared: a := here would shadow the run's spend
		// inside the loop body and the budget would stop being the budget.
		v, spent, asked, why = d.putToModel(d.Judge, q.Work, q.Text, q.Text, b, spent, settled)
		if why != "" {
			t.Failed = why
			return t, spent, nil
		}
		if !asked || !v.Decided() {
			// A decline is a real answer and it counts. Retrying a question a
			// model has declined to settle spends the run's budget on a
			// question it has already said it cannot answer. So is a question
			// past the bound: it is not dropped, it becomes the list of where a
			// person is still needed, which is the run's honest bill.
			t.Unanswered = append(t.Unanswered, q)
			continue
		}
		if err := d.Send(q.Work, v.Answer); err != nil {
			t.Failed = fmt.Sprintf("sending an answer to %s: %v", q.Work, err)
			return t, spent, nil
		}
		t.Answered = append(t.Answered, q.Work)
	}
	// Taste last. An executor's question is somebody waiting to work; a card
	// that could be promoted one turn later is nobody waiting. So the judgement
	// budget is spent on the work first and taste gets what is left, which is
	// why a run with a small judgement bound promotes nothing rather than
	// answering a stranger and leaving its own work stuck.
	ft, spent, err := d.taste(b, spent, settled, put)
	ft.Answered, ft.Unanswered, ft.Escalated = t.Answered, t.Unanswered, t.Escalated
	ft.Closed, ft.Blocked = t.Closed, t.Blocked
	if ft.Failed != "" {
		return ft, spent, nil
	}
	return ft, spent, nil
}

// putToModel asks the judge one question when there is budget for it, and records
// the verdict before anything acts on it. It returns whether it was asked: a
// question past the bound is not asked and not asking is not a decline, so the
// caller decides what an unasked question means for its own kind of work.
func (d *Driver) putToModel(judge Judge, work, asked, recorded string, b, spent Budget, settled []core.Work) (Verdict, Budget, bool, string) {
	if judge == nil {
		return Verdict{}, spent, false, ""
	}
	if b.Judgements > 0 && spent.Judgements >= b.Judgements {
		return Verdict{}, spent, false, ""
	}
	if b.Tokens > 0 && spent.Tokens >= b.Tokens {
		return Verdict{}, spent, false, ""
	}
	v, err := judge(asked, settled)
	if err != nil {
		// A judge that could not run is not a decline: it is a failure, and it
		// is reported as one rather than quietly counted as an answer.
		return Verdict{}, spent, false, fmt.Sprintf("judging %s: %v", work, err)
	}
	spent.Judgements++
	spent.Tokens += v.Tokens
	if d.Spend != nil {
		if err := d.Spend(work, recorded, v); err != nil {
			return v, spent, false, "recording a judgement: " + err.Error()
		}
	}
	return v, spent, true, ""
}

// taste is the loop's own standing question: which rule cards, if any, are worth
// making global. One card a turn, each asked once, through the same judge, the
// same bound and the same record as an executor's question — because promoting
// a card changes what every future session believes, and a change that loud
// deserves the same scrutiny as a decision delivered to a running task, not
// less.
func (d *Driver) taste(b, spent Budget, settled []core.Work, put map[string]bool) (Turn, Budget, error) {
	var t Turn
	if d.Candidates == nil || d.TasteJudge == nil {
		return t, spent, nil
	}
	cands, err := d.Candidates()
	if err != nil {
		t.Failed = "looking for taste to judge: " + err.Error()
		return t, spent, nil
	}
	for _, cand := range cands {
		if cand.Apply == nil {
			continue
		}
		if put[cand.Recorded] {
			// Already put to the model this run. A card is a candidate until it
			// is promoted, so without this the loop would ask about the same
			// card every turn and reach the same answer every turn, spending
			// the budget to do it. A later run asks again, because more evidence
			// may have arrived and that is a different question.
			continue
		}
		put[cand.Recorded] = true
		var v Verdict
		var asked bool
		var why string
		v, spent, asked, why = d.putToModel(d.TasteJudge, cand.Work, cand.Asked, cand.Recorded, b, spent, settled)
		if why != "" {
			t.Failed = why
			return t, spent, nil
		}
		if !asked {
			// Out of budget. The card stays a candidate, which is exactly where
			// it already was, and a run with budget can judge it.
			return t, spent, nil
		}
		card, err := cand.Apply(v)
		if err != nil {
			t.Failed = "promoting taste: " + err.Error()
			return t, spent, nil
		}
		if card == "" {
			continue
		}
		if v.Decided() {
			t.Promoted = append(t.Promoted, card)
		} else {
			// A decline is an answer, so the card is held and the run says so
			// rather than leaving a reader to wonder whether the loop looked.
			t.Held = append(t.Held, card)
		}
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
