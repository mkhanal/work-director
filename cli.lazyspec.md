> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# CLI rail (Go)

The CLI's commands, flags, `--json` schema, error messages and exit codes are
its contract. The `--json` rail is the contract column consumers (TUI, serve,
coordinators, executors) read: same keys, same value types.

## Work Items Serialize With The Wire Schema
Work objects carry exactly the keys id, project, title, detail, kind, state,
runner, session, ref, cwd, created, updated, parent, heading, claim, impact —
strings or null, kind and state from the closed enums.

## Ledger Objects Keep Their Column Names
Event, feedback, concern, worktree and conflict objects serialize with the
ledger's column names (id, work, kind, body, at; resolved_at; resolved), so a
consumer written against the TypeScript output reads the Go output unchanged.

## Empty Collections Serialize As Empty Arrays
A command that returns no rows emits `[]` for the collection, never null.

## Commands Compute The Same States And Values
Over the same fixture ledger, each command produces the same `--json` schema
and key values: states transition through the same machine, claims and impacts
record per task, conflicts pair overlapping claims, concerns resolve with a
decision.

## Doctor Emits Its Report On The Json Rail
`wd doctor --json` emits runners (runner, command, detected, path),
workspace (the `wd context` object) and init — path and init strings or
null.

## Doctor Exits Zero Whatever It Finds
Undetected runners and a directory outside any repo are reported, never a
failure: `wd doctor` exits 0.

## Runner List Shows Which Runners Are Detected
`wd runner list` shows every registered runner, whether it is built in, and
whether it is detected with its path; `--json` emits runner, command,
builtin, detected, path.

## Models Covers Only Detected Runners
`wd models` lists models for detected runners only; naming an undetected
runner fails with the not-detected guidance.

## Spawning To An Undetected Runner Fails Before Anything Changes
`wd spawn` and `wd epic plan|spawn|run` to an undetected runner exit 1 with
the not-detected guidance; the work item's state and worktrees stay as they
were.

## Status Marks Work Stale After Thirty Days Without Activity
Work that is neither done nor dropped and whose last activity — a state change
or any event — is over 30 days old is marked stale on its `wd status` row.
`--json` rows carry every work key plus `stale`, a boolean.

## A Human Closes Queued Briefed Or Blocked Work
`wd done <id> --cancelled "<reason>"` closes queued, briefed and blocked work directly and records the reason as a decision, so a closure that came through the soft-done gate is never later read as a cancellation; closing one of those states without the reason is refused and names the flag; running, needs-input and review work still refuses with the illegal-transition message.

## An Installed Binary Reads The Rule Cards It Was Built With
With no `WD_ROOT` and no `taste/cards` beside the binary or in the working
directory, `wd brief`, `wd spawn` and `wd scan` read the cards embedded in the
binary at build time.
`wd scan --adopt` there exits 1 naming `WD_ROOT` and writes nothing: adopting
writes a card into a checkout.

## Cards In A Checkout Replace The Embedded Ones
`$WD_ROOT/taste/cards`, else `taste/cards` beside the binary, else in the
working directory, is read instead of the embedded cards.
An edit to a card there reaches the next brief without a rebuild.

## Errors And Exit Codes Match
Usage errors, illegal transitions, not-ready refusals and unknown runners
print the same message and exit 1. A verify whose commands ran and failed
exits 1 after recording the failed verify.

## A Decision Is Recorded In One Line And Reaches The Brief
`wd decide <id> <text>` records a `decision` event on existing work. The
work's brief lists its decisions and its resolved concerns' decisions under
"Decisions already made", and nothing else there.

## Every Json Command Writes One Document
With `--json`, every command writes exactly one JSON document to stdout — no
progress lines, no document per spawned session. `wd tui`, which draws the
terminal, refuses `--json` and writes nothing to stdout.

## Projects Add Never Writes A File It Cannot Parse
`wd projects add` with a path or flag value its project file cannot hold —
an unknown mode, a line break, a value that would read back changed — exits
1 naming it and writes no file, so every later command still loads the
projects.

