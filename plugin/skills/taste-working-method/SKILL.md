---
name: taste-working-method
description: Use when planning a task, delegating, testing, or about to claim something is done.
---
# Taste: working-method

## Fresh context per task, progress in files
Split work into tasks a fresh session can finish; keep progress in files and git, not in context. Use the smallest model that does the job.
**Why:** Long contexts degrade; re-established facts and ignored decisions are the symptom.
**Apply:** Loop: brief, execute, verify, record. Delegate mechanical work to cheap subagents with a precise brief.

## Look it up yourself; ask only for judgments
Facts you can obtain (pricing, docs, versions, live behaviour) you obtain. Ask humans only for value judgments and facts nobody can look up.
**Why:** "Please confirm X" hands the agent's work back to the person who delegated it.
**Apply:** Reply ≤10 lines; a question carries the answer you would give and what it changes.

## Write the requirement the moment it is known
A requirement is written as soon as it settles, married to a test, then locked. Not all up front, not all at the end.
**Why:** Specs accrete; an agent must never rewrite a requirement to match what it built.
**Apply:** Where the repo uses lazyspec, `/lazyspec` is the only way to change one.

## Standard library first, no framework for support code
Native JS first; a well-known package for what native lacks; never a helper you could import. Model choice is config, never code.
**Why:** Support code is not the product.
**Apply:** `Map`, `Object.groupBy`, `structuredClone` before es-toolkit; date-fns for dates; Zod for validation; never lodash.

## Watch the test fail, run the check before claiming done
Write the test first and watch it fail. Before claiming done or passing, run the check and quote its output.
**Why:** A test you never saw fail proves nothing; a claim without output is a guess.
**Apply:** Report format: command, exit code, the lines that matter.
