---
name: taste-organisation
description: Use when naming, placing or structuring code, modules, tests or data access.
---
# Taste: organisation

## Name things in the domain, organise by capability
Identifiers, modules and tests speak the domain, not the technology. Group by capability, not by layer.
**Why:** A reader learns the product from the code; layer folders and tech nouns hide what the software does.
**Apply:** `Invoice`, `Verdict`, `reconcile` over `DataDto`, `utils`, `manager`. One folder per capability holding its model, rules and tests.

## Separate the record from the action
When one entity appears in several views, define each view's question independently and let the entity appear in each.
**Why:** One-view-per-entity creates hidden records and vanishing items.
**Apply:** "What happened?" and "what can I do?" are two queries over the same rows.

## Data access behind one boundary
Drivers, ORM clients and storage SDKs are constructed in one module; everything else imports from it.
**Why:** One boundary carries tenancy, environment assertions and key composition; scattered access leaks all three.
**Apply:** Enforce with a restricted-imports rule naming the driver packages and pointing at the boundary module.

## Code expresses present state, not the journey
No `v2`, `new`, `legacy`, `phase1`, PR numbers or 'after the refactor' in identifiers, files, tests, comments.
**Why:** Journey names carry dead cognitive load and outlive the cleanup they marked.
**Apply:** Journey lives in commits, PRs and decision logs. When replacing something, the old name disappears.
