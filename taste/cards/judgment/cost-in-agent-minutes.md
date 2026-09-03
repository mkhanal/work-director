---
id: cost-in-agent-minutes
title: Cost is agent minutes, never human hours
category: judgment
scope: [global]
kind: principle
status: adopted
always: true
enforce: []
evidence: []
---
Estimate in agent minutes and tokens. Never accept tech debt justified by a human-hour estimate; if the proper fix is agent-minutes, do it.
**Why:** Agents inherit human estimates from training and ship debt that would take them 15 minutes to avoid.
**Apply:** Reject "8 hours" reasoning in briefs and reports; ask "how many agent minutes, verified how?"
