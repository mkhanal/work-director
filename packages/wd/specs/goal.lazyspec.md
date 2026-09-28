# Goals decompose and are driven to done

A goal is the director's unit of direction: a distinct work kind that behaves like an epic — it
holds headed tasks, runs them on one shared branch, and is driven by the coordinator until verified
and merged. `wd goal` is its rail: `add` records it, `plan` decomposes it, `run` drives it,
`review` harvests it, `status` shows where it stands.

## A Goal Is A Distinct Work Kind
`wd goal add <project> <title>` records a work item of kind `goal`, while `wd add --kind goal`
does the same; goals are epic-like, so tasks can sit under them, but they stay their own kind so
`wd status` and the UI can surface them as entry points.

## A Goal Plan Decomposes It Into Headed Tasks
`wd goal plan <id>` feeds the goal and the project's verify commands to the project's planner
session and files every parsed `## heading` / `- [ ] task` line as a task under it, moving the goal
from `queued` to `briefed`.

## A Goal Run Spawns Its Open Tasks On One Shared Worktree
`wd goal run <id>` starts every open task of the goal on the goal's shared worktree and leaves a
running task with a live session alone; `--wait` coordinates until tasks close or a question
reaches a human.

## A Goal Review Harvests Or Escalates Its Tasks
`wd goal review <id>` answers a task's `ASK:` from a recorded decision whose words overlap the
question, moves an unanswered question to `needs-input` without guessing, and harvests `STATUS:
DONE` transcripts into `review` and `STATUS: BLOCKED` into `blocked`.

## A Goal Status Reports Its Open Tasks
`wd goal status <id>` shows the goal's state and every open task under it.