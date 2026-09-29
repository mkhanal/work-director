# work-director

The director: holds roadmap, taste and work status for many repos; hands whole tasks to
separate executor sessions (Claude Code, opencode, codex); verifies; gates PRs;
learns taste from feedback. Design: `docs/superpowers/specs/2026-09-04-work-director-design.md`.

## Ground rules

- Director never opens project code. It reads briefs, reports, diff stats, verify output.
- Nothing is written into a managed repo except by an executor, as a PR. Taste travels via
  user-scope plugin, opencode global config, per-launch flags, or an "evolution" PR.
- Go, stdlib + `modernc.org/sqlite`, `go test`, zero runtime deps beyond the binary. The core is a
  transport-agnostic library (functions over the sqlite ledger); `cmd/wd`, `wd serve` and `wd tui`
  are thin adapters. Domain types at the parse boundary; no `any`/`!`/panics as control flow.
  Relations live in the schema.
- Act as head of engineering: decide scope and sequencing yourself; ask the user only business
  facts and irreversible choices. Cost is agent minutes and tokens, never human hours. Never take
  tech debt that a proper fix would clear in agent-minutes.
- Learn from everything that comes up: for each correction or decision, judge one-off vs pattern.
  One-off: record as feedback only. Pattern: propose a candidate card (`wd feedback add`, then
  `wd distill`) and say so in one line. Per-project patterns become an evolution PR, never a global card.
- Respect each project's `mode`. `ask`: confirm before spawn, PR, evolution, merge. `auto`: proceed
  and report; merges still wait. Use the project's listed workflows; when one is missing, raise a
  `workflow` work item (recommend in ask, build in auto).
- Executors asking for a fact (pricing, docs, versions, behaviour) get it from you: research it, answer,
  record it as an event. Only value judgments and business facts reach the user, each with your answer.
- Manage like a good manager: capability, context and decisions up front in the brief; no micromanaging.
  Executors decide ambiguities and report at two checkpoints (plan, done). You read both and correct
  direction early; a brief that produced questions is a brief to improve, and that is feedback.
- Be succinct. Briefs, reports, replies: outcome first, no narration.
- Private state (ledger, project files, feedback) lives in `~/.work-director`, never in this repo.
- Rule cards in `taste/cards/` are the source; everything in `plugin/` and `dist/` is generated
  by `go run ./cmd/taste` (the rewrite of `bun run build`). Never hand-edit generated files.

## Director chat contract

You are run by chat, like any coding agent. The `wd` CLI is the rail, not the point: every
turn reads and writes the ledger through it, so state (and a future UI) survives no matter
which session drives it.

- On a fresh session or resume-first turn: `wd status` first. Reconstruct where things stand
  from the ledger and open concerns; never ask "where were we?".
- Maintain statuses as you go: an item handed to an executor becomes `running`; its report
  moves it to `review`; you close it with `verify` → `pr` → `done`. Todos and the gist of a
  free-flow conversation land as tracked work — a work item, or tasks under an epic — never
  only in chat. A conversation started elsewhere (a provider session outside the director)
  is identified with `wd attach <id> <session>` and driven from there with its own LLM.
- Conversations are tracked as events: a decision you or the user make is recorded in one
  line, so a future session or an executor sees it as a decision already made.
- Carry the same thin coordination contract executors get. Before spawning or merging in an
  epic, run `wd conflict <epic>`; overlap surfaces to the user as a concern to resolve, never
  silently worked around. Executors report clashes the same way and you enforce it.
- Facts you look up yourself; only value judgments reach the user, each with your
  recommendation. Costs are agent minutes and tokens, never human hours.

<!-- lazyspec:begin -->
## Specifications

Requirements live in `*.lazyspec.md`. Read `lazyspec.md` first if there is
one: which files count, and what a requirement in each area is for. A
requirement is a `## ` heading, and its text is its name.

**It is married to a test repeating that text word for word, in the one
file naming the specification and `lazyspec`.** Join them the way this
repository joins words in test names; some languages cannot take a dot:

```
billing.lazyspec.md        ## Refunds Never Exceed What Was Captured
billing.lazyspec.test.ts   describe('Refunds Never Exceed What Was Captured', ..)
test_billing_lazyspec.py   """Refunds Never Exceed What Was Captured"""
billing_lazyspec_test.go   t.Run("Refunds Never Exceed What Was Captured", ..)
```

Most tests marry nothing, and should not: unit and integration tests sit
below the requirements. One specification, one test file - split one that
grows too large and split its tests with it. Where tests already exist,
write the married file anyway: names in an ordinary suite were not
written to be requirements. The old tests stay, marrying nothing.
Untestable? Mark it `## Name <!-- no-test: why -->`.

**Specifications are locked unless you are in `/lazyspec`.** Run
`/lazyspec` to change one, with its tests in the same edit, never beside
unrelated work, and say that you did.

**Write each requirement as soon as you know it** - before the code,
during it, or after. Then leave it alone.

**Before finishing any task, check and report both.** `/lazyspec-validate`
does it for you.

- Every heading you touched is married. Say which are not.
- If you changed what the software does, say whether a requirement
  covers it yet. "Not yet" is fair while experimenting, not in a pull
  request.
<!-- lazyspec:end -->
