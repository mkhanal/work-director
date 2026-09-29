package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/taste"
)

// Every requirement in cli.lazyspec.md is married to a test here. The schema
// tests marshal the domain types and assert the wire keys; the functional
// tests run the Go CLI over a seeded fixture and assert the --json schema,
// key values, error messages and exit codes.

func TestWorkItemsSerializeWithTheWireSchema(t *testing.T) {
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

func TestAnInstalledBinaryReadsTheRuleCardsItWasBuiltWith(t *testing.T) {
	f := newCLIFixture(t)
	standalone := f.ids["standalone"]
	installed := f.install(t, t.TempDir())
	outside := t.TempDir()
	env := withoutWDRoot(f.env(t, f.bin))
	tsTitles := checkoutTSCardTitles(t)
	code, out, errStr := f.runBinAt(t, installed, outside, env, "brief", standalone)
	if code != 0 {
		t.Fatalf("brief exited %d\n%s", code, errStr)
	}
	for _, title := range tsTitles {
		if !strings.Contains(out, title) {
			t.Fatalf("brief does not carry the built-in card %q:\n%s", title, out)
		}
	}
	for _, args := range [][]string{{"scan"}, {"spawn", standalone}} {
		if code, _, errStr := f.runBinAt(t, installed, outside, env, args...); code != 0 {
			t.Fatalf("wd %s exited %d\n%s", strings.Join(args, " "), code, errStr)
		}
	}
	code, _, errStr = f.runBinAt(t, installed, outside, env, "scan", "--adopt", "any-card")
	if code != 1 || !strings.Contains(errStr, "WD_ROOT") {
		t.Fatalf("scan --adopt exited %d, stderr %q; want 1 naming WD_ROOT", code, errStr)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("scan --adopt wrote into %s: %v %v", outside, entries, err)
	}
}

func TestCardsInACheckoutReplaceTheEmbeddedOnes(t *testing.T) {
	f := newCLIFixture(t)
	standalone := f.ids["standalone"]
	env := withoutWDRoot(f.env(t, f.bin))
	tsTitles := checkoutTSCardTitles(t)
	// checkoutWith writes a tree holding one lang:ts card titled title.
	checkoutWith := func(root, title string) {
		t.Helper()
		path := filepath.Join(root, "taste", "cards", "judgment", "checkout-rule.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		text := "---\nid: checkout-rule\ntitle: " + title + "\ncategory: judgment\nscope: [lang:ts]\nkind: practice\nstatus: adopted\nalways: false\nenforce: []\nevidence: []\n---\nKeep it in the checkout.\n"
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	briefCarries := func(bin, dir string, env []string, title string) {
		t.Helper()
		code, out, errStr := f.runBinAt(t, bin, dir, env, "brief", standalone)
		if code != 0 {
			t.Fatalf("brief exited %d\n%s", code, errStr)
		}
		if !strings.Contains(out, title) {
			t.Fatalf("brief does not carry the checkout's card %q:\n%s", title, out)
		}
		for _, embedded := range tsTitles {
			if strings.Contains(out, embedded) {
				t.Fatalf("brief carries the embedded card %q beside the checkout's:\n%s", embedded, out)
			}
		}
	}

	root := t.TempDir()
	checkoutWith(root, "Named by WD_ROOT")
	named := append(slices.Clone(env), "WD_ROOT="+root)
	briefCarries(f.goBin, t.TempDir(), named, "Named by WD_ROOT")
	checkoutWith(root, "Edited without a rebuild")
	briefCarries(f.goBin, t.TempDir(), named, "Edited without a rebuild")

	beside := t.TempDir()
	checkoutWith(beside, "Beside the binary")
	briefCarries(f.install(t, beside), t.TempDir(), env, "Beside the binary")

	workingDir := t.TempDir()
	checkoutWith(workingDir, "In the working directory")
	briefCarries(f.install(t, t.TempDir()), workingDir, env, "In the working directory")
}

// checkoutTSCardTitles returns the titles of this repository's adopted
// lang:ts cards, which reach a brief for the fixture's ts project.
func checkoutTSCardTitles(t *testing.T) []string {
	t.Helper()
	cards, err := taste.LoadCards(os.DirFS(filepath.Join(repoRoot(t), "taste", "cards")), "checkout")
	if err != nil {
		t.Fatalf("load checkout cards: %v", err)
	}
	var titles []string
	for _, c := range cards {
		if c.Status == taste.StatusAdopted && slices.Contains(c.Scope, taste.Scope("lang:ts")) {
			titles = append(titles, c.Title)
		}
	}
	if len(titles) == 0 {
		t.Fatal("the checkout holds no adopted lang:ts card")
	}
	return titles
}

// withoutWDRoot returns env with WD_ROOT removed.
func withoutWDRoot(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "WD_ROOT=") {
			out = append(out, kv)
		}
	}
	return out
}

// install copies the built binary alone into dir, as scripts/install.sh
// leaves it, and returns its path.
func (f *cliFixture) install(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(f.goBin)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	bin := filepath.Join(dir, "wd")
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatalf("install binary: %v", err)
	}
	return bin
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

// rowCount is the number of work, event, concern and worktree rows in the
// fixture's ledger.
func (f *cliFixture) rowCount(t *testing.T) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM work) + (SELECT COUNT(*) FROM event) +
		(SELECT COUNT(*) FROM concern) + (SELECT COUNT(*) FROM worktree)`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func TestADecisionIsRecordedInOneLineAndReachesTheBrief(t *testing.T) {
	f := newCLIFixture(t)
	id := f.ids["standalone"]

	out := f.runOK(t, "decide", id, "use", "sqlite", "everywhere", "--json")
	var decided core.Event
	if err := json.Unmarshal([]byte(out), &decided); err != nil {
		t.Fatalf("decide --json: %v\n%s", err, out)
	}
	if decided.Kind != core.EventDecision || decided.Body != "use sqlite everywhere" || decided.Work != id {
		t.Fatalf("decide recorded %+v, want the decision use sqlite everywhere on %s", decided, id)
	}
	if errStr := f.runFail(t, "decide", "nope", "x"); !strings.Contains(errStr, "no work nope") {
		t.Fatalf("decide on unknown work: stderr %q", errStr)
	}

	out = f.runOK(t, "concern", "add", id, "which queue?", "--json")
	f.runOK(t, "concern", "resolve", jsonNumber(t, out, "id"), "use the outbox table")
	f.runOK(t, "pr", id, "https://example.test/pr-leak")
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	others := map[core.EventKind]string{
		core.EventReport: "report-leak", core.EventSent: "sent-leak", core.EventNote: "note-leak",
		core.EventQuestion: "question-leak", core.EventAnswer: "answer-leak",
	}
	for kind, body := range others {
		if _, err := l.AddEvent(id, kind, body); err != nil {
			t.Fatalf("add %s: %v", kind, err)
		}
	}
	l.Close()

	brief := f.runOK(t, "brief", id)
	start := strings.Index(brief, "Decisions already made")
	if start < 0 {
		t.Fatalf("brief has no decisions section:\n%s", brief)
	}
	lines := strings.Split(brief[start:], "\n")
	end := 1
	for end < len(lines) && strings.HasPrefix(lines[end], "- ") {
		end++
	}
	section := strings.Join(lines[:end], "\n")
	for _, want := range []string{"- use sqlite everywhere", "- use the outbox table"} {
		if !strings.Contains(section, want) {
			t.Fatalf("decisions section missing %q:\n%s", want, section)
		}
	}
	for _, leak := range []string{"resolved:", "pr-leak", "report-leak", "sent-leak", "note-leak", "question-leak", "answer-leak"} {
		if strings.Contains(section, leak) {
			t.Fatalf("decisions section carries %q, which is no decision:\n%s", leak, section)
		}
	}
	if end != 3 {
		t.Fatalf("decisions section lists %d items, want the 2 decisions:\n%s", end-1, section)
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

	f.runOK(t, "projects", "add", "failing", f.sample, "--verify", "false", "--lazyspec", "n")
	id := jsonString(t, f.runOK(t, "add", "failing", "Checked", "--json"), "id")
	if errStr := f.runFail(t, "verify", id); !strings.Contains(errStr, "verify failed for "+id) {
		t.Fatalf("failing verify: stderr %q, want verify failed for %s", errStr, id)
	}
	var verifies []core.Event
	if err := json.Unmarshal([]byte(f.runOK(t, "events", id, "--json")), &verifies); err != nil {
		t.Fatalf("events --json: %v", err)
	}
	recorded := 0
	for _, e := range verifies {
		if e.Kind == core.EventVerify {
			recorded++
			if !strings.HasPrefix(e.Body, "fail\n") {
				t.Fatalf("verify event = %q, want the failure recorded", e.Body)
			}
		}
	}
	if recorded != 1 {
		t.Fatalf("verify events = %d, want the failed verify recorded once", recorded)
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

func TestEveryJsonCommandWritesOneDocument(t *testing.T) {
	f := newCLIFixture(t)
	epic, planEpic, t2, t3, standalone := f.ids["epic"], f.ids["planEpic"], f.ids["t2"], f.ids["t3"], f.ids["standalone"]
	waited := jsonString(t, f.runOK(t, "add", "sample-app", "Waited on", "--epic", epic, "--json"), "id")
	attached := jsonString(t, f.runOK(t, "add", "sample-app", "Attached", "--json"), "id")
	goal := jsonString(t, f.runOK(t, "goal", "add", "sample-app", "A goal", "--json"), "id")
	ready := jsonString(t, f.runOK(t, "add", "sample-app", "Ready", "--epic", epic, "--json"), "id")
	f.runOK(t, "set", ready, "running")
	f.runOK(t, "set", ready, "review")
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if _, err := l.AddEvent(ready, core.EventReport, "DONE\nSTATUS: DONE"); err != nil {
		t.Fatalf("report: %v", err)
	}
	l.Close()
	spec := filepath.Join(t.TempDir(), "added.toml")
	if err := os.WriteFile(spec, []byte("spawn = \"added run {brief}\"\nsession_id = 'session=(\\w+)'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oneDocument := func(args []string, out string) {
		t.Helper()
		dec := json.NewDecoder(strings.NewReader(out))
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Errorf("wd %s: %v in %q", strings.Join(args, " "), err, out)
			return
		}
		if dec.More() {
			t.Errorf("wd %s: more than one document in %q", strings.Join(args, " "), out)
		}
	}
	env := f.envWithPath(t, f.pathWith(t, "claude", "opencode", "codex", "planner"))
	covered := map[string]bool{}
	for _, args := range [][]string{
		{"projects"}, {"projects", "list"},
		{"projects", "add", "second", f.sample, "--lazyspec", "n"},
		{"projects", "add", "third", f.sample, "--lazyspec", "y"},
		{"models", "opencode"},
		{"runner", "list"}, {"runner", "init", "fresh"}, {"runner", "add", "added", spec},
		{"add", "sample-app", "Fresh task"},
		{"tasks", epic},
		{"brief", t3},
		{"spawn", standalone},
		{"report", standalone},
		{"verify", standalone},
		{"pr", standalone, "https://example.test/pr/1"},
		{"soft-done", ready},
		{"done", ready},
		{"epic", "plan", planEpic, "--runner", "planner"},
		{"epic", "spawn", planEpic, "--count", "2", "--runner", "claude,opencode"},
		{"epic", "run", epic, "--only", t3},
		{"epic", "run", epic, "--only", waited, "--wait", "--timeout", "1"},
		{"epic", "review", epic},
		{"epic", "status", epic},
		{"goal", "status", goal},
		{"send", t2, "hello"},
		{"attach", attached, "ses_outside"},
		{"decide", t2, "use", "sqlite"},
		{"set", attached, "blocked"},
		{"status"}, {"status", "--all"},
		{"context"}, {"context", "sample-app"}, {"context", t2},
		{"open", "README.md"}, {"open", t2, "README.md"},
		{"claim", t3, "me"}, {"claim", t3, "--drop"},
		{"impact", t3, "+src/x"}, {"impact", t3, "--clear"},
		{"conflict", epic},
		{"worktree", "list", epic}, {"worktree", "attach", t3, filepath.Join(f.dir, "elsewhere")},
		{"verify", t2}, {"merge", t2},
		{"concern", "add", t3, "a worry"}, {"concern", "list"}, {"concern", "list", epic},
		{"concern", "resolve", "2", "settled"},
		{"scan"},
		{"events", t2},
		{"feedback", "add", "a note"}, {"feedback"}, {"feedback", "list"},
		{"distill"},
		{"doctor"},
	} {
		covered[args[0]] = true
		args = append(args, "--json")
		code, out, errStr := f.runEnv(t, env, args...)
		if code != 0 {
			t.Errorf("wd %s exited %d: %s", strings.Join(args, " "), code, errStr)
			continue
		}
		oneDocument(args, out)
	}
	covered["serve"] = true
	oneDocument([]string{"serve", "--json"}, f.serveOutput(t, "--json"))
	covered["tui"] = true
	if code, out, errStr := f.run(t, "tui", "--json"); code != 1 || out != "" || !strings.Contains(errStr, "--json") {
		t.Errorf("wd tui --json: exit %d, stdout %q, stderr %q; want 1 refusing --json with nothing on stdout", code, out, errStr)
	}
	for cmd := range commands {
		if !covered[cmd] {
			t.Errorf("wd %s --json is not exercised", cmd)
		}
	}
}

// serveOutput runs wd serve on a free port until it has announced its
// address, then stops it and returns everything it wrote to stdout.
func (f *cliFixture) serveOutput(t *testing.T, args ...string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	cmd := exec.Command(f.goBin, append([]string{"serve", "--port", port}, args...)...)
	cmd.Env = f.env(t, f.bin)
	cmd.Dir = f.dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	var out []byte
	buf := make([]byte, 4096)
	for !strings.Contains(string(out), "127.0.0.1:"+port) {
		n, err := stdout.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			t.Fatalf("serve stopped before announcing its address: %v\n%s", err, out)
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("stop serve: %v", err)
	}
	rest, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("read serve output: %v", err)
	}
	cmd.Wait()
	return string(append(out, rest...))
}

func TestCommandsOnUnknownWorkFailNamingIt(t *testing.T) {
	f := newCLIFixture(t)
	before := f.rowCount(t)
	defer func() {
		if after := f.rowCount(t); after != before {
			t.Fatalf("ledger rows %d → %d, want nothing recorded", before, after)
		}
	}()
	for _, args := range [][]string{
		{"brief", "nope"}, {"spawn", "nope"}, {"send", "nope", "x"}, {"attach", "nope", "ses_x"},
		{"report", "nope"}, {"verify", "nope"}, {"decide", "nope", "x"}, {"pr", "nope", "http://x"},
		{"soft-done", "nope"}, {"set", "nope", "blocked"}, {"done", "nope"}, {"claim", "nope", "me"},
		{"impact", "nope", "+src"}, {"merge", "nope"}, {"events", "nope"}, {"tasks", "nope"},
		{"conflict", "nope"}, {"epic", "status", "nope"}, {"concern", "add", "nope", "x"},
		{"concern", "list", "nope"}, {"worktree", "attach", "nope", f.sample}, {"worktree", "list", "nope"},
		{"add", "sample-app", "x", "--epic", "nope"}, {"epic", "plan", "nope"}, {"epic", "spawn", "nope"},
		{"epic", "run", "nope"}, {"epic", "review", "nope"}, {"epic", "run", f.ids["epic"], "--only", "nope"},
		{"epic", "run", f.ids["epic"], "--only", f.ids["t3"] + ",nope"},
	} {
		if code, _, errStr := f.run(t, args...); code != 1 || !strings.Contains(errStr, "no work nope") {
			t.Errorf("wd %s: exit %d, stderr %q, want 1 and no work nope", strings.Join(args, " "), code, errStr)
		}
	}
}

func TestProjectsAddNeverWritesAFileItCannotParse(t *testing.T) {
	f := newCLIFixture(t)
	for i, c := range []struct {
		args []string
		want string
	}{
		{[]string{f.sample, "--mode", "bogus"}, "unknown mode bogus"},
		{[]string{f.sample, "--model", "m\nmode: ask"}, `--model "m\nmode: ask"`},
		{[]string{f.sample, "--model", "m\nno key here"}, `--model "m\nno key here"`},
		{[]string{f.sample, "--runner", " claude"}, `--runner " claude"`},
		{[]string{f.sample, "--stack", "go\nts"}, `--stack "go\nts"`},
		{[]string{f.sample, "--workflow", "/lazyspec\n---"}, `--workflow "/lazyspec\n---"`},
		{[]string{f.sample, "--verify", "go test\nverify: true"}, `--verify "go test\nverify: true"`},
		{[]string{f.sample + "\nmode: ask"}, `path "` + f.sample + `\nmode: ask"`},
	} {
		name := "held" + strconv.Itoa(i)
		code, _, errStr := f.run(t, append([]string{"projects", "add", name}, append(c.args, "--lazyspec", "n")...)...)
		if code != 1 || !strings.Contains(errStr, c.want) {
			t.Errorf("projects add %q: exit %d, stderr %q; want 1 naming %s", c.args, code, errStr, c.want)
		}
		if _, err := os.Stat(filepath.Join(f.wdHome, "projects", name+".md")); !os.IsNotExist(err) {
			t.Errorf("projects add %q: project file left behind: %v", c.args, err)
		}
	}
	f.runOK(t, "status")
}

func TestAttachRecordsTheRefAndCwdItIsGiven(t *testing.T) {
	f := newCLIFixture(t)
	t3 := f.ids["t3"]
	out := f.runOK(t, "attach", t3, "ses_ref", "--ref", "r1", "--cwd", f.sample, "--json")
	assertHasKey(t, out, `"ref": "r1"`)
	assertHasKey(t, out, `"cwd": "`+f.sample+`"`)
	out = f.runOK(t, "status", "--json")
	var rows []core.Work
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	for _, w := range rows {
		if w.ID == t3 && (strOrEmpty(w.Ref) != "r1" || strOrEmpty(w.Cwd) != f.sample || strOrEmpty(w.Session) != "ses_ref") {
			t.Fatalf("%s = ref %v cwd %v session %v, want r1, %s, ses_ref", t3, w.Ref, w.Cwd, w.Session, f.sample)
		}
	}
}

func TestSetCannotSkipTheSoftDoneGate(t *testing.T) {
	f := newCLIFixture(t)
	t2, t3 := f.ids["t2"], f.ids["t3"]
	f.runOK(t, "set", t2, "review")
	f.runOK(t, "set", t3, "running")
	f.runOK(t, "set", t3, "review")
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer l.Close()
	if _, err := l.AddEvent(t3, core.EventReport, "DONE\nSTATUS: DONE"); err != nil {
		t.Fatalf("report: %v", err)
	}

	errStr := f.runFail(t, "set", t2, "soft-done")
	if !strings.Contains(errStr, "not ready for soft-done") {
		t.Fatalf("stderr = %q, want the soft-done gate", errStr)
	}
	out := f.runOK(t, "set", t3, "soft-done", "--json")
	assertHasKey(t, out, `"state": "soft-done"`)
	for id, want := range map[string]core.State{t2: core.StateReview, t3: core.StateSoftDone} {
		w, err := l.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if w.State != want {
			t.Errorf("%s = %s, want %s", id, w.State, want)
		}
	}
}

func TestVerifyWithNoCommandsIsRefusedWithoutRecording(t *testing.T) {
	f := newCLIFixture(t)
	f.runOK(t, "projects", "add", "unverified", f.sample, "--lazyspec", "n")
	id := jsonString(t, f.runOK(t, "add", "unverified", "Unchecked", "--json"), "id")
	if errStr := f.runFail(t, "verify", id); !strings.Contains(errStr, "no verify commands") {
		t.Fatalf("stderr = %q, want no verify commands", errStr)
	}
	if out := f.runOK(t, "events", id, "--json"); strings.Contains(out, `"kind": "verify"`) {
		t.Fatalf("a verify event was recorded: %s", out)
	}
}

func TestAnAdoptedCardLeavesThePromotionCandidates(t *testing.T) {
	f := newCLIFixture(t)
	root := t.TempDir()
	card := filepath.Join(root, "taste", "cards", "judgment", "proj-rule.md")
	if err := os.MkdirAll(filepath.Dir(card), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\nid: proj-rule\ntitle: A rule\ncategory: judgment\nscope: [project:sample-app]\nkind: practice\nstatus: adopted\nalways: false\nenforce: []\nevidence: []\n---\nKeep it small.\n"
	if err := os.WriteFile(card, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	env := f.env(t, f.bin)
	for i, kv := range env {
		if strings.HasPrefix(kv, "WD_ROOT=") {
			env[i] = "WD_ROOT=" + root
		}
	}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		return f.runEnv(t, env, args...)
	}
	promoted := func() []string {
		t.Helper()
		code, out, errStr := run("scan", "--json")
		if code != 0 {
			t.Fatalf("scan exited %d: %s", code, errStr)
		}
		var res struct {
			Promotion []struct {
				Card struct {
					ID string `json:"id"`
				} `json:"card"`
			} `json:"promotion"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("scan --json: %v\n%s", err, out)
		}
		var ids []string
		for _, p := range res.Promotion {
			ids = append(ids, p.Card.ID)
		}
		return ids
	}
	if ids := promoted(); len(ids) != 1 || ids[0] != "proj-rule" {
		t.Fatalf("promotion candidates = %v, want [proj-rule]", ids)
	}
	if code, _, errStr := run("scan", "--adopt", "proj-rule"); code != 0 {
		t.Fatalf("adopt exited %d: %s", code, errStr)
	}
	global := filepath.Join(root, "taste", "cards", "judgment", "proj-rule-global.md")
	written, err := os.ReadFile(global)
	if err != nil {
		t.Fatalf("global candidate: %v", err)
	}
	if ids := promoted(); len(ids) != 0 {
		t.Fatalf("promotion candidates after adopting = %v, want none", ids)
	}
	code, _, errStr := run("scan", "--adopt", "proj-rule")
	if code != 1 || !strings.Contains(errStr, "no promotion candidate proj-rule") {
		t.Fatalf("second adopt: exit %d, stderr %q; want 1 with no promotion candidate", code, errStr)
	}
	if again, err := os.ReadFile(global); err != nil || string(again) != string(written) {
		t.Fatalf("global candidate changed by the second adopt: %v", err)
	}
}

