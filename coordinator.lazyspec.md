> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Coordinator (Go)

The coordinator owns the planning brief, the plan
parser, the known-answer lookup and one coordination pass over a goal's
children. The ledger's claims, impacts, conflicts, concerns and worktrees
are specified in `ledger.lazyspec.md`.

## The Goal Plan Brief Asks For Changes, Not Checks
- `GoalPlanBrief(goal, p, kit)` names the project and its path, carries the goal's title and detail, and demands the headed task-list shape.
- It lists the project's checks in backticks as what the director runs after each task, or says there are none.
- It forbids tasks that only run checks, plan, set up a worktree or find out what the goal is.
- It names the specifications the repository keeps, or says it keeps none and no task adds any.
- It asks for `ASK: <question>` alone when the goal does not say what should change.
- It asks the plan to open with `NAME: <name>`, naming the goal in a few words.

## A Plan Reply Names Its Goal
- `PlanName(texts)` returns the name of a `NAME:` line in the planner's reply, without surrounding quotes, cut at a word to at most 72 characters.
- A reply with no `NAME:` line has no name.

## A Plan Reply Asking A Question Is A Question
- `PlanQuestion(texts)` returns the question of an `ASK:` line in the planner's reply.
- A reply with no `ASK:` line has no question.

## A Heading Line Opens A Group
In `ParsePlan`, a `## <heading>` line sets the heading for the task lines
that follow it.

## A Task Line Is A Task
`- [ ] <title>` (and `- [x]`/`* [X]`) lines parse into tasks with their
title trimmed; other lines are ignored.

## Tasks Before Any Heading Fall Under A Default Heading
Task lines before any heading line parse under the heading `(no heading)`.

## A Resolved Concern's Decision Can Answer
`KnownAnswer` finds a resolved concern's decision on the goal or one of its
children that shares ≥2 significant words with the question.

## A Decision Event Can Answer
`KnownAnswer` finds a `decision` event on the goal or one of its children
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
answer with the question it answers as answered by the director, and lists
the child under answered; a needs-input child returns to running.

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

## A Needs-Input Report Escalates
A transcript ending in `STATUS: NEEDS-INPUT` files the report, moves the
child to needs-input and lists it under escalated.

## A Question Already Filed Is Not Filed Again
An `ASK:` or `STATUS: NEEDS-INPUT` the pass already filed for the child is
not filed, answered or escalated again: a needs-input child stays listed
under escalated, and a running child — answered since — waits.

## A Question Asked Again Is Filed Again
An `ASK:` or `STATUS: NEEDS-INPUT` the executor writes again — in a later
transcript entry, or from a new session — is filed and answered or
escalated again.

## A Child Without Its Own Runner Or Directory Uses Its Goal's Then Its Project's
A child with no runner coordinates with its goal's runner, else its
project's; with no cwd it works in its goal's active shared worktree, else
the project's path.

## Sending Records The Ref The Runner Returns
`Send` continues a child's session and records the ref the runner returns,
so a status check reaches the process now serving it; the child's runner
and directory still follow its goal.

## A Queued Message Reaches Its Session Once The Session Is Idle
- Each pass delivers a running or needs-input child's undelivered messages when its runner reports the session is not running: as one message, oldest first, since a session busy with the first would fork on the second. Each is marked delivered and the child is listed under `delivered`.
- A child whose session is running keeps its messages queued.
- A needs-input child that was sent a message returns to running.

## The Supervision Brief Gives A Goal's Supervisor The Whole Picture
- `SupervisionBrief(picture)` carries the goal's id, title and detail, every decision on it, and the project's checks or that there are none.
- It lists each task with its id, state and title, and its latest report.
- A task whose session stopped without a report is listed with what it last said.
- It lists each open concern under the goal with its number and the work it was raised on.
- It forbids editing files, and says landing waits for the person.
- It asks for choices under an `ASK` whenever the answer is one of a few.
- It asks for a reply ending in an `ACTIONS:` line followed by one `SEND`, `TASK`, `START`, `DROP`, `RESOLVE`, `ASK`, `DONE` or `WAIT` line each.

## A Question The Director Files Keeps Its Choices
- `QuestionBody(asks)` writes each question on its own line, its choices under it as `- ` lines.
- `AskedQuestion(event)` reads that back as one item per question, each with its choices.
- A question asking yes or no, or proposing something, that offers no choices of its own offers Yes and No, marking the one it recommends; a proposal recommends Yes.
- `OpenQuestion(events, work)` is the latest question on the work with no answer or decision on it since.

## A Supervision Reply Parses Into Actions
- `ParseSupervision(texts)` reads the lines after the last `ACTIONS:` line.
- `SEND <id>: <text>`, `TASK: <title>`, `START <id>`, `DROP <id>: <why>`, `RESOLVE <n>: <decision>`, `ASK: <question>`, `DONE: <summary>` and `WAIT` each parse into their action.
- A listed line under an `ASK` is one of its choices.
- A non-blank line that is none of these is returned as refused, with the line.
- A reply with no `ACTIONS:` line has no actions and nothing refused.
