// Package promotion decides when a project-scoped rule card has been seen in
// the real world often enough to be worth asking about as a global rule.
//
// It used to decide that on its own, by counting projects and attached
// feedback. Now it only finds what is worth asking about, and the judgement is
// made elsewhere and recorded like every other judgement: a count is a proxy
// for "is this rule really general", and a proxy that grows on its own ends up
// in the taste having only ever been seen twice in the same place.
package promotion

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wd/internal/core"
	"wd/internal/taste"
)

// PromotionCandidate is a project-scoped adopted card with real-world evidence
// behind it. A card whose global card exists is already promoted and never a
// candidate.
type PromotionCandidate struct {
	Card     taste.Card      `json:"card"`
	Evidence []core.Feedback `json:"evidence"`
	Projects int             `json:"projects"`
	Attached int             `json:"attached"`
}

// PromotionCandidates returns the cards worth putting to a model, most evidence
// first. The bar on asking is one observation — anything less is a card nothing
// has ever tested, and there is nothing to ask about — and whether that
// observation is enough to believe is the model's judgement, not a count.
func PromotionCandidates(cards []taste.Card, feedback []core.Feedback) []PromotionCandidate {
	ids := map[string]bool{}
	for _, c := range cards {
		ids[c.ID] = true
	}
	out := []PromotionCandidate{}
	for _, card := range cards {
		if card.Status != taste.StatusAdopted || ids[globalID(card.ID)] {
			continue
		}
		projectScoped := false
		for _, s := range card.Scope {
			if strings.HasPrefix(string(s), "project:") {
				projectScoped = true
				break
			}
		}
		if !projectScoped {
			continue
		}
		var evidence []core.Feedback
		for _, f := range feedback {
			if f.Card != nil && *f.Card == card.ID {
				evidence = append(evidence, f)
			}
		}
		if len(evidence) == 0 {
			continue
		}
		projects := map[string]bool{}
		attached := 0
		for _, f := range evidence {
			if f.Project != nil {
				projects[*f.Project] = true
			}
			if f.Source == core.FeedbackAttached {
				attached++
			}
		}
		out = append(out, PromotionCandidate{Card: card, Evidence: evidence, Projects: len(projects), Attached: attached})
	}
	// Evidence first, then by id, so two runs over the same cards ask the same
	// question in the same order: a taste decision that arrives in a different
	// order each run is not reproducible, and a promotion is the loudest thing
	// this package does.
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Evidence) != len(out[j].Evidence) {
			return len(out[i].Evidence) > len(out[j].Evidence)
		}
		return out[i].Card.ID < out[j].Card.ID
	})
	return out
}

// WriteGlobal writes a global card beside the project card it came from, at the
// status given, and returns the path written. The project card stays as it is,
// and an existing global card is never overwritten: a taste decision that could
// be edited in place is a decision with no history, and the review surface shows
// a change rather than a state.
func WriteGlobal(card taste.Card, evidence []string, status taste.Status, cardsDir string) (string, error) {
	dir := filepath.Join(cardsDir, string(card.Category))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	id := globalID(card.ID)
	text := `---
id: ` + id + `
title: ` + card.Title + `
category: ` + string(card.Category) + `
scope: [global]
kind: ` + string(card.Kind) + `
status: ` + string(status) + `
always: false
enforce: []
evidence: [` + strings.Join(evidence, ", ") + `]
---
` + card.Body + `
`
	path := filepath.Join(dir, id+".md")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// AdoptCard writes a global candidate card (never adopted in one step) from a
// project card and its evidence, and returns the path written. It is a person's
// own act of proposing: a judgement that promotes a card does not come through
// here, because on that path the judgement is the gate and a hand-typed card is
// not one.
func AdoptCard(card taste.Card, evidence []string, cardsDir string) (string, error) {
	return WriteGlobal(card, evidence, taste.StatusCandidate, cardsDir)
}

// GlobalID is the id of the global card promoted from a project card.
func GlobalID(projectID string) string { return globalID(projectID) }

func globalID(projectID string) string { return projectID + "-global" }
