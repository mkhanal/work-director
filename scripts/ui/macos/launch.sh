# Launching the macOS app for a UI check, sourced by act.sh and shoot.sh.
# The check runs on a Mac a person is using, so the app it launches must
# never become the active app: an active window takes their keystrokes (a
# check once came up frontmost with all its text selected).
#
# - The app gets `-ui.background YES`: it sets its activation policy to
#   prohibited before any window exists, so neither macOS nor SwiftUI can make
#   it active or its window key, and it never calls activate. Its window still
#   draws, answers the accessibility API (press.swift, text.swift) and is
#   photographed alone with `screencapture -l <window>` (window.swift), even
#   behind other windows.
# - `-NSAppSleepDisabled YES` keeps App Nap from slowing a window nobody sees.
# - `open -g` is no help: the build is an unbundled binary, which open hands
#   to Terminal. The binary is run directly; the launch argument is what
#   keeps it in the background.
# - frontmost.swift watches the frontmost app from before the launch until
#   the check ends; should the app under test ever come to the front it is
#   ended at once and the check fails.
# - Windows are placed through System Events (the accessibility API), which
#   neither raises nor activates them. Never post mouse or key events.
#
# Needs: $root, $work, $app. Sets $pid.

ui_tools() { # builds the helpers into $work
  local t
  for t in press window frontmost; do swiftc -O -o "$work/$t" "$root/scripts/ui/macos/$t.swift"; done
}

ui_watch() { # starts watching the frontmost app until ui_unwatch
  : >"$work/watching"
  # However the check ends, the watcher stops and the app with it.
  trap 'rm -f "$work/watching"; kill "${pid:-}" 2>/dev/null || true' EXIT
  "$work/frontmost" 86400 "$app" -until "$work/watching" >"$work/frontmost.log" 2>&1 &
  watcher=$!
  until grep -q READY "$work/frontmost.log"; do sleep 0.05; done
}

ui_unwatch() { # stops watching; fails if the app under test came to the front
  rm -f "$work/watching"
  if ! wait "$watcher"; then
    echo "FAIL the app under test came to the front:" >&2
    grep -v '^moved' "$work/frontmost.log" >&2
    return 1
  fi
}

ui_open() { # log, app arguments…: launches in the background, waits for its window
  local log=$1
  shift
  "$app" -ui.background YES -NSAppSleepDisabled YES "$@" >"$log" 2>&1 &
  pid=$!
  for _ in $(seq 150); do "$work/window" "$pid" >/dev/null 2>&1 && break; sleep 0.2; done
  # A window just listed can still refuse to be placed for a moment.
  for _ in $(seq 10); do
    osascript -e "tell application \"System Events\" to tell (first process whose unix id is $pid)
      set position of window 1 to {40, 60}
      set size of window 1 to {1280, 900}
    end tell" >/dev/null 2>&1 && return 0
    sleep 1
  done
  echo "no window to place for $pid" >&2
  return 1
}

ui_close() { kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null || true; }
