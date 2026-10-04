# Work Director — Web UI Design

Date 2026-10-02. Grounded in the existing `wd serve` JSON API, the ledger state
machine, and the delivery derivation in `internal/delivery`.

## 1. Information architecture

Three top-level surfaces, plus a detail view. Nothing else.

| Surface | What it answers | Why it exists |
|---|---|---|
| **Chat** | "What do I want to do?" / "What's happening?" | The primary input surface. Plain English in, ledger writes out. Also the live feed of loop activity. |
| **Board** | "What's the state of everything right now?" | The at-a-glance status of all work. Read as a statement about the world. |
| **Review** | "What did the loop decide while I wasn't looking?" | The audit diff: claims, costs, reversals, taste promotions, unlanded work. A pass, not a dump. |
| **Goal detail** (drill-down, not a tab) | "Tell me everything about this one goal." | Timeline, decisions, claims, tasks, landings, delivery, actions. |

**Justification.** The user is technical and time-poor. They have exactly two
modes of engagement: *acting* (chat) and *checking* (board/review). A third
mode — *auditing* — is the review surface. Everything else is a detail view.
No settings tab (the CLI owns config), no roadmap tab (roadmap items become
goals; the board shows goals), no analytics tab (cost is shown per-decision,
not charted).

The board and review are read-mostly. Chat is write-heavy. This split keeps the
read surfaces fast and the write surface deliberate.

---

## 2. The chat surface

### Layout

```
┌─────────────────────────────────────────────────────────┐
│  wd — work-director                              [Board] [Review]  │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌───────────────────────────────────────┐              │
│  │ You: make the login page remember me  │              │
│  └───────────────────────────────────────┘              │
│                                                         │
│  ┌─ System ──────────────────────────────┐              │
│  │ Filed as goal "login page remembers   │              │
│  │ me" · type: build · project: web     │              │
│  │                                       │              │
│  │ 3 tasks queued · executor spawned     │              │
│  └───────────────────────────────────────┘              │
│                                                         │
│  ┌─ System ──────────────────────────────┐              │
│  │ Decision recorded on "login page      │              │
│  │ remembers me":                       │              │
│  │ Q: Should we use localStorage or     │              │
│  │    a cookie?                         │              │
│  │ A: localStorage — no server round    │              │
│  │    trip.                             │              │
│  │ Decided by: judge (claude, 1.2k      │              │
│  │ tokens)                              │              │
│  └───────────────────────────────────────┘              │
│                                                         │
├─────────────────────────────────────────────────────────┤
│  ┌─ Interpretation ──────────────────────┐              │
│  │ This will be filed as a new goal:     │              │
│  │ "add dark mode toggle"  [build ▾]     │              │
│  │                              [Send]   │              │
│  └───────────────────────────────────────┘              │
│  Type a message, or / for commands…                     │
└─────────────────────────────────────────────────────────┘
```

### The composer

A single text input at the bottom. Placeholder: `Type a message, or / for commands…`

**Interpretation card.** When the user types, the system classifies the intent
and shows a staging card directly above the composer, before anything is
written to the ledger. The card shows:

- **What it will be filed as** — "a new goal", "a question on goal X", "a
  follow-up on goal Y", "a decision on goal Z", "a status answer".
- **The extracted title** (editable inline).
- **The inferred type** (query | build | fix | change | review) as a dropdown.
- **The target project** (editable, defaults to the project context).

The user can edit any field before sending. The card is the contract: what the
user sees on the card is exactly what will be written. No surprises.

**Why this matters.** The ledger is durable and append-only. A mis-filed goal
is not a typo — it's a row that says the wrong thing about the world. The
interpretation card is the last chance to catch a misclassification. It turns
the ledger from a write-only black box into a conversation with a preview.

**Confidence behavior.** If the system is confident (clear imperative, clear
question, clear follow-up), the card appears immediately and the user just hits
Enter. If the system is uncertain, the card shows the top interpretation with a
"or did you mean…" alternative. The user picks. Nothing is written until the
user confirms.

### Question vs. goal — displayed differently

**A question** is light. It gets answered, not tracked. Displayed as:

```
┌─ Question ──────────────────────────────┐
│ "why is the build red?"                 │
│                                         │
│ Answered by: judge (claude, 0.8k tokens)│
│ "The test suite expects a fixture that   │
│  was removed in commit a1b2c3d."         │
│                                         │
│ No goal filed. This was a query.         │
└─────────────────────────────────────────┘
```

