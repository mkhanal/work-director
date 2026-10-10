> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# CLI rail (Go)

The CLI's commands, flags, `--json` schema, error messages and exit codes are
its contract. A rule the ledger enforces is specified in `ledger.lazyspec.md`;
here is only what the CLI adds: the command, its flags, its output and its exit.

## Work Items Serialize With The Wire Schema
Work objects carry exactly the keys id, project, title, detail, kind, state,
runner, session, ref, cwd, created, updated, parent, heading, claim, impact,
goal_type, archived — strings or null, kind, state and goal_type from the closed enums.

## Ledger Objects Keep Their Column Names
Event, feedback, concern, worktree and conflict objects serialize with the
ledger's column names (id, work, kind, body, at; resolved_at; resolved).

## Json Collections Are Empty Arrays, Never Null
A command that returns no rows emits `[]` for the collection.

## Commands Compute The Same States And Values
Over one fixture ledger, each command's `--json` has a fixed schema and its
values follow the state machine: claims and impacts record per task, conflicts
pair overlapping claims, concerns resolve with a decision.

## Doctor Emits Its Report On The Json Rail
`wd doctor --json` emits `runners` (runner, command, detected, path),
`workspace` (the `wd context` object) and `init` — path and init strings or null.

## Doctor Exits Zero Whatever It Finds
Undetected runners and a directory outside any repo are reported; `wd doctor`
exits 0.

## Runner List Shows Which Runners Are Detected
`wd runner list` shows every registered runner, whether it is built in, and
whether it is detected with its path; `--json` emits runner, command, builtin,
detected, path.

## Models Covers Only Detected Runners
`wd models` lists models for detected runners only; naming an undetected runner
fails with the not-detected guidance.

## Spawning To An Undetected Runner Fails Before Anything Changes
`wd spawn` and `wd goal plan|spawn|run` to an undetected runner exit 1 with the
not-detected guidance; the work's state and worktrees stay as they were.

## Status Marks Work Stale After Thirty Days Without Activity
- A `wd status` row is stale when the work is neither done nor dropped and its last state change or event is over 30 days old.
- `--json` rows carry every work key plus `stale`, a boolean.

## A Human Closes Queued Briefed Or Blocked Work
- `wd done <id> --cancelled "<reason>"` closes queued, briefed and blocked work and records the reason as a decision.
- Closing one of those states without `--cancelled` is refused naming the flag.
- Running, needs-input and review work is refused with the illegal-transition message.

## An Installed Binary Reads The Rule Cards It Was Built With
- With no `WD_ROOT` and no `taste/cards` beside the binary or in the working directory, `wd brief`, `wd spawn` and `wd scan` read the cards embedded at build time.
- There, `wd scan --adopt` exits 1 naming `WD_ROOT` and writes nothing.

## Cards In A Checkout Replace The Embedded Ones
- `$WD_ROOT/taste/cards`, else `taste/cards` beside the binary, else in the working directory, is read instead of the embedded cards.
- An edit to a card there reaches the next brief without a rebuild.

## Errors And Exit Codes Match
- Usage errors, illegal transitions, not-ready refusals and unknown runners print their message and exit 1.
- A verify whose commands ran and failed records the failed verify, then exits 1.

## A Decision Is Recorded In One Line And Reaches The Brief
- `wd decide <id> <text>` records a `decision` event on existing work.
- The work's brief lists its decisions and its resolved concerns' decisions under "Decisions already made", and nothing else there.

## Every Json Command Writes One Document
- With `--json`, every command writes exactly one JSON document to stdout: no progress lines, no document per spawned session.
- `wd tui` and `wd serve --stdio` refuse `--json` and write nothing to stdout.
- `wd serve --stdio` also refuses `--port`.

## Projects Add Never Writes A File It Cannot Parse
`wd projects add` with a path or flag value its project file cannot hold — an
unknown mode, a line break, a value that would read back changed — exits 1
naming it and writes no file.

## Attach Records The Ref And Cwd It Is Given
`wd attach <id> <session> --ref <ref> --cwd <dir>` records that session, ref and
cwd on the work.

## Set Cannot Skip The Soft Done Gate
`wd set <id> soft-done` passes the same readiness gate as `wd soft-done`: work
that is not ready exits 1 with the not-ready message and keeps its state.

