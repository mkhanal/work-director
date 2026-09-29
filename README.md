# work-director

A head-of-engineering agent for many repos. It holds roadmap, taste and work status in one place, hands whole tasks to separate executor sessions (Claude Code, opencode or codex), verifies their work, gates pull requests, and turns your corrections into rules that travel to every repo and team without polluting any repo's own instructions.

Two products, one source:

| | What | Where it runs |
|---|---|---|
| **taste** | Your engineering taste as a Claude Code plugin: an always-on constitution (≤2000 chars) plus one skill per category, generated from rule cards. Same content for opencode and, via an AGENTS.md fragment, any other agent. | Any session, user or project scope |
| **director** | `wd`: a ledger of work items and feedback, briefs that carry context and decisions, runner adapters, and the director role in `AGENTS.md`. | A Claude Code session started in this repo |

The director is lazy by design — it honours each project's own skills, hooks and agent files
— and adds only three things: **every work item has a status** (view many ways, all `--json`);
**claude, opencode and codex coordinate on one shared branch** (claims, impacts, conflicts,
concerns, verify-gated merges, even one command that parcels sessions across providers; the
model for a run is chosen live from the provider's own CLI via `wd models`, never a registry); and
**learning that injects direction** (global rules at runtime via the plugin, project-level
rules into the project's own files as a PR it accepts).

## Install the taste plugin

```
claude plugin marketplace add mkhanal/work-director
claude plugin install taste@mkhanal            # user scope: every repo, nothing written into any repo
```

opencode: add `"instructions": ["<clone>/plugin/constitution.md"]` to `~/.config/opencode/opencode.json` and symlink `plugin/skills/*` into `~/.config/opencode/skills/`. Any agent including Claude Code reads an `AGENTS.md` — paste `dist/AGENTS.fragment.md` into it (wrapped in `<!-- taste:begin/end -->` markers) to carry your taste everywhere.

Mechanical rules ship as presets in `presets/` (Biome, ESLint, tsconfig).

> Getting a team or another agent started? See **`docs/adoption.md`** — it walks through installing taste for claude/opencode/AGENTS.md, day-one `wd` usage, and the epic workflow.

## Rule cards

One file per rule in `taste/cards/<category>/<id>.md`: scope (`global`, `lang:ts`, `stack:biome`, `project:x`), kind, status, whether it is always-on, and which lint rules enforce it. `go run ./cmd/taste` regenerates the plugin and fails if the constitution exceeds its limit or an enforced rule is missing from the presets. Cards with `project:` scope never leave the repo they describe; the director proposes them as a PR to that repo instead.

## Run the director

The director is a **chat session in this repo** — open claude or opencode here and `AGENTS.md`
makes it the chat surface: it maintains statuses and todos, records decisions, auto-resumes
from the ledger after a crash, and enforces clash reporting. The `wd` CLI is the rail that
session drives — and the hands-off rail for scripted work.

```
curl -fsSL https://raw.githubusercontent.com/mkhanal/work-director/main/scripts/install.sh | sh
wd doctor                         # which runner CLIs are detected; repo state here
cp projects/example.md ~/.work-director/projects/my-app.md   # edit path, runner, mode, verify
claude                                                        # in this repo: the session is the director
```

`wd` is one static binary (Go, CGO disabled) — no runtime dependencies, nothing to build.
The binary carries the taste cards it was built with. A work-director checkout's `taste/cards`
(named by `WD_ROOT`, or found beside the binary or in the working directory) replaces them, so
an edited card reaches the next brief without a rebuild; `wd scan --adopt` needs that checkout.
`wd doctor` reports which registered runners (claude, opencode, codex, your spec files)
are detected on PATH and whether this directory is a repo. A missing runner is information:
install the ones you use. Outside a repo it offers `git init`; running it is your call.

One work item:

```
wd add my-app "Make pnpm typecheck pass" --detail "..."
wd models <runner>            # live model list from the provider's own CLI (not a registry)
wd spawn <id> --worktree --model fable  # claude --bg / opencode run / codex exec, brief injected
wd runner init <name>         # any other provider = one TOML command file, no code change
wd attach <id> <session>      # bind a conversation started OUTSIDE the director (its own LLM) to this item
wd context <id>               # cwd, worktree, branch + an OSC-8 link that opens it in your editor
wd report <id>                # files the STATUS report once, moves the item to review/blocked/needs-input
wd verify <id>                    # runs the project's verify commands in the executor's worktree
wd soft-done <id> && wd done <id> # task: needs only a DONE report; standalone: + passing verify, + PR when code changed
wd feedback add "..." --card parse-at-boundary ; wd distill
```

An epic — one branch, many parallel executors, a goal decomposed into tasks:

```
wd add my-app "Move Metabase → Superset" --kind epic   # a goal IS work broken into tasks
wd epic plan <e>              # a planner session decomposes the goal into headed tasks
wd epic run <e> --wait        # spawn open tasks on one shared branch; the coordinator answers
                              #   known questions, escalates unknowns to you, harvests DONE reports
wd epic run <e> --only <id,id> | --heading <label>     # run a slice now; leftover tasks stay open
wd epic review <e>            # one coordination pass (answer / escalate / review / blocked)
wd add my-app "Port dashboards"   --epic <e> --heading Dashboards
wd epic spawn <e> --count 3       # N parallel sessions on one shared worktree (branch wd-<epic>)
wd claim <task> <session> ; wd impact <task> <+path> ; wd conflict <e>   # who touches what, spot overlap
wd worktree attach <task> <path> ; wd merge <task>  # on-demand workspaces; merge folds back after passing verify + clean conflicts
wd concern add <task> "..." ; wd concern resolve <n> "<decision>"        # the decision queue
wd decide <id> "<decision>"       # record a decision in one line; briefs and ASK: answers read it
wd scan --adopt <candidate>       # promote a project rule to a global candidate card
```

Complex goals run partially across conversations — `wd epic run --only` works a slice and leaves
the rest open for another conversation; the coordinator drives every open task, including ones
`wd attach`ed from a provider session started outside the director, and answers their `ASK:`
questions from recorded decisions instead of guessing. Simple goals run in one go (`wd epic spawn`).

Goals are first-class entry points, not conversations. `wd tui` is the terminal board: every
goal with a rolled-up goal-level status, and each item's ledger and live transcript. `wd serve`
exposes the same board as JSON and a WebSocket event stream on 127.0.0.1:8787 for native
clients; each action runs through the real `wd` CLI, so a board can never drift from the CLI.

A task's `soft-done` needs only its DONE report; the **epic** closes when every task is done,
its DONE report is in, verify passes and a PR is recorded. Lazyspec is the repo's own fact:
work-director installs it in itself (its requirements are married to tests); a new managed
project gets the "adopt preferred lazyspec?" question at `wd projects add`, and a "yes" files
an evolution work item that installs it in that repo — the director never claims it applies.

State lives in `~/.work-director` (SQLite ledger, project files, worktrees). The repo carries no private data. Every command emits `--json` for a future UI wrapper.

## Design

`docs/superpowers/specs/2026-09-04-work-director-design.md` for the decisions; `docs/superpowers/specs/2026-09-25-work-director-parallel-orchestration.md` for the epic/coordination model (worktrees, claims, concerns, promotion, lazyspec adoption) with its recorded decisions; `docs/analysis/` for the verified primitives and the landscape survey; `docs/adoption.md` to get people started. Requirements are the root `*.lazyspec.md` files, married to `internal/<pkg>/<stem>_lazyspec_test.go`; `scripts/lazyspec-check.sh` checks every one. The CLI tests drive the built `wd` against a disposable sample repo with fake runners (`internal/cli/testdata`), no LLMs.

MIT.
