// Package review builds the pass a periodic audit reads: what the loop decided
// since the last acknowledgement, when each claim was recorded and when it
// became effective, what has been reversed, which taste cards were promoted
// under it, and which work stopped without shipping.
//
// A pass is a diff, not a dump. The cursor is what the last pass saw, so the
// next one shows what changed rather than everything that ever happened, and
// acknowledging a pass is what moves the cursor.
package review

import (
	"fmt"
	"strconv"
	"strings"

	"wd/internal/core"
	"wd/internal/ledger"
)

// The cursors a pass keeps. They are separate because events and feedback are
// separate streams with separate ids, and a pass that acknowledged one and not
// the other would either re-show decisions or silently swallow a new card.
const (
	// CursorEvents is how far the decision stream has been read.
	CursorEvents = "review.events"
	// CursorFeedback is how far the taste stream has been read.
	CursorFeedback = "review.feedback"
)

// Claim is one decision to review, with the work it belongs to resolved so a
// reader sees what it was about and not only which row it sits on.
type Claim struct {
	Event  core.Event `json:"event"`
	Work   *core.Work `json:"work"`
	Stands bool       `json:"stands"`
	// Reversed says what undid this claim, in words, so the reason for the
	// reversal travels with the claim it corrects.
	Reversed string `json:"reversed"`
}

// Card is a taste card promoted since the cursor. It ships with the pass
// because a card that changed what the loop believes is part of what the loop
// decided: the decisions under it were made by a taste the reader has not seen.
type Card struct {
	Card    string              `json:"card"`
	Text    string              `json:"text"`
	Project *string             `json:"project"`
	Source  core.FeedbackSource `json:"source"`
	At      string              `json:"at"`
}

// Unshipped is work that stopped without its change reaching anywhere.
type Unshipped struct {
	Work   core.Work `json:"work"`
	Reason string    `json:"reason"`
	At     string    `json:"at"`
}

// Pass is one review, over a project, from a cursor.
type Pass struct {
	Project string `json:"project"`
	// Since is the cursor the pass started from, and Through is the highest id
	// it saw. Acknowledging the pass moves the cursor to Through.
	Since   int `json:"since"`
	Through int `json:"through"`
	// SinceFeedback and ThroughFeedback are the same two marks on the taste
	// stream.
	SinceFeedback   int         `json:"since_feedback"`
	ThroughFeedback int         `json:"through_feedback"`
	Claims          []Claim     `json:"claims"`
	Cards           []Card      `json:"cards"`
	Unshipped       []Unshipped `json:"unshipped"`
	// Reversals is how many claims in this pass were undone by another claim
	// in this pass, counted so a reader can see at a glance that something was
	// corrected rather than having to spot it.
	Reversals int `json:"reversals"`
}

// Empty reports whether a pass found nothing at all, so a periodic caller can
// tell "nothing happened" from "nothing was read". Both are honest; only one
// of them means the loop is idle.
func (p Pass) Empty() bool {
	return len(p.Claims) == 0 && len(p.Cards) == 0 && len(p.Unshipped) == 0
}

// From reads a pass for a project, starting at the stored cursors unless the
// caller overrides them. The override exists for two readers: a human asking
// for a window by hand, and a test. Neither of them moves the cursor; only
// Acknowledge does.
func From(l *ledger.Ledger, project string, since *int, sinceFeedback *int) (Pass, error) {
	p := Pass{Project: project, Claims: []Claim{}, Cards: []Card{}, Unshipped: []Unshipped{}}
	var err error
	if p.Since, err = l.Cursor(project, CursorEvents); err != nil {
		return Pass{}, err
	}
	if p.SinceFeedback, err = l.Cursor(project, CursorFeedback); err != nil {
		return Pass{}, err
	}
	if since != nil {
		p.Since = *since
	}
	if sinceFeedback != nil {
		p.SinceFeedback = *sinceFeedback
	}
	events, err := l.EventsAfter(p.Since)
	if err != nil {
		return Pass{}, err
	}
	feedback, err := l.FeedbackAfter(p.SinceFeedback)
	if err != nil {
		return Pass{}, err
	}
	if p.Through, err = l.LastEventID(); err != nil {
		return Pass{}, err
	}
	if p.ThroughFeedback, err = l.LastFeedbackID(); err != nil {
		return Pass{}, err
	}

	// A reversal is a decision naming the one it undoes, and the two can be in
	// the same pass or the reversal can be older than the claim. Both are
	// resolved here so a claim always reads with its correction attached when
	// one exists.
	reversedBy := map[int]int{}
	for _, e := range events {
		if e.Decision != nil && e.Decision.Reverses > 0 {
			reversedBy[e.Decision.Reverses] = e.ID
		}
	}
	// Reasons for reversals that predate the cursor, read directly so a claim
	// at the edge of the window is not shown as standing when it is not.
	olderReasons, err := reversalReasonsBefore(l, p.Since, events)
	if err != nil {
		return Pass{}, err
	}

	for _, e := range events {
		switch e.Kind {
		case core.EventDecision:
			work, err := workOf(l, e.Work)
			if err != nil {
				return Pass{}, err
			}
			claim := Claim{Event: e, Work: work, Stands: true}
			if by, ok := reversedBy[e.ID]; ok {
				claim.Stands = false
				claim.Reversed = "by event " + strconv.Itoa(by)
				p.Reversals++
			} else if reason, ok := olderReasons[e.ID]; ok {
				claim.Stands = false
				claim.Reversed = reason
			}
			p.Claims = append(p.Claims, claim)
		case core.EventAbandon:
			work, err := workOf(l, e.Work)
			if err != nil {
				return Pass{}, err
			}
			p.Unshipped = append(p.Unshipped, Unshipped{Work: *work, Reason: e.Body, At: e.At})
		}
	}

	for _, f := range feedback {
		if f.Card == nil || *f.Card == "" {
			continue
		}
		p.Cards = append(p.Cards, Card{Card: *f.Card, Text: f.Text, Project: f.Project, Source: f.Source, At: f.At})
	}
	return p, nil
}