## Verify With No Commands Is Refused Without Recording
`wd verify` on a project with no verify commands runs nothing, exits 1 saying
so, and records no verify event.

## An Adopted Card Leaves The Promotion Candidates
- After `wd scan --adopt <card>` writes the global candidate, `wd scan` exits 0 without listing that card.
- Adopting it again exits 1 with `no promotion candidate <card>` and leaves the written candidate as it was.

## The Loop Decides For Itself What Becomes Global
- `wd drive` puts each promotion candidate to the judge, under the run's judgement bound.
- The brief names the card and its evidence.
- The decision is recorded on the work that supplied the newest evidence.
- A decided card is written global and adopted, the artifacts are rebuilt, and a commit stages only the card and the artifacts.
- The run's output names the promoted card, and it is no longer a candidate.
- `wd feedback add --work <id>` files evidence against that work and refuses a work that does not exist.
- A card behind no checkout is not a candidate.

## A Card The Loop Judges Not Global Is Held And Nothing Is Written
A declined card's file is unchanged, nothing is committed, the decline is
recorded on the work and is not a claim, and the run's output names the card as
judged not global.

## Goal Run Spawns Only Children Not Yet Under Way
- `wd goal run` spawns open children that are queued or briefed, or running with neither session nor claim.
- Children in review, needs-input, blocked or soft-done, and running children with a session or claim, keep their state, session and claim.
- It reports how many it spawned; spawning none leaves the goal's state as it was.

## Send And Report Reach A Child Where Its Goal's Pass Does
`wd send` and `wd report` on a child with no session of its own reach its
claim, with its goal's runner else its project's, in its goal's active shared
worktree else the project's path — the session `wd goal review` coordinates with.

## A Claim Names Someone
`wd claim <id>` on work with no session, and `wd claim <id> ""`, exit 1 with
the claim usage and leave the claim as it was.

## Commands On Unknown Work Fail Naming It
Every command given a work id that does not exist exits 1 with `no work <id>`
and records nothing.

## Flags That Do Not Parse Fail
These exit 1 naming the flag, never falling back to a default:
- a value flag with no value (`--tail=`, or `--ref` last or before another flag);
- a switch given a value (`--json=x`);
- a numeric flag that is not a positive integer (`--tail`, `--timeout`, `--port`, `--count`).

## Report Without A Status Line Files Nothing And Fails
- `wd report` whose last transcript message has no STATUS line exits 1 saying the report has not arrived, records no event and keeps the state.
- A STATUS line of NEEDS-INPUT files its report and exits 0.

## A Report Can Be Filed As Text, And The Gate Is The Same
- `wd report <id> "<text>"` files the text as the report and moves the work to the state its STATUS line names: DONE to review, BLOCKED to blocked.
- Work already in that state stays there; the report is still filed.
- Queued or briefed work moves through running on the way.
- A text with no STATUS line files nothing.
- The report body says it was filed as text, not read from a session.
- Verify and landing gates are unchanged.
- On work with no session, `wd report <id>` with no text says how to file one.

## A Standalone Task Closes Through The CLI Alone
- A task with no goal goes from queued to done through `wd add`, `wd spawn`, `wd report`, `wd verify`, `wd pr`, `wd soft-done` and `wd done`.
- `wd report` with no text and no session files nothing and exits 1.
- `wd soft-done` refuses a task whose latest report is not DONE, even in review with a passing verify and a landing.

## Reflection Rides On The Report And The Goal Pass
- `wd report` on a DONE report, and `wd goal review` on children that just reported DONE, each ask one bounded question of the session's transcript and record one event naming runner, model, tokens and verdicts.
- `--no-reflect` skips it on both.
- Reflection does not move state, write a card, or satisfy a gate.
- A reflection that cannot run is recorded on the work; the command still succeeds.

## A Supervised Project Proposes Reflection Without Filing It
- In mode `ask` a durable verdict is recorded as an event and not added as feedback.
- In mode `auto` it is filed as feedback with source `attached`.
- The event is recorded in both modes.

## Report Files What The Coordinator Would
- `wd report` on running or needs-input work files a STATUS report as `wd goal review` would: DONE to review, BLOCKED to blocked, NEEDS-INPUT to needs-input.
- A report already filed is not filed again.

