#!/usr/bin/env bash
# Package the host binary as a macOS disk image — the path a Mac user expects:
# open the .dmg, drag wd to Applications, done. No Terminal, no PATH editing,
# no curl-pipe-shell.
#
#   VERSION=v1.2.0 scripts/dmg.sh
#
# Produces dist/wd-<version>-<arch>.dmg. hdiutil only exists on macOS, so this
# runs on a macos runner in CI; on other platforms it says so and exits
# non-zero rather than leaving a half-made image behind.
#
# The image is laid out the way a double-clicked dmg should behave: the binary
# and an Applications alias on the desktop.
#
# There is deliberately no hand-placed window layout. hdiutil's -layout flag is
# rejected outright on current macOS ("Invalid argument"), so a plist that looks
# like the right thing is a config that breaks the build on release day.
# Finder's default window already shows both icons, which is the whole of what
# the layout was for.
set -euo pipefail
cd "$(dirname "$0")/.."

[ "$(uname -s)" = "Darwin" ] || {
  echo "dmg: only macOS can build a disk image (hdiutil); run this on a macos runner" >&2
  exit 1
}
command -v hdiutil >/dev/null || { echo "dmg: hdiutil not found" >&2; exit 1; }

version=${VERSION:-dev}
arch=$(go env GOARCH)
os=$(go env GOOS)
commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}

mkdir -p dist
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

# The binary is the same self-contained one every other channel ships: pure Go,
# pure-Go sqlite, nothing to install alongside it.
CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
  -ldflags "-s -w -X wd/internal/cli.Version=$version -X wd/internal/cli.Commit=$commit" \
  -o "$stage/wd" ./cmd/wd

# A staging volume with the app folder as a symlink, which is what makes the
# window show an Applications drop target rather than a list of loose files.
vol="$stage/Volume"
mkdir -p "$vol"
cp "$stage/wd" "$vol/wd"
ln -s /Applications "$vol/Applications"

# The volume gets the app icon, and so does the binary: macOS takes a file's icon
# from an .icns of the same name sitting beside it, which is why wd.icns is
# copied next to wd rather than only used as the volume's.
icon="assets/icon/wd.icns"
if [ -f "$icon" ]; then
  cp "$icon" "$vol/wd.icns"
  cp "$icon" "$stage/wd.icns"
  # -a C is "use this file as the custom icon". Without it the volume shows the
  # generic disk and the whole point of a dmg is the first thing someone sees.
  SetFile -a C "$vol" 2>/dev/null || true
else
  echo "dmg: assets/icon/wd.icns is missing — run scripts/icons.sh (needs macOS) for the icon; shipping without it" >&2
fi
cat > "$vol/README.txt" <<TXT
wd $version ($commit)

Drag wd to Applications, then open a Terminal and run:

    wd doctor     # which runner CLIs are detected
    wd serve      # the web UI and JSON API — open the address it prints
    wd --help     # every command

Your ledger lives in ~/.work-director. Nothing else is installed and nothing is
uploaded: wd runs entirely on this machine.
TXT

dmg="dist/wd-$version-$arch.dmg"
rm -f "$dmg"
# UDZO is the compressed read-only format Finder mounts like a disk image.
hdiutil create -volname "wd $version" -srcfolder "$vol" -ov -format UDZO "$dmg" >/dev/null

if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$dmg" > "$dmg.sha256"
else sha256sum "$dmg" > "$dmg.sha256"; fi

echo "built $dmg"
