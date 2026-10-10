package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"wd/internal/core"
	"wd/internal/ledger"
)

// This file holds the CLI's own fixture: a ledger seeded through the Go
// library, a sample git repo for worktree commands, and fake
// runners. Tests assert the --json schema, key values, error messages and
// exit codes — the contract column consumers read.

type cliFixture struct {
	dir    string
	wdHome string
	home   string
	sample string
	bin    string
	goBin  string
	ids    map[string]string
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if st, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || st.IsDir() {
		t.Fatalf("repo root not found at %s", root)
	}
	return root
}

// newCLIFixture builds a fixture in the test's own temp directory, resolved
// through symlinks: macOS links /var to /private/var, and the fake runners'
// $PWD transcript slug must match the CLI's path.
func newCLIFixture(t *testing.T) *cliFixture {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	f := &cliFixture{
		dir:    dir,
		wdHome: filepath.Join(dir, "wd-home"),
		home:   filepath.Join(dir, "home"),
		sample: filepath.Join(dir, "sample-app"),
		bin:    filepath.Join(dir, "bin"),
		ids:    map[string]string{},
	}
	for _, d := range []string{f.wdHome, f.home, f.bin, filepath.Join(f.wdHome, "projects"), filepath.Join(f.wdHome, "runners")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// Runner spec files (planner, advisor, myagent) for the spec-file runners.
	if err := copyDirRecursive(filepath.Join("testdata", "specs"), filepath.Join(f.wdHome, "runners")); err != nil {
		t.Fatalf("copy specs: %v", err)
	}
	f.seedLedger(t)
	f.seedProject(t)
	f.seedGitRepo(t)
	f.seedRunners(t)
	f.build(t)
	return f
}

func (f *cliFixture) seedLedger(t *testing.T) {
	t.Helper()
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	epic, err := l.Add("sample-app", "Metabase → Superset", ledger.AddOptions{Kind: core.WorkEpic})
	if err != nil {
		t.Fatalf("add epic: %v", err)
	}
	t1, err := l.Add("sample-app", "Port dashboards", ledger.AddOptions{Parent: &epic.ID, Heading: strPtr("Dashboards")})
	if err != nil {
		t.Fatalf("add t1: %v", err)
	}
	t2, err := l.Add("sample-app", "Rewrite ingestion", ledger.AddOptions{Parent: &epic.ID, Heading: strPtr("Data")})
	if err != nil {
		t.Fatalf("add t2: %v", err)
	}
	t3, err := l.Add("sample-app", "Wire the schema", ledger.AddOptions{Parent: &epic.ID, Heading: strPtr("Data")})
	if err != nil {
		t.Fatalf("add t3: %v", err)
	}
	standalone, err := l.Add("sample-app", "Standalone fix", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add standalone: %v", err)
	}
	planEpic, err := l.Add("sample-app", "Plan and slice", ledger.AddOptions{Kind: core.WorkEpic})
	if err != nil {
		t.Fatalf("add planEpic: %v", err)
	}
	f.ids["epic"], f.ids["t1"], f.ids["t2"] = epic.ID, t1.ID, t2.ID
	f.ids["t3"], f.ids["standalone"], f.ids["planEpic"] = t3.ID, standalone.ID, planEpic.ID

	if _, err := l.SetClaim(t1.ID, strPtr("ses_x")); err != nil {
		t.Fatalf("claim t1: %v", err)
	}
	if _, err := l.SetClaim(t2.ID, strPtr("ses_y")); err != nil {
		t.Fatalf("claim t2: %v", err)
	}
	if _, err := l.SetImpact(t1.ID, []string{"src/dashboards"}); err != nil {
		t.Fatalf("impact t1: %v", err)
	}
	if _, err := l.SetImpact(t2.ID, []string{"src/ingest"}); err != nil {
		t.Fatalf("impact t2: %v", err)
	}
	if _, err := l.SetImpact(t2.ID, []string{"src/dashboards"}); err != nil {
		t.Fatalf("impact t2: %v", err)
	}
	if _, err := l.AddConcern(t2.ID, "metabase rate limits sync"); err != nil {
		t.Fatalf("concern: %v", err)
	}
	if _, err := l.ResolveConcern(1, "batch the sync"); err != nil {
		t.Fatalf("resolve concern: %v", err)
	}
	if _, err := l.AddFeedback("rule held up in sample-app", ledger.FeedbackOptions{Card: strPtr("proj-rule"), Project: strPtr("sample-app")}); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	if _, err := l.AddFeedback("rule held up in other-app", ledger.FeedbackOptions{Card: strPtr("proj-rule"), Project: strPtr("other")}); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	if _, err := l.AddFeedback("attached recurrence 1", ledger.FeedbackOptions{Card: strPtr("proj-rule2"), Project: strPtr("sample-app"), Source: core.FeedbackAttached}); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	if _, err := l.AddFeedback("attached recurrence 2", ledger.FeedbackOptions{Card: strPtr("proj-rule2"), Project: strPtr("sample-app"), Source: core.FeedbackAttached}); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	// A shared worktree for the epic and a private one for t2, so spawn commands
	// reuse them instead of creating new git worktrees. The directories are
	// created as real git worktrees by seedGitRepo.
	sharedPath := filepath.Join(f.dir, "worktrees", "sample-app-epic-"+epic.ID)
	if _, err := l.AddWorktree(epic.ID, ledger.WorktreeInfo{Path: sharedPath, Branch: strPtr("wd-" + epic.ID), Kind: core.WorktreeShared, Origin: core.OriginDirector}); err != nil {
		t.Fatalf("add shared worktree: %v", err)
	}
	privatePath := filepath.Join(f.dir, "worktrees", "sample-app-"+t2.ID)
	if _, err := l.AddWorktree(t2.ID, ledger.WorktreeInfo{Path: privatePath, Branch: strPtr("wd-" + t2.ID), Kind: core.WorktreePrivate, Origin: core.OriginDirector}); err != nil {
		t.Fatalf("add private worktree: %v", err)
	}
	// t1 is running with a session and a DONE report, verify and PR.
	if err := l.SetSession(t1.ID, ledger.SessionInfo{Runner: "claude", Session: "ses_000001", Cwd: sharedPath}); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if _, err := l.Transition(t1.ID, core.StateBriefed); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, err := l.Transition(t1.ID, core.StateRunning); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, err := l.AddEvent(t1.ID, core.EventReport, "DONE\nSTATUS: DONE"); err != nil {
		t.Fatalf("report event: %v", err)
	}
	if _, err := l.Transition(t1.ID, core.StateReview); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, err := l.AddEvent(t1.ID, core.EventVerify, "pass\ntest -f README.md → 0"); err != nil {
		t.Fatalf("verify event: %v", err)
	}
	if _, err := l.AddEvent(t1.ID, core.EventPr, "https://github.com/x/sample-app/pull/1"); err != nil {
		t.Fatalf("pr event: %v", err)
	}
	if _, err := l.SoftDone(t1.ID, false); err != nil {
		t.Fatalf("soft-done: %v", err)
	}
	if _, err := l.Transition(t1.ID, core.StateDone); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// t2 is running with a session in its private worktree.
	if err := l.SetSession(t2.ID, ledger.SessionInfo{Runner: "claude", Session: "ses_000002", Cwd: privatePath}); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if _, err := l.Transition(t2.ID, core.StateBriefed); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, err := l.Transition(t2.ID, core.StateRunning); err != nil {
		t.Fatalf("transition: %v", err)
	}
}

func (f *cliFixture) seedProject(t *testing.T) {
	t.Helper()
	proj := "---\npath: " + f.sample + "\nrunner: claude\nmode: ask\nstack: [ts]\nworkflows: [/lazyspec]\nverify: [test -f README.md]\ninstructions_file: AGENTS.md\ndefault_branch: main\n---\nA fixture repo.\n"
	if err := os.WriteFile(filepath.Join(f.wdHome, "projects", "sample-app.md"), []byte(proj), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
}

func (f *cliFixture) seedGitRepo(t *testing.T) {
	t.Helper()
	if err := copyDirRecursive(filepath.Join("testdata", "sample-app"), f.sample); err != nil {
		t.Fatalf("copy sample: %v", err)
	}
	gitRun(t, f.sample, "init", "-b", "main", "-q")
	gitRun(t, f.sample, "config", "user.email", "fixture@work-director")
	gitRun(t, f.sample, "config", "user.name", "Fixture")
	gitRun(t, f.sample, "add", ".")
	gitRun(t, f.sample, "commit", "-qm", "init")
	// The fixture's worktrees, as real git worktrees of the sample repo.
	sharedPath := filepath.Join(f.dir, "worktrees", "sample-app-epic-"+f.ids["epic"])
	gitRun(t, f.sample, "worktree", "add", "-b", "wd-"+f.ids["epic"], sharedPath)
	privatePath := filepath.Join(f.dir, "worktrees", "sample-app-"+f.ids["t2"])
	gitRun(t, f.sample, "worktree", "add", "-b", "wd-"+f.ids["t2"], privatePath, "wd-"+f.ids["epic"])
}

func (f *cliFixture) seedRunners(t *testing.T) {
	t.Helper()
	for _, r := range []string{"claude", "opencode", "codex", "myagent", "planner", "advisor", "driven", "broken", "finder"} {
		data, err := os.ReadFile(filepath.Join("testdata", "runners", r))
		if err != nil {
			t.Fatalf("read runner %s: %v", r, err)
		}
		if err := os.WriteFile(filepath.Join(f.bin, r), data, 0o755); err != nil {
			t.Fatalf("write runner %s: %v", r, err)
		}
	}
}

func (f *cliFixture) build(t *testing.T) {
	t.Helper()
	root := repoRoot(t)
	f.goBin = filepath.Join(f.dir, "wd-go")
	cmd := exec.Command("go", "build", "-o", f.goBin, "./cmd/wd")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
}

// run runs the Go CLI with args and returns exit code, stdout, stderr.
func (f *cliFixture) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return f.runEnv(t, f.env(t, f.bin), args...)
}

// env builds the command environment; binDir is prepended to PATH when set.
func (f *cliFixture) env(t *testing.T, binDir string) []string {
	t.Helper()
	path := os.Getenv("PATH")
	if binDir != "" {
		path = binDir + string(os.PathListSeparator) + path
	}
	return []string{
		"HOME=" + f.home,
		"WD_HOME=" + f.wdHome,
		"WD_PROJECTS=" + filepath.Join(f.wdHome, "projects"),
		"WD_FAKE_STATE=" + filepath.Join(f.dir, "fake-state"),
		"WD_ROOT=" + repoRoot(t),
		"PATH=" + path,
		"VISUAL=",
		"EDITOR=",
	}
}

// runEnv runs the Go CLI with args under env and returns exit code, stdout,
// stderr.
func (f *cliFixture) runEnv(t *testing.T, env []string, args ...string) (int, string, string) {
	t.Helper()
	return f.runAt(t, f.dir, env, args...)
}

// runAt runs the Go CLI with args under env in dir and returns exit code,
// stdout, stderr.
func (f *cliFixture) runAt(t *testing.T, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	return f.runBinAt(t, f.goBin, dir, env, args...)
}

// runBinAt runs the wd binary at bin with args under env in dir and returns
// exit code, stdout, stderr.
func (f *cliFixture) runBinAt(t *testing.T, bin, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	c := exec.Command(bin, args...)
	c.Env = env
	c.Dir = dir
	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run wd %s: %v", strings.Join(args, " "), err)
		}
	}
	return code, stdout.String(), stderr.String()
}

