#!/usr/bin/env bash
# Settled lazyspec checks: every `## ` requirement has a describe() in its married test; no orphan tests.
set -euo pipefail
fail=0
for spec in packages/*/specs/*.lazyspec.md; do
  stem=$(basename "$spec" .lazyspec.md); pkg=${spec#packages/}; pkg=${pkg%%/*}
  test="packages/$pkg/test/$stem.lazyspec.test.ts"
  [ -f "$test" ] || { echo "orphan spec: $spec (no $test)"; fail=1; continue; }
  while IFS= read -r h; do
    grep -qF "describe('$h'" "$test" || { echo "unmarried: $spec :: $h"; fail=1; }
  done < <(grep '^## ' "$spec" | sed 's/^## //; s/ *<!--.*//')
done
for t in packages/*/test/*.lazyspec.test.ts; do
  stem=$(basename "$t" .lazyspec.test.ts); pkg=${t#packages/}; pkg=${pkg%%/*}
  [ -f "packages/$pkg/specs/$stem.lazyspec.md" ] || { echo "orphan test: $t"; fail=1; }
done
[ $fail -eq 0 ] && echo "lazyspec: all requirements married"
exit $fail
