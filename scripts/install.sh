#!/usr/bin/env bash
# Single-command install for the director:
#
#   curl -fsSL https://raw.githubusercontent.com/mkhanal/work-director/main/scripts/install.sh | sh
#
# Downloads the static wd binary for this platform from the latest GitHub
# release, verifies its sha256, and puts it on PATH. The binary has no
# runtime dependencies; what it shells out to (git, the runner CLIs) is
# probed by `wd setup` afterwards.
set -euo pipefail

repo=mkhanal/work-director
version=${1:-latest}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case $arch in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "install: unsupported architecture $arch" >&2; exit 1 ;;
esac
case $os in
  darwin|linux) ;;
  *) echo "install: unsupported platform $os" >&2; exit 1 ;;
esac

if [ "$version" = latest ]; then
  version=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$version" ] || { echo "install: cannot resolve the latest $repo release" >&2; exit 1; }
fi

asset="wd-$os-$arch"
base="https://github.com/$repo/releases/download/$version"
dir=${WD_INSTALL_DIR:-$HOME/.local/bin}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "installing wd $version ($os/$arch)"
curl -fsSL "$base/$asset.tar.gz" -o "$tmp/$asset.tar.gz"
curl -fsSL "$base/$asset.tar.gz.sha256" -o "$tmp/$asset.tar.gz.sha256"

# check verifies the download against its .sha256 ("<hash>  <filename>").
check() {
  if command -v shasum >/dev/null 2>&1; then
    (cd "$(dirname "$1")" && shasum -a 256 -c "$(basename "$2")")
  else
    (cd "$(dirname "$1")" && sha256sum -c "$(basename "$2")")
  fi
}
check "$tmp/$asset.tar.gz" "$tmp/$asset.tar.gz.sha256"

mkdir -p "$dir"
tar -xzf "$tmp/$asset.tar.gz" -C "$tmp"
install -m 0755 "$tmp/wd" "$dir/wd"
echo "installed $dir/wd"

case :$PATH: in
  *:$dir:*) ;;
  *) echo "note: $dir is not on PATH — add it with: export PATH=\"$dir:\$PATH\"" ;;
esac
echo "next: wd setup"
