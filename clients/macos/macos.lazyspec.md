> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Work Director for macOS

A native app that bundles `wd` and holds `wd serve --stdio` open as its only
link to the ledger. It renders the views `wd` computes; every write is a `wd`
command the person confirms.

## The App Runs The wd Bundled Beside It
- Inside an app bundle it runs the `wd` in the bundle's executables.
- Outside one it runs `$WD_BIN`.
- With neither it refuses to start, naming both. It never searches `PATH`.

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
Every state has a symbol and a label of its own; no two states share either.

## A Change Made Elsewhere Reaches The Board
When any `wd` process records an event, the app's board is read again within
two seconds.

## The App Opens On A New Goal <!-- no-test: a window's first selection is not observable outside SwiftUI; checked by launching the app -->
The app opens on the new-goal page of the product in scope, laid out like a
goal page and typed into the same bar as a running goal.

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
- A reader within 48 points of the bottom, or with nothing to scroll, is at the bottom.
- At the bottom, what comes in keeps the newest in view.
- Scrolled further up, the conversation stays where the reader is, until they are back at the bottom.

## A Question Is Answered In Place, One Answer Per Item
- Picking a single-choice option replaces the item's pick; typed words replace it too.
- Multiple-choice picks join in the order offered, then any typed words.
- A pick is sent as its label without a `(recommended)` mark.
- Nothing can be sent until some item has an answer; an item left without one is sent as `(no answer)`.
- Sending runs `wd answer <id>` with one answer per item, in the order asked.
- Letting the director decide answers each item with its recommended choice, or with the director's own judgement where none is recommended.

## The Person Chooses A Light, Dark Or The System's Appearance
- Settings offers System, Light and Dark, kept as `appearance`; System is the default.
- Light or Dark sets the whole app's look; every colour takes its value from that column of the design's tokens.

## The Key That Sends A Message Is The Person's Choice
- By default Return sends and Shift-Return starts a new line.
- With ⌘Return or ⌥Return chosen to send, that chord sends and Return starts a new line.
- Any other chord with Return starts a new line.

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

## A Goal Reads As One Conversation With Its Supervisor And Its Agents
- A goal's thread holds, oldest first, what was decided, said, asked, answered, reported, verified and landed on the goal or its tasks, and the state changes that end work.
- A supervisor's reply shows what it said without its action lines, then what was done, each with the task it told, started or filed, and then what was refused.
- A look shows as the supervisor looking, with why.
- A task that stopped without a report shows as such, with what it last said.
- A question reads with its answer, and the answer says whether the director gave it or the person did; the answer's message to the session is not shown again.
- The question a goal waits on the person for is answered in place, one answer per question, its choices offered as a session's are.

## A Goal Is Read As Its Overview, Its Conversation Or Its Record
- A goal page's tabs are Overview, Conversation and Record.
- Everything folds each run of two or more updates — looks, state changes, checks, landings and messages told to a task — into one row.
- A report reads as its verdict, what it said with it, its files and its named sections.

## The Sidebar Bands Top Goals By Whether They Need You, Work Or Are Completed
- Top goals and standalone work in the product in scope are banded Needs you, Working and Completed, each most recently active first.
- Needs you holds work in the Needs You band, and goals with a question below them waiting on the person.
- Completed holds done work; dropped, abandoned and superseded work is kept apart, folded.
- A row says where a done goal finished, else how many of its sub-goals are done, with how many questions below it need the person.
- A goal whose breakdown waits for approval is marked Proposed, with how many sub-goals it proposes.
- With a sub-goal open, its path from the top goal shows under the top goal's row.

## Work Is Renamed From Its Page Or Its Menu
- Renaming runs `wd rename <id> "<title>"` with the title trimmed.
- A blank title is not sent.

## The Next Gate Is One Button
- A work page offers the gate `wd` serves for it as one button, which runs the gate's argv.
- Work waiting on no gate has no such button.

## Work That Comes To Wait On You Is Announced Once
- Work that has moved into Needs You, Ready to Push or Ready to Close since the board was last read is announced, once.
- Nothing is announced from the first read.
- Work that stays in its band, or moves into any other, is not announced.

## A Product Is Added From A Folder
- Adding a product runs `wd projects add <name> <folder> --runner <runner> --mode <mode>`, and the product is offered once it is added.
- The name offered is the folder's own name in lower case, its words joined by hyphens.
- The agents offered are the runners `wd doctor` lists, those on PATH first chosen.

## A Correction Can Be Kept As Feedback
- With Remember on, what is said to work is sent and also filed as feedback on that work and its project, from the director.
- A message that cannot be filed as feedback says it was sent but not remembered.


