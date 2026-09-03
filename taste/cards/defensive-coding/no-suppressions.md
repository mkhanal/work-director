---
id: no-suppressions
title: No suppressions
category: defensive-coding
scope: [lang:ts]
kind: mechanical
status: adopted
always: false
enforce: [biome:suspicious/noTsIgnore, biome:suspicious/noEvolvingTypes, eslint:@typescript-eslint/ban-ts-comment]
evidence: []
---
No `@ts-ignore`, no lint-disable comments, no variables that evolve into `any`.
**Why:** A suppression is a root cause left in place.
**Apply:** Fix the type or the code; `@ts-expect-error` only with the reason and a test that would catch its removal.
