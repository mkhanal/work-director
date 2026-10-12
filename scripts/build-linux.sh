#!/usr/bin/env bash
# Build Work Director for Linux: the GTK4 + libadwaita app with the wd it runs
# installed beside it, as a tarball and a .deb.
#
#   scripts/build-linux.sh                 # dist/Work-Director-dev-linux-x86_64.tar.gz and .deb
#   VERSION=v1.2.0 scripts/build-linux.sh
#
# The app talks to the wd beside it over stdio and to no other, so the two are
# built from the same checkout here and can never disagree about the wire.
# Needs cargo, go, libgtk-4-dev and libadwaita-1-dev; the .deb needs dpkg-deb.
set -euo pipefail
cd "$(dirname "$0")/.."

[ "$(uname -s)" = "Linux" ] || { echo "build-linux: the app builds only on Linux" >&2; exit 1; }

version=${VERSION:-dev}
commit=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}
arch=$(uname -m)
app_id=dev.mkhanal.WorkDirector
name="Work-Director-${version}-linux-${arch}"

cargo build --release --manifest-path clients/linux/Cargo.toml -p work-director
bin=clients/linux/target/release/work-director

# The installed tree, shared by the tarball and the .deb: the app and its wd
# side by side, the desktop entry and the icons.
root=dist/linux-root
rm -rf "$root"
lib="$root/usr/lib/work-director"
mkdir -p "$lib" "$root/usr/bin" "$root/usr/share/applications"
install -m 755 "$bin" "$lib/work-director"
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X wd/internal/cli.Version=$version -X wd/internal/cli.Commit=$commit" \
  -o "$lib/wd" ./cmd/wd
ln -s ../lib/work-director/work-director "$root/usr/bin/work-director"
install -m 644 "clients/linux/data/$app_id.desktop" "$root/usr/share/applications/$app_id.desktop"
for size in 32 256 512; do
  dir="$root/usr/share/icons/hicolor/${size}x${size}/apps"
  mkdir -p "$dir"
  install -m 644 "assets/icon/icon-$size.png" "$dir/$app_id.png"
done
mkdir -p "$root/usr/share/icons/hicolor/scalable/apps"
install -m 644 assets/icon/icon.svg "$root/usr/share/icons/hicolor/scalable/apps/$app_id.svg"
"$lib/wd" --version >/dev/null

# The tarball: the tree under one directory, unpacked anywhere.
rm -rf "dist/$name"
mkdir -p "dist/$name"
cp -a "$root/usr/." "dist/$name/"
tar -C dist -czf "dist/$name.tar.gz" "$name"
(cd dist && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")

# The .deb, for Ubuntu 24.04 and later: GTK 4.14 and libadwaita 1.5.
deb_version=${version#v}
case "$deb_version" in [0-9]*) ;; *) deb_version="0.0.0~$deb_version" ;; esac
deb_arch=$(dpkg --print-architecture)
mkdir -p "$root/DEBIAN"
cat > "$root/DEBIAN/control" <<CONTROL
Package: work-director
Version: $deb_version
Architecture: $deb_arch
Maintainer: Mridul Khanal <mkhanal@users.noreply.github.com>
Depends: libgtk-4-1 (>= 4.14), libadwaita-1-0 (>= 1.5)
Section: devel
Priority: optional
Homepage: https://github.com/mkhanal/work-director
Description: Work Director: goals, their agents and what waits on you
 A native GTK app over the wd ledger. It runs the wd installed with it,
 over wd serve --stdio; every write is a wd command.
CONTROL
deb="$name.deb"
dpkg-deb --build --root-owner-group "$root" "dist/$deb"
(cd dist && sha256sum "$deb" > "$deb.sha256")

echo "built dist/$name.tar.gz and dist/$deb ($version, $commit)"
