---
id: no-defensive-mechanism
title: No defensive mechanism
category: defensive-coding
scope: [global]
kind: practice
status: adopted
always: true
enforce: []
evidence: []
---
No 100%-delegating wrapper, flag parameter, just-in-case null check, parallel implementation, or capability without a use case.
**Why:** Each is a hedge against a fact nobody verified; together they double the code.
**Apply:** Fix the interface or the upstream type instead. If the core needs changing, read all callers and change it.
