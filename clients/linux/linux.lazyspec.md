> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Work Director for Linux

A native GTK4 and libadwaita app that is installed with its own `wd` and holds
`wd serve --stdio` open as its only link to the ledger. It renders the views
`wd` computes; every write is a `wd` command.

## The App Runs The wd Installed Beside It
- It runs the `wd` beside its own executable.
- Without one there, it runs `$WD_BIN`.
- With neither it refuses to start, naming both. It never searches `PATH`.

## Wd Runs With The Person's Login Shell PATH
- Every `wd` the app starts gets as `PATH` what the person's login shell (`$SHELL`, else their account's shell, else `/bin/bash`) prints for it, then each inherited entry it lacks. Nothing else in its environment changes.
- The shell runs as an interactive login shell (`-l -i`), so it reads the person's profile and rc files.
- Whatever the shell prints around its `PATH` is ignored.
- When the shell fails or does not answer within 5 seconds, `PATH` is the inherited one plus whichever of `~/.local/bin`, `~/.opencode/bin`, `~/.cargo/bin`, `~/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `~/.npm-global/bin` exist, in that order.

## A Reply Reaches The Request That Carries Its Id
- Replies may arrive in any order; each completes the request with its id.
- A request outstanding when `wd` exits fails, naming the exit.
- An error reply fails its request with the kind and message `wd` sent.

## A Write Runs At Once And Shows The Command It Ran
- A write runs when chosen, with no confirmation; the command it ran shows as `wd` followed by the plan's argv.
- The argv sent is the plan's argv, unchanged.
- A cancel or a pause offers an undo that resumes it; an archive, one that unarchives it.

## Text In The Command Bar Becomes Argv Without A Shell
- Words split on whitespace; single or double quotes keep a phrase together.
- Nothing is expanded: `$HOME`, `~` and `*` reach `wd` as typed.
- A leading `wd` is dropped. An unclosed quote is refused.

## Only Reopening Asks For Words
- A reopen cannot be planned with text that is empty or only whitespace: it says what is being worked on.
- A cancel, a release and an archive are planned with no reason, and carry one only when given.

## Needs-Input And Blocked Never Look The Same
Every state has an icon and a label of its own; no two states share either.

## A Change Made Elsewhere Reaches The Board
When any `wd` process records an event, the app's board is read again within
two seconds.

## A Goal Opened Again Shows As Last Read While It Is Read Again
- Every read of a goal, its record and its next gate is kept; opening that goal again shows it at once as last read.
- The page is then read again, and is drawn again only when the new read differs.

## The App Opens On A New Goal <!-- no-test: a window's first page is not observable outside GTK; checked by launching the app -->
The app opens on the new-goal page of the product in scope, laid out like a
goal page and typed into the same composer as a running goal.

## A Product Scope Shows Only That Product's Work
With a product in scope, the board's bands, their counts and the recent goals
hold only that product's work; with none, every product's.

## A New Goal First Offers The Goals It May Continue
- Submitting a request asks `wd goal find` first and starts nothing while matches are offered.
- With no match it starts the goal in the same step, returning the goal at once while it is planned in the background.
- Continuing an offered goal runs `wd goal continue` with the request.

## A Session's Conversation Follows It, Reading Only What Can Still Change
- A task's page shows its session's steps as `wd` serves them: prompts, texts, thinking, tool calls and questions.
- Each later read asks from the first step that can still change: a tool call without its result, or a question without its answer.
- Steps the session adds, and results and answers that arrive, appear on that read.

## A Conversation Keeps The Newest In View Until You Scroll Up To Read
- A reader within 48 pixels of the bottom, or with nothing to scroll, is at the bottom.
- At the bottom, what comes in keeps the newest in view.
- Scrolled further up, the conversation stays where the reader is, until they are back at the bottom.

## A Question Is Answered In Place, One Answer Per Item
- Picking a single-choice option replaces the item's pick; typed words replace it too.
- Multiple-choice picks join in the order offered, then any typed words.
- A pick is sent as its label without a `(recommended)` mark.
- Each option is announced to assistive technology by the title it shows, without a `(recommended)` mark.
- Nothing can be sent until some item has an answer; an item left without one is sent as `(no answer)`.
- Sending runs `wd answer <id>` with one answer per item, in the order asked.

## The Key That Sends A Message Is The Person's Choice
- By default Enter sends and Shift+Enter starts a new line.
- With Ctrl+Enter or Alt+Enter chosen to send, that chord sends and Enter starts a new line.
- Any other chord with Enter starts a new line.

## Stopping A Session Puts Back What It Had Not Read
- Stopping runs `wd stop <id>`.
- The messages it withdraws come back as one text, oldest first, for the person to edit and send again.

## A Message Shows At Once And The Field Is Free Again
- Sending puts the message in the outbox at once, sending, and clears the field without waiting for `wd`.
- A message `wd` took is sent, and shows until the work's record holds it: a decision on a goal, or a prompt in its session.
- A message `wd` refused stays, with why; retrying sends it again, and what `wd` already took is not sent twice.
- What the person said on a goal, its decisions and the answers they gave, is set apart from what agents said and listed to jump back to.

## Escape Pressed Twice Quickly Stops The Work
- A second Escape within 0.8 seconds of the first stops the task or goal on screen, as the Stop button does.
- A lone Escape, or a second one later than that, stops nothing.
- A third quick Escape does not stop it again.

## The Agents Panel Shows Each Task And The Subagents It Started
- A goal's panel has a card for each task with a session, in the goal's order, with the subagents its session started under it.
- The goal's own session is the first card while it has one: titled Supervisor once the goal is supervised, else Plan.
- A card is asking when its session waits on a question, working while its session runs, resting while it is up with nothing to do, and settled otherwise; a subagent works until its last reply ends its turn.
- A card's time runs from its first record to now while it works, and to its last record once it does not.
- The panel counts settled cards and adds every card's tokens, subagents included.
- Choosing a subagent shows its own conversation.

## A Goal Page Shows What Done Means, What Needs You And Its Sub-Goals
- A top goal's page shows its done-when, where it finishes and where that came from, and its course; a sub-goal's page shows its done-when and its course.
- Each question waiting on the person anywhere under the goal is answered in place, or left to the director.
- Each sub-goal reads its state, then what makes it done, what it is doing, or what it waits on, and a badge; a replaced one is struck through with why.
- A sub-goal whose session stopped without finishing reads Stopped without finishing and the first line it last said, with Tell it to carry on, which runs `wd send <id> "Carry on where you left off."`; a blocked one reads why, when `wd` says.
- A sub-goal that is a goal opens its own page; a task's steps fold open and closed, closed except the first working task's.
- A sub-goal is added with its outcome and what makes it done.
- What holds the goal back from done is listed, and a sub-goal's page lists what the director did under it.

## A Sub-Goal Reads As Working Only While An Agent Works On It
- A sub-goal's badge says the state `wd` serves it, a group of an older plan's tasks included: Working, To review, Needs you or Blocked among them; its steps are counted on it only while it is working.
- The moving mark shows on a row only while a session in it or under it runs.
- A sub-goal waiting on the person since its agent stopped reads Needs you, says Stopped without finishing with the first line it last said, and offers Tell it to carry on.

## The Conversation Tags Each Turn With Its Sub-Goal And Filters By It
- Every turn on work under the goal is tagged with that work's path through the tree.
- It narrows to everything, to the person and the director, or to one sub-goal and everything under it, each choice with how many turns it keeps.
- A sub-goal finishing reads as one line, with how it was checked; the tree being reshaped is left to the record.
- A message goes to the whole goal or to any sub-goal chosen, as `wd send` to it.

## The Record Lists What Was Decided And Done, Filtered By Kind And Sub-Goal
- It counts decisions and how many were the person's, direction checks and how many corrected course, questions and how many the director answered, housekeeping tasks the director ran, and reports checked.
- Each entry reads its time, its kind, what it says, where in the tree it was recorded and who made it, newest first, under the day it fell on.
- Kind filters keep Decisions, Direction checks, Questions, Reports, Housekeeping or Delivery; a sub-goal filter keeps what was recorded on that sub-goal or under it.

## A Change Waiting For The Person Shows What Would Change
- A proposed breakdown shows each sub-goal numbered, with what makes it done, when it starts and whether one agent does it; each can be edited, split or dropped.
- A proposed breakdown is approved and started, planned again with a note, or discarded.
- A course change shows the top goal now and as proposed — its title, done-when and finish line — and why; a plan change shows each change it makes in a sentence.
- A question on where the goal finishes offers each finish line with what it means.
- Each is answered through the question's own answer.

## A Proposal Says Why It Is Broken Down So
- Beside the proposal, Why this breakdown, Left out and How you'd judge it read the planner's reasoning, each only when the planner said it.
- With no reasoning, nothing shows beside it.

## A Proposal Is Reordered, Merged And Planned Again From Its Cards
- Proposed sub-goals show each after the siblings it waits on, otherwise in the order served.
- Dragging a card sets the order as dropped: each sub-goal waits on the one before it, the first on none; until a drag the planned waits stay.
- A drag writes only the waits that change, from the top, each as `wd goal after <id> <id|none>`.
- Merging two cards runs `wd goal merge <earlier> <later>`: the earlier card is kept.
- Re-planning with a note runs `wd decide <goal> "<note>"`, then `wd goal plan <goal>`; an empty note is not sent.
- Each card's Edit, Split, Merge and Drop is announced to assistive technology with the card's number and title.

## A Person Changes Where A Top Goal Finishes
- The proposal's and the Overview's Finishes at offer the six finish lines, named as a finish-line question names them.
- Choosing one runs `wd goal finish <goal> "<line>"`.
- The menu is announced as Finishes at and the line it shows, not by the lines it offers.
- A sub-goal's page offers none.

## A Course Change Shows What Was Found And What Happens To Each Sub-Goal
- What was found and why change course show side by side.
- Each sub-goal after the change is a row badged Kept · done, Kept · working, Kept · not started, New, or Replaced with why.
- Keep answers with the keep choice and is labelled with it; Change course answers with the first choice.
- Editing the change answers with the first choice, then runs `wd goal edit <goal>` for a title or done-when and `wd goal finish <goal> "<line>"` for a finish line the person changed from the proposal; what they left is not written.

## A Finish-Line Question Says Why It Asks And Can Be Remembered
- Under its choices, Why I'm asking: these disagree lists what The goal, Your taste and This repo said, only those that said something.
- A checkbox, Finish goals in <product> this way from now on, sends the answer with `--remember`; unchecked, the answer is sent without it.

## Letting The Director Decide Shows What It Decided Or Why It Declined
- Letting the director decide runs `wd answer <id> --director --json`.
- An answer the director gave reads as its turn, each part with its answer and why.
- When it declines any part nothing is answered: a note says why for each part, and the question stays open.

## The Kit Reads A Goal As A Tree
- A goal at any depth reads as its breadcrumb, done-when, finish line, progress, what keeps it from done, what needs the person, its course and its sub-goals as a tree.
- Every work under a goal is named by its path through the tree.
- A sub-goal that is a goal opens its own page; a task, and a group of an older plan's tasks, do not.
- The board reads each top goal's progress, what needs the person, its finish line and, once done, where it finished.
- A goal's record reads as entries with their kind, place and maker, newest first, with its counts.

## A Goal Reads As One Conversation With Its Supervisor And Its Agents
- A goal's thread holds, oldest first, what was decided, said, asked, answered, reported, verified and landed on the goal or its tasks, and the state changes that end work.
- A supervisor's reply shows what it said without its action lines, then what was done, each with the task it told, started or filed, and then what was refused.
- A look shows as the supervisor looking, with why.
- A task that stopped without a report shows as such, with what it last said.
- A question reads with its answer, and the answer says whether the director gave it or the person did; the answer's message to the session is not shown again.
- The question a goal waits on the person for is answered in place, one answer per question, its choices offered as a session's are.

## A Message Waiting For The Director Says So
- A message of the person's on a goal that the director has not been given yet reads `Waiting for the director to finish the last one` under it, until the director is given it.

## A Goal Is Read As Its Overview, Its Conversation Or Its Record
- Everything folds each run of two or more updates — looks, state changes, checks, landings and messages told to a task — into one row.
- A report reads as its verdict, what it said with it, its files and its named sections.

## The Sidebar Bands Top Goals By Whether They Need You, Work Or Are Completed
- Top goals and standalone work in the product in scope are listed in bands, each most recently updated first.
- Needs you holds work with a question open anywhere under it, or waiting on a person to push or close it.
- Completed holds done work; dropped, abandoned and superseded work is listed apart as settled.
- A goal whose breakdown waits for approval says it is proposed.
- Everything else is working.
- A goal reads its progress, or its finish line before it has sub-goals, and how many questions need the person; a completed one reads where it finished and when.

## The Next Gate Is One Button
- A work page offers the gate `wd` serves for it as one button, which runs the gate's argv.
- Work waiting on no gate has no such button.
- The button says what it does: Check it, Deliver it, Confirm it's finished or Mark it done, or the words `wd` serves for the gate.

## A Notification Says What Happened And Where
- Its title says what happened: Question for you, Approve the plan, Change of course proposed, Choose where it finishes, Plan change to approve, Blocked, Stopped without finishing, Finished or Ready for review.
- Its body names the goal, then what about it: a question's first line and how many more came with it, how many sub-goals a plan has, the course's proposed title, what under it is blocked and why when `wd` says, what under it stopped and the first line it last said, or where it finished.
- A stop is told once for each work and moment it stopped.
- Work waiting on the person since its agent stopped is told as Stopped without finishing, not again as a question.
- Ready for review is told when work reaches Ready to Push, In Review or Ready to Close.
- Nothing is told from the first read, twice, or about work the person wrote to from the app in the last few seconds.
- Clicking it opens its goal.

## A Product Is Added From A Folder
- Adding a product runs `wd projects add <name> <folder> --runner <runner> --mode <mode>`, and the product is offered once it is added.
- The name offered is the folder's own name in lower case, its words joined by hyphens.
- The agents offered are the runners `wd doctor` lists, those on PATH first chosen.

## A Correction Can Be Kept As Feedback
- With Remember on, what is said to work is sent and also filed as feedback on that work and its project, from the director.
- A message that cannot be filed as feedback says it was sent but not remembered.

## Work Is Renamed From Its Page Or Its Menu
- Renaming runs `wd rename <id> "<title>"` with the title trimmed; the page and the sidebar show the new name once `wd` has it.
- A blank title is not sent.

## Work Under Way Shows What It Is Doing And For How Long
- While a session under the goal works, or a write the app started waits on `wd` (a message, an answer, a question left to the director, planning, continuing, approving), the conversation ends with a row for each: who, what it is doing now in one line, and how long since it started, ticking.
- A session started when its usage says it did, else at its latest step; a write when the app started it.
- The goal's own session is the Director; rows are newest first, three shown and the rest counted.
- What a session is doing reads from its latest step: the tool it runs, as Running `go test ./...`, Reading, Editing or Searching, else Thinking.
- A sub-goal on the Overview with a session working under it, and a goal in the sidebar with running work under it, carry a working mark, still when animations are off.

## Any Text The App Shows Can Be Copied
- Text the app shows can be selected and copied; where a click opens something, its menu has Copy.
- A turn's menu copies it as plain text, or as the markdown it was written in.
- Ctrl+C on a goal selected in the sidebar, or its menu's Copy, copies its title and id.

## Files And Images Go With What The Person Sends
- A file pasted or dropped on a composer waits above the field until sent or removed: an image as a thumbnail, any other file by its name and size; a pasted image is written as a PNG of its own; a folder is refused.
- Sending passes each file as `--attach <path>` to the command it runs: `wd send`, `wd goal start` or `wd goal continue`; files alone can be sent, except to continue a goal.
- A file shows on the message it was sent with: the one on the same work at the same moment; a file no message carries shows on its own, once per moment.
- An image opens in the person's default app; any other file opens the folder it is kept in.

## The App Speaks The Product's Language, Not wd's
- No label the app shows says superseded, judged, soft-done, briefed, needs-input, verify, land or claim, nor PR for a pull request.
- A state reads the same on a row's badge and its icon: Ready to start, Needs you, To review, Waiting to be confirmed and Replaced among them; dropped, abandoned and replaced work is announced as Set aside.
- A check of a sub-goal's done-when reads Checked: done or Checked: not done yet.
- A branch a change of plan let go of reads Replaced by a change of plan, with why.

## The Agents Panel Sits Beside The Page, Never Over It
- The panel sits beside the page, which narrows to make room for it.
- Dragging its edge resizes it between 260 and 480 pixels; it opens at 320 until a width is set, and the width set is remembered.
- Its toggle hides and shows it, and whether it shows is remembered.
- Only at a window width where the goals sidebar folds away does it open over the page.

## A Table In A Reply Reads As A Table
- A pipe table — a header row, a separator row of `---` cells, then its rows — shows as a table: the header semibold, a hairline between rows, each cell's words wrapping within its column.
- A column's separator sets its alignment: `:---` start, `---:` end, `:---:` centred.
- A cell's inline markdown is styled as anywhere else, and `\|` is a pipe inside a cell; a row short of cells is padded, one with too many is cut.
- Copied, a table reads as its rows, its cells separated by tabs.
- A blockquote, a horizontal rule and a bullet nested under another show as such.

## Copying Across The Conversation Keeps Every Line It Spans
- Between its interactive cards — a question with choices, a plan to approve — a conversation is one stretch of selectable text: who spoke and when, each step's line, replies with their tables, and what the person said. A drag or Ctrl+A selects across all of it; Ctrl+C copies it.
- Copied text reads as shown: words without their markdown marks, a table's cells separated by tabs, a folded step as its one line and an opened one with what it holds. Chevrons, icons and controls are not copied.
- Clicking a step's line opens or folds it; folded or open, its text can be selected.
- A refresh redraws only from the first turn that changed: what was shown before it keeps its place and its selection, and the reader stays where they scrolled.
