---
id: domain-over-tech-speak
title: Name things in the domain, organise by capability
category: organisation
scope: [global]
kind: practice
status: adopted
always: true
enforce: []
evidence: []
---
Identifiers, modules and tests speak the domain, not the technology. Group by capability, not by layer.
**Why:** A reader learns the product from the code; layer folders and tech nouns hide what the software does.
**Apply:** `Invoice`, `Verdict`, `reconcile` over `DataDto`, `utils`, `manager`. One folder per capability holding its model, rules and tests.
