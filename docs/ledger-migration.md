# Ledger migration: TypeScript wd → Go wd

The Go rewrite keeps one thing exactly as the TypeScript wd left it: the ledger.
Both binaries open `$WD_HOME/ledger.db` (default `~/.work-director/ledger.db`).
No export, no import, no separate migration command — the first Go `wd` command
migrates the file in place, additively, and carries on.

## What the Go wd may find

| Generation | `work` columns | `concern` / `worktree` tables |
|---|---|---|
| Pre-epic TypeScript wd | 12 (no `parent`, `heading`, `claim`, `impact`) | absent |
| Goal-mode TypeScript wd | 16 | present |
| Already opened by the Go wd | 16 | present |

## What first open does (all additive, in place)

- `CREATE TABLE IF NOT EXISTS` for each missing table.
- `ALTER TABLE work ADD COLUMN` for each missing column — nullable `TEXT`, no
  default. In SQLite this is a metadata-only change, so migration is O(1) in
  ledger size: a ledger with a hundred thousand rows migrates as fast as an
  empty one.
- Nothing is dropped, renamed, rewritten or reordered. Every existing row keeps
  every value byte for byte (covered by the married test
  `Old Ledgers Stay Readable`).

## Cutover steps

1. Install the Go binary (the Install task). Installation does not touch the
   ledger.
2. Run any `wd` command, e.g. `wd status --json`. First open runs the
   migration, then behaves exactly as the TS wd did.
3. Diff the output against the TS wd's: same work items, same states, same
   events.

## Rollback

Rollback is a data question, not a schema question. The TS wd reads `SELECT *`
and inserts with explicit columns, so the four extra nullable columns and the
two extra tables are invisible to it: a Go-migrated ledger opens and runs in
the TS wd unchanged. The one-way door is data, not schema — rows written with
epic features (a task under an epic, a claim, an impact, a concern, a
worktree) are ignored by a pre-epic TS wd but never lost. Keep the TS wd
installed until the Go wd has run a full epic; rolling back is
`bun run packages/wd/src/cli.ts <command>` against the same file.

## Operational notes

- Both binaries run the ledger in WAL mode. Never copy `ledger.db` without its
  `-wal` and `-shm` siblings while a wd is running.
- Migration is idempotent: opening an already-migrated ledger changes nothing
  (covered by the married test `Migration Is Additive Only`).
- Future schema changes follow the same rule: additive only, never a rewrite;
  each migration ships as a married requirement in `ledger.lazyspec.md` with
  its test, applied in `migrate()` at open.
