> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# TUI (Go)

The Go TUI is the terminal adapter over the core and the ledger (`wd tui [id]`):
a stateless terminal UI. It reads the ledger and the runners' own transcript
stores, and keeps no state of its own — every frame is rendered from a fresh
read.

## The Board Renders The Ledger
The board lists every goal with its per-state task rollup, then the
standalone open work; each row shows id, state, project and title, and work
with a session shows its runner and session id.

## A Detail View Shows One Work Item's Ledger
Opening a work item shows its title, state, kind and project, its tasks (for a
goal), its events, its open concerns, and — when it has a session — the live
transcript of that session with the runner's status.

## A Goal Is Readable In One Place
A goal's header names its type, or names nothing when it is unclassified rather
than letting a reader assume. The detail view then carries a **Decisions** panel
— each claim as what was asked and what was decided, with a claim another claim
undid marked withdrawn on its own line — and a **Landed** panel giving each
landing with the kind of place it reached, an unknown kind said to be unknown.
Both are read out of the events the view already holds rather than fetched again,
so a client cannot show two reads of the same ledger that disagree. They outrank
tasks and the event log when the terminal is short, because under autonomy they
are the two things a reader opened the goal for: what the loop decided in their
place, and where the work reached. A landing whose work row is not among the
goal's is still listed with its link.

## Live Transcripts Refresh From The Runner's Store
The detail view re-reads the selected session's transcript on every refresh and
shows its latest messages; new executor messages appear without restarting.

## Quitting And Relaunching Reconstructs The View
The view is a pure function of the ledger and the terminal size: two renders
from the same ledger and size are identical, and a relaunch after a ledger
change shows the change.

## Resume Re-attaches The Ledger's Session
Sending a message from the detail view continues the session recorded in the
ledger for that work item — same runner, same session id — never a new one.

## The TUI Keeps No State That Matters
The TUI persists nothing and caches nothing: every render re-reads the ledger
and the transcripts, so a killed or restarted process loses nothing.
