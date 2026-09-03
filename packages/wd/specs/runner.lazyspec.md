# Runners

## Spawning Records Runner Session And Attach Hint
`spawn` returns a handle with the runner's session id and a hint a human can run to join the session.

## Sending Continues The Same Session
`send` addresses the runner with the handle's session id, never starting a new session.

## A Transcript Yields The Executor Messages
`transcript` returns the assistant texts of the session, oldest first, from the runner's own store.

## The Ao Adapter Spawns With The Brief As Prompt
The ao runner passes the brief via `--prompt`, a display name of at most 20 characters, and sends follow-ups with `--session`/`--message`.
