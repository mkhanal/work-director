#!/usr/bin/env bash
# Inside the container: build wd and the app from the read-only checkout at
# /repo, seed a ledger with the goal-tree screens, open each screen in the app
# on a virtual display and photograph its window into /shots/<screen>[-dark].png
# at the size of docs/design/goal-tree/<screen>.png.
set -euo pipefail
export CARGO_TARGET_DIR=/build/target GOFLAGS=-buildvcs=false
mkdir -p /build /shots
(cd /repo && go build -o /build/wd ./cmd/wd)
(cd /repo/clients/linux && cargo build --release --workspace -q)
app=$CARGO_TARGET_DIR/release/work-director
install -m 755 /build/wd "$(dirname "$app")/wd"

export WD_HOME=/data/home XDG_CONFIG_HOME=/data/config HOME=/data
mkdir -p "$XDG_CONFIG_HOME/work-director"
ids=$(/repo/scripts/ui/seed-goal-tree.sh /build/wd | tail -1)
eval "$ids" # goal= subgoal= working= question= pivot= proposal= finish=

Xvfb :99 -screen 0 1600x1100x24 >/tmp/xvfb.log 2>&1 &
export DISPLAY=:99
sleep 1
eval "$(dbus-launch --sh-syntax)"

# screen work tab
screens=(
  "main $goal overview"
  "subgoal $subgoal overview"
  "proposal $proposal overview"
  "pivot $pivot overview"
  "finishline $finish conversation"
  "conversation $goal conversation"
  "record $goal record"
  "session $working conversation"
)
# ONLY=<screen|screen…> photographs just those.
[ -z "${ONLY:-}" ] || mapfile -t screens < <(printf '%s\n' "${screens[@]}" | grep -E "^($ONLY) ")
for scheme in light dark; do
  for row in "${screens[@]}"; do
    read -r screen work tab <<<"$row"
    printf 'last-work=%s\ngoal-tab=%s\nagents=hidden\n' "$work" "$tab" >"$XDG_CONFIG_HOME/work-director/settings"
    if [ "$scheme" = dark ]; then export ADW_DEBUG_COLOR_SCHEME=prefer-dark; else unset ADW_DEBUG_COLOR_SCHEME; fi
    "$app" >/tmp/app-$screen.log 2>&1 &
    pid=$!
    win=""
    for _ in $(seq 50); do
      win=$(xdotool search --onlyvisible --name "Work Director" 2>/dev/null | head -1) && [ -n "$win" ] && break
      sleep 0.2
    done
    [ -n "$win" ] || { echo "no window for $screen"; cat /tmp/app-$screen.log; exit 1; }
    xdotool windowsize "$win" 1280 900 windowmove "$win" 0 0
    sleep 2.5
    name=$screen; [ "$scheme" = dark ] && name=$screen-dark
    import -window "$win" "/shots/$name.png"
    if [ "$tab" = overview ]; then
      xdotool mousemove --window "$win" 780 600 click --repeat 25 --delay 40 5
      sleep 1
      import -window "$win" "/shots/$name-bottom.png"
    fi
    kill "$pid"; wait "$pid" 2>/dev/null || true
    if grep -q panicked /tmp/app-$screen.log; then echo "the app panicked on $screen"; cat /tmp/app-$screen.log; exit 1; fi
  done
done
ls /shots
