---
name: taste-defensive-coding
description: Use when tempted to add a check, wrapper, flag, catch, suppression or branch.
---
# Taste: defensive-coding

## Fail loud, fail soon
Errors propagate to the boundary. Empty catch, `.catch(() => [])`, log-and-forget, null-on-failure are violations.
**Why:** A swallowed error is a bug with its evidence destroyed.
**Apply:** Catch only where you can act; otherwise let it throw.

## Early return and dispatch over if/else chains
Guard clauses return early; variants dispatch through a lookup or exhaustive switch; no nested ternaries; parameters are not reassigned.
**Why:** if/else chains grow a branch per bug and hide the missing case.
**Apply:** When a second `else if` appears, reach for a map keyed by the discriminant.

## No defensive mechanism
No 100%-delegating wrapper, flag parameter, just-in-case null check, parallel implementation, or capability without a use case.
**Why:** Each is a hedge against a fact nobody verified; together they double the code.
**Apply:** Fix the interface or the upstream type instead. If the core needs changing, read all callers and change it.

## No suppressions
No `@ts-ignore`, no lint-disable comments, no variables that evolve into `any`.
**Why:** A suppression is a root cause left in place.
**Apply:** Fix the type or the code; `@ts-expect-error` only with the reason and a test that would catch its removal.
