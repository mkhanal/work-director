# Work Director Implementation Plan

> Compact by owner's instruction (token efficiency). Each task: files, behaviour, married lazyspec test, commit.
> Spec: `docs/superpowers/specs/2026-09-04-work-director-design.md`.

**Goal:** Taste cards → generated plugin/config/presets; `wd` director CLI with ledger and runner adapters; installed at user scope; smoke-tested against real Claude and opencode.

**Stack:** Bun 1.0, TypeScript strict, `bun test`, `bun:sqlite`, zero runtime deps.

## File map

```
taste/cards/<category>/<id>.md          rule cards (source)
packages/taste/src/card.ts              parse + validate card (frontmatter → Card)
packages/taste/src/build.ts             cards → plugin skills, constitution, AGENTS fragment; enforce-id check
packages/taste/src/cli.ts               `build` entry
packages/taste/specs/build.lazyspec.md  + test/build.lazyspec.test.ts
presets/biome/biome.json, presets/eslint/index.js, presets/tsconfig/base.json
plugin/.claude-plugin/plugin.json, plugin/hooks/hooks.json, plugin/hooks/session-start   (hand-authored)
plugin/skills/**, dist/**               generated
.claude-plugin/marketplace.json         marketplace root
packages/wd/src/ledger.ts               sqlite: work, event, feedback; state machine
packages/wd/src/project.ts              projects/<name>.md loader
packages/wd/src/brief.ts                brief composition (cards by scope)
packages/wd/src/runner/{types,claude,opencode,ao}.ts
packages/wd/src/cli.ts                  commands
packages/wd/specs/{ledger,brief,runner}.lazyspec.md + married tests
projects/example.md                     shape of a project file (real ones in ~/.work-director/projects)
```

## Tasks

- [x] **1 Cards.** Seed ~25 adopted cards across judgment, alternatives, organisation, types-and-schemas, defensive-coding, comments, working-method, communication from the four seed docs. `always: true` on ≤12. Commit.
- [x] **2 taste build.** `card.ts` parses frontmatter, rejects missing fields/bad enums. `build.ts` writes `plugin/skills/taste-<category>/SKILL.md`, `dist/constitution.md` (always statements, ≤2000 chars), `dist/AGENTS.fragment.md`; fails if an `enforce:` id is absent from presets. Married tests. Commit.
- [x] **3 Presets.** Biome (noExplicitAny, noNonNullAssertion, noUnsafeTypeAssertion, noTsIgnore, noEvolvingTypes, noEnum, noNamespace, noNestedTernary, noUselessElse, noParameterAssign, useExhaustiveSwitchCases, noBarrelFile), eslint equivalent, tsconfig base. Commit.
- [x] **4 Plugin shell + marketplace.** plugin.json, hooks.json, session-start (inject constitution), marketplace.json. `bun run build` produces skills. Commit.
- [x] **5 Ledger.** Tables + transitions per spec; illegal transition throws; `soft-done` requires report+verify(+pr). Married test. Commit.
- [x] **6 Projects + brief.** Loader validates frontmatter; brief includes goal, acceptance, matching cards, report format; rejects human-hour estimates wording in constraints. Married test. Commit.
- [x] **7 Runners.** Interface + claude/opencode/ao adapters shelling out; tested with fake `claude`/`opencode`/`ao` scripts on PATH. Married test. Commit.
- [x] **8 CLI.** `wd add|brief|spawn|send|report|status|set|done|feedback|distill|sessions`; `--json`. Test via subprocess. Commit.
- [x] **9 Install.** Marketplace add (local path) + plugin install user scope; opencode `instructions` entry in `~/.config/opencode/opencode.json`; `~/.claude/skills` not needed (plugin skills). Verify `claude plugin list`, opencode `skill` tool sees taste skills.
- [x] **10 Projects.** Four real project files in `~/.work-director/projects/`; `projects/example.md` in the repo.
- [x] **11 Smoke.** `wd add agentic-study "Report the test command"` → spawn claude runner → report → done. Same with opencode runner. Cleanup sessions. Record results in plan.
- [x] **12 AO.** Install dmg, `ao` on PATH, `ao project add`, `wd spawn --runner ao` smoke. Record.
- [x] **13 Validate.** `/lazyspec-validate`, `bun test`, typecheck. Final commit.

## Smoke results

All on 2026-09-04, real sessions, scratch ledger then copied to `~/.work-director/ledger.db`.

| Runner | Task | Result |
|---|---|---|
| claude | agentic-study, read-only "report the test command" | DONE report in the exact format on first try; found DB-down failures and a pre-existing TS18003 |
| opencode | lazy-spec, read-only "report remote/branch/skills" | DONE report, 0.5 agent-minutes |
| ao | scratch project, "reply DONE" | `ao spawn` works (`spawned session scratch-N`); no transcript via CLI, status via `session.activity.state` |
| claude, worktree | agentic-study, "make pnpm typecheck pass", no PR | Deleted empty `apps/cli` scaffold, commit on worktree branch, typecheck 0, 86 tests pass, lazyspec-validate pass, ~7 agent-minutes. Director `wd verify` re-ran both commands in the worktree. `soft-done` correctly refused: code changed, no PR (mode ask). |

Findings: (1) briefs were 6.5 KB with every global rule; now 2.8 KB, global rules ride the plugin. (2) Bun resolves executables against the startup PATH; adapters use `Bun.which` with the live PATH (a test once hit the real CLIs). (3) AO's `ao agent ls` sees claude-code and opencode; codex not installed. (4) Read-only tasks inherit repo verify commands and fail on pre-existing defects; verify should be scoped to the task's claim (filed as feedback).
