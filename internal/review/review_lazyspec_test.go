package review

import (
	"path/filepath"
	"strings"
	"testing"

	"wd/internal/core"
	"wd/internal/ledger"
)

func newPass(t *testing.T, seed func(*ledger.Ledger)) (*ledger.Ledger, Pass) {
	t.Helper()
	l, err := ledger.New(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	seed(l)
	p, err := From(l, "p", nil, nil)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	return l, p
}

func addClaim(t *testing.T, l *ledger.Ledger, work, body, question, answer, effective string) core.Event {
	t.Helper()
	d := core.Decision{Question: question, Answer: answer, Source: "opencode", Model: "big-pickle", Tokens: 1200}
	var eff *string
	if effective != "" {
		eff = &effective
	}
	e, err := l.Decide(work, body, d, eff)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	return e
}

func TestAPassIsADiffFromTheCursor(t *testing.T) {
	l, p := newPass(t, func(l *ledger.Ledger) {
		w, err := l.Add("p", "Work", ledger.AddOptions{})
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		addClaim(t, l, w.ID, "use sqlite", "which database", "modernc.org/sqlite", "")
	})

	// A cursor that does not exist reads as 0, so the first pass sees
	// everything rather than nothing.
	if p.Since != 0 || p.SinceFeedback != 0 {
		t.Fatalf("since = %d/%d, want 0/0 on a ledger with no cursor", p.Since, p.SinceFeedback)
	}
	if len(p.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(p.Claims))
	}
	if p.Claims[0].Work == nil || p.Claims[0].Work.Title != "Work" {
		t.Fatalf("claim work = %+v, want the work the decision is about", p.Claims[0].Work)
	}

	// Acknowledging moves both cursors to the end of the pass, and the next
	// pass is a diff from there.
	if err := Acknowledge(l, p); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	again, err := From(l, "p", nil, nil)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if !again.Empty() {
		t.Fatalf("second pass = %+v, want empty: the first was acknowledged", again)
	}
	if again.Since != p.Through || again.SinceFeedback != p.ThroughFeedback {
		t.Errorf("second since = %d/%d, want %d/%d", again.Since, again.SinceFeedback, p.Through, p.ThroughFeedback)
	}

	// --since reads a window without moving anything, so a hand-asked question
	// does not consume the next pass.
	window, err := From(l, "p", ptr(0), nil)
	if err != nil {
		t.Fatalf("From since=0: %v", err)
	}
	if len(window.Claims) != 1 {
		t.Errorf("windowed pass claims = %d, want 1", len(window.Claims))
	}
	after, err := From(l, "p", nil, nil)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if after.Since != p.Through {
		t.Errorf("cursor moved to %d by a read, want it still at %d", after.Since, p.Through)
	}
}

