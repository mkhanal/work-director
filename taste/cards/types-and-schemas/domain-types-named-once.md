---
id: domain-types-named-once
title: Domain types, defined once in the owning module
category: types-and-schemas
scope: [lang:ts]
kind: practice
status: adopted
always: false
enforce: []
evidence: []
---
Types are named for what they mean and defined once where they are owned. An anonymous `{ id: string; text: string }` passed around is a smell.
**Why:** Incidental shapes multiply and drift; a named type is a contract.
**Apply:** Brand a primitive only when confusing two of them is a real bug. An id is a `string`.
