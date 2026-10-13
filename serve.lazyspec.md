> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Serve (Go)

The serve adapter computes the views every client renders — board, goal, work,
events — and serves them over two transports: loopback HTTP and WebSocket, and
the standard streams of a client that bundles `wd`. Writes are CLI actions.

## The Server Binds To Loopback
`serve.Start` listens on 127.0.0.1 at the given port (default 8787). It
never binds to 0.0.0.0 or a public interface.

## JSON Endpoints Serve The Board
- `GET /api/board` returns the board view: every top goal with its rollup, and every open standalone work item; a goal under a goal is not on the board. `GET /api/goals` returns the same data.
- Each goal carries `progress` over its sub-goals, `needs_you` counting the open questions in its whole tree, and `finish`.
- Once done, its `finished` says where: the finish line with the landing, `Complete PR #512` for a pull request or the short commit, else the finish line alone; before then it is null.
- `GET /api/goal/<id>` returns one goal with its children, rollup and events; unknown ids and ids that are not goals return 404.

## A Goal Serves What The Loop Decided And Where It Reached
- `GET /api/goal/<id>` also returns `claims` and `landings`, derived from the `events` in the same response.
- A claim carries its work and whether it still stands.
- A landing carries its kind, `commit` or `pull-request`.
- Both are `[]` when empty.
- A landing whose work is not among the goal's is still listed with its link.

## A Goal Serves Each Report In Sections
- `GET /api/goal/<id>` returns `reports`, each report event's id with the report read into parts.
- A report's verdict is `done`, `blocked` or `needs-input`, with what its `STATUS:` line said after it.
- Its `FILES:` line becomes the paths it names, and the rest of what it says about them as a note.
- Every other line opening with an upper-case name and a colon opens a section of that name, holding the lines up to the next.

## A Goal Waiting On The Person Serves Its Question
- While a goal is in needs-input on a question nothing has answered, `GET /api/goal/<id>` returns it as `asking`: one item per question, each with its choices.
- Each task in needs-input whose session asks nothing is returned under `waiting`, with what it last asked, or else what its last report said it needs, as a question.
- Otherwise there is no `asking` key.

## A Goal's Delivery Is Read In The Goal's Own Directory
- `delivery` on `GET /api/goal/<id>` is derived from git in the goal's working directory: its tasks' directory, else the goal's own.
- A goal with no working directory has no `delivery` key.

## The Board Places Every Item In One Band
Every goal and standalone item on the board carries `band`:
- `needs-you` — the item is needs-input or blocked, or is a goal with a task that is, or whose breakdown is proposed.
- `in-flight` — queued, briefed or running.
- `paused` — paused by a person.
- `ready-to-push` — in review with no landing since its last reopening.
- `in-review` — in review with a landing since its last reopening.
- `ready-to-close` — soft-done.
- `at-rest` — done, dropped or abandoned.

Standalone items serve as `{work, band}`, goals as `{work, rollup, band}`.

## Archived Work Is Not On The Board
Archived goals and standalone items are left off the board.

## A Client Can Hold The Server On Its Standard Streams
`wd serve --stdio` reads one JSON request per line from stdin and writes one
JSON message per line to stdout:
- A request is `{"id": n, "method": m, "params": {...}}`; `m` is `board`,
  `goal` (`id`), `work` (`id`), `events` (`id`), `gate` (`id`), `conversation`
  (`id`, `from`, `agent`), `record` (`id`) or `action` (`argv`).
- Its reply is `{"id": n, "result": ...}` with the value the HTTP route serves,
  or `{"id": n, "error": {"kind": k, "message": "..."}}`, `k` one of
  `not-found`, `bad-request`, `internal`.
- A line that is not a request gets an error with no `id`.
- Every ledger event, from any process, is pushed as `{"push": "event", "data": event}`.
- End of stdin ends the server without error.

