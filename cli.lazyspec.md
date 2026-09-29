# CLI rail (Go)

The Go CLI is a port of the TypeScript CLI (`packages/wd/src/cli.ts`): same
commands, flags and `--json` schema, same error messages and exit codes. The
`--json` rail is the contract column consumers (TUI, serve, coordinators,
executors) read: same keys, same value types.

## Work Items Serialize With The Wire Schema
Work objects carry exactly the keys id, project, title, detail, kind, state,
runner, session, ref, cwd, created, updated, parent, heading, claim, impact —
strings or null, kind and state from the closed enums.

## Ledger Objects Keep Their Column Names
Event, feedback, concern, worktree and conflict objects serialize with the
ledger's column names (id, work, kind, body, at; resolved_at; resolved), so a
consumer written against the TypeScript output reads the Go output unchanged.

## Empty Collections Serialize As Empty Arrays
A command that returns no rows emits `[]` for the collection, never null.

## Commands Compute The Same States And Values
Over the same fixture ledger, each command produces the same `--json` schema
and key values: states transition through the same machine, claims and impacts
record per task, conflicts pair overlapping claims, concerns resolve with a
decision.

## Doctor Emits Its Report On The Json Rail
`wd doctor --json` emits runners (runner, command, detected, path),
workspace (the `wd context` object) and init — path and init strings or
null.

## Doctor Exits Zero Whatever It Finds
Undetected runners and a directory outside any repo are reported, never a
failure: `wd doctor` exits 0.

## Errors And Exit Codes Match
Usage errors, illegal transitions, not-ready refusals and unknown runners
print the same message and exit 1; a failing verify exits 1 after recording.
