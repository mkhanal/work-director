# TUI (Go)

The Go TUI is the terminal adapter over the core and the ledger (`wd tui [id]`):
it replaces the TypeScript `wd ui` web board with a stateless terminal UI. It
reads the ledger and the runners' own transcript stores, and keeps no state of
its own — every frame is rendered from a fresh read.

## The Board Renders The Ledger
The board lists every goal and epic with its per-state task rollup, then the
standalone open work; each row shows id, state, project and title, and work
with a session shows its runner and session id.

## A Detail View Shows One Work Item's Ledger
Opening a work item shows its title, state, kind and project, its tasks (for an
epic), its events, its open concerns, and — when it has a session — the live
transcript of that session with the runner's status.

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
