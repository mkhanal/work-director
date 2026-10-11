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
`GET /api/board` returns the board view: every goal with its rollup and
every open standalone work item. `GET /api/goals` returns the same data.
`GET /api/goal/<id>` returns one goal with its children, rollup and
events; unknown ids and ids that are not goals return 404.

## A Goal Serves What The Loop Decided And Where It Reached
- `GET /api/goal/<id>` also returns `claims` and `landings`, derived from the `events` in the same response.
- A claim carries its work and whether it still stands.
- A landing carries its kind, `commit` or `pull-request`.
- Both are `[]` when empty.
- A landing whose work is not among the goal's is still listed with its link.

## A Goal's Delivery Is Read In The Goal's Own Directory
- `delivery` on `GET /api/goal/<id>` is derived from git in the goal's working directory: its tasks' directory, else the goal's own.
- A goal with no working directory has no `delivery` key.

## The Board Places Every Item In One Band
Every goal and standalone item on the board carries `band`:
- `needs-you` — the item is needs-input or blocked, or is a goal with a task that is.
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
  `goal` (`id`), `work` (`id`), `events` (`id`), `conversation` (`id`, `from`)
  or `action` (`argv`).
- Its reply is `{"id": n, "result": ...}` with the value the HTTP route serves,
  or `{"id": n, "error": {"kind": k, "message": "..."}}`, `k` one of
  `not-found`, `bad-request`, `internal`.
- A line that is not a request gets an error with no `id`.
- Every ledger event, from any process, is pushed as `{"push": "event", "data": event}`.
- End of stdin ends the server without error.

## A Work Item Serves Its Session's Conversation
- `GET /api/conversation/<id>?from=<n>` and the `conversation` method return the work's session steps from index `n` on (default 0), as the runner's `conversation` gives them.
- The reply carries `total` steps, the session's `status`, and `asking`: the question the session ends waiting on, else null.
- Work with no session has status `none` and no steps.
- An unknown id is not-found; a negative or non-numeric `from` is bad-request.

## A Goal Serves What Each Working Task Is Doing Now
- `activity` on a goal maps each running or needs-input task with a session to its latest step as one line (`now`), that step's `kind`, and `asking`.
- A tool step reads as its name and what it acted on; a question as its questions; others as their first line.
- A session that cannot be read carries `error` instead.
- With no such task, `activity` is `{}`.

## A Running Session's Question Puts Its Work In Needs You
- While serving, a running work item whose session ends on a question has the question filed as a `question` event and moves to needs-input within seconds.
- A question already filed at that point of the conversation is not filed again.
- Nothing is sent to the session.

## Work Items Serve Over HTTP
`GET /api/work` lists all work items. `GET /api/work/<id>` returns one
work item; unknown ids return 404. `GET /api/work/<id>/events` returns
the events for one work item.

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
