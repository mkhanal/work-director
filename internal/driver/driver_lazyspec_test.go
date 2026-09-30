package driver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wd/internal/core"
	"wd/internal/ledger"
)

type harness struct {
	l     *ledger.Ledger
	goal  core.Work
	tasks []core.Work
	d     *Driver
	turns int
	// answer maps a question to the verdict the judge gives, so a test can say
	// which questions the model settles and which it refuses.
	answer   map[string]Verdict
	judged   []string
	spent    []string
	sent     map[string]string
	unanswer []Question
	coordErr error
	closed   int
	// pass substitutes the whole coordination pass, for the tests that are
	// about turns rather than about questions.
	pass func(turn int) (Turn, error)
	// failed records why a substitution could not do what the test asked, so
	// the test fails on the harness rather than on an unexplained stop.
	failed string
}

func newHarness(t *testing.T, tasks int) *harness {
	t.Helper()
	l, err := ledger.New(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	goal, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatalf("add goal: %v", err)
	}
	h := &harness{l: l, goal: goal, answer: map[string]Verdict{}, sent: map[string]string{}}
	for i := range tasks {
		w, err := l.Add("p", fmt.Sprintf("Task %d", i), ledger.AddOptions{Parent: &goal.ID})
		if err != nil {
			t.Fatalf("add task: %v", err)
		}
		if _, err := l.Transition(w.ID, core.StateRunning); err != nil {
			t.Fatalf("run task: %v", err)
		}
		h.tasks = append(h.tasks, w)
	}
	h.d = &Driver{
		Ledger: l,
		Now:    func() time.Time { return time.Unix(1700000000, 0) },
		Send:   func(work, answer string) error { h.sent[work] = answer; return nil },
		Spend:  func(work, question string, v Verdict) error { h.spent = append(h.spent, work+":"+question); return nil },
		Judge:  h.judge,
	}
	return h
}

func (h *harness) judge(question string, _ []core.Work) (Verdict, error) {
	h.judged = append(h.judged, question)
	if v, ok := h.answer[question]; ok {
		return v, nil
	}
	return Verdict{Decline: "no settled position covers it", Runner: "opencode", Model: "big-pickle"}, nil
}

// coordinate is the pass the harness plays: it reports whatever questions the
// test has queued, and lets a test substitute the whole pass when the test is
// about the pass rather than about the questions.
func (h *harness) coordinate(core.Work) (Turn, error) {
	h.turns++
	if h.pass != nil {
		return h.pass(h.turns)
	}
	if h.coordErr != nil {
		return Turn{}, h.coordErr
	}
	return Turn{Unanswered: h.unanswer}, nil
}

func (h *harness) drive(b Budget) (Result, error) {
	h.d.Coordinate = h.coordinate
	return h.d.Drive(h.goal, b)
}

// close moves every task to done the way it has to be moved: a DONE report, a
// review, the soft-done gate, then done. The gate is the ledger's own, so a
// finished run in a test is one that went through it rather than one whose
// state was written past it.
func (h *harness) close() {
	for _, w := range h.tasks {
		if _, err := h.l.AddEvent(w.ID, core.EventReport, "DONE\nSTATUS: DONE"); err != nil {
			h.failed = fmt.Sprintf("report %s: %v", w.ID, err)
			return
		}
		if _, err := h.l.Transition(w.ID, core.StateReview); err != nil {
			h.failed = fmt.Sprintf("review %s: %v", w.ID, err)
			return
		}
		if _, err := h.l.SoftDone(w.ID, true); err != nil {
			h.failed = fmt.Sprintf("soft-done %s: %v", w.ID, err)
			return
		}
		if _, err := h.l.Transition(w.ID, core.StateDone); err != nil {
			h.failed = fmt.Sprintf("done %s: %v", w.ID, err)
			return
		}
		h.closed++
	}
}

