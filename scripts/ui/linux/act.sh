#!/usr/bin/env bash
# Inside the container: drives the Linux app through its accessibility tree on
# the seeded ledger and checks that each control did what it says in the
# ledger itself. Prints one PASS or FAIL line per check; exits non-zero on a FAIL.
set -euo pipefail
export CARGO_TARGET_DIR=/build/target GOFLAGS=-buildvcs=false
mkdir -p /build /shots
(cd /repo && go build -o /build/wd ./cmd/wd)
(cd /repo/clients/linux && cargo build --release --workspace -q)
app=$CARGO_TARGET_DIR/release/work-director
install -m 755 /build/wd "$(dirname "$app")/wd"
export WD_HOME=/data/home XDG_CONFIG_HOME=/data/config HOME=/data PATH=/build:$PATH
mkdir -p "$XDG_CONFIG_HOME/work-director"
eval "$(/repo/scripts/ui/seed-goal-tree.sh /build/wd | tail -1)"

Xvfb :99 -screen 0 1600x1100x24 >/tmp/xvfb.log 2>&1 &
export DISPLAY=:99 GTK_A11Y=atspi NO_AT_BRIDGE=0
sleep 1
eval "$(dbus-launch --sh-syntax)"
/usr/libexec/at-spi-bus-launcher --launch-immediately >/tmp/atspi.log 2>&1 &
sleep 1

db=$WD_HOME/ledger.db
fail=0
check() { # name, sql, want
  got=$(sqlite3 -cmd ".timeout 10000" "$db" "$2")
  if [ "$got" = "$3" ]; then echo "PASS $1"; else echo "FAIL $1: got '$got', want '$3'"; fail=1; fi
}
open() { # work tab
  printf 'last-work=%s\ngoal-tab=%s\nagents=hidden\n' "$1" "$2" >"$XDG_CONFIG_HOME/work-director/settings"
  "$app" >/tmp/app.log 2>&1 &
  pid=$!
  for _ in $(seq 50); do xdotool search --onlyvisible --name "Work Director" >/dev/null 2>&1 && break; sleep 0.2; done
  win=$(xdotool search --onlyvisible --name "Work Director" | head -1)
  xdotool windowsize "$win" 1280 900 windowmove "$win" 0 0
  sleep 2.5
}
close() { kill "$pid"; wait "$pid" 2>/dev/null || true; }
act() { python3 -c "import sys; sys.path.insert(0, '/repo/scripts/ui/linux'); from a11y import *; $1"; sleep 3; }

# The course change: Change course applies the proposal to the top goal.
open "$pivot" overview
act "press('push button', 'Change course')"
import -window "$win" /shots/act-pivot.png
close
check "change course renames the goal" "SELECT title FROM work WHERE id = '$pivot'" "Sync never drops events"
check "change course sets its finish line" "SELECT finish_line FROM work WHERE id = '$pivot'" "complete PR"

# The proposal: merging two cards supersedes the second.
open "$proposal" overview
python3 -c "import sys; sys.path.insert(0, '/repo/scripts/ui/linux'); from a11y import *; dump()" > /shots/proposal-tree.txt 2>&1 || true
act "press('toggle button', 'Merge 3 ·')"
python3 -c "import sys; sys.path.insert(0, '/repo/scripts/ui/linux'); from a11y import *; dump()" > /shots/merge-tree.txt 2>&1 || true
xdotool key Down Return
sleep 3
import -window "$win" /shots/act-merge.png
close
check "merging supersedes one proposed sub-goal" "SELECT count(*) FROM work WHERE parent = '$proposal' AND state = 'superseded'" "1"


# The proposal: Approve and start approves the breakdown.
open "$proposal" overview
act "press('push button', 'Approve and start')"
import -window "$win" /shots/act-proposal.png
close
check "approve records the approval" "SELECT count(*) FROM event WHERE work = '$proposal' AND kind = 'decision' AND body LIKE 'approved:%'" "1"

# The finish-line question: a choice, remembered.
open "$finish" conversation
python3 -c "import sys; sys.path.insert(0, '/repo/scripts/ui/linux'); from a11y import *; dump()" > /shots/finish-tree.txt 2>&1 || true
act "press('radio button', 'Research complete'); press('check box', 'this way from now on'); press('push button', 'Answer', exact=True)"
import -window "$win" /shots/act-finish.png
close
check "the finish choice is stored" "SELECT finish_line FROM work WHERE id = '$finish'" "research"
if grep -q '^finish: research' "$WD_HOME/projects/metariki-eqms.md"; then echo "PASS the finish choice became the product's setting"; else echo "FAIL the product's finish setting: $(grep '^finish' "$WD_HOME/projects/metariki-eqms.md")"; fail=1; fi

# The Overview's finish line: chosen from its card's menu.
open "$goal" overview
python3 -c "import sys; sys.path.insert(0, '/repo/scripts/ui/linux'); from a11y import *; dump()" > /shots/main-tree.txt 2>&1 || true
act "press('toggle button', 'A complete pull request')"
python3 -c "import sys; sys.path.insert(0, '/repo/scripts/ui/linux'); from a11y import *; dump()" > /shots/menu-tree.txt 2>&1 || true
act "press('push button', 'Merged', exact=True)"
import -window "$win" /shots/act-finish-menu.png
close
check "the finish line changes from the overview" "SELECT finish_line FROM work WHERE id = '$goal'" "merged"

# The conversation copies as one. GTK's Text interface reads a text view's
# selection but cannot set one, so the selection is dragged: from the second
# Bash step's line to the end of the reply, where the seeded session lies in
# a 1280 by 900 window scrolled to its newest. The Text interface reads it
# back; Ctrl+C copies the step line, the table as tab-separated rows and the
# words after; Ctrl+A then Ctrl+C copies the whole run, brief to reply.
has() { # name, file, text
  if grep -qF -- "$3" "$2"; then echo "PASS $1"; else echo "FAIL $1: $(head -c 400 "$2")"; fail=1; fi
}
open "$working" conversation
xdotool mousemove 376 370 mousedown 1 mousemove 500 500 mousemove 700 684 mouseup 1
sleep 1
act "a=find('text', 'Conversation'); r=Atspi.Text.get_selection(a, 0); open('/shots/selection.txt', 'w').write(Atspi.Text.get_text(a, r.start_offset, r.end_offset))"
xdotool key ctrl+c; sleep 1
xclip -o -selection clipboard >/shots/copied.txt 2>/dev/null || true
import -window "$win" /shots/act-selection.png
has "a selection dragged across a step and the reply reads back from the step" /shots/selection.txt "Bash go test ./notes/..."
has "the copy starts at the step's line" /shots/copied.txt "Bash go test ./notes/..."
has "the copy keeps the table as tab-separated rows" /shots/copied.txt "$(printf 'Check\tRun?\tResult')"
has "the copy keeps the words after the table" /shots/copied.txt "Next I generate the note for v1.0.0 as a sample."
if grep -qF "▸" /shots/copied.txt; then echo "FAIL a chevron was copied"; fail=1; else echo "PASS chevrons are not copied"; fi
xdotool key ctrl+a ctrl+c; sleep 1
xclip -o -selection clipboard >/shots/copied-all.txt 2>/dev/null || true
has "Ctrl+A copies from the brief" /shots/copied-all.txt "Write the customer release note template, in plain language."
has "Ctrl+A copies to the reply's end" /shots/copied-all.txt "Next I generate the note for v1.0.0 as a sample."
close

cp "$db" /shots/ledger.db; cp /tmp/app.log /shots/app.log
exit $fail
