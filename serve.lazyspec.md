# Serve (Go)

The Go serve adapter is a loopback HTTP and WebSocket server over the core
and ledger. It is a port of the TypeScript `wd ui` (`packages/wd/src/ui.ts`):
same JSON endpoints, same board data, same action dispatch. It binds to
127.0.0.1 only — it is a local adapter for native clients, not a remote
server.

## The Server Binds To Loopback
`serve.Start` listens on 127.0.0.1 at the given port (default 8787). It
never binds to 0.0.0.0 or a public interface.

## JSON Endpoints Serve The Board
`GET /api/board` returns the board view: every epic with its rollup and
every open standalone work item. `GET /api/goals` returns the same data.
`GET /api/goal/<id>` returns one epic with its children, rollup and
events; unknown or non-epic ids return 404.

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
`{"type": "action", "argv": [...]}` to run a CLI action.

## Empty Collections Serialize As Empty Arrays
A command that returns no rows emits `[]` for the collection, never null.

## Errors Return JSON
An unknown path returns 404 with `{"error": "not found"}`. A bad
request returns 400 with `{"error": "..."}`. An unknown work id returns
404 with `{"error": "no work <id>"}`.
