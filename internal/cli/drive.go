package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/driver"
	"wd/internal/judge"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/promotion"
	"wd/internal/runner"
	"wd/internal/taste"
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

// closeFinished puts the goal's finished work through the gates that are left,
// and is the task-shaped half of what goalGates does for the goal itself.
//
// Each report is tried once. A verify that fails, or a landing link that cannot
// be worked out, is written on the work as the gate that stopped it and then
// left alone: re-running a failing build on a loop is how a fleet spends its
// whole budget proving the same thing, and a gate the loop cannot pass is a
// question for a person, not a retry. A later report is a new attempt, because
// new work deserves a new answer.
func (c *Cli) closeFinished(goal core.Work) (driver.Closing, error) {
	out := driver.Closing{Closed: []string{}, Blocked: map[string]string{}}
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		return out, err
	}
	for _, t := range tasks {
		if t.State != core.StateReview {
			continue
		}
		ready, stopped, err := c.closeReady(t.ID)
		if err != nil {
			return out, err
		}
		if !ready {
			continue
		}
		if stopped != "" {
			out.Blocked[t.ID] = stopped
			continue
		}
		gate, err := c.closeOne(t)
		if err != nil {
			return out, err
		}
		if gate != "" {
			out.Blocked[t.ID] = gate
			continue
		}
		out.Closed = append(out.Closed, t.ID)
	}
	return out, nil
}

// closeReady is whether the work is a finished thing waiting to be closed, and
// if it has already been tried, the gate that stopped it. Readiness is a DONE
// report and nowhere newer saying otherwise, and "already tried" is a note the
// last attempt left: both are read off the work rather than kept in the run, so
// a run that starts over does not forget what the last one found.
func (c *Cli) closeReady(id string) (ready bool, stopped string, err error) {
	evs, err := c.Ledger.Events(id, nil)
	if err != nil {
		return false, "", err
	}
	report := -1
	for i, e := range evs {
		if e.Kind == core.EventReport {
			report = i
		}
	}
	if report < 0 || !strings.HasPrefix(evs[report].Body, "DONE") {
		return false, "", nil
	}
	for _, e := range evs[report+1:] {
		if e.Kind == core.EventNote {
			if gate, ok := strings.CutPrefix(e.Body, stoppedAtNote); ok {
				return true, gate, nil
			}
		}
	}
	return true, "", nil
}

// stoppedAtNote marks the note a refused gate leaves, so the note is the
// machine's own record of why the work is still open rather than prose somebody
// has to interpret.
const stoppedAtNote = "drive stopped at "

// closeOne runs the gates for one piece of finished work and returns the gate
// that stopped it, or "" when it closed.
func (c *Cli) closeOne(t core.Work) (string, error) {
	pass, body, _, err := c.runVerify(t.ID)
	if err != nil {
		return "", err
	}
	if !pass {
		// The gate is doing its job. What it found goes on the work, because a
		// loop that reported a clean run over a tree that does not build would
		// be believed, and believed wrongly is worse than stopped.
		if _, err := c.Ledger.AddEvent(t.ID, core.EventNote, stoppedAtNote+"verify\n"+body); err != nil {
			return "", err
		}
		return "verify", nil
	}
	// A task under a goal needs no landing link of its own: the goal is what
	// lands, and a task holding its own would claim a place the change is not.
	if t.Parent == nil {
		if _, err := c.recordPR(t.ID, nil); err != nil {
			if _, err := c.Ledger.AddEvent(t.ID, core.EventNote, stoppedAtNote+"pull request\n"+err.Error()); err != nil {
				return "", err
			}
			return "pull request", nil
		}
	}
	if _, err := c.Ledger.SoftDone(t.ID, t.Parent == nil); err != nil {
		var nr core.NotReady
		if errors.As(err, &nr) {
			if _, err := c.Ledger.AddEvent(t.ID, core.EventNote, stoppedAtNote+"soft-done\n"+nr.Error()); err != nil {
				return "", err
			}
			return "soft-done", nil
		}
		return "", err
	}
	if _, err := c.Ledger.Transition(t.ID, core.StateDone); err != nil {
		return "", err
	}
	return "", nil
}

