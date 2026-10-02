#!/usr/bin/env bash
# Render assets/icon/icon.svg into every size the platforms actually ask for.
#
#   scripts/icons.sh
#
# Produces:
#   assets/icon/wd.icns            macOS, every size Finder, Dock and the DMG
#                                  volume ask for, plus a dark-appearance
#                                  variant so the mark does not glare at night
#   assets/icon/icon-<n>.png       the bare squares, for the web UI and docs
#   dist/wd.png / dist/wd@2x.png   the same for a README and a release body
#
# Rendering needs rsvg-convert; building the .icns needs Apple's iconutil. Both
# are absent on a stock Linux CI box, so the script says which stage it could not
# run and exits non-zero rather than shipping half the set.
set -euo pipefail
cd "$(dirname "$0")/.."
cd assets/icon

command -v rsvg-convert >/dev/null || { echo "icons: rsvg-convert is needed (brew install librsvg)" >&2; exit 1; }
command -v iconutil   >/dev/null || { echo "icons: iconutil is macOS only; the png set is still written" >&2; }

mkdir -p build

# iconset names are Apple's, not numbers: the same file has to be named
# icon_16x16.png and icon_16x16@2x.png or iconutil rejects the set.
render() { rsvg-convert -w "$2" -h "$2" "$1" -o "build/$3"; }

# The dark variant is not a recolour. A mark that reads on a light desktop
# becomes a glare on a dark one, so the plate is deepened and the sheen dropped
# while the mark keeps its contrast.
render_dark() {
  sed -e 's/#5B21B6/#3B1178/' \
      -e 's/#341068/#1E0A3C/' \
      -e 's/#120A26/#0A0516/' \
      -e 's/stop-opacity="0.20"/stop-opacity="0.10"/' \
      -e 's/stop-opacity="0.04"/stop-opacity="0.02"/' \
      icon.svg > build/icon-dark.svg
  rsvg-convert -w "$1" -h "$1" build/icon-dark.svg -o "build/$2"
}

rm -rf build/wd.iconset
mkdir -p build/wd.iconset

for spec in "16 16x16" "32 16x16@2x" "32 32x32" "64 32x32@2x" "128 128x128" "256 128x128@2x" \
            "256 256x256" "512 256x256@2x" "512 512x512" "1024 512x512@2x"; do
  set -- $spec
  render icon.svg "$1" "wd.iconset/icon_$2.png"
  render_dark "$1" "wd.iconset/icon_$2-dark.png"
done

if command -v iconutil >/dev/null; then
  # -c icns compiles the set; the dark members are named with the -dark suffix
  # that Apple reserves for exactly this appearance variant.
  iconutil -c icns build/wd.iconset -o wd.icns
  echo "built assets/icon/wd.icns"
fi

# The square set the web UI and the docs use. 180 is the Apple touch icon and 512
# is what a social preview wants.
for n in 32 180 192 256 384 512; do
  rsvg-convert -w "$n" -h "$n" icon.svg -o "icon-$n.png"
done
echo "built the png set"

mkdir -p ../../dist
cp icon-512.png ../../dist/wd.png
cp icon-256.png ../../dist/wd@2x.png

rm -f build/icon-dark.svg
echo "done"