## Attach Records The Ref And Cwd It Is Given
`wd attach <id> <session> --ref <ref> --cwd <dir>` records that session,
ref and cwd on the work item.

## Set Cannot Skip The Soft Done Gate
`wd set <id> soft-done` passes the same readiness gate as `wd soft-done`:
work that is not ready exits 1 with the not-ready message and keeps its
state.

## Verify With No Commands Is Refused Without Recording
`wd verify` on a project that lists no verify commands is a refusal, not a
failed verify: it runs nothing, exits 1 saying so and records no verify
event.

## An Adopted Card Leaves The Promotion Candidates
After `wd scan --adopt <card>` writes the global candidate, `wd scan` exits 0
without listing that card, and adopting it again exits 1 with `no promotion
candidate <card>`, leaving the written candidate as it was.

## The Loop Decides For Itself What Becomes Global
`wd drive` asks about the taste each turn, through the same judge, the same
bound and the same record as an executor's question: the cards waiting to be
judged come from the promotion candidates, the brief names the card and its
evidence, and the decision is recorded on the work that supplied the most recent
piece of evidence, so a review reads why the taste changed and not only that it
did. A decided card is written global and adopted, the artifacts are rebuilt from
it, and the change is committed — staged to the card and the artifacts and
nothing else, because a loop that ran `git add -A` would sweep in whatever else
was in the tree. A declined card is left exactly as it was and the run names it
as judged not global, so a reader can see the loop looked rather than left to
wonder; the refusal is on the work and is not a claim, because the ledger never
gains a decision nobody made. Either way the run's own output names the card:
what the loop believes about itself changed, and the largest thing it did should
not have to be found later by somebody diffing the taste. A card promoted this
way is a candidate no more. `wd feedback add --work <id>` files evidence against
the work it came from and refuses a work that is not there, and a checkout
behind no card is no candidate: a taste the loop believes in but cannot ship is a
taste that quietly does not exist.

## Goal Run Spawns Only Children Not Yet Under Way
`wd epic run` spawns the open children that are queued or briefed, or
running with neither session nor claim. Children in review, needs-input,
blocked or soft-done, and running children with a session or a claim, keep
their state, session and claim. It reports how many it spawned; a run that spawns none leaves the
epic's state as it was.

## Send And Report Reach A Child Where Its Goal's Pass Does
`wd send` and `wd report` on a child with no session of its own reach its
claim, with its epic's runner else its project's, in its epic's active
shared worktree else the project's path: the session `wd epic review`
coordinates with.

## A Claim Names Someone
`wd claim <id>` on work with no session, and `wd claim <id> ""`, exit 1
with the claim usage and leave the claim as it was.

## Commands On Unknown Work Fail Naming It
Every command given a work id that does not exist — reading or writing —
exits 1 with `no work <id>` and records nothing.

## Flags That Do Not Parse Fail
A value flag with no value (`--tail=`, or `--ref` last or before another
flag), a switch given a value (`--json=x`), and a numeric flag that is not a
positive integer (`--tail`, `--timeout`, `--port`, `--count`) exit 1 naming
the flag; none falls back to its default.

## Report Without A Status Line Files Nothing And Fails
`wd report` on work whose last transcript message carries no STATUS line exits
1 saying the executor's report has not arrived yet; it records no event and
leaves the state as it was. A STATUS line of NEEDS-INPUT files its report and
exits 0.

## A Report Can Be Filed As Text, And The Gate Is The Same
`wd report <id> "<text>"` files the text as the report and moves the work to the
state the text's STATUS line names: DONE to review, BLOCKED to blocked, and
nowhere if the work is already there. Work whose row still says queued or
briefed goes to running on the way, because a report is a thing that has
finished and so the thing has run; a row that has never been moved is stale
rather than true, since nothing spawns work the director builds itself, and a
ledger that refused to believe a report it was just handed would have its state
and its record disagree. The report is the work author's own account of what it
did, so a text handed over is the same verdict as one read out of a session —
and it is the only account work built in the director's own session can have,
since there is no executor to read one from. The verify and pull-request gates
are untouched, a text with no STATUS line files nothing, and the report names
where it came from so a reader comparing two reports need not guess which was
read and which was filed. A work item with no session is told how to file a
report rather than only that it has none.

