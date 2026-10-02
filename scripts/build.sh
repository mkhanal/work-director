#!/usr/bin/env bash
# Build wd as a single static binary. The Go port links modernc.org/sqlite
# (pure Go), so CGO_ENABLED=0 produces a binary with no shared-library
# dependencies — the file itself is the whole install.
#
#   scripts/build.sh                                # host platform
#   TARGETS="darwin/arm64 linux/amd64" scripts/build.sh   # release assets
#
# Each target yields dist/wd-<os>-<arch>.{tar.gz,zip} (the binary at the
# root) and its .sha256, plus dist/checksums.txt covering every asset so a
# person can verify the whole set in one command. Windows gets .zip because
# tar.gz on Windows is a second unzip step for no gain.
#
# VERSION and COMMIT are stamped into the binary, so a downloaded wd can say
# what it is:
#
#   VERSION=v1.2.0 COMMIT=$(git rev-parse --short HEAD) scripts/build.sh
set -euo pipefail
cd "$(dirname "$0")/.."

host=$(go env GOOS)/$(go env GOARCH)
targets=${TARGETS:-darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64}

version=${VERSION:-dev}
commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}
ldflags="-s -w -X wd/internal/cli.Version=$version -X wd/internal/cli.Commit=$commit"

mkdir -p dist

# sha256 hashes a file: shasum on macOS, sha256sum elsewhere.
sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$@"
  else
    sha256sum -a 256 "$@"
  fi
}

# zip only exists on Windows runners; where it is missing a release is built
# without the .zip and says so rather than shipping a broken archive name.
have_zip() { command -v zip >/dev/null 2>&1; }

packaged=""
for target in $targets; do
  os=${target%/*}
  arch=${target#*/}
  stage=$(mktemp -d)
  ext=tar.gz
  if [ "$os" = windows ]; then
    ext=zip
    name=wd.exe
  else
    name=wd
  fi
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$stage/$name" ./cmd/wd

  asset="dist/wd-$os-$arch.$ext"
  if [ "$ext" = zip ]; then
    if have_zip; then
      (cd "$stage" && zip -q -X "../$(basename "$asset")" "$name")
    else
      rm -f "$asset"
      echo "skipping $asset: no zip on PATH"
      rm -rf "$stage"
      continue
    fi
  else
    tar -czf "$asset" -C "$stage" "$name"
  fi
  rm -rf "$stage"
  (cd dist && sha256 "$(basename "$asset")" > "$(basename "$asset").sha256")
  packaged="$packaged $asset"
  echo "built $asset ($version)"
done

# One file covering every asset, so verifying a download is a single command
# against a single list rather than a per-asset lookup.
(cd dist && sha256 $(for a in $packaged; do basename "$a"; done) > checksums.txt 2>/dev/null || true)
[ -s dist/checksums.txt ] && echo "built dist/checksums.txt"

# The host binary, unpacked, for anyone who just wants the file.
hostasset=""
for target in $targets; do
  if [ "$target" = "$host" ]; then hostasset=$(echo "$packaged" | tr ' ' '\n' | grep "$(echo "$target" | tr '/' '-')" || true); fi
done
if [ -n "$hostasset" ] && [ -f "$hostasset" ]; then
  case "$hostasset" in
    *.zip) have_zip && (cd dist && unzip -qo "$(basename "$hostasset")") ;;
    *) tar -xzf "$hostasset" -C dist ;;
  esac
  [ -f dist/wd.exe ] && mv -f dist/wd.exe dist/wd.exe.host 2>/dev/null || true
  [ -f dist/wd ] && echo "built dist/wd"
fi