Visual weight: a thin card, no board presence, no delivery tracking. The answer
is shown inline. If the question was about an existing goal, a link to that
goal is shown. The question does not reopen the goal.

**A goal** is heavy. It gets a card, tasks, delivery tracking, a board presence.
Displayed as:

```
┌─ Goal filed ────────────────────────────┐
│ "login page remembers me"                │
│ type: build · project: web · id: g-123   │
│                                         │
│ 3 tasks queued · executor spawned       │
│ View on board →                         │
└─────────────────────────────────────────┘
```

Visual weight: a solid card with a border, a link to the board, and a goal id.
The user can see it's now a tracked thing.

### Slash commands

Typing `/` opens a command palette overlaying the composer. The palette lists
every command the CLI supports, organized by category:

```
/ add          — file a new work item
/goal          — goal operations (classify, reopen, release, run, …)
/roadmap       — roadmap operations
/status        — show status
/set           — set state
/done          — close work
/reopen        — reopen finished work (reason required)
/release       — release abandoned work (reason required)
/decide        — record a decision
/drive         — run the goal loop
/review        — review what the loop decided
/feedback      — feedback operations
/pr            — record a landing
/verify        — run verify
/report        — file a report
/send          — send a message to a running session
/claim         — claim a task
/brief         — brief a goal
/spawn         — spawn an executor
/context       — show context for a goal
/events        — show events for a goal
/tasks         — show tasks for a goal
/conflict      — check conflicts
/projects      — project management
/taste         — taste card operations
/distill       — distill feedback
/doctor        — diagnostics
/runner        — runner management
/worktree      — worktree management
```

**Rules for slash commands:**

1. **1:1 with the CLI.** Every slash command maps to exactly one `wd` CLI
   invocation. No UI-only commands. If the CLI can't do it, the UI doesn't
   offer it.
2. **Autocomplete.** Filter as you type. Show the command syntax, a one-line
   description, and the flags.
3. **Help on `/`.** Pressing `/` with an empty query shows the full list with
   descriptions. This is the discovery mechanism.
4. **Arguments are explicit.** If a command needs an id, the palette prompts for
   it (with autocomplete from known goal ids). If it needs a reason, the
   palette requires it — the UI never sends a reason the user didn't type.
5. **Result is shown inline.** The command's output appears in the chat as a
   system message. Errors are shown as errors, not silently swallowed.

### Live activity feed

The chat is also the feed of loop activity. When the loop does something —
spawns an executor, files a report, makes a decision, hits a gate — it appears
in the chat as a system message. This is not a separate "activity" tab; it's
part of the conversation. The user sees the loop working.

System messages are visually distinct from user messages and from interpretation
cards. They are timestamped and link to the relevant goal.

---

## 3. The board (kanban)

### Layout

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Board                    [Needs You] [All]    Filter: [Project ▾] [Type ▾] │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─ NEEDS YOU ─────────────────────────────────────────────────────┐    │
│  │ ⚠ "mobile app" — ready to close (landed, not closed)      [Close] │    │
│  │ ⚠ "auth refactor" — blocked: verify failed               [Why?]  │    │
│  │ ⚠ "api migration" — needs input: which endpoint?          [Answer]│    │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                         │
│  ┌─ QUEUED ──────┐  ┌─ RUNNING ──────┐  ┌─ REVIEW ──────┐  ┌─ DONE ──┐ │
│  │               │  │               │  │               │  │         │ │
│  │ ┌───────────┐ │  │ ┌───────────┐ │  │ ┌───────────┐ │  │ ┌─────┐ │ │
│  │ │ dark mode │ │  │ │ login page│ │  │ │ auth      │ │  │ │ ... │ │ │
│  │ │ toggle    │ │  │ │ remembers │ │  │ │ refactor  │ │  │ └─────┘ │ │
│  │ │           │ │  │ │ me        │ │  │ │           │ │  │         │ │
│  │ │ build     │ │  │ │           │ │  │ │ fix       │ │  │         │ │
│  │ │ 0/3 tasks │ │  │ │ 2/3 tasks │ │  │ │ 3/3 tasks │ │  │         │ │
│  │ │ none      │ │  │ │ pr-open   │ │  │ │ landed-pr │ │  │         │ │
│  │ └───────────┘ │  │ └───────────┘ │  │ └───────────┘ │  │         │ │
│  │               │  │               │  │               │  │         │ │
│  └───────────────┘  └───────────────┘  └───────────────┘  └─────────┘ │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### Columns — exact rules

