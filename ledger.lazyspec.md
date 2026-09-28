# Ledger (Go)

The Go ledger keeps the schema and semantics of the TypeScript ledger it ports
(`packages/wd/src/ledger.ts`): same tables, same DDL, same additive migration, same
error messages, same state machine.

## New Work Starts Queued
`Add` returns work in state `queued` with a `state` event recording it.

## Only Listed Transitions Are Allowed
queued→briefed→running→(needs-input|review)→soft-done→done, with blocked and dropped reachable from open states; queued→running is reserved for attaching an outside conversation that is already working; anything else fails with IllegalTransition naming both states.

## Soft Done Requires A Done Report And A Passing Verify
From `review`, `SoftDone` refuses (NotReady listing what is missing) until the latest report starts with DONE and the latest verify starts with pass.

## Code Changes Need A Pull Request Before Soft Done
With `codeChanged` true, `SoftDone` also requires a `pr` event.

## Feedback Seen Twice Becomes A Distill Candidate
`Distill` returns groups keyed by card, else project, else global, only where two or more feedback entries share the key.

## Epics Hold Tasks And Nothing Sits Under An Epic
`Add` with a parent requires the parent to exist and be an epic; an epic or goal itself cannot have a parent.

## Concerns Resolve With A Decision
`AddConcern` records an unresolved concern; `ResolveConcern` marks it resolved with the decision and files a note event.

## Worktrees Track Path Branch And State
`AddWorktree` records an active worktree; `SetWorktreeState` moves it to merged or abandoned.

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
