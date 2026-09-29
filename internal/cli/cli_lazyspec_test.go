package cli

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"wd/internal/core"
	"wd/internal/ledger"
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

	// runner list: the three built-ins plus the spec-file runners.
	out = f.runOK(t, "runner", "list", "--json")
	for _, r := range []string{"claude", "opencode", "codex", "myagent", "planner", "advisor"} {
		assertHasKey(t, out, `"`+r+`"`)
	}

	// models: from the provider CLI, not a registry.
	out = f.runOK(t, "models", "opencode", "--json")
	assertHasKey(t, out, `"anthropic/claude-opus-5"`)
}

func TestDoctorEmitsItsReportOnTheJsonRail(t *testing.T) {
	f := newCLIFixture(t)
	var report map[string]any
	if err := json.Unmarshal([]byte(f.runOK(t, "doctor", "--json")), &report); err != nil {
		t.Fatalf("doctor --json: %v", err)
	}
	assertKeys(t, report, []string{"init", "runners", "workspace"})
	runners, ok := report["runners"].([]any)
	if !ok || len(runners) == 0 {
		t.Fatalf("runners = %v, want a non-empty array", report["runners"])
	}
	for _, r := range runners {
		assertKeys(t, r, []string{"command", "detected", "path", "runner"})
	}
	assertKeys(t, report["workspace"], []string{"branch", "changed", "dir", "linked", "repo"})
}

func TestDoctorExitsZeroWhateverItFinds(t *testing.T) {
	f := newCLIFixture(t)
	// Only git on PATH: no runner is detected and the fixture dir is no repo.
	code, out, errStr := f.runAt(t, t.TempDir(), f.envWithPath(t, gitOnlyPath(t)), "doctor")
	if code != 0 {
		t.Fatalf("doctor exited %d, want 0\n%s\n%s", code, out, errStr)
	}
	if !strings.Contains(out, "not detected") || !strings.Contains(out, "no git repo") {
		t.Fatalf("doctor report = %q", out)
	}
}

func TestRunnerListShowsWhichRunnersAreDetected(t *testing.T) {
	f := newCLIFixture(t)
	_, out, _ := f.runEnv(t, f.envWithPath(t, f.pathWith(t, "claude", "myagent")), "runner", "list", "--json")
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("runner list --json: %v\n%s", err, out)
	}
	got := map[string]map[string]any{}
	for _, r := range list {
		assertKeys(t, r, []string{"builtin", "command", "detected", "path", "runner"})
		got[r["runner"].(string)] = r
	}
	want := map[string][2]bool{ // runner: {builtin, detected}
		"claude": {true, true}, "opencode": {true, false}, "codex": {true, false},
		"myagent": {false, true}, "planner": {false, false}, "advisor": {false, false},
	}
	for name, w := range want {
		r, ok := got[name]
		if !ok || r["builtin"] != w[0] || r["detected"] != w[1] || (r["path"] != nil) != w[1] {
			t.Errorf("%s = %v, want builtin %v detected %v with a path when detected", name, r, w[0], w[1])
		}
	}
	_, text, _ := f.runEnv(t, f.envWithPath(t, f.pathWith(t, "claude")), "runner", "list")
	if !strings.Contains(text, "claude") || !strings.Contains(text, "not detected") {
		t.Fatalf("runner list = %q, want detection per runner", text)
	}
}

func TestModelsCoversOnlyDetectedRunners(t *testing.T) {
	f := newCLIFixture(t)
	env := f.envWithPath(t, f.pathWith(t, "opencode"))
	code, out, errStr := f.runEnv(t, env, "models", "--json")
	if code != 0 {
		t.Fatalf("models exited %d\n%s", code, errStr)
	}
	var rows map[string][]string
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("models --json: %v\n%s", err, out)
	}
	if len(rows) != 1 || len(rows["opencode"]) == 0 {
		t.Fatalf("models = %v, want only opencode", rows)
	}
	code, _, errStr = f.runEnv(t, env, "models", "codex")
	if code != 1 {
		t.Fatalf("models codex exited %d, want 1", code)
	}
	assertNotDetected(t, errStr, "codex")
}

func TestSpawningToAnUndetectedRunnerFailsBeforeAnythingChanges(t *testing.T) {
	f := newCLIFixture(t)
	env := f.envWithPath(t, f.pathWith(t, "claude"))
	standalone, planEpic, epic, t3 := f.ids["standalone"], f.ids["planEpic"], f.ids["epic"], f.ids["t3"]
	for _, args := range [][]string{
		{"spawn", standalone, "--runner", "codex"},
		{"epic", "plan", planEpic, "--runner", "codex"},
		{"epic", "spawn", planEpic, "--runner", "codex"},
		{"epic", "run", epic, "--runner", "codex"},
	} {
		code, _, errStr := f.runEnv(t, env, args...)
		if code != 1 {
			t.Fatalf("wd %s exited %d, want 1\n%s", strings.Join(args, " "), code, errStr)
		}
		assertNotDetected(t, errStr, "codex")
	}
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer l.Close()
	for _, id := range []string{standalone, planEpic, t3} {
		w, err := l.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if w.State != core.StateQueued || w.Cwd != nil || w.Session != nil {
			t.Errorf("%s = state %s cwd %v session %v, want untouched", id, w.State, w.Cwd, w.Session)
		}
	}
	wts, err := l.Worktrees(planEpic)
	if err != nil {
		t.Fatalf("worktrees: %v", err)
	}
	if len(wts) != 0 {
		t.Errorf("worktrees for %s = %v, want none", planEpic, wts)
	}
}

