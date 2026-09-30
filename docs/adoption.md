# Adopting work-director

Two things to adopt, independently. **taste** makes your engineering rules travel with the
agent. **director** (`wd`) orchestrates many repos: work items, epics, parallel executors,
verification, PR gates.

The director is lazy by design: a project's own skills, hooks, agent files and workflows are
honoured as-is — the director never installs a parallel work system into a repo. Against that,
it adds exactly three things: **(1)** every piece of work has a status, viewable many ways
now (`wd status`, `wd tasks`, all `--json`) and in a future UI; **(2)** claude, opencode and
codex sessions coordinate on one shared branch through claims, impacts, conflicts, concerns and
verification-gated merges; **(3)** feedback and learning — global rules injected at runtime
via the taste plugin, project-level direction landing in the project's own instruction files
as a PR the project accepts.

Every capability is a `wd` command that also emits `--json`, so a UI later is only a wrapper.
Nothing to adopt ever means a rule file inside a project repo.

## 1. Taste — your rules, wherever the agent runs

Rule cards in `taste/cards/` are the single source. `go run ./cmd/taste` generates the artifacts
below; never hand-edit generated files (`plugin/`, `dist/`).

### Claude Code (claude)

```
claude plugin marketplace add mkhanal/work-director
claude plugin install taste@mkhanal
```

Install at **user scope**: the plugin is active in every project, nothing is written into any
repo. Your projects set up their own overrides.

### opencode

Point `~/.config/opencode/opencode.json` at the generated constitution and symlink the skills
(the ones under `~/.claude/skills` are read natively):

```json
{ "instructions": ["<clone>/dist/constitution.md"] }
```

```
ln -s <clone>/plugin/skills/* /Users/you/.claude/skills/
ln -s <clone>/plugin/skills/* /Users/you/.config/opencode/skills/
```

### Any agent, any team — one AGENTS.md (claude supports it too)

`dist/AGENTS.fragment.md` is a paste-into-place fragment wrapped in
`<!-- taste:begin / end -->` markers, so it can be regenerated without stomping surrounding
content:

```
<!-- taste:begin -->
<paste dist/AGENTS.fragment.md here>
<!-- taste:end -->
```

Paste it into a repo's `AGENTS.md`. **Both opencode and Claude Code read `AGENTS.md`** (Claude
in addition to `CLAUDE.md`), so one file carries your taste to every agent on the repo. Codex,
Cursor and agents on GitHub likewise have an entry point; add to `CLAUDE.md` too when a repo
only reads that.

Mechanical rules (Biome, ESLint, tsconfig) ship as presets in `presets/` — copy the files that
match your stack.

### Growing taste

```
wd feedback add "a handler swallowed the failure and logged" --card fail-loud
wd distill                      # groups repeated feedback into candidates
wd scan                         # also surfaces project rules to promote to global
wd scan --adopt <candidate>     # writes a global *candidate* card; adopt it, then go run ./cmd/taste
```

## 2. Director — orchestrate many repos from the terminal

```
curl -fsSL https://raw.githubusercontent.com/mkhanal/work-director/main/scripts/install.sh | sh
wd doctor                         # detected runner CLIs (claude, opencode, codex) + repo state
cp projects/example.md ~/.work-director/projects/my-app.md   # edit path, runner, mode, stack, verify
```

`wd` is one static binary — no runtime dependencies. `wd doctor` reports which registered
runners are detected on PATH and whether the directory is a
repo, offering `git init` when it is not. Nothing it reports is a failure.

A project file is one markdown with frontmatter and a roadmap body:

```yaml
---
path: ~/work/my-app
runner: claude            # claude | opencode | codex
mode: auto                # auto (default): proceed and report; ask: confirm PRs/merges
model:                    # optional: chosen once, e.g. fable or openai/gpt-5.2 — see `wd models`
stack: [ts, biome]
workflows: [/lazyspec]    # the repo's own agent commands
verify: [bun test, bunx tsc --noEmit]
instructions_file: AGENTS.md
---
Roadmap, most important first.
```

Create new projects from the director, and it will ask the one workflow question that matters:

```
wd projects add my-app ~/work/my-app
# Use the director's preferred lazyspec for my-app? [y/N]   → "yes" queues an evolution
# work item that installs lazyspec in the repo (requirements married to tests, changed via
# /lazyspec); "no" leaves the repo's own conventions alone.
```

**Lazyspec is the repo's own fact.** The director never claims "lazyspec applies / not here":
if a managed repo installed lazyspec, its agent files and listed workflows make executors
honor it; if not, they don't. Work-director's own requirements are married to tests because
it installs lazyspec in itself.

### Day one with one work item

Pick the mechanism, then the model — both chosen live, never from a list the director keeps:

```
wd models                             # detected runners only: whatever each provider's own CLI prints (opencode models,
                                      # codex debug models; claude has no list → their own pickers)
wd add my-app "Make the import idempotent"
wd spawn <id> --model fable           # forwards --model to the provider's CLI
wd report <id>                    # files the STATUS report once, moves it to review/blocked/needs-input
wd report <id> "<text>"           # or supply it, for work with no executor to read one from
wd verify <id>                    # runs the project's verify in the executor's worktree
wd pr <id>                        # where it landed: the pushed commit, worked out; <url> for a real pull request
wd soft-done <id> && wd done <id>
```

### Any other provider is a file, not a code change

claude, opencode and codex are code adapters; every *other* provider is one TOML command
list that `wd` runs, so adding a provider never means editing the director:

```
wd runner init myagent                # writes ~/.work-director/runners/myagent.toml — edit
                                      # the commands, placeholders {cwd} {brief} {model} {session}
                                      # are shell-quoted for you; session_id regex names the session
wd runner add myagent ~/.work-director/runners/myagent.toml
wd runner list                        # built-ins + files, where each lives, detected or not
wd spawn <id> --runner myagent --model victory/1
```

### Big work — an epic with many parallel executors on one branch

```
wd add my-app "Move Metabase → Superset" --kind epic
wd add my-app "Port dashboards"   --epic <e> --heading Dashboards
wd add my-app "Rewrite ingestion" --epic <e> --heading Data
```

- Each task is tracked **separately**, grouped under its **heading** (`wd tasks <e>`).
- `wd epic spawn <e> --count 3` puts three sessions on one shared git worktree/branch.
- Executors keep focus and the branch safe by claiming and declaring impact:

```
wd claim <task> <session>        # who owns it
wd impact <task> <+path>         # what they will touch
wd conflict <e>                  # overlapping claims, before touching a shared path
wd concern add <task> "..."      # anything blocking or conflicting — a decision queue
wd concern resolve <n> "<decision>"
```

- An executor that wants isolation makes its **own worktree** — spawned or on the fly —
  and registers it (`wd worktree attach`); `wd merge` folds it back into the epic branch
  only after a passing `wd verify` and a clean `wd conflict`.
- A task's `soft-done` needs only its DONE report. The **epic** close needs every task done,
  a DONE report, a passing verify and a PR — verification and merge happen once, on the branch.

### Where everything lives

No private data is in the repo. The ledger (SQLite), project files, feedback and worktrees
live under `~/.work-director` (override with `WD_HOME`).

## 3. If you want the UI later

Every command emits `--json` (`wd tasks <e> --json`, `wd status --json`, ...). The UI is a
reader/writer over those commands; no capability exists only in a UI. The schedule: an epic
built on the terminal first, UI as a wrapper after.