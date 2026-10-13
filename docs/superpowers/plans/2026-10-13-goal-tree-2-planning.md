# Goal tree, phase 2: planning, agents' steps and reshaping

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans. Spec: `docs/superpowers/specs/2026-10-13-goal-tree-design.md`.
> Builds on phase 1 (`docs/superpowers/plans/2026-10-13-goal-tree-1-ledger.md`): read the
> ledger API it added (`Children`, depth-first `Tasks`, `SetDoneWhen`, `SetFinish`,
> `Supersede`, `SetSteps`/`Steps`, `SetAfter`/`After`, `core.Settled`, `core.RollupState`, and
> the rule that a goal under a goal cannot be transitioned by hand).

**Goal:** a goal is planned into outcome sub-goals with done-whens, order and a finish line; a
sub-goal is either handed to one agent (a `task` row) or planned further (a `goal` row) when its
turn comes; agents see the whole chain and report their own steps; a person reshapes the tree
with `wd goal` commands; `ask` mode holds a fresh breakdown until it is approved.

**Architecture:** the planner's brief and reply parsing live in `internal/coordinator`
(`coordinator.go`); writing the tree, starting sub-goals and the reshaping commands live in
`internal/cli` (`commands.go` `planGoal`, `supervise.go` `startUnder`, a new `goaltree.go` for
`wd goal …` reshaping); executor briefs in `internal/brief`; step parsing beside the report
parsing in `internal/coordinator`.

## Ground rules

Same as phase 1: `/lazyspec` (Skill `lazyspec:lazyspec`) for every requirement change, married
test in the area's `*_lazyspec_test.go`, test first, `go test ./...` + `scripts/lazyspec-check.sh`
+ `gofmt -l internal cmd` clean before each commit, commit to `main` and push after each task,
the two attribution lines on every commit, comments state invariants only, no `rm -rf`. Do not
touch `clients/`. Where an existing requirement contradicts this plan, stop and report it.

## Finish lines (core)

```go
type FinishLine string
const (
	FinishDiscovery  FinishLine = "discovery"     // a findings report; no code changes
	FinishResearch   FinishLine = "research"      // a written comparison and a pick; no code changes
	FinishLocalBuild FinishLine = "local build"   // built and runnable, with how to run it; no PR
	FinishPR         FinishLine = "complete PR"   // PR open, checks green, done-when met
	FinishMerged     FinishLine = "merged"
	FinishReleased   FinishLine = "released"
)
func ParseFinishLine(s string) (FinishLine, error) // case-insensitive, trims; names the set on refusal
func (f FinishLine) ChangesCode() bool             // false for discovery and research
func (f FinishLine) NeedsPR() bool                 // complete PR, merged, released
```

`ledger.SetFinish` takes a `core.FinishLine` (change phase 1's string; parse at the boundary).

Project files gain an optional `finish:` frontmatter field (`project.Project.Finish *core.FinishLine`),
parsed with `ParseFinishLine`; absent means unset. Add it to the template commented out the
way optional fields are (read `ProjectTemplate` first).

## Tasks

### Task 1: the plan brief asks for outcomes, and its reply parses into them

**Files:** `internal/coordinator/coordinator.go`, `coordinator.lazyspec.md`,
`internal/coordinator/coordinator_lazyspec_test.go`.

New signature:

```go
// Chain is a goal with what it serves: its ancestors, top first, and its siblings.
type Chain struct {
	Ancestors []core.Work // top goal first; empty for a top goal
	Siblings  []core.Work // the goal's siblings, with their states and done-whens
}
func GoalPlanBrief(goal core.Work, chain Chain, p *project.Project, kit project.Toolkit, past []core.FinishLine) (string, error)

type PlanOutcome struct {
	Title    string
	DoneWhen string
	After    []int // 1-based positions of earlier outcomes in the same reply
	Split    bool  // planned further before an agent takes it
}
func ParseOutcomes(texts []string) []PlanOutcome

type PlanFinish struct {
	Line    core.FinishLine
	Source  core.FinishSource // goal, repo or taste
	Unsure  bool              // the planner could not settle it
	Choices []string          // when unsure: the lines it offers, recommendation first
}
func ParseFinish(texts []string) (PlanFinish, bool)
```

The reply shape the brief demands (exact):

```
NAME: <the goal's name in a few words>
FINISH: <discovery | research | local build | complete PR | merged | released> | from: <goal | repo | taste>
- <an outcome a person would say, never a file or a code verb>
  done when: <one or two sentences checkable from a report, verify output or a PR>
  after: <numbers of earlier outcomes it waits on>      (optional)
  split: yes                                             (optional: too big for one agent)
```

When the finish line can't be settled (the goal's wording, the repository's `finish:` setting,
its past goals' finish lines and the taste card text the brief quotes disagree, or none
applies), the reply says `FINISH: ?` followed by `- <line>` choices, recommendation first,
marked `(recommended)`.

