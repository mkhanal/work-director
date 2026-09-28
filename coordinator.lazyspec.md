# Coordinator (Go)

The Go coordinator keeps the behaviour of the TypeScript coordinator it
ports (`packages/wd/src/coordinator.ts`): the planning brief, the plan
parser, the known-answer lookup and one coordination pass over an epic's
children. The ledger's claims, impacts, conflicts, concerns and worktrees
are specified in `ledger.lazyspec.md`.

## The Epic Plan Brief Names The Project And Verify Commands
`EpicPlanBrief` names the project and its path, lists the project's verify
commands in backticks (or "none listed"), carries the goal title and detail,
and demands the exact headed task-list shape.

## A Heading Line Opens A Group
In `ParsePlan`, a `## <heading>` line sets the heading for the task lines
that follow it.

## A Task Line Is A Task
`- [ ] <title>` (and `- [x]`/`* [X]`) lines parse into tasks with their
title trimmed; other lines are ignored.

## Tasks Before Any Heading Fall Under A Default Heading
Task lines before any heading line parse under the heading `(no heading)`.

## A Resolved Concern's Decision Can Answer
`KnownAnswer` finds a resolved concern's decision on the epic or one of its
children that shares ≥2 significant words with the question.

## A Decision Event Can Answer
`KnownAnswer` finds a `decision` event on the epic or one of its children
that shares ≥2 significant words with the question.

## Exact Containment Wins
A candidate that contains the whole question (case-insensitive) wins over
any token overlap.

## Two Shared Significant Words Answer
A candidate sharing two or more significant words — lowercase words of four
or more characters after dropping punctuation — with the question answers;
the most shared words win.

## A Known Question Is Answered
`CoordinateOnce` files the question, sends the known answer, files the
answer, and lists the child under answered; a needs-input child returns to
running.

## An Unknown Question Escalates
A question with no known answer moves the child to needs-input and lists
it under escalated.

## A Done Report Moves The Child To Review
A transcript ending in `STATUS: DONE` files the report and moves a running
child to review, listing it under reviewed.

## A Blocked Report Moves The Child To Blocked
A transcript ending in `STATUS: BLOCKED` files the report and moves the
child to blocked, listing it under blocked.

## A Child With A Claimed Session Uses The Claim
A running child with no session but a claim coordinates against the claim's session.

## A Child Without A Session Waits
A running child with neither session nor claim is listed under waiting.

## A Child With Nothing New Waits
A running child whose last transcript line is neither an ASK nor a
STATUS report is listed under waiting.