func workOf(l *ledger.Ledger, id string) (*core.Work, error) {
	w, err := l.Get(id)
	if err != nil {
		// A claim whose work is gone is still a claim. Reviewing the audit
		// trail must not fall over because the thing it was about is no
		// longer in the ledger.
		return nil, nil
	}
	return &w, nil
}

// reversalReasonsBefore reads the reasons of reversals older than the cursor, so
// a claim that entered this pass's window but was undone earlier still reads as
// undone rather than standing.
func reversalReasonsBefore(l *ledger.Ledger, since int, inPass []core.Event) (map[int]string, error) {
	reversedBy := map[int]int{}
	for _, e := range inPass {
		if e.Decision != nil && e.Decision.Reverses > 0 {
			reversedBy[e.Decision.Reverses] = e.ID
		}
	}
	if len(reversedBy) == 0 {
		return map[int]string{}, nil
	}
	earlier, err := l.EventsAfter(0)
	if err != nil {
		return nil, err
	}
	out := map[int]string{}
	for _, e := range earlier {
		if e.ID > since || e.Decision == nil || e.Decision.Reverses == 0 {
			continue
		}
		if _, ok := reversedBy[e.Decision.Reverses]; !ok {
			out[e.Decision.Reverses] = "by event " + strconv.Itoa(e.ID) + ", before this pass"
		}
	}
	return out, nil
}

// Acknowledge moves both cursors to the end of the pass, so the next pass is a
// diff from here. It is deliberately not folded into From: reading a pass and
// having seen it are two different acts, and a reader that crashed mid-read
// should get the same pass again rather than skip it.
func Acknowledge(l *ledger.Ledger, p Pass) error {
	if err := l.SetCursor(p.Project, CursorEvents, p.Through); err != nil {
		return err
	}
	return l.SetCursor(p.Project, CursorFeedback, p.ThroughFeedback)
}

// Render is the pass as text a person reads: what was decided, what it cost,
// when it took hold, what was undone and why, what taste it ran under, and
// what stopped without shipping. Recorded and effective times are both shown
// and differ only when they differ.
func (p Pass) Render() string {
	if p.Empty() {
		return fmt.Sprintf("nothing since event %d — the loop has not decided anything new", p.Since)
	}
	out := []string{fmt.Sprintf("%s · since event %d · through %d", p.Project, p.Since, p.Through)}
	for _, c := range p.Claims {
		line := fmt.Sprintf("  %d %s %s", c.Event.ID, claimWord(c), clip(c.Question(), 120))
		if c.Event.Decision != nil {
			d := c.Event.Decision
			if d.Source != "" || d.Model != "" {
				line += fmt.Sprintf(" [%s]", strings.TrimSpace(strings.Join(nonEmpty(d.Source, d.Runner, d.Model), " ")))
			}
			if d.Tokens > 0 {
				line += fmt.Sprintf(" (%d tokens)", d.Tokens)
			}
		}
		if c.Event.Effective != nil && *c.Event.Effective != c.Event.At {
			line += fmt.Sprintf("\n      recorded %s, effective %s", c.Event.At, *c.Event.Effective)
		}
		if !c.Stands {
			line += "\n      reversed — " + c.Reversed
		}
		out = append(out, line)
	}
	if len(p.Cards) > 0 {
		out = append(out, "  taste promoted since:")
		for _, c := range p.Cards {
			out = append(out, fmt.Sprintf("    %s · %s", c.Card, oneline(c.Text)))
		}
	}
	if len(p.Unshipped) > 0 {
		out = append(out, "  stopped without shipping:")
		for _, u := range p.Unshipped {
			out = append(out, fmt.Sprintf("    %s %s · %s", u.Work.ID, u.Work.Title, u.Reason))
		}
	}
	return strings.Join(out, "\n")
}

// claimWord says what a claim is, using the structured question when there is
// one and the prose body otherwise. A decision recorded as one line is still
// reviewable; it just has less to show.
func (c Claim) Question() string {
	if c.Event.Decision != nil && c.Event.Decision.Question != "" {
		q := c.Event.Decision.Question
		if c.Event.Decision.Answer != "" {
			return q + " → " + c.Event.Decision.Answer
		}
		return q
	}
	return c.Event.Body
}

func claimWord(c Claim) string {
	if !c.Stands {
		return "REVERSED"
	}
	if c.Event.Decision != nil && c.Event.Decision.Reverses > 0 {
		return "reversal"
	}
	return "decision"
}

func nonEmpty(parts ...string) []string {
	out := []string{}
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func oneline(s string) string { return clip(s, 80) }

// clip collapses whitespace and cuts to a budget. A rendered pass is a summary
// a person reads at a glance; the whole claim, untruncated, is what --json
// gives, and the ledger holds it either way. Truncating the text view rather
// than the record keeps both honest.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}