Brief rules (each a sentence in the brief): titles are outcomes; every outcome has a done-when;
no outcome only sets up, installs, lands, tags or runs checks (the director does those); 1–7
outcomes; a goal one agent can finish gets exactly one outcome; `ASK:` as today. The brief
lists the chain (top goal with its done-when and finish line, each ancestor, the siblings with
states) when there is one, the project's mode, `finish:` setting and the last five finish lines
of its finished goals (`past`). FINISH is asked only of a top goal; a sub-goal inherits its top
goal's.

Requirements (via `/lazyspec`; these replace `## The Goal Plan Brief Asks For Changes, Not
Checks`, `## A Heading Line Opens A Group`, `## A Task Line Is A Task` and
`## Tasks Before Any Heading Fall Under A Default Heading`, whose tests go with them;
`ParsePlan` and `PlanTask` are deleted once nothing calls them):

```markdown
## The Goal Plan Brief Asks For Outcomes, Not Changes
- `GoalPlanBrief` names the project and its path, carries the goal's title and detail, and demands the outcome shape: a title, a done-when, and optional order and split lines.
- It forbids outcomes that only set up, install, land, tag or run checks, and lists the project's checks as what the director runs.
- It names the specifications the repository keeps, or says it keeps none.
- A sub-goal's brief carries its top goal with its done-when and finish line, each ancestor, and its siblings with their states.
- A top goal's brief asks for a FINISH line and gives the repository's finish setting and its past goals' finish lines.

## An Outcome Line Is A Sub-Goal With Its Done-When And Order
- `ParseOutcomes` reads each `- <title>` line with its indented `done when:`, `after:` and `split:` lines.
- An outcome without a done-when is still read, with an empty one.
- `after:` numbers that are not earlier outcomes are dropped.

## A Plan Reply Says Where The Goal Finishes
- `ParseFinish` reads `FINISH: <line> | from: <source>` into a finish line and its source.
- `FINISH: ?` with listed choices reads as unsure, keeping the choices in order.
- A line or source outside its set reads as no finish at all.
```

- [ ] Married tests first, then implement; delete the replaced tests with their headings.

### Task 2: planning writes the tree

**Files:** `internal/cli/commands.go` (`planGoal`, `awaitPlan`), `cli.lazyspec.md`,
`internal/cli/cli_lazyspec_test.go` (read how existing plan tests fake a planner runner first).

Behaviour:
- Each outcome becomes, under the goal being planned, a `task` row (or a `goal` row when
  `split: yes`) with its done-when set; `after:` becomes `SetAfter` on the created rows.
- A top goal's FINISH is stored with `SetFinish`. When unsure, a question event with the
  choices is filed on the goal (body built with `coordinator.QuestionBody` so the apps render
  choices), the goal moves to needs-input, and the sub-goals are still filed.
- A note `plan: N sub-goals` is filed.
- Mode `ask`: the breakdown is proposed — the note reads `proposed: N sub-goals`, nothing
  starts, and `wd goal approve <goal>` (Task 5) starts it. Mode `auto`: unchanged, it starts.
- Planning a goal under a goal never transitions it (its state follows its children).
- The supervisor's `START <id>` on a `goal` row plans it (same `planGoal`, with its chain) and
  then starts its first unblocked leaves.

Requirements:

```markdown
## Planning Files Sub-Goals With Their Done-When And Order
- `wd goal plan` files each outcome under the goal: a task, or a goal when it is to be split, each with its done-when and what it waits on.
- A top goal's finish line is stored with where it came from; an unsure one is asked as a question with its choices, and the goal waits on it.
- In ask mode the breakdown is proposed and nothing starts until `wd goal approve`.
```

### Task 3: a sub-goal waits for what it is after

**Files:** `internal/cli/supervise.go` (`startUnder`), `internal/cli/commands.go` (wherever
tasks are spawned for a goal: `spawnTasks`, `goalRun`), `cli.lazyspec.md`.

```markdown
## A Sub-Goal Starts Only Once What It Waits On Is Settled
- Starting a sub-goal that waits on an unsettled sibling is refused naming the sibling.
- Starting every open sub-goal of a goal starts only those with nothing left to wait on.
```

### Task 4: agents see the chain and report their steps

