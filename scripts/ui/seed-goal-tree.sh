#!/usr/bin/env bash
# Seeds $WD_HOME with the goals the goal-tree design screens show
# (docs/design/goal-tree), so a UI check can put each app beside its
# reference. Structure goes through wd; states, steps and the events a
# running loop would have filed are written straight into the ledger,
# because no agent runs here.
#
#   WD_HOME=/tmp/ui-home scripts/ui/seed-goal-tree.sh [wd binary]
set -euo pipefail
: "${WD_HOME:?WD_HOME names the ledger to seed; never your own}"
WD=${1:-wd}
db=$WD_HOME/ledger.db
repo=$WD_HOME/repo
mkdir -p "$WD_HOME" "$repo"
git -C "$repo" init -q -b main
git -C "$repo" -c user.name=ui -c user.email=ui@wd commit -q --allow-empty -m init

"$WD" projects add metariki-eqms "$repo" --runner claude --lazyspec n >/dev/null
id() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id") or d["goal"]["id"])'; }
sub() { "$WD" goal add "$@" --json | id; }
q() { sqlite3 "$db" "$1"; }
ago() { python3 -c "import datetime,sys; print((datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(minutes=int(sys.argv[1]))).strftime('%Y-%m-%dT%H:%M:%S.000Z'))" "$1"; }
event() { # work kind body minutes-ago
  q "INSERT INTO event (work, kind, body, at) VALUES ('$1', '$2', '$(printf %s "$3" | sed "s/'/''/g")', '$(ago "$4")')"
}
payload() { # work kind body payload minutes-ago
  q "INSERT INTO event (work, kind, body, at, payload) VALUES ('$1', '$2', '$(printf %s "$3" | sed "s/'/''/g")', '$(ago "$5")', '$(printf %s "$4" | sed "s/'/''/g")')"
}
state() { q "UPDATE work SET state = '$2' WHERE id = '$1'"; event "$1" state "$2" "${3:-30}"; }

# The goal on the main screen.
G=$("$WD" goal add metariki-eqms "Release notes for every release" --json | id)
q "UPDATE work SET done_when = 'For every release, whoever deploys gets a guide and customers get a plain-language note, produced by any agent from one documented workflow.', finish_line = 'complete PR', finish_source = 'taste' WHERE id = '$G'"
event "$G" decision "finishes at: complete PR (from taste)" 120
S1=$(sub "$G" "Every release is tagged and records what changed since the last one" --done-when "each release has a vX.Y.Z tag and a change list grouped by type")
S2=$(sub "$G" "Whoever deploys gets a guide for each release" --done-when "a release's guide lists its migrations, env and infra changes, feature flags, per-org config, how to verify it and how to roll it back" --split)
S3=$(sub "$G" "Customers get a plain-language release note" --done-when "a customer note in plain language is produced for each release")
S4=$(sub "$G" "Any agent can produce the notes from the documented workflow" --done-when "a fresh agent following the workflow produces both notes for a release" --after "$S2,$S3")
S2a=$(sub "$S2" "The guide lists migrations and env changes" --done-when "every migration and env change since the last tag is listed")
S2b=$(sub "$S2" "The guide lists feature flags and per-org config" --done-when "every flag and per-org config change is listed")
S2c=$(sub "$S2" "The guide says how to verify and roll back" --done-when "the guide has verify and rollback steps for the release")
S2d=$(sub "$S2" "Guides are published to the team wiki" --done-when "the guide is on the wiki")
"$WD" goal drop "$S2d" "guides live with each release in docs/releases/" >/dev/null
"$WD" decide "$G" "Release notes are written in English only for now" >/dev/null

state "$G" running 115
for t in "$S1" "$S2a"; do
  state "$t" done 60
  event "$t" judged "met: verify passed and the change list is grouped by type" 55
