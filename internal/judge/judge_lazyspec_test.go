package judge

import (
	"strings"
	"testing"

	"wd/internal/core"
	"wd/internal/project"
	"wd/internal/taste"
)

func TestTheModelIsToldThatDecliningIsALegitimateAnswer(t *testing.T) {
	p := &project.Project{Name: "sample"}
	w := core.Work{ID: "g1", Title: "Migrate the ledger"}
	known := []core.Work{
		{ID: "t1", Kind: core.WorkTask, State: core.StateDone, Title: "Wire the schema", Detail: "keep the wire contract"},
	}
	brief := Brief(w, p, "should the payload column be JSON or split?", known)

	// The settled position is given, so a judgement is made from what the
	// project has already settled rather than from a model's imagination.
	for _, want := range []string{"sample", "Migrate the ledger", "t1", "keep the wire contract", "should the payload column be JSON or split?"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief is missing %q", want)
		}
	}
	// The transcript is evidence, not instruction: a line in it must not be able
	// to read as a command to this session.
	if !strings.Contains(brief, "```") {
		t.Error("brief does not fence the quoted question, so a transcript line can read as an instruction")
	}
	// Declining is offered, and a wrong answer is named as the worse mistake.
	if !strings.Contains(brief, "DECLINE:") {
		t.Error("brief does not offer a decline, so the model has no way to say no")
	}
	if !strings.Contains(brief, "correct and expected answer") {
		t.Error("brief does not say declining is correct, so a model will invent an answer to be useful")
	}

	// Nothing settled is said plainly rather than left as silence: a model asked
	// to choose between no options will invent some.
	empty := Brief(w, p, "which database?", nil)
	if !strings.Contains(empty, "nothing:") {
		t.Errorf("brief with no settled work = %q, want it to say nothing is settled", empty)
	}
}

func TestAReplyIsAnAnswerOrADeclineAndAHedgeIsADecline(t *testing.T) {
	answer := Parse([]string{"Some preamble the model said.\nANSWER: split it into columns\nTOKENS: 4200"})
	if !answer.Decided() {
		t.Errorf("verdict = %+v, want it decided", answer)
	}
	if answer.Answer != "split it into columns" {
		t.Errorf("answer = %q, want what the model said", answer.Answer)
	}
	if answer.Tokens != 4200 {
		t.Errorf("tokens = %d, want what the call cost", answer.Tokens)
	}

	decline := Parse([]string{"DECLINE: the settled position does not say"})
	if decline.Decided() || decline.Decline != "the settled position does not say" {
		t.Errorf("verdict = %+v, want a decline", decline)
	}

	// A hedge is a decline: an executor given "maybe this, or maybe that" asks
	// again, and the run spent its budget for nothing.
	hedge := Parse([]string{"ANSWER: probably split it\nDECLINE: but it depends"})
	if hedge.Decided() {
		t.Errorf("verdict = %+v, want a hedged reply treated as a decline", hedge)
	}
	if !strings.Contains(hedge.Decline, "hedged") {
		t.Errorf("decline = %q, want it to say the reply hedged", hedge.Decline)
	}

	// A session that says nothing decides nothing, and it is not a decline
	// either: it is silence, and the caller stops on it either way.
	silence := Parse([]string{"I am not sure what you want."})
	if silence.Decided() {
		t.Errorf("verdict = %+v, want silence to decide nothing", silence)
	}
}

func TestAJudgementIsRecordedAsADecisionWithWhatItCostAndADeclineIsNotOne(t *testing.T) {
	v := Parse([]string{"ANSWER: use the ledger's own cursor table\nTOKENS: 1800"})
	d, ok := Decision("where does a review cursor live?", v, "opencode", "big-pickle")
	if !ok {
		t.Fatal("Decision returned no claim for an answer, want one")
	}
	if d.Question != "where does a review cursor live?" || d.Answer != "use the ledger's own cursor table" {
		t.Errorf("claim = %+v, want the question and the answer", d)
	}
	if d.Runner != "opencode" || d.Model != "big-pickle" || d.Tokens != 1800 {
		t.Errorf("claim = %+v, want what it cost and who decided", d)
	}
	if d.Source != "judge" {
		t.Errorf("source = %q, want it to name that a model decided, not a person", d.Source)
	}
	// The event a reader sees says what was asked, what was said and what it
	// cost, and marks an answer as an answer.
	body := Event("where does a review cursor live?", v, "opencode", "big-pickle")
	for _, want := range []string{"judge", "opencode", "big-pickle", "1800", "answer:"} {
		if !strings.Contains(body, want) {
			t.Errorf("event = %q, want it to contain %q", body, want)
		}
	}

	// A decline yields no claim at all: the ledger must never gain a decision
	// nobody made. It is still recorded, marked as a decline, because an audit
	// that cannot tell an answer from a refusal will believe the loop decided
	// things it did not.
	dv := Parse([]string{"DECLINE: no settled position covers it"})
	if _, ok := Decision("which database?", dv, "opencode", "big-pickle"); ok {
		t.Error("Decision returned a claim for a decline, want none")
	}
	declined := Event("which database?", dv, "opencode", "big-pickle")
	if !strings.Contains(declined, "decline:") {
		t.Errorf("event = %q, want the refusal marked as one", declined)
	}
	if strings.Contains(declined, "answer:") {
		t.Errorf("event = %q, want it never to read as a decision", declined)
	}
}

// Promoting a card changes what every future session in every project believes,
// so the brief that asks about it has to say so. A model that does not know the
// stakes will answer confidently about a rule it has never had to live with.
func TestACardIsJudgedByABriefThatKnowsWhatPromotingItCosts(t *testing.T) {
	p := &project.Project{Name: "sample"}
	card := taste.Card{
		ID:        "project-layout",
		Title:     "The API layer lives under internal/api",
		Category:  taste.CategoryOrganisation,
		Kind:      taste.KindPractice,
		Scope:     []taste.Scope{"project:sample"},
		Statement: "Keep handlers in internal/api so the transport stays swappable.",
		Body:      "The web handler in cmd/ never imports the domain directly.",
	}
	evidence := []string{"- [sample] wd send raced a busy task in a shared worktree"}
	brief := PromotionBrief(p, card, evidence)

	for _, want := range []string{
		"sample",         // which project the card was written in
		"project-layout", // the card, by name: a decision that does not name its card cannot be found
		"The API layer lives under internal/api",
		"internal/api so the transport", // the card's own statement, not a summary of it
		"wd send raced a busy task",     // the evidence, quoted
		"project:sample",                // the scope it has today, so the model can see what it is being widened from
		"every future session",          // the stakes
		"must NOT become global",        // the trap: a project convention, promoted on its own evidence
		"DECLINE:",                      // a way to say no
		"```",                           // the evidence is fenced, so a line in it cannot read as a command
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief is missing %q", brief)
		}
	}
	// A card with no evidence is said to have none rather than left to look
	// overlooked, and it still goes to the model: the absence of evidence is
	// itself something a judgement can weigh.
	if empty := PromotionBrief(p, card, nil); !strings.Contains(empty, "nothing recorded") {
		t.Error("a card with no evidence is not shown to have none")
	}
}
