# Goal tree, phase 1: ledger

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans. Steps use `- [ ]`. Spec: `docs/superpowers/specs/2026-10-13-goal-tree-design.md`.

**Goal:** the ledger holds a goal tree. Goals nest; every goal carries a done-when and a finish
line; work can be superseded; agents' steps and sibling order are stored; a nested goal's state
is rolled up from its children.

**Architecture:** additive columns and tables in `internal/ledger/ledger.go` (`schema`,
`addedColumns`), value types in `internal/core/core.go`. No stored value is rewritten. Leaf
sub-goals stay `task` rows; `Tasks(goal)` becomes every task under the goal at any depth.

**Tech stack:** Go stdlib + `modernc.org/sqlite`, `go test`.

## Ground rules for this phase

- Requirements are written through `/lazyspec`, with their married tests in
  `internal/ledger/ledger_lazyspec_test.go`, in the same commit as the code. The heading texts
  below are exact. Run `scripts/lazyspec-check.sh` before each commit.
- Test first: write the married test, watch it fail, then implement.
- Comments state invariants only (repo taste). Match the file's existing style; errors name the
  command that would work, as existing ones do.
- `go test ./...` must pass at every commit (it takes ~3 minutes; run it in the background).
- Commit to `main` and push after each task. End commit messages with:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01DDU83dJ9DJqebGmSZVALWJ
  ```

## API this phase adds (exact)

```go
// core
const StateSuperseded State = "superseded"

type FinishSource string
const (
	FinishFromGoal   FinishSource = "goal"
	FinishFromRepo   FinishSource = "repo"
	FinishFromTaste  FinishSource = "taste"
	FinishFromPerson FinishSource = "person"
)
func ParseFinishSource(s string) (FinishSource, error)

type StepState string
const (
	StepTodo    StepState = "todo"
	StepDoing   StepState = "doing"
	StepDone    StepState = "done"
	StepDropped StepState = "dropped"
)
func ParseStepState(s string) (StepState, error)
type Step struct{ N int; Text string; State StepState }

// Work gains:
DoneWhen     *string
FinishLine   *string
FinishSource *FinishSource

// Settled: done, dropped or superseded — what a parent counts as no longer open.
func Settled(s State) bool
// RollupState is a nested goal's state from its children's: needs-input if any
// child needs input; else blocked if any is blocked; else running if any is
// briefed, running, review or soft-done; else done if every child is settled
// and at least one is done; else superseded if every child is superseded;
// else queued. No children: queued.
func RollupState(children []State) State

// ledger
func (l *Ledger) SetDoneWhen(id, text string) error           // empty text clears it to unset
func (l *Ledger) SetFinish(id, line string, src core.FinishSource) error
func (l *Ledger) Supersede(id, why string) ([]core.Work, error) // id and its open subtree
func (l *Ledger) SetSteps(work string, steps []core.Step) error  // replaces the list
func (l *Ledger) Steps(work string) ([]core.Step, error)         // ordered by N
func (l *Ledger) SetAfter(work string, waitsOn []string) error   // replaces the set
func (l *Ledger) After(work string) ([]string, error)            // ids it waits on
func (l *Ledger) Children(id string) ([]core.Work, error)        // direct children, oldest first
// Tasks(goal) now returns every task under the goal at any depth, oldest first.
```

Schema additions:

```sql
-- addedColumns
work.done_when TEXT, work.finish_line TEXT, work.finish_source TEXT
-- schema (CREATE TABLE IF NOT EXISTS, also run in migrate for old ledgers)
CREATE TABLE IF NOT EXISTS step (work TEXT NOT NULL REFERENCES work(id), n INTEGER NOT NULL,
  text TEXT NOT NULL, state TEXT NOT NULL, PRIMARY KEY (work, n));
CREATE TABLE IF NOT EXISTS after (work TEXT NOT NULL REFERENCES work(id),
  waits_on TEXT NOT NULL REFERENCES work(id), PRIMARY KEY (work, waits_on));