## Done Removes A Worktree Wd Made Once Its Branch Has Landed
- `wd done` and `wd set <id> done` remove each worktree wd made for the work, and its branch, when it is unlocked, clean, and its branch's content is already where it lands.
- A task's private worktree lands on its goal's shared branch; a goal's shared worktree on the project's default branch, or its upstream fetched first.
- A squash-merged branch has landed.
- A removed worktree lists as removed.

## Done Keeps A Worktree That Would Lose Work
A worktree wd made that is locked, has local changes or holds unlanded work
stays with its branch and state; `wd done` still closes the work, exits 0, and
raises a concern naming the worktree, each reason and `wd worktree remove <id>`.

## Done Leaves Worktrees Wd Did Not Make
A worktree registered with `wd worktree attach`, or stored without an origin,
is never removed.

## Worktree Remove Retries The Worktrees Done Work Kept
- `wd worktree remove <id>` on done work applies the same rule: exit 0 once nothing stays, else exit 1 naming each worktree that stays.
- On work not done it exits 1 and removes nothing.

## A Roadmap Holds Items And Turns Them Into Goals
- `wd roadmap add <project> <title>` files a roadmap; `wd roadmap item <roadmap> <title>` files an item under it.
- `wd roadmap plan <roadmap>` turns every waiting item into a goal with the same id and records it; items already turned are left alone.
- With no items waiting, `plan` says so and exits 0.
- `wd roadmap show <id>` lists the waiting items and each goal with its open-task count and how it ended; `wd roadmap` lists roadmaps.
- An item with no roadmap is refused.

## A Goal Says What Kind Of Thing It Was
- `wd goal classify <id> <query|build|fix|change|review>` records the type and a decision; `--type` sets it when the goal is filed.
- A goal with no type reads as none.
- A type outside the set, or on work that is not a goal, is refused.

## Work Ends Abandoned And Says Why It Did Not Ship
- `wd abandon <id> [no-pr|unmerged] [--reason "<detail>"]`, also `wd goal abandon`, abandons any kind of work.
- With no reason given it is computed: a landing never merged reads `unmerged`, none reads `no-pr`.
- Any other reason is refused.

## A Stop Recorded As A Failure Can Be Released Into A Choice
- `wd release <id> "<what was true instead>"`, also `wd goal release`, releases abandoned work and refuses without the text.
- `wd set <id> dropped` cannot release abandoned work.
- Work in any state but abandoned is refused.

## A Project Says What Its Judgements May Reach And Where They May Not
- `wd projects policy <project>` prints, per role, the runners its judgements may use, the runners its policy blocks, and whether it resolves.
- `wd model for <role>` prints the model chosen and every candidate tried with its outcome.
- `wd model set <role> --prefer m --runners r --model m` writes one role's policy to `$WD_HOME/judge.json`; `wd model reset <role>` clears it.
- A preference no detected runner offers is refused naming the runners that exist; a forced model whose runner is not in `--runners` is refused.
- Both judgement call sites resolve runner and model through the project's policy, and a runner the project does not allow is refused before anything is spawned.

## Wd Workspace Registers A Directory And Creates New Ones Only Under Permission
- `wd workspace add <path> [--name <label>] [--creates]` registers a directory; `--creates` makes it a parent new workspaces may be made under.
- `wd workspace list` prints them parents first; `wd workspace show <id-or-path>` prints one.
- `wd workspace create <parent> <name>` creates the directory under the parent, `git init`s it, and registers it as both a project and a workspace.
- The parent is always named; none is picked.

## A Finished Goal Is Reopened Only For Work, Not For A Question
- `wd reopen <id> "<what is being worked on>"`, also `wd goal reopen`, refuses without the text, naming `wd context <id>` and `wd events <id>`.
- `wd add --goal <id>` and `wd send` on a finished goal are refused naming the reopen.
- A `query` goal asking about a finished goal leaves that goal done.

## Epic Spellings Reach The Goal Commands
- `wd epic` runs the same code as `wd goal`.
- `--kind epic` writes a goal, and `--epic <id>` names the same parent as `--goal <id>`.
- A goal filed either way, or stored as `epic`, reads as `goal` in `status` and `tasks`.

