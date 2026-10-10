#!/usr/bin/env bash
# Build "Work Director.app": the native macOS app with the wd it runs inside it.
#
#   scripts/build-macos.sh                 # dist/Work Director.app, host arch
#   VERSION=v1.2.0 scripts/build-macos.sh
#
# The app talks to the wd in its own bundle over stdio and to no other, so the
# two are built from the same checkout here and can never disagree about the
# wire between them.
set -euo pipefail
cd "$(dirname "$0")/.."

[ "$(uname -s)" = "Darwin" ] || { echo "build-macos: the app builds only on macOS" >&2; exit 1; }

version=${VERSION:-dev}
commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}
app="dist/Work Director.app"

swift build --package-path clients/macos -c release --product WorkDirector
bin=$(swift build --package-path clients/macos -c release --show-bin-path)

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin/WorkDirector" "$app/Contents/MacOS/WorkDirector"
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X wd/internal/cli.Version=$version -X wd/internal/cli.Commit=$commit" \
  -o "$app/Contents/MacOS/wd" ./cmd/wd
cp assets/icon/wd.icns "$app/Contents/Resources/AppIcon.icns"

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Work Director</string>
  <key>CFBundleDisplayName</key><string>Work Director</string>
  <key>CFBundleIdentifier</key><string>dev.mkhanal.workdirector</string>
  <key>CFBundleExecutable</key><string>WorkDirector</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${version#v}</string>
  <key>CFBundleVersion</key><string>$commit</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

# Ad-hoc signed: enough for this machine. Distribution needs a Developer ID.
codesign --force --sign - "$app/Contents/MacOS/wd"
codesign --force --sign - "$app"

echo "built $app ($version, $commit)"
