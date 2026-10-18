#!/usr/bin/env bash
# Finds words in a goal's conversation and record through the Find menu, in
# light and dark, and photographs the find bar with its matches marked into
# <out> (default: ./ui-shots/macos-find). Menu items are pressed and the
# field filled through the accessibility API; no key event is posted, and the
# app never comes to the front: see launch.sh.
#
#   scripts/ui/macos/find.sh [out]
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
if ioreg -n Root -d1 -a | grep -A1 CGSSessionScreenIsLocked | grep -q '<true/>'; then
  echo "the screen is locked: macOS draws no app windows until it is unlocked" >&2
  exit 1
fi
out=$(mkdir -p "${1:-$root/ui-shots/macos-find}" && cd "${1:-$root/ui-shots/macos-find}" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/wd-ui-find.XXXXXX")
trap 'rm -rf "$work"' EXIT
(cd "$root" && go build -o "$work/wd" ./cmd/wd)
(cd "$root/clients/macos" && swift build -c release -q)
app=$root/clients/macos/.build/release/WorkDirector
export WD_HOME=$work/home WD_BIN=$work/wd
. "$root/scripts/ui/macos/launch.sh"
ui_tools
for t in menu value text; do swiftc -O -o "$work/$t" "$root/scripts/ui/macos/$t.swift"; done
eval "$("$root/scripts/ui/seed-goal-tree.sh" "$WD_BIN" | tail -1)"
fail=0
ui_watch

shot() { screencapture -x -o -l "$("$work/window" "$pid")" "$out/$1.png"; }
menu() { "$work/menu" "$pid" Edit Find "$1" | grep -q pressed || { echo "FAIL no Find menu item $1"; fail=1; }; sleep 1; }
said() { # what the find bar's count reads
  "$work/text" "$pid" role AXStaticText | grep -Eo '[0-9]+ of [0-9]+|No results' | head -1
}

for scheme in light dark; do
  for view in conversation record; do
    ui_open "$work/app.log" -place.work "$goal" -goal.view "$view" -detail.agents NO -appearance "$scheme"
    sleep 2.5
    menu "Find…"
    "$work/value" "$pid" AXTextField Find "${FIND:-sub}" | grep -q set || { echo "FAIL the find field takes no words"; fail=1; }
    sleep 1.5
    count=$(said)
    if [ -n "$count" ] && [ "$count" != "No results" ]; then echo "PASS $view, $scheme: finding '${FIND:-sub}' reads $count"; else echo "FAIL $view, $scheme: the count reads '$count'"; fail=1; fi
    shot "find-$view-$scheme"
    menu "Find Next"
    next=$(said)
    if [ "$next" != "$count" ] || [ "${count#1 of 1}" = "" ]; then echo "PASS $view, $scheme: Find Next reads $next"; else echo "FAIL $view, $scheme: Find Next left the count at $next"; fail=1; fi
    shot "find-$view-$scheme-next"
    "$work/press" "$pid" AXButton Done | grep -q clicked || { echo "FAIL no Done"; fail=1; }
    sleep 1
    shot "find-$view-$scheme-done"
    picked=""
    for n in $(seq 0 $(($("$work/text" "$pid" count | head -1) - 1))); do
      picked=$("$work/text" "$pid" selected "$n")
      [ -n "$picked" ] && break
    done
    if [ "$(printf %s "$picked" | tr '[:upper:]' '[:lower:]')" = "${FIND:-sub}" ]; then echo "PASS $view, $scheme: Done leaves the match selected"; else echo "FAIL $view, $scheme: Done left '$picked' selected"; fail=1; fi
    ui_close
    sleep 1
  done
done
ui_unwatch || fail=1
exit $fail