func TestEpicRunSpawnsOnlyChildrenNotYetUnderWay(t *testing.T) {
	f := newCLIFixture(t)
	epic, t2, t3 := f.ids["epic"], f.ids["t2"], f.ids["t3"]
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer l.Close()
	child := func(parent, title string, path ...core.State) string {
		t.Helper()
		w, err := l.Add("sample-app", title, ledger.AddOptions{Parent: &parent})
		if err != nil {
			t.Fatalf("add %s: %v", title, err)
		}
		for _, s := range path {
			if s == core.StateSoftDone {
				if _, err := l.AddEvent(w.ID, core.EventReport, "DONE"); err != nil {
					t.Fatalf("report %s: %v", title, err)
				}
				_, err = l.SoftDone(w.ID, false)
			} else {
				_, err = l.Transition(w.ID, s)
			}
			if err != nil {
				t.Fatalf("%s → %s: %v", title, s, err)
			}
		}
		return w.ID
	}
	claimed := child(epic, "Claimed by an outside session", core.StateRunning)
	if _, err := l.SetClaim(claimed, strPtr("ses_outside")); err != nil {
		t.Fatalf("claim: %v", err)
	}
	underWay := []string{
		t2,
		claimed,
		child(epic, "In review", core.StateRunning, core.StateReview),
		child(epic, "Waiting on a human", core.StateRunning, core.StateNeedsInput),
		child(epic, "Blocked", core.StateBlocked),
		child(epic, "Soft done", core.StateRunning, core.StateReview, core.StateSoftDone),
	}
	briefed := child(epic, "Briefed", core.StateBriefed)
	bare := child(epic, "Running with no session or claim", core.StateRunning)
	before := map[string]core.Work{}
	for _, id := range underWay {
		if before[id], err = l.Get(id); err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
	}

	out := f.runOK(t, "epic", "run", epic)
	if !strings.Contains(out, "3 task(s) spawned") {
		t.Fatalf("epic run = %q, want 3 task(s) spawned", out)
	}
	for _, id := range underWay {
		w, err := l.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		was := before[id]
		if w.State != was.State || strOrEmpty(w.Session) != strOrEmpty(was.Session) || strOrEmpty(w.Claim) != strOrEmpty(was.Claim) {
			t.Errorf("%s = %s session %v claim %v, want %s session %v claim %v untouched", id, w.State, w.Session, w.Claim, was.State, was.Session, was.Claim)
		}
	}
	for _, id := range []string{t3, briefed, bare} {
		if w, err := l.Get(id); err != nil || w.State != core.StateRunning || w.Session == nil {
			t.Errorf("%s = %+v, %v; want running with a session", id, w, err)
		}
	}

	idle, err := l.Add("sample-app", "Nothing to start", ledger.AddOptions{Kind: core.WorkEpic})
	if err != nil {
		t.Fatalf("add epic: %v", err)
	}
	child(idle.ID, "Blocked alone", core.StateBlocked)
	out = f.runOK(t, "epic", "run", idle.ID, "--json")
	if !strings.Contains(out, `"spawned": []`) {
		t.Fatalf("epic run = %s, want nothing spawned", out)
	}
	if w, err := l.Get(idle.ID); err != nil || w.State != core.StateQueued {
		t.Fatalf("epic = %+v, %v; want it left queued", w, err)
	}
}

