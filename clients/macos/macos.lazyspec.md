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

## A Write Shows The Exact Command It Will Run
The confirmation shows `wd` followed by the plan's argv, and confirming sends
that same argv.

## Text In The Command Bar Becomes Argv Without A Shell
- Words split on whitespace; single or double quotes keep a phrase together.
- Nothing is expanded: `$HOME`, `~` and `*` reach `wd` as typed.
- A leading `wd` is dropped. An unclosed quote is refused.

## Reopening, Releasing And Dropping Need A Reason
- A reopen, a release, a cancel, or an archive of open work cannot be planned
  with a reason that is empty or only whitespace.
- Archiving work at rest needs no reason.

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
- With no match it starts the goal in the same step.
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
