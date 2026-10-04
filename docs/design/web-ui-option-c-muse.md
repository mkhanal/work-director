# Work Director — Web UI Design (option C)

Model: `opencode/muse-spark-1.3-contributor-free`, run 2026-10-02.

Assumption stated up front: the UI is the primary surface; the CLI remains but
no screen may require knowing a command. Everything shown must be verifiable
against the ledger (`GET /api/board`, `GET /api/goal/<id>`, `POST /api/action` →
real `wd` argv). **If the ledger can't prove it, the UI doesn't show it.**

## 1. Information architecture — 3 surfaces, no more

The user's only questions are: *what do I say next, what's happening, what's
stuck, what shipped?*

**A. Chat (default landing, left pane)** — "do I need to type anything, and did
the system understand me?" Home. Plain-English input + durable ledger writes,
with a preview step before anything is committed. Power users get `/` here.

**B. Board (right pane / second tab on narrow)** — "what's open, what's stuck,
what needs me — at a glance?" From `GET /api/board`. Grouped by state, not by
type: **Needs you** (needs-input, blocked, attention flags) → **Running**
(queued, briefed, running, review) → **Done this week** (soft-done, done,
landed). Roadmap items awaiting promotion appear as a collapsed strip at the
top, not a separate page. Each row shows title, goal type chip, work state,
derived delivery state, and one attention flag max.

**C. Goal detail (slide-over, not a route)** — "what did we decide, where did
it reach in git, and what closes it?" Full view from `GET /api/goal/<id>`. Opens
as a right-side slide-over preserving chat context. Deep-linkable
(`/goal/<id>`) but never a full page replacement.

*Why only three:* the user has two jobs — tell the system things, and clear
what's stuck. Splitting roadmap/goals/tasks/decisions into separate tabs forces
them to reconstruct the story the ledger already joins via `EventsUnder`. One
board + one detail preserves it.

## 2. The chat surface

```
+--------------------------------+-------------------------------+
| CHAT (55%)                     | BOARD (45%)                   |
|                                | [Needs you 2] [Running 3]     |
|  messages...                   |  rows...                      |
|  [preview card when pending]   |                               |
|  [composer            ]         |                               |
+--------------------------------+-------------------------------+
```

Narrow (<900px): single column with a `Chat | Board` segmented toggle, Chat
default. Opening a goal always slides over whichever is visible.

**Composer.** Single multiline input, placeholder: `Tell wd what to do or ask —
e.g. "make login remember me"`. Enter sends, Shift+Enter newline. Two buttons:
`Send` and `/`. While drive is acting on a goal the composer stays enabled — you
can always interrupt with a decision or a question.

**`/` behaviour.** Typing `/` at line start (or the `/` button, or Cmd+K) opens
autocomplete listing **real verbs only**: `/add /promote /reopen /release
/decide /concern /resolve /abandon /drop /close /drive /status /goal-type`.
Each row shows verb, one-line description, required args. Selecting one fills
the composer with the arg template (e.g. `/reopen wd-1f2a "<what is being
worked on>"`). Typing `/` alone and pressing Enter shows: *"Type a command, or
just describe what you want in words. Commands do exactly what the CLI does."*

**Interpretation preview — mandatory before any write.** Chat-to-goal is a
classifier, and classification writes to a durable ledger, so **no message
commits on send**. Every send produces a preview card within ~1s:

```
I understood this as: NEW GOAL (build)
Title: "Remember-me on login page"
Will run: wd add --goal --type build "Remember-me on login page"
[Confirm]  [Edit]  [It's a question instead]
```

Variants:

- **Goal** — goal type chip (editable dropdown), title, exact argv.
- **Question** — `I understood this as: QUESTION — will answer from ledger,
  nothing will be written. [Confirm]`
- **Follow-up on existing goal** — `FOLLOW-UP on wd-1f2a "login session" — will
  record as decision/event on that goal. [Confirm] [Pick a different goal]`
