---
id: no-enums-namespaces
title: No TypeScript enums or namespaces
category: types-and-schemas
scope: [lang:ts]
kind: mechanical
status: adopted
always: false
enforce: [biome:style/noEnum, biome:style/noNamespace, eslint:no-restricted-syntax]
evidence: []
---
Use `as const` objects and union types; use modules, not namespaces.
**Why:** Enums have runtime cost and nominal quirks; namespaces predate modules.
**Apply:** `const Status = { open: 'open', closed: 'closed' } as const; type Status = typeof Status[keyof typeof Status]`.
