---
id: size-gaps-numerically
title: Size every gap against existing bounds
category: judgment
scope: [global]
kind: principle
status: adopted
always: false
enforce: []
evidence: []
---
A gap is not binary. Size it against caps, windows, retention and expiry; many are already bounded to negligible.
**Why:** "Could be missed" is not an answer; "300 events at one site inside a 1-day staleness window" is.
**Apply:** For each residual gap state the concrete conditions to hit it and the bound on its lifetime.