## A Standalone Task Closes Through The CLI Alone
A task with no epic goes from queued to done through `wd add`, `wd spawn`,
`wd report`, `wd verify`, `wd pr`, `wd soft-done` and `wd done`; nothing
else writes the ledger. `wd report` with no text and no session files nothing
and exits 1. A task whose latest report is not DONE is refused by
`wd soft-done`, even in review with a passing verify and a PR.

## Reflection Rides On The Report And The Goal Pass
`wd report` on a DONE report and `wd epic review` on children that just reported
DONE each ask one bounded question of the finished session's transcript, and
record one event naming the runner, model, tokens and verdicts. `--no-reflect`
skips it on both. Reflection is not a gate: it does not move the work's state,
write a card or satisfy the report, verify or pull-request gate, and a
reflection that cannot run is recorded on the work rather than failing the
command that triggered it.

## A Supervised Project Proposes Reflection Without Filing It
In mode `ask` a durable verdict is recorded as an event on the work but is not
added as feedback, so asking for a conversation's confirmation means something;
in mode `auto` it is filed with source `attached`. Either way the event is
recorded, so a declined reflection is as visible as a productive one.

## Report Files What The Coordinator Would
`wd report` on running or needs-input work, with or without an epic, files a
STATUS report as `wd epic review` files a child's: DONE moves it to review,
BLOCKED to blocked, NEEDS-INPUT to needs-input. A report already filed is not
filed again.

## Done Removes A Worktree Wd Made Once Its Branch Has Landed
`wd done` and `wd set <id> done` remove each worktree wd made for the work,
and its branch, when it is unlocked, has no local changes and its branch's
content is already where it lands: the epic's shared branch for a task's
private worktree; for an epic's shared worktree the project's default branch,
or that branch's upstream, fetched first, when it has one. A squash-merged
branch has landed. The worktree then lists as removed.

## Done Keeps A Worktree That Would Lose Work
A worktree wd made that is locked, has local changes or whose branch holds
work not landed stays on disk with its branch and state. `wd done` still
closes the work and exits 0, and raises a concern naming the worktree, each
reason and `wd worktree remove <id>`.

## Done Leaves Worktrees Wd Did Not Make
A worktree registered with `wd worktree attach`, or stored without an origin,
is never removed.

## Worktree Remove Retries The Worktrees Done Work Kept
`wd worktree remove <id>` applies the same rule to done work: it exits 0 once
nothing stays and 1 naming each worktree that still does. On work not done it
exits 1 and removes nothing.

## A Roadmap Holds Items And Turns Them Into Goals
`wd roadmap add <project> <title>` files a roadmap; `wd roadmap item <roadmap>
<title>` files an item under it; `wd roadmap plan <roadmap>` translates every
item still waiting into a goal on the same row, keeping the id, and records the
translation. An item already translated is left alone, so planning twice is not
an error and does not reset a goal that has begun. With no items left, `plan`
says so and exits 0. `wd roadmap show <id>` reads the roadmap, the items still
waiting, and every goal under it with its open-task count and how it ended;
`wd roadmap` lists the roadmaps. An item with no roadmap is refused.

## A Goal Says What Kind Of Thing It Was
`wd goal classify <id> <query|build|fix|change|review>` records the type and a
decision event saying what was claimed, and `--type` sets it at the moment the
goal is filed. A goal with no type reads as no type: nothing is defaulted, and a
type that reads as right and is wrong is worse than a missing one. A type
outside the set is not spellable, and one on work that is not a goal is refused.

## Work Ends Abandoned And Says Why It Did Not Ship
`wd abandon <id>` (and `wd goal abandon <id>`) ends work that stopped without
shipping, recording the reason and the detail. It takes any kind of work, not
only a goal: a task's pull request can go unmerged as easily as a goal's. With no reason given it is computed from the ledger: a
pull request that was raised and never merged reads `unmerged`, and no pull
request at all reads `no-pr`. A reason outside those two is not spellable,
because a reason nobody can verify is not a reason. Abandoned comes to rest, and
the one way out of it is `wd release <id> "<what was true instead>"` (also `wd goal
release <id> "<what>"`), which is covered on its own below.

