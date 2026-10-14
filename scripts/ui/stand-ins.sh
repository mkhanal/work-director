# Stand-ins for every program wd reaches agents and services through, sourced
# by the UI checks. The app's wd serve supervises every running goal in the
# seeded ledger: given the real claude it would spawn a real supervisor on
# this machine (one check only failed to because the workspace was not
# trusted). So the wd the app runs is a wrapper that puts these first on its
# PATH, and nothing else: the app's own login PATH never reaches wd.
#
# Each stand-in writes its arguments to <dir>/calls.log and does nothing:
# `claude agents` lists no sessions, everything else fails.
#
#   stand_ins <dir> <real wd> -> <dir>/wd, the wd for WD_BIN
#   stand_ins_check <ledger> <last event id before the run> -> fails on any
#     spawn or message the run filed

stand_ins() {
  local dir=$1 wd=$2 name
  mkdir -p "$dir/bin"
  for name in claude codex opencode gh code cursor; do
    cat >"$dir/bin/$name" <<EOF
#!/bin/sh
printf '%s %s\n' "$name" "\$(printf '%s' "\$*" | tr '\n' ' ' | cut -c1-160)" >>"$dir/calls.log"
if [ "$name" = claude ] && [ "\$1" = agents ]; then echo "[]"; exit 0; fi
echo "$name: a stand-in for a UI check; nothing runs" >&2
exit 1
EOF
    chmod +x "$dir/bin/$name"
  done
  cat >"$dir/wd" <<EOF
#!/bin/sh
export PATH="$dir/bin:/usr/bin:/bin:/usr/sbin:/sbin"
exec "$wd" "\$@"
EOF
  chmod +x "$dir/wd"
}

stand_ins_check() {
  local filed
  filed=$(sqlite3 -cmd ".timeout 10000" "$1" "SELECT count(*) FROM event WHERE id > $2 AND kind IN ('spawn', 'sent', 'send', 'start', 'supervised', 'attach')")
  if [ "$filed" != 0 ]; then
    echo "FAIL the run filed $filed spawn or message events:" >&2
    sqlite3 "$1" "SELECT work, kind, body FROM event WHERE id > $2 AND kind IN ('spawn', 'sent', 'send', 'start', 'supervised', 'attach')" >&2
    return 1
  fi
  echo "PASS no agent was started or messaged"
}