done
q "UPDATE work SET runner = 'claude', session = 'seed-s3' WHERE id = '$S3'"
state "$S3" running 40
q "INSERT INTO step (work, n, text, state) VALUES
  ('$S3', 1, 'Read how past releases were announced', 'done'),
  ('$S3', 2, 'Pick which change types customers see', 'done'),
  ('$S3', 3, 'Map module names to product names', 'done'),
  ('$S3', 4, 'Write the customer note template', 'doing'),
  ('$S3', 5, 'Generate the note for v1.0.0 as a sample', 'todo')"
# The customer-note agent's session: Bash and Thinking steps, then a reply
# with a table. A claude session is read from $HOME/.claude/projects, so it is
# written only where HOME holds the seeded ledger, as in the UI containers.
if [ "$(dirname "$WD_HOME")" = "$HOME" ]; then
  mkdir -p "$HOME/.claude/projects/wd-seed"
  python3 - "$HOME/.claude/projects/wd-seed/seed-s3.jsonl" <<'PY'
import datetime, json, sys
now = datetime.datetime.now(datetime.timezone.utc)
def at(m): return (now - datetime.timedelta(minutes=m)).strftime('%Y-%m-%dT%H:%M:%S.000Z')
def user(m, content): return {"type": "user", "timestamp": at(m), "message": {"content": content}}
def agent(m, *parts): return {"type": "assistant", "timestamp": at(m), "message": {"content": list(parts), "stop_reason": "tool_use"}}
def tool(i, name, inp): return {"type": "tool_use", "id": i, "name": name, "input": inp}
def result(m, i, text): return user(m, [{"type": "tool_result", "tool_use_id": i, "content": text}])
reply = """The template reads well for the three sample releases. What I checked:

| Check | Run? | Result |
|:---|:---:|---:|
| `go test ./notes/...` | yes | **passed** |
| Customer wording review | yes | 2 terms renamed |
| Link check | no | skipped: offline |

> Module names stay out of the customer note; only product names show.

Next I generate the note for v1.0.0 as a sample."""
rows = [
    user(40, "Write the customer release note template, in plain language."),
    agent(39, {"type": "thinking", "thinking": "Start from how past releases were announced, then map module names to product names."}),
    agent(38, tool("t1", "Bash", {"command": "cd release-notes && ls templates/", "description": "List the templates"})),
    result(38, "t1", "customer.md\ndeploy.md"),
    agent(37, tool("t2", "Bash", {"command": "go test ./notes/...", "description": "Run the notes tests"})),
    result(36, "t2", "ok  \trelease-notes/notes\t0.41s"),
    agent(35, {"type": "thinking", "thinking": "The tests pass. A short table says what was checked."}),
    {"type": "assistant", "timestamp": at(34), "message": {"content": [{"type": "text", "text": reply}], "stop_reason": "end_turn"}},
]
with open(sys.argv[1], "w") as f:
    for r in rows:
        f.write(json.dumps(r) + "\n")
PY
fi
q "UPDATE work SET runner = 'codex', session = 'seed-s2b' WHERE id = '$S2b'"
state "$S2b" running 35
q "UPDATE work SET runner = 'claude', session = 'seed-s2c' WHERE id = '$S2c'"
state "$S2c" needs-input 4
event "$S2c" question "Should a release's rollback steps cover database down-migrations, or only redeploying the previous build?
- Only redeploying the previous build (recommended)
- Redeploy and down-migrations" 4
state "$S2" needs-input 4
event "$G" course "correct: guides live with each release, not in the wiki" 40
event "$G" course "on course: the tag format the first sub-goal chose suits the other three" 12
event "$S2" note "chore: Refreshed dependencies in the main checkout so the flags agent could run validate" 31

# The sidebar's other goals.
W=$("$WD" goal add metariki-eqms "Resolve today's GitHub issues" --json | id)
state "$W" running 90
# With SEED_SESSIONS=1, sessions' conversations too: they live in claude's
# store under HOME, so the app is run with HOME at $WD_HOME/home to read
# these invented ones. A waiting question in one changes what needs you, so
# the design screens are seeded without them.
L=""
if [ -n "${SEED_SESSIONS:-}" ]; then
  sessions=$WD_HOME/home/.claude/projects/seed
  python3 "$(dirname "$0")/seed-session.py" "$sessions/seed-s3.jsonl"
  L=$(sub "$W" "Every open issue has an owner" --done-when "each open issue is assigned")
  q "UPDATE work SET runner = 'claude', session = 'seed-long' WHERE id = '$L'"
  state "$L" running 20
  python3 "$(dirname "$0")/seed-session.py" "$sessions/seed-long.jsonl" --long
