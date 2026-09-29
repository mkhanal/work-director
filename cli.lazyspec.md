> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# CLI rail (Go)

The CLI's commands, flags, `--json` schema, error messages and exit codes are
its contract. The `--json` rail is the contract column consumers (TUI, serve,
coordinators, executors) read: same keys, same value types.

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

## Runner List Shows Which Runners Are Detected
`wd runner list` shows every registered runner, whether it is built in, and
whether it is detected with its path; `--json` emits runner, command,
builtin, detected, path.

## Models Covers Only Detected Runners
`wd models` lists models for detected runners only; naming an undetected
runner fails with the not-detected guidance.

## Spawning To An Undetected Runner Fails Before Anything Changes
`wd spawn` and `wd epic plan|spawn|run` to an undetected runner exit 1 with
the not-detected guidance; the work item's state and worktrees stay as they
were.

## Status Marks Work Stale After Thirty Days Without Activity
Work that is neither done nor dropped and whose last activity — a state change
or any event — is over 30 days old is marked stale on its `wd status` row.
`--json` rows carry every work key plus `stale`, a boolean.

## A Human Closes Queued Briefed Or Blocked Work
`wd done` closes queued, briefed and blocked work directly; running,
needs-input and review work still refuses with the illegal-transition message.

## Commands That Need Taste Cards Say Where They Looked
With no `WD_ROOT` and no `taste/cards` beside the binary or in the working
directory, `wd brief`, `wd spawn` and `wd scan` exit 1 naming `WD_ROOT` and
each directory searched; the work item stays as it was. Commands that read no
cards run from any directory.

## Errors And Exit Codes Match
Usage errors, illegal transitions, not-ready refusals and unknown runners
print the same message and exit 1. A verify whose commands ran and failed
exits 1 after recording the failed verify.

## A Decision Is Recorded In One Line And Reaches The Brief
`wd decide <id> <text>` records a `decision` event on existing work. The
work's brief lists its decisions and its resolved concerns' decisions under
"Decisions already made", and nothing else there.

## Every Json Command Writes One Document
With `--json`, every command writes exactly one JSON document to stdout — no
progress lines, no document per spawned session. `wd tui`, which draws the
terminal, refuses `--json` and writes nothing to stdout.

## Projects Add Never Writes A File It Cannot Parse
`wd projects add` with a path or flag value its project file cannot hold —
an unknown mode, a line break, a value that would read back changed — exits
1 naming it and writes no file, so every later command still loads the
projects.

## Attach Records The Ref And Cwd It Is Given
`wd attach <id> <session> --ref <ref> --cwd <dir>` records that session,
ref and cwd on the work item.

## Set Cannot Skip The Soft Done Gate
`wd set <id> soft-done` passes the same readiness gate as `wd soft-done`:
work that is not ready exits 1 with the not-ready message and keeps its
state.

## Verify With No Commands Is Refused Without Recording
`wd verify` on a project that lists no verify commands is a refusal, not a
failed verify: it runs nothing, exits 1 saying so and records no verify
event.

## An Adopted Card Leaves The Promotion Candidates
After `wd scan --adopt <card>` writes the global candidate, `wd scan` exits 0
without listing that card, and adopting it again exits 1 with `no promotion
candidate <card>`, leaving the written candidate as it was.

## Epic Run Spawns Only Children Not Yet Under Way
`wd epic run` spawns the open children that are queued or briefed, or
running with neither session nor claim. Children in review, needs-input,
blocked or soft-done, and running children with a session or a claim, keep
their state, session and claim. It reports how many it spawned; a run that spawns none leaves the
epic's state as it was.

## Send And Report Reach A Child Where Its Epic's Pass Does
`wd send` and `wd report` on a child with no session of its own reach its
claim, with its epic's runner else its project's, in its epic's active
shared worktree else the project's path: the session `wd epic review`
coordinates with.

## A Claim Names Someone
`wd claim <id>` on work with no session, and `wd claim <id> ""`, exit 1
with the claim usage and leave the claim as it was.

## Commands On Unknown Work Fail Naming It
Every command given a work id that does not exist — reading or writing —
exits 1 with `no work <id>` and records nothing.

## Flags That Do Not Parse Fail
A value flag with no value (`--tail=`, or `--ref` last or before another
flag), a switch given a value (`--json=x`), and a numeric flag that is not a
positive integer (`--tail`, `--timeout`, `--port`, `--count`) exit 1 naming
the flag; none falls back to its default.

## Report Without A Status Line Files Nothing And Fails
`wd report` on work whose last transcript message carries no STATUS line exits
1 saying the executor's report has not arrived yet; it records no event and
leaves the state as it was. A STATUS line of NEEDS-INPUT files its report and
exits 0.
