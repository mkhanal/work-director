---
id: fail-loud
title: Fail loud, fail soon
category: defensive-coding
scope: [global]
kind: practice
status: adopted
always: true
enforce: []
evidence: []
---
Errors propagate to the boundary. Empty catch, `.catch(() => [])`, log-and-forget, null-on-failure are violations.
**Why:** A swallowed error is a bug with its evidence destroyed.
**Apply:** Catch only where you can act; otherwise let it throw.
