package cli

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"

	"wd/internal/core"
)

// Every requirement in cli.lazyspec.md is married to a test here. The schema
// tests marshal the domain types and assert the wire keys; the functional
// tests run the Go CLI over a seeded fixture and assert the --json schema,
// key values, error messages and exit codes.

func TestWorkItemsSerializeWithTheWireSchema(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	w := core.Work{
		ID: "abc12345", Project: "p", Title: "T", Detail: "D",
		Kind: core.WorkTask, State: core.StateRunning,
		Runner: strPtr("claude"), Session: strPtr("ses_1"), Ref: strPtr("ref1"),
		Cwd: strPtr("/tmp/x"), Created: "c", Updated: "u",
		Parent: strPtr("parent1"), Heading: strPtr("H"), Claim: strPtr("cl"),
		Impact: strPtr("src/a"),
	}
	assertKeys(t, w, []string{
		"id", "project", "title", "detail", "kind", "state", "runner", "session",
		"ref", "cwd", "created", "updated", "parent", "heading", "claim", "impact",
	})
	// Nullable fields are null, not omitted, when unset.
	empty := core.Work{ID: "x", Project: "p", Title: "t", Kind: core.WorkTask, State: core.StateQueued, Created: "c", Updated: "u"}
	assertKeys(t, empty, []string{
		"id", "project", "title", "detail", "kind", "state", "runner", "session",
		"ref", "cwd", "created", "updated", "parent", "heading", "claim", "impact",
	})
}

func TestLedgerObjectsKeepTheirColumnNames(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	assertKeys(t, core.Event{ID: 1, Work: "w", Kind: core.EventReport, Body: "b", At: "a"},
		[]string{"id", "work", "kind", "body", "at"})
	assertKeys(t, core.Feedback{ID: 1, Text: "t", Project: strPtr("p"), Card: strPtr("c"), Source: core.FeedbackDirector, At: "a"},
		[]string{"id", "text", "project", "card", "source", "at"})
	// resolved_at keeps the ledger's snake_case column name.
	assertKeys(t, core.Concern{ID: 1, Work: "w", Text: "t", Resolved: 1, Decision: strPtr("d"), At: "a", ResolvedAt: strPtr("ra")},
		[]string{"id", "work", "text", "resolved", "decision", "at", "resolved_at"})
	assertKeys(t, core.Worktree{ID: 1, Work: "w", Path: "/p", Branch: strPtr("b"), Kind: core.WorktreeShared, State: core.WorktreeActive, Created: "c"},
		[]string{"id", "work", "path", "branch", "kind", "state", "created"})
	assertKeys(t, core.Conflict{A: "a", B: "b", Paths: []string{"src/x"}},
		[]string{"a", "b", "paths"})
	assertKeys(t, core.Candidate{Key: "k", Count: 2, Texts: []string{"a", "b"}},
		[]string{"key", "count", "texts"})
}

func TestEmptyCollectionsSerializeAsEmptyArrays(t *testing.T) {
	f := newCLIFixture(t)
	// An epic with no tasks: tasks --json is [], not null.
	out := f.runOK(t, "tasks", f.ids["planEpic"], "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("tasks --json = %q, want []", out)
	}
	// No concerns: concern list --json is [].
	out = f.runOK(t, "concern", "list", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("concern list --json = %q, want []", out)
	}
	// No conflicts: conflict --json is [].
	out = f.runOK(t, "conflict", f.ids["epic"], "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("conflict --json = %q, want []", out)
	}
}

