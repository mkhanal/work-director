# Work Director — verified primitives and landscape (2026-09-04)

Facts checked on this machine (Claude Code 2.1.259, opencode installed, codex not installed, no tmux).

## Hand-off primitives that exist today

| Need | Primitive | Verified |
|---|---|---|
| Spawn a full session in a project, detached | `claude --bg -n "<name>" [flags] "<prompt>"` run with cwd = project. Prints short id. | Yes — spawned, ran, exited "done", conversation kept |
| Human joins/leaves the same session | `claude attach <id>` (Ctrl+Z detaches, keeps running); `claude agents` lists all | Yes (help text; sessions listed with status/state) |
| Director sends a message into a running session | `SendMessage` to the peer session name; `notify_when_idle: true` returns one idle notice | Yes — but **held for human approval when permission-mode classes differ** (plan vs bypass). Same class delivers automatically. |
| Fallback message channel | `claude --bg --resume <session-id> "<prompt>"` | Yes — with *no flags* it continues the same session; *with flags* it starts a copy under a new id |
| Read what the executor did, cheaply | `~/.claude/projects/<cwd-slug>/<session-id>.jsonl` (`type: assistant` lines carry text; `type: system` carry hook/meta) | Yes. `claude logs` is raw ANSI, not for parsing |
| Inject brief + taste without touching the repo | `--append-system-prompt "<text>"` | Flag exists (not exercised) |
| Inject hooks (Stop/SessionEnd report-back) without touching the repo | `--settings '<json>'` per launch | Flag exists (not exercised) |
| Inject director skills into executor only | `--plugin-dir <path>` per launch | Flag exists |
| Isolation per task | `--worktree [name]`; `claude rm` deletes session + worktree | Flag exists |
| Cost/effort control | `--effort`, `--model`, `--max-budget-usd` (print mode only) | Flags exist |
| Pure programmatic drive (later, daemon path) | `-p --output-format stream-json --input-format stream-json`, Agent SDK | Flags exist |
| opencode equivalents | `opencode run --dir <p> -s <sid> --format json --agent <a>`; `opencode serve` HTTP/SSE + SDK; `opencode acp`; `opencode export <sid>` | CLI present |
| codex equivalents | `codex exec --json` (+ `resume`), `codex app-server` JSON-RPC; `mcp-server` being retired | Not installed; from docs |

## Existing taste material (seed for `taste/`)
- `~/work/ai-guidelines/analysis-and-design-guidelines.md` — 14 project-agnostic analysis/design rules.
- `~/work/agentic-development/agent-system/core/CLAUDE.md` — anti-defensive-coding rules, comment policy, tiered gates, secrets boundary.
- `~/work/personal/agentic-study/AGENTS.md` — TypeScript taste: domain types, Zod at boundary, no `as`/`any`/`!` without a why, relations in schema, smallest model, no framework.
- lazyspec convention (own project) — some repos marry requirements to tests.

## Community landscape (Sept 2026) — what to borrow
- **beads/bd** (Gas Town): issue store as JSONL in git + SQLite cache, hash ids. Borrow: git-backed ledger with local index.
- **Symphony** (OpenAI): issue tracker as control plane; daemon restarts stalled agents; one workspace per issue. Borrow: ledger is the source of truth, director restarts/re-briefs.
- **Trellis**: centralized spec library propagated to many repos. Borrow: taste lives centrally, injected per launch.
- **Ralph loops**: re-feed the brief until acceptance criteria met; progress in files/git, not context.
- **Paperclip / Multica**: budgets, atomic task checkout, org chart. Heavy; GUI-first; taste not first-class.
- **ccmanager / Claude Squad / Conductor / Superset**: session runners (TUI/GUI), most tmux+worktree. They run sessions; none carries roadmap, taste, or verification. That gap is the director.
- Dead/sunsetting: vibe-kanban (sunsetting), crystal (deprecated), Overstory (archived).
- Interop standard: **ACP** (Zed) — Claude Code (adapter), Codex (adapter), opencode (native). `session/load` replays history to a new client. Candidate long-term adapter contract.

## Does something already do this? (checked 2026-09-04)

