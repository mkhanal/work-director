// Package brief composes the briefs executors receive: the standalone brief,
// the epic-slice brief and the epic spawn brief, plus the task-block
// rendering. It is a port of packages/wd/src/brief.ts with the same text,
// the same card selection and the same human-hours rejection.
package brief

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"wd/internal/core"
	"wd/internal/project"
	"wd/internal/taste"
)

// ReportFormat is the report format every brief ends with.
const ReportFormat = `## Report format (last message, exactly)
STATUS: DONE | BLOCKED | NEEDS-INPUT
FILES: <changed files or none>
VERIFY: <command> → <exit code>, <lines that matter>
PR: <url or none>
NOTES: <one line; agent-minutes spent>`

var humanHoursRe = regexp.MustCompile(`(?i)\b\d+(\.\d+)?\s*(h|hrs?|hours?)\b|\bstory points?\b|\b(man|person)[- ](day|hour)s?\b`)

// HumanHoursRejected rejects a brief that justifies a shortcut with a
// human-hour estimate.
type HumanHoursRejected struct {
	Match string
}

func (e *HumanHoursRejected) Error() string {
	return fmt.Sprintf("human-hour estimate rejected: %q", e.Match)
}

// AssertNoHumanHours rejects human-hour estimates in a brief's text.
func AssertNoHumanHours(text string) error {
	if m := humanHoursRe.FindString(text); m != "" {
		return &HumanHoursRejected{Match: m}
	}
	return nil
}

