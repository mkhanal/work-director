# Work Director — parallel orchestration

Date 2026-09-25. An evolution of `2026-09-04-work-director-design.md`, which stays
authoritative for taste, the ledger, runners and the director role. This document adds
what that design assumed away: one big work as many parallel tasks by many executors on a
single branch, a task list under headings, impact-based conflict handling, asks and concerns,
token-lean coordination, a promotion path from project rules to global rules, and a terminal
surface that a UI can wrap later.

## What the director adds

The director is deliberately lazy about a repo's own machinery. A project may carry any
number of skills, hooks, agent files and workflows — the director honours all of it, because
executors live in the project's own conventions, not a parallel system. Against that
background the director provides exactly three things:

1. **Every piece of work has a status.** The ledger gives every work item a lifecycle
   (queued → briefed → running → review → done / dropped), tracks it under project and epic
   and heading, and records events (spawn, report, verify, PR, decisions). Because every
   command emits `--json`, a UI tomorrow can view the same registry many ways — across
   projects, filtered by state, grouped by heading, per epic — without the director owning
   any UI.
2. **Many agents, different providers, one piece of work.** Claims, impact paths, conflict
scans, concerns and one shared branch let claude, opencode, codex and AO sessions work the same
    epic without colliding. Sessions are provider-tagged, and a single `wd epic spawn` can
    parcel its sessions across providers (`--runner claude,opencode,codex,ao`).
3. **Feedback and learning inject direction, then let it take.** Corrections become feedback
   events; `distill` and `scan` turn patterns into candidate rules. Global rules are injected
   into every session at runtime (the taste plugin/constitution, chosen by the user's
   install preferences and per-launch flags). Project-level direction lands where it must —
   in the project's own instruction files — as an evolution PR that the project accepts;
   the director never disobeys the boundary it cannot own.

## Goal

The director must orchestrate a *big work* — one goal, one branch, many tasks — that people
and different LLMs (claude, opencode, codex, AO) can work in parallel without stepping on each
other, with the work registered in a shared task list that is qualified under headings and
tracked separately per task. Every capability must live on the terminal so a UI is only a
wrapper. The system must stay token-lean and keep learning: project-specific rules accumulate,
and an incremental scan can promote them to all projects.

## Principles

| Principle | Meaning |
|---|---|
| **Chat is the surface; `wd` commands are the rail** | A human runs the director as a chat session in this repo, guided by `AGENTS.md` — conversations, statuses and todos live in the ledger so any session resumes without asking "where were we?". Every capability stays a `--json` `wd` command, so chat prompts, scheduled/scripted hands-off runs and a future real UI all drive the same rail. |
| **Work registers itself** | Any agent touching a repo logs what it is doing into the task list — claim, impact, concern, done — through `wd`, so shared state is always current without narration. |
| **One branch, many providers** | Parallel executors from any inference provider converge on one shared branch — the merge point and the PR source. Within that, an agent may work in the shared worktree or in its own worktree, spawned or created on the fly. Focus is enforced by the task list (each agent owns claimed tasks) and the impact list (paths other agents have claimed). A conflict — or anything that blocks — is raised as a concern, never silently worked around; the director runs `wd conflict` before spawns and merges and surfaces overlap to the user. |
| **Tokens are the budget** | Coordination state lives in lean ledger rows exchanged as small `wd` calls. Briefs carry only the active slice: epic goal, the task, and *other agents' active claims* — not history, not the whole backlog. The executor-facing coordination contract is a few lines (claim, impact, conflict, concern, merge), not a document. |

## Decisions

