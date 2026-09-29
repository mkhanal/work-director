# Where our requirements live

- **Go module (repo root)** — `*.lazyspec.md` at the root, married tests in
  `internal/<pkg>/<stem>_lazyspec_test.go` (`t.Run` or `func Test` per requirement).
  What the director guarantees at its boundary: ledger states and transitions, the
  `--json` rail, briefs, epic coordination, runner adapters, the taste build, serve and
  the TUI. Not how a runner or adapter works internally. `scripts/lazyspec-check.sh`
  checks every marriage.
