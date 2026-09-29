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

## Outside A Repo Doctor Offers Git Init And Never Runs It
With no repo, the report offers `git init` and the directory stays no repo;
inside a repo nothing is offered.
