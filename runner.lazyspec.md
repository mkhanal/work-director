> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Runners (Go)

The runners own each provider's commands, session discovery, transcript
parsing and error messages. The three foundation adapters are code; every other
provider is a TOML file of commands under `~/.work-director/runners/`.

## A Role Takes The Cheapest Model That Also Does The Job
- `Pick` chooses a role's model: `interpret` classifies text into a closed vocabulary, `taste` judges whether a rule generalises.
- The order is: the forced model; else a preferred model that passes the probe; else the cheapest model that passes, probing cheapest first and stopping at the first pass; else the runner's own default when nothing could be ranked; else a refusal.
- A model with no declared cost is unrankable, never free.
- A probe that could not be asked (no runner, quota or session) is recorded as not reached, distinct from failing.
- Every candidate tried is returned with its verdict.

## A Project Narrows The Policy And Can Never Widen It
- A role's policy is its runners, its preferred models, and optionally one forced model.
- `Resolve` narrows the global policy with the project's: a project runner list is checked against the global list, not substituted for it.
- A project may remove and reorder runners; a runner the global policy does not allow is refused.
- A forced model whose runner is not allowed is refused.
- `PromoteGlobal` defaults to true.
- Every refusal names what is allowed.

## A Model Outside The Allowed Runners Is Refused By Name
- `RolePolicy.Allowed` refuses a model whose runner is not listed, naming the runners that are.
- Under a forced model, every other model is refused.

## A Runner That Reports Nothing Is Asked Rather Than Guessed At
- Model costs come only from `$WD_HOME/models.json`; an absent file is not an error.
- With nothing declared and nothing rankable, `Pick` returns the runner with no model, so the runner's default is used.
- Every model name the ladder uses comes from a runner's own listing or from that file; none is hardcoded.

## Spawning Records Runner Session And Attach Hint
`spawn` returns a handle with the runner's session id and a hint a human can run to join the session.

## Sending Continues The Same Session
`send` addresses the runner with the handle's session id, never starting a new session.

## A Transcript Yields The Executor Messages
`transcript` returns the assistant texts of the session, oldest first, from the runner's own store.

## A Transcript Follows Its Session Into A Worktree
`transcript` finds a session by its id wherever the runner's store now keeps it, so a session that moved into a worktree after spawn still reports.

## A Transcript Skips A Line Still Being Written
Reading a runner's jsonl store, `transcript` skips a final line with no
newline yet, which the executor is still writing, and fails on a complete
line that does not parse.

## A Runner Forwards The Chosen Model
Providers whose CLI takes a `--model` flag get it on spawn, forwarded as-is; a runner whose command line cannot take one fails loud instead of dropping the choice.

## Model Lists Come From The Provider CLI, Not A Registry
`models()` shells the provider's own model-listing command (`opencode models`, `codex debug models`) and returns whatever lines it prints. No model ids are hardcoded in the director; a provider with no list command returns none, never a guess.

## A Runner Can Be Defined By A File Of Commands
Every runner besides the three foundation adapters is a TOML file under `~/.work-director/runners/` listing the commands to spawn, send, check status and list models; any provider whose CLI fits that shape works with no code change, and `wd runner init` writes the starter file.

## Every Registered Runner Reports Whether It Is Detected
Each built-in and spec-file runner names the command it runs (a spec's is
the first word of its spawn line); it is detected, with the resolved path,
when that command is on PATH. Not detected is a fact, never an error.

## Only A Detected Runner Resolves For Spawning
Resolving a runner to spawn fails when its command is not on PATH. The
error names the runner and its command, says it was not found on PATH, and
points at `wd runner init` for a provider that is not built in.

## A Runner File That Cannot Work Is Rejected When Added
A runner file whose regexes do not compile, or whose commands use a
placeholder that command cannot fill, fails `wd runner add` naming the regex
or placeholder, and is never written.
