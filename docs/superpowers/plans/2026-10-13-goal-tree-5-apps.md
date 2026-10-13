# Goal tree, phase 5: the macOS and Linux apps

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans. Spec: `docs/superpowers/specs/2026-10-13-goal-tree-design.md`
> ("Views", "UI"). The look is fixed by `docs/design/goal-tree/` — open each `<screen>.html` and
> `<screen>.png`; they are the target, pixel for pixel where the platform allows. The data comes
> from phase 4 (`docs/superpowers/plans/2026-10-13-goal-tree-4-views.md`; read the real field
> names in `internal/serve` — they win over the plan).

**Goal:** both apps show goals as the canvas does: Overview with done-when / finish line /
course cards, needs-you, sub-goals with steps, "not done yet, because"; nested sub-goal pages
with a breadcrumb; the proposal, course-change and finish-line questions; the Conversation tab
with sub-goal tags and filters; the Record tab; the sidebar banded Needs you · Working ·
Completed.

**Architecture:** each app decodes the new JSON in its kit (`clients/macos/Sources/WorkDirectorKit`,
`clients/linux/src`) and renders natively (SwiftUI in `clients/macos/Sources/WorkDirector`,
GTK4/libadwaita in `clients/linux/app/src`). Views stay computed in Go; an app never derives
progress, paths, why-not-moving or record rows itself. One task per screen, macOS and Linux
done by separate agents in parallel, each in its own worktree.

## Tokens (exact, from the canvas; both apps, light mode)

| Token | Value |
|---|---|
| page | `#ffffff` |
| sidebar | `#f2f2f6`, width 268, hairline right `#e3e3e8`, padding 14 / 10 |
| hairline | `#e3e3e8`; inner list separators `#ececf1` |
| text / text2 / text3 | `#1d1d1f` / `#55555b` / `#6c6c72` |
| accent | `#0a64d6` (links `#0a5cc7`) |
| micro (section header) | 11/14, semibold, +0.4 tracking, uppercase, text3 |
| sidebar row | padding 8 / 10, radius 10, selected `#dcdce3`; dot 8; title 13/18 medium; meta 12/16 text3 |
| toolbar | 52 high, hairline below; buttons 13/18, padding 5 / 12, radius 7, border `#d6d6dc`; primary filled accent |
| header | padding 20 / 32; breadcrumb 12/16 text3; title 22/28 semibold; tabs 13, padding 6 / 10, radius 7, selected fill `#ececf1` semibold |
| body | padding 20 / 32, 20 between sections, hairline above |
| card | hairline border, radius 10, padding 14 / 16; label 12/16 medium text3 |
| badge | 10.5/13 semibold, padding 2 / 6, radius 6 — attention `#fff1e0`/`#9a3b00`, settled `#e6f6ec`/`#11692f`, live `#e8effc`/`#0a4fb0`, waiting `#efeff3`/`#55555b`, decision `#f1ebfb`/`#5b2aa8` |
| question card | border `#f3c99a`, fill `#fff8f0`, 3 px leading bar `#e8820c`, radius 10; option rows 7 / 10, radius 8, 13/18 medium; chosen row white with border `#f3c99a` |
| state icon (20) | done: filled `#1f9d55` circle, white check · needs you: `#e8820c` ring + centre dot · working: `#0a64d6` ring + quarter fill · waiting: `#9a9aa0` dashed ring · superseded: `#9a9aa0` ring + bar |
| sub-goal row | padding 14 / 16, title 14 medium, meta 12 text3, badge trailing |
| steps | 13/20; ✓ `#1f9d55` done, ● accent semibold doing, ○ text3 todo; dropped struck through |
| progress bar | 120 × 4, radius 2, track `#ececf1`, fill `#1f9d55` |
| filter chip | 12/16, padding 4 / 10, radius 14, hairline; selected `#1d1d1f` fill, white text |
| your bubble | fill `#e5effc`, border `#bcd3f3`, radius 14, padding 10 / 14 |
| sub-goal tag | 11/14 medium, padding 2 / 7, radius 6, fill `#f2f2f6`, hairline |
| event line | 12 text3 centred between hairlines |
| record row | columns 52 · 96 · flexible · 220 · 90, gap 12, padding 9 / 0, separator `#f0f0f3` |
| record summary | five cards, number 20/24 semibold, label 12 text3 |

Dark mode: same structure; derive each colour by the platform's dark semantic equivalent
(macOS `NSColor` dynamic colours, libadwaita dark palette), badges as the same hue at 18 %
alpha over the dark page with the light foreground lightened to 4.5:1. Record the dark values in
`docs/design/desktop.md` beside the light ones.

`docs/design/desktop.md`'s token tables are replaced by this table (light and dark); every text
style and colour in each app comes from it (`clients/macos/Sources/WorkDirector/Design.swift`,
`clients/linux/app/src/ui.rs` + `style.css`). The existing "Every text style comes from this
table" rule stays.