**Untrivial-ai/agent-orchestrator (AO)** — closest existing tool for the *execution* half.
- Electron + React desktop app over a Go daemon (loopback HTTP/SSE/WS), `ao` CLI, Apache-2.0, ~10.9k stars, created Feb 2026, pushed daily (last 2026-09-03), 760 open issues.
- 26 agent CLIs incl. Claude Code, Codex, opencode. One git worktree per worker. TUI sessions run under tmux (macOS: detached PTY host); "Chat" sessions via ACP-style driver.
- Multiple repos as "projects", side by side. SQLite (goose/sqlc) with change-log → SSE.
- Human can open any worker: continue conversation, attach terminal, inspect diff, hand off between TUI and Chat mode.
- SCM observer polls GitHub CI/review state and nudges the worker; merge/resolve-comments are explicit API actions.
- Has a per-project "project orchestrator" agent that plans, spawns, redirects and follows workers.
- **Missing for us:** orchestrator is scoped to one repo (no cross-project director); no preference/taste learning; no documented shared-guideline injection across repos (unverified negative).
- Config: `ao project add`, `ao project set-config`, `ao spawn --project X --agent codex`.

Others (Gas Town's Mayor is the only other real manager-LLM; Multica has team skill reuse; Claude Squad/ccmanager are runners; vibe-kanban stale since Apr 2026). None learns a personal style or holds a cross-repo roadmap. That gap is the director's job; the runner half is a commodity.

### AO, second pass (docs read directly)
- `ao` is a Go/Cobra client for the loopback daemon (`~/.ao/running.json`, port 3001, SQLite in `~/.ao/data`). `ao spawn [--project] [--agent] [--args ...]` → `POST /api/v1/sessions`; `ao send` → `POST /api/v1/sessions/{id}/send`; `ao session ls/get`, `ao orchestrator ls`, `ao review trigger/submit`, `ao pr merge/resolve-comments`. Session activity states: active, idle, waiting_input, blocked, exited, via 5s reaper probes + SSE `session.updated`.
- Daemon can run without Electron today, but the project is deliberately moving to "desktop app owns the daemon"; npm `ao` is a deprecated on-ramp that now opens the desktop app. Nightly releases (v0.12.11-nightly), 760 open issues.
- Data model has `projects`, `sessions`, `pull_requests`, `pr_checks`, `conversations`. **No task/work-item table.** Orchestrator is one per project; no documented programmatic brief intake, no per-project system prompt/env config, no OpenAPI or API stability statement.
- Cross-repo taste could only ride on `--args` passed to the agent CLI (e.g. `--append-system-prompt`); not documented as a supported path.
- Other list entries claiming a manager LLM: Agent Teams, corellis ("governance for fleets with memory"), Agon, LoopTroop, loki-mode. Claiming adaptive behaviour: toryo ("trust-based delegation with quality ratcheting"), hermes-agent. None checked in depth; none combine cross-repo roadmap + personal taste learning + multi-CLI hand-off.

## opencode primitives, verified 2026-09-04 (opencode 1.18.22)
- `opencode run --dir <project> --format json --title <t> "<prompt>"` runs headless, emits JSON events with `sessionID`. Sessions persist; `opencode session list`, `opencode export <sid>` returns full history as JSON.
- `opencode run -s <sid> "<follow-up>"` continues the same session (verified: READY then PONG in one session). `--fork` forks.
- Human attach: `opencode -s <sid>` opens the TUI on that exact session; `opencode attach <url> -s <sid>` does the same against a running server.
- `opencode serve --port N` exposes an OpenAPI 3.1 API (`/doc`): `POST /api/session` with `{location:{directory}}`, `POST /api/session/{id}/prompt` with `{prompt:{text,files,agents}, delivery:"steer"|"queue"}`, `GET /api/session?directory=`, `/api/session/{id}/message`, `/event` SSE, `/api/session/{id}/wait`. **No per-message system prompt** in PromptInput; taste goes in via config, brief goes in as the first message.
- Config precedence: global `~/.config/opencode/opencode.json` → `OPENCODE_CONFIG` file → project `opencode.json` → `.opencode/` → `OPENCODE_CONFIG_CONTENT` inline (highest). `instructions: ["~/path/*.md"]` may point outside the repo. Custom agents with `prompt` live in `~/.config/opencode/agents/*.md`. So taste is injected at **user level or per-launch env**, never in the repo.