**Files:** `internal/brief/brief.go` (task and goal briefs), `internal/coordinator`
(new `ParseSteps`, `ParseSplit`, `ParseOffCourse` beside the report parsing),
`internal/cli/supervise.go` (`tend`: after reading a transcript, store steps when they changed),
`internal/serve` only if a test there pins brief text. Read `cli.go` `cycle`/`briefFor` first.

Brief additions, for a task (leaf sub-goal): "Why this exists" listing the top goal with its
done-when and finish line and each ancestor between; "Done when"; "Finishes at" with what that
means for this agent (discovery/research: change no code, report findings; local build: no PR);
and the checkpoint contract:

```
## Checkpoints
First, before changing anything, reply with your plan as
STEPS:
- [ ] <step>
Repeat STEPS in every later message that changes it, marking [x] done, [~] doing, [-] dropped.
If this sub-goal is too big for one agent, reply SPLIT: followed by `- <outcome> | done when: <…>` lines instead of starting.
If you find something that breaks why this sub-goal or a goal above it exists, say OFF-COURSE: <what you found> and stop.
```

```go
func ParseSteps(texts []string) ([]core.Step, bool)        // the last STEPS block
func ParseSplit(texts []string) ([]PlanOutcome, bool)      // the last SPLIT block
func ParseOffCourse(texts []string) (string, bool)         // the last OFF-COURSE line
```

Requirements (`coordinator.lazyspec.md` for parsing, `cli.lazyspec.md` for storing):

```markdown
## An Agent's Steps Are Read From Its Last STEPS Block
- `ParseSteps` reads the last `STEPS:` block: `[ ]` todo, `[~]` doing, `[x]` done, `[-]` dropped.
- A transcript with no `STEPS:` block has no steps.

## An Agent Can Ask For A Split Or Say It Is Off Course
- `ParseSplit` reads the outcomes after the last `SPLIT:` line, each with its done-when.
- `ParseOffCourse` reads what the last `OFF-COURSE:` line found.

## An Agent's Brief Carries Why Its Sub-Goal Exists
- A sub-goal's brief carries its top goal with its done-when and finish line, each goal between, its own done-when, and the checkpoint contract.
- A sub-goal finishing at discovery or research is told to change no code.

## Tending A Goal Keeps Each Agent's Steps
- Each look stores an agent's latest steps when they differ from the ones stored.
- A SPLIT or OFF-COURSE an agent raised is filed once as a concern on its sub-goal, for the supervisor.
```

### Task 5: a person reshapes the tree

**Files:** new `internal/cli/goaltree.go`, the command table in `internal/cli/cli.go`,
`cli.lazyspec.md`, `README.md` command list. Every command records a decision event on the
goal it changes (`wd decide` style body), prints with `c.out`, and supports `--json` like the
rest.

```
wd goal edit <id> [--title "<t>"] [--done-when "<d>"]   # a running agent is told by message
wd goal add <parent> "<title>" --done-when "<d>" [--after <id>,<id>] [--split]
wd goal drop <id> "<why>"          # Supersede; stops running sessions under it
wd goal merge <keep> <other>      # supersedes <other> ("merged into <keep>") and joins its done-when onto <keep>'s
wd goal split <id>                 # a new goal under the task's parent takes its title and done-when; the task moves under it
wd goal after <id> [<id>,…]        # replaces what it waits on; none clears
wd goal approve <goal>             # starts a proposed breakdown
```

`split` must not rewrite a stored kind (ledger: migration is additive and rows keep meaning):
it creates the new goal row and re-parents the task under it with a new `ledger.Reparent(id,
parent)` that refuses anything but moving a task under a goal that sits under the task's
current parent. Add `Reparent` to `ledger.lazyspec.md` as
`## A Task Moves Only Under A Sub-Goal Beside It` with its married test.

Requirements (`cli.lazyspec.md`):

```markdown
## A Person Reshapes A Goal's Sub-Goals
- `wd goal edit`, `add`, `drop`, `merge`, `split` and `after` each change the tree as named and record a decision on the goal they change.
- Dropping supersedes the sub-goal and everything open under it, and stops their sessions.
- Editing a sub-goal an agent is working on tells the agent.

## A Proposed Breakdown Starts On Approval
- `wd goal approve` starts a proposed breakdown's unblocked sub-goals and records the approval.
- Approving a goal with nothing proposed is refused.
```

## Done when

All tasks are on `main`; `go test ./...`, `scripts/lazyspec-check.sh` and `gofmt` are clean;
`wd goal plan` on a real project (run it against `~/.work-director/projects/wd-demo.md` if it
exists, else report that it was not run) produces outcome sub-goals with done-whens.
