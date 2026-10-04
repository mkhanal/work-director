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
queued→briefed→running→(needs-input|review)→soft-done→done, with blocked and dropped reachable from open states; queued→running is reserved for attaching an outside conversation that is already working; queued, briefed and blocked work goes straight to done when a human closes it; anything else fails with IllegalTransition naming both states. Three edges are reachable only through the command that checks why: soft-done through `SoftDone`, done→running through `Reopen`, and abandoned→dropped through `Release`, so a plain `Transition` onto any of them is refused naming the command that takes it.

## Finished Work Is Reopened With A Reason Or Not At All
`Reopen(id, why)` returns done work to running and records a decision saying what is being worked on. The reason is required, because the ledger cannot tell the two reasons someone arrives at a finished goal from a state change: asking it a question, which changes nothing, and building more on top of it, which does. A reopen with no reason is refused and names the reads that answer a question instead. `Reopen` refuses any state but done — dropped and abandoned are the two ways of saying a thing is finished with, on purpose — and `Transition` refuses done→running outright, so no caller can move a finished goal back to life without saying what for. Reopening leaves the first run's events in place: the events are the history, and a goal's second run reads against the one before it.

## Readiness Is Judged On This Run's Evidence
`SoftDone` reads the report, verify and pull request recorded after the most recent reopening, ignoring the earlier run's. A reopened goal still holds the evidence that closed it, and a goal reopened with no new work would otherwise walk straight back to soft-done on the strength of a run already shipped — a completion nobody did. Nothing else moves: work never reopened is judged on all of its events.

## Workspaces Are Registered, And New Ones Are Only Made Under A Parent That Allows It
A workspace is a directory this director serves, with an id, a name, a resolved absolute path, and whether it is a parent under which new workspaces may be created. `AddWorkspace` registers one, storing the path resolved so `~/work/foo` and its absolute form are one workspace rather than two rows that disagree; `Workspace` reads one by id or by path, and `Workspaces` lists parents first. `CreateWorkspacePath(parent, name)` is the only place a new workspace path is computed, and it refuses three things by name: a parent that is not registered as one, a name that is not a single directory (a separator, `.`, `..`, or an absolute path), and a joined path that is not still under the parent. The containment is checked rather than assumed because the name arrives from a client that is very often a remote one, and building the path ourselves is not the same as it being inside the parent.

## An Abandoned Row Can Be Released Into A Choice
`Release(id, why)` moves abandoned work to dropped and files the reason as a decision, and refuses without one. Abandoning records that a thing stopped without shipping, and for a duplicate whose work landed under another id, or a probe that was never meant to ship, that sentence is false of it. Reclassifying a failure as a choice is a claim, and the only thing standing between the two is what the caller says, so it is required and filed as a decision rather than an edit: the abandon event stays, because work really did not ship, and the row simply stops claiming the work stopped. `Transition` refuses abandoned→dropped outright, so no caller can take that edge without a reason, and `Release` refuses any state but abandoned — it does not lead back to running, because work that never shipped does not become work in progress by being relabelled.

## A Task Cannot Be Added Under A Goal That Has Come To Rest
`Add` with a parent that is done is refused naming `wd reopen`. A task under a finished goal would sit there with nothing to run it — the goal is not open, so no pass drives it — and the rollup would show open work under finished work. Reopening first is what puts the goal back in the loop, and it costs one command.

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

## A Goal's Events Are Its Own And Its Tasks'
`EventsUnder` returns the events of a work item and everything under it, in
event order, from one query: a goal's own row carries the spine — promoted,
running, ended — and every decision, landing and report in between is filed on
the task that caused it, because that is what the row is for. So reading only
the goal's row shows a goal that decided nothing however much it decided, and
reading only the tasks' shows tasks with no parent. For a task, which has
nothing under it, this is its own events. `Events` still reads one work item
alone, for the surfaces that want one row's log.

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