func strPtr(s string) *string { return &s }

func TestSendAndReportReachAChildWhereItsEpicsPassDoes(t *testing.T) {
	f := newCLIFixture(t)
	bin := t.TempDir()
	recorder := "#!/usr/bin/env bash\nmkdir -p \"$WD_FAKE_STATE\"\n" +
		"printf '%s|%s|%s|%s\\n' \"$1\" \"$2\" \"$3\" \"$PWD\" >> \"$WD_FAKE_STATE/recorder.log\"\n" +
		"case \"$2\" in status) echo 'state: waiting';; export) echo 'STATUS: DONE';; *) echo ok;; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "recorder"), []byte(recorder), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rec-epic", "rec-project"} {
		spec := fmt.Sprintf("name = %[1]q\nspawn = \"recorder %[1]s run {brief}\"\nsession_id = 'session=(\\S+)'\n"+
			"send = \"recorder %[1]s send {session} {text}\"\nstatus = \"recorder %[1]s status {session}\"\n"+
			"running = 'state: *running'\nwaiting = 'state: *waiting'\nexited = 'state: *exited'\n"+
			"transcript = \"recorder %[1]s export {session}\"\nattach = \"recorder %[1]s attach {session}\"\n", name)
		if err := os.WriteFile(filepath.Join(f.wdHome, "runners", name+".toml"), []byte(spec), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := f.env(t, bin+string(os.PathListSeparator)+f.bin)
	run := func(args ...string) string {
		t.Helper()
		code, out, errStr := f.runEnv(t, env, args...)
		if code != 0 {
			t.Fatalf("wd %s exited %d: %s", strings.Join(args, " "), code, errStr)
		}
		return out
	}
	run("projects", "add", "recorded", f.sample, "--runner", "rec-project", "--lazyspec", "n")
	child := func(epic, claim string) string {
		t.Helper()
		id := jsonString(t, run("add", "recorded", "Child of "+epic, "--epic", epic, "--json"), "id")
		run("claim", id, claim)
		run("set", id, "running")
		return id
	}
	bare := jsonString(t, run("add", "recorded", "Epic of its project", "--kind", "epic", "--json"), "id")
	own := jsonString(t, run("add", "recorded", "Epic with its own runner", "--kind", "epic", "--json"), "id")
	run("attach", own, "ses_epic", "--runner", "rec-epic")
	shared := filepath.Join(f.dir, "shared")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if _, err := l.AddWorktree(own, ledger.WorktreeInfo{Path: shared, Kind: core.WorktreeShared}); err != nil {
		t.Fatalf("shared worktree: %v", err)
	}
	l.Close()

	for _, c := range []struct {
		epic, claim, runner, dir string
	}{
		{bare, "ses_bare", "rec-project", f.sample},
		{own, "ses_own", "rec-epic", shared},
	} {
		id := child(c.epic, c.claim)
		log := filepath.Join(f.dir, "fake-state", "recorder.log")
		reached := func(what string, args ...string) {
			t.Helper()
			if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			run(args...)
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatalf("%s: nothing reached a runner: %v", what, err)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				parts := strings.Split(line, "|")
				if len(parts) != 4 || parts[0] != c.runner || parts[2] != c.claim || parts[3] != c.dir {
					t.Errorf("%s reached %q, want %s %s in %s", what, line, c.runner, c.claim, c.dir)
				}
			}
		}
		reached("wd send", "send", id, "hello")
		reached("wd epic review", "epic", "review", c.epic)
		reached("wd report", "report", id)
	}
}