**Top section: "Needs You"** — not a column, a priority section. Any card with
an attention flag lands here, regardless of its column. Sorted by severity:
blocked > needs-input > ready-to-close > stale-pr > ci-blocked >
forgotten-after-merge. A card can appear in "Needs You" and in its column
simultaneously (the "Needs You" entry is a pinned copy, not a move).

**Four columns:**

| Column | Rule (state) | Meaning |
|---|---|---|
| **Queued** | `state = queued OR state = briefed` | Work filed but not yet running. Waiting for a spawn or a drive. |
| **Running** | `state = running` | An executor is actively working, or the loop is driving it. |
| **Review** | `state = review OR state = soft-done` | Work finished, waiting for verification and close. |
| **Done** | `state = done OR state = dropped OR state = abandoned` | Finished. Shown collapsed by default; expand to see history. |

**Why these four.** The state machine has ten states, but the user doesn't need
ten columns. The four columns map to the four things a person needs to know:
"not started", "in progress", "finished, verify it", "done". The intermediate
states (needs-input, blocked) are not columns — they are attention flags on a
card in Running or Queued. This keeps the board readable at a glance.

**Delivery is not a column.** Delivery (none → shipped) is a property of a card,
shown as a badge. A card in Review might be `landed-pr` (ready to close) or
`none` (still being written). That difference is the attention flag, not a
column move.

### What a card shows at a glance

```
┌─────────────────────────────┐
│ ⚠ login page remembers me   │  ← title + attention icon
│                             │
│ build · web · g-123         │  ← type · project · id
│                             │
│ ▓▓▓▓▓▓▓▓░░░░ 2/3 tasks     │  ← task rollup (done/total)
│                             │
│ pr-open · landed on main    │  ← delivery badge
│                             │
│ 2m ago · judge (claude)    │  ← last activity
└─────────────────────────────┘
```

**Six elements, no more:**

1. **Title** — one line, truncated with ellipsis.
2. **Attention icon** (if any) — ⚠ for blocked/needs-input, ○ for
   ready-to-close, ◌ for stale-pr/ci-blocked/forgotten-after-merge. Clicking
   the icon shows the flag text.
3. **Type · project · id** — small text. Type is a colored dot (build=blue,
   fix=red, change=amber, query=gray, review=purple). Project is plain text.
   Id is monospace, copyable on click.
4. **Task rollup** — a progress bar + "N/M tasks". Only shown for goals (items
   and standalone tasks have no children, so no rollup).
5. **Delivery badge** — a pill showing the delivery status. Color-coded:
   - `none` — gray
   - `committed-unpushed` / `pushed-unpr` — yellow
   - `pr-draft` / `pr-open` — blue
   - `pr-closed-unmerged` — red
   - `pr-merged` / `landed-pr` / `landed-commit` — green
   - `shipped` — solid green
6. **Last activity** — relative time + who (judge, executor name, or "you").

**What doesn't fit on a card:** decisions, claims, landings detail, verify
output, PR URLs, worktree paths, session refs. All of that lives in the goal
detail view. The card is a summary, not a report.

### Card interactions

- **Click** → opens goal detail.
- **Right-click / long-press** → quick actions (reopen, release, done, drive).
- **Drag** → not supported. State changes are ledger transitions with rules;
  drag-and-drop would imply any state can go to any state, which is false.
  State changes happen through actions in the goal detail or via slash commands.

---

## 4. Goal detail

Opening a goal (from the board, from chat, or from a slash command) shows a
detail view. This is the "everything about one goal" surface.

### Layout

