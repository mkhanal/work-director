package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"wd/internal/core"
	"wd/internal/taste"
	tastecards "wd/taste/cards"
)

// tasteCard is a rule card in effect and where it comes from: the cards built
// into wd, a checkout's directory, or yours.
type tasteCard struct {
	taste.Card
	Source string `json:"source"`
}

// tasteView is the taste every brief carries, and what feedback has gathered
// toward new rules.
type tasteView struct {
	Cards   []tasteCard      `json:"cards"`
	Learned []core.Candidate `json:"learned"`
}

func (c *Cli) tasteView(rest []string) error {
	if len(rest) != 0 {
		return fail("usage: wd taste")
	}
	checkout, err := tasteCheckout()
	if err != nil {
		return err
	}
	var base []taste.Card
	source := "built-in"
	if checkout == "" {
		base, err = taste.LoadCards(tastecards.Snapshot, "built-in taste/cards")
	} else {
		base, err = taste.LoadCards(os.DirFS(checkout), checkout)
		source = checkout
	}
	if err != nil {
		return err
	}
	own, _, err := ownCards()
	if err != nil {
		return err
	}
	mine := map[string]bool{}
	for _, o := range own {
		mine[o.ID] = true
	}
	view := tasteView{Cards: []tasteCard{}}
	for _, card := range layer(base, own) {
		from := source
		if mine[card.ID] {
			from = "yours"
		}
		view.Cards = append(view.Cards, tasteCard{Card: card, Source: from})
	}
	slices.SortStableFunc(view.Cards, func(a, b tasteCard) int { return strings.Compare(string(a.Category), string(b.Category)) })
	if view.Learned, err = c.Ledger.Distill(); err != nil {
		return err
	}
	var b strings.Builder
	category := ""
	for _, card := range view.Cards {
		if string(card.Category) != category {
			category = string(card.Category)
			fmt.Fprintf(&b, "%s\n", category)
		}
		mark := ""
		if card.Source == "yours" {
			mark = " (yours)"
		}
		fmt.Fprintf(&b, "  %s%s\n", card.Title, mark)
	}
	if len(view.Learned) > 0 {
		b.WriteString("learned, not yet a rule:\n")
		for _, l := range view.Learned {
			fmt.Fprintf(&b, "  %s ×%d\n", l.Key, l.Count)
		}
	}
	return c.out(view, strings.TrimRight(b.String(), "\n"))
}
