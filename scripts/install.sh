#!/usr/bin/env bash
# Single-command install for the director on macOS and Linux:
#
#   curl -fsSL https://raw.githubusercontent.com/mkhanal/work-director/main/scripts/install.sh | sh
#
# Downloads the static wd binary for this machine from the latest GitHub
# release, verifies its sha256 against the release's checksums file, and puts
# it on PATH. The binary has no runtime dependencies; `wd doctor` afterwards
# reports which runner CLIs are detected.
#
# Pass a version to install something other than the latest:
#
#   ... | sh -s -- v1.2.0
#
# Windows uses scripts/install.ps1 instead; this script says so rather than
# failing with a confusing tar error.
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
  windows*) echo "install: on Windows use install.ps1:" >&2
    echo "  irm https://raw.githubusercontent.com/$repo/main/scripts/install.ps1 | iex" >&2
    exit 1 ;;
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

# checksums.txt lists every asset in the release, so one download verifies the
# whole set and a swapped tarball is caught before anything is unpacked.
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || {
  echo "install: $version published no checksums.txt — refusing to install unverified" >&2; exit 1; }
curl -fsSL "$base/$asset.tar.gz" -o "$tmp/$asset.tar.gz"

want=$(awk -v f="$asset.tar.gz" '$2 == f || $2 == "*"f {print $1}' "$tmp/checksums.txt" | head -n 1)
[ -n "$want" ] || { echo "install: $asset.tar.gz is not listed in the release checksums" >&2; exit 1; }

sha256() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}';
  else sha256sum "$1" | awk '{print $1}'; fi
}
got=$(sha256 "$tmp/$asset.tar.gz")
if [ "$got" != "$want" ]; then
  echo "install: $asset.tar.gz does not match its published sha256" >&2
  echo "  expected $want" >&2
  echo "  got      $got" >&2
  exit 1
fi
echo "verified $asset.tar.gz"

tar -xzf "$tmp/$asset.tar.gz" -C "$tmp"
mkdir -p "$dir"
install -m 0755 "$tmp/wd" "$dir/wd"
echo "installed $dir/wd"

case :$PATH: in
  *:$dir:*) ;;
  *) echo "note: $dir is not on PATH — add it with: export PATH=\"$dir:\$PATH\"" ;;
esac
echo "next: wd doctor"
