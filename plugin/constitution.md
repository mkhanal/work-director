# Taste
Full cards: /taste-* skills.
- **Comments state constraints, not narration.** A comment states an invariant the code cannot express. Never narrate the change, the history or the reviewer.
- **Cost is agent minutes, never human hours.** Estimate in agent minutes and tokens. Never accept tech debt justified by a human-hour estimate; if the proper fix is agent-minutes, do it.
- **Name things in the domain, organise by capability.** Identifiers, modules and tests speak the domain, not the technology. Group by capability, not by layer.
- **Fail loud, fail soon.** Errors propagate to the boundary. Empty catch, `.catch(() => [])`, log-and-forget, null-on-failure are violations.
- **No defensive mechanism.** No 100%-delegating wrapper, flag parameter, just-in-case null check, parallel implementation, or capability without a use case.
- **Outcomes first, sized to the reader.** Lead with the conclusion. Tables for facts, prose for reasoning. Never narrate your process.
- **Parse at the boundary, never cast inside.** External input (HTTP, DB, model output, env) is parsed once into a domain type; after that no `as`, `any` or `!` without a why.
- **Relations live in the schema.** Related rows are related in the database: foreign key, natural key, constraint. No side-channel arrays or maps holding facts the schema should.
- **Root-cause, do not patch.** No retry-on-flake, force-exit, lint-disable, cast or magic sleep. Trace the cause; fix it there.
- **Look it up yourself; ask only for judgments.** Facts you can obtain (pricing, docs, versions, live behaviour) you obtain. Ask humans only for value judgments and facts nobody can look up.
- **Code expresses present state, not the journey.** No `v2`, `new`, `legacy`, `phase1`, PR numbers or 'after the refactor' in identifiers, files, tests, comments.
- **Watch the test fail, run the check before claiming done.** Write the test first and watch it fail. Before claiming done or passing, run the check and quote its output.
