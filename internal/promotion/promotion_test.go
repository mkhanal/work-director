package promotion

import (
	"os"
	"path/filepath"
	"testing"

	"wd/internal/core"
	"wd/internal/taste"
)

func TestAdoptCardWritesBesideTheProjectCardNeverOverIt(t *testing.T) {
	cardsDir := t.TempDir()
	projectText := "---\nid: proj-rule\ntitle: A rule\ncategory: judgment\nscope: [project:sample-app]\nkind: practice\nstatus: adopted\nalways: false\nenforce: []\nevidence: []\n---\nKeep it small.\n"
	projectPath := filepath.Join(cardsDir, "judgment", "proj-rule.md")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(projectText), 0o644); err != nil {
		t.Fatal(err)
	}
	card, err := taste.ParseCard(projectText, projectPath)
	if err != nil {
		t.Fatal(err)
	}

	path, err := AdoptCard(*card, []string{"1", "2"}, cardsDir)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if path == projectPath {
		t.Fatalf("global candidate written over the project card at %s", path)
	}
	if got, err := os.ReadFile(projectPath); err != nil || string(got) != projectText {
		t.Fatalf("project card changed: %q, %v", got, err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	global, err := taste.ParseCard(string(text), path)
	if err != nil {
		t.Fatalf("global candidate does not parse: %v", err)
	}
	if global.ID == card.ID || global.Status != taste.StatusCandidate || len(global.Scope) != 1 || global.Scope[0] != "global" {
		t.Fatalf("global = %+v, want a distinct global candidate", global)
	}
	cards, err := taste.LoadCards(cardsDir)
	if err != nil || len(cards) != 2 {
		t.Fatalf("cards = %v, %v, want the project card and the candidate", cards, err)
	}

	if _, err := AdoptCard(*card, []string{"1", "2", "3"}, cardsDir); err == nil {
		t.Fatal("second adopt overwrote the existing candidate")
	}
}

func TestAnAdoptedCardIsNoLongerAPromotionCandidate(t *testing.T) {
	cardsDir := t.TempDir()
	projectText := "---\nid: proj-rule\ntitle: A rule\ncategory: judgment\nscope: [project:sample-app]\nkind: practice\nstatus: adopted\nalways: false\nenforce: []\nevidence: []\n---\nKeep it small.\n"
	projectPath := filepath.Join(cardsDir, "judgment", "proj-rule.md")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(projectText), 0o644); err != nil {
		t.Fatal(err)
	}
	card, appA, appB := "proj-rule", "app-a", "app-b"
	feedback := []core.Feedback{{ID: 1, Card: &card, Project: &appA}, {ID: 2, Card: &card, Project: &appB}}

	cards, err := taste.LoadCards(cardsDir)
	if err != nil {
		t.Fatal(err)
	}
	candidates := PromotionCandidates(cards, feedback)
	if len(candidates) != 1 || candidates[0].Card.ID != card {
		t.Fatalf("before adopting: candidates = %+v, want proj-rule", candidates)
	}
	if _, err := AdoptCard(candidates[0].Card, []string{"1", "2"}, cardsDir); err != nil {
		t.Fatal(err)
	}

	cards, err = taste.LoadCards(cardsDir)
	if err != nil {
		t.Fatal(err)
	}
	if candidates := PromotionCandidates(cards, feedback); len(candidates) != 0 {
		t.Fatalf("after adopting: candidates = %+v, want none", candidates)
	}
}
