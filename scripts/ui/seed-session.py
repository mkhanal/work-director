"""Writes an invented claude session store, so a UI check shows a task's
conversation: a brief, thinking, tool calls, a reply with a pipe table, a
question answered and one waiting. With --long, the same turns repeated
until the session is about 250 KB, for timing how fast a long one shows.

    python3 scripts/ui/seed-session.py <file.jsonl> [--long]
"""
import datetime
import json
import os
import sys

out, long_run = sys.argv[1], "--long" in sys.argv[2:]
os.makedirs(os.path.dirname(out), exist_ok=True)
now = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(minutes=30)
n = 0


def at():
    global n
    n += 1
    return (now + datetime.timedelta(seconds=n * 5)).strftime("%Y-%m-%dT%H:%M:%S.000Z")


def user(content):
    return {"type": "user", "timestamp": at(), "message": {"content": content}}


def assistant(*parts):
    return {"type": "assistant", "timestamp": at(),
            "message": {"model": "claude-sonnet", "content": list(parts), "usage": {"input_tokens": 900, "output_tokens": 120}}}


def tool(tid, name, inp, result, failed=False):
    return [assistant({"type": "tool_use", "id": tid, "name": name, "input": inp}),
            user([{"type": "tool_result", "tool_use_id": tid, "content": result, "is_error": failed}])]


REPLY = """The customer note template is drafted. What I checked before writing it:

| Check | Run? | Result |
|---|:---:|---:|
| `make notes` builds the sample | **yes** | passed |
| Module names map to product names | yes | 14 of 14 |
| Links in the note open | no | [see the plan](https://example.com/plan) |

> Customers read the note in their inbox, so it stays under 200 words.

---

Next steps:
- Generate the note for v1.0.0
  - check every product name against the map
- [x] Template written
- [ ] Sample reviewed"""


def turn(k):
    t = f"t{k}"
    rows = [assistant({"type": "thinking", "thinking": "The note should name products, not modules. I will read how past releases were announced first, then map module names to product names.\nThe map lives next to the release scripts."})]
    rows += tool(t + "a", "Bash", {"command": "cd release-notes && ls templates"}, "customer.md\ndeploy.md")
    rows += tool(t + "b", "Read", {"file_path": "release-notes/templates/customer.md"}, "# {{version}}\n\n{{changes}}")
    rows += tool(t + "c", "Bash", {"command": "make notes VERSION=v1.0.0 && head -20 out/customer.md"}, "wrote out/customer.md\n# v1.0.0\n- Faster sign-in\n- Printed forms keep signatures")
    rows.append(assistant({"type": "thinking", "thinking": "The sample builds. Now check the links and write up what was checked."}))
    rows += tool(t + "d", "Bash", {"command": "cd release-notes && go test ./... 2>&1 | tail -5"}, "ok  \trelease-notes/map\t0.21s\nok  \trelease-notes/render\t0.34s")
    rows.append(assistant({"type": "text", "text": REPLY}))
    return rows


rows = [user("Customers get a plain-language release note.\n\nDone when: a customer note in plain language is produced for each release.\nWrite the template, then generate the note for v1.0.0 as a sample.")]
rows += turn(0)
rows += tool("q1", "AskUserQuestion", {"questions": [{"header": "Tone", "question": "Should the note speak to customers as you or as they?",
                                                       "multiSelect": False, "options": [{"label": "You (recommended)", "description": "Reads as a letter"}, {"label": "They", "description": "Reads as a report"}]}]},
             "answered")
rows[-1]["toolUseResult"] = {"answers": {"Should the note speak to customers as you or as they?": "You"}}
rows.append(user("Keep it under 200 words, please."))
if long_run:
    k = 1
    while sum(len(json.dumps(r)) for r in rows) < 250_000:
        rows += turn(k)
        rows.append(user(f"Carry on with release {k}."))
        k += 1
rows += turn(99)
rows.append(assistant({"type": "tool_use", "id": "q2", "name": "AskUserQuestion", "input": {"questions": [{"header": "Sample", "question": "Generate the sample for v1.0.0 or for the latest release?", "multiSelect": False,
                                                                                                             "options": [{"label": "v1.0.0 (recommended)", "description": "The release the goal names"}, {"label": "Latest", "description": "The newest tag"}]}]}}))
with open(out, "w") as f:
    for r in rows:
        f.write(json.dumps(r) + "\n")
