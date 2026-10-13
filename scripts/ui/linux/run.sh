#!/usr/bin/env bash
# Photographs every goal-tree screen in the Linux app on Ubuntu 24.04, in
# Docker, into <out> (default: ./ui-shots/linux), to put beside
# docs/design/goal-tree/<screen>.png.
#
#   scripts/ui/linux/run.sh [out]
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mkdir -p "${1:-$root/ui-shots/linux}" && cd "${1:-$root/ui-shots/linux}" && pwd)
docker build -q -t wd-ui-linux "$root/scripts/ui/linux" >/dev/null
docker run --rm -v "$root":/repo:ro -v "$out":/shots -v wd-ui-cargo:/root/.cargo/registry \
  -v wd-ui-build:/build wd-ui-linux bash /repo/scripts/ui/linux/shoot.sh
