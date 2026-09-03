---
id: parse-at-boundary
title: Parse at the boundary, never cast inside
category: types-and-schemas
scope: [lang:ts]
kind: practice
status: adopted
always: true
enforce: [biome:suspicious/noExplicitAny, biome:style/noNonNullAssertion, biome:nursery/noUnsafeTypeAssertion, eslint:@typescript-eslint/no-explicit-any, eslint:@typescript-eslint/no-non-null-assertion, eslint:@typescript-eslint/consistent-type-assertions]
evidence: []
---
External input (HTTP, DB, model output, env) is parsed once into a domain type; after that no `as`, `any` or `!` without a why.
**Why:** Casts assert what parsing proves; every cast is a place the schema and the code can disagree silently.
**Apply:** Zod or the ORM's inferred types at the edge; `as const` is fine; a satisfies-check beats a cast.