## A Work Item Serves Its Session's Conversation
- `GET /api/conversation/<id>?from=<n>` and the `conversation` method return the work's session steps from index `n` on (default 0), as the runner's `conversation` gives them.
- The reply carries `total` steps, the session's `status`, and `asking`: the question the session ends waiting on, else null.
- Where the runner can say, it carries the session's `usage` and its `subagents`.
- With `agent=<subagent id>` (`agent` on the method) the steps are that subagent's conversation; a subagent the session does not have is not-found.
- Work with no session has status `none` and no steps.
- An unknown id is not-found; a negative or non-numeric `from` is bad-request.

## A Goal Serves What Each Of Its Tasks Is Doing
- `activity` on a goal maps each task with a session, and the goal itself while its planning or supervising session is recorded, to its latest step as one line (`now`), that step's `kind`, and `asking`.
- Where the runner can say, each carries the session's `usage` and its `subagents`.
- A tool step reads as its name and what it acted on; a question as its questions; others as their first line.
- Each carries its session's `status`, `unknown` when the runner cannot say.
- A session that cannot be read carries `error` instead.
- With no task that has a session, `activity` is `{}`.

## A Goal Serves Its Sub-Goals As A Tree
- `GET /api/goal/<id>` serves `chain`, `tree` and `paths` for any goal, top or nested.
- Each node carries its work, done-when, what it waits on and why it is not moving, its owner or how many sub-goals it was planned into, its progress, its current step and steps, its judgement and how many things under it need the person.
- Tasks of an older plan that share a heading are served as one grouped node with no done-when.

## A Goal Serves What Done Means And Why It Is Not Done Yet
- `done_when`, `finish` with its source, and `progress` with superseded counted apart; a nested goal's `finish` is its top goal's.
- `not_done_because` names, in order, the open sub-goals, a done-when not yet judged met, and a finish line not reached; empty once done.

## A Goal Serves Everything Below It That Needs The Person
- `needs_you` lists each open question on the goal or under it, with the work it is on and its path.

## A Goal Serves Its Course
- `course` is the latest course event of its top goal read into a verdict, why and when; null before any.
- `superseded` lists the superseded sub-goals under it with why and when.

## A Proposed Breakdown Is Served As Proposed
- A goal serves `proposed`: true while its latest breakdown waits for `wd goal approve`, else false.
- Each queued sub-goal of a proposed goal serves `why_not_moving` `Proposed, waiting for your approval`.
- Its board entry serves `proposed` the same way, and sits in `needs-you`.
- Once approved, none of these holds.

## A Goal Serves Why It Was Broken Down So
- A goal serves `reasoning` `{why, left_out, judge_by}` from the `reasoning:` note its latest planning filed, each part null when the planner did not give it.
- `reasoning` is null before any planning, and when its latest planning gave none.

## A Change Waiting For The Person Is Served With What Would Change
- Each `needs_you` item serves `change`, null when answering it changes nothing by itself.
- A plan change serves `kind` `plan`, its `actions` as written and `lines`, each action as a sentence naming work by its title.
- A top-goal change serves `kind` `course`, `now` and `proposed` each with `title`, `done_when` and `finish`, and `why`; asked together with a plan change, it also serves that change's `actions` and `lines`.
- The finish-line question serves `kind` `finish` and the `choices` offered, in order.
- It serves `sources` `{goal, repo, taste}`, what each said, a source that said nothing null; `sources` is null when the planner gave none.

## A Course Change Serves What Was Found And What Happens To Each Sub-Goal
- A `course` or `plan` change serves `found`, what was found that calls for it, or null.
- It serves `sub_goals`, each `{work, title, status, state, why}`: the goal's open sub-goals in tree order, `superseded` with why when the change supersedes one, else `kept` with its state.
- Then each sub-goal the change adds, `new`, with null `work`, `state` and `why`.

## A Goal Serves Its Record
- `GET /api/record/<goal>` and the `record` method return a summary of counts and one entry per decision, finish choice, course check, question, answer, judgement, chore, report, delivery and supersession under the goal, newest first, ending with what the person first asked.
- Each entry says when, what, where in the tree, and whether the person or the director made it.
- A decision filed with its maker is by that maker: a reshape or done-when a person made reads `you`, one the supervisor made reads `director`.

