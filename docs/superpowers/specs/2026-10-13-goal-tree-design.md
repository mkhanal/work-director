# Goal tree: sub-goals are outcomes, not an agent's steps

Approved 2026-10-13. Visual target:
[Goal Tree Design canvas](https://claude.ai/artifact/9CXtntt3b7zH2D21CFoZ45) — seven screens:
the goal overview, a nested sub-goal, a proposed breakdown, a course change, the finish-line
question, the conversation tab and the record tab.

## Problem

A goal is planned today as executor-sized changes ("Port `bump-version.js` to TypeScript",
"Add a self-test harness"). Those are how an agent works, not what a person judges. The
director's own chores ("refresh dependencies", "land and push the tag") show up as tasks
beside them. The real structure ("Release baseline", "Release-notes collector") is only a
heading, with no state, no done-when and no owner. Nothing records what "done" means for
the goal, so the person can't tell why a goal is still pending.

## Model

**A goal is a node.** A `work` row of kind `goal` holds:

- `title`: an outcome a person would say. It never names a file or a code verb.
- `done_when` (new column): one or two sentences checkable from a report, verify output or
  a PR, without reading code.
- `finish_line` (new column): what is handed back, e.g. discovery, research complete, a
  local build, a complete PR, merged, released. Free text from a known set, plus its
  `finish_source`: the goal, the repo, taste or the person.
- `parent`: another goal, or a roadmap item. Nesting goes as deep as the work needs.
- `state`:
  - **A leaf's** state comes from its agent and verification, as today.
  - **A parent's** state is rolled up from its children and never set by hand.
  - **Superseded** (new state) marks a goal a course change abandoned. Its history and work
    stay readable.

**A leaf goal is one handover.** One agent owns it end to end.

**Steps** (new table `step(work → work.id, n, text, state)`, state is todo, doing, done or
dropped):

- They hold the agent's own plan, from its plan checkpoint.
- The whole list is replaced on each plan or report.
- They are never handed out as separate work, and the UI folds them away by default.

**Order** (new table `after(work → work.id, waits_on → work.id)`, unique pair, siblings
only): a sub-goal waiting on another is shown as *waiting on X*, not "queued".

**Chores are not goals.**

- Dependency fixes, landing, tagging and nudges are events on the goal they serve.
- A chore that needs its own session hangs off that goal as a session, not as a child.

**Kind `task`** stops being a planning unit: it is how a leaf sub-goal is stored (see Storage and migration).

## Planning

**The plan brief asks for outcomes**, one level at a time:

```
NAME: Release notes
FINISH: complete PR | from: taste
- Every release is tagged and records what changed since the last one
  done when: each release has a vX.Y.Z tag and a change list grouped by type
- Whoever deploys gets a guide for each release
  done when: a release's guide covers migrations, env, flags and rollback
  after: 1
```

**What the brief requires:**

- Titles are outcomes.
- Every sub-goal has a done-when.
- No chore sub-goals: no setup, dependencies, landing or checks-only items.
- Each sub-goal is about one agent session, and a goal gets 1–7 of them.
- A goal small enough for one agent gets none and is handed over as a leaf.
- `ASK:` stays for a question the planner can't settle.

**Each brief carries the whole chain:** the top goal and its done-when, every ancestor, and
the sibling sub-goals with their states.

**When a sub-goal's turn comes**, the director hands it to an agent or plans it into its own
sub-goals. At its plan checkpoint an agent may reply:

- `SPLIT:` with the outcomes it proposes;
- `OFF-COURSE:` with a finding that breaks the premise of its sub-goal or an ancestor.

**Mode gates the breakdown:**

- **`ask`**: a breakdown is *proposed* until the person approves it (canvas screen 3).
- **`auto`**: it starts at once.

**Reshaping commands**, each recorded as a decision:

| Command | What it does |
|---|---|
| `wd goal edit` | changes a title or done-when; a running agent is told by message |
| `wd goal add` | adds a sub-goal |
| `wd goal drop` | supersedes it with a reason; its agent is stopped |
| `wd goal merge` | joins two; their steps join and one agent continues |
| `wd goal split` | turns one into a parent |
| `wd goal after` | reorders |
| `wd goal approve` | starts a proposed breakdown |

**Coordinator actions:**

- `TASK:` becomes `GOAL: <title> | done when: … [| after: …]` under a named parent.
- `SPLIT <id>` is added.
- Chores stay `SEND` or a chore session.

## Alignment

**When the director checks course:**

- a breakdown is made;
- a sub-goal finishes;
- an agent raises `OFF-COURSE:` or `SPLIT:`;
- the person records a decision or reshapes.

**What each check does:** it judges the open sub-goals against the top goal's done-when and
records a verdict event. The three verdicts:

| Verdict | Effect |
|---|---|
| On course | Nothing changes; the check is recorded |
| Correct | Siblings are added, edited, dropped or reordered |
| Pivot | The plan goes back to the nearest ancestor whose done-when still holds and is planned from there. Abandoned sub-goals become superseded, with why, and their agents stop. |

**Who acts:**

- **Correct and pivot** follow the product's mode.
- **The top goal is the exception.** A change to its title, done-when or finish line, or
  dropping it as the wrong goal, always waits for the person, whatever the mode (canvas
  screen 4).

## Finish line and done

**The finish line is decided per goal at planning**, in this order:

1. the goal's own meaning ("investigate…", "ship…");
2. the repo: its mode, its workflows, and what its past goals finished at;
3. the person's taste, e.g. the card "in auto mode, a complete PR is done".

**When the sources disagree or none applies**, the director asks: a multiple-choice question
with its recommendation and the sources shown (canvas screen 5).

- The answer is recorded as a decision.
- A repeated answer becomes a candidate card or a product setting, said in one line.

**The finish line decides which pipeline steps apply:**

- Discovery and research skip worktree delivery, verify and PR, and their agents are told not
  to change code.
- A local build skips the PR.
- Released runs through publishing.

**A goal is done when all three hold:**

1. every child is done or superseded;
2. the director judges its done-when met against reports, verify output and the PR;
3. its finish line is reached.

A leaf's done report that misses its done-when goes back to the same agent with what is
missing. A done goal moves to **Completed**.

**`goal_type`** (query, build, fix, change, review) stays as what the goal turned out to be.
The finish line is what it aimed at.

## Views (`wd serve`)

Computed in Go; the apps only render.

**Goal view**, at any depth:

- the breadcrumb, each level with its state;
- `done_when`, `finish_line` and `finish_source`;
- progress: done of total, superseded counted apart;
- `not_done_because`: open children, done-when not yet judged, finish line not reached;
- `needs_you`: questions from anywhere below, each with its path and the director's
  recommendation;
- `course`: the last verdict, when it was made, and the superseded branches with why;
- per running agent, its sub-goal and its `doing` step.

**Sub-goal row:**

- title, state and done-when;
- owner: an agent and its runner, or "planned into N";
- why it isn't moving: waiting on X, waiting for you, proposed or superseded;
- its steps;
- its needs-you count.

**Sidebar:**

- top goals only, banded **Needs you · Working · Completed**;
- each with progress, a needs-you count and, once completed, where it finished ("Complete PR
  #512", "Research");
- the open goal's branch shown under its row.

**Conversation:**

- every turn is tagged with its sub-goal path;
- filters: everything, you and the director, and one per sub-goal;
- a sub-goal finishing is an event line;
- an agent's question and the director's answer show together in one card;
- the text box can be addressed to the whole goal or to one sub-goal.

**Record:**

- a summary of counts: decisions (yours and the director's), course checks, questions,
  chores and reports judged;
- kind filters: decisions, course, questions, reports, chores, delivery;
- a sub-goal filter;
- one row per entry: time, kind, what, where, by whom.

**Proposal view:** the proposed breakdown or the change against the current one, with the
director's reasoning: why this breakdown, what was left out, how you'd judge it.

**Language:** every string a view serves is written for a person. File paths and code live
only in steps and reports.

## UI

macOS and Linux match the canvas: its spacing, type sizes, pills, state icons, sidebar rows,
cards, question card, filter chips and record table. Where the canvas and
`docs/design/desktop.md` differ, the canvas wins and `desktop.md` is updated to match.

**Tabs:** Overview (sub-goals), Conversation and Record. The current Tasks tab is replaced by
the sub-goal list on Overview.

## Storage and migration

- A sub-goal that is planned further is a `goal` row whose parent is a goal.
- A leaf sub-goal, one agent's handover, is stored as a `task` row: the existing spawn,
  report and verify machinery drives it unchanged. Views call both "sub-goals".
- `Tasks(goal)` returns every task under a goal at any depth, so callers that drive a goal's
  agents keep working however deep it nests.
- Existing ledgers open unchanged and no stored value is rewritten (the ledger's "Migration Is
  Additive Only"). Tasks sharing a `heading` under a goal are grouped into a sub-goal **at read
  time** in the views, with done-when "not set", the way `epic` reads as `goal`. New plans write
  real sub-goal rows.

## Requirements

Through `/lazyspec`, in the areas that own each behaviour:

| Area | Requirements |
|---|---|
| `coordinator` | the outcome plan brief and its parse; `SPLIT` and `OFF-COURSE`; alignment verdicts; the finish-line decision |
| `ledger` | steps; `after`; superseded; roll-up state; migration |
| `cli` | the `wd goal` reshaping commands and approve |
| `serve` | goal view, sub-goal rows, sidebar bands, conversation tags and filters, record |

Reworded or removed headings take their tests with them.

## Build order

1. Ledger: columns, `step`, `after`, superseded, roll-up, migration.
2. Planning: the brief, the parse, chores as events, the finish-line decision and its
   question.
3. Steps from agent checkpoints; the done-when judgement; finish-line-aware pipeline.
4. Alignment checks, correct and pivot, mode gating.
5. Serve views.
6. macOS, then Linux, matched to the canvas; Ubuntu re-check in Docker.

## Out of scope

- WinUI.
- Moving the director's plan into the agents' own todo tools; steps are read from reports.
