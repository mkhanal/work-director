# Goal tree, phase 3: alignment, done-when judgement and finish lines

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans. Spec: `docs/superpowers/specs/2026-10-13-goal-tree-design.md`.
> Builds on phases 1 and 2 (`docs/superpowers/plans/2026-10-13-goal-tree-{1-ledger,2-planning}.md`);
> read what they added before starting.

**Goal:** the goal's supervisor checks course at every look against the top goal's done-when,
and corrects or pivots through structural actions; `ask` mode and any change to the top goal go
through the person; a sub-goal closes only when its done-when is judged met; a goal is done
only when its finish line is reached.

**Architecture:** the supervisor (an LLM session per goal, `internal/cli/supervise.go` +
`internal/coordinator/supervise.go`) already reads a picture and replies with `ACTIONS:`. This
phase widens the picture to the tree, adds a `COURSE:` line and structural actions, routes
gated actions through a question whose answer applies them, and makes the soft-done gate read
the finish line.

## Ground rules

As phases 1–2: `/lazyspec` for requirement changes with married tests in the same commit, test
first, full suite + lazyspec check + gofmt clean per commit, commit to `main` and push, the two
attribution lines, invariant-only comments, no `rm -rf`, do not touch `clients/`. Stop and
report on a conflict with an existing requirement.

## Tasks

### Task 1: the supervisor sees the tree and says where the goal stands

**Files:** `internal/coordinator/supervise.go` (`Picture`, `SupervisionBrief`), `internal/cli/supervise.go`
(`picture`), `coordinator.lazyspec.md` (reword `## The Supervision Brief Gives A Goal's Supervisor The Whole Picture`).

- `Picture` gains the top goal's done-when and finish line, and replaces the flat task list
  with the tree: each sub-goal (goal or task) with id, state, done-when, what it waits on,
  its current `doing` step and steps done/total, its latest report or what it last said, and
  its children indented under it. Superseded sub-goals are listed once, with why, so the
  supervisor does not re-propose them.
- The brief tells the supervisor its first duty is the top goal's done-when, and that every
  reply carries, before `ACTIONS:`, one line `COURSE: on course | correct | pivot — <why>`.
- New event kind `core.EventCourse` (`"course"`), body `<verdict>: <why>`, filed once per reply.

Reworded requirement bullets (keep the heading):

```markdown
## The Supervision Brief Gives A Goal's Supervisor The Whole Picture
- It carries the top goal's title, detail, done-when and finish line, and the decisions recorded on it.
- It lists the goal's sub-goals as a tree, each with its state, done-when, what it waits on, its current step, and its latest report or what it last said; superseded ones with why.
- It lists open concerns with their numbers and the checks the director runs.
- It asks for a COURSE line and then an ACTIONS list, naming every action.
```

```markdown
## Every Supervision Reply Says Whether The Goal Is On Course
- `ParseCourse` reads `COURSE: <on course | correct | pivot> — <why>`.
- Carrying out a reply files its course as a course event; a reply without one files `unstated`.
```

### Task 2: structural actions

**Files:** `internal/coordinator/supervise.go` (`ActionKind`, `Action`, `Line`, `action`),
`internal/cli/supervise.go` (`carryOut`), `coordinator.lazyspec.md` (reword
`## A Supervision Reply Parses Into Actions`), `cli.lazyspec.md`.

New and changed action lines (exact):

```
GOAL <parent>: <title> | done when: <d> [| after: <id>,<id>] [| split]   (replaces TASK:)
SUPERSEDE <id>: <why>                                                    (replaces DROP; works on started sub-goals too)
EDIT <id>: [title: <t>] [| done when: <d>]
AFTER <id>: <id>,<id> | none
MET <id>: <why the done-when holds>
MISSED <id>: <what is missing>
CHORE: <what the director needs done that is no sub-goal>
PROPOSE: <title> | done when: <d> | finish: <line> — <why>             (a change to the top goal)
```

`carryOut` applies them with the phase 2 `wd goal` internals (share code; do not duplicate):
- `GOAL` files a sub-goal under `<parent>` (the goal or one of its sub-goals) and starts it
  when nothing holds it back and the mode allows.
- `SUPERSEDE` supersedes and stops sessions under it.
- `EDIT`/`AFTER` change the sub-goal; an agent working on it is told.
- `MET` files `core.EventJudged` (`"judged"`, body `met: <why>`) on a sub-goal in review and
  closes it the way `closeOne` does today.
- `MISSED` files `judged` with `missed: <what>`, sends the agent what is missing and moves the
  sub-goal back to running.
- `CHORE` spawns a session in the goal's worktree named `<goal> chore`, files a spawn event
  `chore:<runner>:<session>` and a note `chore: <what>` on the goal; it files no work row.
