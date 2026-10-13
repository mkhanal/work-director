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

## A Goal Is Read As Its Overview, Its Conversation, Its Tasks Or Its Record
- The overview counts the goal's tasks as done, working, in review, blocked, waiting or dropped.
- The overview counts what waits on the person: the goal's own question and each task's.
- The conversation narrows to the person's words, the supervisor, reports or questions.
- Everything folds each run of two or more updates — looks, state changes, checks, landings and messages told to a task — into one row.
- A report reads as its verdict, what it said with it, its files and its named sections.

## The Sidebar Bands Top Goals By Whether They Need You, Work Or Are Completed
- Top goals and standalone work in the product in scope are banded Needs you, Working and Completed, each most recently active first.
- Needs you holds work in the Needs You band, and goals with a question below them waiting on the person.
- Completed holds done work; dropped, abandoned and superseded work is kept apart, folded.
- A row says where a done goal finished, else how many of its sub-goals are done, with how many questions below it need the person.
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
- Every question below the goal is answered in place, headed by its place in the tree, each level that is a goal a way to its page.
- Sub-goals are listed as served, each with its state, its line and its badge; a question below a row marks it as needing the person.
- A goal row opens its own page; a task row folds its steps open and closed, all closed but the first working row's.
- Adding a sub-goal runs `wd goal add <id> "<outcome>" --done-when "<done when>"`; both are required.
- What holds the goal from done is listed as served.
- A sub-goal's page lists what the director did there, from its record.