fi

# With SEED_OLD_PLAN=1, a goal of an older plan (screen: oldplan): tasks
# grouped by heading rather than sub-goal rows, titled by their whole brief,
# some finished, some stopped without a report, one working. Its questions
# change what needs you, so the design screens are seeded without it.
O=""
if [ -n "${SEED_OLD_PLAN:-}" ]; then
  O=$("$WD" goal add metariki-eqms "We filed a lot of issues today; resolve them all" --json | id)
  state "$O" running 100
  old() { # heading state brief
    local t
    local by=()
    [ -z "$1" ] || by=(--heading "$1")
    t=$("$WD" add metariki-eqms "placeholder" --goal "$O" ${by[@]+"${by[@]}"} --json | id)
    # Older rows kept the whole brief as their title.
    q "UPDATE work SET title = '$(printf %s "$3" | sed "s/'/''/g")', runner = 'claude', session = 'seed-$t' WHERE id = '$t'"
    state "$t" "$2" 50
    echo "$t"
  }
  old "Change Control template visibility (#490)" review "Fix the parent template so that choosing New Product shows its section, and add the married test that proves it. Work off main in your own worktree." >/dev/null
  old "Change Control template visibility (#490)" review "Fix the Please specify if Others visibility rule (it is missing its prefix) in the shared template, with a test." >/dev/null
  T1=$(old "Time answers in the org clock format (#501)" needs-input "Add a forms requirement and its married test saying a time answer shows, is entered and prints in the organisation's time format. Then convert time inputs to an org-format mask.")
  event "$T1" sent "have you tested it end to end with all our tests, including browser tests?" 12
  event "$T1" note "stopped without a report: No. Only part of the suite has run on this change, and none of it in a browser.
The unit and API tiers pass; the browser tier needs the dev stack up.
Shall I run all three?" 11
  T2=$(old "Printed request content independent of who prints (#493)" running "Add the guard to the not-allowed banners so a printed request shows the same content whoever prints it, with a test.")
  T3=$(old "Printed request content independent of who prints (#493)" needs-input "Add a forms requirement and its married test saying entity-link context prints the same for every role.")
  event "$T3" note "stopped without a report: Ready and waiting for your release. I haven't pushed or opened a pull request." 25
  q "INSERT INTO step (work, n, text, state) VALUES
    ('$T2', 1, 'Find every not-allowed banner', 'done'),
    ('$T2', 2, 'Add the guard to each', 'doing'),
    ('$T2', 3, 'Run the print tests', 'todo')"
  old "" review "Check the PDF export on main: does a form PDF print accented names and the euro sign correctly in a text answer, a comment and a fillable field? Add a requirement and its married test, and fix any case that still fails." >/dev/null
fi