| Question | Decision | Why |
|---|---|---|
| Hierarchy shape | New `epic` kind; tasks are ordinary `work` rows with `parent` + `heading` | Tasks get ids, states, events and runners for free; the existing lifecycle, verify and PR code is untouched for non-epic work. |
| Heading is a label, not a structure | `heading` is free text on each task; the task list groups by it | Matches "cleanup" as a heading holding many separately tracked things; no fixed tree schema. Headings come and go as work does. |
| Task states | Same state machine as standalone work; task `soft-done` needs only the executor's DONE report | Per-task PRs are meaningless on a shared branch — the epic owns the PR and the verify. |
| Epic completion | Epic `soft-done` requires every task `done`/`dropped`, a DONE report, a passing verify, and a PR | The epic is the unit of verification and merge; tasks are the units of focus. |
| Coordination state lives in the ledger | `claim` and `impact` columns on `work`; a `concern` table; read/written by `wd` from any executor | Cheap rows, machine-readable, runner-agnostic. A coordination file in the repo would pollute the PR. |
| Worktrees: shared and on demand | The epic has one shared worktree on the shared branch; an agent may instead work in its own worktree — a spawned `--worktree` or one the LLM decides to create. Every worktree is registered in a `worktree` table, and the shared branch is always the merge point and the PR source | Isolation is the executor's tool, not the director's monopoly; the ledger must know every worktree so verify and conflict run in the right place. |
| Conflict detection | `wd conflict <epic>` compares impact paths of concurrently claimed tasks | An agent need not poll; it runs the scan before touching a shared path. |
| Concerns are first-class | Anyone — executor or director — raises `wd concern add`; resolution records a decision as an event on the work | The branch keeps moving: a raised concern is not a stalled agent, it is a queue item for the director. |
| Token-efficient briefs | Task briefs are slices; session briefs carry the compact open task list | Executors get only what they must know; the full backlog lives in `wd tasks`, not in context. |
| Promotion | `wd scan` generalizes `distill`: project cards with recurring or cross-project evidence become candidate *global* cards | The "every few days" incremental scan is a command today and a scheduler hook later. |
| Interaction model — chat first | The human runs the director as a chat session in this repo (AGENTS.md is the persona); the CLI is the rail it drives and the hands-off rail for scheduled work | "Like any coding agent" is how people use it; nothing about chat forces a new capability, it forces the ledger to be the memory. |
| Conversation memory and auto-resume | The ledger's events (spawn, report, note, pr) are the durable memory; on start or resume the director does `wd status` first, reconstructs, and continues — a dead executor is resumed through its last session or re-briefed from events | A chat session itself may crash; the state that matters must not live only in a transcript. |
| Clash reporting is the director's job | Before spawning or merging in an epic, `wd conflict`; overlap is surfaced to the user as a concern, never worked around; executors carry the same thin contract | The director is the arbiter — silent overlap defeats the whole coordination model. |
| Provider adapters wrap each provider's own CLI | One `Runner` interface; claude, opencode, codex are native adapters (`claude --bg`, `opencode run`, `codex exec --json`), ao is its own provider whose CLI is `ao`. Adapters forward the task with only the mechanics headless orchestration needs (detach, machine-readable output, workspace dir, session resume) plus one grant so an unattended executor can act (`bypassPermissions`/`--full-auto`) — never a flag that restricts or changes the model's functioning. A new provider is one adapter file (spawn/send/status/transcript) plus a fake runner in the testbed — no change to standards or coordination | "Any provider works" must hold — the standard is the contract, not the harness. Providers' CLIs differ and drift; adapters are the thin, isolated layer that absorbs that. A routed path (e.g. codex via ao) is simply choosing ao, whose own CLI is driven. |
| Model choice is live, never a registry | `wd models [runner]` shells the provider's own list command (`opencode models`, `codex debug models`) and prints whatever it prints; a chosen model is a plain string stored in project frontmatter and forwarded via each provider's own `--model`. A provider with no list command (claude, ao) returns none and points at its own picker | Providers ship models on their own cadence and flag free ones themselves; a catalog copied into the director is wrong the day it is written. Default mode is `auto` — proceed and report — with `ask` the explicit opt-out |
| Lazyspec is the repo's own fact | Work-director installs lazyspec in itself; a managed repo that installed it is honored through its own agent files and workflows. The director never claims "applies / doesn't"; `wd projects add` asks whether to adopt the preferred lazyspec, and a "yes" files an evolution work item that installs it in the repo | the director ever needs to gate on lazyspec itself |
| Outside the foundation four, a runner is a file of commands, not a code change | claude, opencode, codex, ao are code adapters (two-step session resolve, staged session discovery, transcript stores). Any other provider is a TOML command list in `~/.work-director/runners/*.toml` — spawn/send/status/transcript/models as shell commands with safely-quoted `{placeholders}`, session id from a regex on spawn output, status from regexes on a status command. `wd runner init` writes the starter file; `wd runner add` registers it; the registry loads specs at runtime so an added file just works | "adding any command line must be so simple a user can add a file" — a provider whose CLI produces a session id and takes text is one file, no rebuild. The four foundations need code only because their sessions are discovered, not bound, by a single command | a spec-engine runner needs richer session discovery |
| Utility libraries ship with the runtime, we do not write them | Arg parsing uses `node:util` `parseArgs` (single-dash words like `-src/…` are data, moved past `--` by a 4-line shim because wd has no short options); ids use `crypto.randomUUID`; dates are `toISOString`. Shell quoting stays hand-rolled only where no stdlib exists (spec-file values run through `bash -lc`, so replacing a formatter would be a format change, and `/bin/sh` quoting is the canonical 3-rule algorithm) | "if a parser/utility exists, don't write our own" | a runtime dep on a shell-quoting library becomes worth it |

