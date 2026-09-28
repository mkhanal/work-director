# Epic tasks and coordination

A goal is an epic: work broken into headed tasks. Simple goals run in one go (`wd spawn` or
`wd epic spawn`); complex ones run task-by-task on one shared branch (`wd epic run`), optionally a
partial slice at a time, with the coordinator driving every executor — including conversations that
began outside the director — toward the goal.

## A Task Lives Under An Epic And A Heading
`add` with a `parent` epic sets `parent` and `heading`; a parent that is not an epic is rejected.

## A Task Soft-Done Needs Only The Executor's Done Report
From `review`, a task soft-done never requires verify or PR; a missing DONE report still refuses.

## An Epic Soft-Done Requires Every Task And A Pull Request
From `review`, an epic soft-done refuses until all children are done or dropped, the report
starts with DONE, verify passes, and a `pr` event exists.

## Claims And Impacts Are Tracked Per Task
`setClaim` records and drops the claim; `setImpact` adds and removes newline-joined paths.

## Conflicting Impacts Surface Together
`conflicts` returns pairs of concurrently claimed, non-terminal tasks whose impact paths overlap
(identical or one containing the other).

## Concerns Resolve With A Recorded Decision
`resolveConcern` marks the concern resolved, stores the decision, and records a note event on the work.

## Worktrees Are Registered Shared Or Private
`addWorktree` records a shared/private worktree and its branch in `active` state; there are
exactly one shared and any number of private worktrees, and `setWorktreeState` moves them on.

## An Epic Spawn Can Parcel Sessions Across Inference Providers
`parseRunnerList` accepts a comma-separated runner list (`claude,opencode`), falling back to the
project runner when empty; the list cycles over the session count so one shared branch hosts
sessions from different providers at once.

## An Epic Plan Decomposes Into Headed Tasks From A Planner Session
`epicPlanBrief` restates the goal and the project's verify commands; `parsePlan` turns a planner
session's `## heading` and `- [ ] item` lines into headed tasks, ignoring other lines.

## The Coordinator Answers Known Questions And Escalates Unknown Ones
When an open task's latest transcript asks (`ASK:`), `coordinateOnce` answers it from a recorded
decision (a resolved concern's decision or a `decision` event on the epic or its children) whose
words overlap the question's, sends the answer back and records it; a question no decision answers
moves the task to `needs-input` without guessing.

## The Coordinator Harvests Done Reports Into Review
A running task whose transcript ends `STATUS: DONE` gains a report event and moves to `review`;
`STATUS: BLOCKED` moves it to `blocked`. Nothing is invented from missing transcripts.

## An Epic Run Spawns Open Tasks On One Shared Worktree
`wd epic run` starts every open task on the epic's shared worktree and resumes only the ones
without a live session; `--wait` coordinates until tasks close or a question reaches a human.

## An Epic Run Can Work A Partial Slice
`wd epic run --only <id,id>` or `--heading <label>` spawns just that slice of the open tasks and
leaves the rest open, so another conversation can finish them later.

## An Outside Conversation Can Be Attached And Driven With The Same Llm
`wd attach <id> <session>` binds an existing provider session (started outside the director) to a
work item as its live session, so the coordinator drives that same conversation — its own LLM —
toward the goal, the way it drives a spawned one.