#!/usr/bin/env bash
# Update a checkout of the Homebrew tap (mkhanal/homebrew-work-director) to a
# published release: its wd.rb and work-director.rb assets, which
# scripts/formula.sh generated from that release's checksums, become the tap's
# Formula/wd.rb and Casks/work-director.rb, committed.
#
#   scripts/tap.sh v1.2.0 ../homebrew-work-director && git -C ../homebrew-work-director push
#
# Needs gh. It never pushes: the caller does, once it has looked.
set -euo pipefail

tag=${1:?usage: scripts/tap.sh <tag> <tap checkout>}
tap=${2:?usage: scripts/tap.sh <tag> <tap checkout>}
repo=${REPO:-mkhanal/work-director}

git -C "$tap" rev-parse --git-dir >/dev/null
mkdir -p "$tap/Formula" "$tap/Casks"
gh release download "$tag" -R "$repo" -p wd.rb -O "$tap/Formula/wd.rb" --clobber
gh release download "$tag" -R "$repo" -p work-director.rb -O "$tap/Casks/work-director.rb" --clobber
git -C "$tap" add Formula/wd.rb Casks/work-director.rb
git -C "$tap" commit -q -m "wd and work-director $tag"
echo "committed $tag to $tap; push it to publish"
