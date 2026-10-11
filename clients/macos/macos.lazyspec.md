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

## A Question Is Answered In Place, One Answer Per Item
- Picking a single-choice option replaces the item's pick; typed words replace it too.
- Multiple-choice picks join in the order offered, then any typed words.
- Nothing can be sent until every item has an answer.
- Sending runs `wd answer <id>` with one answer per item, in the order asked.

## The Key That Sends A Message Is The Person's Choice
- By default Return sends and Shift-Return starts a new line.
- With ⌘Return or ⌥Return chosen to send, that chord sends and Return starts a new line.
- Any other chord with Return starts a new line.

## Stopping A Session Puts Back What It Had Not Read
- Stopping runs `wd stop <id>`.
- The messages it withdraws come back as one text, oldest first, for the person to edit and send again.

## Escape Pressed Twice Quickly Stops The Work
- A second Escape within 0.8 seconds of the first stops the task or goal on screen, as the Stop button does.
- A lone Escape, or a second one later than that, stops nothing.
- A third quick Escape does not stop it again.

## The Agents Panel Shows Each Task And The Subagents It Started
- A goal's panel has a card for each task with a session, in the goal's order, with the subagents its session started under it.
- While a goal is planned, its planning session is the first card, titled Plan.
- A card is asking when its session waits on a question, working while its session runs, resting while it is up with nothing to do, and settled otherwise; a subagent works until its last reply ends its turn.
- A card's time runs from its first record to now while it works, and to its last record once it does not.
- The panel counts settled cards and adds every card's tokens, subagents included.
- Choosing a subagent shows its own conversation.

## The Sidebar Lists Work As Threads, Most Recently Active First
- Goals and standalone work in the product in scope are listed most recently updated first.
- Work not started yet is listed apart as queued, and work at rest apart as settled.
- Choosing a band shows only the work in it.

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