## Data model

`work` gains:

- `kind` admits `epic`.
- `parent` text NULL — for a task, the epic id. Null for epic and standalone work.
- `heading` text NULL — grouping label within the epic (e.g. "Cleanup", "Data modeling").
- `claim` text NULL — session id (or executor tag) currently working this task.
- `impact` text NULL — newline-separated paths or areas the task will touch.

New table `concern(id PK, work TEXT NOT NULL, text TEXT NOT NULL, resolved INT NOT NULL DEFAULT 0,
decision TEXT, at TEXT NOT NULL, resolved_at TEXT)`. A concern attaches to any work; resolution
writes the decision as an event so the branch has the answer in its record.

New table `worktree(id PK, work TEXT NOT NULL, path TEXT NOT NULL, branch TEXT, kind TEXT NOT NULL,
state TEXT NOT NULL, created TEXT NOT NULL)`. `kind` is `shared` (the epic's base worktree) or
`private` (a spawned `--worktree` or one the LLM created on the fly); `state` is `active`,
`merged`, `abandoned`. The `work.cwd` column stays the session's current directory and points at
whichever worktree the session is in.

Existing rows migrate cleanly: `parent`, `heading`, `claim`, `impact` are null for non-epic work.

## Coordination workflow

**Spawn a big work.**

`wd add proj "Move Metabase → Superset" --kind epic` then `wd add proj "<task>" --epic <id> --heading "<label>"`.
The epic has a shared branch and one worktree. `wd epic spawn <epic> --count 3` attaches any
number of sessions to that single worktree; each session's brief lists the open tasks it may
claim.

**Stay focused.** An agent claims a task (`wd claim <task> <session>`), records what it will
touch (`wd impact <task> <+path>`), works only that task, and never edits a path under another
task's active impact. Before touching anything shared it runs `wd conflict <epic>`; an overlap
is a concern, not a silent edit.

**Worktrees are wherever the work happens.** An agent works in the epic's shared worktree or,
alone, in its own — spawned by `wd spawn --worktree` or created by the LLM itself. Any worktree
it uses is registered (`wd worktree attach <work> <path> [--branch x]`), so `wd verify` runs in
the right place and the ledger shows every branch in flight. Only work on the shared branch
matters for the epic's PR; a finished private worktree merges back there (`wd merge <work>`)
only after a passing verify and a clean `wd conflict` against claims merged since it branched.

**Raise and resolve.** `wd concern add <work> <text>` from any agent or the director.
`wd concern resolve <id> "<decision>"` records the decision; in `ask` mode the user decides,
in `auto` the director does and reports. Blocked work stays `blocked` until the concern that
blocks it resolves.

**Finish.** A task reports DONE and is marked `done`. When every task is done or dropped, the
director verifies on the shared branch, gates the epic's PR, and `soft-done` + `done` close it,
exactly as standalone work does today.

## Briefs as slices

Task brief = epic goal (one line) + own task title/detail/heading + the *active* claims
elsewhere (paths to avoid, as a short list) + the `wd` contract (claim, record impact, run
`wd conflict` before shared paths, raise concerns, mark done) + the existing rules, workflows,
verify and report format. The roadmap and history are in the epic brief, not repeated per task.

Epic spawn brief = goal, roadmap, decisions, and the compact open task list grouped by heading
(rendered from the ledger), plus how to claim. Agents never restate the backlog in messages —
that is what token-lean means here.

## Lazyspec adoption in projects

Lazyspec is the repo's own fact, for every repo — this one including. Work-director installs
lazyspec in itself: requirements are `## ` headings married to tests in the same edit, gated
by `/lazyspec` and `/lazyspec-validate`. A managed repo is the same: if it installed lazyspec, its
executors honor it because the brief already names the repo's own instructions file and
workflows (`/lazyspec`). The director never tells an executor lazyspec applies or not.

- `Project` and frontmatter carry no lazyspec field; a brief carries no verdict on lazyspec.
- `wd projects add <name> <path>` writes the project file, then asks whether to use the
  director's preferred lazyspec. A "yes" files an evolution work item whose detail says to
  install lazyspec in the project's own agent files (the ground-rule rail: nothing is written
  into a managed repo except by an executor, as a PR).
