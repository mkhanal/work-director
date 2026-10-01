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
		"ref", "cwd", "created", "updated", "parent", "heading", "claim", "impact", "goal_type",
	})
	// Nullable fields are null, not omitted, when unset. A goal with no type
	// has goal_type null: never a defaulted value, because a type that reads as
	// right and is wrong is worse than no type.
	empty := core.Work{ID: "x", Project: "p", Title: "t", Kind: core.WorkTask, State: core.StateQueued, Created: "c", Updated: "u"}
	assertKeys(t, empty, []string{
		"id", "project", "title", "detail", "kind", "state", "runner", "session",
		"ref", "cwd", "created", "updated", "parent", "heading", "claim", "impact", "goal_type",
	})
}

func TestLedgerObjectsKeepTheirColumnNames(t *testing.T) {
	// An event carries the moment it was recorded, the moment it took effect
	// when the two differ, and a structured claim when there is one. The last
	// two are null rather than absent, so a reader can tell "not separated"
	// from "recorded the same way".
	assertKeys(t, core.Event{ID: 1, Work: "w", Kind: core.EventReport, Body: "b", At: "a"},
		[]string{"id", "work", "kind", "body", "at", "effective", "decision"})
	// The claim's own shape is the shape a review reads.
	assertKeys(t, core.Decision{Question: "q", Answer: "a"},
		[]string{"question", "answer", "source", "runner", "model", "tokens", "reverses", "reversed_by"})
	// Feedback carries the work the evidence came from, under the column's own
	// name like every other ledger object: evidence that cannot be pointed back
	// at the thing that produced it cannot be audited against it, and the
	// column is called work.
	assertKeys(t, core.Feedback{ID: 1, Text: "t", Project: strPtr("p"), Card: strPtr("c"), Source: core.FeedbackDirector, Work: strPtr("w"), At: "a"},
		[]string{"id", "text", "project", "card", "source", "work", "at"})
	// resolved_at keeps the ledger's snake_case column name.
	assertKeys(t, core.Concern{ID: 1, Work: "w", Text: "t", Resolved: 1, Decision: strPtr("d"), At: "a", ResolvedAt: strPtr("ra")},
		[]string{"id", "work", "text", "resolved", "decision", "at", "resolved_at"})
	assertKeys(t, core.Worktree{ID: 1, Work: "w", Path: "/p", Branch: strPtr("b"), Kind: core.WorktreeShared, State: core.WorktreeActive, Origin: core.OriginDirector, Created: "c"},
		[]string{"id", "work", "path", "branch", "kind", "state", "origin", "created"})
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
	assertHasKey(t, out, `"kind": "goal"`)
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
			"ref", "cwd", "created", "updated", "parent", "heading", "claim", "impact", "goal_type", "stale",
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
	// A direct close skips the gate on purpose, so it is refused without a
	// reason and the message names the flag that makes it legitimate.
	for _, id := range []string{standalone, t3, planEpic} {
		code, _, errStr := f.run(t, "done", id)
		if code != 1 || !strings.Contains(errStr, `--cancelled`) {
			t.Fatalf("done on %s without a reason exited %d: %q", id, code, errStr)
		}
	}
	for _, id := range []string{standalone, t3, planEpic} {
		out := f.runOK(t, "done", id, "--cancelled", "superseded by the new plan", "--json")
		assertHasKey(t, out, `"state": "done"`)
		// The reason is what later tells a cancellation from a completion.
		ev := jsonEvents(t, f, id)
		var found bool
		for _, e := range ev {
			if e.Kind == core.EventDecision && strings.Contains(e.Body, "superseded by the new plan") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s has no decision recording the cancellation: %+v", id, ev)
		}
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
		{[]string{"tasks", t1}, "is not a goal"},
		{[]string{"conflict", t1}, "is not a goal"},
		{[]string{"set", t1, "bogus"}, "unknown state bogus"},
		{[]string{"set", t1, "done"}, "illegal transition"},
		{[]string{"models", "bogus"}, "unknown runner bogus"},
		{[]string{"impact", t1}, "usage: wd impact"},
		{[]string{"merge", f.ids["standalone"]}, "merge is only for tasks under a goal"},
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

// jsonEvents reads `wd events <id> --json` back as the events it reported.
func jsonEvents(t *testing.T, f *cliFixture, id string) []core.Event {
	t.Helper()
	var ev []core.Event
	if err := json.Unmarshal([]byte(f.runOK(t, "events", id, "--json")), &ev); err != nil {
		t.Fatalf("unmarshal events for %s: %v", id, err)
	}
	return ev
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
	// Abandonment is a one-way door, so it gets work of its own rather than
	// taking one the rest of the census still needs.
	unshipped := jsonString(t, f.runOK(t, "add", "sample-app", "Never shipped", "--json"), "id")
	// Reopening needs finished work, so this one closes through the gates first.
	// Abandonment and re-opening are both one-way enough that neither borrows a
	// row the rest of the census still needs.
	finished := jsonString(t, f.runOK(t, "add", "sample-app", "Finished, then reopened", "--json"), "id")
	f.runOK(t, "set", finished, "running")
	f.runOK(t, "set", finished, "review")
	f.runOK(t, "report", finished, "DONE\nSTATUS: DONE")
	f.runOK(t, "verify", finished)
	f.runOK(t, "pr", finished, "https://example.test/pr/finished")
	f.runOK(t, "soft-done", finished)
	f.runOK(t, "done", finished)
	finishedGoal := jsonString(t, f.runOK(t, "goal", "add", "sample-app", "Finished goal, then reopened", "--json"), "id")
	f.runOK(t, "set", finishedGoal, "running")
	f.runOK(t, "set", finishedGoal, "review")
	f.runOK(t, "report", finishedGoal, "DONE\nSTATUS: DONE")
	f.runOK(t, "verify", finishedGoal)
	f.runOK(t, "pr", finishedGoal, "https://example.test/pr/finished-goal")
	f.runOK(t, "soft-done", finishedGoal)
	f.runOK(t, "done", finishedGoal)
	driveGoal := jsonString(t, f.runOK(t, "goal", "add", "sample-app", "Never driven", "--json"), "id")
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
		{"roadmap"},
		{"drive", driveGoal},
		{"abandon", unshipped},
		{"reopen", finished, "a second half nobody had asked for"},
		{"goal", "reopen", finishedGoal, "and again, through the goal spelling"},
		{"review", "--project", "sample-app"}, {"review", "--ack", "--project", "sample-app"},
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

func TestGoalRunSpawnsOnlyChildrenNotYetUnderWay(t *testing.T) {
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

func TestSendAndReportReachAChildWhereItsGoalsPassDoes(t *testing.T) {
	f := newCLIFixture(t)
	bin := t.TempDir()
	// One call per line, four fields: the third is flattened so a multi-line
	// brief cannot break the record apart.
	recorder := "#!/usr/bin/env bash\nmkdir -p \"$WD_FAKE_STATE\"\n" +
		"printf '%s|%s|%s|%s\\n' \"$1\" \"$2\" \"$(printf '%s' \"$3\" | tr '\\n| ' '.')\" \"$PWD\" >> \"$WD_FAKE_STATE/recorder.log\"\n" +
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
	if _, err := l.AddWorktree(own, ledger.WorktreeInfo{Path: shared, Kind: core.WorktreeShared, Origin: core.OriginDirector}); err != nil {
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
				if len(parts) != 4 || parts[0] != c.runner || parts[3] != c.dir {
					t.Errorf("%s reached %q, want the %s runner in %s", what, line, c.runner, c.dir)
					continue
				}
				// Every call runs where the work runs. The calls that
				// coordinate with the work address its claim; the reflection
				// call is a separate bounded session of its own, spawned in
				// the same runner and directory.
				if parts[1] == "run" {
					continue
				}
				if parts[2] != c.claim {
					t.Errorf("%s reached %q, want session %s", what, line, c.claim)
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

// transcriptSay appends an assistant entry to the fake claude session's
// transcript, so it becomes the entry wd report reads last.
func (f *cliFixture) transcriptSay(t *testing.T, session, text string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(f.home, ".claude", "projects", "*", session+".jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("transcript for %s: %v %v", session, files, err)
	}
	entry, err := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}}}})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(append(entry, '\n')); err != nil {
		t.Fatal(err)
	}
}

// eventsOf counts work's events of kind through wd events.
func (f *cliFixture) eventsOf(t *testing.T, id string, kind core.EventKind) int {
	t.Helper()
	var evs []core.Event
	if err := json.Unmarshal([]byte(f.runOK(t, "events", id, "--json")), &evs); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// bodies is every event of one kind on work, in the order they were filed.
func (f *cliFixture) bodies(t *testing.T, id string, kind core.EventKind) []string {
	t.Helper()
	var evs []core.Event
	if err := json.Unmarshal([]byte(f.runOK(t, "events", id, "--json")), &evs); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e.Body)
		}
	}
	return out
}

// events is every event on work, in the order they were filed.
func (f *cliFixture) events(t *testing.T, id string) []core.Event {
	t.Helper()
	var evs []core.Event
	if err := json.Unmarshal([]byte(f.runOK(t, "events", id, "--json")), &evs); err != nil {
		t.Fatal(err)
	}
	return evs
}