```
┌─────────────────────────────────────────────────────────────────────────┐
│  ← Back to board                                                        │
│                                                                         │
│  login page remembers me                                    [Reopen] […] │
│  build · web · g-123 · running                                          │
│                                                                         │
│  ┌─ Delivery ──────────────────────────────────────────────────────┐    │
│  │ pr-open · https://github.com/org/repo/pull/456                │    │
│  │ 2 commits ahead of main · branch: feat/login-remember-me       │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                         │
│  ┌─ Tasks ────────────────────────────────────────────────────────┐     │
│  │ ✓ Add remember-me checkbox to login form      · done            │     │
│  │ ✓ Persist token to localStorage               · done            │     │
│  │ ○ Add "remember me" to signup flow            · running         │     │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                         │
│  ┌─ Timeline ──────────────────────────────────────────────────────┐    │
│ │                                                                 │    │
│ │  10:32  You filed this goal                                     │    │
│ │  10:32  Executor spawned (claude, session abc-123)             │    │
│ │  10:35  Report: DONE — "Added checkbox, wired to localStorage"  │    │
│ │  10:35  Verify: pass (go test ./...)                           │    │
│ │  10:36  Landing: commit a1b2c3d on feat/login-remember-me       │    │
│ │                                                                 │    │
│ │  ┌─ Decision ─────────────────────────────────────────────┐    │    │
│ │  │ Q: localStorage or cookie?                              │    │    │
│ │  │ A: localStorage — no server round trip.                 │    │    │
│ │  │ Decided by: judge (claude, 1.2k tokens) · 10:34        │    │    │
│ │  │ [Reverse]                                               │    │    │
│ │  └─────────────────────────────────────────────────────────┘    │    │
│ │                                                                 │    │
│ │  10:40  State: running → review                                │    │
│ │                                                                 │    │
│ └──────────────────────────────────────────────────────────────────┘    │
│                                                                         │
│  ┌─ Actions ──────────────────────────────────────────────────────┐     │
│  [Drive] [Send message] [File report] [Record decision] [Close]   │     │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### Sections

**Header.** Title, type, project, id, state. Actions that apply to the current
state are shown as buttons. Actions that don't apply are hidden, not disabled —
a disabled button is a tease; a hidden button is clarity.

**Delivery panel.** The derived delivery status, the PR URL (if any), the
branch, commits ahead/behind. This is computed from git, not stored — it's a
fact about the world right now, not a fact about the ledger. If the work is in
a worktree, the delivery panel shows the worktree path.

**Tasks.** A list of child tasks with their states. Each task is a row: status
icon, title, state. Clicking a task opens its own detail (a task has its own
events, but no children). For a goal with no tasks, this section shows "No
tasks yet" with a hint that the loop will create them.

**Timeline.** The full event stream for the goal and its tasks, in order. Each
event is a row: timestamp, icon, summary. Event types:

- `state` — "State: running → review"
- `report` — "Report: DONE — 'summary'"
- `verify` — "Verify: pass (go test ./...)" or "Verify: fail — 'output'"
- `pr` — "Landing: commit a1b2c3d" or "Landing: PR #456"
- `decision` — expands to show the full claim (question, answer, source, cost)
- `question` — "Question: 'which endpoint?'"
- `answer` — "Answer: 'the /users endpoint'"
- `note` — "Note: 'text'"
- `spawn` — "Executor spawned (claude, session abc-123)"
- `attach` — "Attached to session abc-123"
- `abandon` — "Abandoned: unmerged — 'PR #456 was closed without merging'"

**Decisions** are the first-class citizens of the timeline. A decision event
expands inline to show the full claim: the question, the answer, who decided
(source), which runner and model, what it cost in tokens, and whether it has
been reversed. A reversed decision shows the reversal as a nested event — the
original is never edited or hidden.

**Claims** (resolved concerns) are shown in the timeline as decisions. A claim
that is still open (unresolved concern) is shown in the "Needs You" section of
the board and as a highlighted item in the goal detail.

**Landings** are shown in the timeline and in the delivery panel. A landing
carries its kind (commit or pull-request) and its target (URL or SHA). The kind
is never inferred from the URL shape — it's read from the event body.

**Actions.** Context-sensitive buttons:

| State | Actions |
|---|---|
| queued, briefed | Drive, Send message, Close (cancel) |
| running | Drive, Send message, File report, Record decision, Block |
| needs-input | Answer (opens a question-answering flow), Send message |
| blocked | Unblock (→ running), Close (cancel) |
| review | Verify, Close, Reopen |
| soft-done | Close, Reopen |
| done | Reopen (reason required) |
| abandoned | Release (reason required) |
| dropped | — (terminal) |

Every action maps to a slash command. The buttons are shortcuts; the slash
commands are the source of truth.

---

## 5. States and feedback

### Loading

**Board loading:**
```
Loading board…
```
Shown as a skeleton (column outlines with pulsing placeholders), not a spinner.
The board is the default view; it should feel instant. If it takes more than
300ms, show the skeleton.

**Goal detail loading:**
```
Loading goal…
```
Same skeleton pattern.

**Chat sending:**
```
Interpreting…
```
Shown in the interpretation card while the system classifies the intent. If
classification takes more than 500ms, show "Still thinking…" — the user should
know the system hasn't hung.

### Empty states

**Empty board (no open work):**
```
No open work.

