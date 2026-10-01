> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Serve (Go)

The Go serve adapter is a loopback HTTP and WebSocket server over the core
and ledger: JSON endpoints for the board, live events and action dispatch. It
binds to 127.0.0.1 only — it is a local adapter for native clients, not a remote
server.

## The Server Binds To Loopback
`serve.Start` listens on 127.0.0.1 at the given port (default 8787). It
never binds to 0.0.0.0 or a public interface.

## JSON Endpoints Serve The Board
`GET /api/board` returns the board view: every epic with its rollup and
every open standalone work item. `GET /api/goals` returns the same data.
`GET /api/goal/<id>` returns one epic with its children, rollup and
events; unknown or non-epic ids return 404.

## A Goal Serves What The Loop Decided And Where It Reached
`GET /api/goal/<id>` also returns `claims` and `landings`, read out of the
events it already serves rather than fetched again — a client showing the goal's
decisions and its event log must not be reading the ledger twice and disagreeing
with itself. A claim carries the work it belongs to and whether it still stands; a
landing carries the kind of place the work reached, so a client can tell a change
that went straight into the product from one still waiting on a merge, which is
the difference between "reviewed" and "landed". Both arrays are `[]` rather than
absent when empty, and a landing whose work row is not among the goal's is still
listed with its link, because the link is the fact and the row is only the
caption.

## Work Items Serve Over HTTP
`GET /api/work` lists all work items. `GET /api/work/<id>` returns one
work item; unknown ids return 404. `GET /api/work/<id>/events` returns
the events for one work item.

## Actions Dispatch To The CLI
`POST /api/action` with `{"argv": [...]}` runs the wd CLI with those
arguments and returns `{"code": N, "stdout": "..."}`. Every board
action is a wrapper over the CLI.

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