## A Stop Recorded As A Failure Can Be Released Into A Choice
`wd release <id> "<what was true instead>"` (and `wd goal release <id> "<what>"`)
moves abandoned work to dropped and files the reason as a decision, and refuses
without one. Abandoning says a thing stopped without shipping, and for a
duplicate whose work landed under another id, or a probe that was never meant to
ship, that sentence is false. The reason is required for the same reason the
abandon reason is: nothing separates the failure from the choice except what the
person filing it says, so it is said. `wd set <id> dropped` cannot do it — the
reason is the whole content of the command — and the abandon event stays on the
row, because the work really did not ship and a reader sees the stop and then the
correction that stop was the wrong word for it. It refuses work in any state but
abandoned: releasing done work would be reopening it and renaming it at once.

## Wd Workspace Registers A Directory And Creates New Ones Only Under Permission
`wd workspace add <path> [--name <label>] [--creates]` registers a directory this director serves, and `--creates` marks it as a parent under which new workspaces may be made. `wd workspace list` prints them parents first, and `wd workspace show <id-or-path>` prints one. `wd workspace create <parent> <name>` makes `~/work/workspaces/<name>` and registers it: it resolves the path through the ledger's containment check, creates the directory, `git init`s it, registers it as a project and registers it as a workspace, so a project started with wd is a workspace without being registered twice. Permission lives with the machine, not the client — a remote actor can create a workspace anywhere this director is allowed to create one and nowhere else — so the command takes the parent explicitly rather than picking one.

## A Finished Goal Is Reopened Only For Work, Not For A Question
`wd reopen <id> "<what is being worked on>"` (and `wd goal reopen <id> "<what>"`)
works more on top of finished work and refuses without a reason, naming the reads
that answer a question instead — `wd context <id>`, `wd events <id>` — because a
finished goal that someone asks something of has not been reopened, and the state
machine cannot tell that from someone building on it. `wd add --goal <id>` on a
finished goal is refused naming the reopen, so no task is filed under a goal that
nothing will run it; `wd send` to a finished goal is refused the same way rather
than recording a message the work never accepted. A `query`-typed goal asked
about a finished one stands on its own and leaves that goal done: asking is not
reopening.

## Wd Epic Is The Old Spelling Of Wd Goal
`wd epic` reaches the same code as `wd goal`, `--kind epic` writes a goal, and
`--epic <id>` names the same parent as `--goal <id>`, so anything written
before the rename keeps working. A goal opened through either says `goal` in
`status` and `tasks`, and a row stored under the old name reads as a goal
without being rewritten.

## Review Is A Diff Over What The Loop Decided
`wd review [--project <name>] [--since <event-id>] [--json]` shows what the
loop decided since the last acknowledgement, each claim with its question and
answer, who decided it, which model and what it cost, when it was recorded and
when it took effect, what has been undone and why, which taste cards were
promoted under it, and which work stopped without shipping. `--since` reads a
window by hand without moving anything. With no `--project` and one registered
project it reads that one; with several it names them rather than guessing,
because a wrong guess answers a question about what a loop decided with a
confident lie. An empty pass says so rather than printing nothing.

## Acknowledging A Review Is What Makes The Next One Short
`wd review --ack` moves the project's cursors to the end of the pass. Reading a
pass and having seen it are separate acts, so a pass that was read but never
reached a reader comes round again rather than being skipped. A cursor refuses
to move backwards.

## A Decision Can Be Recorded In One Line Or In Its Parts
`wd decide <id> "<text>"` records one line of prose, which is what a person
types. With `--question` and `--answer` it records the parts a review reads,
and `--source`, `--runner`, `--model` and `--tokens` fill in what it cost and
who decided. A claim with no question or no answer is refused, and
`--effective <when>` records when the answer took hold as distinct from when it
was written. A reversal needs no reason flag of its own: `wd review reverse
<event-id> "<reason>"` takes the reason as its argument, and one with no reason
is refused.

