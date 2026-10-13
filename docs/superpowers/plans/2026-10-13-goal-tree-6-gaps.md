# Goal tree, phase 6: the parts of the canvas still missing

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans. Spec: `docs/superpowers/specs/2026-10-13-goal-tree-design.md`.
> Look: `docs/design/goal-tree/{proposal,pivot,finishline}.png`. Builds on phases 1–5.

**Goal:** every part of the canvas works: the proposal's reasoning panel, the finish-line
question's sources and its "from now on" choice, the course change's what-was-found and its
Kept / New / Superseded list, changing a finish line, and letting the director decide a question.

## Part A — `wd` (one agent, Go)

Ground rules as phases 1–4: `/lazyspec` for every requirement change with married tests, test
first, `go test ./...` + `scripts/lazyspec-check.sh` + `gofmt -l internal cmd` clean per commit,
commit to `main` and push, attribution lines, invariant-only comments, no `rm -rf`, no `clients/`.

1. **The planner says why.** The plan brief asks for three more lines after the outcomes:
   `WHY: <why this breakdown>`, `LEFT OUT: <what the goal doesn't ask for and was left out>`,
   `JUDGE BY: <how a person would judge it done>`. `coordinator.ParseReasoning(texts)` reads them;
   planning files them as one `note` event `reasoning:` with a JSON payload `{why,left_out,judge_by}`
   (use the event payload column; reasoning is not a decision). Served on the goal view as
   `reasoning: {why, left_out, judge_by} | null` (the latest planning's).
2. **The planner says where its finish line came from.** With `FINISH: ?` the reply adds
   `SOURCES: goal: <…> | repo: <…> | taste: <…>` (any of the three). Stored in the finish
   question's `core.Change` payload; served as `change.sources {goal, repo, taste}`.
3. **A finish choice can be remembered.** `wd answer <goal> "<line>" --remember` on a finish-line
   question also writes the project's `finish:` setting (rewrite that one frontmatter line of
   `$WD_HOME/projects/<project>.md`, keeping the rest byte for byte) and records the decision
   `finish setting: <line>` by the person.
4. **`wd goal finish <goal> "<line>"`** sets a top goal's finish line from the person
   (`SetFinish`, source person, decision `finishes at: <line> (from person)`); a goal under a
   goal is refused naming its top goal; a line outside the set is refused naming the set.
5. **A course change says what was found and what happens to each sub-goal.** `PROPOSE:` gains
   an optional `| found: <what was found>` part; the question body adds `Found: …`. The served
   `change` for kind `course` (and `plan`) gains:
   - `found: "…" | null`,
   - `sub_goals: [{work: id|null, title, status: "kept"|"new"|"superseded", state: state|null, why: "…"|null}]`
     computed from the goal's current sub-goals and the change's actions (GOAL → new,
     SUPERSEDE → superseded with why, the rest kept with their state), in tree order then new.
6. **The director can decide a question.** `wd answer <id> --director` puts each open item of
   the work's open question to the judge (`internal/judge`, the same role `wd drive` uses, its
   model from `wd model`), with the goal's context; each item it decides is answered with its
   answer and recorded as decided by the director, with its reason; an item it declines stays
   open and the command says why. The answer then flows exactly as a person's would (resume,
   apply a change's payload, …). Fails loud when no judge runner is available.

Requirements (headings fixed; bullets yours): coordinator — `## A Plan Reply Says Why It Is
Broken Down So`, `## An Unsure Finish Line Says What Each Source Said`; cli — `## A Finish
Choice Can Become The Product's Setting`, `## A Person Sets Where A Goal Finishes`, `## The
Director Can Decide A Question Put To The Person`; serve — `## A Goal Serves Why It Was
Broken Down So`, `## A Course Change Serves What Was Found And What Happens To Each
Sub-Goal`, and a bullet on `## A Change Waiting For The Person Is Served With What Would Change`
for `sources`.

## Part B — the apps (one agent per app, in parallel, after A lands)

Each app renders, matching the canvas (rules as phase 5):
1. Proposal: the right-hand "Why this breakdown / Left out / How you'd judge it" panel from
   `reasoning`; "Finishes at" becomes a real picker running `wd goal finish`; "Merge with N"
   (`wd goal merge`), drag to reorder (`wd goal after`), "Re-plan with a note…" (both apps).
2. Course change: "What was found" and "Why change course" side by side; "Sub-goals after the
   change" as Kept · done / Kept · working / New / Superseded rows from `sub_goals`; the three
   canvas buttons (Keep it a discovery → answer the keep choice, Edit the change… → edit the
   proposed title/done-when/finish then answer, Change course · Recommended → answer the first
   choice).
3. Finish-line question: the "Why I'm asking: these disagree" sources block from `sources`;
   the "Finish … this way from now on" checkbox passing `--remember`.
4. "Let the director decide" runs `wd answer <id> --director` (both apps) and shows what it
   decided, or why it declined, as the answer turn.
5. Changing a finish line from the Overview's "Finishes at" card (a menu of the six lines).