```

`workColumns`, `scanWork` and every SELECT of work rows carry the three new columns.

## Tasks

### Task 1: goals nest

**Files:** `internal/ledger/ledger.go` (`childKind`, `Add`, `Tasks`, new `Children`),
`ledger.lazyspec.md`, `internal/ledger/ledger_lazyspec_test.go`, callers of `l.Tasks` that mean
direct children (grep `\.Tasks(`; most mean "the goal's tasks" and want the new depth-first set).

Requirement (rewords `## A Roadmap Holds Items And A Goal Holds Tasks`):

```markdown
## A Roadmap Holds Items And A Goal Holds Sub-Goals And Tasks
- `Add` with a parent requires the parent to exist and to hold this kind: a roadmap holds items, a goal holds goals and tasks.
- Anything else is refused naming what the parent holds.
- A roadmap cannot sit under another work item, and a goal can sit only under a goal.
- An item with no roadmap is refused.
- `Tasks(goal)` returns every task under the goal at any depth, oldest first; `Children(id)` returns its direct children.
```

- [ ] Reword the heading and bullets via `/lazyspec`; rename the married `t.Run` to the new
  heading. Change the test: a goal under a goal is accepted; a goal under a roadmap is refused
  with `"under <roadmap>: a roadmap holds items, not goal"`; a roadmap under anything is refused
  with `"a roadmap cannot sit under another work item"`; an item under a goal is refused with
  `"under <goal>: a goal holds goals and tasks, not item"`; a task under a nested goal is listed
  by `Tasks(top)` and by `Children(nested)`, not by `Children(top)`.
- [ ] Run `go test ./internal/ledger -run 'TestLedger/A_Roadmap'`; expect FAIL.
- [ ] Implement: `childKind` lets a goal hold `goal` or `task`; `Add` refuses only roadmaps with a
  parent, and goals whose parent is not a goal; `Tasks` walks the subtree (reuse `subtree`) and
  keeps kind `task` rows in created order; `Children` is the old `children`.
- [ ] Update other `l.Tasks` callers only where a direct-children meaning is needed (none
  expected; verify with the full suite).
- [ ] `go test ./...` passes; `scripts/lazyspec-check.sh` passes; commit
  `ledger: a goal holds sub-goals as well as tasks`.

### Task 2: done-when and finish line

Requirement (new):

```markdown
## A Goal Says What Done Means And Where It Finishes
- `SetDoneWhen` stores a goal's done-when; an empty one clears it, and an unset one reads as absent.
- `SetFinish` stores its finish line with where it came from: the goal, the repo, taste or the person.
- A finish line with no source, or a source outside that set, is refused.
- Each change files a decision event saying what it now is.
```

- [ ] Write the married test: set and read back both; clear done-when; `SetFinish(id, "", src)`
  refused with `"a finish line names what is handed back"`; a decision event per change whose body
  is `done when: <text>` / `finishes at: <line> (from <source>)`.
- [ ] Watch it fail. Add the columns to `addedColumns`, `workColumns`, `scanWork` (parse the
  source with `core.ParseFinishSource`, failing loud on a bad stored value as goal types do), the
  setters, and the core types.
- [ ] Add the three columns to the `Old Ledgers Stay Readable` test's list of what an old ledger
  lacks, if that test builds an explicit old schema (read it first).
- [ ] Full suite, lazyspec check, commit `ledger: a goal says what done means and where it finishes`.

### Task 3: superseded

Requirements (new, plus rewording `## Only Listed Transitions Are Allowed` and
`## Soft Done Requires Goal Tasks Done`):

```markdown
## Superseded Work Keeps Its History And Counts As Settled
- `Supersede(id, why)` moves the work and every open work under it to superseded and files the reason on each.
- An empty reason is refused.
- Work already done, dropped, abandoned or superseded is left as it is.
- No transition leaves superseded.
```

In `## Only Listed Transitions Are Allowed`, add the bullet:
`- superseded is reached only through `Supersede`; a bare transition onto it is refused naming `Supersede`.`
and add `superseded` to the list of states that have no way out (append to the bullet about
`Transition` refusals: `out of superseded` → no command).
`## Soft Done Requires Goal Tasks Done` becomes:
`For a goal, `SoftDone` also requires every task under it done, dropped or superseded.`

- [ ] Married tests first (supersede a goal with a running task, a done task and a nested goal
  with a queued task: the running and queued ones and the nested goal become superseded with a
  `superseded: <why>` note each; the done one stays; `Transition(x, superseded)` fails with
  `"use Supersede to supersede work"`; `Transition` out of superseded fails as IllegalTransition;
  a goal whose only open task was superseded passes the task check of `SoftDone`).
- [ ] Implement `core.StateSuperseded` (empty transitions list), `core.Settled`, the refusal in
  `transition` (mirror the soft-done refusal), `Supersede` (mirror `Drop`'s subtree walk), and use
  `Settled` in `SoftDone`.
- [ ] grep for switch statements over states (`StateDropped`) in serve/cli/tui and give
  superseded the same treatment as dropped where a state must be named (a band, a label).
- [ ] Full suite, lazyspec check, commit `ledger: work can be superseded by a course change`.

### Task 4: a nested goal's state is rolled up

Requirement (new):

```markdown
## A Sub-Goal's State Is Rolled Up From Its Children
- A goal under a goal takes the state `RollupState` gives its children's states, whenever a child's state changes.
- The change is filed as a state event on it, and climbs to its parent while that parent is also under a goal.
- A top goal's state is never rolled up.
- A bare transition of a goal under a goal is refused, naming what sets it.
```

- [ ] Married test: top → mid (goal) → two tasks. Move one task to running: mid is running with
  a state event; top is unchanged. Move it to needs-input: mid needs-input. Settle both (one done
  through `Transition(done)` from queued is allowed for the second; drop the first): mid done.
  `Transition(mid, review)` fails with `"<mid> is a sub-goal; its state follows its children"`.
- [ ] Unit-test `core.RollupState` table-driven in a plain `core_test.go` (not married).
- [ ] Implement: after every state write (transition, drop, supersede, restore, reopen, release,
  abandon, soft-done), call one `rollUp(tx, parentID)` helper inside the same transaction.
- [ ] Full suite, lazyspec check, commit `ledger: a sub-goal's state follows its children`.

### Task 5: steps and order

Requirements (new):

```markdown
## An Agent's Steps Replace The Last Ones It Gave
- `SetSteps(work, steps)` stores the steps numbered from 1 in the order given, replacing any before.
- `Steps(work)` returns them in order with their states: todo, doing, done or dropped.
- A step with empty text, or a state outside that set, is refused and nothing is replaced.

## A Sub-Goal Waits Only On Its Siblings
- `SetAfter(work, ids)` records what the work waits on, replacing the set before; `After(work)` reads it.
- Waiting on itself, on work with another parent, or on work that already waits on it at any remove is refused naming the two.
```

- [ ] Married tests first; watch them fail.
- [ ] Implement the two tables (`schema` and `migrate`), the four methods, `core.Step`,
  `core.ParseStepState`. Replacing is delete-then-insert in one transaction.
- [ ] Extend `Old Ledgers Stay Readable` if it lists tables an old ledger lacks.
- [ ] Full suite, lazyspec check, commit `ledger: an agent's steps and a sub-goal's order`.

## Done when

All five commits are on `main`, `go test ./...` and `scripts/lazyspec-check.sh` pass, and
nothing outside `internal/ledger`, `internal/core` and the state-naming switches changed
behaviour.
