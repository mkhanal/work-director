package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/tui"
)

// The TUI source tests run over the CLI fixture's real ledger and projects:
// the board rolls the ledger's work items into rows, and sending re-attaches
// the session the ledger recorded — never a new spawn.

// newTuiSource builds a TUI source over the fixture's ledger and projects.
func newTuiSource(t *testing.T, f *cliFixture) *tuiSource {
	t.Helper()
	l, err := ledger.New(filepath.Join(f.wdHome, "ledger.db"))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	projects, err := project.LoadProjects(filepath.Join(f.wdHome, "projects"))
	if err != nil {
		t.Fatalf("load projects: %v", err)
	}
	c := &Cli{
		Home:        f.wdHome,
		ProjectsDir: filepath.Join(f.wdHome, "projects"),
		Projects:    projects,
		Ledger:      l,
	}
	return &tuiSource{Cli: c}
}

func TestTuiSource(t *testing.T) {
	t.Run("the board rolls the fixture ledger into goal and standalone rows", func(t *testing.T) {
		f := newCLIFixture(t)
		src := newTuiSource(t, f)
		b, err := src.Board()
		if err != nil {
			t.Fatalf("board: %v", err)
		}
		text := strings.Join(tui.BoardFrame(b, tui.Model{Width: 100, Height: 30}), "\n")
		// The epic with its three tasks: one done, one running, one queued.
		assertHasKey(t, text, f.ids["epic"])
		assertHasKey(t, text, "1/3 done")
		assertHasKey(t, text, "running 1")
		// The plan epic has no tasks yet.
		assertHasKey(t, text, f.ids["planEpic"])
		assertHasKey(t, text, "no tasks")
		// The standalone open work.
		assertHasKey(t, text, f.ids["standalone"])
		// t1 is done and sits under the epic: not a board row of its own.
		if strings.Contains(text, "Port dashboards") {
			t.Fatalf("done epic task listed as standalone:\n%s", text)
		}
	})

	t.Run("sending re-attaches the ledger's recorded session", func(t *testing.T) {
		f := newCLIFixture(t)
		// A logging fake claude: it records its argv and answers the resume.
		bin := t.TempDir()
		script := fmt.Sprintf("#!/usr/bin/env bash\necho \"claude $*\" >> %q\ncase \"$1 $2\" in\n  \"--bg --resume\") echo \"backgrounded · bg1\";;\n  \"agents --json\") echo \"[]\";;\n  *) echo \"unhandled claude $*\" >&2; exit 1;;\nesac\n", filepath.Join(bin, "calls.log"))
		if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
			t.Fatalf("write fake claude: %v", err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+f.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("WD_FAKE_STATE", t.TempDir())
		src := newTuiSource(t, f)
		// t2 is running with the recorded session claude:ses_000002.
		if err := src.Send(f.ids["t2"], "more"); err != nil {
			t.Fatalf("send: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(bin, "calls.log"))
		if err != nil {
			t.Fatalf("read calls: %v", err)
		}
		calls := string(data)
		if !strings.Contains(calls, "claude --bg --resume ses_000002 more") {
			t.Fatalf("send did not re-attach the recorded session:\n%s", calls)
		}
		if strings.Contains(calls, "--bg -n") {
			t.Fatalf("send spawned a new session instead of resuming:\n%s", calls)
		}
		// The send is recorded as an event on the work item.
		events, err := src.Ledger.Events(f.ids["t2"], kindPtr(core.EventSent))
		if err != nil {
			t.Fatalf("events: %v", err)
		}
		if len(events) != 1 || events[0].Body != "more" {
			t.Fatalf("sent events = %+v, want one 'more'", events)
		}
	})
}
