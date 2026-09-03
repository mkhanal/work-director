---
id: single-data-layer
title: Data access behind one boundary
category: organisation
scope: [global]
kind: practice
status: adopted
always: false
enforce: []
evidence: []
---
Drivers, ORM clients and storage SDKs are constructed in one module; everything else imports from it.
**Why:** One boundary carries tenancy, environment assertions and key composition; scattered access leaks all three.
**Apply:** Enforce with a restricted-imports rule naming the driver packages and pointing at the boundary module.
