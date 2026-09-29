package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate points HOME, WD_HOME and PATH at fresh temp dirs and returns the
// fake bin dir and WD_HOME.
func isolate(t *testing.T) (bin, wdHome string) {
	t.Helper()
	bin = t.TempDir()
	home := t.TempDir()
	wdHome = filepath.Join(home, ".work-director")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("WD_HOME", wdHome)
	return bin, wdHome
}

func TestWDHomeFailsWithoutAHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("WD_HOME", "")
	if dir, err := WDHome(); err == nil {
		t.Fatalf("WDHome = %q, want an error without HOME", dir)
	}
	t.Setenv("WD_HOME", "/tmp/wd")
	if dir, err := WDHome(); err != nil || dir != "/tmp/wd" {
		t.Fatalf("WDHome = %q, %v, want /tmp/wd", dir, err)
	}
}

func TestClaudeAgentsFailureCarriesStderr(t *testing.T) {
	bin, _ := isolate(t)
	writeFake(t, bin, "claude", `case "$1 $2" in
  "--bg -n") echo "backgrounded · b765c0a2 · $3";;
  "agents --json") echo 'not logged in' >&2; exit 1;;
esac`)
	cwd := t.TempDir()
	if _, err := claude.Status(Handle{Runner: "claude", Session: testSID, Cwd: cwd}); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("status err = %v, want the agents stderr", err)
	}
	start := time.Now()
	_, err := claude.Spawn(SpawnOptions{Cwd: cwd, Name: "n", Brief: "B"})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("spawn err = %v, want the agents stderr", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("spawn took %s: an agents failure must stop the wait", time.Since(start))
	}
}

func TestClaudeTranscriptIsEmptyOnlyWhenTheFileIsMissing(t *testing.T) {
	isolate(t)
	cwd := t.TempDir()
	h := Handle{Runner: "claude", Session: testSID, Cwd: cwd}
	got, err := claude.Transcript(h)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing transcript = %v, %v, want empty", got, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects", projectSlug(cwd), testSID+".jsonl"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := claude.Transcript(h); err == nil {
		t.Fatal("unreadable transcript returned no error")
	}
}

func TestCodexTranscriptRejectsAnUnparseableLine(t *testing.T) {
	_, wdHome := isolate(t)
	dir := filepath.Join(wdHome, "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex-abc123.1.jsonl"), []byte("not json\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if got, err := (codexRunner{}).Transcript(Handle{Runner: "codex", Session: "codex-abc123"}); err == nil {
		t.Fatalf("transcript = %v, want a parse error", got)
	}
}

func TestName20CountsCharacters(t *testing.T) {
	if got := name20(strings.Repeat("é", 25)); got != strings.Repeat("é", 20) {
		t.Fatalf("name20 = %q, want 20 é", got)
	}
}

func TestLoadSpecsFailsOnAnUnreadableDirectory(t *testing.T) {
	root := t.TempDir()
	got, err := loadSpecs(filepath.Join(root, "absent"))
	if err != nil || len(got) != 0 {
		t.Fatalf("absent dir = %v, %v, want no specs", got, err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadSpecs(file); err == nil {
		t.Fatal("a runners path that is a file returned no error")
	}
}

func TestASpecIsRejectedWhenItsRegexesOrPlaceholdersCannotWork(t *testing.T) {
	_, wdHome := isolate(t)
	base := `spawn = "x run {brief}"
session_id = 'session=(\w+)'
`
	for _, c := range []struct{ extra, want string }{
		{"", ""},
		{`status = "x status {session}"` + "\n" + `running = '(unclosed'`, "running"},
		{`status = "x status {session}"` + "\n" + `exited = '[bad'`, "exited"},
		{`status = "x status {text}"`, "{text}"},
		{`send = "x send {brief}"`, "{brief}"},
		{`models = "x models --cwd {cwd}"`, "{cwd}"},
		{`transcript = "cat {log}"`, "detach"},
		{`transcript = "x export ${HOME}"`, "{HOME}"},
	} {
		_, err := WriteSpec("x", base+c.extra+"\n")
		if c.want == "" {
			if err != nil {
				t.Fatalf("valid spec rejected: %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want it to name %s", c.extra, err, c.want)
		}
	}
	if err := os.WriteFile(filepath.Join(wdHome, "runners", "bad.toml"), []byte(`spawn = "x"`+"\n"+`session_id = '(bad'`+"\n"), 0o644); err != nil {
		t.Fatalf("write bad.toml: %v", err)
	}
	if _, err := loadSpecs(filepath.Join(wdHome, "runners")); err == nil || !strings.Contains(err.Error(), "bad.toml") {
		t.Fatalf("loadSpecs err = %v, want bad.toml's regex error", err)
	}
	if _, err := WriteSpec("y", `spawn = "x"`+"\n"+`session_id = '(bad'`+"\n"); err == nil || !strings.Contains(err.Error(), "session_id") {
		t.Fatalf("WriteSpec err = %v, want the session_id regex error", err)
	}
}
