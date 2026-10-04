# Web UI designs — three options and what they agree on

Three free models designed the UI independently on 2026-10-02, from the same
brief and the same grounding (`internal/serve/serve.go`, the state machine, the
delivery derivation).

| | Model | Document |
|---|---|---|
| **A** | `opencode/longcat-2.5-preview-free` | [web-ui-option-a-longcat.md](web-ui-option-a-longcat.md) |
| **B** | `opencode/fledge-alpha-free` | [web-ui-option-b-fledge.md](web-ui-option-b-fledge.md) |
| **C** | `opencode/muse-spark-1.3-contributor-free` | [web-ui-option-c-muse.md](web-ui-option-c-muse.md) |

A visual preview of the consensus is at [preview.html](preview.html) — open it
in a browser.

## The convergence is the finding

Three models, no coordination, and they landed in the same place on almost
everything that matters:

1. **Three surfaces.** Talk/chat, Board, Goal detail. All three explicitly
   refused a settings tab and a separate roadmap/decisions/events page.
2. **Preview before every write** — all three named this *the* defining
   interaction, independently. Enter classifies; only an explicit Confirm
   commits; the card shows the exact `argv` that will run.
3. **No new endpoints except one.** All three checked `serve.go`. The existing
   `/api/board`, `/api/goal/<id>`, `/api/work/<id>`, `/api/action`, `/ws` cover
   v1. The only new route is free-text → structured interpretation.
4. **Questions never create rows.** A question about a finished goal answers from
   the ledger and leaves it finished — the same invariant as `wd reopen`'s
   required reason.
5. **Delivery is derived, never declared.** Show the value *and* the git fact it
   came from. No control that lets anyone set it.
6. **Every claim names its source.** Unsourced assertions render as
   `Unconfirmed`.
7. **`/` commands map 1:1 to real CLI verbs.** C's phrasing is the sharpest:
   power users will diff them against `wd --help` once, lose trust, and leave.
8. **A finished goal is finished until someone says what is being worked on.**
9. **Few surfaces, no notification spam.** The loop runs unattended; the UI
   interrupts only for needs-input, blocked and attention flags.

## Where they diverge

**A adds a fourth surface: a Review tab** (claims, costs, reversals, taste
promotions, unlanded work). B and C fold decisions and events into the goal that
owns them.

- *For A:* the audit question — "what did the loop decide while I wasn't looking?"
  — is genuinely different from "what needs me?" and it's the question this
  product exists to answer. Putting it in the goal detail makes it something you
  find per-goal rather than something you're shown.
- *Against A:* three surfaces already; a fourth is a place the story fragments,
  which C's anti-patterns section argues at length.

**B merges `needs-input` and `blocked` into one board column**, showing which one
it is on the card. A and C keep them visually distinct, and C argues the stronger
case: if blocked and needs-input look the same, needs-input gets lost.

**B gives the board a delivery-aware column set** (Review includes "delivery ≠
none"; Landed requires `done` *and* a landing). A and C group by state into
three bands. B's is more informative; it is also the one that most risks
implying git state is a work state.

**C is the strictest about state separation** — work state and delivery state
never share a pill, delivery always reads as past-tense evidence, work state as
present-tense ownership.

## What I would build

The consensus, with A's Review surface and B's delivery-aware columns:

- **Three surfaces plus Review** — but Review reachable from the header, not a
  peer tab, until the board proves it earns one.
- **Five board bands**: Needs you · In flight · Ready to push · In review ·
  Ready to close, with the attention strip above them. Delivery refines a card,
  it doesn't define a column.
- **`needs-input` and `blocked` stay visually distinct** (C).
- **Work state and delivery never share a pill** (C).
- Preview-before-write exactly as all three described, showing the real argv.

## Open questions none of them answered

- **What happens to the interpretation classifier's failure mode.** All three
  assume confidence exists. What does the UI show when it's genuinely unsure?
- **How the board scales past ~200 cards.** Every design assumes a person can
  scan it.
- **Whether Review is worth it at all** until taste promotion has run a few times
  and there is something to audit.