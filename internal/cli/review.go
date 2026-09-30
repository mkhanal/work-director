package cli

import (
	"fmt"
	"strconv"
	"strings"

	"wd/internal/review"
)

// review is the periodic audit. It answers one question: since the last time
// anyone looked, what did the loop decide, what did it cost, when did each
// claim take hold, what has since been undone, what taste was it running
// under, and what stopped without shipping.
//
// It is a diff rather than a dump because the cursor remembers where the last
// pass stopped. Reading a pass and having seen it are separate acts: --ack is
// what acknowledges, so a pass that fails to print is not silently lost.
func (c *Cli) review(rest []string) error {
	if len(rest) > 0 && rest[0] == "reverse" {
		return c.reviewReverse(rest[1:])
	}
	project := strOr(c.Args, "project", "")
	if project != "" {
		if _, err := c.project(project); err != nil {
			return err
		}
	}
	since, err := intFlag(c.Args, "since", nil)
	if err != nil {
		return err
	}
	sinceFeedback, err := intFlag(c.Args, "since-feedback", nil)
	if err != nil {
		return err
	}
	if project == "" {
		project = c.defaultProject()
	}
	if project == "" {
		return fail("which project: wd review --project <name> — known: %s", strings.Join(c.projectNames(), ", "))
	}
	pass, err := review.From(c.Ledger, project, since, sinceFeedback)
	if err != nil {
		return err
	}
	if flag(c.Args, "ack") {
		if err := review.Acknowledge(c.Ledger, pass); err != nil {
			return err
		}
		return c.out(pass, fmt.Sprintf("acknowledged %s through event %d and card %d — the next pass is a diff from here",
			project, pass.Through, pass.ThroughFeedback))
	}
	return c.out(pass, pass.Render())
}

// reviewReverse records that a decision no longer stands. It is a new decision
// naming the one it undoes, not an edit: the original claim stays visible as it
// was made, and a reader sees both the claim and the correction in the order
// they happened. The reason is required, because a reversal nobody can read the
// motive for is indistinguishable from an error.
func (c *Cli) reviewReverse(rest []string) error {
	if len(rest) < 1 {
		return fail("usage: wd review reverse <event-id> \"<reason>\" [--work <id>]")
	}
	target, err := strconv.Atoi(rest[0])
	if err != nil {
		return fail("event id %q is not a number: wd events <work> lists ids", rest[0])
	}
	reason := strings.Join(rest[1:], " ")
	if reason == "" {
		reason = strOr(c.Args, "reason", "")
	}
	if reason == "" {
		return fail("a reversal says why: wd review reverse %d \"<reason>\"", target)
	}
	// The reversal is filed against the work the original decision belongs to,
	// so it lands in the same stream and shows up in the same review.
	work := strOr(c.Args, "work", "")
	if work == "" {
		original, err := c.Ledger.EventWork(target)
		if err != nil {
			return err
		}
		work = original
	}
	e, err := c.Ledger.Reverse(work, target, reason)
	if err != nil {
		return err
	}
	return c.out(e, fmt.Sprintf("reversed event %d — %s", target, e.Body))
}

// defaultProject is the project a bare wd review reads: the only one there is.
// Guessing among several would be a silent wrong answer to a question about
// what a loop decided, so with more than one the command asks rather than
// assuming.
func (c *Cli) defaultProject() string {
	names := c.projectNames()
	if len(names) == 1 {
		return names[0]
	}
	return ""
}

func intFlag(a Args, name string, def *int) (*int, error) {
	raw := str(a, name)
	if raw == nil {
		return def, nil
	}
	v, err := strconv.Atoi(*raw)
	if err != nil {
		return nil, fail("--%s %q is not a number", name, *raw)
	}
	return &v, nil
}
