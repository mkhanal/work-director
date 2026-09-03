---
id: root-cause-not-patch
title: Root-cause, do not patch
category: judgment
scope: [global]
kind: principle
status: adopted
always: true
enforce: []
evidence: []
---
No retry-on-flake, force-exit, lint-disable, cast or magic sleep. Trace the cause; fix it there.
**Why:** A workaround that hides a signal is worse than the failure.
**Apply:** When tempted to suppress, write down the mechanism that produces the symptom first.
