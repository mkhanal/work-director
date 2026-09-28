# Where our requirements live

- **packages/taste** — `packages/taste/specs/*.lazyspec.md`
  What the build produces from rule cards: which cards reach which artifact, and what
  a card must contain. Not the wording of any rule.

- **packages/wd** — `packages/wd/specs/*.lazyspec.md`
  What the director CLI guarantees at its boundary: ledger states and transitions,
  brief contents, epic tasks and coordination (claims, impacts, conflicts, concerns,
  worktrees), promotion candidates, what each runner adapter records. Not how a runner
  works internally.

- **Go module (repo root)** — `*.lazyspec.md` at the root, married tests in
  `internal/<pkg>/<stem>_lazyspec_test.go` (`t.Run` per requirement).
  The Go port of the director CLI: same schema, DDL and semantics as the TypeScript
  code it ports. So far: `ledger.lazyspec.md` (internal/ledger), `runner.lazyspec.md`
  (internal/runner), `taste.lazyspec.md` (internal/taste),
  `project.lazyspec.md` (internal/project), `coordinator.lazyspec.md`
  (internal/coordinator).