# A course change waiting on the person (screen: pivot).
D=$("$WD" goal add metariki-eqms "Why does sync drop events?" --json | id)
q "UPDATE work SET done_when = 'The cause is found and explained, with proof', finish_line = 'discovery', finish_source = 'goal' WHERE id = '$D'"
D1=$(sub "$D" "The cause is found and explained" --done-when "a findings report names the cause, with proof")
D2=$(sub "$D" "We know which tenants lost events, and how many" --done-when "a list of tenants with lost event counts")
D3=$(sub "$D" "Compare other sync vendors" --done-when "a comparison of two vendors")
state "$D1" done 70
event "$D1" judged "met: the report names the SDK's dropped 429 batches, with logs" 66
q "UPDATE work SET runner = 'claude', session = 'seed-d2' WHERE id = '$D2'"
state "$D2" running 50
state "$D" needs-input 6
found='The vendor SDK drops a batch when the API answers 429 and never retries it. About 0.4% of events since the March upgrade, all on large tenants.'
change=$(python3 -c 'import json,sys; d3=sys.argv[1]; g=sys.argv[2]; print(json.dumps({"actions":["GOAL "+g+": A rejected batch is retried until delivered | done when: a 429 batch is retried until delivered","GOAL "+g+": Events lost since March are replayed, once each | done when: every lost event is replayed exactly once","SUPERSEDE "+d3+": no longer needed: the vendor is fine once retried"],"title":"Sync never drops events","done_when":"A 429 batch is retried until delivered, and events lost since March are replayed","finish":"complete PR","found":sys.argv[3]}))' "$D3" "$D" "$found")
payload "$D" question "Change course?
  Now: Why does sync drop events? | done when: The cause is found and explained, with proof | finish: discovery
  Proposed: Sync never drops events | done when: A 429 batch is retried until delivered, and events lost since March are replayed | finish: complete PR
  Found: $found
  Why: The cause is found and the fix is small: one retry policy plus a replay of what was lost. A separate goal would repeat this context.
- Change course (recommended)
- Keep the goal as it is" "$change" 6

# A breakdown proposed in ask mode (screen: proposal).
A=$("$WD" goal add metariki-eqms "An inspector can get a full audit trail" --json | id)
q "UPDATE work SET done_when = 'An org admin exports, for any date range, every record change with who, when, what and why, in a file an inspector accepts.', finish_line = 'complete PR', finish_source = 'taste' WHERE id = '$A'"
A1=$(sub "$A" "Every record change keeps who, when, what and why" --done-when "a change to any form, template or setting stores its before and after, its author and a reason" --split)
A2=$(sub "$A" "An admin can export the trail for a date range" --done-when "the export is a signed PDF and a CSV, filtered by range, module and person" --after "$A1")
A3=$(sub "$A" "The trail cannot be edited or deleted" --done-when "no role, API or migration can change or remove a stored entry, and a test proves it" --after "$A1")
A4=$(sub "$A" "An inspector's checklist maps to the export" --done-when "each audit-trail clause points to where the export satisfies it" --after "$A2,$A3")
event "$A" note "proposed: 4 sub-goals" 2
why='An inspector checks three things: that every change is captured, that it can be produced on demand, and that nobody can alter it. Each is its own sub-goal, so each is judged on its own.

Number 1 is large, so it will be planned into its own sub-goals when it starts, once the modules it touches are known.'
left_out="Electronic signatures: they already exist and the goal doesn't ask to change them."
judge_by="Exporting last month's trail for one org and handing it to an inspector with the checklist."
reasoning=$(python3 -c 'import json,sys; print(json.dumps({"why":sys.argv[1],"left_out":sys.argv[2],"judge_by":sys.argv[3]}))' "$why" "$left_out" "$judge_by")
payload "$A" note "reasoning: why: $why | left out: $left_out | judge by: $judge_by" "$reasoning" 2

# Where a goal finishes, asked before planning (screen: finishline).
F=$("$WD" goal add metariki-eqms "Pick a queue library for the event bus" --json | id)
q "UPDATE work SET detail = 'Compare NATS, Redis Streams and Kafka for the event bus and pick one. We push about 2k events a second at peak.' WHERE id = '$F'"
state "$F" needs-input 1
sources='{"sources":{"goal":"\"Compare and pick one\" reads as research","repo":"No finished goals yet to learn from","taste":"In auto mode, a complete PR is done"}}'
payload "$F" question "Where does this goal finish?
- research (recommended)
- local build
- complete PR" "$sources" 1
P=$("$WD" goal add metariki-eqms "Printed forms keep every signature" --json | id)
q "UPDATE work SET finish_line = 'complete PR', finish_source = 'taste' WHERE id = '$P'"
event "$P" pr "pull-request https://github.com/metariki/metariki-eqms/pull/512" 130
state "$P" done 120

echo "home=$WD_HOME/home issues=$W long=$L oldplan=$O goal=$G subgoal=$S2 working=$S3 question=$S2c pivot=$D proposal=$A finish=$F"
