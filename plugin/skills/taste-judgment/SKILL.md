---
name: taste-judgment
description: Use before any analysis or design decision: how to size a need, a gap, a cost, a fix.
---
# Taste: judgment

## Business facts gate the design, they are not edge cases
If a decision hinges on whether a scenario occurs, ask the owner first and let the answer gate the design.
**Why:** Conservative machinery built around unverified hypotheticals gets stripped away later at cost.
**Apply:** List the factual assumptions; confirm each against spec, live system or domain owner before building on it.

## Cost is agent minutes, never human hours
Estimate in agent minutes and tokens. Never accept tech debt justified by a human-hour estimate; if the proper fix is agent-minutes, do it.
**Why:** Agents inherit human estimates from training and ship debt that would take them 15 minutes to avoid.
**Apply:** Reject "8 hours" reasoning in briefs and reports; ask "how many agent minutes, verified how?"

## Exhaust data in hand before adding mechanism
Before a new call, cache, flag or process, enumerate what the system already fetches, stores or knows.
**Why:** Most gaps are a missing comparison or wiring, not missing data.
**Apply:** List every present data source and what it covers; only the uncovered remainder may justify new mechanism.

## Re-derive the need before extending a mechanism
State the requirement a mechanism serves in plain terms before defending or extending it. The incumbent explains what is, not what must be.
**Why:** Code comments and past decisions are descriptions, not constraints.
**Apply:** Write the requirement, then ask whether existing data or bounds already satisfy it.

## Prefer removing mechanism to perfecting it
When a mechanism serves a negligible, self-bounded gap, delete it rather than build a cheaper variant.
**Why:** Simplicity is a result of subtraction.
**Apply:** Propose deletion as the first option whenever analysis shows a gap is already bounded.

## Root-cause, do not patch
No retry-on-flake, force-exit, lint-disable, cast or magic sleep. Trace the cause; fix it there.
**Why:** A workaround that hides a signal is worse than the failure.
**Apply:** When tempted to suppress, write down the mechanism that produces the symptom first.

## Size every gap against existing bounds
A gap is not binary. Size it against caps, windows, retention and expiry; many are already bounded to negligible.
**Why:** "Could be missed" is not an answer; "300 events at one site inside a 1-day staleness window" is.
**Apply:** For each residual gap state the concrete conditions to hit it and the bound on its lifetime.

## Never build on a guess
Validate assumptions against the authoritative source: the spec, the live system, the real story. A paraphrase is not the source.
**Why:** Recollection drifts; live behaviour and spec can differ.
**Apply:** Where spec and live behaviour may differ, verify against the live system. Label anything unverified.
