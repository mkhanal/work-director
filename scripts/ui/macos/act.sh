#!/usr/bin/env bash
# Drives the macOS app through its accessibility tree on a seeded ledger and
# checks in the ledger that each control did what it says: the same checks as
# scripts/ui/linux/act.sh. Prints one PASS or FAIL line per check; exits
# non-zero on a FAIL. Needs an unlocked screen and Accessibility access for the
# terminal running it.
#
#   scripts/ui/macos/act.sh [out]
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
if ioreg -n Root -d1 -a | grep -A1 CGSSessionScreenIsLocked | grep -q '<true/>'; then
  echo "the screen is locked: macOS draws no app windows until it is unlocked" >&2
  exit 1
fi
out=$(mkdir -p "${1:-$root/ui-shots/macos-act}" && cd "${1:-$root/ui-shots/macos-act}" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/wd-ui-act.XXXXXX")
(cd "$root" && go build -o "$work/wd" ./cmd/wd)
(cd "$root/clients/macos" && swift build -c release -q)
swiftc -O -o "$work/press" "$root/scripts/ui/macos/press.swift"
app=$root/clients/macos/.build/release/WorkDirector
export WD_HOME=$work/home WD_BIN=$work/wd
eval "$("$root/scripts/ui/seed-goal-tree.sh" "$WD_BIN" | tail -1)"
db=$WD_HOME/ledger.db
fail=0

check() { # name, sql, want
  got=$(sqlite3 -cmd ".timeout 10000" "$db" "$2")
  if [ "$got" = "$3" ]; then echo "PASS $1"; else echo "FAIL $1: got '$got', want '$3'"; fail=1; fi
}
open_app() { # work view
  "$app" -place.work "$1" -goal.view "$2" -detail.agents NO -appearance light >"$work/app.log" 2>&1 &
  pid=$!
  for _ in $(seq 150); do
    osascript -e "tell application \"System Events\" to tell (first process whose unix id is $pid) to count windows" 2>/dev/null | grep -qv '^0$' && break
    sleep 0.2
  done
  # A window just counted can still refuse to be placed for a moment.
  for _ in $(seq 10); do
    osascript -e "tell application \"System Events\" to tell (first process whose unix id is $pid)
      set frontmost to true
      set position of window 1 to {40, 60}
      set size of window 1 to {1280, 900}
    end tell" >/dev/null 2>&1 && break
    sleep 1
  done
  sleep 2.5
}
close_app() { kill "$pid"; wait "$pid" 2>/dev/null || true; sleep 1; }
# press clicks the first element of a role whose title or description contains
# the text, through the accessibility API: System Events reads the names
# SwiftUI gives its controls as missing value.
press() { # role text
  "$work/press" "$pid" "$1" "$2" | grep -q clicked || { echo "no $1 containing '$2'"; return 1; }
  sleep 3
}
shot() { screencapture -x "$out/$1.png"; }

open_app "$pivot" overview
press AXButton "Change course" && shot act-pivot
close_app
check "change course renames the goal" "SELECT title FROM work WHERE id = '$pivot'" "Sync never drops events"
check "change course sets its finish line" "SELECT finish_line FROM work WHERE id = '$pivot'" "complete PR"

open_app "$proposal" overview
press AXButton "Approve and start" && shot act-proposal
close_app
check "approve records the approval" "SELECT count(*) FROM event WHERE work = '$proposal' AND kind = 'decision' AND body LIKE 'approved:%'" "1"

open_app "$finish" conversation
press AXRadioButton "Research complete" || press AXButton "Research complete" || true
press AXCheckBox "this way from now on" || true
press AXButton "Answer" && shot act-finish
close_app
check "the finish choice is stored" "SELECT finish_line FROM work WHERE id = '$finish'" "research"
if grep -q '^finish: research' "$WD_HOME/projects/metariki-eqms.md"; then echo "PASS the finish choice became the product's setting"; else echo "FAIL the product's finish setting: $(grep '^finish' "$WD_HOME/projects/metariki-eqms.md" || echo none)"; fail=1; fi

exit $fail