Type something above to start a goal, or run /roadmap to see what's planned.
```

**Empty chat (no messages yet):**
```
What are we working on?

Type a plain English request — "make the login page remember me" — or press / for commands.
```

**Empty goal detail (no events):**
```
This goal has no activity yet.

Run /drive to start the loop, or /send to message the executor.
```

**Empty review (nothing new):**
```
Nothing new since your last review.

The loop is idle. Next review will show what changed.
```

**Empty tasks (goal with no children):**
```
No tasks yet.

The loop creates tasks as it works. Run /drive to start.
```

### Error states

**Ledger unreachable:**
```
The ledger could not be read.

Check that `wd serve` is running and try again.
```

**Action failed (generic):**
```
The action failed: <error message from the CLI>

Nothing was written to the ledger.
```

**Action failed (illegal transition):**
```
Cannot move from review to queued.

Allowed: review → soft-done, review → running, review → blocked, review → dropped, review → abandoned.
```

**Action failed (not ready):**
```
Not ready for soft-done: verify, pull request.

Run /verify to check the build, and /pr to record a landing.
```

**Action failed (missing reason):**
```
A reason is required.

Reopening a finished goal needs to say what is being worked on. Releasing an abandoned goal needs to say what was true instead.
```

### Blocked / needs-input

**Blocked card (in "Needs You"):**
```
⚠ "auth refactor" — blocked

The loop stopped because: verify failed (go test ./...).

[Why?] [Unblock]
```

Clicking "Why?" opens the goal detail to the failed verify event. "Unblock"
runs `/set <id> running` (or the appropriate transition).

**Needs-input card (in "Needs You"):**
```
⚠ "api migration" — needs input

The executor is waiting on: "Which endpoint should the client call?"

[Answer] [Send message]
```

Clicking "Answer" opens a focused input to type the answer. The answer is filed
as a decision and sent to the waiting executor. "Send message" opens the full
chat with that goal.

### Attention flags

**Ready to close:**
```
○ "mobile app" — ready to close

This goal has landed (pr-merged) but is not closed. Close it to mark it done.

[Close] [Keep open]
```

**Stale PR:**
```
◌ "auth refactor" — stale PR

PR #789 has been open for 14 days without activity.

[View PR] [Close]
```

**CI blocked:**
```
◌ "api migration" — CI blocked

The build is red on the PR. The loop cannot merge until it passes.

[View PR] [Run verify locally]
```

**Forgotten after merge:**
```
◌ "dark mode" — landed, not closed

This goal landed 3 days ago and is still open. Close it or reopen it.