## Ground rules

- Each app keeps its spec: `clients/macos/macos.lazyspec.md` (married in
  `clients/macos/Tests/WorkDirectorKitTests/MacosLazyspecTests.swift` by `@Test("<heading>")`)
  and `clients/linux/linux.lazyspec.md` (married in `clients/linux/tests/linux_lazyspec.rs`,
  snake_case fn names). Change them only through `/lazyspec` (Skill `lazyspec:lazyspec`), with
  tests, in the same commit. Requirements describe what the kit computes or decides (what is
  shown, in what order, what a click runs), not pixel values.
- Test first for kit logic. `swift test` (macOS) / `cargo test` (Linux) + `scripts/lazyspec-check.sh`
  clean before each commit; the app must build (`swift build`, `cargo build --release`).
- Commit to `main` (pull --rebase, push) after each task; attribution lines on every commit;
  invariant-only comments; no `rm -rf`. Do not change Go code; if the JSON lacks something a
  screen needs, stop and report it.

## Tasks (each for both apps; macOS agent and Linux agent do their own)

1. **Kit: decode the views.** Goal view's `chain`, `done_when`, `finish`, `progress`,
   `not_done_because`, `needs_you`, `course`, `superseded`, `tree` (recursive nodes), `paths`;
   board goals' `progress`, `needs_you`, `finish`, `finished`; the record view (`record` method).
   Requirement: `## The Kit Reads A Goal As A Tree` (what it decodes, and that a goal page can
   open any node that is a goal). Test against JSON captured from phase 4's `wd serve`.
2. **Tokens.** Replace the colour and type constants with the table above; badges, state icons,
   cards, chips as reusable components (`Design.swift` / `ui.rs` + `style.css`).
3. **Sidebar.** Top goals only, bands Needs you · Working · Completed (Completed holds done
   goals; dropped, abandoned and superseded ones stay where the app keeps them today),
   row = dot + title + meta (`1 of 4 · [1 needs you]` badge, or `finished` once done); the open
   goal's path shown under its row when a nested goal is open (canvas `subgoal`). Requirement:
   `## The Sidebar Bands Top Goals By Whether They Need You, Work Or Are Completed`.
4. **Overview** (canvas `main`, `subgoal`): breadcrumb from `chain`; title + state badge; three
   cards (Done when · Finishes at with source sentence · Course with verdict badge, age and why;
   a nested goal shows two: Done when · Course); needs-you cards (question with options,
   Recommended badge, "Let the director decide", Answer — reuse the existing question
   component and answer path); Sub-goals header with progress bar and counts and "+ Add
   sub-goal"; rows with state icon, title, meta, trailing badge, a goal row navigates to its own
   page, a task row folds its steps open and closed (closed by default; the first working row
   starts open); superseded rows struck through with why; "Not done yet,
   because" card from `not_done_because`; nested page adds "What the director did here" from the
   record entries of that subtree. Requirement: `## A Goal Page Shows What Done Means, What
   Needs You And Its Sub-Goals`.
5. **Proposal, course change, finish line** (canvas `proposal`, `pivot`, `finishline`): these
   are questions whose payload carries a plan change, a proposal or finish-line choices; render
   them as the canvas does (proposed sub-goal cards with Edit/Split/Drop running the `wd goal`
   commands; the Now/Proposed table; the sources block), answering through the existing answer
   path. Requirement: `## A Change Waiting For The Person Shows What Would Change`.
6. **Conversation tab** (canvas `conversation`): filter chips (Everything, You and the director,
   one per sub-goal from `tree`); every agent turn tagged with its `paths` entry; the director's
   plan as a card of sub-goals with state badges; a sub-goal finishing as an event line; an
   agent's question with the director's answer in one card; composer "to: whole goal ▾" choosing
   a sub-goal sends to it (`wd send <id>`). Keep everything the current conversation already
   does (outbox, follow, jump). Requirement: `## The Conversation Tags Each Turn With Its
   Sub-Goal And Filters By It`.
7. **Record tab** (canvas `record`): summary cards; kind chips; sub-goal picker; the table.
   Requirement: `## The Record Lists What Was Decided And Done, Filtered By Kind And Sub-Goal`.
8. **Tabs**: Overview · Conversation · Record; the Tasks tab is gone (its sorting and record
   view move into Overview and Record). Remove its code and requirements with their tests.

## Verification (done by the coordinating session, not the agents)

UI automation against a seeded ledger on Ubuntu (Docker, `scripts/ui/linux/`) and macOS
(accessibility + `screencapture`), each screen compared with `docs/design/goal-tree/<screen>.png`
side by side and the token values checked; every mismatch fixed or named.
