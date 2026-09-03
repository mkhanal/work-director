---
id: relations-in-schema
title: Relations live in the schema
category: types-and-schemas
scope: [global]
kind: practice
status: adopted
always: true
enforce: []
evidence: []
---
Related rows are related in the database: foreign key, natural key, constraint. No side-channel arrays or maps holding facts the schema should.
**Why:** Agents are weak at schemas and reach for application-level joins that rot.
**Apply:** Generate types from the schema; a hand-written type that mirrors a table is a defect.