- `PROPOSE` is Task 3's.
- `START` also accepts a sub-goal row (phase 2 plans it).

Requirements:

```markdown
## A Supervision Reply Parses Into Actions
- `SEND <id>: <text>`, `GOAL <parent>: …`, `START <id>`, `SUPERSEDE <id>: <why>`, `EDIT <id>: …`, `AFTER <id>: …`, `MET <id>: <why>`, `MISSED <id>: <what>`, `CHORE: <what>`, `PROPOSE: …`, `RESOLVE <n>: <decision>`, `ASK: <question>`, `DONE: <summary>` and `WAIT` each parse into their action.
- Listed lines under an ASK are its choices; any other line is returned as not an action.

## A Sub-Goal Closes Only When Its Done-When Is Judged Met
- A sub-goal in review closes on `MET` with the judgement filed.
- `MISSED` files what is missing, tells its agent, and puts it back to work.
- A sub-goal is never closed by its report alone.

## A Chore Runs As A Session On The Goal, Not As Work
- `CHORE` starts a session hung off the goal and files it and what it is for on the goal, adding no work.
```

Reword the existing `cli.lazyspec.md` / `coordinator.lazyspec.md` requirements that name
`TASK:`/`DROP` (grep `TASK:` and `DROP` in `*.lazyspec.md`) in the same commit.

### Task 3: what waits for the person

**Files:** `internal/cli/supervise.go`, the answer path (`wd answer` / `Ledger.Answer`; read
how an answer reaches the work first), `internal/core` (question payload), `cli.lazyspec.md`.

- In mode `ask`, the structural actions of a reply (`GOAL`, `SUPERSEDE`, `EDIT`, `AFTER`) are
  not applied: they are filed as one question on the goal, `Change the plan?`, choices
  `Apply these changes (recommended)` and `Keep the plan as it is`, with the action lines in the
  question's body and its payload. `SEND`, `MET`, `MISSED`, `CHORE`, `START` of an approved
  sub-goal, `RESOLVE`, `ASK`, `DONE` are applied as today.
- `PROPOSE`, in any mode, is filed as a question `Change course?` with the Now/Proposed lines in
  its body, choices `Change course (recommended)` and `Keep the goal as it is`, and the proposed
  title, done-when and finish line in its payload; the goal waits on it.
- Answering with the first choice applies the payload's actions or proposal (title via
  `SetTitle` with a rename note, done-when, `SetFinish` with source `person`), records the
  decision, and resumes the goal; any other answer records it and changes nothing.

```markdown
## A Plan Change Waits For The Person In Ask Mode
- In ask mode a supervisor's GOAL, SUPERSEDE, EDIT and AFTER are filed as one question with the changes, not applied.
- Answering it with Apply carries them out; any other answer leaves the plan as it is.

## A Change To The Top Goal Always Waits For The Person
- PROPOSE files a question with the goal now and as proposed, whatever the mode, and the goal waits on it.
- Answering Change course sets the title, done-when and finish line as proposed, from the person; any other answer changes nothing.
```

### Task 4: a goal is done only when its finish line is reached

**Files:** `internal/core` (`FinishReached`), `internal/ledger/ledger.go` (`SoftDone`),
`internal/cli/supervise.go` (`unfinished`, the DONE path), `ledger.lazyspec.md`,
`cli.lazyspec.md`.

```go
// FinishReached says whether this run's events show the finish line reached, and if not, what is missing.
func FinishReached(f FinishLine, run []Event) (bool, string)
```

discovery, research: a DONE report. local build: a DONE report and a passing verify. complete
PR: those and a pull-request landing. merged: a commit landing. released: a commit landing and
a note starting `released:`.

- `SoftDone` for a goal with a finish line uses `FinishReached` in place of the fixed
  report/verify/PR checks; a goal with no finish line keeps today's checks. Its tasks must be
  settled (phase 1).
- The supervisor's `DONE` is refused while any sub-goal is unsettled, any done task has no
  `met` judgement, or the finish line is not reached, naming which.

```markdown
## A Goal's Finish Line Decides What Soft Done Requires
- A goal finishing at discovery or research needs a DONE report; at a local build also a passing verify; at a complete PR also a pull request; at merged a commit landing; at released a commit landing and a released note.
- A goal with no finish line needs a DONE report, a passing verify and a pull request.

## A Goal Is Declared Done Only When Every Part Of Done Holds
- A supervisor's DONE is refused naming the first of: a sub-goal not settled, a finished sub-goal not judged met, the finish line not reached.
```

## Done when

All tasks on `main`, the suite, lazyspec check and gofmt clean.