// tasteJudgeOnce asks about the taste, which needs its own brief and so its own
// call: a model told it is settling an executor's fork would answer a different
// question than the one asked, and the wrong answer here reaches every project
// rather than one task.
func (c *Cli) tasteJudgeOnce(p *project.Project) driver.Judge {
	return func(question string, _ []core.Work) (driver.Verdict, error) {
		brief := question
		rn, err := detectedRunner(p.Runner)
		if err != nil {
			return driver.Verdict{}, err
		}
		h, err := rn.Spawn(runner.SpawnOptions{
			Cwd:   p.Path,
			Name:  "wd judge taste",
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

// promoteTaste reports the rule cards waiting to be judged for the global taste,
// as candidates for the driver's own judge. The question is the brief, because
// the brief is the whole shape of this judgement and the driver passes a
// question through untouched.
//
// A card with no checkout behind it is not a candidate: promoting it would write
// a file into a directory the tool had only guessed at, and a taste the loop
// believes in but cannot ship is a taste that quietly does not exist.
func (c *Cli) promoteTaste(p *project.Project) ([]driver.Candidate, error) {
	cards, cardsDir, err := loadTasteCards()
	if err != nil {
		return nil, err
	}
	if cardsDir == "" {
		return nil, nil
	}
	feedback, err := c.Ledger.Feedback()
	if err != nil {
		return nil, err
	}
	cands := promotion.PromotionCandidates(cards, feedback)
	out := make([]driver.Candidate, 0, len(cands))
	for _, cand := range cands {
		shown := make([]string, 0, len(cand.Evidence))
		for _, f := range cand.Evidence {
			line := fmt.Sprintf("- %s", clipLine(f.Text, 200))
			if f.Project != nil {
				line = fmt.Sprintf("- [%s] %s", *f.Project, line[2:])
			}
			shown = append(shown, line)
		}
		// The ledger line names the card; the brief is what the model reads. A
		// brief truncated onto the work would cut the card off, and the record
		// of what was promoted is the one place a reader looks for the name.
		recorded := fmt.Sprintf("promote card %s (%s) to the global taste: is it true beyond the project it was written in?", cand.Card.ID, clipLine(cand.Card.Title, 80))
		out = append(out, driver.Candidate{
			Work:     hostOf(cand.Evidence),
			Asked:    judge.PromotionBrief(p, cand.Card, shown),
			Recorded: recorded,
			Apply:    c.promoteCard(cand, cardsDir),
		})
	}
	return out, nil
}

// hostOf is the work a promotion decision is recorded on: the most recent piece
// of evidence, because that is the observation that put the card forward. A
// decision needs a row to live on so a reader can find it, and the work that
// showed the pattern is the row a reader looks at.
func hostOf(evidence []core.Feedback) string {
	if len(evidence) == 0 {
		return ""
	}
	last := evidence[len(evidence)-1]
	if last.Work != nil {
		return *last.Work
	}
	return ""
}

// promoteCard is what a decided promotion does: the global card is written
// adopted, the artifacts are rebuilt from it, and the change is committed on its
// own. The card is the source and never hand-edited, so writing it is the edit;
// the rebuild is what makes it reach a session; and the commit is the record
// that it did.
//
// Only these files are staged. A loop that ran `git add -A` in somebody's
// working tree would sweep in whatever else was in it, and the one thing a
// taste promotion must never do is take unrelated work with it.
//
// A commit that fails is not a reason to un-adopt the card: the judgement was
// made and the file was written, and shipping a change is a separate fact from
// believing it. The failure is reported and the card stands.
func (c *Cli) promoteCard(cand promotion.PromotionCandidate, cardsDir string) func(driver.Verdict) (string, error) {
	return func(v driver.Verdict) (string, error) {
		if !v.Decided() {
			// A decline is an answer, so the card is held. It stays a candidate
			// and the refusal is on the work the judgement was recorded on.
			return cand.Card.ID, nil
		}
		ids := make([]string, 0, len(cand.Evidence))
		for _, f := range cand.Evidence {
			ids = append(ids, strconv.Itoa(f.ID))
		}
		path, err := promotion.WriteGlobal(cand.Card, ids, taste.StatusAdopted, cardsDir)
		if err != nil {
			return "", err
		}
		if _, err := taste.Build(buildPaths(cardsDir)); err != nil {
			return "", err
		}
		if err := c.commitPromotion(checkoutRoot(cardsDir), path, cand.Card); err != nil {
			return "", err
		}
		return cand.Card.ID, nil
	}
}

// commitPromotion records the change a promotion made, staged to the card it
// wrote and the artifacts rebuilt from it and nothing else.
func (c *Cli) commitPromotion(root, cardPath string, card taste.Card) error {
	// The paths are relative to the checkout rather than absolute: a partial
	// commit is resolved against the index, and git will not match an absolute
	// path against a checkout that has to recognise it as its own first.
	rel, err := filepath.Rel(root, cardPath)
	if err != nil {
		return err
	}
	paths := []string{rel, "plugin", "dist"}
	args := append([]string{"add", "--"}, paths...)
	r, err := c.runGit(args, root)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		// A staged add that failed leaves nothing to commit and a commit that
		// then refuses would name the wrong problem, so the add is the thing
		// reported when the add is what went wrong.
		return fail("staging the promoted card %s: %s", card.ID, gitSays(r))
	}
	msg := fmt.Sprintf("taste: promote %s to the global taste\n\nA judgement decided this card states something true beyond the project it\nwas written in, and the evidence is on the work it is recorded against.\n", card.ID)
	args = append([]string{"commit", "-m", msg, "--"}, paths...)
	r, err = c.runGit(args, root)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		// Nothing to commit is the ordinary case when the card was already
		// global and the artifacts were already right; anything else is the
		// checkout saying no, and the run says so.
		if strings.Contains(r.Stderr, "nothing to commit") || strings.Contains(r.Stdout, "nothing to commit") {
			return nil
		}
		return fail("committing the promoted card %s: %s", card.ID, gitSays(r))
	}
	return nil
}

// gitSays is what a git command complained about, which is on stderr and is
// sometimes also on stdout depending on the git version.
func gitSays(r runner.RunResult) string {
	msg := strings.TrimSpace(r.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(r.Stdout)
	}
	return msg
}

// buildPaths are the taste build's four directories, derived from where the cards
// were found: the build is the same build cmd/taste runs, and deriving it from
// the one directory already known keeps the loop from having to be told where
// the repository is.
func buildPaths(cardsDir string) taste.BuildPaths {
	root := checkoutRoot(cardsDir)
	return taste.BuildPaths{
		CardsDir:   cardsDir,
		PresetsDir: filepath.Join(root, "presets"),
		PluginDir:  filepath.Join(root, "plugin"),
		DistDir:    filepath.Join(root, "dist"),
	}
}

// checkoutRoot is the repository a cards directory belongs to: two levels up
// from taste/cards, because the paths are the shape of the checkout rather than
// something a caller should have to supply. It takes the cards directory and not
// a card's path, because a card sits three levels down and two up from a card is
// taste, which is still inside the repository and not the root of it.
func checkoutRoot(cardsDir string) string {
	return filepath.Dir(filepath.Dir(cardsDir))
}

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
		TasteJudge: c.tasteJudgeOnce(p),
		Candidates: func() ([]driver.Candidate, error) { return c.promoteTaste(p) },
		Close:      c.closeFinished,
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
	// A run that landed every task drives the goal's own gates, because a goal
	// left in running with all its work shipped is the last thing a person has to
	// come and do. Where a gate needs a fact the loop cannot have — the work is
	// not pushed, so there is nowhere to point at — it stops there and says so.
	// A goal with a task that stopped without shipping is not this case: it came
	// to rest short of its own plan, and only a person decides whether that goal
	// was worth finishing another way. A goal that dropped a task is this case —
	// it chose not to do the work, and what it did do still has to land.
	gates := []string{}
	if res.Shipped {
		gates, err = c.goalGates(goal, res)
		if err != nil {
			return err
		}
	}
	return c.printDrive(goal, res, gates)
}

// goalGates drives a goal whose work has all landed through its own report,
// verify, pull request, soft-done and done, and returns the gate it stopped at.
// Every one of those is machine-checkable, which is the only reason it is safe
// for a loop with nobody in it to run them at all: a gate that needed taste
// would be left standing and named.
func (c *Cli) goalGates(goal core.Work, res driver.Result) ([]string, error) {
	stopped := func(gate string) ([]string, error) { return []string{gate}, nil }
	// A goal's own row often still says queued while its tasks are running:
	// nothing spawns a goal, so nothing has ever moved it. By the time its
	// work has all landed the goal demonstrably ran, and the transition is
	// recorded here rather than assumed away — a board that says queued about a
	// goal whose tasks are all done is a board that is lying.
	goal, err := c.Ledger.Get(goal.ID)
	if err != nil {
		return nil, err
	}
	if goal.State == core.StateQueued || goal.State == core.StateBriefed {
		if goal, err = c.Ledger.Transition(goal.ID, core.StateRunning); err != nil {
			return nil, err
		}
	}
	report := c.goalReport(goal, res)
	if _, err := coordinator.FileReport(c.Ledger, goal, "DONE",
		"DONE\nfiled as text, not read from a session: this goal has no executor session\n"+report); err != nil {
		return nil, err
	}
	pass, verifyBody, _, err := c.runVerify(goal.ID)
	if err != nil {
		return nil, err
	}
	if !pass {
		// The gate is doing its job. What it found is written out, because a
		// loop that reported a clean run over a tree that does not build would
		// be worse than useless: it would be believed.
		if !c.JSON {
			fmt.Fprint(c.Stderr, verifyBody)
		}
		return stopped("verify")
	}
	url, err := c.recordPR(goal.ID, nil)
	if err != nil {
		// Not pushed, no remote, nothing at HEAD: a gate that cannot be checked
		// stays standing and is named, which is the only honest place to stop.
		if !c.JSON {
			fmt.Fprintf(c.Stderr, "the goal could not be linked to a landing: %v\n", err)
		}
		return stopped("pull request")
	}
	if !c.JSON {
		fmt.Fprintf(c.Stderr, "landed: %s\n", url)
	}
	if _, err := c.Ledger.SoftDone(goal.ID, true); err != nil {
		return stopped("soft-done")
	}
	if _, err := c.Ledger.Transition(goal.ID, core.StateDone); err != nil {
		return stopped("done")
	}
	return nil, nil
}

// goalReport is the goal's own account of the run that finished it: the shape of
// the run, and every task that landed under it. It is written from the ledger
// rather than remembered, so it cannot claim a task the run did not close.
func (c *Cli) goalReport(goal core.Work, res driver.Result) string {
	tasks, err := c.Ledger.Tasks(goal.ID)
	if err != nil {
		tasks = nil
	}
	out := []string{
		"Goal " + goal.ID + " — " + goal.Title + ".",
		fmt.Sprintf("Driven with no human present: %d turn(s), %d judgement(s), %d token(s), stopping on %s (%s).",
			res.Turns, res.Judgements, res.Tokens, res.Stop, res.Why),
	}
	if len(tasks) > 0 {
		out = append(out, "", "Every task landed:")
		for _, t := range tasks {
			out = append(out, "- "+t.ID+" "+t.Title)
		}
	}
	return strings.Join(out, "\n")
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
	if n := len(t.Closed); n > 0 {
		bits = append(bits, fmt.Sprintf("closed %d", n))
	}
	if n := len(t.Blocked); n > 0 {
		bits = append(bits, fmt.Sprintf("gated %d", n))
	}
	if n := len(t.Promoted); n > 0 {
		bits = append(bits, fmt.Sprintf("promoted %d", n))
	}
	if n := len(t.Held); n > 0 {
		bits = append(bits, fmt.Sprintf("kept %d", n))
	}
	fmt.Fprintln(c.Stderr, strings.Join(bits, " · "))
}

// tasteLines is what the run did to the taste, and it is always said. A loop
// that changes what every future session believes and does not name the card in
// its own output leaves the largest thing it did to be found later by somebody
// diffing the taste and guessing which run did it.
func tasteLines(res driver.Result) []string {
	out := []string{}
	if len(res.Promoted) > 0 {
		out = append(out, "promoted to the global taste: "+strings.Join(res.Promoted, ", "))
	}
	if len(res.Held) > 0 {
		out = append(out, "judged not global, left as it is: "+strings.Join(res.Held, ", "))
	}
	return out
}

// sortedKeys so a map of gates is read in the same order every run. A bill that
// reorders itself is a bill nobody can compare against last week's.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *Cli) printDrive(goal core.Work, res driver.Result, gates []string) error {
	out := []string{fmt.Sprintf("%s %s", goal.ID, goal.Title)}
	switch {
	case res.Shipped && len(gates) == 0:
		// Every gate passed and the goal is closed. Nothing is left for a person
		// to do, which is the whole claim.
		out = append(out, fmt.Sprintf("shipped and closed: %d turn(s), %d judgement(s), %d tokens",
			res.Turns, res.Judgements, res.Tokens))
		out = append(out, tasteLines(res)...)
	case res.Shipped:
		out = append(out, fmt.Sprintf("every task landed after %d turn(s), %d judgement(s), %d tokens",
			res.Turns, res.Judgements, res.Tokens))
		out = append(out, "the goal still owes: "+strings.Join(gates, ", "))
		out = append(out, tasteLines(res)...)
	default:
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
		if len(res.Blocked) > 0 {
			// A gate the loop would not pass, named. Leaving it out would make
			// the run read as though nothing was in its way, and a person
			// looking at this output is the last reader before it goes back in
			// the box.
			out = append(out, "waiting on a gate:")
			for _, id := range sortedKeys(res.Blocked) {
				out = append(out, "  "+id+": "+res.Blocked[id])
			}
		}
		out = append(out, tasteLines(res)...)
		if res.Stop == driver.StopComplete && len(res.Unlanded) > 0 {
			// Every task came to rest and one of them was not a landing, so
			// there is nothing left to drive and the goal is not finished. Only
			// a person knows whether that goal was worth finishing another way.
			out = append(out, "came to rest without shipping: "+strings.Join(res.Unlanded, ", "))
		} else if res.Stop != driver.StopFailed {
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
