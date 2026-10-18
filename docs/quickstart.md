# Quickstart

From nothing to a goal being worked on, in about five minutes. You need git and at least one
coding-agent CLI: Claude Code (`claude`), opencode or codex.

## 1. Install

```
curl -fsSL https://raw.githubusercontent.com/mkhanal/work-director/main/scripts/install.sh | sh
wd doctor
```

`wd doctor` lists the agent CLIs it found on your PATH. One is enough.

Tab completion for your shell: `wd completion zsh > "${fpath[1]}/_wd"`, or
`source <(wd completion bash)`, `wd completion fish | source`, or
`wd completion powershell | Out-String | Invoke-Expression`.

On a Mac, the app bundles its own `wd`. Build it from a checkout:

```
scripts/build-macos.sh        # dist/Work Director.app
open "dist/Work Director.app"
```

## 2. Add a project

A project is a repository the director works in.

```
wd projects add my-app ~/code/my-app --runner claude --mode auto
```

- `--runner` is the agent that does the work: `claude`, `opencode`, `codex`, or one you define
  with `wd runner init`.
- `--mode auto` lets the director spawn work and report back; `ask` makes it confirm first.
- Checks come from the repository itself: its lockfile picks npm, pnpm, yarn or bun, and the
  `lint`, `typecheck`, `check`, `validate` and `test` scripts it defines are run; `go.mod`
  and `Cargo.toml` add Go and Rust checks. List your own under `verify:` in
  `~/.work-director/projects/my-app.md` only when the repository's are not the ones you want.

## 3. Start a goal

In the app, type what you want done on the **New goal** page and press Return. From a
terminal:

```
wd goal start my-app "Let people export their tasks to CSV, with tags and due dates"
```

The director first checks whether this continues a goal you already have, then plans it into
tasks and starts an agent on each, all on one shared branch.

## 4. Watch it work

The goal opens as a thread: what you asked, the plan, then what happened. The **Agents**
panel on the right shows the planner while it plans, then the supervisor, then every task's agent and the
subagents each one starts, with what each is doing, its model, its tokens and its time.
Choose any card to read its conversation, thinking and tool calls included.

- **A question** an agent asks appears at the end of the goal and in its conversation,
  answered in place: `wd answer <task> "<answer>"`.
- **The supervisor** keeps the goal whole. Whenever no task is working and something changed,
  it reads the reports and the combined work against what you asked. Then it tells a task
  what is missing, starts a new task for a gap no task owns, settles concerns tasks raise
  about each other, asks you only what only you can answer, and reports the goal done once
  every part fits. Its replies, and the agents it told or started, appear in the goal's
  thread: `wd goal supervise <goal>` runs one pass by hand.
- **A message** to the goal is kept as a decision and goes to its supervisor, which passes it
  to the tasks it concerns: `wd send <goal> "<text>"`. To one task: `wd send <task> "<text>"`.
- **Stop** a task from its card, or everything on screen by pressing Esc twice:
  `wd stop <id>`. A message it had not read yet comes back for you to edit.
- **Pause, resume, cancel**: every write runs at once and can be undone; a cancelled goal
  comes back with `wd resume <goal>`.

## 5. Land it

When a task reports done, it moves to review by itself. The toolbar then offers the next gate
as one button:

| Gate | Command | What it checks |
|---|---|---|
| Verify | `wd verify <id>` | the repository's checks pass in the work's own worktree |
| Land | `wd pr <id> [<url>]` | the work is on a pushed commit or a pull request that resolves |
| Soft-Done | `wd soft-done <id>` | a done report, a passing verify, and a landing |
| Done | `wd done <id>` | closes it |

## Where things live

- The ledger, projects and logs: `~/.work-director` (set `WD_HOME` to move it).
- Your taste rules travel to every agent through the taste plugin: see the README. Rules of
  your own go in `~/.work-director/taste/cards/<category>/<id>.md`; every brief reads them on
  the next command, and one with a shipped card's id replaces it.
- Everything the app does is a `wd` command, so a terminal and the app always agree.

## When the app crashes

`wd crashes` lists every crash of the desktop apps and of the `wd serve` they run, newest
first; `wd crashes <id>` shows one: why it crashed, what was on screen, the last things the
app did (pages shown, the names of its requests to `wd`), and the backtraces. Add `--json`
to hand it to an agent.

- Each crash is a folder in `~/.work-director/crashes/<time>-<macos|linux>-<pid>/`:
  `note.json`, and on macOS the system's crash report (`WorkDirector-*.ips`), copied there
  from `~/Library/Logs/DiagnosticReports` the next time the app opens.
- An exception the macOS app dies of writes the note as it dies. A Swift trap (an index out
  of range, a force unwrap) cannot be caught: the app keeps
  `crashes/running-macos-<pid>.json` up to date instead, and the next launch turns it into
  the note, the system's report giving the reason.
- A `wd serve` that panics leaves what Go printed in
  `~/.work-director/logs/serve-<time>-<pid>.log`.
- Release builds of the macOS app keep their function names. Their source lines are in
  `WorkDirector.dSYM`: `dist/` beside a local build, a `-dSYM.zip` asset on each release.
  With it anywhere Spotlight indexes, new crash reports carry file and line; for an older
  one, `atos -o WorkDirector.dSYM/Contents/Resources/DWARF/WorkDirector -l <load address>
  <addresses>`.