[Close] [Reopen]
```

---

## 6. The rules of the UI

These are hard constraints. An engineer should treat them as invariants, not
guidelines.

**1. Never show a command the ledger cannot verify.**
Every action in the UI maps to a real `wd` CLI invocation. If the CLI can't do
it, the UI doesn't offer it. No "fake" buttons that optimistically update the
UI and sync later. The ledger is the source of truth; the UI is a view.

**2. Every claim names its source.**
A decision without a source is not a decision — it's an opinion. Every claim
shows who decided (judge, executor, or director), which runner and model, and
what it cost in tokens. A decision recorded as one line of prose shows "recorded
by: <who>" and no cost. A structured claim shows all fields. A missing field
says exactly that — it is never defaulted or guessed.

**3. A finished goal is finished until someone says what is being worked on.**
The UI never reopens a goal without a reason. The reopen action opens a dialog
that requires a reason before the command is sent. The reason is shown in the
timeline as a decision. A question about a finished goal does not reopen it —
it is answered and the goal stays done.

**4. The board is a statement about the world.**
The board never shows stale data as current. If the ledger says a goal is
running, the board shows running — even if the executor died an hour ago. The
board does not infer liveness from process state; it shows what the ledger says.
Staleness (30 days without activity) is shown as a flag, not silently hidden.

**5. Interpretation before commitment.**
Nothing is written to the ledger without the user seeing what will be written.
The interpretation card is mandatory for chat input. Slash commands show their
full argv before execution (in the palette preview). The user confirms; the
system writes.

**6. Cost is agent minutes and tokens, never human hours.**
The UI never shows "this will take 3 days" or "estimated effort: large". It
shows what the loop spent: "2 turns, 1 judgement, 3.4k tokens". Cost is a fact
about the run, not a guess about the future.

**7. A goal type is set deliberately or not at all.**
The UI never defaults a goal type. If the system infers a type, it shows the
inference in the interpretation card and lets the user change it. If the user
doesn't pick, the goal has no type — and the UI shows "unclassified" rather
than a guess. A wrong type that reads as right is worse than a missing one.

**8. Empty is empty, not null.**
The API returns `[]` for empty collections, never `null`. The UI renders an
empty collection as an empty state with a helpful sentence — never as a broken
view, a spinner that never ends, or a crash. An empty board is a good thing;
it means nothing is open.

---

## 7. Anti-patterns

What a bad version of this product looks like. Avoid these.

**1. A chat UI that's a CLI wrapper.**
A text input that takes `wd add "title"` and shows raw stdout. This defeats
the entire purpose. The chat takes plain English. The system decides what it
is. The user never types a command unless they choose to with `/`.

**2. A board with delivery as columns.**
Columns for "no PR", "PR open", "PR merged", "landed". This conflates work
state with delivery state and produces a board with 20+ columns. Delivery is a
badge on a card, not a column. The four columns (Queued, Running, Review, Done)
are the right granularity.

**3. Hiding the system's interpretation.**
A chat that just says "Done" after filing a goal, without showing what it filed.
The user has to trust the system blindly. The interpretation card is the fix —
it shows what will be written before it is written.

**4. Treating questions and goals the same.**
Filing "why is the build red?" as a new goal with tasks and delivery tracking.
A question is a query — it gets answered, not tracked. The UI must distinguish
them visually and behaviorally.

**5. Showing "running" for dead work.**
A board that shows a goal as running because the ledger says so, even though
the executor process died three days ago. The board shows what the ledger says,
but it also shows staleness. A goal that hasn't moved in 30 days is flagged as
stale — not hidden, not silently kept as "running".

**6. A review surface that's a dump.**
A page that lists every event the loop ever produced, with no cursor, no diff,
no acknowledgement. The review is a pass — it shows what changed since the last
acknowledgement, and acknowledging moves the cursor. A dump is not a review.

**7. Optimistic UI that lies.**
A board that moves a card to "Done" before the ledger confirms the transition.
If the transition fails, the card snaps back — but the user already believed it
was done. The UI writes first, then reflects. No optimistic updates for ledger
transitions.

**8. A command palette with UI-only commands.**
A `/deploy` button that runs a script the CLI doesn't know about. Every slash
command maps to a real `wd` invocation. If the CLI can't do it, the UI doesn't
offer it. The palette is a window into the CLI, not a separate API.

**9. Showing cost in human hours.**
"Estimated time: 2 days" on a goal. The system doesn't know how long a human
would take. It knows what the loop spent: turns, judgements, tokens. That's what
the UI shows.

**10. A goal detail that's a wall of JSON.**
Dumping the raw `goalView` JSON on the screen. The goal detail is a curated
view: header, delivery, tasks, timeline, actions. The JSON is the API; the UI is
the product.

---

## Appendix: API mapping

Every UI action maps to an existing endpoint or CLI command:

| UI action | API / CLI |
|---|---|
| Load board | `GET /api/board` |
| Load goal detail | `GET /api/goal/<id>` |
| Load work item | `GET /api/work/<id>` |
| Load events | `GET /api/work/<id>/events` |
| Live updates | `GET /ws` (WebSocket) |
| Chat interpretation | `POST /api/action` with `argv: ["add", ...]` or equivalent |
| Slash command | `POST /api/action` with `argv: [<command>, ...]` |
| Any CLI command | `POST /api/action` with `argv: [...]` |

The UI is a thin client over the existing API. No new endpoints are needed for
the core UX. The interpretation card may need a new endpoint (or a client-side
heuristic), but all writes go through the existing action dispatch.