func TestAClaimNamesSomeone(t *testing.T) {
	f := newCLIFixture(t)
	id := jsonString(t, f.runOK(t, "add", "sample-app", "Unclaimed", "--json"), "id")
	for _, args := range [][]string{{"claim", id}, {"claim", id, ""}} {
		if errStr := f.runFail(t, args...); !strings.Contains(errStr, "usage: wd claim") {
			t.Errorf("wd %q: stderr %q, want claim usage", args, errStr)
		}
	}
	t2 := f.ids["t2"]
	f.runFail(t, "claim", t2, "")
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer l.Close()
	for id, want := range map[string]string{id: "", t2: "ses_y"} {
		w, err := l.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if strOrEmpty(w.Claim) != want || (want == "" && w.Claim != nil) {
			t.Errorf("%s claim = %v, want it left as %q", id, w.Claim, want)
		}
	}
}

func TestFlagsThatDoNotParseFail(t *testing.T) {
	f := newCLIFixture(t)
	epic, t2, t3 := f.ids["epic"], f.ids["t2"], f.ids["t3"]
	for _, c := range []struct {
		args []string
		flag string
	}{
		{[]string{"report", t2, "--tail", "x"}, "--tail"},
		{[]string{"report", t2, "--tail="}, "--tail"},
		{[]string{"report", t2, "--tail"}, "--tail"},
		{[]string{"attach", t3, "ses_x", "--ref"}, "--ref"},
		{[]string{"attach", t3, "ses_x", "--ref", "--cwd", f.sample}, "--ref"},
		{[]string{"attach", t3, "ses_x", "--ref", "--json"}, "--ref"},
		{[]string{"attach", t3, "ses_x", "--cwd", "--json"}, "--cwd"},
		{[]string{"status", "--json=x"}, "--json"},
		{[]string{"epic", "run", epic, "--wait", "--timeout", "30m"}, "--timeout"},
		{[]string{"epic", "spawn", f.ids["planEpic"], "--count", "0"}, "--count"},
		{[]string{"serve", "--port", "http"}, "--port"},
	} {
		if errStr := f.runFail(t, c.args...); !strings.Contains(errStr, c.flag) {
			t.Errorf("wd %s: stderr %q, want %s named", strings.Join(c.args, " "), errStr, c.flag)
		}
	}
}