// Reading a pass and having seen it are different acts, so a pass that was read
// but never rendered — a reader that crashed, a pipe that closed — is read
// again rather than skipped. Only acknowledging consumes it.
func TestReadingAPassAndHavingSeenItAreDifferentActs(t *testing.T) {
	l := ledgerFor(t)
	w, err := l.Add("p", "Work", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	addClaim(t, l, w.ID, "first", "which database", "sqlite", "")

	// Reading it three times, and rendering none of the results, must not
	// advance the cursor: nothing was seen by a reader, so nothing was seen.
	for range 3 {
		if _, err := From(l, "p", nil, nil); err != nil {
			t.Fatalf("From: %v", err)
		}
	}
	cur, err := l.Cursor("p", CursorEvents)
	if err != nil {
		t.Fatalf("Cursor: %v", err)
	}
	if cur != 0 {
		t.Fatalf("cursor = %d after reading only, want 0: reading is not seeing", cur)
	}
	if p, err := From(l, "p", nil, nil); err != nil || len(p.Claims) != 1 {
		t.Fatalf("pass = %+v, %v; want the same claim every time", p, err)
	}

	// Acknowledging is what consumes it, and it consumes exactly what was in
	// the pass — not everything that exists by the time it is called.
	addClaim(t, l, w.ID, "second", "which runner", "codex", "")
	pass, err := From(l, "p", nil, nil)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if len(pass.Claims) != 2 {
		t.Fatalf("claims = %d, want both", len(pass.Claims))
	}
	if err := Acknowledge(l, pass); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	addClaim(t, l, w.ID, "third", "which model", "whatever", "")
	next, err := From(l, "p", nil, nil)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if len(next.Claims) != 1 || next.Claims[0].Event.Body != "third" {
		t.Fatalf("next pass = %+v, want only what was decided since", next.Claims)
	}
	if next.Since != pass.Through {
		t.Errorf("next since = %d, want the acknowledged mark %d", next.Since, pass.Through)
	}

	// A cursor that forgets is refused: it would show the same thing twice and
	// hide what changed since.
	if err := l.SetCursor("p", CursorEvents, pass.Through-1); err == nil {
		t.Error("SetCursor backwards: want it refused")
	}
}

func TestRecordedTimeAndEffectiveTimeAreSeparateFields(t *testing.T) {
	_, p := newPass(t, func(l *ledger.Ledger) {
		w, err := l.Add("p", "Work", ledger.AddOptions{})
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		addClaim(t, l, w.ID, "ship it", "should we ship", "yes", "2026-01-01T00:00:00Z")
	})
	c := p.Claims[0]
	if c.Event.Effective == nil {
		t.Fatal("effective = nil, want the moment it took hold recorded separately")
	}
	if c.Event.At == *c.Event.Effective {
		t.Errorf("recorded and effective are both %s, want the test to actually separate them", c.Event.At)
	}
	if !strings.Contains(p.Render(), "effective") {
		t.Errorf("render = %q, want it to show both moments when they differ", p.Render())
	}
}

func TestADecisionCarriesItsParts(t *testing.T) {
	_, p := newPass(t, func(l *ledger.Ledger) {
		w, err := l.Add("p", "Work", ledger.AddOptions{})
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		addClaim(t, l, w.ID, "use sqlite", "which database", "modernc.org/sqlite", "")
	})
	c := p.Claims[0]
	if c.Event.Decision == nil {
		t.Fatal("decision = nil, want the parts stored")
	}
	if c.Event.Decision.Question != "which database" || c.Event.Decision.Answer != "modernc.org/sqlite" {
		t.Errorf("question/answer = %q/%q, want what was settled", c.Event.Decision.Question, c.Event.Decision.Answer)
	}
	if c.Event.Decision.Model != "big-pickle" || c.Event.Decision.Tokens != 1200 {
		t.Errorf("model/tokens = %q/%d, want what it cost", c.Event.Decision.Model, c.Event.Decision.Tokens)
	}

	// A claim with no question behind it cannot be reviewed, so both parts are
	// required and the ledger says which is missing.
	l := ledgerFor(t)
	w, err := l.Add("p", "Work", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := l.Decide(w.ID, "text", core.Decision{Answer: "a"}, nil); err == nil {
		t.Error("a decision with no question: want it refused")
	}
	if _, err := l.Decide(w.ID, "text", core.Decision{Question: "q"}, nil); err == nil {
		t.Error("a decision with no answer: want it refused")
	}
}

func TestAReversalIsANewDecisionNotAnEdit(t *testing.T) {
	l := ledgerFor(t)
	w, err := l.Add("p", "Work", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	original := addClaim(t, l, w.ID, "use postgres", "which database", "postgres", "")
	first, err := From(l, "p", nil, nil)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if !first.Claims[0].Stands {
		t.Fatal("the original claim does not stand, want it to before any reversal")
	}

	// A reversal with no reason is refused: nobody could read the motive for it.
	if _, err := l.Reverse(w.ID, original.ID, ""); err == nil {
		t.Error("Reverse with no reason: want it refused")
	}
	rev, err := l.Reverse(w.ID, original.ID, "the fleet has no postgres")
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if rev.Decision == nil || rev.Decision.Reverses != original.ID {
		t.Errorf("reversal = %+v, want it to name the claim it undoes", rev.Decision)
	}

	after, err := From(l, "p", nil, nil)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if after.Reversals != 1 {
		t.Errorf("reversals = %d, want 1", after.Reversals)
	}
	byID := map[int]Claim{}
	for _, c := range after.Claims {
		byID[c.Event.ID] = c
	}
	if got := byID[original.ID]; got.Stands {
		t.Errorf("original stands = true, want it marked reversed")
	} else if !strings.Contains(got.Reversed, "by event") {
		t.Errorf("reversed = %q, want what undid it", got.Reversed)
	}
	// The original is left exactly as it was made: the claim survives, which is
	// the one thing an audit cannot afford to lose.
	if byID[original.ID].Event.Body != "use postgres" {
		t.Errorf("original body = %q, want the claim as made", byID[original.ID].Event.Body)
	}
	if byID[original.ID].Event.Decision == nil || byID[original.ID].Event.Decision.Answer != "postgres" {
		t.Errorf("original answer = %+v, want it untouched", byID[original.ID].Event.Decision)
	}
	if !strings.Contains(after.Render(), "REVERSED") {
		t.Errorf("render = %q, want the reversal visible", after.Render())
	}

	// Only a decision can be reversed.
	pr, err := l.AddEvent(w.ID, core.EventPr, "https://example.test/pr/1")
	if err != nil {
		t.Fatalf("pr: %v", err)
	}
	if _, err := l.Reverse(w.ID, pr.ID, "because"); err == nil {
		t.Error("Reverse a pr event: want it refused, a pull request is not a decision")
	}
	if _, err := l.Reverse(w.ID, 9999, "because"); err == nil {
		t.Error("Reverse a missing event: want it refused naming the id")
	}
}

func TestAPassCarriesTheTasteItRanUnder(t *testing.T) {
	_, p := newPass(t, func(l *ledger.Ledger) {
		w, err := l.Add("p", "Work", ledger.AddOptions{})
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		addClaim(t, l, w.ID, "use sqlite", "which database", "modernc.org/sqlite", "")
		if _, err := l.AddFeedback("a promoted lesson", ledger.FeedbackOptions{Project: ptr("p"), Card: ptr("card:send-frugal"), Source: core.FeedbackAttached}); err != nil {
			t.Fatalf("feedback: %v", err)
		}
		// An observation with no card is left for wd distill, not shown here.
		if _, err := l.AddFeedback("a one-off note", ledger.FeedbackOptions{Project: ptr("p"), Source: core.FeedbackNote}); err != nil {
			t.Fatalf("feedback: %v", err)
		}
	})
	if len(p.Cards) != 1 {
		t.Fatalf("cards = %+v, want only the one that promoted something", p.Cards)
	}
	if p.Cards[0].Card != "card:send-frugal" {
		t.Errorf("card = %s, want the promoted one", p.Cards[0].Card)
	}
	if !strings.Contains(p.Render(), "taste promoted") {
		t.Errorf("render = %q, want the taste in the pass", p.Render())
	}
}

func TestWorkThatStoppedWithoutShippingIsInTheSamePass(t *testing.T) {
	_, p := newPass(t, func(l *ledger.Ledger) {
		w, err := l.Add("p", "Thing", ledger.AddOptions{Kind: core.WorkGoal})
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		if _, err := l.Abandon(w.ID, core.AbandonUnmerged, "PR 12 never merged"); err != nil {
			t.Fatalf("abandon: %v", err)
		}
	})
	if len(p.Unshipped) != 1 {
		t.Fatalf("unshipped = %+v, want the abandoned goal in the pass", p.Unshipped)
	}
	if p.Unshipped[0].Reason != "unmerged: PR 12 never merged" {
		t.Errorf("reason = %q, want the reason it did not ship", p.Unshipped[0].Reason)
	}
	if !strings.Contains(p.Render(), "stopped without shipping") {
		t.Errorf("render = %q, want the abandoned work in the pass", p.Render())
	}
	if p.Empty() {
		t.Error("Empty = true, want a pass that found something to be not empty")
	}
}

func ptr[T any](v T) *T { return &v }

func ledgerFor(t *testing.T) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}
