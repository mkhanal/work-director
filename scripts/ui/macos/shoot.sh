#!/usr/bin/env bash
# Photographs every goal-tree screen in the macOS app into <out> (default:
# ./ui-shots/macos), to put beside docs/design/goal-tree/<screen>.png. The app
# runs unbundled from its build directory against a seeded ledger in a
# temporary WD_HOME, opened on each screen through its stored settings passed
# as launch arguments, so neither your ledger nor your app's settings are read
# or written.
#
#   scripts/ui/macos/shoot.sh [out]
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
out=$(mkdir -p "${1:-$root/ui-shots/macos}" && cd "${1:-$root/ui-shots/macos}" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/wd-ui-macos.XXXXXX")
(cd "$root" && go build -o "$work/wd" ./cmd/wd)
(cd "$root/clients/macos" && swift build -c release -q)
app=$root/clients/macos/.build/release/WorkDirector
export WD_HOME=$work/home WD_BIN=$work/wd
eval "$("$root/scripts/ui/seed-goal-tree.sh" "$WD_BIN" | tail -1)" # goal= subgoal= … finish=

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
    "$app" -place.work "$id" -goal.view "$view" -detail.agents NO -appearance "$scheme" >"$work/app-$screen.log" 2>&1 &
    pid=$!
    bounds=""
    for _ in $(seq 50); do
      bounds=$(osascript -e "tell application \"System Events\" to tell (first process whose unix id is $pid)
        if (count of windows) is 0 then return \"\"
        set frontmost to true
        set position of window 1 to {40, 60}
        set size of window 1 to {1280, 900}
        delay 2
        set {x, y} to position of window 1
        set {w, h} to size of window 1
        return (x as text) & \",\" & (y as text) & \",\" & (w as text) & \",\" & (h as text)
      end tell" 2>/dev/null) && [ -n "$bounds" ] && break
      sleep 0.2
    done
    [ -n "$bounds" ] || { echo "no window for $screen"; cat "$work/app-$screen.log"; kill "$pid"; exit 1; }
    name=$screen; [ "$scheme" = dark ] && name=$screen-dark
    screencapture -x -R "$bounds" "$out/$name.png"
    sips -z 900 1280 "$out/$name.png" >/dev/null
    kill "$pid"; wait "$pid" 2>/dev/null || true
  done
done
ls "$out"
