package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/runner"
)

// The TUI's tests drive the app over a fake source and a scripted fake
// terminal: the view functions are pure, and the app re-reads its source on
// every frame, so no real terminal or runner is needed.

func newLedger(t *testing.T) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func add(t *testing.T, l *ledger.Ledger, project, title string, kind core.WorkKind, parent ...string) core.Work {
	t.Helper()
	opts := ledger.AddOptions{Kind: kind}
	if len(parent) > 0 {
		opts.Parent = &parent[0]
	}
	w, err := l.Add(project, title, opts)
	if err != nil {
		t.Fatalf("add %s: %v", title, err)
	}
	return w
}

func transition(t *testing.T, l *ledger.Ledger, id string, to core.State) {
	t.Helper()
	if _, err := l.Transition(id, to); err != nil {
		t.Fatalf("transition %s → %s: %v", id, to, err)
	}
}

func assertContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("frame missing %q:\n%s", want, text)
	}
}

// fakeSource is a scripted source: it counts its calls so a test can prove the
// app re-reads on every frame, and grows the transcript to prove live refresh.
type fakeSource struct {
	board   Board
	detail  Detail
	grow    bool
	sent    []string
	handles []runner.Handle

	boardCalls  int
	detailCalls int
}

func (f *fakeSource) Board() (Board, error) {
	f.boardCalls++
	return f.board, nil
}

func (f *fakeSource) Detail(id string) (Detail, error) {
	f.detailCalls++
	d := f.detail
	if f.grow && d.Session != nil {
		msgs := make([]string, 0, f.detailCalls)
		for i := 1; i <= f.detailCalls; i++ {
			msgs = append(msgs, fmt.Sprintf("message %d", i))
		}
		d.Session = &SessionView{Handle: d.Session.Handle, Status: d.Session.Status, Messages: msgs}
	}
	return d, nil
}

func (f *fakeSource) Send(id, text string) error {
	f.sent = append(f.sent, text)
	if f.detail.Session != nil {
		f.handles = append(f.handles, f.detail.Session.Handle)
	}
	return nil
}

// fakeTerm is a scripted terminal: pre-loaded keys, captured frames.
type fakeTerm struct {
	keys   chan Key
	frames [][]string
	closed bool
	err    error
}

func newFakeTerm(script []Key) *fakeTerm {
	keys := make(chan Key, len(script)+1)
	for _, k := range script {
		keys <- k
	}
	return &fakeTerm{keys: keys}
}

func (f *fakeTerm) Size() (int, int, error) { return 80, 24, nil }
func (f *fakeTerm) Keys() <-chan Key        { return f.keys }
func (f *fakeTerm) Err() error              { return f.err }
func (f *fakeTerm) Write(lines []string) error {
	f.frames = append(f.frames, lines)
	return nil
}
func (f *fakeTerm) Close() error {
	f.closed = true
	return nil
}

// runApp runs an app over a scripted terminal and returns its frames.
func runApp(t *testing.T, src Source, script []Key) [][]string {
	t.Helper()
	term := newFakeTerm(script)
	app := New(src, term, time.Hour)
	done := make(chan error, 1)
	go func() { done <- app.Run() }()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	return term.frames
}

func frameText(frames [][]string) string {
	if len(frames) == 0 {
		return ""
	}
	return strings.Join(frames[len(frames)-1], "\n")
}

// longDetail is an epic whose event history alone overflows a 24-row terminal.
func longDetail() Detail {
	d := Detail{
		Work:     core.Work{ID: "abc12345", Project: "work-director", Title: "Rewrite the core", Kind: core.WorkEpic, State: core.StateRunning},
		Tasks:    []core.Work{{ID: "task0001", Title: "Port dashboards", State: core.StateBriefed}},
		Concerns: []core.Concern{{Text: "metabase rate limits sync"}},
		Session: &SessionView{
			Handle:   runner.Handle{Runner: "claude", Session: "ses_000001"},
			Status:   runner.StatusRunning,
			Messages: []string{"Here is the plan", "Step one", "latest message"},
		},
	}
	for i := 1; i <= 30; i++ {
		d.Events = append(d.Events, core.Event{Kind: core.EventReport, Body: fmt.Sprintf("event %d", i)})
	}
	return d
}

