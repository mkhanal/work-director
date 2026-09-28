# Runners (Go)

The Go runners keep the behaviour of the TypeScript runners they port
(`packages/wd/src/runner/`): same commands, session discovery, transcript
parsing and error messages. The four foundation adapters are code; every other
provider is a TOML file of commands under `~/.work-director/runners/`.

## Spawning Records Runner Session And Attach Hint
`spawn` returns a handle with the runner's session id and a hint a human can run to join the session.

## Sending Continues The Same Session
`send` addresses the runner with the handle's session id, never starting a new session.

## A Transcript Yields The Executor Messages
`transcript` returns the assistant texts of the session, oldest first, from the runner's own store.

## The Ao Adapter Spawns With The Brief As Prompt
The ao runner passes the brief via `--prompt`, a display name of at most 20 characters, and sends follow-ups with `--session`/`--message`.

## A Runner Forwards The Chosen Model
Providers whose CLI takes a `--model` flag get it on spawn, forwarded as-is; a runner whose command line cannot take one fails loud instead of dropping the choice.

## Model Lists Come From The Provider CLI, Not A Registry
`models()` shells the provider's own model-listing command (`opencode models`, `codex debug models`) and returns whatever lines it prints. No model ids are hardcoded in the director; a provider with no list command returns none, never a guess.

## A Runner Can Be Defined By A File Of Commands
Every runner besides the four foundation adapters is a TOML file under `~/.work-director/runners/` listing the commands to spawn, send, check status and list models; any provider whose CLI fits that shape works with no code change, and `wd runner init` writes the starter file.
