> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Delivery (Go)

Where a goal's work reached, read from the landing the ledger recorded, the
git repository the goal works in, and GitHub through `gh`.

## A Commit Landing Is Landed Once Origin's Base Branch Has It
- A commit landing is a sha, or a link ending in `/commit/<sha>`.
- It is `landed-commit` when `origin/<base>` contains the commit, else `committed-unpushed`.

## A Pull Request Landing Reads Its State From GitHub
- A pull-request landing is `pr-draft`, `pr-open` or `pr-closed-unmerged` as `gh pr view` reports it, and `landed-pr` once merged.

## Landed Work Is Shipped Once It Is Done
- Landed work that is done is `shipped`.
- Landed work that is not done carries the attention `ready-to-close`.

## Unlanded Work Is Read From Its Branch
- With no landing, commits on the branch that `origin/<base>` lacks are `pushed-unpr` when the branch's upstream has them all, and `committed-unpushed` otherwise.
- A branch with nothing beyond `origin/<base>` is `none`.

## What Cannot Be Read Is Said, Not Guessed
- A `gh` or `git` read that fails puts its message in `error`; the status says only what could be read.

## A Landing Link Is Checked Before It Is Recorded
- `Resolve` accepts a GitHub pull request or commit link that `gh` can read, and any other `https` link that answers with a status below 400.
- Anything else is refused naming the link and why.