// CardsFor selects the adopted cards that reach an executor through the
// project's name and stack scope; the taste plugin carries the rest.
func CardsFor(p *project.Project, cards []taste.Card) []taste.Card {
	scopes := map[string]bool{"project:" + p.Name: true}
	for _, s := range p.Stack {
		scopes["lang:"+s] = true
		scopes["stack:"+s] = true
	}
	var out []taste.Card
	for _, c := range cards {
		if c.Status != taste.StatusAdopted {
			continue
		}
		for _, s := range c.Scope {
			if scopes[string(s)] {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// Context is what the brief adds beyond the work item itself: the project
// roadmap, decisions already made, and relevant history.
type Context struct {
	Roadmap   *string
	Decisions []string
	History   []string
}

func (c Context) roadmapOr(p *project.Project) string {
	if c.Roadmap != nil {
		return strings.TrimSpace(*c.Roadmap)
	}
	return strings.TrimSpace(p.Roadmap)
}

func (c Context) decisionsOr() string {
	if len(c.Decisions) == 0 {
		return "- none yet"
	}
	var b strings.Builder
	for i, d := range c.Decisions {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(d)
	}
	return b.String()
}

func (c Context) historyOr() string {
	if len(c.History) == 0 {
		return "- none"
	}
	var b strings.Builder
	for i, h := range c.History {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(h)
	}
	return b.String()
}

const coordination = `You are an executor, not the whole team. Claim what you work, and let others see it:
- Claim your task: ` + "`wd claim <id> <session>`" + `. Record every area you will touch: ` + "`wd impact <id> <+path>`" + `.
- Look before stepping: ` + "`wd conflict <epic>`" + ` shows overlapping claims; never edit a path another active task claims.
- Something does not work or conflicts? Raise ` + "`wd concern add <id> \"<text>\"`" + ` — a concern is a decision queue for the director, not something you work around silently.
- Own worktree for isolation? Register it: ` + "`wd worktree attach <id> <path> [--branch <b>]`" + `. Merge it back when done: ` + "`wd merge <id>`" + ` (after verify and a clean conflict scan).`

func shared(p *project.Project) string {
	workflows := "none listed"
	if len(p.Workflows) > 0 {
		workflows = strings.Join(p.Workflows, ", ")
	}
	verify := "none listed"
	if len(p.Verify) > 0 {
		var quoted []string
		for _, v := range p.Verify {
			quoted = append(quoted, "`"+v+"`")
		}
		verify = strings.Join(quoted, ", ")
	}
	modeRule := "open a draft PR when verify passes."
	if p.Mode == "ask" {
		modeRule = "do not open a PR; stop at review with the diff ready."
	}
	return `- Repo: ` + p.Path + `; follow its own instructions in ` + p.InstructionsFile + `.
- Workflows to use: ` + workflows + `.
- Verify before reporting: ` + verify + `.
- Mode ` + string(p.Mode) + `: ` + modeRule + `
- Cost is agent minutes; never justify a shortcut with a human-hour estimate.
- Facts you can look up, you look up. NEEDS-INPUT is for value judgments only, and carries your proposed answer.
- Every message to the director is ≤10 lines. No narration, no restating the brief.
`
}

func rules(p *project.Project, cards []taste.Card) string {
	chosen := CardsFor(p, cards)
	if len(chosen) == 0 {
		return "- none beyond the constitution"
	}
	var b strings.Builder
	for i, c := range chosen {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- **")
		b.WriteString(c.Title)
		b.WriteString(".** ")
		b.WriteString(c.Statement)
	}
	return b.String()
}

// Compose renders the standalone brief: full context, single executor, own
// branch and PR.
func Compose(w core.Work, p *project.Project, cards []taste.Card, ctx Context) (string, error) {
	if err := AssertNoHumanHours(w.Title + "\n" + w.Detail); err != nil {
		return "", err
	}
	roadmap := ctx.roadmapOr(p)
	if roadmap == "" {
		roadmap = "not written"
	}
	return `# Brief ` + w.ID + ` · ` + p.Name + ` · ` + string(w.Kind) + `

## Goal
` + w.Title + `
` + w.Detail + `

## Context
Where this project is heading:
` + roadmap + `
Decisions already made (do not re-open):
` + ctx.decisionsOr() + `
Relevant history:
` + ctx.historyOr() + `

## How to work
- Decide ambiguities yourself, consistent with the rules below; record each decision under NOTES. Stop only for irreversible actions.
- Checkpoint: after your plan (before code) and at DONE, send a report in the format below. Nothing else in between.
- Capabilities available: the repo's workflows and verify commands listed here, web research, the repo's own docs.

## Constraints
` + shared(p) + `
## Rules
Global rules are in your taste constitution and /taste-* skills. Specific to this project and stack:
` + rules(p, cards) + `

` + ReportFormat + `
`, nil
}

// ComposeSlice renders one task of an epic: the epic goal, the task, and the
// other executors' live claims. No backlog, no history.
func ComposeSlice(w core.Work, epic core.Work, claims []core.Work, p *project.Project, cards []taste.Card) (string, error) {
	if err := AssertNoHumanHours(w.Title + "\n" + w.Detail); err != nil {
		return "", err
	}
	claimLines := "- none currently"
	if len(claims) > 0 {
		var b strings.Builder
		for i, c := range claims {
			if i > 0 {
				b.WriteByte('\n')
			}
			impact := core.SplitImpact(c.Impact)
			impactStr := "no impact recorded"
			if len(impact) > 0 {
				impactStr = strings.Join(impact, ", ")
			}
			b.WriteString("- " + c.ID + " " + c.Title + " (" + str(c.Claim) + ") — " + impactStr)
		}
		claimLines = b.String()
	}
	heading := w.Heading
	if heading == nil {
		heading = ptr("task")
	}
	detail := ""
	if w.Detail != "" {
		detail = "\n" + w.Detail
	}
	return `# Brief ` + w.ID + ` · ` + p.Name + ` · ` + *heading + ` of epic ` + epic.ID + `

## Goal (epic)
` + epic.Title + `
— this task:
` + w.Title + detail + `

## Co-workers on this branch — do not touch these paths
` + claimLines + `
The full register is ` + "`wd tasks " + epic.ID + "`" + `; before a shared path run ` + "`wd conflict " + epic.ID + "`" + `. A resolved concern is a decision; follow it.

## How to work
` + strings.ReplaceAll(strings.ReplaceAll(coordination, "<id>", w.ID), "<epic>", epic.ID) + `
- Decide ambiguities yourself, consistent with the rules below. Stop only for irreversible actions.
- Checkpoint: after your plan (before code) and at DONE, send a report in the format below. Nothing else in between.

## Constraints
` + shared(p) + `
## Rules
Global rules are in your taste constitution and /taste-* skills. Specific to this project and stack:
` + rules(p, cards) + `

` + ReportFormat + `
`, nil
}

// ComposeEpic renders an epic spawn brief: goal, context, and the compact
// open task list grouped by heading.
func ComposeEpic(epic core.Work, taskBlock string, p *project.Project, cards []taste.Card, ctx Context) (string, error) {
	if err := AssertNoHumanHours(epic.Title + "\n" + epic.Detail); err != nil {
		return "", err
	}
	roadmap := ctx.roadmapOr(p)
	if roadmap == "" {
		roadmap = "not written"
	}
	return `# Brief ` + epic.ID + ` · ` + p.Name + ` · epic

## Goal
` + epic.Title + `
` + epic.Detail + `

## Context
Where this project is heading:
` + roadmap + `
Decisions already made (do not re-open):
` + ctx.decisionsOr() + `
Relevant history:
` + ctx.historyOr() + `

## Task list (open; claim with ` + "`wd claim <id> <session>`" + `)
` + taskBlock + `

## How to work
` + strings.ReplaceAll(coordination, "<epic>", epic.ID) + `
- Decide ambiguities yourself, consistent with the rules below. Stop only for irreversible actions.
- Checkpoint: after your plan (before code) and at DONE, send a report in the format below. Nothing else in between.

## Constraints
` + shared(p) + `
## Rules
Global rules are in your taste constitution and /taste-* skills. Specific to this project and stack:
` + rules(p, cards) + `

` + ReportFormat + `
`, nil
}

func ptr(s string) *string { return &s }

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RenderTaskBlock renders tasks grouped by heading, e.g.
// "- <id> <state> <title> (claim: <who>)".
func RenderTaskBlock(tasks []core.Work) string {
	byHeading := map[string][]core.Work{}
	var unheaded []core.Work
	var headings []string
	for _, t := range tasks {
		if t.Heading != nil {
			h := *t.Heading
			if _, ok := byHeading[h]; !ok {
				headings = append(headings, h)
			}
			byHeading[h] = append(byHeading[h], t)
		} else {
			unheaded = append(unheaded, t)
		}
	}
	sort.Slice(headings, func(i, j int) bool {
		return localeCompare(headings[i], headings[j]) < 0
	})
	var lines []string
	for _, h := range headings {
		lines = append(lines, "## "+h)
		for _, t := range byHeading[h] {
			lines = append(lines, renderTaskLine(t))
		}
	}
	if len(unheaded) > 0 {
		lines = append(lines, "## (no heading)")
		for _, t := range unheaded {
			lines = append(lines, renderTaskLine(t))
		}
	}
	if len(lines) == 0 {
		return "- none"
	}
	return strings.Join(lines, "\n")
}

func renderTaskLine(t core.Work) string {
	claim := ""
	if t.Claim != nil {
		claim = " (claim: " + *t.Claim + ")"
	}
	line := "- " + t.ID + " " + string(t.State) + " " + t.Title + claim
	if impact := core.SplitImpact(t.Impact); len(impact) > 0 {
		line += " — " + strings.Join(impact, ", ")
	}
	return line
}

// localeCompare approximates JavaScript's String.localeCompare for plain
// ASCII: case-insensitive first, case as tiebreaker.
func localeCompare(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		if la < lb {
			return -1
		}
		return 1
	}
	if a == b {
		return 0
	}
	if a < b {
		return -1
	}
	return 1
}