## A Reversal Is A New Decision, Not An Edit
`wd review reverse <event-id> "<reason>"` records that a decision no longer
stands as a fresh decision naming the one it undoes, filed against the same
work so it lands in the same stream. The original claim, its body and the
moment it was recorded are left as they were made, and the original is marked
with what reversed it. Only a decision can be reversed, and the event must
exist. Reversing is a decision, so it reads in the audit like one.

## A Goal Runs Its Loop With Nobody Watching
`wd drive <goal-id> [--turns N] [--judgements N] [--tokens N] [--stalled N]
[--deadline <duration>] [--json]` runs the goal's loop: each turn coordinates
its open work, closes what has finished, and every question the ledger could not
answer goes to one bounded model judgement that is recorded as a decision with
what it cost before the answer reaches the executor waiting on it. It never asks
whether to carry on, and every bound it runs under can be set from the command
line; a bound nobody set is a bound nobody can reason about, and the judgement
bound defaults low because it counts how often the loop acts in a person's
place. A question is judged from the project's settled position and the model
may decline, and a decline is recorded as a decline — never as a decision the
ledger then believes was made. Only a goal can be driven.

## A Run That Did Not Ship Ends The Goal Abandoned, And Says Why
When a run stops because a bound ran out or nothing moved with work left, `wd
drive` ends the goal abandoned with the stop and the reason in it, and ends
every task that was still open with the same words. A task left running under a
goal that has come to rest would claim work is in progress when nothing is
driving it, and the board is read as a statement about the world. A run that
stopped because the loop itself could not run does not end anything: the work
is untouched, and ending a goal because a runner could not read a transcript
would throw away real work over a failure that says nothing about it.

## A Run That Lands Every Task Closes The Goal
Each turn puts the goal's finished work through the gates that are left, and a
run that lands every task then puts the goal through its own: the report written
from the run and the tasks it closed, the project's verify commands in the
goal's own directory, the landing link, soft-done and done. A goal whose tasks
are all done sitting in running is the last thing a person has to come and do,
and a driver that stops there has replaced supervision with a queue.

Each report is tried once. A gate that refuses — a verify that fails, a landing
link that cannot be worked out — is written on the work naming itself and then
left alone, and the run names the work and the gate it is held at. Re-running a
failing build on a loop is how a run spends its whole budget proving the same
thing, and a gate the loop cannot pass is a question for a person, not a retry.
A later report is a new attempt, because new work deserves a new answer.

A goal with a dropped or abandoned task came to rest without shipping: nothing
is left to drive, the run says which task stopped it, and the goal is left
alone, because only a person knows whether that goal was worth finishing another
way. A goal whose own row still says queued while its tasks run is moved to
running at the point its work has all landed — the ledger can justify that
transition, and a board saying queued about a goal whose work all landed is a
board that is lying.

## The Landing Link Is Derived Rather Than Typed
`wd pr <id> <url>` records the url given. `wd pr <id>` works it out: the commit
the work's own branch is at, on the project's remote, as a permalink. The gate's
question is whether the work landed somewhere a person can read, and a commit
already pushed to main answers that as well as a pull request does — it is
already true, and a gate only a person typing a link can pass is a gate an
unattended loop cannot pass at all.

The link is given only when the commit is on a remote branch. A permalink to a
commit nobody has pushed does not open, and a gate satisfied by a dead link is
not a gate; that work is left unlinked and told so, and `wd pr <id> <url>` is
the way through for a real pull request. Verify and the landing link resolve the
work's directory the same way, so a goal cannot be verified in one tree and
linked from another.

The kind of landing is recorded with it — `commit` for a link the tool worked
out, `pull-request` for a url given — because the command is the only thing that
knows which it was, and an audit that cannot tell a change that went straight to
main from one that waited for a merge cannot say whether anything was reviewed.

## A Run Reports Where A Person Is Still Needed
A run that stopped without shipping says which questions it settled, which it
could not, and which work is still open, so the cost of running without a person
is a list rather than a silence. A goal that was never touched is unfinished, not
complete: nothing running is not the same as nothing left.