func TestCommandsComputeTheSameStatesAndValues(t *testing.T) {
	f := newCLIFixture(t)
	epic, t2, t3 := f.ids["epic"], f.ids["t2"], f.ids["t3"]

	// status --json: the epic and its tasks, with states.
	out := f.runOK(t, "status", "--json")
	assertHasKey(t, out, `"kind": "epic"`)
	assertHasKey(t, out, `"state": "running"`)

	// tasks --json: open tasks grouped under the epic (t1 is done, filtered).
	out = f.runOK(t, "tasks", epic, "--json")
	assertHasKey(t, out, `"id": "`+t2+`"`)
	assertHasKey(t, out, `"heading": "Data"`)

	// add: a new task is queued.
	out = f.runOK(t, "add", "sample-app", "Fresh task", "--json")
	assertHasKey(t, out, `"state": "queued"`)
	assertHasKey(t, out, `"title": "Fresh task"`)

	// set: queued → running.
	id := jsonString(t, out, "id")
	f.runOK(t, "set", id, "running", "--json")
	out = f.runOK(t, "status", "--json", "--all")
	assertHasKey(t, out, `"id": "`+id+`",`)
	assertHasKey(t, out, `"state": "running"`)

	// claim + impact + conflict: overlapping claims pair (t1 is done, excluded).
	f.runOK(t, "claim", t3, "ses_z", "--json")
	f.runOK(t, "impact", t3, "+src/dashboards", "--json")
	out = f.runOK(t, "conflict", epic, "--json")
	assertHasKey(t, out, `"`+t2+`"`)
	assertHasKey(t, out, `"`+t3+`"`)
	assertHasKey(t, out, `"src/dashboards"`)

	// concern add + resolve: the decision is recorded.
	out = f.runOK(t, "concern", "add", t3, "a concern", "--json")
	cid := jsonNumber(t, out, "id")
	f.runOK(t, "concern", "resolve", cid, "the decision", "--json")
	out = f.runOK(t, "concern", "list", epic, "--json")
	if strings.Contains(out, "a concern") {
		t.Fatalf("resolved concern still listed: %s", out)
	}

	// soft-done: a fresh standalone needs a DONE report first.
	errStr := f.runFail(t, "soft-done", f.ids["standalone"], "--no-code")
	if !strings.Contains(errStr, "not ready for soft-done") {
		t.Fatalf("soft-done should refuse without a DONE report, got %q", errStr)
	}

	// brief: renders and transitions queued → briefed.
	out = f.runOK(t, "brief", t3)
	if !strings.Contains(out, "# Brief "+t3) {
		t.Fatalf("brief missing header")
	}
	out = f.runOK(t, "status", "--json", "--all")
	assertHasKey(t, out, `"state": "briefed"`)

	// attach: binds an outside session.
	out = f.runOK(t, "attach", t3, "advisor-s9", "--runner", "advisor", "--json")
	assertHasKey(t, out, `"runner": "advisor"`)
	assertHasKey(t, out, `"session": "advisor-s9"`)

	// send: records a sent event and keeps the session.
	f.runOK(t, "send", t2, "hello", "--json")
	out = f.runOK(t, "events", t2, "--json")
	assertHasKey(t, out, `"kind": "sent"`)

	// verify: records a pass and exits 0.
	f.runOK(t, "verify", t2, "--json")
	out = f.runOK(t, "events", t2, "--json")
	assertHasKey(t, out, `"kind": "verify"`)

	// epic status: the epic with its open tasks.
	out = f.runOK(t, "epic", "status", epic, "--json")
	assertHasKey(t, out, `"open":`)

	// distill: feedback seen twice on one card is a candidate.
	out = f.runOK(t, "distill", "--json")
	assertHasKey(t, out, `"key": "card:proj-rule"`)

	// scan: the project-scoped adopted card with recurring evidence promotes.
	out = f.runOK(t, "scan", "--json")
	assertHasKey(t, out, `"promotion"`)

	// worktree list: the epic's shared worktree.
	out = f.runOK(t, "worktree", "list", epic, "--json")
	assertHasKey(t, out, `"kind": "shared"`)
	assertHasKey(t, out, `"state": "active"`)

	// runner list: the four built-ins plus the spec-file runners.
	out = f.runOK(t, "runner", "list", "--json")
	for _, r := range []string{"claude", "opencode", "codex", "ao", "myagent", "planner", "advisor"} {
		assertHasKey(t, out, `"`+r+`"`)
	}

	// models: from the provider CLI, not a registry.
	out = f.runOK(t, "models", "opencode", "--json")
	assertHasKey(t, out, `"anthropic/claude-opus-5"`)
}

