#!/usr/bin/env bash
# Settled lazyspec checks: every `## ` requirement in a root *.lazyspec.md is married to a test in
# internal/<pkg>/<stem>_lazyspec_test.go — t.Run("<heading>") or func Test<Heading> (words joined,
# a leading article optional); no orphan test files.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0

# joined prints a heading as a Go test name: words capitalised and joined, punctuation dropped.
joined() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9' ' ' | awk '{for (i = 1; i <= NF; i++) printf "%s", toupper(substr($i, 1, 1)) substr($i, 2)}'
}

for spec in *.lazyspec.md; do
  stem=$(basename "$spec" .lazyspec.md)
  tests=(internal/*/"${stem}"_lazyspec_test.go)
  [ -f "${tests[0]}" ] || { echo "orphan spec: $spec (no internal/<pkg>/${stem}_lazyspec_test.go)"; fail=1; continue; }
  [ ${#tests[@]} -eq 1 ] || { echo "ambiguous spec: $spec married to ${tests[*]}"; fail=1; continue; }
  test=${tests[0]}
  funcs=$(sed -n 's/^func Test\([A-Za-z0-9_]*\)(.*/\1/p' "$test" | tr 'A-Z' 'a-z')
  while IFS= read -r h; do
    grep -qF "t.Run(\"$h\"" "$test" && continue
    name=$(joined "$h" | tr 'A-Z' 'a-z')
    bare=$(joined "$(printf '%s' "$h" | sed -E 's/^(A|An|The) //')" | tr 'A-Z' 'a-z')
    printf '%s\n' "$funcs" | grep -qxF -e "$name" -e "$bare" && continue
    echo "unmarried: $spec :: $h"
    fail=1
  done < <(grep '^## ' "$spec" | grep -v 'no-test:' | sed 's/^## //')
done

for t in internal/*/*_lazyspec_test.go; do
  stem=$(basename "$t" _lazyspec_test.go)
  [ -f "$stem.lazyspec.md" ] || { echo "orphan test: $t (no $stem.lazyspec.md)"; fail=1; }
done

# The macOS app: clients/macos/<stem>.lazyspec.md is married to
# clients/macos/Tests/WorkDirectorKitTests/<Stem>LazyspecTests.swift by @Test("<heading>").
swift_tests=clients/macos/Tests/WorkDirectorKitTests
for spec in clients/macos/*.lazyspec.md; do
  [ -f "$spec" ] || continue
  stem=$(basename "$spec" .lazyspec.md)
  test="$swift_tests/$(printf '%s' "$stem" | awk '{print toupper(substr($0, 1, 1)) substr($0, 2)}')LazyspecTests.swift"
  [ -f "$test" ] || { echo "orphan spec: $spec (no $test)"; fail=1; continue; }
  while IFS= read -r h; do
    grep -qF "@Test(\"$h\"" "$test" && continue
    echo "unmarried: $spec :: $h"
    fail=1
  done < <(grep '^## ' "$spec" | grep -v 'no-test:' | sed 's/^## //')
done
for t in "$swift_tests"/*LazyspecTests.swift; do
  [ -f "$t" ] || continue
  stem=$(basename "$t" LazyspecTests.swift | awk '{print tolower(substr($0, 1, 1)) substr($0, 2)}')
  [ -f "clients/macos/$stem.lazyspec.md" ] || { echo "orphan test: $t (no clients/macos/$stem.lazyspec.md)"; fail=1; }
done

# The Linux app: clients/linux/<stem>.lazyspec.md is married to
# clients/linux/tests/<stem>_lazyspec.rs by a test fn named for the heading: lower
# case, apostrophes dropped, every other run of punctuation and spaces one `_`.
snake() {
  printf '%s' "$1" | tr 'A-Z' 'a-z' | tr -d "'" | tr -c 'a-z0-9' '_' | tr -s '_' | sed 's/^_//; s/_$//'
}
for spec in clients/linux/*.lazyspec.md; do
  [ -f "$spec" ] || continue
  stem=$(basename "$spec" .lazyspec.md)
  test="clients/linux/tests/${stem}_lazyspec.rs"
  [ -f "$test" ] || { echo "orphan spec: $spec (no $test)"; fail=1; continue; }
  while IFS= read -r h; do
    grep -qx "fn $(snake "$h")() {" "$test" && continue
    echo "unmarried: $spec :: $h"
    fail=1
  done < <(grep '^## ' "$spec" | grep -v 'no-test:' | sed 's/^## //')
done
for t in clients/linux/tests/*_lazyspec.rs; do
  [ -f "$t" ] || continue
  stem=$(basename "$t" _lazyspec.rs)
  [ -f "clients/linux/$stem.lazyspec.md" ] || { echo "orphan test: $t (no clients/linux/$stem.lazyspec.md)"; fail=1; }
done

[ $fail -eq 0 ] && echo "lazyspec: all requirements married"
exit $fail
