#!/usr/bin/env bash
# Photographs every goal-tree screen in the macOS app into <out> (default:
# ./ui-shots/macos), to put beside docs/design/goal-tree/<screen>.png. The app
# runs unbundled from its build directory against a seeded ledger in a
# temporary WD_HOME, opened on each screen through its stored settings passed
# as launch arguments, so neither your ledger nor your app's settings are read
# or written. It never comes to the front: see launch.sh.
#
#   scripts/ui/macos/shoot.sh [out]
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
if ioreg -n Root -d1 -a | grep -A1 CGSSessionScreenIsLocked | grep -q '<true/>'; then
  echo "the screen is locked: macOS draws no app windows until it is unlocked" >&2
  exit 1
fi
out=$(mkdir -p "${1:-$root/ui-shots/macos}" && cd "${1:-$root/ui-shots/macos}" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/wd-ui-macos.XXXXXX")
(cd "$root" && go build -o "$work/wd" ./cmd/wd)
(cd "$root/clients/macos" && swift build -c release -q)
app=$root/clients/macos/.build/release/WorkDirector
export WD_HOME=$work/home WD_BIN=$work/wd
. "$root/scripts/ui/macos/launch.sh"
ui_tools
eval "$("$root/scripts/ui/seed-goal-tree.sh" "$WD_BIN" | tail -1)" # goal= subgoal= … finish=

ui_watch
# screen work goal.view
screens=(
  "main $goal overview"
  "subgoal $subgoal overview"
  "proposal $proposal overview"
  "pivot $pivot overview"
  "finishline $finish conversation"
  "conversation $goal conversation"
  "record $goal record"
)
for scheme in light dark; do
  for row in "${screens[@]}"; do
    read -r screen id view <<<"$row"
    ui_open "$work/app-$screen.log" -place.work "$id" -goal.view "$view" -detail.agents NO -appearance "$scheme" ||
      { cat "$work/app-$screen.log"; ui_close; exit 1; }
    sleep 2
    name=$screen; [ "$scheme" = dark ] && name=$screen-dark
    screencapture -x -o -l "$("$work/window" "$pid")" "$out/$name.png"
    sips -z 900 1280 "$out/$name.png" >/dev/null
    ui_close
  done
done
ui_unwatch
ls "$out"
