#!/usr/bin/env bash
# Build wd as a single static binary. The Go port links modernc.org/sqlite
# (pure Go), so CGO_ENABLED=0 produces a binary with no shared-library
# dependencies — the file itself is the whole install.
#
#   scripts/build.sh                                # host platform
#   TARGETS="darwin/arm64 linux/amd64" scripts/build.sh   # release assets
#
# Each target yields dist/wd-<os>-<arch>.tar.gz (the binary at the root) and
# its .sha256. A host build also leaves the unpacked binary at dist/wd.
set -euo pipefail
cd "$(dirname "$0")/.."

host=$(go env GOOS)/$(go env GOARCH)
targets=${TARGETS:-$host}

mkdir -p dist
rm -f dist/wd dist/wd-*

# sha256 hashes a file: shasum on macOS, sha256sum elsewhere.
sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$@"
  else
    sha256sum "$@"
  fi
}

for target in $targets; do
  os=${target%/*}
  arch=${target#*/}
  stage=$(mktemp -d)
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "-s -w" -o "$stage/wd" ./cmd/wd
  tar -czf "dist/wd-$os-$arch.tar.gz" -C "$stage" wd
  rm -rf "$stage"
  (cd dist && sha256 "wd-$os-$arch.tar.gz" > "wd-$os-$arch.tar.gz.sha256")
  echo "built dist/wd-$os-$arch.tar.gz"
done

# The host binary, unpacked, for anyone who just wants the file. $os/$arch
# are the host's here: the condition means the loop ran for the host only.
if [ "$targets" = "$host" ]; then
  tar -xzf "dist/wd-$os-$arch.tar.gz" -C dist
  echo "built dist/wd"
fi
