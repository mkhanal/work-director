> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Doctor (Go)

`wd doctor` reports what the host offers the director: which registered
runners it can use, and whether the directory it runs in is a repo. The
shell, the editor and git are assumed, never probed.

## Doctor Lists Every Registered Runner As Detected Or Not
Every built-in and spec-file runner is listed with the command it runs:
detected with its resolved path when that command is on PATH, else not
detected.

## Doctor Reports Whether The Directory Is A Repo
Inside a repo the report carries the repo and branch; outside one it says
there is no git repo.

## Doctor Says Where Each Setting Comes From
- The report's `config` names the director's home and whether `WD_HOME` set it, the ledger, the projects directory and how many projects load from it, and the runner files directory.
- It names where the taste cards come from, a checkout's directory or `built-in`, and the person's own cards directory when there is one.

## Outside A Repo Doctor Offers Git Init And Never Runs It
With no repo, the report offers `git init` and the directory stays no repo;
inside a repo nothing is offered.
