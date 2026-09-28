# Goals board UI

The `wd ui` board makes goals first-class entry points: it lists every goal (epic) with a
goal-level status rolled up from its tasks, opens each goal's tasks and events, and runs every
action through the real `wd` CLI — so the board can never drift from the CLI's behaviour.

## The Board Lists Each Goal With A Rolled-Up Status
`startUi` serves `/api/goals`: every epic with a per-state count of its tasks and a done/total
fraction, plus standalone work items separately — epics are entry points, never the reverse.

## A Goal Detail Shows Tasks And Events
`/api/goal/<id>` returns the epic, its children, the rollup and its event history; an unknown or
non-epic id is a 404, so the board cannot invent structure the ledger does not hold.

## Every Board Action Runs Through The Real Cli
`POST /api/action` runs an argv array through a CLI runner and returns `{ code, stdout }`; the
default runner spawns the real `wd` CLI with the same env, and the board's "new goal" form posts
`add <project> <title> --kind epic` — the board is a wrapper, not a second implementation.