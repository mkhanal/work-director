# Workspace context

## A Workspace Report Says Where The Executor Stands
`workspaceContext` reports the repo top, whether the current repo sits in a linked worktree, the
current branch (or `detached <sha>`), and the changed files; a directory outside any git repo
reports exactly that, no git claims. `contextLine` renders it in one line a status header can print.