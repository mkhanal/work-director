# work-director

A head-of-engineering agent for many repos. It holds roadmap, taste and work status in one place, hands whole tasks to separate executor sessions (Claude Code, opencode, or Agent Orchestrator), verifies their work, gates pull requests, and turns your corrections into rules that travel to every repo and team without polluting any repo's own instructions.

Two products, one source:

| | What | Where it runs |
|---|---|---|
| **taste** | Your engineering taste as a Claude Code plugin: an always-on constitution (≤2000 chars) plus one skill per category, generated from rule cards. Same content for opencode and, via an AGENTS.md fragment, any other agent. | Any session, user or project scope |
| **director** | `wd`: a ledger of work items and feedback, briefs that carry context and decisions, runner adapters, and the director role in `AGENTS.md`. | A Claude Code session started in this repo |

## Install the taste plugin

```
claude plugin marketplace add mkhanal/work-director
claude plugin install taste@mkhanal            # user scope: every repo, nothing written into any repo
```

opencode: add `"instructions": ["<clone>/plugin/constitution.md"]` to `~/.config/opencode/opencode.json` and symlink `plugin/skills/*` into `~/.config/opencode/skills/`. Other agents: paste `dist/AGENTS.fragment.md` into `AGENTS.md`.

Mechanical rules ship as presets in `presets/` (Biome, ESLint, tsconfig).

## Rule cards

One file per rule in `taste/cards/<category>/<id>.md`: scope (`global`, `lang:ts`, `stack:biome`, `project:x`), kind, status, whether it is always-on, and which lint rules enforce it. `bun run build` regenerates the plugin and fails if the constitution exceeds its limit or an enforced rule is missing from the presets. Cards with `project:` scope never leave the repo they describe; the director proposes them as a PR to that repo instead.

## Run the director

```
bun install
ln -s "$PWD/packages/wd/src/cli.ts" ~/.local/bin/wd
cp projects/example.md ~/.work-director/projects/my-app.md   # edit path, runner, verify
claude                                                        # in this repo: the session is the director
```

```
wd add my-app "Make pnpm typecheck pass" --detail "..."
wd spawn <id> --worktree          # claude --bg / opencode run / ao spawn, brief injected, nothing written to the repo
wd report <id>                    # reads the executor's STATUS report, moves the item to review
wd verify <id>                    # runs the project's verify commands in the executor's worktree
wd soft-done <id> && wd done <id> # refuses without a DONE report, a passing verify, and a PR when code changed
wd feedback add "..." --card parse-at-boundary ; wd distill
```

State lives in `~/.work-director` (SQLite ledger, project files). The repo carries no private data.

## Design

`docs/superpowers/specs/2026-09-04-work-director-design.md` for the decisions, `docs/analysis/` for the verified primitives and the landscape survey (what `claude --bg`, opencode and Agent Orchestrator can and cannot do), `docs/superpowers/plans/` for how it was built and the smoke results. Requirements are lazyspec files married to tests under `packages/*/specs`.

MIT.