func TestARunStopsOnAConditionTheLedgerAlreadyHolds(t *testing.T) {
	h := newHarness(t, 2)
	h.pass = func(turn int) (Turn, error) {
		if turn == 3 {
			h.close()
		}
		return Turn{}, nil
	}
	res, err := h.drive(Budget{Turns: 10, Judgements: 5, Stalled: 3})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if h.failed != "" {
		t.Fatalf("harness: %s", h.failed)
	}
	if res.Stop != StopComplete {
		t.Fatalf("stop = %s (%s), want complete", res.Stop, res.Why)
	}
	if !res.Shipped {
		t.Error("shipped = false, want true on a complete stop")
	}
	if res.Open != nil && len(res.Open) != 0 {
		t.Errorf("open = %v, want nothing open when every task closed", res.Open)
	}
	if res.Why != "every task closed" {
		t.Errorf("why = %q, want it to say what held", res.Why)
	}

	// A turn bound is a bound: it stops with work still open and says which one
	// ran out, because "it stopped" is not something a person can act on.
	h2 := newHarness(t, 1)
	h2.d.Coordinate = h2.coordinate
	over, err := h2.drive(Budget{Turns: 2, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if over.Stop != StopBudget || !strings.Contains(over.Why, "2 turns") {
		t.Errorf("stop = %s (%q), want the turn bound named", over.Stop, over.Why)
	}
	if over.Shipped {
		t.Error("shipped = true on a budget stop, want false")
	}
	if len(over.Open) != 1 {
		t.Errorf("open = %v, want the task that was still running", over.Open)
	}

	// A deadline is a bound too, and it is read from the injected clock.
	h3 := newHarness(t, 1)
	clock := time.Unix(1700000000, 0)
	h3.d.Now = func() time.Time { return clock }
	h3.d.Coordinate = h3.coordinate
	clock = clock.Add(time.Hour)
	late, err := h3.drive(Budget{Deadline: clock.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if late.Stop != StopBudget || !strings.Contains(late.Why, "deadline") {
		t.Errorf("stop = %s (%q), want the deadline named", late.Stop, late.Why)
	}

	// A loop that could not run is a failure with its reason, not a stop that
	// looks like a decision.
	h4 := newHarness(t, 1)
	h4.coordErr = fmt.Errorf("the ledger is locked")
	h4.d.Coordinate = h4.coordinate
	broken, err := h4.drive(Budget{Turns: 5, Stalled: 5})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if broken.Stop != StopFailed || !strings.Contains(broken.Why, "the ledger is locked") {
		t.Errorf("stop = %s (%q), want the failure and its reason", broken.Stop, broken.Why)
	}
}

func TestARunJudgesAQuestionInAPersonsPlaceUpToABound(t *testing.T) {
	h := newHarness(t, 1)
	h.answer["which driver?"] = Verdict{Answer: "modernc.org/sqlite", Tokens: 900, Runner: "opencode", Model: "big-pickle"}
	h.unanswer = []Question{{Work: h.tasks[0].ID, Text: "which driver?"}}

	res, err := h.drive(Budget{Turns: 1, Judgements: 1, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if len(h.judged) != 1 || h.judged[0] != "which driver?" {
		t.Fatalf("judged = %v, want the one question asked once", h.judged)
	}
	if res.Judgements != 1 || res.Tokens != 900 {
		t.Errorf("spend = %d judgements / %d tokens, want what the turn actually reported", res.Judgements, res.Tokens)
	}
	if h.sent[h.tasks[0].ID] != "modernc.org/sqlite" {
		t.Errorf("sent = %v, want the answer delivered to the waiting task", h.sent)
	}
	if len(res.Answered) != 1 || res.Answered[0] != h.tasks[0].ID {
		t.Errorf("answered = %v, want the task whose question was settled", res.Answered)
	}
	// A decision is recorded before it is delivered, and an unrecorded decision
	// is one no review can show a reader.
	if len(h.spent) != 1 || !strings.Contains(h.spent[0], "which driver?") {
		t.Errorf("spent = %v, want the judgement recorded against the work", h.spent)
	}

	// Two questions and a bound of one: the second is not dropped, it becomes
	// the list of where a person is still needed.
	h2 := newHarness(t, 1)
	h2.answer["first?"] = Verdict{Answer: "yes", Runner: "opencode"}
	h2.answer["second?"] = Verdict{Answer: "yes", Runner: "opencode"}
	h2.unanswer = []Question{
		{Work: h2.tasks[0].ID, Text: "first?"},
		{Work: h2.tasks[0].ID, Text: "second?"},
	}
	bounded, err := h2.drive(Budget{Turns: 1, Judgements: 1, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if bounded.Judgements != 1 || len(h2.judged) != 1 {
		t.Errorf("judged %d for a bound of 1, want exactly one", len(h2.judged))
	}
	if len(bounded.Unanswered) != 1 || bounded.Unanswered[0].Text != "second?" {
		t.Errorf("unanswered = %+v, want the question past the bound named", bounded.Unanswered)
	}

	// A token ceiling bounds the same way a count does.
	h3 := newHarness(t, 1)
	h3.answer["costly?"] = Verdict{Answer: "yes", Tokens: 5000, Runner: "opencode"}
	h3.unanswer = []Question{
		{Work: h3.tasks[0].ID, Text: "costly?"},
		{Work: h3.tasks[0].ID, Text: "also costly?"},
	}
	spent2, err := h3.drive(Budget{Turns: 1, Judgements: 9, Tokens: 5000, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if len(h3.judged) != 1 {
		t.Errorf("judged %d under a 5000 token ceiling with a 5000 call, want one", len(h3.judged))
	}
	if len(spent2.Unanswered) != 1 {
		t.Errorf("unanswered = %+v, want the question past the ceiling named", spent2.Unanswered)
	}
}

func TestARefusalIsAnAnswerAndItIsRetriedNever(t *testing.T) {
	h := newHarness(t, 1)
	h.unanswer = []Question{{Work: h.tasks[0].ID, Text: "which database?"}}
	// The model declines: no verdict in the answer map, so the harness judge
	// refuses.
	res, err := h.drive(Budget{Turns: 2, Judgements: 2, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if res.Stop != StopBudget {
		t.Errorf("stop = %s, want the run to keep going until its bound", res.Stop)
	}
	if len(h.sent) != 0 {
		t.Errorf("sent = %v, want nothing delivered for a refusal", h.sent)
	}
	if len(res.Unanswered) != 1 {
		t.Errorf("unanswered = %+v, want one question: the same one, not duplicates", res.Unanswered)
	}
	if res.Unanswered[0].Text != "which database?" {
		t.Errorf("unanswered = %+v, want the question named", res.Unanswered)
	}
	// A pass reports the question again every turn because the executor is still
	// asking it. Asking the model again spends the run's budget on a question it
	// has already refused, so the question is put once and then waited on — and
	// it stays in the bill, because a person may still settle it.
	if len(h.judged) != 1 {
		t.Errorf("judged = %v, want the question put to the model once", h.judged)
	}
	if res.Judgements != 1 {
		t.Errorf("judgements = %d, want one call spent on a question the model refused", res.Judgements)
	}
	// The refusal was still recorded, because a question nobody answered is
	// exactly what a review needs to see.
	if len(h.spent) != 1 {
		t.Errorf("spent = %v, want the refusal recorded once", h.spent)
	}
}

func TestAJudgementIsRecordedBeforeItIsDelivered(t *testing.T) {
	h := newHarness(t, 1)
	h.answer["q?"] = Verdict{Answer: "a", Tokens: 10, Runner: "opencode"}
	h.unanswer = []Question{{Work: h.tasks[0].ID, Text: "q?"}}
	order := []string{}
	h.d.Spend = func(work, question string, v Verdict) error {
		order = append(order, "record")
		return nil
	}
	h.d.Send = func(work, answer string) error {
		order = append(order, "deliver")
		return nil
	}
	h.d.Coordinate = h.coordinate
	if _, err := h.d.Drive(h.goal, Budget{Turns: 1, Judgements: 1, Stalled: 99}); err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if len(order) != 2 || order[0] != "record" || order[1] != "deliver" {
		t.Errorf("order = %v, want the judgement recorded before it is delivered", order)
	}
}

func TestADriverWithNoJudgeIsTheOldSupervisedBehaviour(t *testing.T) {
	h := newHarness(t, 1)
	h.d.Judge = nil
	h.unanswer = []Question{{Work: h.tasks[0].ID, Text: "which database?"}}
	h.d.Coordinate = h.coordinate
	res, err := h.d.Drive(h.goal, Budget{Turns: 5, Judgements: 5, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if len(res.Unanswered) != 1 || res.Unanswered[0].Text != "which database?" {
		t.Errorf("unanswered = %+v, want the question named as the reason it stopped", res.Unanswered)
	}
	if res.Judgements != 0 {
		t.Errorf("judgements = %d, want none spent with no judge to spend them on", res.Judgements)
	}
	if res.Shipped {
		t.Error("shipped = true, want false: nothing was settled")
	}
}

// A run that ended honestly is one whose bill says exactly where a person is
// still required. The bill is the point: without it a goal that stopped looks
// the same as a goal that finished, and the only way to tell is to go reading.
func TestARunReportsWhatAPersonIsStillNeededFor(t *testing.T) {
	h := newHarness(t, 2)
	h.answer["settled?"] = Verdict{Answer: "yes", Tokens: 40, Runner: "opencode"}
	h.unanswer = []Question{
		{Work: h.tasks[0].ID, Text: "settled?"},
		{Work: h.tasks[0].ID, Text: "not settled?"},
		{Work: h.tasks[1].ID, Text: "also not settled?"},
	}
	h.pass = func(int) (Turn, error) { return Turn{Unanswered: h.unanswer}, nil }
	res, err := h.drive(Budget{Turns: 1, Judgements: 1, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if len(res.Answered) != 1 || res.Answered[0] != h.tasks[0].ID {
		t.Errorf("answered = %v, want the one question the model settled", res.Answered)
	}
	// The questions the bound or the model held back are named, with the work
	// each belongs to, so a person knows where to look.
	want := map[string]bool{"not settled?": true, "also not settled?": true}
	if len(res.Unanswered) != len(want) {
		t.Fatalf("unanswered = %+v, want both held-back questions", res.Unanswered)
	}
	for _, q := range res.Unanswered {
		if !want[q.Text] {
			t.Errorf("unanswered = %+v, want only the questions nobody settled", q)
		}
		if q.Work == "" {
			t.Errorf("unanswered %q has no work, want it to say which task is waiting", q.Text)
		}
		delete(want, q.Text)
	}
	if len(want) != 0 {
		t.Errorf("unanswered = %+v, want every held-back question named", res.Unanswered)
	}
	// Both tasks were still open, and both are named, so the caller can end the
	// goal from the bill rather than from silence.
	if len(res.Open) != 2 {
		t.Errorf("open = %v, want both tasks still running", res.Open)
	}
	if res.Shipped {
		t.Error("shipped = true, want false: work was left open")
	}
	// A run that closed something says so, so a caller can see the run did work
	// rather than only watching it.
	h2 := newHarness(t, 1)
	h2.pass = func(turn int) (Turn, error) {
		if turn == 2 {
			h2.close()
		}
		return Turn{}, nil
	}
	done, err := h2.drive(Budget{Turns: 5, Judgements: 5, Stalled: 3})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if h2.failed != "" {
		t.Fatalf("harness: %s", h2.failed)
	}
	// What the run finished is computed from the open set rather than reported
	// by the pass, which cannot see it: a pass moves work between states, and
	// only the ledger sees a task come to rest.
	if len(done.Closed) != 1 || done.Closed[0] != h2.tasks[0].ID {
		t.Errorf("closed = %v, want the task the run's turns finished", done.Closed)
	}
	if !done.Shipped {
		t.Errorf("stop = %s, want complete with nothing open", done.Stop)
	}
}

// A goal whose tasks are all still queued has not been driven, and calling that
// complete because nothing happened to be running would report a run that never
// ran — the most confident way for a loop to lie.
func TestNothingRunningIsNotTheSameAsNothingLeft(t *testing.T) {
	l, err := ledger.New(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer l.Close()
	goal, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatalf("add goal: %v", err)
	}
	for _, title := range []string{"Queued", "Briefed", "Queued too"} {
		if _, err := l.Add("p", title, ledger.AddOptions{Parent: &goal.ID}); err != nil {
			t.Fatalf("add %s: %v", title, err)
		}
	}
	turns := 0
	d := &Driver{
		Ledger:     l,
		Now:        func() time.Time { return time.Unix(1700000000, 0) },
		Coordinate: func(core.Work) (Turn, error) { turns++; return Turn{}, nil },
	}
	res, err := d.Drive(goal, Budget{Turns: 1, Judgements: 1, Stalled: 99})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if res.Stop == StopComplete {
		t.Errorf("stop = complete with %d task(s) never started, want a run that admits it did not finish", len(res.Unfinished))
	}
	if res.Shipped {
		t.Error("shipped = true with untouched work, want false")
	}
	if len(res.Open) != 0 {
		t.Errorf("open = %v, want nothing running: the tasks are queued, not open", res.Open)
	}
	if len(res.Unfinished) != 3 {
		t.Errorf("unfinished = %v, want all three named", res.Unfinished)
	}
}

func TestNothingMovingIsAStop(t *testing.T) {
	h := newHarness(t, 1)
	res, err := h.drive(Budget{Turns: 50, Judgements: 5, Stalled: 3})
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	// A task that is running and never reports is stuck, and waiting on it
	// forever is not patience.
	if res.Stop != StopStalled || !strings.Contains(res.Why, "3 turns") {
		t.Errorf("stop = %s (%q), want the stall named", res.Stop, res.Why)
	}
	if res.Shipped {
		t.Error("shipped = true on a stall, want false")
	}
}
