# Work Director — Web UI Design (option B)

Model: `opencode/fledge-alpha-free`, run 2026-10-02 against the real
`internal/serve/serve.go`. It verified its own claims against the code before
proposing anything, which is why the endpoint list below is short.

Grounding: the UI is a thin client over exactly five capabilities —
`GET /api/board`, `GET /api/goal/<id>`, `GET /api/work/<id>` (+`/events`),
`POST /api/action {argv}` (runs real CLI verbs), and `/ws` (board + event
stream).

## 1. Information architecture — three surfaces, no more

| Surface | What the person is trying to know | What it is |
|---|---|---|
| **Talk** (default route, `/`) | "What should happen next, and what just happened?" | Chat + a live activity rail. The primary surface. |
| **Board** (`/board`) | "Where is everything, and what needs me?" | Kanban of goals with attention flags. |
| **Goal** (`/g/<id>`, `/w/<id>`) | "What exactly is this, what was decided, what's it waiting on?" | Full ledger view for one item. |

No settings tab, no roadmap tab, no admin panel in v1. The roadmap is visible
two clicks deep (Talk free-text + a filtered board view), not as a top-level
surface. Every top-level surface answers a distinct question; a fourth would
overlap with one that already exists.

The header is a single row: product mark, a one-line status ("drive loop:
running · 2 active · 1 needs you · 1 attention"), and a link to the board.

## 2. The Talk surface

**Layout.** Two columns, chat left (~65%), **activity rail** right (~35%), one
screen, no page scroll. The rail shows the last ~20 ledger-meaningful events
(goal promoted, task blocked, PR opened, landed, flag raised) newest-first, each
with its source (`drive`, `executor:codex`, `git`, `user`). Live over `/ws`.

**Composer.** A single-line-growing input at the bottom, ~3 lines max, `↵` to
submit. Placeholder: *"Talk plain English, or `/` for commands."* Three static
example chips on the empty state: `make the login page remember me` ·
`why is the build red?` · `/status`. Three persistent quick-filter chips above
the rail: `needs you`, `attention`, `running`.

**Interpretation preview — the part that matters.** Nothing writes to the ledger
on Enter if it's ambiguous. The system posts an interpretation card before
committing:

> I read this as: **new goal** — *"make the login page remember me"* (type:
> build, roadmap: app)
> [Create goal] [Edit] [It's a question] [Cancel]

The card names exactly what it will write. "Edit" expands the fields inline.
"It's a question" re-interprets. Committing fires `POST /api/action` with the
real argv; the card then turns into a confirmation chip linking to the new goal.
**The system shows its write before it writes.**

**How the five interpretations look different.**

- **New goal** — green-tinted card: title/type/roadmap, action `Create goal`.
- **Question** — no commit; the answer streams inline prefixed "Answer:", with a
  "Record as decision?" follow-up chip if the answer is load-bearing.
- **Status answer** — "Attached to `goal-412`: …" with a confirm. Writing is a
  decision-record event, not a new row.
- **Follow-up on existing goal** — names the goal it matched ("Looks like you
  mean goal-412: remember-me login — 78% done"), with [That's it] [Different
  goal].
- **Decision to record** — "Record decision on goal-412: …", editable.

Only the unambiguous power-user path (`/` commands) and a clearly-parseable
goal skip the card and show a short confirmation chip instead.

**`/` commands.** Typing `/` opens an autocomplete popover anchored to the
composer: verb name, one-line description, required args with the same
placeholder grammar as the CLI (e.g. `/reopen <id> <what is being worked on>`).
`/` alone opens a help sheet grouped by kind (create, move, answer, correct,
drive). Each row shows the 1:1 CLI verb in a monospace tag. Submitting fires
`POST /api/action` with exactly that argv; the result echoes into the chat.

The misdirect guard: if plain English is typed but the system is confident it's
a command, it never mutates — it shows "Did you mean `/status`? [Run it] [No,
new goal]".

## 3. The board

| Column | Rule (state AND delivery) |
|---|---|
| **Queued** | state ∈ {queued, briefed} |
| **Running** | state = running |
| **Needs input** | state ∈ {needs-input, blocked} — merged visually, but the card still shows which one |
| **Review** | state = review OR (state ∈ {soft-done, review} and delivery ≠ none) |
| **Landed / shipped** | state = done AND delivery ∈ {landed-pr, landed-commit, shipped} |
| **Dropped** | state ∈ {dropped, abandoned} |

Finished work (`done` with delivery `none`) lands in a collapsed "Done
(unverified)" strip under Landed — one line per item, expandable, not a full
column. A row whose delivery contradicts its state is pulled into a top
**Attention** band regardless of state.

**Attention band.** A horizontal strip above the columns, one card each, with
the flag sentence: ready-to-close → "Goal g-412 is done on main but still open —
close it?", stale-pr → "PR #88 has had no update in 6 days", ci-blocked → "CI
red on g-401 for 2 days", forgotten-after-merge → "Merged 3 days ago, row still
says running". Each carries a one-click verb mapping to a real action.

**Card anatomy (fixed, ~4 lines):**

1. Title (one line, truncate) + type chip (`build`/`fix`/`change`/`review`/`query`)
2. State label + delivery chip (monospace, e.g. `pr-open`)
3. Task rollup: `3/5 tasks` + a thin progress bar; on goals with an active drive
   loop, a budget mini-bar (`turns 12/40`)
4. Last activity: relative time + actor, e.g. "14m · drive"; attention flag as a
   coloured left border

No description, no event history, no checkbox list. Four lines max. Click opens
goal detail in a right-side drawer, so board context survives.

## 4. Goal detail

1. **Header:** title, type chip, state, delivery chip, roadmap path. Action
   bar: `Answer` (if needs-input/blocked), `Reopen…`, `Release…`, `Close`,
   `Drive now`, `Open PR` (if pr-open). `Reopen` and `Release` open a required
   text modal — empty-submit says "Reopen needs what is being worked on — a
   question never reopens."
2. **Delivery line:** the derived delivery state + its source. This is where
   "derived, never stored" is made visible.
3. **Task list:** each task with its state; blocked/needs-input tasks carry the
   open question text inline.
4. **Timeline:** the event stream — decision records, state changes, claims,
   landings, with source and timestamp. Decisions render in full text;
   everything else collapses to one line.
5. **Decisions:** the subset that are decisions, pinned. One line each: the
   decision + who answered it.
6. **Claims & landings:** claim: who/what, by what source; landing: sha/PR,
   pushed/merged facts.

Query-type goals show a shorter variant: no tasks, no delivery section; timeline
+ decision + answer only.

## 5. States and feedback — exact sentences

| Situation | Exact string |
|---|---|
| Board loading | Skeleton columns with pulsing cards; no spinner text. |
| Nothing open | "Nothing open. Type in Talk what you want to be true." + three example chips. |
| Drive loop idle | "Drive is idle. Say `/drive` or type what should run." |
| Needs input (goal) | "Goal g-412 needs you: *Which auth provider?* — answer here or `/answer g-412 \"...\"`." |
| Blocked (task) | "Task t-88 is blocked: *waiting on staging deploy*. Resume when it's true." |
| ready-to-close | "PR merged; row still open. Close g-401?" |
| forgotten-after-merge | "Merged 3 days ago; goal says running. Fix the story." |
| API/WS error | "Lost the ledger feed. Retrying. Your last view is below." + Reload. Never blank screen. |
| Unknown `/` verb | "No command `/foo`. `/help` lists the real ones." |
| `/` arg missing | "`/reopen <id> <what is being worked on>` — reason required." |
| Action failed | Show the server's error verbatim; no sugarcoating rewrite. |

## 6. The rules of the UI

1. **The system shows its write before it writes.**
2. **Never show a command the ledger cannot verify.** Autocomplete is built from
   the same verb list the server accepts; unknown verbs error, never silently
   no-op.
3. **Every claim names its source.** Claims, decisions, landings always carry
   `who/what`.
4. **Delivery is derived, never stored.** The UI shows the value *and the git
   fact it came from*; there is no "edit delivery" control.
5. **A finished goal is finished until someone says what is being worked on.**
   Reopen demands its reason; no quick "unclose" button.
6. **Questions never create rows.**
7. **One board, one Talk, one detail.** If a new idea doesn't fit one of these
   three answers to a question, it doesn't ship as a surface.
8. **Live or loud.** State is pushed over `/ws`; on disconnect the UI says so
   plainly and keeps the last view.

## 7. Anti-patterns

- **A command palette as the whole UI.** "Press ⌘K, type `wd goal new`" — that's
  the CLI in a browser.
- **Auto-committing interpretations.** The ledger is durable; guessing wrong
  leaves garbage rows.
- **Delivery as an editable dropdown.** It contradicts the derived model and
  produces lies.
- **A card with a description, five chips, and a scrollbar.**
- **Equal-weight states.** If queued and blocked look the same, needs-input gets
  lost.
- **A settings/automation page in v1.**
- **Disappearing history.** The timeline is the product's memory.
- **Silent failures.** A failed action with a toast that says "Done." Never.
- **Chat pretending it's the ledger.** The interpretation card is the boundary;
  keep it visible.

## Note for the engineer

Maps 1:1 onto the existing `/api/board`, `/api/goal/<id>`, `/api/work/<id>`,
`/api/action`, `/ws` — no new endpoints required for v1. The Talk interpretation
layer needs **one** new route that turns free text into a structured
interpretation (kind, title, fields, confidence, clarification).