- Off the question's rails, non-interactive runs (`--lazyspec y|n`, or no TTY) still default
  to "no" — the repo's conventions are its own unless its owner says otherwise.

## Promotion scan

`distill` already groups feedback by card/project when a signal recurs ≥2 times. `wd scan`
extends it with a promotion pass over project-scoped cards:

- a project card whose evidence is ≥2 feedback from **different projects**, or ≥2 `attached`
  feedback (real-world recurrence), graduates to a **promotion candidate**;
- `wd scan` lists candidates with their evidence; `wd scan --adopt <candidate>` writes a new
  global candidate card into `taste/cards/<category>/<id>.md`, and `go run ./cmd/taste` regenerates
  the plugin so it reaches every repo.

The scan is the incremental, every-few-days command. A scheduler wrapper is a later hook, not
this design.

## Command surface

New and changed `wd` commands (all keep `--json`):

| Command | What it does |
|---|---|
| `wd add <proj> <title> --kind epic` | create a goal (an epic — work broken into tasks) |
| `wd add <proj> <title> --epic <id> --heading <label>` | add a task under a heading |
| `wd tasks <epic>` | task list grouped by heading, `--json` for the UI |
| `wd epic plan <epic>` | a planner session decomposes the goal into headed tasks |
| `wd epic run <epic> [--only <id,id>] [--heading <label>] [--wait]` | spawn open tasks (a slice, optionally) on the shared branch; `--wait` coordinates to done or a human |
| `wd epic review <epic>` | one coordination pass: answer / escalate / review / blocked |
| `wd epic spawn <epic> --count n` | n sessions on the epic's one branch/worktree |
| `wd attach <id> <session> [--runner r] [--ref x] [--cwd d]` | bind a conversation started outside the director as the item's live session; the coordinator drives it with its own LLM |
| `wd serve [--port n]` | loopback board over `net/http`: goal-level status, detail, live events over a WebSocket, actions wrapped through the real CLI |
| `wd tui [id]` | the same board in the terminal: goals with their rollup, one work item's ledger and its live transcript |
| `wd claim <task> [session]` | mark a task claimed / `drop` to release |
| `wd impact <task> <+path\|-path>` | record a claimed area; `--clear` resets |
| `wd conflict <epic>` | overlap scan across active claims |
| `wd worktree attach <work> <path> [--branch x]` | register a private worktree, spawned or LLM-created |
| `wd merge <work>` | merge a finished worktree into the epic's shared branch after passing verify and a clean conflict scan |
| `wd concern add <work> <text>` | raise |
| `wd concern resolve <id> <decision>` | resolve; decision becomes an event |
| `wd concern list [<epic>]` | open concerns |
| `wd scan` / `wd scan --adopt <candidate>` | distill + promotion candidates; adopt one |

Schema migration is additive-only; existing ledgers and non-epic commands keep working.

## Testing