- **Decision** — `DECISION for wd-1f2a — drive will answer its own question from
  this. [Confirm]`
- **Status request** ("why is the build red?", "what's stuck?") — answered
  inline immediately, **no preview, read-only**.

Confirm executes `POST /api/action`; the card flips to committed state showing
the work id link. Edit returns text to the composer. **Nothing ever writes on
Enter alone for goal/decision/follow-up classes.** This is the single most
important interaction in the product.

**Question vs goal display.** Visually distinct, never ambiguous:

- **Question:** no left border, small `ANSWER` label, plain text, ends with a
  source line: `From: decision on wd-1f2a, 2h ago · report on wd-1f2a, 1h ago`.
  No action buttons except `Ask follow-up`.
- **Goal creation:** card with left accent border, `GOAL wd-3c9e · build ·
  queued` header, title, `[Open goal]`.
- **Follow-up/decision:** compact card under the parent goal's thread,
  `DECISION → wd-1f2a` header, quoted decision text.

## 3. Goal detail

Slide-over, sections in fixed order. **Order is load-bearing:** time-poor users
read top-down and stop at the first section that answers them.

1. **Header.** Title, id (`wd-1f2a`, click-to-copy), goal type chip, work state
   pill, derived delivery pill. One-line rollup: `3/5 tasks done · last activity
   20m ago`.
2. **Needs you (only if present).** The blocking question verbatim + inline
   input. e.g. `drive is asking: "Which OAuth provider — Google only, or GitHub
   too?" [Answer]`. Answering files a decision and resumes drive. No navigation
   required.
3. **Delivery.** Derived from git, never asserted by the agent. One status line
   + evidence link: `Pushed — waiting on PR. commit a3f9c1 · no PR yet.` Attention
   flags appear here as red/amber banners, each with its one action (`Close`,
   `Check PR`, `Verify`).
4. **Tasks.** Checklist from the goal's children: title + state pill each.
   Collapsed to 5 rows, expandable. No per-task detail page — click scrolls to
   that task's events in the timeline.
5. **Decisions & claims.** Flat list, newest last: decision text + who/when +
   the work it was recorded against. Every claim names its source: `Claim:
   "login persists 30 days" — from report on wd-1f2a/task-3, still stands.` A
   claim that no longer stands is struck through with `superseded by decision …,
   <time>` — never deleted.
6. **Landings.** `landed-commit a3f9c1 → main` vs `pr-open #412 awaiting
   review`. A landing whose row isn't among the goal's tasks still lists with its
   link (the link is the fact, the row is the caption).
7. **Timeline.** Merged event log (`EventsUnder`: goal spine + task
   decisions/landings/reports in event order). Filter chips: `All · Decisions ·
   Reports · PRs · Errors`. New events stream over WebSocket and append with a
   `3 new ↓` pill, never auto-scroll-jacking.
8. **Actions footer (sticky).** Five buttons max, each a real argv wrapper:
   `Answer / Decide`, `Reopen…`, `Release…`, `Close`, `Drive`. `Reopen…` and
   `Release…` open a dialog with a required text field. Destructive actions
   (`Abandon`, `Drop`) sit behind `···`.

## 4. States and feedback — exact sentences

Vocabulary is fixed. **Work state and delivery state are never mixed in one
pill**: delivery always reads as past-tense evidence (`Pushed`, `Merged`), work
state as present-tense ownership (`Running`, `Needs input`).

- **Loading board:** `Loading board from wd serve…`
- **Loading goal:** `Loading wd-1f2a…`
- **Empty:** `Nothing open. Describe the first goal above — e.g. "add
  remember-me to login".` Composer focused. No illustration, no tour.
- **Error (serve down):** `Can't reach wd serve on 127.0.0.1:8787. Start it with
  wd serve, then reload.` + Retry.
- **Action failed:** `That didn't write. <server error text>. Nothing changed —
  try again.` Never "Something went wrong".
- **Needs-input:** `Needs input: <verbatim question> — drive is waiting on you.`
  + inline answer box.
