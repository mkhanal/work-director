---
id: standard-library-first
title: Standard library first, no framework for support code
category: working-method
scope: [lang:ts]
kind: practice
status: adopted
always: false
enforce: []
evidence: []
---
Native JS first; a well-known package for what native lacks; never a helper you could import. Model choice is config, never code.
**Why:** Support code is not the product.
**Apply:** `Map`, `Object.groupBy`, `structuredClone` before es-toolkit; date-fns for dates; Zod for validation; never lodash.
