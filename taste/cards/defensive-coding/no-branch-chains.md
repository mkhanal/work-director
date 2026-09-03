---
id: no-branch-chains
title: Early return and dispatch over if/else chains
category: defensive-coding
scope: [lang:ts]
kind: mechanical
status: adopted
always: false
enforce: [biome:style/noUselessElse, biome:style/noNestedTernary, biome:style/noParameterAssign, eslint:no-else-return, eslint:no-nested-ternary, eslint:no-param-reassign]
evidence: []
---
Guard clauses return early; variants dispatch through a lookup or exhaustive switch; no nested ternaries; parameters are not reassigned.
**Why:** if/else chains grow a branch per bug and hide the missing case.
**Apply:** When a second `else if` appears, reach for a map keyed by the discriminant.