## The Kit Reads A Goal As A Tree
- A goal reads its breadcrumb, done-when, finish line, progress, what holds it from done, what needs the person below it, its course and what was superseded, as `wd` serves them.
- Its sub-goals read as a tree at any depth, each with its state, owner, steps, order and what it waits on.
- A node that is a goal opens as a goal page of its own; a task, or a group of an older plan's tasks, does not.
- The board reads each top goal's progress, how many questions below it wait on the person, and where it finished.
- A goal's record reads as its counts and its entries, newest first.

## A Goal Page Shows What Done Means, What Needs You And Its Sub-Goals
- A goal page is headed by its breadcrumb from the product down, each ancestor a way to its page, and its title with a badge that says Needs you while a question waits below it.
- The overview opens with the goal's done-when, where its top goal finishes and who decided it, and its course with its verdict; a sub-goal's page leaves out where it finishes.
- The done-when and the course's why start with a capital letter.
- Every question below the goal is answered in place, headed by its place in the tree, each level that is a goal a way to its page.
- Sub-goals are listed as served, each with its state, its line and its badge; a question below a row marks it as needing the person.
- A sub-goal that is a goal says how many sub-goals it holds and how many are done, with the superseded ones counted apart.
- A goal row opens its own page; a task row folds its steps open and closed, all closed but the first working row's.
- Adding a sub-goal runs `wd goal add <id> "<outcome>" --done-when "<done when>"`; both are required.
- What holds the goal from done is listed as served.
- A sub-goal's page lists what the director did there, from its record.

## The Record Lists What Was Decided And Done, Filtered By Kind And Sub-Goal
- The record opens with five counts: decisions and how many were yours, course checks and corrections, questions and how many the director answered, chores the director ran, and reports judged.
- Kind chips narrow it to decisions, course, questions, reports, chores or delivery; a superseded branch counts as course.
- A sub-goal picker narrows it to what happened at or under one of the goal's sub-goals.
- Each row reads its time, its kind as a badge, what it says, where in the tree and by whom, newest first, grouped by day.
- Ledger text set where a sentence starts begins with a capital letter.

## The Conversation Tags Each Turn With Its Sub-Goal And Filters By It
- Every turn said under the goal is tagged with its sub-goal's path; the goal's own turns are not.
- Chips narrow it to everything, to what passed between the person and the director, or to what was said at or under one sub-goal; a sub-goal's chip shows its title cut to 28 characters.
- The decisions the ledger files when a goal is reshaped or settled belong to the record, not the conversation.
- A sub-goal finishing is a line of its own; an agent's question the director answered reads as one turn.
- The goal's plan opens it as a card of its sub-goals, each with its state.
- A message from the goal's page goes to the whole goal, or to one open sub-goal chosen in the composer, as `wd send <id>`; a goal's composer is on its Conversation only.

## A Change Waiting For The Person Shows What Would Change
- A breakdown waiting for approval shows each proposed sub-goal in order, numbered, with its done-when, what it starts after, and whether one agent takes it or it will be planned further.
- Approving runs `wd goal approve <id>`; editing, splitting and dropping a sub-goal run `wd goal edit`, `wd goal split` and `wd goal drop <id> "<why>"`, a drop always with why.
- A course change on the top goal shows the goal, its done-when and its finish line as they are and as they would be, with why, and the plan changes it brings.
- A plan change shows each change as a sentence; a finish-line question offers each finish line as a choice, named and explained.
- Each is answered through the question's own answer path, its recommended choice applying the change.

## A Proposal Says Why It Is Broken Down So
- Beside the proposal, Why this breakdown, Left out and How you'd judge it read the planner's reasoning, each only when the planner said it.
- With no reasoning, nothing shows beside it.

## A Proposal Is Reordered, Merged And Planned Again From Its Cards
- Proposed sub-goals show each after the siblings it waits on, otherwise in the order served.
- Dragging a card sets the order as dropped: each sub-goal waits on the one before it, the first on none; until a drag the planned waits stay.
- A drag writes only the waits that change, from the top, each as `wd goal after <id> <id|none>`.
- Merging two cards runs `wd goal merge <earlier> <later>`: the earlier card is kept.
- Re-planning with a note runs `wd decide <goal> "<note>"`, then `wd goal plan <goal>`; an empty note is not sent.

## A Person Changes Where A Top Goal Finishes
- The proposal's and the Overview's Finishes at offer the six finish lines, named as a finish-line question names them.
- Choosing one runs `wd goal finish <goal> "<line>"`.
- A sub-goal's page offers none.

## A Course Change Shows What Was Found And What Happens To Each Sub-Goal
- What was found and why change course show side by side.
- Each sub-goal after the change is a row badged Kept · done, Kept · working, Kept · not started, New, or Superseded with why.
- Keep answers with the keep choice and is labelled with it; Change course answers with the first choice.
- Editing the change answers with the first choice, then runs `wd goal edit <goal>` for a title or done-when and `wd goal finish <goal> "<line>"` for a finish line the person changed from the proposal; what they left is not written.