## Sentences A Goal Serves Are Free Of Internal Terms
- The sentences wd writes for the person — `not_done_because`, each node's `why_not_moving`, `finished`, a change's `lines`, a gate's `why`, and a record entry's `what` for what the director filed — name work by its title, never its id.
- None says a report status, verify, soft-done, supersede as a verb, claim, judged, ACTIONS, STEPS, payload or worktree, or carries a session id.
- A goal short of its finish line reads `Not finished yet: ` and what has not happened, as in `the work isn't reported done, its checks haven't passed, and there's no pull request`; one not yet checked against its done-when reads `It hasn't been checked against what done means yet`.
- A finish line set reads in the record as `Finishes at a complete pull request, as your taste says`; a report as `Reported finished`, `Reported stuck` or `Asked for your input` with what it said.

## A Running Session's Question Puts Its Work In Needs You
- While serving, a running work item whose session ends on a question has the question filed as a `question` event and moves to needs-input within seconds.
- A question already filed at that point of the conversation is not filed again.
- Nothing is sent to the session.

## A Running Session's Report Moves Its Work On
- While serving, a running work item whose session ends on a `STATUS: DONE` report has it filed as a `report` event and moves to review within seconds.
- `STATUS: BLOCKED` moves it to blocked, and `STATUS: NEEDS-INPUT` to needs-input.
- A report already filed at that point of the conversation is not filed again.

## A Running Session That Stops Without A Report Is Noted
- While serving, a running work item whose session is not running and ends on neither a question nor a report has a `note` filed, `stopped without a report: ` and the first line it last said; its state stays.
- It is noted once at that point of the conversation.

## A Running Goal Is Supervised While Serving
- While serving, each running top goal (a goal with no goal above it) is supervised every few seconds, as `wd goal supervise` does.
- A sub-goal is not supervised on its own: its top goal's supervisor looks after it.

## Work Items Serve Over HTTP
`GET /api/work` lists all work items. `GET /api/work/<id>` returns one
work item; unknown ids return 404. `GET /api/work/<id>/events` returns
the events for one work item.

## A Work Item Serves The Next Gate It Waits On
- `GET /api/work/<id>/gate` and the `gate` method return the next gate on the work's way to done, the argv that passes it, and why it is next.
- Work in review waits on `verify` (`verify <id>`) until a verify after its latest report passes.
- Verified and not landed, it waits on `land` (`pr <id>`); landed, on `soft-done`.
- Soft-done work waits on `done`.
- Work in any other state waits on no gate: `gate` is empty and `argv` is `[]`.
- Each gate serves `label` for the person: `verify` reads `Check it`, `land` `Deliver it`, `soft-done` `Confirm it's finished`, `done` `Mark it done`; no gate, an empty `label`.

## Actions Dispatch To The CLI
`POST /api/action` with `{"argv": [...]}` runs the wd CLI with those
arguments and returns `{"code": N, "stdout": "...", "stderr": "..."}`, the two
streams kept apart so a `--json` document reads on its own. Every board action
is a wrapper over the CLI.

## WebSocket Serves Live Events
`GET /ws` upgrades to a WebSocket connection. The server sends a
`board` message with the current board state on connect, then sends
`event` messages whenever a ledger event is added. Clients can send
`{"type": "action", "argv": [...]}` to run a CLI action; a message that is
not one gets a `{"type": "error", "error": "..."}` reply. A plain `GET /ws`
that is no upgrade returns 400 with `{"error": "..."}`.

## A Client That Stops Reading Never Delays The Others
Every other client receives each board and event message as promptly as if
the stalled client were not connected. The stalled client is disconnected.

## Empty Collections Serialize As Empty Arrays
A command that returns no rows emits `[]` for the collection, never null.

## Errors Return JSON
An unknown path returns 404 with `{"error": "not found"}`. A bad
request returns 400 with `{"error": "..."}`, and a wrong method 405. An unknown work id returns 404
with `{"error": "no work <id>"}`.

## A Goal Serves The Files Attached Under It
- The goal view's `attachments` is every `attachment` event on the goal and its tasks, oldest first, each as `{event, work, at, name, path, size, media_type}`; `[]` when none.
- A file sent with a decision or an answer has that event's `work` and `at`, so a client shows it with that message.
- A file is served by its path only; no route returns its bytes.
