// Package promotion decides when a project-scoped rule card has been seen
// enough in the real world to become a global candidate.
package promotion

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wd/internal/core"
	"wd/internal/taste"
)

// PromotionCandidate is a project-scoped adopted card whose evidence
// recurs: two or more projects, or two or more real-world (attached) feedback.
type PromotionCandidate struct {
	Card     taste.Card      `json:"card"`
	Evidence []core.Feedback `json:"evidence"`
	Projects int             `json:"projects"`
	Attached int             `json:"attached"`
}

// PromotionCandidates returns the cards ready to promote, most evidence first.
func PromotionCandidates(cards []taste.Card, feedback []core.Feedback) []PromotionCandidate {
	out := []PromotionCandidate{}
	for _, card := range cards {
		if card.Status != taste.StatusAdopted {
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
		if len(projects) >= 2 || attached >= 2 {
			out = append(out, PromotionCandidate{Card: card, Evidence: evidence, Projects: len(projects), Attached: attached})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Evidence) > len(out[j].Evidence) })
	return out
}

// AdoptCard writes a global candidate card (never adopted in one step) from a
// project card and its evidence, and returns the path written.
func AdoptCard(card taste.Card, evidence []string, cardsDir string) (string, error) {
	dir := filepath.Join(cardsDir, string(card.Category))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	text := `---
id: ` + card.ID + `
title: ` + card.Title + `
category: ` + string(card.Category) + `
scope: [global]
kind: ` + string(card.Kind) + `
status: candidate
always: false
enforce: []
evidence: [` + strings.Join(evidence, ", ") + `]
---
` + card.Body + `
`
	path := filepath.Join(dir, card.ID+".md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
