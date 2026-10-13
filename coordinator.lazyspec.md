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

## The Goal Plan Brief Asks For Outcomes, Not Changes
- `GoalPlanBrief` names the project and its path, carries the goal's title and detail, and demands the outcome shape: a title, a done-when, and optional order and split lines.
- It forbids outcomes that only set up, install, land, tag or run checks, and lists the project's checks as what the director runs.
- It names the specifications the repository keeps, or says it keeps none.
- A sub-goal's brief carries its top goal with its done-when and finish line, each ancestor, and its siblings with their states.
- A top goal's brief asks for a FINISH line and gives the repository's finish setting and its past goals' finish lines.

## A Plan Reply Names Its Goal
- `PlanName(texts)` returns the name of a `NAME:` line in the planner's reply, without surrounding quotes, cut at a word to at most 72 characters.
- A reply with no `NAME:` line has no name.

## A Plan Reply Asking A Question Is A Question
- `PlanQuestion(texts)` returns the question of an `ASK:` line in the planner's reply.
- A reply with no `ASK:` line has no question.

## An Outcome Line Is A Sub-Goal With Its Done-When And Order
- `ParseOutcomes` reads each `- <title>` line with its indented `done when:`, `after:` and `split:` lines.
- An outcome without a done-when is still read, with an empty one.
- `after:` numbers that are not earlier outcomes are dropped.

## A Plan Reply Says Where The Goal Finishes
- `ParseFinish` reads `FINISH: <line> | from: <source>` into a finish line and its source.
- `FINISH: ?` with listed choices reads as unsure, keeping the choices in order.
- A line or source outside its set reads as no finish at all.

## A Plan Reply Says Why It Is Broken Down So
- `GoalPlanBrief` asks, after the outcomes, for `WHY:`, `LEFT OUT:` and `JUDGE BY:` lines.
- `ParseReasoning(texts)` reads the last of each into why, left out and judge by; a part not given is null.
- A reply with none of them has no reasoning.

## An Agent's Steps Are Read From Its Last STEPS Block
- `ParseSteps` reads the last `STEPS:` block: `[ ]` todo, `[~]` doing, `[x]` done, `[-]` dropped.
- A transcript with no `STEPS:` block has no steps.

## An Agent Can Ask For A Split Or Say It Is Off Course
- `ParseSplit` reads the outcomes after the last `SPLIT:` line, each with its done-when.
- `ParseOffCourse` reads what the last `OFF-COURSE:` line found.

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
- It carries the top goal's title, detail, done-when and finish line, and the decisions recorded on it.
- It lists the goal's sub-goals as a tree, each with its state, done-when, what it waits on, its current step, and its latest report or what it last said; superseded ones with why.
- It lists open concerns with their numbers and the checks the director runs.
- It asks for a COURSE line and then an ACTIONS list, naming every action.

## Every Supervision Reply Says Whether The Goal Is On Course
- `ParseCourse` reads `COURSE: <on course | correct | pivot> — <why>`.
- Carrying out a reply files its course as a course event; a reply without one files `unstated`.

## A Question The Director Files Keeps Its Choices
- `QuestionBody(asks)` writes each question on its own line, its choices under it as `- ` lines.
- `AskedQuestion(event)` reads that back as one item per question, each with its choices.
- A question asking yes or no, or proposing something, that offers no choices of its own offers Yes and No, marking the one it recommends; a proposal recommends Yes.
- `OpenQuestion(events, work)` is the latest question on the work with no answer or decision on it since.

## A Supervision Reply Parses Into Actions
- `ParseSupervision(texts)` reads the lines after the last `ACTIONS:` line.
- `SEND <id>: <text>`, `GOAL <parent>: …`, `START <id>`, `SUPERSEDE <id>: <why>`, `EDIT <id>: …`, `AFTER <id>: …`, `MET <id>: <why>`, `MISSED <id>: <what>`, `CHORE: <what>`, `PROPOSE: …`, `RESOLVE <n>: <decision>`, `ASK: <question>`, `DONE: <summary>` and `WAIT` each parse into their action.
- Listed lines under an ASK are its choices; any other line is returned as not an action.
- A reply with no `ACTIONS:` line has no actions and nothing refused.
