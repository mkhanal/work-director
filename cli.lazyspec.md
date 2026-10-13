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

## Your Own Cards Join The Taste Every Brief Carries
- Cards under `$WD_HOME/taste/cards` are read on every command alongside the checkout's or the embedded cards.
- A card there with the id of another replaces it; the rest are added.
- A card there that does not parse fails the command naming its file.

## Wd Taste Shows The Rules In Effect And What Was Learned
- `wd taste --json` prints `{cards, learned}`: every rule card a brief can carry, each with its `source` (`built-in`, the checkout's cards directory, or `yours`), and feedback grouped as `wd distill` groups it.
- Without `--json` it lists the cards by category, yours marked, then each group of feedback with its count.

## Errors And Exit Codes Match
- Usage errors, illegal transitions, not-ready refusals and unknown runners print their message and exit 1.
- A verify whose commands ran and failed records the failed verify, then exits 1.

## A Decision Is Recorded In One Line And Reaches The Brief
- `wd decide <id> <text>` records a `decision` event on existing work.
- The work's brief lists its decisions, its goal's decisions and its resolved concerns' decisions under "Decisions already made", and nothing else there.

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
`wd verify` on a project listing no verify commands, in a repository whose
toolkit gives no checks, runs nothing, exits 1 saying so, and records no
verify event.

## Wd Verify Runs What The Repository's Toolkit Gives
- A project listing no verify commands is verified with its repository's checks, as `ReadToolkit` finds them.
- A listed command for another package manager than the repository's is refused naming both, and nothing runs.

## A Session Started For One Answer Is Stopped Once It Answers
- The sessions the director starts to plan a goal, find a goal, judge and reflect are stopped once their reply is read.
- A runner file with no `stop` command leaves them as they are.

## A Goal Is Planned Where Its Planning Can Be Watched And Stopped
- `wd goal begin <goal>` plans a goal not yet started and spawns its tasks, as `wd goal start` does; any other goal is refused.
- `wd goal start <project> "<text>" --detach` files the goal, prints it with no tasks, and begins it in a process of its own.
- While it plans, the planning session is the goal's own session, recorded as a `spawn` event `plan <runner> <session> [<ref>]`.
- A planning session that ends without a plan ends planning at once, saying so, and planning that fails is filed on the goal as a note.
- `wd stop <goal>` stops the goal's own live session as well as its tasks'.
- The goal's own session is its latest planning or supervising session.

## A Planning Session That Fails Is Reported At Once With Why
- `wd goal plan` stops as soon as its planning session ends on an error, failing with that error, and files no sub-goals.

## Planning Files Sub-Goals With Their Done-When And Order
- `wd goal plan` files each outcome under the goal: a task, or a goal when it is to be split, each with its done-when and what it waits on.
- A top goal's finish line is stored with where it came from; an unsure one is asked as a question with its choices and what each source said, and the goal waits on it.
- Answering that question with one of its finish lines stores it, from the person.
- In ask mode the breakdown is proposed and nothing starts until `wd goal approve`.
- The planner's why, left out and judge by are filed on the goal as one `reasoning:` note carrying them.

## A Finish Choice Can Become The Product's Setting
- `wd answer <goal> "<line>" --remember` on a finish-line question also sets `finish:` in the project's file to that line: that one line rewritten, or added last in the frontmatter, every other byte kept.
- It records the decision `finish setting: <line>` on the goal, by the person.
- `--remember` on work not asking where it finishes, or with an answer that is not a finish line, is refused and nothing is answered.

## A Person Sets Where A Goal Finishes
- `wd goal finish <goal> "<line>"` sets a top goal's finish line, from the person, recording the decision `finishes at: <line> (from person)`.
- A goal under a goal is refused naming its top goal.
- A line outside the set is refused naming the set.

## A Sub-Goal Starts Only Once What It Waits On Is Settled
- Starting a sub-goal that waits on an unsettled sibling is refused naming the sibling.
- Starting every open sub-goal of a goal starts only those with nothing left to wait on.

## An Agent's Brief Carries Why Its Sub-Goal Exists
- A sub-goal's brief carries its top goal with its done-when and finish line, each goal between, its own done-when, and the checkpoint contract.
- A sub-goal finishing at discovery or research is told to change no code.

## Tending A Goal Keeps Each Agent's Steps
- Each look stores an agent's latest steps when they differ from the ones stored.
- A SPLIT or OFF-COURSE an agent raised is filed once as a concern on its sub-goal, for the supervisor.

## A Person Reshapes A Goal's Sub-Goals
- `wd goal edit`, `add`, `drop`, `merge`, `split` and `after` each change the tree as named and record a decision on the goal they change.
- Each such decision, and each done-when they set, is recorded as made by the person.
- Dropping supersedes the sub-goal and everything open under it, and stops their sessions.
- Editing a sub-goal an agent is working on tells the agent.

## A Proposed Breakdown Starts On Approval
- `wd goal approve` starts a proposed breakdown's unblocked sub-goals and records the approval.
- Approving a goal with nothing proposed is refused.

## A Goal Is Named By Its Planner Until A Person Renames It
- A goal its planner names takes that name as its title, noting it.
- A goal a person has renamed keeps their name when it is planned.

## Wd Rename Gives Work A New Title
- `wd rename <id> "<title>"` sets the work's title and notes the title it had.
- A blank title is refused.

## Wd Goal Start Files Nothing When The Request Says Nothing To Change
- A planner replying `ASK: <question>` makes `wd goal start` exit 1 with the question.
- The goal it filed is dropped with the question as the reason and archived, with no tasks.

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

## A Worktree Wd Makes Has Its Own Dependencies
- Making a goal's shared worktree or a task's private one runs the repository's install command in it, before any session starts there.
- An install that fails removes the worktree it was made for and fails naming the command and what it printed; the next start makes it again.
- Every brief says the dependencies are installed in the worktree, and never to link them from another checkout.

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
- `wd release <id> ["<what was true instead>"]`, also `wd goal release`, releases abandoned work, with `released` when no text is given.
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
- Each turn puts the goal's finished tasks through their remaining gates; a task with a done-when goes through them only once it is judged met.
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
- `wd pr <id> <url>` records the url as a `pull-request` landing once `Resolve` accepts it; a link that does not resolve is refused naming it, and nothing is recorded.
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

## Every Command Explains Itself
- `wd <command> --help` and `wd help <command>` print the command's usage and what it does, with no ledger opened.
- `wd help`, `wd --help` and `wd` alone list every command with what it does.
- `wd man` prints the same as a wd(1) man page.
- Help asked of a command wd does not have is refused, pointing at `wd help`.

## Wd Completion Prints A Shell's Completion Script
- `wd completion <bash|zsh|fish|powershell>` prints a script that completes every `wd` command name in that shell.
- `--json` prints `{shell, script}`.
- Any other shell is refused naming the four.

## Wd Archive Hides Work And Wd Unarchive Brings It Back
- `wd archive <id> ["<why>"]` and `wd unarchive <id>` apply the ledger's archive rules and exit 1 with its refusals.
- Open work archived without a `<why>` is dropped as `archived`.
- `wd status` and `wd status --all` leave archived work out; `wd status --archived` lists only archived work.

## A Project No Longer Used Is Archived With Its Work
- `wd projects archive <name> ["<why>"]` archives every work of the project and moves its file to `projects/archive/`, after which the project does not load.
- With open work and no `<why>` it is refused naming that work, and nothing moves.
- `wd projects unarchive <name>` moves the file back and unarchives the project's work.

## Wd Send Queues For A Busy Session And Never Forks It
- `wd send <task> "<text>"` queues the text and delivers it at once when the task's session is not running, saying `delivered`.
- To a running session it stays queued, saying `queued`, and the next coordination pass (`wd drive`, `wd goal review`) delivers it once the session is idle.
- Messages already waiting go out with it, as one message.

## Wd Answer Replies To The Question A Session Is Waiting On
- `wd answer <id> <answer>...` answers the question with options the work's session ends on: one answer per item, in the order asked.
- A count of answers other than the item count is refused naming the questions, and nothing is sent.
- Work in needs-input with no such question takes the answers as words.
- Work asking nothing is refused, pointing at `wd send`.
- Work in needs-input on a question the director filed takes one answer per question it asks, or words.
- The answer is recorded on the work as an `answer` event with the question it answers, as answered by the person, and reaches the session as `wd send` does.

## A Goal's Answer Reaches Its Supervisor, Never Its Planner
- On a goal, the answer is sent to the goal's supervisor as `wd send` does.
- A goal with no supervisor yet keeps the answer recorded, carries out what it changes, goes back to work, and says `recorded`.
- The session that planned the goal is never sent an answer.

## The Director Can Decide A Question Put To The Person
- `wd answer <id> --director` puts each item of the question the work waits on to the judgement model, with its top goal's title, done-when and finish line and the project's settled work.
- An item with choices is decided only by one of them; any other answer is a decline.
- When every item is decided, the answers flow as a person's would, carrying out what the question changes; the answer is recorded as by the director, and each item as a decision by the director with its reason.
- When any item is declined, nothing is answered, the question stays open, each decline is noted, and the command says why.
- `--json` prints `answered`, `decided` `[{question, answer, why}]` and `declined` `[{question, why}]` beside `told`, `answer` and `applied`.
- With no judgement runner available it fails, and nothing is answered.

## Wd Send To A Goal Keeps It As A Decision For The Goal's Supervisor
- `wd send <goal> "<text>"` files the text as a decision on the goal and has the goal's supervisor look at once, as `wd goal supervise` does, even while tasks are working.
- No task is sent the text; a task not yet started reads it in its brief.
- `--json` prints `{decision, looked}`.

## Wd Stop Interrupts A Session And Hands Back What It Had Not Read
- `wd stop <id>` stops a task's live session, working, idle or waiting, through its runner; the task keeps its state, and the next message continues the same session.
- Messages still queued for the task are withdrawn; `--json` prints `{ok, stopped, withdrawn}`, `withdrawn` holding their texts oldest first.
- On a goal it stops and withdraws for every open task under it.
- With no live session and nothing queued it is refused, saying there is nothing to stop.

## Wd Pause Stops Running Sessions And Keeps Their Conversations
- `wd pause <id>` stops the live session of a task through its runner, which keeps the conversation, and moves the task to paused.
- On a goal it pauses every open task under it, then the goal.
- A session that cannot be stopped is named, its task keeps its state, the goal is not paused, and the command exits 1.
- `wd drive` on a paused goal is refused naming `wd resume`.

## Wd Resume Continues What Was Paused
- `wd resume <id> ["<note>"]` returns each paused task, and a paused goal, to the state it had before it was paused.
- Each task with a session is sent "Resume where you left off." followed by the note, as `wd send` does.

## Wd Cancel Stops And Drops Work Until Wd Resume Brings It Back
- `wd cancel <id> ["<why>"]` stops every live session under the work and drops it and its open work, as `Drop` does, with the reason or `cancelled`.
- `wd resume <id>` on dropped work restores it as `Restore` does, and each restored task with a session is sent "Resume where you left off.".
- `wd archive` of open work stops running sessions the same way before it drops them.

## Wd Goal Find Asks A Model Which Goals A Request Continues
- `wd goal find <project> "<text>"` shows the project's goals, finished and archived ones included, to one bounded model call with the `interpret` role's model, through the project's runner or `--runner`.
- `--json` prints `[{goal, why}]`, the goal as its whole work row, most relevant first.
- A project with no goals answers `[]` without asking a model.

## Wd Goal Continue Picks A Goal Up Where It Stands
- `wd goal continue <goal> "<text>"` on an open goal sends it the text, as `wd send` to a goal does.
- On a paused goal it resumes the goal with the text as the note.
- On a done goal it unarchives it when archived, reopens it with the text as the reason, plans it and spawns its tasks, as `wd goal start` does.
- On a dropped or abandoned goal it is refused naming `wd goal start`.
- `--json` prints `{goal, tasks}`.

## Wd Goal Supervise Has The Goal's Supervisor Look Once No Task Is Working
- `wd goal supervise <goal>` on a running goal first delivers its tasks' queued messages to sessions that are not running, and notes each running task whose session stopped without a report, as serving does.
- When no task is working and a task reported, asked, was answered, changed state or was noted, a concern was raised, or the goal was decided on since the supervisor last looked, the supervisor looks.
- A decision on the goal since then has it look even while tasks are working.
- The first look starts the supervisor in the goal's shared worktree, else the project's path, on `--runner` else the goal's else the project's runner, with the supervision brief, and records a `spawn` event `supervise <runner> <session> [<ref>]`; a later look sends the brief to that session.
- Each look is a `look` event on the goal; nothing is looked at while the supervisor is working.
- Passes run at once, from any process, look once.
- `--json` prints `{looked, applied, actions, refused}`.

## Wd Goal Supervise Carries Out What The Supervisor Decided
- A supervisor that has replied since it was last read, and is not working, has the reply's actions carried out once.
- `SEND` tells a sub-goal's agent as `wd send` does; `GOAL` files a sub-goal under the goal or one of its sub-goals and starts it unless it waits; `START` starts a sub-goal not yet started; `SUPERSEDE` supersedes a sub-goal with why and stops its sessions; `EDIT` and `AFTER` change a sub-goal and tell its agent; `RESOLVE` resolves a concern.
- The decisions `GOAL`, `SUPERSEDE`, `EDIT` and `AFTER` record, and each done-when they set, are recorded as made by the director.
- `ASK` files a `question` on the goal, each question with its choices, and moves it to needs-input; `wd answer <goal>` reaches the supervisor.
- `DONE` files the summary as the goal's `report` and moves the goal to review.
- `DONE` is refused while a task of the goal is working or not yet started, naming that task.
- An action that cannot be carried out is refused with why, and the rest still are.
- A reply is carried out once, however many passes read it at once.
- The reply, what was done and what was refused are one `supervised` event on the goal.

## A Sub-Goal Closes Only When Its Done-When Is Judged Met
- A sub-goal in review closes on `MET` with the judgement filed.
- `MISSED` files what is missing, tells its agent, and puts it back to work.
- A sub-goal is never closed by its report alone.

## A Chore Runs As A Session On The Goal, Not As Work
- `CHORE` starts a session hung off the goal and files it and what it is for on the goal, adding no work.

## A Plan Change Waits For The Person In Ask Mode
- In ask mode a supervisor's GOAL, SUPERSEDE, EDIT and AFTER are filed as one question with the changes, not applied.
- Answering it with Apply carries them out; any other answer leaves the plan as it is.

## A Change To The Top Goal Always Waits For The Person
- PROPOSE files a question with the goal now and as proposed, whatever the mode, and the goal waits on it.
- A PROPOSE that says what was found keeps it with the question, whose body says `Found: …`.
- Answering Change course sets the title, done-when and finish line as proposed, from the person; any other answer changes nothing.

## A Goal Is Declared Done Only When Every Part Of Done Holds
- A supervisor's DONE is refused naming the first of: a sub-goal not settled, a finished sub-goal not judged met, the finish line not reached.
