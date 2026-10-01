package promotion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wd/internal/core"
	"wd/internal/taste"
)

func card(id string, scope ...taste.Scope) taste.Card {
	return taste.Card{
		ID:       id,
		Title:    id + " title",
		Category: taste.CategoryJudgment,
		Kind:     taste.KindPrinciple,
		Scope:    scope,
		Status:   taste.StatusAdopted,
		Body:     "The body of " + id + ".",
	}
}

func fb(id int, cardID, project, source string) core.Feedback {
	f := core.Feedback{ID: id, Text: "observation " + cardID, Source: core.FeedbackSource(source)}
	if cardID != "" {
		c := cardID
		f.Card = &c
	}
	if project != "" {
		p := project
		f.Project = &p
	}
	return f
}

// The bar on *asking* is one observation, because that is all it takes to have a
// question. The bar on *promoting* is a model's judgement, and a count is a
// proxy for that judgement which grows on its own — a rule that reached the taste
// having only ever been seen twice in the same place.
func TestACardWithOneObservationIsWorthAskingAbout(t *testing.T) {
	cards := []taste.Card{
		card("no-evidence", "project:sample"),
		card("one-here", "project:sample"),
		card("one-there", "project:other"),
		card("global-already", "project:sample"),
		card("global-already-global", "global"),
		card("not-adopted", "project:sample"),
		card("not-project-scoped", "lang:ts"),
	}
	for i, c := range cards {
		if c.ID == "not-adopted" {
			c.Status = taste.StatusCandidate
			cards[i] = c
		}
	}
	feedback := []core.Feedback{
		fb(1, "one-here", "sample", "note"),
		fb(2, "one-there", "other", "note"),
		fb(3, "global-already", "sample", "note"),
		fb(4, "not-adopted", "sample", "note"),
		fb(5, "not-project-scoped", "sample", "note"),
	}
	got := PromotionCandidates(cards, feedback)
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.Card.ID] = true
	}
	if len(got) != 2 {
		t.Fatalf("candidates = %v, want the two project cards that have been observed", got)
	}
	for _, want := range []string{"one-here", "one-there"} {
		if !ids[want] {
			t.Errorf("%s is not a candidate, want it asked about on one observation", want)
		}
	}
	// A card nothing has ever tested is not a candidate: there is no evidence to
	// put to a model, and asking anyway would spend the run's judgement budget
	// on a question with no argument behind it.
	if ids["no-evidence"] {
		t.Error("a card with no evidence is a candidate, so the loop will judge nothing")
	}
	// The global card existing means it is already promoted and never a
	// candidate again, however much more evidence turns up.
	if ids["global-already"] {
		t.Error("a card with a global card is a candidate again, so it could be promoted twice")
	}
	if ids["not-adopted"] || ids["not-project-scoped"] {
		t.Error("a card that is not adopted, or is not project-scoped, is a candidate for the global taste")
	}

	// Most evidence first, ties by id, so two runs over the same cards ask in
	// the same order: a promotion is the loudest thing this package does, and a
	// decision that arrives in a different order each run is not reproducible.
	same := []taste.Card{card("b-card", "project:sample"), card("a-card", "project:sample")}
	tied := []core.Feedback{fb(1, "b-card", "s", "note"), fb(2, "a-card", "s", "note")}
	order := PromotionCandidates(same, tied)
	if len(order) != 2 || order[0].Card.ID != "a-card" {
		t.Errorf("order = %v, want a-card first: a tie is settled by id, not by map order", order)
	}
	fewer := PromotionCandidates(same, []core.Feedback{fb(1, "b-card", "s", "note")})
	if len(fewer) != 1 || fewer[0].Card.ID != "b-card" {
		t.Errorf("candidates = %v, want the only observed card", fewer)
	}
}

// A global card is created, never rewritten. A taste decision that could be
// edited in place would have no history, and the review surface shows a change
// rather than a state.
func TestAGlobalCardIsWrittenAtTheStatusAskedAndNeverOverwritten(t *testing.T) {
	src := card("no-branch-chains", "project:sample")

	// The status asked for is the status written, which is the only difference
	// between a person's proposal and a judgement's decision.
	for _, status := range []taste.Status{taste.StatusCandidate, taste.StatusAdopted} {
		dir := t.TempDir()
		path, err := WriteGlobal(src, []string{"7", "9"}, status, dir)
		if err != nil {
			t.Fatalf("WriteGlobal %s: %v", status, err)
		}
		want := filepath.Join(dir, string(src.Category), "no-branch-chains-global.md")
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
		loaded, err := taste.LoadCards(os.DirFS(dir), dir)
		if err != nil {
			t.Fatalf("LoadCards: %v", err)
		}
		found := map[string]taste.Card{}
		for _, c := range loaded {
			found[c.ID] = c
		}
		g, ok := found["no-branch-chains-global"]
		if !ok {
			t.Fatalf("no global card written, have %v", found)
		}
		if g.Status != status {
			t.Errorf("status = %q, want %q: a judgement that promotes writes adopted, a person proposing writes candidate", g.Status, status)
		}
		if g.Title != src.Title || g.Body != src.Body || g.Kind != src.Kind {
			t.Errorf("global card = %+v, want the project card's title, body and kind kept", g)
		}
		for _, s := range g.Scope {
			if s != "global" {
				t.Errorf("scope = %q, want global: this is the whole point of the write", s)
			}
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "[7, 9]") {
			t.Errorf("card body = %q, want the evidence ids recorded on the card", body)
		}
		// The second write of the same card is refused rather than overwriting.
		// AdoptCard is the candidate path, and it must not be a way past the
		// decision the loop already made.
		if _, err := WriteGlobal(src, []string{"1"}, taste.StatusCandidate, dir); err == nil {
			t.Errorf("an existing global card was overwritten at status %s", status)
		}
		if _, err := AdoptCard(src, []string{"1"}, dir); err == nil {
			t.Error("AdoptCard overwrote an existing global card, so it is a way to un-adopt one by hand")
		}
	}
}
