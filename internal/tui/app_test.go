package tui

import (
	"errors"
	"io"
	"testing"
	"time"

	"wd/internal/core"
)

func TestRunFailsWhenInputStops(t *testing.T) {
	src := &fakeSource{board: Board{Rows: []BoardRow{{Work: core.Work{ID: "abc12345", Title: "Work", Kind: core.WorkTask, State: core.StateQueued}}}}}
	term := newFakeTerm(nil)
	term.err = io.EOF
	close(term.keys)
	err := New(src, term, time.Hour).Run()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("run = %v, want the terminal's input error", err)
	}
}
