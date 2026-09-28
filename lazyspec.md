# Where our requirements live

- **packages/taste** — `packages/taste/specs/*.lazyspec.md`
  What the build produces from rule cards: which cards reach which artifact, and what
  a card must contain. Not the wording of any rule.

- **packages/wd** — `packages/wd/specs/*.lazyspec.md`
  What the director CLI guarantees at its boundary: ledger states and transitions,
  brief contents, epic tasks and coordination (claims, impacts, conflicts, concerns,
  worktrees), promotion candidates, what each runner adapter records. Not how a runner
  works internally.

- **Go module (repo root)** — `ledger.lazyspec.md` and `taste.lazyspec.md` at the
  root, married tests in `internal/ledger/ledger_lazyspec_test.go` and
  `internal/taste/build_lazyspec_test.go` (`t.Run` per requirement).
  The Go port of the director CLI: same schema, DDL and semantics as the TypeScript
  ledger it ports. The Go taste build (`go run ./cmd/taste`) is the rewrite of
  `bun run build`: same cards in, same plugin and dist artifacts out.
