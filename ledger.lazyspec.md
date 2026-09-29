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

## Epics Hold Tasks And Nothing Sits Under An Epic
`Add` with a parent requires the parent to exist and be an epic; an epic or goal itself cannot have a parent.

## Concerns Resolve With A Decision
`AddConcern` records an unresolved concern; `ResolveConcern` marks it resolved with the decision and files a note event; resolving it again fails and keeps the first decision.

## Worktrees Track Path Branch And State
`AddWorktree` records an active worktree; `SetWorktreeState` moves it to merged or abandoned.

## An Epic Has At Most One Active Shared Worktree
`AddWorktree` and `SetWorktreeState` refuse a second active shared worktree
for the same work; a merged or abandoned one no longer counts.

## Conflicts Surface When Claimed Tasks Overlap
`Conflicts` pairs concurrently claimed, non-terminal tasks of an epic whose impact paths overlap, listing the overlapping paths.

## Soft Done Requires Epic Tasks Done
For an epic, `SoftDone` additionally requires every task done or dropped.

## The Ledger Persists Across Reopen
Work added to a file-backed ledger is still there after closing and reopening it.

## Old Ledgers Stay Readable
`New` on a ledger written before epics — a `work` table without parent, heading, claim or impact, and no concern or worktree tables — reads every existing row, adds the missing columns and tables in place, and leaves all stored values untouched.

## Migration Is Additive Only
Migration adds nullable columns and creates missing tables; it never drops, renames or rewrites a column or table, so a ledger migrated by the Go wd stays readable by the TypeScript wd, and opening an already-migrated ledger changes nothing.

## Events Concerns And Worktrees Belong To Existing Work
`AddEvent`, `AddConcern` and `AddWorktree` for a work id that does not exist
fail and store nothing.