`go test ./...`. New lazyspec requirements are married to tests in the root `*.lazyspec.md`
files, in the existing style: hierarchy (`epic` kind, parent/heading, task states, epic
completion gate), coordination (claim/impact/concern/conflict), promotion (`distill` variants
and `scan --adopt`); `scripts/lazyspec-check.sh` proves every heading is married. Smoke-tested
against real `claude --bg` and `opencode run` on one shared-branch epic, recorded in a plan doc.

## Out of scope

Multi-user. Cloud sessions. Editing any managed repo directly — coordination state lives in the
ledger and via executors only. The board (`wd serve`, `wd tui`) is a wrapper over the CLI by
construction: every action posts argv to the real `wd`, so the terminal surface owns the behaviour.

## Decisions taken during implementation

Recorded here so they can be revisited as a set, not asked one by one.

| Decision | What I chose | Revisited when |
|---|---|---|
| Parent must be an epic | `add --epic` rejects a parent that is not `kind=epic`; an epic cannot be a child | someone wants nested headings deeper than one level |
| Task completion gate | a task's `soft-done` needs only the executor's DONE report; the epic owns verify + PR | a task under an epic ever needs its own branch/PR |
| Epic completion gate | `soft-done` for an epic requires every child done or dropped + DONE + passing verify + `pr` event | an epic is merged piecemeal |
| Default runner for epic sessions | `wd epic spawn` uses the project's runner (like every other spawn); `--runner` overrides | mixed-runner epics are wanted |
| Worktree location | git worktrees under `$WD_HOME/worktrees/<project>-epic-<id>` on branch `wd-<id>`; task-private on `wd-<taskId>` off `wd-<epic>` | an executor needs a worktree inside the repo |
| Claim == session | `wd claim <id>` without a who uses the work's session; `wd report <id>` falls back to project runner + claim as session/cwd | executors want explicit identities |
| Impact overlap | identical paths or one contained in the other; only claimed, non-terminal tasks count | tracking *why* an overlap matters, not just what |
| Do-not-merge gate | `wd merge` refuses until the task's verify passes and its epic conflicts are empty; a git merge conflict is reported, never auto-resolved | merge conflicts need an arbiter |
| `wd scan --adopt` writes a candidate, never an adopted card | the new global card starts `status: candidate`; the build only ships adopted cards | promotion should be a one-step approve |
| Lazyspec presence lives in the repo, not the frontmatter | earlier "default off via `lazyspec: true`" claimed a verdict the director cannot hold; replaced by: briefs and project files say nothing, `wd projects add` asks, "yes" files an install evolution | demand grows to gate on it |
| Epic verify cwd | `wd verify <epic>` runs in the epic's shared worktree | the shared branch diverges from the worktree |
| Folded `goal` into `epic` | a goal is not a kind: it *is* an epic — work broken into tasks, `WorkKinds` has no `goal`, `wd goal *` became `wd epic plan/run/review` | two kinds for the same thing is a concept the minimal set should not carry; a goal is a runnable epic |
| Partial slices | `wd epic run --only <id,id>` / `--heading <label>` spawns just that slice; leftovers stay open | complex goals run partially across conversations; non-complex ones run in one go (`wd epic spawn`) |
| `/goal`-equivalent without the provider feature | briefs carry a Goal block; the coordinator answers `ASK:` questions from recorded decisions, escalates unknown ones to `needs-input`, harvests `STATUS: DONE/BLOCKED` | "treat things as goal" must hold even when the provider cannot drive to one; the director *is* the drive |
| Attach lets any outside conversation in | `wd attach <id> <session>` binds a provider session started outside the director as the item's live session (runner + ref + cwd); `queued → running` is reserved for it and joins the allowed transitions | everything is CLI-driven, so any conversation is identifiable and resumed with the same LLM; pretending only the director's spawns run work would hide real work |
| The board is a wrapper over the CLI | `wd serve` (stdlib `net/http` + WebSocket, zero deps) and `wd tui` (terminal) both render the ledger and post argv to the real `wd` | "every capability is a `--json` command" extends to the UI: it can never drift from the CLI, and chat/scripts/UI share one rail |
| Free-flow gist is a tracked goal | the gist of a conversation lands as an epic or task (`wd add --kind epic`), never only in chat | untracked talk is work lost; the ledger is the memory |