---
name: taste-types-and-schemas
description: Use when writing TypeScript types, parsing input, or touching a schema.
---
# Taste: types-and-schemas

## Domain types, defined once in the owning module
Types are named for what they mean and defined once where they are owned. An anonymous `{ id: string; text: string }` passed around is a smell.
**Why:** Incidental shapes multiply and drift; a named type is a contract.
**Apply:** Brand a primitive only when confusing two of them is a real bug. An id is a `string`.

## Switches over unions are exhaustive
Every case of a union is handled; adding a member fails compilation until handled.
**Why:** Silent default branches hide new states.
**Apply:** Prefer a lookup object or exhaustive switch over if/else chains.

## No TypeScript enums or namespaces
Use `as const` objects and union types; use modules, not namespaces.
**Why:** Enums have runtime cost and nominal quirks; namespaces predate modules.
**Apply:** `const Status = { open: 'open', closed: 'closed' } as const; type Status = typeof Status[keyof typeof Status]`.

## Parse at the boundary, never cast inside
External input (HTTP, DB, model output, env) is parsed once into a domain type; after that no `as`, `any` or `!` without a why.
**Why:** Casts assert what parsing proves; every cast is a place the schema and the code can disagree silently.
**Apply:** Zod or the ORM's inferred types at the edge; `as const` is fine; a satisfies-check beats a cast.

## Relations live in the schema
Related rows are related in the database: foreign key, natural key, constraint. No side-channel arrays or maps holding facts the schema should.
**Why:** Agents are weak at schemas and reach for application-level joins that rot.
**Apply:** Generate types from the schema; a hand-written type that mirrors a table is a defect.
