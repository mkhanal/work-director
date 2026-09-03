# Work Director — design

Date 2026-09-04. Facts behind every choice: `docs/analysis/2026-09-04-director-primitives-and-landscape.md`.

## Goal

One place that holds roadmap, taste and work status for many repos, hands complete tasks to
separate executor sessions (Claude Code and opencode now, codex via AO), verifies their work,
gates PR creation, and turns the user's corrections into rules that travel to every repo and
team without polluting any repo's own instructions.

## Role

The director is a head of engineering, not a queue. Given a task it finds the way: reads the project's
roadmap and history in the ledger, decides scope and sequencing, briefs, verifies, and makes judgment calls
itself, asking the user only for business facts and irreversible choices.

**Cost model.** Effort is measured in agent minutes and tokens, never in human hours. A "quick fix now,
proper fix later" trade is judged at agent cost: if the proper fix is 15 agent-minutes, the debt is never
taken. Estimates that cite human effort are rejected in briefs and in executor reports.

## Decisions

| Question | Decision | Why |
|---|---|---|
| Build or adopt | Adopt a runner, build the brain | Runners are a commodity (AO, `claude --bg`, `opencode run`). Nothing holds cross-repo roadmap or learns taste. |
| Runner | AO first, native adapters as fallback | Claude + opencode + codex day one. AO API is unstable; adapter seam isolates it. |
| Executor permissions | Same class as director | Peer messages across classes are held for approval. |
| Isolation | Worktree when work becomes a PR | Read-only work runs in place. |
| Ledger | SQLite via `bun:sqlite`; markdown for what humans read | Session facts live in the runner; ledger stores ids + director-owned state. Zero deps. |
| Taste transport | Rule cards → generated plugin, opencode config, AGENTS fragment, lint presets | Cards are the single source; artifacts never drift. |
| Per-project taste | Lives in the repo; director proposes an "evolution" PR via an executor | Repo owns its rules. |

## Sub-project 1 — taste system (`taste/`, `packages/taste`, `plugin/`, `presets/`)

**Card** = `taste/cards/<category>/<id>.md`, frontmatter + body:

```
id, title, category, scope: [global | lang:<x> | stack:<x> | project:<name>],
kind: principle | practice | mechanical, status: candidate | adopted | retired,
always: bool (in compact constitution), enforce: [biome:<rule>, eslint:<rule>], evidence: [feedback ids]
```
Body: statement (≤2 lines), **Why**, **Apply**. Categories: judgment, alternatives, organisation,
types-and-schemas, defensive-coding, comments, working-method, communication.

**Build** (`bun run build`) from adopted, non-project cards:
- `plugin/` Claude plugin `taste`: `skills/taste-<category>/SKILL.md` (full cards), `hooks/` SessionStart
  injecting `dist/constitution.md` (always:true statements only, ≤2k chars). Marketplace at
  `.claude-plugin/marketplace.json` so `claude plugin marketplace add mkhanal/work-director`.
- `dist/constitution.md` — also the target of opencode global `instructions`.
- `dist/AGENTS.fragment.md` — for codex/cursor/teams, between `<!-- taste:begin/end -->` markers.
- `presets/biome/biome.json`, `presets/eslint/index.js`, `presets/tsconfig/base.json` — hand-authored;
  build verifies every `enforce:` id appears in a preset.

**Install (user scope, no repo touched):** plugin at user scope; opencode reads `~/.claude/skills`
natively, plus `instructions: ["~/work/personal/work-director/dist/constitution.md"]` in
`~/.config/opencode/opencode.json`.

**Seeds:** `~/work/ai-guidelines/analysis-and-design-guidelines.md`, agent-system constitution,
agentic-study ground rules, two product constitutions (global parts only).

## Sub-project 2 — director (`packages/wd`, `projects/`, `AGENTS.md`)

**Autonomy.** Each project has `mode: ask | auto` (global default `ask`). In `ask`, spawning, PR creation,
evolution PRs and merges wait for the user. In `auto`, the director proceeds and reports; merges still wait.
Goal state after the first month: the user supplies a roadmap and the director works it in `auto`.

**Workflows.** A project lists the workflows its repo offers (skills, commands, scripts such as `/lazyspec`,
`/review`, `bun test`). Briefs name the ones to use. When a needed workflow is missing, the director raises a
work item of kind `workflow`: in `ask` it recommends, in `auto` it builds it through an executor PR.

- `~/.work-director/projects/<name>.md` (`WD_PROJECTS`; `projects/example.md` in the repo shows the shape): frontmatter `path, runner, mode, stack[], workflows[], verify[], instructions_file, default_branch`, body = roadmap.
- Ledger tables: `work(id, project, title, kind: task|evolution|workflow, state, runner, session, worktree, created, updated)`,
  `event(id, work, kind, body, at)`, `feedback(id, text, project?, card?, source, at)`.
- States: queued → briefed → running → (needs-input | review) → soft-done → done; blocked, dropped from any.
  soft-done requires: executor report says done, verify passed, PR exists when code changed.
- `wd` commands: `add`, `brief`, `spawn`, `send`, `report`, `status [--json]`, `set`, `done`, `feedback add|list`,
  `distill` (feedback grouped by card/tag with count ≥2 → candidates). Wording of rules is the director's job.
- Runner adapter: `spawn(project, brief) → handle`, `send(handle, text)`, `status(handle)`, `transcript(handle)`,
  `attachHint(handle)`. Implementations: `ao` (`ao spawn/send/session get`), `claude` (`--bg`, JSONL transcript,
  peer message), `opencode` (`run --dir --format json`, `-s` resume, `export`).
- Brief = goal + acceptance + context (roadmap, decisions made, history from ledger) + how to work (decide ambiguities,
  two checkpoints: plan and done) + constraints + cards where scope ∩ project scopes ≠ ∅ + report format
  (`DONE|BLOCKED|NEEDS-INPUT`, files, verify output, PR url).
- Director role = this repo's `AGENTS.md`. Feedback capture: `wd feedback add` by the director; at review it scans
  executor transcripts for user turns and files them as feedback with `source=attached`.

## Sub-project 3 — team distribution

Versioned marketplace, adoption doc, per-team overlay cards (`scope: team:<x>`). Not in this pass beyond the
marketplace file.

## Testing

`bun test`. Requirements as lazyspec married tests in `packages/*/specs`. Runner adapters tested against
a fake CLI on PATH; one manual smoke against real `claude --bg` and `opencode run`, recorded in the plan.

## Out of scope

GUI. Multi-user. Cloud sessions. Editing any managed repo directly.