## Review Is A Diff Over What The Loop Decided
- `wd review [--project <name>] [--since <event-id>] [--json]` prints the pass: each claim with question, answer, who decided, model, cost, recorded and effective time; reversals; promoted cards; work that stopped without shipping.
- `--since` reads a window without moving anything.
- With no `--project` and one project it reads that one; with several it refuses, naming them.
- An empty pass says so.

## Acknowledging A Review Is What Makes The Next One Short
`wd review --ack` moves the project's cursors to the end of the pass; a pass
read without `--ack` is shown again.

## A Decision Can Be Recorded In One Line Or In Its Parts
- `wd decide <id> "<text>"` records one line of prose.
- `--question` and `--answer` record a structured claim; `--source`, `--runner`, `--model` and `--tokens` fill in who decided and the cost.
- A claim with only one of question and answer is refused.
- `--effective <when>` records when the answer took hold.

## Wd Review Reverse Records A Reversal
- `wd review reverse <event-id> "<reason>"` records a reversal on the original's work.
- A missing reason, an unknown event, or an event that is not a decision is refused.

## A Goal Runs Its Loop With Nobody Watching
- `wd drive <goal-id> [--turns N] [--judgements N] [--tokens N] [--stalled N] [--deadline <duration>] [--json]` runs the driver over the goal.
- Every bound can be set from the command line.
- Each judgement is recorded as a decision with its cost before the answer reaches the executor.
- A decline is recorded as a decline, never as a decision.
- Only a goal can be driven.

## A Run That Did Not Ship Leaves The Goal Open And Reports Why
- When a bound runs out or nothing moves, `wd drive` leaves the goal and its tasks in their states, names the stop and what is still open, and records no abandon.
- A run that failed to run leaves the work untouched and names the failure.

## A Run That Lands Every Task Closes The Goal
- Each turn puts the goal's finished tasks through their remaining gates.
- When every task has landed, the goal goes through its own: a report naming the tasks it closed, the project's verify in the goal's directory, a derived landing link, soft-done, done.
- A goal still queued or briefed is moved to running first.
- `wd drive` on a goal at rest is refused with "nothing left to run".

## A Refused Gate Is Held On The Work And Tried Once
- A gate that refuses is written once on the work, naming the gate, and the run names the work and the gate.
- The gate is not tried again in that run; a later report is a new attempt.
- The goal is left open.

## A Goal Whose Task Ended Without Shipping Is Left Alone
`wd drive` on a goal with an abandoned task names that task as having come to
rest without shipping and leaves the goal's state and gates untouched.

## The Landing Link Is Derived Rather Than Typed
- `wd pr <id> <url>` records the url as a `pull-request` landing.
- `wd pr <id>` records a permalink to the commit the work's branch is at, on the project's remote, as a `commit` landing.
- A commit not on any remote branch, or a project with no remote, is refused naming the commit and `wd pr <id> <url>`, and nothing is recorded.
- SSH, `ssh://`, `https://` and `git://` remotes all yield a link.
- Verify and the landing link resolve the work's directory the same way.

## Wd Drive Prints Where A Person Is Still Needed
A run that stopped without shipping prints the questions it settled, those it
could not with their work, the bound that ran out, and the work still open.

## A Goal Starts From One Sentence
- `wd goal start <project> "<text>"` files a goal titled by the text's first line, cut at a word to at most 72 characters, with the whole text as its detail.
- It plans the goal with the project's runner, or `--runner`, and spawns its tasks, as `wd goal plan` and `wd goal run` do.
- `--json` prints `{goal, tasks}`, read after the tasks are spawned.
- A plan that yields no tasks leaves the goal filed and exits 1 naming the plan session.

## Wd Archive Hides Work And Wd Unarchive Brings It Back
- `wd archive <id> ["<why>"]` and `wd unarchive <id>` apply the ledger's archive rules and exit 1 with its refusals.
- `wd status` and `wd status --all` leave archived work out; `wd status --archived` lists only archived work.

## A Project No Longer Used Is Archived With Its Work
- `wd projects archive <name> ["<why>"]` archives every work of the project and moves its file to `projects/archive/`, after which the project does not load.
- With open work and no `<why>` it is refused naming that work, and nothing moves.
- `wd projects unarchive <name>` moves the file back and unarchives the project's work.