- **Blocked:** `Blocked: <reason>. Nothing is running on this goal until it's
  cleared.`
- **ready-to-close:** `Landed but still open: <pr/commit link> merged. Close
  wd-1f2a? [Close] [Keep open]`
- **stale-pr:** `PR #412 has no activity for 3 days. [Open PR]`
- **ci-blocked:** `CI is failing on <commit>. Drive can't land this. [Open checks]`
- **forgotten-after-merge:** `PR #412 merged but no landing recorded. [Record
  landing]`
- **Reopen dialog:** title `Reopen wd-1f2a`, field `What is being worked on?
  (required)`, hint `A question never reopens a goal — asking leaves it
  finished. Say what new work starts.` Empty-submit error: `Say what is being
  worked on — reopening needs a reason.`
- **Release dialog:** title `Release wd-1f2a`, field `What was true instead?
  (required)`, hint `e.g. "duplicate of wd-3c9e — landed there" or "probe, never
  meant to ship".` Empty-submit: `Say what was true instead — releasing needs a
  reason.`

## 5. The rules of the UI

1. **Never show a command the ledger cannot verify.** Every button maps 1:1 to a
   real `wd` argv via `POST /api/action`. No demo buttons, no optimistic state
   changes the ledger didn't confirm.
2. **Preview before any write from chat.** Enter classifies; only Confirm
   commits. The preview shows the exact argv. No exceptions for "obvious" goals.
3. **Every claim names its source.** Claim text is always followed by `— from
   <event> on <work id>, <time>`. Unsourced text renders as `Unconfirmed: <text>`.
4. **Delivery is derived, never declared.** The UI never lets a user or agent set
   delivery state by hand. Agent assertions like "shipped" without a landing
   render as `Unconfirmed`.
5. **A finished goal is finished until someone says what is being worked on.**
   `done/dropped/abandoned` rows accept no tasks, no drive, no decisions — only
   `Reopen…` (done → running, reason required) or `Release…` (abandoned →
   dropped, reason required). Questions against finished goals answer from
   history and change nothing.
6. **One board, one truth.** Board, goal detail and chat answers all read the
   same `GET /api/*` snapshot; live updates arrive only via `/ws`. The UI never
   holds a second copy that can disagree with the ledger.
7. **Needs-input always shows the verbatim question and an inline answer.**
   Never paraphrase the drive's question into a status line, never force
   navigation to answer it.
8. **Stale is explicit.** Any data older than the last `/ws` event shows
   `Updated <Xm ago> · live` in the footer. No silent polling, no fake real-time.

## 6. Anti-patterns

- **Chat that writes on send.** With an undo toast. The ledger is durable and
  drive acts on it; undo doesn't un-run an agent. This product previews or it
  corrupts.
- **Command-palette cosplay.** `/` commands that are chat prompts rather than
  exact CLI verbs. Power users will diff them against `wd --help` once, lose
  trust, and leave.
- **Jira-lite board.** A column per work state, swimlanes by type,
  drag-and-drop transitions. Dragging done→running bypasses `Reopen`'s required
  reason and breaks the ledger's core invariant. States group into three bands;
  transitions happen through dialogs that enforce reasons.
- **Agent claims as facts.** Rendering "done", "shipped", "tests pass" from chat
  text instead of report/verify/pr/landing events. Every shipped-looking badge
  without a landing link is a lie the user discovers in git.
- **Goal detail as a dashboard.** Burndown charts, token gauges, budget dials up
  top. Budgets belong in a collapsed `Drive` section as `12/40 turns · 3.1k/50k
  tokens`, not as the headline. The headline is: what does it need, where did it
  land, what was decided.
- **Notification spam for autonomous work.** A toast per drive turn. The loop is
  supposed to run unattended; the UI interrupts only for needs-input, blocked and
  attention flags.
- **Separate pages for roadmap, decisions, events and settings.** Each new tab is
  a place the story fragments.