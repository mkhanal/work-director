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
- queued→briefed→running→(needs-input|review)→soft-done→done.
- blocked and dropped are reachable from every open state.
- paused is reachable from queued, briefed, running, needs-input, review and blocked, and goes back to any of them, or to dropped or abandoned.
- queued→running is allowed, for attaching a conversation already under way.
- queued, briefed and blocked work may go straight to done.
- Any other transition fails with IllegalTransition naming both states.
- `Transition` onto soft-done, done→running or abandoned→dropped is refused naming the command that takes it: `SoftDone`, `Reopen`, `Release`.

## Finished Work Is Reopened With A Reason Or Not At All
- `Reopen(id, why)` moves done work to running and records the reason as a decision.
- An empty reason is refused, naming the reads that answer a question instead.
- Any state but done is refused.
- The first run's events stay.

## Readiness Is Judged On This Run's Evidence
`SoftDone` reads only the report, verify and landing recorded after the most
recent reopening; work never reopened is judged on all its events.

## Workspaces Are Registered, And New Ones Are Only Made Under A Parent That Allows It
- `AddWorkspace` stores an id, a name, the resolved absolute path, and whether new workspaces may be created under it; `~/work/foo` and its absolute form are one workspace.
- `Workspace` reads one by id or path; `Workspaces` lists parents first.
- `CreateWorkspacePath(parent, name)` refuses, by name: a parent not registered as one; a name that is not a single directory (a separator, `.`, `..`, an absolute path); a joined path not under the parent.

## An Abandoned Row Can Be Released Into A Choice
- `Release(id, why)` moves abandoned work to dropped and files the reason as a decision.
- An empty reason is refused.
- Any state but abandoned is refused.
- The abandon event stays.

## A Task Cannot Be Added Under A Goal That Has Come To Rest
`Add` under a done goal is refused naming `wd reopen`.

## Any Event Counts As Activity
Adding an event moves the work's `updated` to that event's time.

## Soft Done Requires A Done Report And A Passing Verify
From `review`, `SoftDone` refuses with NotReady listing what is missing until
the latest report starts with DONE and the latest verify starts with pass.

## Soft Done Is Reached Only Through Its Gate
`Transition` to soft-done fails and changes nothing.

## Code Changes Need A Pull Request Before Soft Done
With `codeChanged` true, `SoftDone` also requires a `pr` event.

## Feedback Seen Twice Becomes A Distill Candidate
`Distill` returns groups keyed by card, else project, else global, only where
two or more feedback entries share the key.

## A Roadmap Holds Items And A Goal Holds Tasks
- `Add` with a parent requires the parent to exist and to hold this kind: a roadmap holds items, a goal holds tasks.
- Anything else is refused naming what the parent holds.
- A goal or roadmap cannot sit under another work item.
- An item with no roadmap is refused.

## A Roadmap Item Becomes A Goal Without Changing Its Id
- `Promote` changes an item's kind to goal on the same row and records an event.
- Events, decisions and sessions filed on the item stay on the goal.
- Promoting anything but an item is refused.

## A Goal's Events Are Its Own And Its Tasks'
- `EventsUnder(id)` returns the events of the work and everything under it, in event order.
- For a task it returns the task's own events.
- `Events(id)` returns one work's own events.

## A Goal Type Is Set Deliberately Or Not At All
- `SetGoalType` records the type and a decision event.
- A goal never gets a default type.
- A type on work that is not a goal is refused; a type outside the set is not spellable.

## Work Can End Abandoned And Says Why
- `Abandon` moves running, needs-input, review, soft-done or blocked work to abandoned.
- It records an abandon event whose body is the reason and the detail, and the state change.
- The reason is `no-pr` or `unmerged`; nothing else is spellable.
- No transition leaves abandoned except `Release`.

## Concerns Resolve With A Decision
- `AddConcern` records an unresolved concern.
- `ResolveConcern` marks it resolved with the decision and files a note event.
- Resolving it again fails and keeps the first decision.

## Worktrees Track Path Branch And State
`AddWorktree` records an active worktree; `SetWorktreeState` moves it to merged
or abandoned.

## A Worktree Records Whether The Director Made It
- `AddWorktree` stores its origin, director or attached.
- `SetWorktreeState` can move it to removed.
- A worktree stored without an origin reads as attached.

## A Goal Has At Most One Active Shared Worktree
`AddWorktree` and `SetWorktreeState` refuse a second active shared worktree for
the same work; a merged or abandoned one does not count.

## Conflicts Surface When Claimed Tasks Overlap
`Conflicts` pairs concurrently claimed, non-terminal tasks of a goal whose
impact paths overlap, listing the overlapping paths.

## Soft Done Requires Goal Tasks Done
For a goal, `SoftDone` also requires every task done or dropped.

## The Ledger Persists Across Reopen
Work added to a file-backed ledger is still there after closing and reopening it.

## Old Ledgers Stay Readable
- `New` opens a ledger lacking any later column or table — work without parent, heading, claim, impact, goal_type or archived; event without effective or payload; no concern, worktree, filed or cursor tables.
- It reads every existing row, adds what is missing in place, and changes no stored value.
- A row stored with kind `epic` reads as `goal`.
- An event stored without effective or payload reads with both absent.

## Migration Is Additive Only
- Migration only adds nullable or defaulted columns and missing tables.
- It never drops, renames or rewrites a column or table.
- Opening an already-migrated ledger changes nothing.

## A Cursor Says How Far A Sweep Has Read, And Only Forwards
- `Cursor` reads a named cursor for a project; one that does not exist reads as 0.
- `SetCursor` moves it forward and refuses to move it backwards.
- Names are independent within a project.

## Evidence From A Session Knows The Work It Came From
- Feedback carries a nullable `work`: set when it came out of a session running that work, null when typed by hand.
- Feedback naming a work that does not exist fails.

## Dropping Work Drops Everything Open Under It With A Reason
- `Drop(id, why)` moves the work and every open work under it to dropped and files `why` as a decision on each.
- Work at rest under it keeps its state.
- With no `why`, or on work already at rest, it is refused and nothing changes.

## A Message To Work Waits In The Ledger Until It Is Delivered
- `QueueMessage(work, text)` stores the message undelivered; blank text and unknown work are refused.
- `Undelivered(work)` lists that work's undelivered messages, oldest first.
- `MarkDelivered(id)` stamps the message delivered and files a `sent` event carrying its text; a message already delivered is refused.

## Archiving Puts Work Out Of Sight With Everything Under It
- `Archive(id, why)` stamps `archived` on the work and every work under it, and files an `archived` note on each.
- Work at rest (done, dropped, abandoned) keeps its state.
- Open work is dropped first and `why` is filed as a decision on it; with no `why` the archive is refused naming the open work, and nothing is stored.
- Archiving work that is already archived is refused.

## Unarchiving Brings Work Back As It Was
- `Unarchive(id)` clears the stamp on the work and everything under it and files an `unarchived` note on each; states are unchanged.
- Unarchiving work that is not archived is refused.

## A List Can Leave Archived Work Out Or Show Only It
`List` returns all work when `Archived` is unset, leaves archived work out when it
is false, and returns only archived work when it is true.

## Events Concerns And Worktrees Belong To Existing Work
`AddEvent`, `AddConcern` and `AddWorktree` for a work id that does not exist
fail and store nothing.
