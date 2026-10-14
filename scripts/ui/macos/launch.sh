# Launching the macOS app for a UI check, sourced by act.sh and shoot.sh.
# The check runs on a Mac a person is using, so the app it launches must
# never become the active app (an active window takes their keystrokes: a
# check once came up frontmost with all its text selected), must never touch
# the settings of their own Work Director, and must never start or message a
# real agent.
#
# - The build is copied into a bundle of its own in $work, with a private
#   bundle id (dev.mkhanal.workdirector.uicheck-<pid>) and LSBackgroundOnly,
#   and launched with `open -g -n`: in the background, a new instance, its
#   settings in a defaults domain of its own that is deleted afterwards. The
#   instance is found by a launch argument unique to it (-ui.run).
# - The app also gets `-ui.background YES`: it sets its activation policy to
#   prohibited before any window exists, never calls activate, and sends its
#   window behind every other. The window still draws, answers the
#   accessibility API (press.swift, text.swift) and is photographed alone
#   with `screencapture -l <window>` (window.swift) wherever it is stacked.
# - `-NSAppSleepDisabled YES` keeps App Nap from slowing a window nobody sees.
# - frontmost.swift watches the frontmost app from before the first launch
#   until the check ends; should the app under test ever come to the front it
#   is ended at once and the check fails.
# - The wd the app runs, bundled beside it and as WD_BIN, is a wrapper (stand-ins.sh) that runs wd with stand-in claude,
#   codex, opencode and gh first and alone on its PATH. Afterwards the check
#   fails if the ledger filed any spawn or message, or if `claude agents`
#   lists a session started in the check's folder.
# - Windows are placed through System Events (the accessibility API), which
#   neither raises nor activates them. Never post mouse or key events.
#
# Needs: $root, $work, $app (the built binary). Sets $pid, $bundle.

. "$root/scripts/ui/stand-ins.sh"

ui_tools() { # builds the helpers and the app's own bundle into $work
  local t
  for t in press window frontmost; do swiftc -O -o "$work/$t" "$root/scripts/ui/macos/$t.swift"; done
  bundle=dev.mkhanal.workdirector.uicheck-$$
  mkdir -p "$work/WorkDirector.app/Contents/MacOS"
  cp "$app" "$work/WorkDirector.app/Contents/MacOS/WorkDirector"
  cat >"$work/WorkDirector.app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleIdentifier</key><string>$bundle</string>
  <key>CFBundleExecutable</key><string>WorkDirector</string>
  <key>CFBundleName</key><string>Work Director UI check</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSBackgroundOnly</key><true/>
</dict></plist>
EOF
  app=$work/WorkDirector.app/Contents/MacOS/WorkDirector
  stand_ins "$work/stand-ins" "$WD_BIN"
  export WD_BIN=$work/stand-ins/wd
  # A bundled app runs the wd inside it, and only that one.
  cp "$WD_BIN" "$work/WorkDirector.app/Contents/MacOS/wd"
}

ui_sessions() { # sessions `claude agents` lists in the check's folder
  local claude
  claude=$(PATH=$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH command -v claude) || { echo 0; return; }
  "$claude" agents --json --all 2>/dev/null |
    python3 -c 'import json,sys; print(sum(1 for a in json.load(sys.stdin) if (a.get("cwd") or "").startswith(sys.argv[1])))' "$work" ||
    echo 0
}

ui_forget() { # deletes the check's own settings, and the empty file macOS leaves of them
  defaults delete "$bundle" >/dev/null 2>&1 || true
  rm -f "$HOME/Library/Preferences/$bundle.plist"
}

ui_watch() { # starts watching the frontmost app until ui_unwatch
  sessions=$(ui_sessions)
  seeded=$(sqlite3 "$WD_HOME/ledger.db" "SELECT coalesce(max(id), 0) FROM event")
  : >"$work/watching"
  # However the check ends, the watcher stops, the app with it, and its
  # settings go.
  trap 'rm -f "$work/watching"; kill "${pid:-}" 2>/dev/null || true; ui_forget' EXIT
  "$work/frontmost" 86400 "$app" -until "$work/watching" >"$work/frontmost.log" 2>&1 &
  watcher=$!
  until grep -q READY "$work/frontmost.log"; do sleep 0.05; done
}

ui_unwatch() { # stops watching; fails if the app came to the front or reached an agent
  local ok=0
  rm -f "$work/watching"
  if ! wait "$watcher"; then
    echo "FAIL the app under test came to the front:" >&2
    grep -v '^moved' "$work/frontmost.log" >&2
    ok=1
  else
    echo "PASS the app under test never came to the front"
  fi
  ui_forget
  stand_ins_check "$WD_HOME/ledger.db" "$seeded" || ok=1
  if [ "$(ui_sessions)" != "$sessions" ]; then
    echo "FAIL claude agents lists a session started in $work" >&2
    ok=1
  fi
  return $ok
}

ui_open() { # log, app arguments…: launches in the background, waits for its window
  local log=$1 run
  shift
  run=$$-$RANDOM
  open -g -n -a "$work/WorkDirector.app" --stdout "$log" --stderr "$log" \
    --env "WD_HOME=$WD_HOME" --env "WD_BIN=$WD_BIN" ${WD_SWITCH_LOG:+--env WD_SWITCH_LOG=$WD_SWITCH_LOG} \
    --args -ui.background YES -NSAppSleepDisabled YES -ui.run "$run" "$@"
  pid=""
  for _ in $(seq 100); do pid=$(pgrep -f -- "MacOS/WorkDirector .*-ui.run $run") && break; sleep 0.1; done
  [ -n "$pid" ] || { echo "the app did not start" >&2; return 1; }
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

ui_close() { # ends the app and waits until it is gone
  kill "$pid" 2>/dev/null || true
  for _ in $(seq 50); do kill -0 "$pid" 2>/dev/null || return 0; sleep 0.1; done
}