// stateOf is work's state through wd status.
func (f *cliFixture) stateOf(t *testing.T, id string) core.State {
	t.Helper()
	var ws []core.Work
	if err := json.Unmarshal([]byte(f.runOK(t, "status", "--all", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	for _, w := range ws {
		if w.ID == id {
			return w.State
		}
	}
	t.Fatalf("no work %s in status", id)
	return ""
}

func TestAStandaloneTaskClosesThroughTheCLIAlone(t *testing.T) {
	f := newCLIFixture(t)
	id := jsonString(t, f.runOK(t, "add", "sample-app", "Close me standalone", "--json"), "id")
	f.runOK(t, "spawn", id, "--runner", "claude")
	if got := jsonString(t, f.runOK(t, "report", id, "--json"), "report"); got != "DONE" {
		t.Fatalf("report = %q, want DONE", got)
	}
	f.runOK(t, "verify", id)
	f.runOK(t, "pr", id, "https://example.test/pr/standalone")
	f.runOK(t, "soft-done", id)
	if got := jsonString(t, f.runOK(t, "done", id, "--json"), "state"); got != string(core.StateDone) {
		t.Fatalf("state = %q, want done", got)
	}
	if got := f.eventsOf(t, id, core.EventReport); got != 1 {
		t.Errorf("%d report events, want 1", got)
	}

	unspawned := jsonString(t, f.runOK(t, "add", "sample-app", "Never spawned", "--json"), "id")
	if errStr := f.runFail(t, "report", unspawned); !strings.Contains(errStr, "no session") {
		t.Errorf("report with no session: stderr %q, want no session named", errStr)
	}
	if got := f.eventsOf(t, unspawned, core.EventReport); got != 0 {
		t.Errorf("report with no session filed %d reports, want 0", got)
	}
	if got := f.stateOf(t, unspawned); got != core.StateQueued {
		t.Errorf("report with no session left state %s, want queued", got)
	}

	for _, status := range []string{"BLOCKED", "NEEDS-INPUT"} {
		id := jsonString(t, f.runOK(t, "add", "sample-app", "Reports "+status, "--json"), "id")
		session := jsonString(t, f.runOK(t, "spawn", id, "--runner", "claude", "--json"), "session")
		f.transcriptSay(t, session, "STATUS: "+status+"\nNOTES: not finished")
		f.runOK(t, "report", id)
		f.runOK(t, "set", id, "running")
		f.runOK(t, "set", id, "review")
		f.runOK(t, "verify", id)
		f.runOK(t, "pr", id, "https://example.test/pr/"+status)
		if errStr := f.runFail(t, "soft-done", id); !strings.Contains(errStr, "DONE report") {
			t.Errorf("soft-done after a %s report: stderr %q, want DONE report named", status, errStr)
		}
		if got := f.stateOf(t, id); got != core.StateReview {
			t.Errorf("soft-done after a %s report left state %s, want review", status, got)
		}
	}
}

func TestReportFilesWhatTheCoordinatorWould(t *testing.T) {
	f := newCLIFixture(t)
	spawn := func(title string, add ...string) (string, string) {
		t.Helper()
		id := jsonString(t, f.runOK(t, append([]string{"add", "sample-app", title, "--json"}, add...)...), "id")
		return id, jsonString(t, f.runOK(t, "spawn", id, "--runner", "claude", "--json"), "session")
	}
	expect := func(id string, want core.State, reports int) {
		t.Helper()
		if got := f.stateOf(t, id); got != want {
			t.Errorf("%s state = %s, want %s", id, got, want)
		}
		if got := f.eventsOf(t, id, core.EventReport); got != reports {
			t.Errorf("%s has %d reports, want %d", id, got, reports)
		}
	}

	asking, session := spawn("Needs a judgment")
	f.transcriptSay(t, session, "STATUS: NEEDS-INPUT\nNOTES: pick a vendor")
	f.runOK(t, "report", asking)
	f.runOK(t, "report", asking)
	expect(asking, core.StateNeedsInput, 1)

	f.transcriptSay(t, session, "STATUS: DONE\nFILES: none")
	f.runOK(t, "report", asking)
	f.runOK(t, "report", asking)
	expect(asking, core.StateReview, 2)

	blocked, session := spawn("Blocked on access")
	f.transcriptSay(t, session, "STATUS: BLOCKED\nNOTES: no credentials")
	f.runOK(t, "report", blocked)
	f.runOK(t, "report", blocked)
	expect(blocked, core.StateBlocked, 1)

	child, session := spawn("A child of the epic", "--epic", f.ids["epic"])
	f.transcriptSay(t, session, "STATUS: NEEDS-INPUT\nNOTES: which schema?")
	f.runOK(t, "report", child)
	f.runOK(t, "report", child)
	expect(child, core.StateNeedsInput, 1)
}

// worktreeFixture is the fixture's epic shared and t2 private worktrees,
// both made by wd, with t2 blocked so a human can close it.
type worktreeFixture struct {
	*cliFixture
	epic, t2         string
	shared, private  string
	sharedBr, privBr string
}

func newWorktreeFixture(t *testing.T) *worktreeFixture {
	t.Helper()
	f := newCLIFixture(t)
	epic, t2 := f.ids["epic"], f.ids["t2"]
	f.runOK(t, "set", t2, "blocked")
	return &worktreeFixture{
		cliFixture: f,
		epic:       epic,
		t2:         t2,
		shared:     filepath.Join(f.dir, "worktrees", "sample-app-epic-"+epic),
		private:    filepath.Join(f.dir, "worktrees", "sample-app-"+t2),
		sharedBr:   "wd-" + epic,
		privBr:     "wd-" + t2,
	}
}

// commit writes name with body in dir and commits it.
func commit(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	gitRun(t, dir, "add", name)
	gitRun(t, dir, "-c", "user.email=fixture@work-director", "-c", "user.name=Fixture", "commit", "-qm", "add "+name)
}

// worktreeGone asserts path is off disk and branch deleted from repo.
func worktreeGone(t *testing.T, repo, path, branch string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree %s still on disk: %v", path, err)
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run(); err == nil {
		t.Fatalf("branch %s still exists", branch)
	}
}

// worktreeStays asserts path is on disk and branch still in repo.
func worktreeStays(t *testing.T, repo, path, branch string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree %s gone: %v", path, err)
	}
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).CombinedOutput(); err != nil {
		t.Fatalf("branch %s gone: %v %s", branch, err, out)
	}
}

// worktreeStates is the state of each of work's worktrees, by path.
func (f *cliFixture) worktreeStates(t *testing.T, work string) map[string]core.WorktreeState {
	t.Helper()
	var wts []core.Worktree
	if err := json.Unmarshal([]byte(f.runOK(t, "worktree", "list", work, "--json")), &wts); err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	out := map[string]core.WorktreeState{}
	for _, wt := range wts {
		out[wt.Path] = wt.State
	}
	return out
}

// work reads one work row.
func (f *cliFixture) workRow(t *testing.T, id string) core.Work {
	t.Helper()
	w, err := f.work(t, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return w
}

// openConcerns is the text of work's own unresolved concerns, not its tasks'.
func (f *cliFixture) openConcerns(t *testing.T, work string) []string {
	t.Helper()
	var cs []core.Concern
	if err := json.Unmarshal([]byte(f.runOK(t, "concern", "list", work, "--json")), &cs); err != nil {
		t.Fatalf("concern list: %v", err)
	}
	var out []string
	for _, c := range cs {
		if c.Work == work && c.Resolved == 0 {
			out = append(out, c.Text)
		}
	}
	return out
}

func TestDoneRemovesAWorktreeWdMadeOnceItsBranchHasLanded(t *testing.T) {
	f := newWorktreeFixture(t)

	// The task's work merged into the epic's shared branch.
	commit(t, f.private, "dash.txt", "dashboards\n")
	gitRun(t, f.shared, "merge", "--no-edit", "-q", f.privBr)
	if out := f.runOK(t, "set", f.t2, "done", "--cancelled", "the work landed, so the task is closed"); !strings.Contains(out, "removed worktree "+f.private) {
		t.Fatalf("set done said %q, want the removed worktree", out)
	}
	worktreeGone(t, f.sample, f.private, f.privBr)
	if got := f.worktreeStates(t, f.t2)[f.private]; got != core.WorktreeRemoved {
		t.Fatalf("private worktree state = %q, want removed", got)
	}

	// The epic's pull request squash-merged upstream; the local main is stale.
	remote := filepath.Join(f.dir, "remote.git")
	gitRun(t, f.dir, "clone", "-q", "--bare", f.sample, remote)
	gitRun(t, f.sample, "remote", "add", "origin", remote)
	gitRun(t, f.sample, "fetch", "-q", "origin")
	gitRun(t, f.sample, "branch", "-q", "--set-upstream-to=origin/main", "main")
	clone := filepath.Join(f.dir, "reviewer")
	gitRun(t, f.dir, "clone", "-q", remote, clone)
	commit(t, clone, "dash.txt", "dashboards\n")
	gitRun(t, clone, "push", "-q", "origin", "main")

	out := f.runOK(t, "done", f.epic, "--cancelled", "the goal's pull request landed upstream", "--json")
	assertHasKey(t, out, `"state": "done"`)
	worktreeGone(t, f.sample, f.shared, f.sharedBr)
	if got := f.worktreeStates(t, f.epic)[f.shared]; got != core.WorktreeRemoved {
		t.Fatalf("shared worktree state = %q, want removed", got)
	}
	if cs := f.openConcerns(t, f.epic); len(cs) != 0 {
		t.Fatalf("concerns = %v, want none", cs)
	}
}

func TestDoneKeepsAWorktreeThatWouldLoseWork(t *testing.T) {
	f := newWorktreeFixture(t)

	commit(t, f.private, "ingest.txt", "unmerged\n")
	if err := os.WriteFile(filepath.Join(f.private, "draft.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.runOK(t, "done", f.t2, "--cancelled", "abandoned with an unmerged draft")
	worktreeStays(t, f.sample, f.private, f.privBr)
	if got := f.worktreeStates(t, f.t2)[f.private]; got != core.WorktreeActive {
		t.Fatalf("private worktree state = %q, want active", got)
	}
	cs := f.openConcerns(t, f.t2)
	if len(cs) != 1 {
		t.Fatalf("concerns = %v, want one", cs)
	}
	for _, want := range []string{f.private, "it has local changes", "holds work not landed in " + f.sharedBr, "wd worktree remove " + f.t2} {
		if !strings.Contains(cs[0], want) {
			t.Fatalf("concern %q does not name %q", cs[0], want)
		}
	}

	gitRun(t, f.sample, "worktree", "lock", "--reason", "on a removable drive", f.shared)
	f.runOK(t, "done", f.epic, "--cancelled", "abandoning the goal with the shared worktree locked")
	worktreeStays(t, f.sample, f.shared, f.sharedBr)
	if cs := f.openConcerns(t, f.epic); len(cs) != 1 || !strings.Contains(cs[0], "it is locked: on a removable drive") {
		t.Fatalf("concerns = %v, want one naming the lock", cs)
	}
}

func TestDoneLeavesWorktreesWdDidNotMake(t *testing.T) {
	f := newCLIFixture(t)
	standalone := f.ids["standalone"]
	path := filepath.Join(f.dir, "own-worktree")
	gitRun(t, f.sample, "worktree", "add", "-q", "-b", "own", path)
	f.runOK(t, "worktree", "attach", standalone, path, "--branch", "own")
	f.runOK(t, "done", standalone, "--cancelled", "closed with a worktree wd did not make")
	worktreeStays(t, f.sample, path, "own")
	if got := f.worktreeStates(t, standalone)[path]; got != core.WorktreeActive {
		t.Fatalf("attached worktree state = %q, want active", got)
	}
	if cs := f.openConcerns(t, standalone); len(cs) != 0 {
		t.Fatalf("concerns = %v, want none", cs)
	}
}

func TestWorktreeRemoveRetriesTheWorktreesDoneWorkKept(t *testing.T) {
	f := newWorktreeFixture(t)

	if errStr := f.runFail(t, "worktree", "remove", f.t2); !strings.Contains(errStr, "only once it is done") {
		t.Fatalf("remove on blocked work said %q", errStr)
	}
	worktreeStays(t, f.sample, f.private, f.privBr)

	commit(t, f.private, "ingest.txt", "unmerged\n")
	f.runOK(t, "done", f.t2, "--cancelled", "abandoned holding an unmerged commit")
	if errStr := f.runFail(t, "worktree", "remove", f.t2); !strings.Contains(errStr, "kept worktree "+f.private) {
		t.Fatalf("remove with unlanded work said %q", errStr)
	}
	worktreeStays(t, f.sample, f.private, f.privBr)

	gitRun(t, f.shared, "merge", "--no-edit", "-q", f.privBr)
	if out := f.runOK(t, "worktree", "remove", f.t2); !strings.Contains(out, "removed worktree "+f.private) {
		t.Fatalf("remove said %q, want the removed worktree", out)
	}
	worktreeGone(t, f.sample, f.private, f.privBr)
}

// reflectionFixture builds a fake runner whose sessions answer a reflection
// with a chosen verdict, so the report trigger and the ask/auto filing split
// are exercised end to end through the built binary.
type reflectionFixture struct {
	*cliFixture
	log string
}

// newReflectionFixture adds a spec-file runner that logs every call and
// answers every export with reply, and registers project projectName in mode.
func newReflectionFixture(t *testing.T, projectName, mode, reply string) *reflectionFixture {
	t.Helper()
	f := newCLIFixture(t)
	log := filepath.Join(f.dir, "reflection.log")
	// The executor's own session reports DONE; the reflection session the
	// director spawns afterwards answers with the verdict under test. One
	// script, two sessions, so the test exercises the real two-call path.
	rec := "#!/usr/bin/env bash\nmkdir -p \"$WD_FAKE_STATE\"\n" +
		"printf '%s|%s|%s\\n' \"$1\" \"$2\" \"$PWD\" >> \"" + log + "\"\n" +
		"case \"$1\" in\n" +
		"  run) echo 'session=ses_refl';;\n" +
		"  status) echo 'state: waiting';;\n" +
		"  export) if [ \"$2\" = ses_reflect ]; then echo 'STATUS: DONE'; else cat <<'REPLY'\n" +
		reply + "\nREPLY\nfi;;\n" +
		"  *) echo ok;;\nesac\n"
	// Into the fixture's own bin, so pathWith finds it alongside the others.
	if err := os.WriteFile(filepath.Join(f.bin, "reflector"), []byte(rec), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "name = \"reflector\"\nspawn = \"reflector run {brief}\"\nsession_id = 'session=(\\S+)'\n" +
		"send = \"reflector send {session} {text}\"\nstatus = \"reflector status {session}\"\n" +
		"running = 'state: *running'\nwaiting = 'state: *waiting'\nexited = 'state: *exited'\n" +
		"transcript = \"reflector export {session}\"\nmodels = \"reflector models\"\nattach = \"reflector attach {session}\"\n"
	if err := os.WriteFile(filepath.Join(f.wdHome, "runners", "reflector.toml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	env := f.env(t, f.bin)
	run := func(args ...string) string {
		t.Helper()
		code, out, errStr := f.runEnv(t, env, args...)
		if code != 0 {
			t.Fatalf("wd %s exited %d\n%s\n%s", strings.Join(args, " "), code, out, errStr)
		}
		return out
	}
	run("projects", "add", projectName, f.sample, "--runner", "reflector", "--mode", mode, "--lazyspec", "n")
	return &reflectionFixture{cliFixture: f, log: log}
}

// calls returns the runner ops recorded so far, one string per call.
func (rf *reflectionFixture) calls(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(rf.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// feedback returns the ledger's feedback rows.
func (rf *reflectionFixture) feedback(t *testing.T) []core.Feedback {
	t.Helper()
	l, err := ledger.New(filepath.Join(rf.wdHome, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	rows, err := l.Feedback()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// events returns the event bodies recorded for work.
func (rf *reflectionFixture) events(t *testing.T, id string) []string {
	t.Helper()
	evs := rf.eventRows(t, id)
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Body)
	}
	return out
}

// eventKinds returns the kinds recorded for work.
func (rf *reflectionFixture) eventKinds(t *testing.T, id string) []core.EventKind {
	t.Helper()
	evs := rf.eventRows(t, id)
	out := make([]core.EventKind, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

// attachedIn returns the attached feedback filed for one project. The fixture's
// own seed carries attached rows for sample-app, so every assertion here is
// scoped to the project under test.
func (rf *reflectionFixture) attachedIn(t *testing.T, project string) []core.Feedback {
	t.Helper()
	var out []core.Feedback
	for _, fb := range rf.feedback(t) {
		if fb.Source == core.FeedbackAttached && fb.Project != nil && *fb.Project == project {
			out = append(out, fb)
		}
	}
	return out
}

func (rf *reflectionFixture) eventRows(t *testing.T, id string) []core.Event {
	t.Helper()
	l, err := ledger.New(filepath.Join(rf.wdHome, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	evs, err := l.Events(id, nil)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

const durableReply = "DURABLE: a gate that cannot fail is not a gate | card: defensive-coding"

// runningWork adds work to the fixture project, claims it and starts it
// running, so wd report has a session with a DONE report to read.
func (rf *reflectionFixture) runningWork(t *testing.T, env []string, project string) string {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		code, out, errStr := rf.runEnv(t, env, args...)
		if code != 0 {
			t.Fatalf("wd %s exited %d\n%s\n%s", strings.Join(args, " "), code, out, errStr)
		}
		return out
	}
	id := jsonString(t, run("add", project, "Port the importer", "--json"), "id")
	// Attaching a session starts the work running, which is what wd report
	// reads.
	run("attach", id, "ses_reflect", "--runner", "reflector")
	return id
}

func TestReflectionRidesOnTheReportAndTheGoalPass(t *testing.T) {
	rf := newReflectionFixture(t, "auto-app", "auto", durableReply)
	env := rf.envWithPath(t, rf.pathWith(t, "reflector"))
	id := rf.runningWork(t, env, "auto-app")

	// The DONE report triggers exactly one reflection call, in the runner and
	// directory the work itself runs in.
	before := len(rf.calls(t))
	rf.runEnvOK(t, env, "report", id)
	after := rf.calls(t)
	if len(after) <= before {
		t.Fatalf("no reflection call: %v", after)
	}
	spawned := false
	for _, c := range after[before:] {
		if strings.HasPrefix(c, "run|") {
			spawned = true
		}
	}
	if !spawned {
		t.Fatalf("no spawn in the reflection calls: %v", after[before:])
	}

	// It filed the verdict as attached feedback.
	filed := rf.attachedIn(t, "auto-app")
	if len(filed) != 1 {
		t.Fatalf("filed %d attached rows for auto-app, want 1: %+v", len(filed), filed)
	}
	if filed[0].Text != "a gate that cannot fail is not a gate" {
		t.Fatalf("filed text = %q, want the lesson without the card marker", filed[0].Text)
	}
	if filed[0].Card == nil || *filed[0].Card != "defensive-coding" {
		t.Fatalf("filed card = %v, want defensive-coding", filed[0].Card)
	}
	// The evidence names the work it came out of, so a card promoted on it
	// records its decision there and a reader can audit the promotion against
	// the session that showed the pattern.
	if filed[0].Work == nil || *filed[0].Work != id {
		t.Errorf("filed work = %v, want %s: evidence that cannot be pointed back at the thing that produced it cannot be audited against it", filed[0].Work, id)
	}

	// It recorded an event naming the runner, model and the verdict.
	var body string
	for _, e := range rf.events(t, id) {
		if strings.Contains(e, "reflect runner=") {
			body = e
		}
	}
	if body == "" {
		t.Fatalf("no reflection event: %v", rf.events(t, id))
	}
	for _, want := range []string{"runner=reflector", "durable=1", "durable [defensive-coding]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("reflection event %q does not name %q", body, want)
		}
	}

	// Reflection is not a gate: the work's state is what the report left it,
	// and it satisfies no later gate — no verify, no PR.
	w, err := rf.work(t, id)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != core.StateReview {
		t.Fatalf("state = %q, want review: reflection must not move work state", w.State)
	}
	for _, kind := range []core.EventKind{core.EventVerify, core.EventPr} {
		for _, got := range rf.eventKinds(t, id) {
			if got == kind {
				t.Fatalf("reflection recorded a %s event: it must satisfy no gate", kind)
			}
		}
	}

	// --no-reflect skips the call, so a second report files nothing more.
	before = len(rf.calls(t))
	rf.runEnvOK(t, env, "report", id, "--no-reflect")
	if after := rf.calls(t); len(after) > before {
		for _, c := range after[before:] {
			if strings.HasPrefix(c, "run|") {
				t.Fatalf("--no-reflect still spawned a reflection session: %v", after[before:])
			}
		}
	}
	if got := rf.attachedIn(t, "auto-app"); len(got) != 1 {
		t.Fatalf("--no-reflect filed %d attached rows, want the 1 already filed: %+v", len(got), got)
	}

	// The epic pass reflects on the children that just reported DONE, in the
	// same command that coordinates them.
	run := func(args ...string) string { return rf.runEnvOK(t, env, args...) }
	epic := jsonString(t, run("add", "auto-app", "Port the invoicing stack", "--kind", "epic", "--json"), "id")
	child := jsonString(t, run("add", "auto-app", "Split the parser", "--epic", epic, "--json"), "id")
	run("attach", child, "ses_reflect", "--runner", "reflector")
	if out := run("epic", "review", epic, "--json"); !strings.Contains(out, `"reviewed"`) {
		t.Fatalf("epic review did not review the child: %s", out)
	}
	if got := rf.attachedIn(t, "auto-app"); len(got) != 2 {
		t.Fatalf("the epic pass filed %d attached rows, want 2: %+v", len(got), got)
	}
	run("epic", "review", epic, "--no-reflect")
	if got := rf.attachedIn(t, "auto-app"); len(got) != 2 {
		t.Fatalf("--no-reflect on the epic pass filed %d, want the 2 already filed: %+v", len(got), got)
	}
}

func TestASupervisedProjectProposesReflectionWithoutFilingIt(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		filed bool
	}{
		{"ask", false},
		{"auto", true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			rf := newReflectionFixture(t, tc.mode+"-app", tc.mode, durableReply)
			env := rf.envWithPath(t, rf.pathWith(t, "reflector"))
			id := rf.runningWork(t, env, tc.mode+"-app")
			rf.runEnvOK(t, env, "report", id)

			filed := len(rf.attachedIn(t, tc.mode+"-app")) > 0
			if filed != tc.filed {
				t.Fatalf("mode %s filed attached feedback = %v, want %v", tc.mode, filed, tc.filed)
			}
			// The event is recorded either way: a declined reflection is as
			// visible as a productive one.
			var body string
			for _, e := range rf.events(t, id) {
				if strings.Contains(e, "reflect runner=") {
					body = e
				}
			}
			if body == "" {
				t.Fatalf("mode %s recorded no reflection event", tc.mode)
			}
			if !strings.Contains(body, "durable=1") {
				t.Fatalf("mode %s event = %q, want the verdict named", tc.mode, body)
			}
		})
	}
}

// runEnvOK runs a command expected to succeed under env and returns stdout.
func (f *cliFixture) runEnvOK(t *testing.T, env []string, args ...string) string {
	t.Helper()
	code, out, errStr := f.runEnv(t, env, args...)
	if code != 0 {
		t.Fatalf("wd %s exited %d\n%s\n%s", strings.Join(args, " "), code, out, errStr)
	}
	return out
}

// work reads one work row.
func (f *cliFixture) work(t *testing.T, id string) (core.Work, error) {
	t.Helper()
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Get(id)
}

func TestARoadmapHoldsItemsAndTurnsThemIntoGoals(t *testing.T) {
	f := newCLIFixture(t)

	// A roadmap holds items, and an item with no roadmap is refused.
	roadmap := jsonString(t, f.runOK(t, "roadmap", "add", "sample-app", "Ship the thing", "--json"), "id")
	item := jsonString(t, f.runOK(t, "roadmap", "item", roadmap, "Migrate the ledger", "--json"), "id")
	second := jsonString(t, f.runOK(t, "roadmap", "item", roadmap, "Retire the old CLI", "--json"), "id")
	if errStr := f.runFail(t, "add", "sample-app", "Orphan", "--kind", "item"); !strings.Contains(errStr, "a roadmap item belongs to a roadmap") {
		t.Errorf("orphan item: %q, want it refused for having no roadmap", errStr)
	}
	if errStr := f.runFail(t, "add", "sample-app", "Task on a roadmap", "--roadmap", roadmap); !strings.Contains(errStr, "a roadmap holds items, not task") {
		t.Errorf("task under a roadmap: %q, want the roadmap to say what it holds", errStr)
	}

	// Planning translates the items on the same rows: the ids do not move.
	f.runOK(t, "roadmap", "plan", roadmap, "--type", "build")
	var promoted struct {
		ID       string         `json:"id"`
		Title    string         `json:"title"`
		GoalType *core.GoalType `json:"goal_type"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "roadmap", "show", roadmap, "--json")), &promoted); err != nil {
		t.Fatalf("roadmap show: %v", err)
	}
	for _, want := range []struct{ id, title string }{{item, "Migrate the ledger"}, {second, "Retire the old CLI"}} {
		got := f.workRow(t, want.id)
		if got.Kind != core.WorkGoal {
			t.Errorf("%s kind = %s, want goal: the row translated rather than being copied", want.id, got.Kind)
		}
		if got.GoalType == nil || *got.GoalType != core.GoalBuild {
			t.Errorf("%s goal_type = %v, want build", want.id, got.GoalType)
		}
	}
	// show reads top-down: the roadmap, its waiting items, and its goals.
	var show struct {
		Roadmap core.Work   `json:"roadmap"`
		Waiting []core.Work `json:"waiting"`
		Goals   []struct {
			Goal  core.Work   `json:"goal"`
			Tasks []core.Work `json:"tasks"`
			Open  int         `json:"open"`
		} `json:"goals"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "roadmap", "show", roadmap, "--json")), &show); err != nil {
		t.Fatalf("roadmap show: %v", err)
	}
	if show.Roadmap.ID != roadmap {
		t.Errorf("show roadmap = %s, want %s", show.Roadmap.ID, roadmap)
	}
	if len(show.Waiting) != 0 {
		t.Errorf("waiting = %v, want none: every item is now a goal", show.Waiting)
	}
	if len(show.Goals) != 2 || show.Goals[0].Goal.ID != item {
		t.Errorf("goals = %+v, want the two goals in the order they were filed", show.Goals)
	}
	// Planning again is not an error and does not reset a goal that has begun.
	f.runOK(t, "set", item, "running")
	out := f.runOK(t, "roadmap", "plan", roadmap, "--json")
	if strings.Contains(out, item) {
		t.Errorf("second plan = %q, want it to leave the already-translated items alone", out)
	}
	if got := f.workRow(t, item); got.State != core.StateRunning {
		t.Errorf("%s state = %s, want running: re-planning must not reset a goal", item, got.State)
	}
	// An empty roadmap says so rather than reporting nothing quietly.
	empty := jsonString(t, f.runOK(t, "roadmap", "add", "sample-app", "Nothing yet", "--json"), "id")
	if out := f.runOK(t, "roadmap", "plan", empty); !strings.Contains(out, "nothing waiting") {
		t.Errorf("plan an empty roadmap = %q, want it to say nothing is waiting", out)
	}
	// The listing finds it.
	if out := f.runOK(t, "roadmap"); !strings.Contains(out, empty) {
		t.Errorf("roadmap list = %q, want it to include %s", out, empty)
	}
}

func TestAGoalSaysWhatKindOfThingItWas(t *testing.T) {
	f := newCLIFixture(t)
	goal := f.ids["epic"]

	// Unset means unset. A type is never defaulted.
	if got := f.workRow(t, goal); got.GoalType != nil {
		t.Fatalf("%s goal_type = %v, want none before anyone says", goal, *got.GoalType)
	}

	// --type at the moment of filing.
	typed := jsonString(t, f.runOK(t, "goal", "add", "sample-app", "What is the verify command", "--type", "query", "--json"), "id")
	if got := f.workRow(t, typed); got.GoalType == nil || *got.GoalType != core.GoalQuery {
		t.Fatalf("%s goal_type = %v, want query", typed, got.GoalType)
	}

	// classify records the claim as a decision, so it can be seen and changed.
	f.runOK(t, "goal", "classify", goal, "build")
	if got := f.workRow(t, goal); got.GoalType == nil || *got.GoalType != core.GoalBuild {
		t.Fatalf("%s goal_type = %v, want build", goal, got.GoalType)
	}
	var events []core.Event
	if err := json.Unmarshal([]byte(f.runOK(t, "events", goal, "--json")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Kind == core.EventDecision && e.Body == "classified build" {
			found = true
		}
	}
	if !found {
		t.Errorf("events = %+v, want the classification recorded as a decision", events)
	}

	// A type outside the set is not spellable.
	if errStr := f.runFail(t, "goal", "classify", goal, "invented"); !strings.Contains(errStr, `unknown goal type "invented"`) {
		t.Errorf("classify: %q, want the set named", errStr)
	}
	// A type on work that is not a goal is refused.
	if errStr := f.runFail(t, "goal", "classify", f.ids["t1"], "build"); !strings.Contains(errStr, "is a task, not a goal") {
		t.Errorf("classify a task: %q, want it refused", errStr)
	}
	// --type on a task is refused at the door too.
	if errStr := f.runFail(t, "add", "sample-app", "x", "--goal", goal, "--type", "build"); !strings.Contains(errStr, "a goal type applies to a goal, not task") {
		t.Errorf("add a typed task: %q, want it refused", errStr)
	}
}

func TestAFinishedGoalIsReopenedOnlyForWorkNotForAQuestion(t *testing.T) {
	f := newCLIFixture(t)

	// A finished goal, closed the way goals close: its task done, then its own
	// report, verify and pull request, then done.
	goal := jsonString(t, f.runOK(t, "goal", "add", "sample-app", "A goal that finished", "--type", "build", "--json"), "id")
	task := jsonString(t, f.runOK(t, "add", "sample-app", "Its only task", "--goal", goal, "--json"), "id")
	closeThroughTheGates(t, f, task)
	closeThroughTheGates(t, f, goal)
	if got := f.workRow(t, goal); got.State != core.StateDone {
		t.Fatalf("goal state = %s, want done", got.State)
	}

	// Asking it something is not reopening it, so the reason is required and the
	// refusal points at the reads that do answer a question.
	errStr := f.runFail(t, "reopen", goal)
	if !strings.Contains(errStr, "wd context "+goal) || !strings.Contains(errStr, "wd events "+goal) {
		t.Errorf("reopen with no reason = %q, want it naming the reads that answer a question", errStr)
	}
	if got := f.workRow(t, goal); got.State != core.StateDone {
		t.Errorf("goal state = %s, want the refusal to leave it done: a question is not work", got.State)
	}

	// A query about the finished goal is its own goal, and the finished one stays
	// finished: a status question costs nothing and changes nothing.
	q := jsonString(t, f.runOK(t, "goal", "add", "sample-app", "What did the loop decide about ports?", "--type", "query", "--json"), "id")
	if got := f.workRow(t, q); got.GoalType == nil || *got.GoalType != core.GoalQuery {
		t.Errorf("query goal type = %v, want query", got.GoalType)
	}
	if got := f.workRow(t, goal); got.State != core.StateDone {
		t.Errorf("goal state = %s after being asked about, want done", got.State)
	}

	// A task under a finished goal would sit there with nothing to run it.
	errStr = f.runFail(t, "add", "sample-app", "Work nobody will run", "--goal", goal)
	if !strings.Contains(errStr, "wd reopen "+goal) {
		t.Errorf("add under a done goal = %q, want it naming the reopen", errStr)
	}
	if tasks := strings.Count(f.runOK(t, "tasks", goal, "--all", "--json"), `"id"`); tasks != 1 {
		t.Errorf("goal holds %d rows, want the refused task not to exist", tasks)
	}

	// A message is not a reason either, and it is refused before the message is
	// recorded rather than after.
	errStr = f.runFail(t, "send", goal, "what did you decide?")
	if !strings.Contains(errStr, "wd reopen "+goal) {
		t.Errorf("send to a done goal = %q, want it naming the reopen", errStr)
	}
	if strings.Contains(f.runOK(t, "events", goal, "--json"), `"kind": "sent"`) {
		t.Error("a sent event was recorded on a goal that refused the message")
	}

	// Working more on top of it: the reopen names what, and the claim travels
	// with it so a reader can see why a finished goal is running again.
	out := f.runOK(t, "reopen", goal, "a second half on the driver", "--json")
	if !strings.Contains(out, `"state": "running"`) {
		t.Fatalf("reopen = %q, want the goal running", out)
	}
	if !strings.Contains(f.runOK(t, "events", goal, "--json"), "a second half on the driver") {
		t.Error("the reopen says nothing about what is being worked on")
	}
	// The first run's evidence does not close the second: the goal has a new task
	// that has not been done, and the report, verify and pull request it still
	// holds are all from before the reopen.
	errStr = f.runFail(t, "soft-done", goal)
	for _, want := range []string{"DONE report", "passing verify", "pull request"} {
		if !strings.Contains(errStr, want) {
			t.Errorf("soft-done after reopen = %q, want it to still want %q", errStr, want)
		}
	}

	// The same command through the goal spelling, on a goal of its own so this
	// one is not reopened twice.
	other := jsonString(t, f.runOK(t, "goal", "add", "sample-app", "Another finished goal", "--type", "build", "--json"), "id")
	otherTask := jsonString(t, f.runOK(t, "add", "sample-app", "Its only task", "--goal", other, "--json"), "id")
	closeThroughTheGates(t, f, otherTask)
	closeThroughTheGates(t, f, other)
	if errStr := f.runFail(t, "goal", "reopen", other); !strings.Contains(errStr, "wd context "+other) {
		t.Errorf("wd goal reopen with no reason = %q, want the same reason guidance", errStr)
	}
	f.runOK(t, "goal", "reopen", other, "the second half")
	if got := f.workRow(t, other); got.State != core.StateRunning {
		t.Errorf("goal state = %s, want running", got.State)
	}

	// A task is reopenable on its own terms too: work that grows a second half
	// is the same shape of thing as a goal that does.
	half := jsonString(t, f.runOK(t, "add", "sample-app", "A task that finished", "--json"), "id")
	closeThroughTheGates(t, f, half)
	f.runOK(t, "reopen", half, "the other half")
	if got := f.workRow(t, half); got.State != core.StateRunning {
		t.Errorf("task state = %s, want running", got.State)
	}
}

// closeThroughTheGates takes work from queued to done through every gate it
// needs: running, review, a DONE report, a passing verify, and — where asked —
// a pull request.
func closeThroughTheGates(t *testing.T, f *cliFixture, id string) {
	t.Helper()
	f.runOK(t, "set", id, "running")
	f.runOK(t, "set", id, "review")
	f.runOK(t, "report", id, "DONE\nSTATUS: DONE")
	f.runOK(t, "verify", id)
	f.runOK(t, "pr", id, "commit https://github.com/mkhanal/sample-app/commit/abc1234")
	f.runOK(t, "soft-done", id)
	f.runOK(t, "done", id)
}

func TestWorkEndsAbandonedAndSaysWhyItDidNotShip(t *testing.T) {
	f := newCLIFixture(t)

	// With no reason given it is computed from the ledger: no PR at all.
	never := f.ids["standalone"]
	out := f.runOK(t, "abandon", never, "--reason", "never started", "--json")
	if !strings.Contains(out, `"state": "abandoned"`) {
		t.Fatalf("abandon = %q, want the goal abandoned", out)
	}
	if got := f.workRow(t, never); got.State != core.StateAbandoned {
		t.Fatalf("%s state = %s, want abandoned", never, got.State)
	}
	var events []core.Event
	if err := json.Unmarshal([]byte(f.runOK(t, "events", never, "--json")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	reason := ""
	for _, e := range events {
		if e.Kind == core.EventAbandon {
			reason = e.Body
		}
	}
	if reason != "no-pr: never started" {
		t.Errorf("abandon event = %q, want the computed reason and the detail", reason)
	}

	// A PR that was raised and never merged reads unmerged.
	raised := jsonString(t, f.runOK(t, "add", "sample-app", "Raised but never merged", "--json"), "id")
	if code, _, errStr := f.run(t, "pr", raised, "https://example.test/pr/1", "--json"); code != 0 {
		t.Fatalf("pr: exit %d: %s", code, errStr)
	}
	f.runOK(t, "abandon", raised, "--json")
	events = nil
	if err := json.Unmarshal([]byte(f.runOK(t, "events", raised, "--json")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	reason = ""
	for _, e := range events {
		if e.Kind == core.EventAbandon {
			reason = e.Body
		}
	}
	if !strings.HasPrefix(reason, "unmerged") {
		t.Errorf("abandon event = %q, want the computed reason unmerged", reason)
	}

	// A reason nobody can verify is not spellable.
	if errStr := f.runFail(t, "abandon", f.ids["t2"], "lost-interest"); !strings.Contains(errStr, `unknown abandon reason "lost-interest"`) {
		t.Errorf("abandon: %q, want only the verifiable reasons spellable", errStr)
	}
	// Abandoned is terminal: it does not transition back out.
	if errStr := f.runFail(t, "set", never, "running"); !strings.Contains(errStr, "illegal transition abandoned → running") {
		t.Errorf("set: %q, want abandoned to be terminal", errStr)
	}
}

func TestWdEpicIsTheOldSpellingOfWdGoal(t *testing.T) {
	f := newCLIFixture(t)
	goal := f.ids["epic"]

	// Both spellings reach the same work.
	f.runOK(t, "epic", "status", goal)
	f.runOK(t, "goal", "status", goal)
	// And they say the same new word.
	if out := f.runOK(t, "epic", "status", goal); strings.Contains(out, "epic") {
		t.Errorf("wd epic status = %q, want the current word, not the old spelling", out)
	}
	if errStr := f.runFail(t, "epic", "status", f.ids["t1"]); !strings.Contains(errStr, "is a task, not a goal") {
		t.Errorf("wd epic status on a task: %q, want the same refusal as wd goal", errStr)
	}

	// --kind epic writes a goal.
	written := jsonString(t, f.runOK(t, "add", "sample-app", "Written the old way", "--kind", "epic", "--json"), "id")
	if got := f.workRow(t, written); got.Kind != core.WorkGoal {
		t.Errorf("%s kind = %s, want goal: the old spelling writes the current kind", written, got.Kind)
	}
	// --epic names the same parent as --goal.
	old := jsonString(t, f.runOK(t, "add", "sample-app", "Under the old flag", "--epic", goal, "--json"), "id")
	if got := f.workRow(t, old); got.Parent == nil || *got.Parent != goal {
		t.Errorf("%s parent = %v, want %s", old, got.Parent, goal)
	}
	// A row stored under the old name reads as a goal without being rewritten.
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer l.Close()
	stored, err := l.Add("sample-app", "Filed before the rename", ledger.AddOptions{Kind: core.WorkEpic})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := f.workRow(t, stored.ID); got.Kind != core.WorkGoal {
		t.Fatalf("old row = %s, want it read as goal without being rewritten", got.Kind)
	}
	// And the board agrees it is a goal, not a stray kind.
	if out := f.runOK(t, "status", "--all", "--json"); !strings.Contains(out, `"id": "`+stored.ID+`"`) || !strings.Contains(out, `"kind": "goal"`) {
		t.Errorf("status --all = %q, want the old row listed as a goal", out)
	}
}

func TestReviewIsADiffOverWhatTheLoopDecided(t *testing.T) {
	f := newCLIFixture(t)
	work := jsonString(t, f.runOK(t, "add", "sample-app", "Pick a driver", "--json"), "id")

	// A decision in its parts, with what it cost and when it took hold.
	f.runOK(t, "decide", work, "use modernc.org/sqlite",
		"--question", "which sqlite driver", "--answer", "modernc.org/sqlite",
		"--source", "director", "--runner", "opencode", "--model", "big-pickle",
		"--tokens", "18400", "--effective", "2026-01-01T00:00:00Z")
	// And one in a single line, which is what a person types.
	f.runOK(t, "decide", work, "keep the worktree shared for the goal")
	// A card promoted under both, which the pass has to carry.
	f.runOK(t, "feedback", "add", "shared worktrees beat one per task", "--project", "sample-app")
	// And work that stopped without shipping.
	unshipped := jsonString(t, f.runOK(t, "add", "sample-app", "Never landed", "--json"), "id")
	f.runOK(t, "abandon", unshipped)

	out := f.runOK(t, "review", "--project", "sample-app")
	for _, want := range []string{
		"which sqlite driver", "modernc.org/sqlite", "big-pickle", "18400 tokens",
		"keep the worktree shared", "effective", unshipped, "stopped without shipping",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("review = %q, want it to contain %q", out, want)
		}
	}

	// The same pass as one document, with the parts addressable.
	var pass struct {
		Project string `json:"project"`
		Since   int    `json:"since"`
		Claims  []struct {
			Event struct {
				ID        int     `json:"id"`
				At        string  `json:"at"`
				Effective *string `json:"effective"`
				Decision  *struct {
					Question string `json:"question"`
					Answer   string `json:"answer"`
					Runner   string `json:"runner"`
					Model    string `json:"model"`
					Tokens   int    `json:"tokens"`
				} `json:"decision"`
			} `json:"event"`
			Work *struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"work"`
			Stands bool `json:"stands"`
		} `json:"claims"`
		Cards     []struct{ Card, Text string } `json:"cards"`
		Unshipped []struct {
			Work struct{ ID string } `json:"work"`
		} `json:"unshipped"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "review", "--project", "sample-app", "--json")), &pass); err != nil {
		t.Fatalf("review --json: %v", err)
	}
	if pass.Since != 0 {
		t.Errorf("since = %d, want 0 on the first pass", pass.Since)
	}
	if len(pass.Claims) != 2 {
		t.Fatalf("claims = %d, want both decisions", len(pass.Claims))
	}
	// The structured claim reads as itself, and the prose one is still
	// reviewable from its body with no claim at all.
	if pass.Claims[0].Event.Decision == nil || pass.Claims[0].Event.Decision.Model != "big-pickle" || pass.Claims[0].Event.Decision.Tokens != 18400 {
		t.Errorf("structured claim = %+v, want the parts a review reads", pass.Claims[0].Event.Decision)
	}
	if pass.Claims[0].Event.Effective == nil || *pass.Claims[0].Event.Effective == pass.Claims[0].Event.At {
		t.Errorf("effective = %v, recorded %v; want the two told apart", pass.Claims[0].Event.Effective, pass.Claims[0].Event.At)
	}
	if pass.Claims[1].Event.Decision != nil {
		t.Errorf("one-line claim = %+v, want no payload: a person typed prose", pass.Claims[1].Event.Decision)
	}
	for _, c := range pass.Claims {
		if c.Work == nil || c.Work.ID != work {
			t.Errorf("claim work = %+v, want the work each decision is about", c.Work)
		}
	}
	if len(pass.Unshipped) != 1 || pass.Unshipped[0].Work.ID != unshipped {
		t.Errorf("unshipped = %+v, want the abandoned work in the same pass", pass.Unshipped)
	}

	// --since reads a window by hand and moves nothing.
	first := pass.Claims[0].Event.ID
	window := f.runOK(t, "review", "--project", "sample-app", "--since", strconv.Itoa(first), "--json")
	if strings.Contains(window, "big-pickle") {
		t.Errorf("windowed review = %q, want it to exclude what came before", window)
	}
	if again := jsonNumber(t, f.runOK(t, "review", "--project", "sample-app", "--json"), "since"); again != "0" {
		t.Errorf("since = %v after a hand-asked window, want it still 0", again)
	}

	// An empty pass says so rather than printing nothing.
	f.runOK(t, "review", "--project", "sample-app", "--ack")
	if out := f.runOK(t, "review", "--project", "sample-app"); !strings.Contains(out, "nothing since event") {
		t.Errorf("empty review = %q, want it to say nothing has been decided", out)
	}
	// And an unknown project is refused by name, not guessed at.
	if errStr := f.runFail(t, "review", "--project", "nope"); !strings.Contains(errStr, "unknown project nope") {
		t.Errorf("review an unknown project: %q, want it named", errStr)
	}
}

func TestAcknowledgingAReviewIsWhatMakesTheNextOneShort(t *testing.T) {
	f := newCLIFixture(t)
	work := jsonString(t, f.runOK(t, "add", "sample-app", "Work", "--json"), "id")
	f.runOK(t, "decide", work, "first", "--question", "which runner", "--answer", "claude")

	// Reading a pass repeatedly, and acknowledging none of them, changes
	// nothing: a pass that was read but never seen comes round again.
	for range 3 {
		if out := f.runOK(t, "review", "--project", "sample-app"); !strings.Contains(out, "which runner") {
			t.Fatalf("review = %q, want the same pass every time until it is acknowledged", out)
		}
	}
	if since := jsonNumber(t, f.runOK(t, "review", "--project", "sample-app", "--json"), "since"); since != "0" {
		t.Fatalf("since = %v after three reads, want 0: reading is not seeing", since)
	}

	// Acknowledging consumes exactly what was in the pass, not everything that
	// exists by the time it is called.
	f.runOK(t, "review", "--project", "sample-app", "--ack")
	f.runOK(t, "decide", work, "second", "--question", "which model", "--answer", "big-pickle")
	out := f.runOK(t, "review", "--project", "sample-app")
	if strings.Contains(out, "which runner") {
		t.Errorf("review = %q, want only what was decided since the acknowledgement", out)
	}
	if !strings.Contains(out, "which model") {
		t.Errorf("review = %q, want the decision made after it", out)
	}

	// A window that is not a number is refused rather than read as nothing,
	// which would look like a quiet ledger.
	if errStr := f.runFail(t, "review", "--since", "recent", "--project", "sample-app"); !strings.Contains(errStr, "--since \"recent\" is not a number") {
		t.Errorf("--since recent = %q, want a number error", errStr)
	}
}

func TestADecisionCanBeRecordedInOneLineOrInItsParts(t *testing.T) {
	f := newCLIFixture(t)
	work := jsonString(t, f.runOK(t, "add", "sample-app", "Work", "--json"), "id")

	// One line is the default and needs no flags at all.
	f.runOK(t, "decide", work, "one line of prose")
	// A claim with no question or no answer cannot be reviewed, so it is
	// refused rather than stored half-formed.
	if errStr := f.runFail(t, "decide", work, "half", "--answer", "a"); !strings.Contains(errStr, "question") {
		t.Errorf("a claim with no question: %q, want it refused", errStr)
	}
	if errStr := f.runFail(t, "decide", work, "half", "--question", "q"); !strings.Contains(errStr, "answer") {
		t.Errorf("a claim with no answer: %q, want it refused", errStr)
	}
	// A reversal needs no reason flag of its own, and one with no reason is
	// refused: nobody could read the motive for it.
	if errStr := f.runFail(t, "review", "reverse", "1"); !strings.Contains(errStr, "says why") {
		t.Errorf("reverse with no reason: %q, want it refused", errStr)
	}
}

func TestAReversalIsANewDecisionNotAnEdit(t *testing.T) {
	f := newCLIFixture(t)
	work := jsonString(t, f.runOK(t, "add", "sample-app", "Work", "--json"), "id")
	original := jsonNumber(t, f.runOK(t, "decide", work, "use postgres",
		"--question", "which database", "--answer", "postgres", "--json"), "id")

	// The claim stands until something reverses it.
	if out := f.runOK(t, "review", "--project", "sample-app"); strings.Contains(out, "REVERSED") {
		t.Errorf("review = %q, want the claim standing before any reversal", out)
	}

	f.runOK(t, "review", "reverse", original, "the fleet has no postgres")

	out := f.runOK(t, "review", "--project", "sample-app")
	if !strings.Contains(out, "REVERSED") || !strings.Contains(out, "the fleet has no postgres") {
		t.Errorf("review = %q, want the reversal and its reason visible", out)
	}
	if !strings.Contains(out, "which database") {
		t.Errorf("review = %q, want the original claim still there: an audit cannot afford to lose it", out)
	}

	// The original is left exactly as it was made, marked with what undid it.
	var events []struct {
		ID       int    `json:"id"`
		Kind     string `json:"kind"`
		Body     string `json:"body"`
		Decision *struct {
			Answer     string `json:"answer"`
			Reverses   int    `json:"reverses"`
			ReversedBy int    `json:"reversed_by"`
		} `json:"decision"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "events", work, "--json")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	claims := []int{}
	for i, e := range events {
		if e.Kind == "decision" {
			claims = append(claims, i)
		}
	}
	if len(claims) != 2 {
		t.Fatalf("decisions = %d, want the claim and the reversal", len(claims))
	}
	originalEvent, reversal := events[claims[0]], events[claims[1]]
	if originalEvent.Body != "use postgres" || originalEvent.Decision == nil || originalEvent.Decision.Answer != "postgres" {
		t.Errorf("original = %+v, want the claim as it was made", originalEvent)
	}
	if originalEvent.Decision.ReversedBy == 0 {
		t.Errorf("original reversed_by = 0, want it marked with what undid it")
	}
	if reversal.Kind != "decision" || reversal.Decision == nil || reversal.Decision.Reverses == 0 {
		t.Errorf("reversal = %+v, want a decision naming what it undoes", reversal)
	}

	// Only a decision can be reversed, and the event has to exist.
	f.runOK(t, "pr", work, "https://example.test/pr/1")
	var pr struct{ ID int }
	if err := json.Unmarshal([]byte(f.runOK(t, "events", work, "--json")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	for _, e := range events {
		if e.Kind == "pr" {
			pr.ID = e.ID
		}
	}
	if errStr := f.runFail(t, "review", "reverse", strconv.Itoa(pr.ID), "because"); !strings.Contains(errStr, "is a pr, not a decision") {
		t.Errorf("reverse a pull request: %q, want it refused", errStr)
	}
	if errStr := f.runFail(t, "review", "reverse", "9999", "because"); !strings.Contains(errStr, "no event 9999") {
		t.Errorf("reverse a missing event: %q, want it named", errStr)
	}
	if errStr := f.runFail(t, "review", "reverse", "abc", "because"); !strings.Contains(errStr, "is not a number") {
		t.Errorf("reverse a non-numeric id: %q, want it refused", errStr)
	}
}

// driveProject writes a project whose executors ask questions nothing in the
// ledger can answer, so a driven goal meets the supervision case head on. The
// judgement model and the executor are the same runner, because a judgement is
// made by the project's own model.
func driveProject(t *testing.T, f *cliFixture) {
	t.Helper()
	path := filepath.Join(f.wdHome, "projects", "driven.md")
	body := "---\npath: " + f.sample + "\nrunner: driven\nmode: auto\nstack: [go]\nverify: [test -f README.md]\ndefault_branch: main\n---\nA project whose executors ask.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
}

// drivenGoal is a goal with one running task that has a session, which is what
// a coordination pass needs before it can read a transcript and find a question.
func drivenGoal(t *testing.T, f *cliFixture, title string) (goal, task string) {
	t.Helper()
	goal = jsonString(t, f.runOK(t, "goal", "add", "driven", title, "--json"), "id")
	task = jsonString(t, f.runOK(t, "add", "driven", "Wire the server", "--goal", goal, "--json"), "id")
	f.runOK(t, "set", task, "running")
	f.runOK(t, "attach", task, "executor-s1", "--runner", "driven")
	return goal, task
}

// judgeAnswer makes the fake model answer the next question, or refuse to. It
// creates the state directory first: a fake runner writing into a directory
// that is not there fails its redirect and carries on, which looks exactly like
// a runner that ran and did nothing.
func judgeAnswer(t *testing.T, f *cliFixture, answer bool) {
	t.Helper()
	dir := filepath.Join(f.dir, "fake-state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	path := filepath.Join(dir, "judge-decline")
	if answer {
		if err := os.RemoveAll(path); err != nil {
			t.Fatalf("clear decline: %v", err)
		}
		return
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seed decline: %v", err)
	}
}

func TestAGoalRunsItsLoopWithNobodyWatching(t *testing.T) {
	f := newCLIFixture(t)
	driveProject(t, f)
	goal, task := drivenGoal(t, f, "Serve on a fixed port")
	judgeAnswer(t, f, true)

	// One turn is enough to see the whole loop: the pass finds the question,
	// the model answers it, the answer is recorded and delivered, and the run
	// stops on its own bound without ever asking whether to carry on.
	out := f.runOK(t, "drive", goal, "--turns", "2", "--judgements", "1",
		"--stalled", "0", "--poll-seconds", "1")

	if !strings.Contains(out, "stopped: budget") {
		t.Errorf("drive = %q, want it to say why it stopped", out)
	}
	if !strings.Contains(out, "settled: "+task) {
		t.Errorf("drive = %q, want the task whose question was settled named", out)
	}

	// The judgement was recorded as a decision, with the question, the answer,
	// the model and what it cost — before it reached the executor.
	var events []struct {
		Kind     core.EventKind `json:"kind"`
		Body     string         `json:"body"`
		Decision *struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
			Source   string `json:"source"`
			Model    string `json:"model"`
			Tokens   int    `json:"tokens"`
		} `json:"decision"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "events", task, "--json")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	claims := 0
	for _, e := range events {
		if e.Decision == nil {
			continue
		}
		claims++
		if e.Decision.Question == "" || e.Decision.Answer == "" {
			t.Errorf("claim = %+v, want the question and the answer", e.Decision)
		}
		if e.Decision.Source != "judge" || e.Decision.Model != "" && e.Decision.Model == " " {
			t.Errorf("claim = %+v, want it to name that a model decided", e.Decision)
		}
		if e.Decision.Tokens != 1200 {
			t.Errorf("claim tokens = %d, want what the call cost", e.Decision.Tokens)
		}
	}
	if claims != 1 {
		t.Errorf("claims = %d, want exactly one: a run must not decide the same question twice", claims)
	}
	// And the answer reached the executor, which is what makes a judgement worth
	// anything: a settled question that never lands changes nothing.
	sent, err := os.ReadFile(filepath.Join(f.dir, "fake-state", "sent-to-executor-s1"))
	if err != nil {
		t.Fatalf("the answer never reached the executor: %v\ndrive said:\n%s", err, out)
	}
	if !strings.Contains(string(sent), "8080") {
		t.Errorf("the executor was sent %q, want the judgement", sent)
	}

	// The brief is the project's settled position and the question quoted as
	// evidence, so a line in a transcript cannot read as a command.
	brief, err := os.ReadFile(filepath.Join(f.dir, "fake-state", "judge-brief-1"))
	if err != nil {
		t.Fatalf("read brief: %v", err)
	}
	// The settled position is what the judgement is made from, so the goal and
	// the tasks under it are in the brief and not just the question.
	for _, want := range []string{"which port", "```", "DECLINE:", "Serve on a fixed port", "The settled position:"} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("the judgement brief is missing %q:\n%s", want, brief)
		}
	}

	// A bound left at zero is not a bound, so a run under one stops on something
	// else. Here on the deadline, and it says which. It needs a goal that can
	// genuinely not finish: the executor is still asking, with the answered state
	// cleared, and the model now refuses — so nothing moves and nothing settles,
	// which is the only state a deadline is the thing that ends.
	untouched, _ := drivenGoal(t, f, "Never started")
	if err := os.Remove(filepath.Join(f.dir, "fake-state", "executor-answered")); err != nil {
		t.Fatalf("clear the answered state: %v", err)
	}
	judgeAnswer(t, f, false)
	unbounded := f.runOK(t, "drive", untouched, "--turns", "0", "--judgements", "0",
		"--stalled", "0", "--deadline", "2s", "--poll-seconds", "1", "--json")
	if !strings.Contains(unbounded, "the deadline passed") {
		t.Errorf("a run with no turn bound = %s, want it to stop on the deadline it was given", unbounded)
	}
}

func TestARunThatDidNotShipEndsTheGoalAbandonedAndSaysWhy(t *testing.T) {
	f := newCLIFixture(t)
	driveProject(t, f)
	goal, task := drivenGoal(t, f, "Serve on a fixed port")
	judgeAnswer(t, f, true)

	// The model settles the question once, so the second turn finds the
	// executor reporting DONE and nothing moves after that.
	out := f.runOK(t, "drive", goal, "--turns", "1", "--judgements", "1", "--stalled", "0", "--poll-seconds", "1")
	if !strings.Contains(out, "the goal is abandoned") {
		t.Errorf("drive = %q, want it to say the goal did not ship", out)
	}

	// The goal came to rest, and it says why: the stop and the reason, with the
	// unfinished work named.
	got := f.workRow(t, goal)
	if got.State != core.StateAbandoned {
		t.Errorf("goal state = %s, want abandoned", got.State)
	}
	var goalEvents []struct {
		Kind core.EventKind `json:"kind"`
		Body string         `json:"body"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "events", goal, "--json")), &goalEvents); err != nil {
		t.Fatalf("events: %v", err)
	}
	var abandon string
	for _, e := range goalEvents {
		if e.Kind == core.EventAbandon {
			abandon = e.Body
		}
	}
	if !strings.HasPrefix(abandon, "no-pr: ") {
		t.Fatalf("abandon body = %q, want it to name the reason", abandon)
	}
	for _, want := range []string{"stopped on", "unfinished: ", task} {
		if !strings.Contains(abandon, want) {
			t.Errorf("abandon body = %q, want it to contain %q", abandon, want)
		}
	}

	// A task left running under a goal that has come to rest would claim work
	// is in progress when nothing is driving it.
	if got := f.workRow(t, task); got.State != core.StateAbandoned {
		t.Errorf("task state = %s, want abandoned with its goal", got.State)
	}
	// A goal that has come to rest has no loop to run, and running one anyway
	// would report a run that never happened as a run that finished.
	if errStr := f.runFail(t, "drive", goal); !strings.Contains(errStr, "nothing left to run") {
		t.Errorf("drive a goal that came to rest: %q, want it refused", errStr)
	}

	// A run that stopped because the loop could not run ends nothing: the work is
	// untouched, and ending a goal because a runner could not read a transcript
	// would throw away real work over a failure that says nothing about it.
	brokenProject := filepath.Join(f.wdHome, "projects", "unreadable.md")
	body := "---\npath: " + f.sample + "\nrunner: broken\nmode: auto\nstack: [go]\nverify: [test -f README.md]\ndefault_branch: main\n---\nA project whose sessions cannot be read.\n"
	if err := os.WriteFile(brokenProject, []byte(body), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
	broken := jsonString(t, f.runOK(t, "goal", "add", "unreadable", "A runner that cannot be read", "--json"), "id")
	brokenTask := jsonString(t, f.runOK(t, "add", "unreadable", "Wire the server", "--goal", broken, "--json"), "id")
	f.runOK(t, "set", brokenTask, "running")
	f.runOK(t, "attach", brokenTask, "broken-s00001", "--runner", "broken")
	out = f.runOK(t, "drive", broken, "--turns", "2", "--judgements", "1", "--stalled", "0", "--poll-seconds", "1")
	if !strings.Contains(out, "the goal is untouched") {
		t.Errorf("a run that could not run = %q, want it to say the work was left alone", out)
	}
	if !strings.Contains(out, "cannot read session") {
		t.Errorf("a run that could not run = %q, want the failure that stopped it", out)
	}
	if got := f.workRow(t, broken); got.State == core.StateAbandoned {
		t.Errorf("goal state = %s, want it untouched: a failed loop is not a goal that did not ship", got.State)
	}
	if got := f.workRow(t, brokenTask); got.State == core.StateAbandoned {
		t.Errorf("task state = %s, want it untouched", got.State)
	}
}

func TestARunReportsWhereAPersonIsStillNeeded(t *testing.T) {
	f := newCLIFixture(t)
	driveProject(t, f)
	goal, task := drivenGoal(t, f, "Serve on a fixed port")
	// The model refuses. An unattended loop must be able to hear no, and a run
	// that cannot is not making judgements — it is making noise.
	judgeAnswer(t, f, false)

	out := f.runOK(t, "drive", goal, "--turns", "3", "--judgements", "3", "--stalled", "0", "--poll-seconds", "1")
	if !strings.Contains(out, "still needing a person:") {
		t.Errorf("drive = %q, want it to say where a person is still required", out)
	}
	if !strings.Contains(out, task+":") {
		t.Errorf("drive = %q, want the waiting task named with its question", out)
	}
	if !strings.Contains(out, "3 turns is the bound") {
		t.Errorf("drive = %q, want the bound that ran out named", out)
	}

	// A decline is recorded as a decline and never as a decision: the ledger
	// must not gain a claim nobody made, and an audit that cannot tell an answer
	// from a refusal will believe the loop decided things it did not.
	var events []struct {
		Kind     core.EventKind `json:"kind"`
		Body     string         `json:"body"`
		Decision *struct {
			Question string `json:"question"`
		} `json:"decision"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "events", task, "--json")), &events); err != nil {
		t.Fatalf("events: %v", err)
	}
	asked, noted := 0, 0
	for _, e := range events {
		if e.Decision != nil {
			t.Errorf("a refusal was recorded as a decision: %+v", e.Decision)
		}
		switch e.Kind {
		case core.EventQuestion:
			asked++
		case core.EventNote:
			if strings.Contains(e.Body, "decline") {
				noted++
			}
		}
	}
	if asked != 1 {
		t.Errorf("questions filed = %d, want the executor's question recorded once however often it is judged", asked)
	}
	if noted != 1 {
		t.Errorf("declines noted = %d, want one: a question nobody answered is what a later reader needs to see", noted)
	}

	// The same bill on the JSON rail, addressable rather than printed. A goal of
	// its own, because the first run came to rest and a goal that has come to
	// rest has no loop to run.
	second, secondTask := drivenGoal(t, f, "Also serve on a port")
	var pass struct {
		Stop       string `json:"stop"`
		Why        string `json:"why"`
		Shipped    bool   `json:"shipped"`
		Judgements int    `json:"judgements"`
		Unanswered []struct{ Work, Question string }
		Unfinished []string `json:"unfinished"`
	}
	if err := json.Unmarshal([]byte(f.runOK(t, "drive", second, "--turns", "1", "--judgements", "1", "--stalled", "0", "--json")), &pass); err != nil {
		t.Fatalf("drive --json: %v", err)
	}
	task = secondTask
	if pass.Shipped || pass.Stop != "budget" {
		t.Errorf("pass = %+v, want a run that admits it did not ship", pass)
	}
	if len(pass.Unanswered) != 1 || pass.Unanswered[0].Work != task || pass.Unanswered[0].Question == "" {
		t.Errorf("unanswered = %+v, want the question and the work waiting on it", pass.Unanswered)
	}
	if len(pass.Unfinished) == 0 {
		t.Errorf("unfinished = %v, want the goal named", pass.Unfinished)
	}

	// Only a goal has a loop to run, and saying so is better than driving a
	// task and reporting a run that never happened.
	if errStr := f.runFail(t, "drive", task); !strings.Contains(errStr, "not a goal") {
		t.Errorf("drive a task: %q, want it refused", errStr)
	}
	if errStr := f.runFail(t, "drive"); !strings.Contains(errStr, "which goal") {
		t.Errorf("drive with no id: %q, want it to ask which goal", errStr)
	}
	// A bound that is not a number is refused rather than read as no bound at
	// all, which would look like a run with no limits. The bounds are read
	// before the goal is looked at, so a mistyped bound is reported as one
	// whatever state the goal is in.
	third, _ := drivenGoal(t, f, "Another untouched one")
	if errStr := f.runFail(t, "drive", third, "--turns", "many"); !strings.Contains(errStr, "is not a number") {
		t.Errorf("--turns many: %q, want a number error", errStr)
	}
	if errStr := f.runFail(t, "drive", goal, "--deadline", "soon"); !strings.Contains(errStr, "is not a duration") {
		t.Errorf("--deadline soon: %q, want a duration error", errStr)
	}
}

func TestAReportCanBeFiledAsTextAndTheGateIsTheSame(t *testing.T) {
	f := newCLIFixture(t)
	// Work the director builds in its own session: no executor, no transcript,
	// and therefore no report to read. Before this there was no way to say the
	// work was done, so it could never be closed.
	built := jsonString(t, f.runOK(t, "add", "sample-app", "Built by the director", "--json"), "id")
	f.runOK(t, "set", built, "running")

	// Told there is nothing to read, and told how to file one anyway. An error
	// that only says no leaves a person with nowhere to go.
	errStr := f.runFail(t, "report", built)
	for _, want := range []string{"no session", "as text", "STATUS: DONE"} {
		if !strings.Contains(errStr, want) {
			t.Errorf("report with no session: stderr %q, want it to name %q", errStr, want)
		}
	}
	if got := f.eventsOf(t, built, core.EventReport); got != 0 {
		t.Errorf("report with no session filed %d reports, want 0", got)
	}

	// A text with no STATUS line says nothing about what happened, so it files
	// nothing: the gate is the same gate, not a looser one.
	if errStr := f.runFail(t, "report", built, "I changed the file and it looks fine."); !strings.Contains(errStr, "no STATUS line") {
		t.Errorf("a report with no status: stderr %q, want it refused", errStr)
	}
	if got := f.eventsOf(t, built, core.EventReport); got != 0 {
		t.Errorf("a report with no STATUS line filed %d reports, want 0", got)
	}
	if got := f.stateOf(t, built); got != core.StateRunning {
		t.Errorf("a refused report left state %s, want running", got)
	}

	// Filed, the text moves the work to review and records where it came from,
	// so a reader comparing two reports need not guess which was read from a
	// session and which was handed over.
	out := f.runOK(t, "report", built, "STATUS: DONE\n\nAdded wd drive and its three new packages.", "--no-reflect", "--json")
	if got := jsonString(t, out, "report"); got != "DONE" {
		t.Errorf("report = %q, want DONE", got)
	}
	if got := jsonString(t, out, "source"); got != "text" {
		t.Errorf("source = %q, want text: a reader has to know this was not read", got)
	}
	if got := f.stateOf(t, built); got != core.StateReview {
		t.Errorf("state = %s, want review", got)
	}
	evs := f.bodies(t, built, core.EventReport)
	if len(evs) != 1 {
		t.Fatalf("%d report events, want 1", len(evs))
	}
	if !strings.HasPrefix(evs[0], "DONE\n") {
		t.Errorf("report body = %q, want it to start with the status the gate reads", evs[0])
	}
	if !strings.Contains(evs[0], "no executor session") || !strings.Contains(evs[0], "STATUS: DONE") {
		t.Errorf("report body = %q, want its provenance and the text both on the record", evs[0])
	}
	// It went through running, and that is on the record too: a report is a
	// thing that has finished, so the ledger has to have seen it run.
	var states []string
	for _, e := range f.events(t, built) {
		if e.Kind == core.EventState {
			states = append(states, e.Body)
		}
	}
	if !slices.Contains(states, string(core.StateRunning)) {
		t.Errorf("states = %v, want running on the way: nothing spawns work the director builds", states)
	}

	// The gate is the same gate. Filing a report is not a way past verify or a
	// pull request; it is only a way to produce the one thing a session-less work
	// item had no way to produce.
	if errStr := f.runFail(t, "soft-done", built); !strings.Contains(errStr, "passing verify") {
		t.Errorf("soft-done with no verify: stderr %q, want the verify gate still standing", errStr)
	}
	f.runOK(t, "verify", built)
	if errStr := f.runFail(t, "soft-done", built); !strings.Contains(errStr, "pull request") {
		t.Errorf("soft-done with no pull request: stderr %q, want the pull-request gate still standing", errStr)
	}
	f.runOK(t, "pr", built, "https://example.test/pr/built-by-the-director")
	f.runOK(t, "soft-done", built)
	if got := jsonString(t, f.runOK(t, "done", built, "--json"), "state"); got != string(core.StateDone) {
		t.Errorf("state = %q, want done: director-built work must be able to close", got)
	}

	// BLOCKED is the same road: the text's status names where the work goes,
	// and a report that is not DONE is still refused at soft-done. Straight from
	// queued, too, since that is where work the director built has always been.
	stuck := jsonString(t, f.runOK(t, "add", "sample-app", "Blocked by hand", "--json"), "id")
	if got := jsonString(t, f.runOK(t, "report", stuck, "STATUS: BLOCKED\n\nWaiting on a decision that is not mine to make.", "--no-reflect", "--json"), "report"); got != "BLOCKED" {
		t.Errorf("report = %q, want BLOCKED", got)
	}
	if got := f.stateOf(t, stuck); got != core.StateBlocked {
		t.Errorf("state = %s, want blocked", got)
	}

	// Work that does have a session says so, because a report read from a
	// session and a report handed over are different records and the reader
	// should not have to work out which one this is.
	spoken := jsonString(t, f.runOK(t, "add", "sample-app", "Has a session", "--json"), "id")
	session := jsonString(t, f.runOK(t, "spawn", spoken, "--runner", "claude", "--json"), "session")
	f.runOK(t, "report", spoken, "STATUS: DONE\n\nCorrecting the transcript.", "--no-reflect", "--json")
	if evs := f.bodies(t, spoken, core.EventReport); len(evs) != 1 || !strings.Contains(evs[0], "not read from session "+session) {
		t.Errorf("report body = %v, want it to name the session whose transcript was not read", evs)
	}

	// Reflection rides on a filed report the same way it rides on a read one,
	// and it cannot lose the report: a model that will not start is recorded on
	// the work, not fatal.
	reflected := jsonString(t, f.runOK(t, "add", "sample-app", "Reflected by hand", "--json"), "id")
	f.runOK(t, "set", reflected, "running")
	out = f.runOK(t, "report", reflected, "STATUS: DONE\n\nFiled with reflection on.", "--json")
	if !strings.Contains(out, `"reflect"`) {
		t.Errorf("report --json = %s, want the reflect result carried on the rail", out)
	}
	if got := f.stateOf(t, reflected); got != core.StateReview {
		t.Errorf("state = %s, want review: reflection is not a gate and cannot move work back", got)
	}

	// A report on work already where the report says it belongs files the fact
	// and moves nothing. Refusing it would lose a record to save a transition
	// that has nowhere to go.
	again := jsonString(t, f.runOK(t, "add", "sample-app", "Already in review", "--json"), "id")
	f.runOK(t, "set", again, "running")
	f.runOK(t, "report", again, "STATUS: DONE\n\nFirst word on it.", "--no-reflect")
	f.runOK(t, "report", again, "STATUS: DONE\n\nA second look found one more thing.", "--no-reflect")
	if got := f.stateOf(t, again); got != core.StateReview {
		t.Errorf("state = %s, want review", got)
	}
	if evs := f.bodies(t, again, core.EventReport); len(evs) != 2 {
		t.Errorf("%d report events, want both: a second report is a new fact", len(evs))
	}
}

// publish gives a repository a remote its branch is actually on, so a commit can
// be pointed at. A gate that only a link nobody can open can satisfy is not a
// gate, so the link the tool works out has to be a real place.
func publish(t *testing.T, repo string) string {
	t.Helper()
	bare := repo + "-origin.git"
	gitRun(t, filepath.Dir(repo), "init", "--bare", "-q", bare)
	gitRun(t, repo, "remote", "add", "origin", bare)
	gitRun(t, repo, "push", "-q", "origin", "HEAD")
	return bare
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		t.Fatalf("rev-parse in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestARunThatLandsEveryTaskClosesTheGoal(t *testing.T) {
	f := newCLIFixture(t)
	publish(t, f.sample)
	driveProject(t, f)
	goal, task := drivenGoal(t, f, "Serve on a fixed port")
	judgeAnswer(t, f, true)

	// Nothing here waits for a person: the task's question is judged, the answer
	// is delivered, the executor reports DONE, the task is put through its gates
	// and then the goal is put through its own.
	out := f.runOK(t, "drive", goal, "--turns", "8", "--judgements", "2", "--poll-seconds", "1")
	if !strings.Contains(out, "shipped and closed") {
		t.Fatalf("drive = %q, want it to say the goal closed itself", out)
	}
	if got := f.workRow(t, task).State; got != core.StateDone {
		t.Errorf("task state = %s, want done: a task that reported DONE has nowhere to sit", got)
	}
	if got := f.workRow(t, goal).State; got != core.StateDone {
		t.Errorf("goal state = %s, want done: a goal whose work all landed is the last thing a person has to do", got)
	}

	// The goal's report is the run's own account and names what it closed. A
	// goal that says it shipped without saying what shipped is a claim, not a
	// record.
	reports := f.bodies(t, goal, core.EventReport)
	if len(reports) != 1 {
		t.Fatalf("%d goal reports, want 1", len(reports))
	}
	if !strings.HasPrefix(reports[0], "DONE\n") {
		t.Errorf("goal report = %q, want it to start with the status the gate reads", reports[0])
	}
	for _, want := range []string{task, "Driven with no human present"} {
		if !strings.Contains(reports[0], want) {
			t.Errorf("goal report = %q, want it to name %q", reports[0], want)
		}
	}
	if got := f.bodies(t, goal, core.EventVerify); len(got) != 1 || !strings.HasPrefix(got[0], "pass\n") {
		t.Errorf("goal verify = %v, want one passing verify", got)
	}
	links := f.bodies(t, goal, core.EventPr)
	if len(links) != 1 {
		t.Fatalf("%d goal links, want 1", len(links))
	}
	if !strings.HasPrefix(links[0], "commit ") {
		t.Errorf("goal link = %q, want it recorded as a commit landing", links[0])
	}
	if want := headOf(t, f.sample); !strings.HasSuffix(links[0], "/commit/"+want) {
		t.Errorf("goal link = %q, want the commit the work landed on (%s)", links[0], want)
	}

	// A goal that came to rest has no loop to run, and running one anyway would
	// report a run that never happened as a run that finished.
	if errStr := f.runFail(t, "drive", goal); !strings.Contains(errStr, "nothing left to run") {
		t.Errorf("drive a closed goal = %q, want it refused", errStr)
	}
}

// A gate that refuses is written on the work and named by the run, and it is
// tried once. Re-running a failing build on a loop is how a run spends its whole
// budget proving the same thing.
func TestARefusedGateIsHeldOnTheWorkAndTriedOnce(t *testing.T) {
	f := newCLIFixture(t)
	publish(t, f.sample)
	driveProject(t, f)
	// Its verify cannot pass, so the gate has something to say.
	path := filepath.Join(f.wdHome, "projects", "driven.md")
	broken := "---\npath: " + f.sample + "\nrunner: driven\nmode: auto\nstack: [go]\nverify: [test -f NOT-THERE]\ndefault_branch: main\n---\nA project that cannot pass its own verify.\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
	goal, task := drivenGoal(t, f, "Cannot be verified")
	judgeAnswer(t, f, true)

	out := f.runOK(t, "drive", goal, "--turns", "10", "--judgements", "2", "--poll-seconds", "1")
	if !strings.Contains(out, "waiting on a gate") || !strings.Contains(out, task) {
		t.Errorf("drive = %q, want the work and the gate it is held at named", out)
	}
	if got := f.workRow(t, task).State; got == core.StateDone {
		t.Errorf("task state = %s, want it held: the gate said no", got)
	}
	notes := f.bodies(t, task, core.EventNote)
	var stops int
	for _, n := range notes {
		if strings.HasPrefix(n, "drive stopped at ") {
			stops++
			if !strings.Contains(n, "verify") {
				t.Errorf("note = %q, want it to name the gate", n)
			}
		}
	}
	if stops != 1 {
		t.Errorf("%d stop notes, want 1: a refused gate is tried once, not every turn", stops)
	}
	// A goal with a task the gate would not let past has not shipped, and the run
	// ends it saying so rather than quietly.
	if got := f.workRow(t, goal).State; got != core.StateAbandoned {
		t.Errorf("goal state = %s, want abandoned: its work did not land", got)
	}
}

func TestTheLandingLinkIsDerivedRatherThanTyped(t *testing.T) {
	f := newCLIFixture(t)

	// A url given is recorded as given: a real pull request is still a real pull
	// request, and the tool has no business second-guessing one.
	given := jsonString(t, f.runOK(t, "add", "sample-app", "A real pull request", "--json"), "id")
	f.runOK(t, "pr", given, "https://example.test/pr/1")
	// The url is recorded unedited, and the kind is the one the command knew: a
	// url handed to wd pr is a pull request, and nothing downstream has to guess
	// that from its shape.
	if got := f.bodies(t, given, core.EventPr); len(got) != 1 || got[0] != "pull-request https://example.test/pr/1" {
		t.Errorf("link = %v, want the url given with the kind recorded", got)
	}

	// Pushed: the link is worked out, and it is the commit the work is at.
	remote := publish(t, f.sample)
	pushed := jsonString(t, f.runOK(t, "add", "sample-app", "Landed on main", "--json"), "id")
	f.runOK(t, "pr", pushed)
	links := f.bodies(t, pushed, core.EventPr)
	if len(links) != 1 {
		t.Fatalf("%d links, want 1", len(links))
	}
	// Recorded as a commit, because that is what it is: the change is already in
	// the product, and the record says so rather than leaving a reader to notice.
	if !strings.HasPrefix(links[0], "commit ") {
		t.Errorf("link = %q, want it recorded as a commit landing", links[0])
	}
	if want := headOf(t, f.sample); !strings.HasSuffix(links[0], "/commit/"+want) {
		t.Errorf("link = %q, want the commit at %s", links[0], want)
	}
	if !strings.Contains(links[0], strings.TrimSuffix(remote, ".git")) {
		t.Errorf("link = %q, want it on the project's own remote %s", links[0], remote)
	}

	// Not pushed: there is nowhere to point at, and a link to a commit nobody
	// has pushed does not open. The work is told so and left alone.
	if err := os.WriteFile(filepath.Join(f.sample, "NEW.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitRun(t, f.sample, "add", ".")
	gitRun(t, f.sample, "commit", "-qm", "unpushed")
	unpushed := jsonString(t, f.runOK(t, "add", "sample-app", "Not pushed yet", "--json"), "id")
	errStr := f.runFail(t, "pr", unpushed)
	for _, want := range []string{"not pushed", headOf(t, f.sample)[:8], "wd pr " + unpushed} {
		if !strings.Contains(errStr, want) {
			t.Errorf("pr on unpushed work = %q, want it to name %q", errStr, want)
		}
	}
	if got := f.bodies(t, unpushed, core.EventPr); len(got) != 0 {
		t.Errorf("link = %v, want nothing recorded: a dead link is not a landing", got)
	}

	// No remote at all is the same answer for the same reason.
	lonely := filepath.Join(f.dir, "lonely")
	if err := os.MkdirAll(lonely, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	gitRun(t, lonely, "init", "-q", "-b", "main")
	gitRun(t, lonely, "config", "user.email", "fixture@work-director")
	gitRun(t, lonely, "config", "user.name", "Fixture")
	if err := os.WriteFile(filepath.Join(lonely, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitRun(t, lonely, "add", ".")
	gitRun(t, lonely, "commit", "-qm", "init")
	alone := filepath.Join(f.wdHome, "projects", "lonely.md")
	if err := os.WriteFile(alone, []byte("---\npath: "+lonely+"\nrunner: claude\nmode: auto\nstack: [go]\nverify: [test -f README.md]\ndefault_branch: main\n---\nNo remote.\n"), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
	nowhere := jsonString(t, f.runOK(t, "add", "lonely", "No remote to point at", "--json"), "id")
	if errStr := f.runFail(t, "pr", nowhere); !strings.Contains(errStr, "no git remote") {
		t.Errorf("pr with no remote = %q, want it to say there is nowhere to point", errStr)
	}

	// Both remote forms have to work, because which one a person has is not
	// something the tool should care about, and a gate that only reads one is a
	// gate that fails for half the people using it.
	sha := strings.Repeat("a", 40)
	for remote, want := range map[string]string{
		"git@github.com:owner/repo.git":       "https://github.com/owner/repo/commit/" + sha,
		"ssh://git@gitlab.test:22/owner/repo": "https://gitlab.test/22/owner/repo/commit/" + sha,
		"https://github.com/owner/repo.git":   "https://github.com/owner/repo/commit/" + sha,
		"git://github.com/owner/repo.git":     "git://github.com/owner/repo/commit/" + sha,
	} {
		if got := commitLink(remote, sha); got != want {
			t.Errorf("commitLink(%q) = %q, want %q", remote, got, want)
		}
	}

	// Verify and the link resolve the work's directory the same way, or a goal
	// could be verified in one tree and linked from another — which is a gate
	// that checks one thing and a reader is shown another.
	verifyPath := filepath.Join(f.wdHome, "projects", "in-tree.md")
	sharedProject := "---\npath: " + f.sample + "\nrunner: claude\nmode: auto\nstack: [go]\nverify: [pwd]\ndefault_branch: main\n---\nVerify reports where it ran.\n"
	if err := os.WriteFile(verifyPath, []byte(sharedProject), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
	inTree := jsonString(t, f.runOK(t, "goal", "add", "in-tree", "A goal with its own tree", "--json"), "id")
	f.runOK(t, "goal", "spawn", inTree, "--runner", "claude")
	var wts []core.Worktree
	if err := json.Unmarshal([]byte(f.runOK(t, "worktree", "list", inTree, "--json")), &wts); err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	tree := ""
	for _, wt := range wts {
		if wt.Kind == core.WorktreeShared {
			tree = wt.Path
		}
	}
	if tree == "" {
		t.Fatalf("worktree list = %v, want the goal's shared tree", wts)
	}
	if err := os.WriteFile(filepath.Join(tree, "IN-TREE.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write in tree: %v", err)
	}
	gitRun(t, tree, "add", ".")
	gitRun(t, tree, "commit", "-qm", "the goal's own commit")
	gitRun(t, tree, "push", "-q", "origin", "HEAD")
	f.runOK(t, "verify", inTree)
	if got := f.bodies(t, inTree, core.EventVerify); len(got) != 1 || !strings.Contains(got[0], tree) {
		t.Errorf("verify = %v, want it run in the goal's own tree %s", got, tree)
	}
	f.runOK(t, "pr", inTree)
	if got := f.bodies(t, inTree, core.EventPr); len(got) != 1 || !strings.HasSuffix(got[0], "/commit/"+headOf(t, tree)) {
		t.Errorf("link = %v, want the goal's own commit %s in its own tree %s", got, headOf(t, tree), tree)
	}
	if headOf(t, tree) == headOf(t, f.sample) {
		t.Fatal("the goal's tree shares the project's commit, so this proves nothing")
	}
}

// tasteCheckout is a taste build in a directory of its own: a real checkout for
// the loop to write a card into and commit, so a test of promotion never writes
// into the repository the tests are running from. It holds one project-scoped
// adopted card and nothing else, because a rule with no evidence is never a
// candidate and would make the test prove nothing.
func seedTasteCheckout(t *testing.T, f *cliFixture) (root, cardID string, env []string) {
	t.Helper()
	root = filepath.Join(f.dir, "taste-checkout")
	cards := filepath.Join(root, "taste", "cards", "organization")
	if err := os.MkdirAll(cards, 0o755); err != nil {
		t.Fatalf("mkdir cards: %v", err)
	}
	// The presets are the real ones, copied in: the build resolves every card's
	// enforce ids against them, and a checkout without them is not a checkout
	// the build can run in.
	if err := copyDirRecursive(filepath.Join(repoRoot(t), "presets"), filepath.Join(root, "presets")); err != nil {
		t.Fatalf("copy presets: %v", err)
	}
	cardID = "watch-the-shared-worktree"
	body := "---\nid: " + cardID + "\ntitle: Wait for the worktree before writing into it\ncategory: organisation\nscope: [project:driven]\nkind: practice\nstatus: adopted\nalways: false\nenforce: []\nevidence: []\n---\nA shared worktree has one working tree, so a second send into it races\nthe first.\n"
	if err := os.WriteFile(filepath.Join(cards, cardID+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write card: %v", err)
	}
	gitRun(t, root, "init", "-b", "main", "-q")
	gitRun(t, root, "config", "user.email", "fixture@work-director")
	gitRun(t, root, "config", "user.name", "Fixture")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "the taste as it was")
	// The environment a run needs to find this checkout rather than the real one.
	env = f.env(t, f.bin)
	for i, e := range env {
		if strings.HasPrefix(e, "WD_ROOT=") {
			env[i] = "WD_ROOT=" + root
		}
	}
	return root, cardID, env
}

// Under autonomy nothing waits on a person, and a card promoted to the global
// taste is the loudest thing the loop can change: every future session in every
// project reads it. So the loop judges it on its own judgement, spends that
// judgement from the same bound, records the decision on the work that showed
// the pattern, and says what it did in its own output.
func TestTheLoopDecidesForItselfWhatBecomesGlobal(t *testing.T) {
	f := newCLIFixture(t)
	driveProject(t, f)
	root, cardID, env := seedTasteCheckout(t, f)
	goal, task := drivenGoal(t, f, "Judge the card yourself")
	f.runEnvOK(t, env, "feedback", "add", "the rule held on three separate runs",
		"--card", cardID, "--project", "driven", "--source", "attached", "--work", task)
	judgeAnswer(t, f, true)

	out := f.runEnvOK(t, env, "drive", goal, "--turns", "1", "--judgements", "2",
		"--stalled", "0", "--poll-seconds", "1")

	// The card is global and adopted, written by the judgement rather than
	// proposed for a person to sign off. That is the whole difference between
	// this and a command somebody has to remember to run.
	global := filepath.Join(root, "taste", "cards", "organisation", cardID+"-global.md")
	card, err := os.ReadFile(global)
	if err != nil {
		t.Fatalf("no global card written: %v", err)
	}
	for _, want := range []string{"status: adopted", "scope: [global]", "id: " + cardID + "-global"} {
		if !strings.Contains(string(card), want) {
			t.Errorf("global card is missing %q:\n%s", want, card)
		}
	}
	// The artifacts are rebuilt from it, or the belief changed on disk and never
	// reached a session: the plugin is how taste travels.
	if _, err := os.Stat(filepath.Join(root, "plugin", "constitution.md")); err != nil {
		t.Errorf("the plugin was not rebuilt from the promoted card: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "AGENTS.fragment.md")); err != nil {
		t.Errorf("the agents fragment was not rebuilt from the promoted card: %v", err)
	}
	// The change is committed, staged to the card and the artifacts and nothing
	// else, so the loop cannot sweep in unrelated work from the tree.
	head := gitOut(t, root, "log", "-1", "--pretty=%s")
	if !strings.Contains(head, cardID) {
		t.Errorf("HEAD = %q, want a commit naming the card it promoted", head)
	}
	if dirty := gitOut(t, root, "status", "--porcelain"); dirty != "" {
		t.Errorf("the checkout is left dirty:\n%s", dirty)
	}
	if body := gitOut(t, root, "show", "--stat", "--pretty=", "HEAD"); strings.Contains(body, "sample-app") {
		t.Errorf("the promotion commit touched something outside the taste:\n%s", body)
	}
	// The decision is on the work that showed the pattern, as a structured claim
	// naming the card, so a review reads why the taste changed rather than only
	// that it did.
	events := f.events(t, task)
	found := false
	for _, e := range events {
		if e.Kind != core.EventDecision || e.Decision == nil {
			continue
		}
		if strings.Contains(e.Decision.Question, cardID) {
			found = true
			if e.Decision.Answer == "" {
				t.Error("the recorded claim has no answer, so it is not a decision anybody can read")
			}
			if e.Decision.Source != "judge" {
				t.Errorf("claim source = %q, want judge: a promotion decided by the loop is not a person's claim", e.Decision.Source)
			}
		}
	}
	if !found {
		t.Errorf("no decision naming %s on %s: a promotion with no claim on it cannot be audited", cardID, task)
	}
	// The run says what it changed about what the loop believes. A loop that
	// promotes a card in silence leaves the largest thing it did to be found
	// later by somebody diffing the taste.
	if !strings.Contains(out, "promoted to the global taste: "+cardID) {
		t.Errorf("run output does not name what it promoted:\n%s", out)
	}
	// And the card is a candidate no more, so a later run cannot promote it twice.
	if scan := f.runEnvOK(t, env, "scan"); strings.Contains(scan, cardID) {
		t.Errorf("wd scan still offers the promoted card:\n%s", scan)
	}
}

// A decline is an answer about a card too. The card stays a candidate, nothing is
// written, nothing is committed, and the run says it looked — rather than
// promoting a rule a model said it could not place, or leaving a reader to
// wonder whether the loop ever saw the card at all.
func TestACardTheLoopJudgesNotGlobalIsHeldAndNothingIsWritten(t *testing.T) {
	f := newCLIFixture(t)
	driveProject(t, f)
	root, cardID, env := seedTasteCheckout(t, f)
	goal, task := drivenGoal(t, f, "Hold the card")
	f.runEnvOK(t, env, "feedback", "add", "the rule held once",
		"--card", cardID, "--project", "driven", "--source", "attached", "--work", task)
	judgeAnswer(t, f, false)
	before := headOf(t, root)

	out := f.runEnvOK(t, env, "drive", goal, "--turns", "1", "--judgements", "2",
		"--stalled", "0", "--poll-seconds", "1")

	if _, err := os.Stat(filepath.Join(root, "taste", "cards", "organisation", cardID+"-global.md")); err == nil {
		t.Error("a global card was written for a judgement that declined")
	}
	if after := headOf(t, root); after != before {
		t.Errorf("HEAD moved from %s to %s, want nothing committed for a held card", before, after)
	}
	if !strings.Contains(out, "judged not global, left as it is: "+cardID) {
		t.Errorf("run output does not name the card it held:\n%s", out)
	}
	// The refusal is on the work the judgement was recorded against, and it is
	// not a claim: the ledger never gains a decision nobody made.
	events := f.events(t, task)
	claims, notes := 0, 0
	for _, e := range events {
		if e.Kind == core.EventDecision && e.Decision != nil {
			claims++
		}
		if e.Kind == core.EventNote && strings.Contains(e.Body, cardID) {
			notes++
		}
	}
	if claims != 0 {
		t.Errorf("%d claims recorded for a declined judgement, want none", claims)
	}
	if notes == 0 {
		t.Error("a declined card is not on the record at all, so a reader cannot tell the loop looked")
	}
	// Still a candidate, so a run with more evidence can ask again.
	if scan := f.runEnvOK(t, env, "scan"); !strings.Contains(scan, cardID) {
		t.Errorf("a held card is no longer a candidate:\n%s", scan)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}
