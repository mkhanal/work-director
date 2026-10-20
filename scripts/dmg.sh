#!/usr/bin/env bash
# Package "Work Director.app" as a macOS disk image: open the .dmg, drag the app
# onto Applications, done.
#
#   scripts/build-macos.sh && VERSION=v1.2.0 scripts/dmg.sh
#
# Produces dist/Work-Director-<version>-macos-<arch>.dmg and its .sha256 from the
# app scripts/build-macos.sh left in dist, so the image holds exactly the app the
# zip holds, wd inside it included. hdiutil only exists on macOS; elsewhere this
# says so and exits non-zero rather than leaving a half-made image behind.
#
# There is no hand-placed window layout: hdiutil's -layout flag is rejected on
# current macOS, and Finder's default window already shows the app beside the
# Applications drop target.
set -euo pipefail
cd "$(dirname "$0")/.."

[ "$(uname -s)" = "Darwin" ] || {
  echo "dmg: only macOS can build a disk image (hdiutil); run this on a macos runner" >&2
  exit 1
}

version=${VERSION:-dev}
app="dist/Work Director.app"
[ -d "$app" ] || { echo "dmg: $app is missing; run scripts/build-macos.sh first" >&2; exit 1; }
arch=$(uname -m)
name="Work-Director-$version-macos-$arch.dmg"

stage=$(mktemp -d)
mnt=$(mktemp -d)
trap 'hdiutil detach -quiet "$mnt" 2>/dev/null || true; rm -rf "$stage" "$mnt"' EXIT

# The volume: the app and a symlink to /Applications, the drop target.
vol="$stage/Volume"
mkdir -p "$vol"
ditto "$app" "$vol/Work Director.app"
ln -s /Applications "$vol/Applications"
cp assets/icon/wd.icns "$vol/.VolumeIcon.icns"

# Built writable first, because the volume shows its icon only once the mounted
# volume's root carries the custom-icon flag; then compressed read-only (UDZO).
rw="$stage/rw.dmg"
hdiutil create -quiet -volname "Work Director" -srcfolder "$vol" -fs HFS+ -format UDRW -ov "$rw"
hdiutil attach -quiet -nobrowse -noautoopen -mountpoint "$mnt" "$rw"
SetFile -a C "$mnt"
hdiutil detach -quiet "$mnt"

mkdir -p dist
rm -f "dist/$name"
hdiutil convert -quiet "$rw" -format UDZO -o "dist/$name"
# Named without its folder, so `shasum -c` checks it beside the download.
(cd dist && shasum -a 256 "$name" > "$name.sha256")

echo "built dist/$name"
