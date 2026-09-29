> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Runners (Go)

The runners own each provider's commands, session discovery, transcript
parsing and error messages. The three foundation adapters are code; every other
provider is a TOML file of commands under `~/.work-director/runners/`.

## Spawning Records Runner Session And Attach Hint
`spawn` returns a handle with the runner's session id and a hint a human can run to join the session.

## Sending Continues The Same Session
`send` addresses the runner with the handle's session id, never starting a new session.

## A Transcript Yields The Executor Messages
`transcript` returns the assistant texts of the session, oldest first, from the runner's own store.

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