func TestTui(t *testing.T) {
	t.Run("The Board Renders The Ledger", func(t *testing.T) {
		t.Run("goals with rollups, then standalone open work", func(t *testing.T) {
			l := newLedger(t)
			epic := add(t, l, "work-director", "Rewrite the core", core.WorkEpic)
			t1 := add(t, l, "work-director", "Port dashboards", core.WorkTask, epic.ID)
			t2 := add(t, l, "work-director", "Wire the schema", core.WorkTask, epic.ID)
			standalone := add(t, l, "work-director", "Single static binary", core.WorkTask)
			finished := add(t, l, "work-director", "Finished work", core.WorkTask)
			transition(t, l, t1.ID, core.StateBriefed)
			transition(t, l, t1.ID, core.StateRunning)
			transition(t, l, t1.ID, core.StateReview)
			transition(t, l, t1.ID, core.StateSoftDone)
			transition(t, l, t1.ID, core.StateDone)
			transition(t, l, t2.ID, core.StateBriefed)
			transition(t, l, finished.ID, core.StateBriefed)
			transition(t, l, finished.ID, core.StateRunning)
			transition(t, l, finished.ID, core.StateReview)
			transition(t, l, finished.ID, core.StateSoftDone)
			transition(t, l, finished.ID, core.StateDone)
			ref := "wd/rewrite-the-core"
			if err := l.SetSession(epic.ID, ledger.SessionInfo{Runner: "claude", Session: "ses_epic", Ref: &ref}); err != nil {
				t.Fatalf("set session: %v", err)
			}
			items, err := l.List(ledger.ListFilter{})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			b, err := NewBoard(items, l.Tasks)
			if err != nil {
				t.Fatalf("new board: %v", err)
			}
			if len(b.Rows) != 2 {
				t.Fatalf("rows = %d, want 2 (epic, standalone; done excluded): %+v", len(b.Rows), b.Rows)
			}
			text := strings.Join(BoardFrame(b, Model{Width: 80, Height: 24}), "\n")
			assertContains(t, text, epic.ID)
			assertContains(t, text, "Rewrite the core")
			assertContains(t, text, "1/2 done")
			assertContains(t, text, "briefed 1")
			assertContains(t, text, "claude:ses_epic")
			assertContains(t, text, standalone.ID)
			assertContains(t, text, "Single static binary")
			if strings.Contains(text, "Finished work") {
				t.Fatalf("done standalone is listed:\n%s", text)
			}
			if strings.Index(text, epic.ID) > strings.Index(text, standalone.ID) {
				t.Fatalf("goals must come before standalone work:\n%s", text)
			}
		})
		t.Run("a goal with no tasks shows the rollup as no tasks", func(t *testing.T) {
			l := newLedger(t)
			add(t, l, "work-director", "Plan and slice", core.WorkEpic)
			items, err := l.List(ledger.ListFilter{})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			b, err := NewBoard(items, l.Tasks)
			if err != nil {
				t.Fatalf("new board: %v", err)
			}
			text := strings.Join(BoardFrame(b, Model{Width: 80, Height: 24}), "\n")
			assertContains(t, text, "no tasks")
		})
		t.Run("more rows than the terminal fits keep the selected row on screen", func(t *testing.T) {
			var b Board
			for i := range 30 {
				b.Rows = append(b.Rows, BoardRow{Work: core.Work{ID: fmt.Sprintf("work%04d", i), Project: "p", Title: fmt.Sprintf("Work %d", i), Kind: core.WorkTask, State: core.StateQueued}})
			}
			for _, sel := range []int{0, 15, 29} {
				lines := BoardFrame(b, Model{Selected: sel, Width: 80, Height: 24})
				if len(lines) > 24 {
					t.Fatalf("selected %d: frame is %d lines, taller than the terminal's 24:\n%s", sel, len(lines), strings.Join(lines, "\n"))
				}
				assertContains(t, strings.Join(lines, "\n"), fmt.Sprintf("> work%04d", sel))
				if lines[len(lines)-1] != footer(Model{View: ViewBoard}) {
					t.Fatalf("selected %d: key help is not the last line:\n%s", sel, strings.Join(lines, "\n"))
				}
			}
		})
	})

	t.Run("A Detail View Shows One Work Item's Ledger", func(t *testing.T) {
		t.Run("tasks, events, concerns and the transcript", func(t *testing.T) {
			d := Detail{
				Work:  core.Work{ID: "abc12345", Project: "work-director", Title: "Rewrite the core", Kind: core.WorkEpic, State: core.StateRunning},
				Tasks: []core.Work{{ID: "task0001", Title: "Port dashboards", State: core.StateBriefed}},
				Events: []core.Event{
					{Kind: core.EventState, Body: "briefed"},
					{Kind: core.EventReport, Body: "DONE\nSTATUS: DONE"},
					{Kind: core.EventVerify, Body: "pass"},
				},
				Concerns: []core.Concern{{Text: "metabase rate limits sync"}},
				Session: &SessionView{
					Handle:   runner.Handle{Runner: "claude", Session: "ses_000001", Cwd: "/tmp/x"},
					Status:   runner.StatusRunning,
					Messages: []string{"Here is the plan", "Step one"},
				},
			}
			lines := DetailFrame(d, Model{View: ViewDetail, Width: 80, Height: 24})
			text := strings.Join(lines, "\n")
			assertContains(t, text, "abc12345")
			assertContains(t, text, "running")
			assertContains(t, text, "Rewrite the core")
			assertContains(t, text, "work-director · epic")
			assertContains(t, text, "Port dashboards")
			assertContains(t, text, "DONE")
			assertContains(t, text, "pass")
			assertContains(t, text, "metabase rate limits sync")
			assertContains(t, text, "claude · ses_000001 · running")
			assertContains(t, text, "Here is the plan")
			assertContains(t, text, "Step one")
		})
		t.Run("a work item without a session shows no transcript panel", func(t *testing.T) {
			d := Detail{Work: core.Work{ID: "abc12345", Title: "Queued work", Kind: core.WorkTask, State: core.StateQueued}}
			text := strings.Join(DetailFrame(d, Model{View: ViewDetail, Width: 80, Height: 24}), "\n")
			assertContains(t, text, "abc12345")
			if strings.Contains(text, "Live transcript") {
				t.Fatalf("transcript panel shown without a session:\n%s", text)
			}
		})
		t.Run("a transcript read error is shown, not swallowed", func(t *testing.T) {
			d := Detail{
				Work:    core.Work{ID: "abc12345", Title: "Running work", Kind: core.WorkTask, State: core.StateRunning},
				Session: &SessionView{Handle: runner.Handle{Runner: "claude", Session: "ses_1"}, Status: runner.StatusUnknown, Err: "export failed: no such session"},
			}
			text := strings.Join(DetailFrame(d, Model{View: ViewDetail, Width: 80, Height: 24}), "\n")
			assertContains(t, text, "export failed: no such session")
		})
		t.Run("a long event history leaves room for concerns and the transcript", func(t *testing.T) {
			d := longDetail()
			lines := DetailFrame(d, Model{View: ViewDetail, Width: 80, Height: 24})
			text := strings.Join(lines, "\n")
			if len(lines) > 24 {
				t.Fatalf("frame is %d lines, taller than the terminal's 24:\n%s", len(lines), text)
			}
			assertContains(t, text, "Port dashboards")
			assertContains(t, text, "event 30")
			assertContains(t, text, "metabase rate limits sync")
			assertContains(t, text, "claude · ses_000001 · running")
			if strings.Contains(text, "event 1\n") {
				t.Fatalf("oldest event shown while newer ones were dropped:\n%s", text)
			}
		})
	})

	t.Run("Live Transcripts Refresh From The Runner's Store", func(t *testing.T) {
		t.Run("new messages appear on refresh without restarting", func(t *testing.T) {
			work := core.Work{ID: "abc12345", Project: "p", Title: "Running work", Kind: core.WorkTask, State: core.StateRunning}
			src := &fakeSource{
				board: Board{Rows: []BoardRow{{Work: work}}},
				detail: Detail{
					Work:    work,
					Session: &SessionView{Handle: runner.Handle{Runner: "claude", Session: "ses_1"}, Status: runner.StatusRunning, Messages: []string{"first"}},
				},
				grow: true,
			}
			// enter opens the detail; j refreshes it; q quits.
			frames := runApp(t, src, []Key{
				{Kind: KeyEnter},
				{Kind: KeyRune, Rune: 'j'},
				{Kind: KeyRune, Rune: 'q'},
			})
			if len(frames) != 3 {
				t.Fatalf("frames = %d, want 3", len(frames))
			}
			if strings.Contains(strings.Join(frames[1], "\n"), "message 2") {
				t.Fatalf("first detail frame shows a message that arrived later:\n%s", strings.Join(frames[1], "\n"))
			}
			assertContains(t, frameText(frames), "message 2")
		})
		t.Run("the latest message shows under a long event history", func(t *testing.T) {
			text := strings.Join(DetailFrame(longDetail(), Model{View: ViewDetail, Width: 80, Height: 24}), "\n")
			assertContains(t, text, "Live transcript")
			assertContains(t, text, "latest message")
		})
	})

	t.Run("Quitting And Relaunching Reconstructs The View", func(t *testing.T) {
		t.Run("two runs from the same source render identical frames", func(t *testing.T) {
			work := core.Work{ID: "abc12345", Project: "p", Title: "Running work", Kind: core.WorkTask, State: core.StateRunning}
			src := &fakeSource{
				board:  Board{Rows: []BoardRow{{Work: work}}},
				detail: Detail{Work: work, Session: &SessionView{Handle: runner.Handle{Runner: "claude", Session: "ses_1"}, Status: runner.StatusRunning, Messages: []string{"hi"}}},
			}
			script := []Key{{Kind: KeyEnter}, {Kind: KeyRune, Rune: 'q'}}
			first := runApp(t, src, script)
			second := runApp(t, src, script)
			if !slices.EqualFunc(first, second, func(a, b []string) bool { return slices.Equal(a, b) }) {
				t.Fatalf("relaunch rendered a different view:\n%v\nvs\n%v", first, second)
			}
		})
		t.Run("a relaunch after a ledger change shows the change", func(t *testing.T) {
			work := core.Work{ID: "abc12345", Project: "p", Title: "Running work", Kind: core.WorkTask, State: core.StateRunning}
			src := &fakeSource{board: Board{Rows: []BoardRow{{Work: work}}}}
			first := runApp(t, src, []Key{{Kind: KeyRune, Rune: 'q'}})
			src.board.Rows = append(src.board.Rows, BoardRow{Work: core.Work{ID: "new00001", Title: "New work", Kind: core.WorkTask, State: core.StateQueued}})
			second := runApp(t, src, []Key{{Kind: KeyRune, Rune: 'q'}})
			assertContains(t, frameText(second), "New work")
			if strings.Contains(frameText(first), "New work") {
				t.Fatalf("first run showed work that did not exist yet")
			}
		})
	})

	t.Run("Resume Re-attaches The Ledger's Session", func(t *testing.T) {
		t.Run("sending continues the recorded session", func(t *testing.T) {
			work := core.Work{ID: "abc12345", Project: "p", Title: "Running work", Kind: core.WorkTask, State: core.StateRunning}
			recorded := runner.Handle{Runner: "claude", Session: "ses_recorded", Cwd: "/tmp/x"}
			src := &fakeSource{
				board:  Board{Rows: []BoardRow{{Work: work}}},
				detail: Detail{Work: work, Session: &SessionView{Handle: recorded, Status: runner.StatusRunning, Messages: []string{"hi"}}},
			}
			// enter opens the detail; s starts composing; "more" is typed; enter sends.
			frames := runApp(t, src, []Key{
				{Kind: KeyEnter},
				{Kind: KeyRune, Rune: 's'},
				{Kind: KeyRune, Rune: 'm'},
				{Kind: KeyRune, Rune: 'o'},
				{Kind: KeyRune, Rune: 'r'},
				{Kind: KeyRune, Rune: 'e'},
				{Kind: KeyEnter},
				{Kind: KeyRune, Rune: 'q'},
			})
			if len(src.sent) != 1 || src.sent[0] != "more" {
				t.Fatalf("sent = %v, want [more]", src.sent)
			}
			if len(src.handles) != 1 || src.handles[0].Runner != "claude" || src.handles[0].Session != "ses_recorded" {
				t.Fatalf("send did not re-attach the recorded session: %+v", src.handles)
			}
			assertContains(t, frameText(frames), "sent")
		})
		t.Run("sending without a session reports it", func(t *testing.T) {
			work := core.Work{ID: "abc12345", Project: "p", Title: "Queued work", Kind: core.WorkTask, State: core.StateQueued}
			src := &fakeSource{
				board:  Board{Rows: []BoardRow{{Work: work}}},
				detail: Detail{Work: work},
			}
			frames := runApp(t, src, []Key{
				{Kind: KeyEnter},
				{Kind: KeyRune, Rune: 's'},
				{Kind: KeyRune, Rune: 'q'},
			})
			if len(src.sent) != 0 {
				t.Fatalf("sent without a session: %v", src.sent)
			}
			assertContains(t, frameText(frames), "no session to send to")
		})
	})

	t.Run("The TUI Keeps No State That Matters", func(t *testing.T) {
		t.Run("every frame re-reads the source", func(t *testing.T) {
			work := core.Work{ID: "abc12345", Project: "p", Title: "Running work", Kind: core.WorkTask, State: core.StateRunning}
			src := &fakeSource{
				board:  Board{Rows: []BoardRow{{Work: work}}},
				detail: Detail{Work: work, Session: &SessionView{Handle: runner.Handle{Runner: "claude", Session: "ses_1"}, Status: runner.StatusRunning, Messages: []string{"hi"}}},
			}
			// board, then three detail frames: one board read, three detail reads.
			runApp(t, src, []Key{
				{Kind: KeyEnter},
				{Kind: KeyRune, Rune: 'j'},
				{Kind: KeyRune, Rune: 'j'},
				{Kind: KeyRune, Rune: 'q'},
			})
			if src.boardCalls != 1 {
				t.Fatalf("board reads = %d, want 1 (no board cache)", src.boardCalls)
			}
			if src.detailCalls != 3 {
				t.Fatalf("detail reads = %d, want 3 (no detail cache)", src.detailCalls)
			}
		})
		t.Run("the app writes nothing and persists nothing", func(t *testing.T) {
			work := core.Work{ID: "abc12345", Project: "p", Title: "Running work", Kind: core.WorkTask, State: core.StateRunning}
			src := &fakeSource{board: Board{Rows: []BoardRow{{Work: work}}}}
			term := newFakeTerm([]Key{{Kind: KeyRune, Rune: 'q'}})
			app := New(src, term, time.Hour)
			if err := app.Run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			// The app's only output is its frames; the terminal closes cleanly.
			for _, f := range term.frames {
				if len(f) == 0 || !strings.HasPrefix(f[0], "wd") {
					t.Fatalf("app wrote a non-frame: %v", f)
				}
			}
			if err := term.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if !term.closed {
				t.Fatalf("terminal not closed")
			}
		})
	})
}