// runOK runs a command expected to succeed and returns stdout.
func (f *cliFixture) runOK(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errStr := f.run(t, args...)
	if code != 0 {
		t.Fatalf("wd %s exited %d\n%s\n%s", strings.Join(args, " "), code, out, errStr)
	}
	return out
}

// runFail runs a command expected to fail and returns stderr.
func (f *cliFixture) runFail(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errStr := f.run(t, args...)
	if code != 1 {
		t.Fatalf("wd %s exited %d (want 1)\n%s\n%s", strings.Join(args, " "), code, out, errStr)
	}
	return errStr
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = os.Environ()
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func copyDirRecursive(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirRecursive(from, to); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// envWithPath is the fixture environment with PATH replaced by path.
func (f *cliFixture) envWithPath(t *testing.T, path string) []string {
	t.Helper()
	env := f.env(t, "")
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + path
		}
	}
	return env
}

// gitOnlyPath is a PATH holding git and nothing else, so no runner CLI
// resolves whatever the host has installed.
func gitOnlyPath(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	if err := os.Symlink(git, filepath.Join(dir, "git")); err != nil {
		t.Fatalf("link git: %v", err)
	}
	return dir
}

// pathWith is a PATH holding git, the named fake runners and /bin (for the
// fakes' bash) — no host-installed runner resolves.
func (f *cliFixture) pathWith(t *testing.T, runners ...string) string {
	t.Helper()
	dir := gitOnlyPath(t)
	for _, r := range runners {
		if err := os.Symlink(filepath.Join(f.bin, r), filepath.Join(dir, r)); err != nil {
			t.Fatalf("link %s: %v", r, err)
		}
	}
	return dir + string(os.PathListSeparator) + "/bin"
}