// assertNotDetected asserts errStr is the not-detected guidance for runner.
func assertNotDetected(t *testing.T, errStr, runner string) {
	t.Helper()
	for _, want := range []string{runner, "not found on PATH", "wd runner init"} {
		if !strings.Contains(errStr, want) {
			t.Fatalf("stderr %q does not contain %q", errStr, want)
		}
	}
}

func TestStatusMarksWorkStaleAfterThirtyDaysWithoutActivity(t *testing.T) {
	f := newCLIFixture(t)
	standalone, t1, t2, t3 := f.ids["standalone"], f.ids["t1"], f.ids["t2"], f.ids["t3"]
	f.backdate(t, standalone, 31*24*time.Hour)
	f.backdate(t, t2, 29*24*time.Hour)
	f.backdate(t, t1, 90*24*time.Hour)
	f.backdate(t, t3, 40*24*time.Hour)
	f.runOK(t, "pr", t3, "https://github.com/x/sample-app/pull/2")
	planEpic := f.ids["planEpic"]
	f.backdate(t, planEpic, 40*24*time.Hour)
	f.runOK(t, "set", planEpic, "blocked")

	var rows []map[string]any
	if err := json.Unmarshal([]byte(f.runOK(t, "status", "--json", "--all")), &rows); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	stale := map[string]any{}
	for _, r := range rows {
		assertKeys(t, r, []string{
			"id", "project", "title", "detail", "kind", "state", "runner", "session",
			"ref", "cwd", "created", "updated", "parent", "heading", "claim", "impact", "stale",
		})
		stale[r["id"].(string)] = r["stale"]
	}
	for id, want := range map[string]bool{standalone: true, t2: false, t1: false, t3: false, planEpic: false} {
		if stale[id] != want {
			t.Errorf("%s stale = %v, want %v", id, stale[id], want)
		}
	}

	for _, line := range strings.Split(f.runOK(t, "status"), "\n") {
		marked := strings.Contains(line, "stale")
		if strings.HasPrefix(line, standalone) && !marked {
			t.Errorf("stale row unmarked: %q", line)
		}
		if strings.HasPrefix(line, t2) && marked {
			t.Errorf("fresh row marked stale: %q", line)
		}
	}
}

func TestAHumanClosesQueuedBriefedOrBlockedWork(t *testing.T) {
	f := newCLIFixture(t)
	standalone, t2, t3, planEpic := f.ids["standalone"], f.ids["t2"], f.ids["t3"], f.ids["planEpic"]
	f.runOK(t, "set", t3, "briefed")
	f.runOK(t, "set", planEpic, "blocked")
	for _, id := range []string{standalone, t3, planEpic} {
		out := f.runOK(t, "done", id, "--json")
		assertHasKey(t, out, `"state": "done"`)
	}
	f.runOK(t, "set", t2, "needs-input")
	code, _, errStr := f.run(t, "done", t2)
	if code != 1 || !strings.Contains(errStr, "illegal transition needs-input → done") {
		t.Fatalf("done on needs-input exited %d: %q", code, errStr)
	}
}

func TestCommandsThatNeedTasteCardsSayWhereTheyLooked(t *testing.T) {
	f := newCLIFixture(t)
	standalone := f.ids["standalone"]
	outside := t.TempDir()
	var env []string
	for _, kv := range f.env(t, f.bin) {
		if !strings.HasPrefix(kv, "WD_ROOT=") {
			env = append(env, kv)
		}
	}
	for _, args := range [][]string{{"brief", standalone}, {"spawn", standalone}, {"scan"}} {
		code, _, errStr := f.runAt(t, outside, env, args...)
		if code != 1 {
			t.Fatalf("wd %s exited %d, want 1\n%s", strings.Join(args, " "), code, errStr)
		}
		for _, want := range []string{"WD_ROOT", filepath.Dir(f.goBin), outside} {
			if !strings.Contains(errStr, want) {
				t.Fatalf("wd %s stderr %q does not name %q", strings.Join(args, " "), errStr, want)
			}
		}
	}
	if code, _, errStr := f.runAt(t, outside, env, "status"); code != 0 {
		t.Fatalf("status outside the checkout exited %d\n%s", code, errStr)
	}
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer l.Close()
	w, err := l.Get(standalone)
	if err != nil {
		t.Fatalf("get %s: %v", standalone, err)
	}
	if w.State != core.StateQueued || w.Session != nil {
		t.Errorf("%s = state %s session %v, want untouched", standalone, w.State, w.Session)
	}
}

// backdate sets a work item's last activity to age ago, as a ledger left alone
// that long would hold it.
func (f *cliFixture) backdate(t *testing.T, id string, age time.Duration) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	at := time.Now().Add(-age).UTC().Format("2006-01-02T15:04:05.000Z07:00")
	if _, err := db.Exec(`UPDATE work SET updated = ? WHERE id = ?`, at, id); err != nil {
		t.Fatalf("backdate %s: %v", id, err)
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
