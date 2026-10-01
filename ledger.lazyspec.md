> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Ledger (Go)

The ledger owns the director's tables, their additive migration, its error messages
and the work state machine. It opens every ledger an earlier wd wrote.

## New Work Starts Queued
`Add` returns work in state `queued` with a `state` event recording it.

## Only Listed Transitions Are Allowed
queued→briefed→running→(needs-input|review)→soft-done→done, with blocked and dropped reachable from open states; queued→running is reserved for attaching an outside conversation that is already working; queued, briefed and blocked work goes straight to done when a human closes it; anything else fails with IllegalTransition naming both states.

## Any Event Counts As Activity
Adding an event moves the work's `updated` to that event's time.

## Soft Done Requires A Done Report And A Passing Verify
From `review`, `SoftDone` refuses (NotReady listing what is missing) until the latest report starts with DONE and the latest verify starts with pass.

## Soft Done Is Reached Only Through Its Gate
`Transition` to soft-done fails and changes nothing; only `SoftDone` moves
work there.

## Code Changes Need A Pull Request Before Soft Done
With `codeChanged` true, `SoftDone` also requires a `pr` event.

## Feedback Seen Twice Becomes A Distill Candidate
`Distill` returns groups keyed by card, else project, else global, only where two or more feedback entries share the key.

## A Roadmap Holds Items And A Goal Holds Tasks
`Add` with a parent requires the parent to exist and to be the level that takes
this child: a roadmap takes items and a goal takes tasks. Anything else is
refused naming what the parent holds, so the two levels cannot drift into a
shape no reader could draw. A goal or a roadmap cannot itself sit under another
work item, and an item with no roadmap is refused because it belongs to one.

## A Roadmap Item Becomes A Goal Without Changing Its Id
`Promote` translates an item into a goal on the same row: the kind changes, the
id does not, and the translation is recorded as an event. Work filed against an
item before it was committed — an event, a decision, a session — stays attached
to the goal it became. Promoting a row that is not an item is refused.

## A Goal Type Is Set Deliberately Or Not At All
`SetGoalType` records the classification and a decision event saying what was
claimed. A goal with no type has none: nothing is defaulted, because a type that
is wrong and reads as right is worse than a missing one. A type on work that is
not a goal is refused, and a type outside the set is not spellable.

## Work Can End Abandoned And Says Why
`Abandon` moves work to abandoned from any state that allows it, records an
abandon event whose body is the reason and the detail, and records the state
change. The reason is one of `no-pr` or `unmerged` and nothing else is
spellable, because a reason nobody can verify is not a reason. Abandoned is
terminal: work comes to rest there, and a reversal is a new decision against
the same goal rather than a transition back out.

## Concerns Resolve With A Decision
`AddConcern` records an unresolved concern; `ResolveConcern` marks it resolved with the decision and files a note event; resolving it again fails and keeps the first decision.

## Worktrees Track Path Branch And State
`AddWorktree` records an active worktree; `SetWorktreeState` moves it to merged or abandoned.

## A Worktree Records Whether The Director Made It
`AddWorktree` stores its origin, director or attached; `SetWorktreeState` can move it to removed.
A worktree stored without an origin reads as attached.

## A Goal Has At Most One Active Shared Worktree
`AddWorktree` and `SetWorktreeState` refuse a second active shared worktree
for the same work; a merged or abandoned one no longer counts.

## Conflicts Surface When Claimed Tasks Overlap
`Conflicts` pairs concurrently claimed, non-terminal tasks of an epic whose impact paths overlap, listing the overlapping paths.

## Soft Done Requires Goal Tasks Done
For a goal, `SoftDone` additionally requires every task done or dropped.

## The Ledger Persists Across Reopen
Work added to a file-backed ledger is still there after closing and reopening it.

## Old Ledgers Stay Readable
`New` on a ledger written before parents, goal types, structured decisions or the goal rename — a `work` table without parent, heading, claim, impact or goal_type, an `event` table without effective or payload, and no concern, worktree, filed or cursor tables — reads every existing row, adds the missing columns and tables in place, and leaves all stored values untouched. A row whose kind reads as `epic` reads as `goal`: the rename lives in Go rather than in a row rewrite, so a ledger from before it opens and still means what it meant. An event written before effective and payload existed reads with both absent rather than defaulted to a value, because "not separated" and "recorded the same way" are different facts and only one of them is true of an old row.

## Migration Is Additive Only
Migration adds columns that are nullable or carry a default and creates missing tables; it never drops, renames or rewrites a column or table, so a ledger migrated by the Go wd stays readable by the TypeScript wd, and opening an already-migrated ledger changes nothing.

## A Cursor Says How Far A Sweep Has Read, And Only Forwards
`Cursor` reads a named cursor for a project and a cursor that does not exist
reads as 0, so a first sweep sees everything rather than nothing.
`SetCursor` moves it forward and refuses to move it backwards: a cursor is what
a sweep has already shown a reader, and one that forgets shows the same thing
twice while hiding everything that changed since. Names are independent within
a project, so a sweep that keeps two streams keeps two independent marks.

## Evidence From A Session Knows The Work It Came From
Feedback carries a nullable `work`, set when the evidence came out of a session
running a work item and null when somebody typed a note by hand. The column is
null rather than defaulted because a note really has no work behind it, and a
feedback row pointed at the work that produced it is what a card promoted on that
evidence records its decision against — evidence that cannot be pointed back at
the thing that produced it cannot be audited against it. The reference means
feedback for a work that does not exist fails, like every other relation.

## Events Concerns And Worktrees Belong To Existing Work
`AddEvent`, `AddConcern` and `AddWorktree` for a work id that does not exist
fail and store nothing.
