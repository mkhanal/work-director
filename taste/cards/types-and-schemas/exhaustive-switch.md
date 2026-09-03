---
id: exhaustive-switch
title: Switches over unions are exhaustive
category: types-and-schemas
scope: [lang:ts]
kind: mechanical
status: adopted
always: false
enforce: [biome:nursery/useExhaustiveSwitchCases, eslint:@typescript-eslint/switch-exhaustiveness-check]
evidence: []
---
Every case of a union is handled; adding a member fails compilation until handled.
**Why:** Silent default branches hide new states.
**Apply:** Prefer a lookup object or exhaustive switch over if/else chains.
