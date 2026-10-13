# Goal tree, phase 4: the views `wd serve` computes

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans. Spec: `docs/superpowers/specs/2026-10-13-goal-tree-design.md`
> (section "Views"). Visual target: the canvas linked at the top of the spec — every field
> below exists because a screen there shows it. Builds on phases 1–3.

**Goal:** `GET /api/goal/<id>`, the board and a new record view carry everything the apps
render for the goal tree, computed in Go, so the macOS and Linux apps only draw.

**Architecture:** `internal/serve/views.go` builds the views; a new `internal/serve/tree.go`
builds the tree, paths, progress, needs-you, course and not-done-because from the ledger; a new
`internal/serve/record.go` builds the record. Fields are added; nothing the apps read today is
removed in this phase (`tasks` stays until the apps move to `tree`).

## Ground rules

As before: `/lazyspec` for `serve.lazyspec.md` with married tests in
`internal/serve/serve_lazyspec_test.go` (check its real name), test first, suite + lazyspec +
gofmt clean, commit to `main` and push, attribution lines, invariant-only comments, no
`rm -rf`, no `clients/` changes. JSON keys are snake_case like the existing ones; empty
collections serialise as `[]` (existing requirement).

## JSON added (exact)

On `GET /api/goal/<id>` (any goal, top or nested):

```jsonc
{
  "chain": [ {"id","title","state"} ],            // top goal first, ending at this goal
  "done_when": "…" | null,
  "finish": {"line":"complete PR","source":"taste"} | null,   // a nested goal shows its top goal's
  "progress": {"done":1,"total":4,"superseded":1},  // direct sub-goals; superseded not in total
  "not_done_because": ["3 sub-goals open","done-when not judged yet","no pull request yet"],
  "needs_you": [ {"work":"<id>","path":"Deployment guide › Rollback","question":{…runner.Question…}} ],
  "course": {"verdict":"on course|correct|pivot|unstated","why":"…","at":"…"} | null,
  "superseded": [ {"work":"<id>","title":"…","why":"…","at":"…"} ],
  "tree": [ node ],
  "paths": { "<work id>": "Sub-goal › Sub-sub-goal" }   // every work under the goal
}
```

`node`:

```jsonc
{
  "work": core.Work,            // for a heading group: a synthetic row, see below
  "group": false,               // true: tasks sharing a heading, grouped at read time
  "done_when": "…" | null,
  "after": ["<id>"],
  "waiting_on": ["<title>"],    // unsettled siblings it waits on
  "why_not_moving": "waits on Collector" | "waits for you" | "proposed — needs approval" | "superseded: …" | null,
  "owner": {"runner":"claude","agent":null} | null,   // a task with a session
  "planned_into": 3 | null,     // a goal row: how many sub-goals
  "progress": {"done","total","superseded"} | null,
  "doing": "Write the customer note template" | null,
  "steps": [ {"n","text","state"} ],
  "judged": {"verdict":"met|missed","why":"…"} | null,
  "needs_you": 1,
  "children": [ node ]
}
```

A heading group (legacy ledgers): tasks directly under a goal with the same non-empty
`heading` form one node with `group: true`, `work` = `{id: "group:<goal id>:<n>", title:
<heading>, kind: "goal", state: RollupState(children)}`, `done_when: null`, and the tasks as
children. Tasks with no heading, or `(no heading)`, stay direct children.

On `GET /api/board`, each goal (top goals only: a goal under a goal is not on the board) gains
`progress`, `needs_you` (count over the whole tree), `finish`, and, once done, `finished`:
`"Complete PR #512"` built from the finish line and the landing (`#<n>` from a GitHub PR URL,
else the short commit), or the finish line alone.

New `GET /api/record/<goal>` and stdio method `record`:

```jsonc
{
  "summary": {"decisions":5,"yours":2,"course":3,"corrections":1,"questions":2,"answered_by_director":1,"chores":2,"judged":1},
  "entries": [ {"event":<id>,"at":"…","kind":"asked|decision|finish|course|question|answer|judged|chore|report|delivery|superseded","what":"…","where":"Release notes (top)|<path>","by":"you|director"} ]
}
```

Newest first. `by` is `you` for decisions, answers and asks recorded from the person
(`wd decide`/`wd answer` invoked by the CLI user, and the goal's first ask), `director`
otherwise; read how decisions carry their maker today (`core.Decision`) and use that, adding
nothing to the ledger if it already says.

## Tasks

### Task 1: the tree, its paths and its progress

Requirement:

```markdown
## A Goal Serves Its Sub-Goals As A Tree
- `GET /api/goal/<id>` serves `chain`, `tree` and `paths` for any goal, top or nested.
- Each node carries its work, done-when, what it waits on and why it is not moving, its owner or how many sub-goals it was planned into, its progress, its current step and steps, its judgement and how many things under it need the person.
- Tasks of an older plan that share a heading are served as one grouped node with no done-when.
```

### Task 2: what done means and why it is not done

```markdown
## A Goal Serves What Done Means And Why It Is Not Done Yet
- `done_when`, `finish` with its source, and `progress` with superseded counted apart.
- `not_done_because` names, in order, the open sub-goals, a done-when not yet judged met, and a finish line not reached; empty once done.
```

### Task 3: what needs the person, and the course

```markdown
## A Goal Serves Everything Below It That Needs The Person
- `needs_you` lists each open question on the goal or under it, with the work it is on and its path.

## A Goal Serves Its Course
- `course` is the latest course event read into a verdict and why; `superseded` lists superseded sub-goals with why.
```

### Task 4: the board carries the tree's summary

Reword `## JSON Endpoints Serve The Board` bullets (keep heading) to add: only top goals;
each with `progress`, `needs_you`, `finish`, and `finished` once done.

### Task 5: the record

```markdown
## A Goal Serves Its Record
- `GET /api/record/<goal>` and the `record` method return a summary of counts and one entry per decision, finish choice, course check, question, answer, judgement, chore, report, delivery and supersession under the goal, newest first.
- Each entry says when, what, where in the tree, and whether the person or the director made it.
```

## Done when

All on `main`; `curl` against `wd serve` for a real goal in `~/.work-director` shows the new
keys (quote the output in the report); suite, lazyspec, gofmt clean.