func TestReportWithoutAStatusLineFilesNothingAndFails(t *testing.T) {
	f := newCLIFixture(t)
	bin := t.TempDir()
	speaker := "#!/usr/bin/env bash\ncase \"$1\" in status) echo 'state: running';; export) cat \"$WD_FAKE_STATE/said-$2\";; *) echo ok;; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "speaker"), []byte(speaker), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "name = \"speaker\"\nspawn = \"speaker run {brief}\"\nsession_id = 'session=(\\S+)'\n" +
		"send = \"speaker send {session} {text}\"\nstatus = \"speaker status {session}\"\n" +
		"running = 'state: *running'\nwaiting = 'state: *waiting'\nexited = 'state: *exited'\n" +
		"transcript = \"speaker export {session}\"\nattach = \"speaker attach {session}\"\n"
	if err := os.WriteFile(filepath.Join(f.wdHome, "runners", "speaker.toml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(f.dir, "fake-state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	env := f.env(t, bin+string(os.PathListSeparator)+f.bin)
	attached := func(title, session, text string) string {
		t.Helper()
		code, out, errStr := f.runEnv(t, env, "add", "sample-app", title, "--json")
		if code != 0 {
			t.Fatalf("add: %s", errStr)
		}
		id := jsonString(t, out, "id")
		if code, _, errStr := f.runEnv(t, env, "attach", id, session, "--runner", "speaker"); code != 0 {
			t.Fatalf("attach: %s", errStr)
		}
		if err := os.WriteFile(filepath.Join(state, "said-"+session), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return id
	}
	quiet := attached("Still working", "ses_quiet", "PLAN: reading the code")
	asking := attached("Needs a fact", "ses_asking", "STATUS: NEEDS-INPUT which database?")

	code, _, errStr := f.runEnv(t, env, "report", quiet)
	if code != 1 || !strings.Contains(errStr, "no STATUS line") || !strings.Contains(errStr, "executor") {
		t.Fatalf("report without a status line: exit %d, stderr %q; want 1 saying the executor's report has not arrived", code, errStr)
	}
	if code, _, errStr := f.runEnv(t, env, "report", asking); code != 0 {
		t.Fatalf("report with NEEDS-INPUT: exit %d, stderr %q; want 0", code, errStr)
	}

	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	defer l.Close()
	report := core.EventReport
	for id, want := range map[string]struct {
		reports int
		state   core.State
	}{quiet: {0, core.StateRunning}, asking: {1, core.StateNeedsInput}} {
		events, err := l.Events(id, &report)
		if err != nil {
			t.Fatalf("events: %v", err)
		}
		w, err := l.Get(id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(events) != want.reports || w.State != want.state {
			t.Errorf("%s: %d reports in state %s, want %d in %s", id, len(events), w.State, want.reports, want.state)
		}
	}
}
