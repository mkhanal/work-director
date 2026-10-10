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

## Reopening And Releasing Need A Reason
A reopen or release plan cannot be made with a reason that is empty or only
whitespace.

## Needs-Input And Blocked Never Look The Same
Every state has a symbol and a label of its own; no two states share either.

## A Change Made Elsewhere Reaches The Board
When any `wd` process records an event, the app's board is read again within
two seconds.
