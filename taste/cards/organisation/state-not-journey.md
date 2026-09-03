---
id: state-not-journey
title: Code expresses present state, not the journey
category: organisation
scope: [global]
kind: practice
status: adopted
always: true
enforce: []
evidence: []
---
No `v2`, `new`, `legacy`, `phase1`, PR numbers or 'after the refactor' in identifiers, files, tests, comments.
**Why:** Journey names carry dead cognitive load and outlive the cleanup they marked.
**Apply:** Journey lives in commits, PRs and decision logs. When replacing something, the old name disappears.