func TestSetupReportsTheRuntimeDependencies(t *testing.T) {
	f := newCLIFixture(t)
	// With the fixture's fake runners on PATH, every dependency is present.
	out := f.runOK(t, "setup", "--json")
	for _, name := range []string{"git", "claude", "opencode", "codex", "ao"} {
		assertHasKey(t, out, `"`+name+`"`)
		assertHasKey(t, out, `"status": "present"`)
	}
	// The text report is a table: name, status, then path or install command.
	out = f.runOK(t, "setup")
	if !strings.Contains(out, "git") || !strings.Contains(out, "present") {
		t.Fatalf("setup text report = %q", out)
	}
}

func TestSetupFailsWhenADependencyIsMissing(t *testing.T) {
	f := newCLIFixture(t)
	// Bare PATH: the exit code matches the report — 1 when any dependency
	// is missing, 0 when all are present.
	code, out, errStr := f.runBare(t, "setup", "--json")
	if !strings.Contains(out, `"git"`) {
		t.Fatalf("setup --json = %q, want the git dependency", out)
	}
	missing := strings.Count(out, `"status": "missing"`)
	if missing > 0 {
		if code != 1 {
			t.Fatalf("setup exited %d with %d missing, want 1", code, missing)
		}
		if !strings.Contains(errStr, "missing") {
			t.Fatalf("stderr %q does not name the missing dependencies", errStr)
		}
		return
	}
	if code != 0 {
		t.Fatalf("setup exited %d with every dependency present, want 0", code)
	}
}

func TestErrorsAndExitCodesMatch(t *testing.T) {
	f := newCLIFixture(t)
	epic, t1 := f.ids["epic"], f.ids["t1"]

	cases := []struct {
		args []string
		want string
	}{
		{[]string{}, "wd <projects|"},
		{[]string{"unknowncmd"}, "wd <projects|"},
		{[]string{"add"}, "usage: wd add"},
		{[]string{"tasks"}, "usage: wd tasks"},
		{[]string{"tasks", t1}, "is not an epic"},
		{[]string{"conflict", t1}, "is not an epic"},
		{[]string{"set", t1, "bogus"}, "unknown state bogus"},
		{[]string{"set", t1, "done"}, "illegal transition"},
		{[]string{"models", "bogus"}, "unknown runner bogus"},
		{[]string{"impact", t1}, "usage: wd impact"},
		{[]string{"merge", f.ids["standalone"]}, "merge is only for tasks under an epic"},
		{[]string{"soft-done", epic}, "not ready for soft-done"},
		{[]string{"epic", "plan"}, "usage: wd epic plan"},
		{[]string{"concern", "add", t1}, "usage: wd concern add"},
		{[]string{"runner", "init", "bad name!"}, "usage: wd runner init"},
		{[]string{"feedback", "add", "x", "--source", "bogus"}, "unknown source bogus"},
	}
	for _, c := range cases {
		errStr := f.runFail(t, c.args...)
		if !strings.Contains(errStr, c.want) {
			t.Errorf("wd %s: stderr %q does not contain %q", strings.Join(c.args, " "), errStr, c.want)
		}
	}
}

// assertKeys marshals v and asserts the top-level JSON keys are exactly want.
func assertKeys(t *testing.T, v any, want []string) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var got []string
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %v, want %v", got, want)
	}
}

func assertHasKey(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("output %q does not contain %q", out, want)
	}
}

func jsonString(t *testing.T, out, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	t.Fatalf("key %s not a string in %s", key, out)
	return ""
}

// jsonNumber extracts an integer JSON field as a string.
func jsonNumber(t *testing.T, out, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if v, ok := m[key].(float64); ok {
		return strconv.Itoa(int(v))
	}
	t.Fatalf("key %s not a number in %s", key, out)
	return ""
}
