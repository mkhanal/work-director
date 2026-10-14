package core

import "testing"

func TestRollupState(t *testing.T) {
	for _, c := range []struct {
		name     string
		children []State
		want     State
	}{
		{"no children", nil, StateQueued},
		{"needs input wins", []State{StateBlocked, StateRunning, StateNeedsInput}, StateNeedsInput},
		{"blocked over running", []State{StateRunning, StateBlocked, StateDone}, StateBlocked},
		{"briefed is running", []State{StateQueued, StateBriefed}, StateRunning},
		{"review over done", []State{StateDone, StateReview}, StateReview},
		{"review beside running is running", []State{StateRunning, StateReview}, StateRunning},
		{"review beside queued is queued", []State{StateQueued, StateReview}, StateQueued},
		{"soft-done alone", []State{StateSoftDone, StateDone}, StateSoftDone},
		{"review over soft-done", []State{StateSoftDone, StateReview}, StateReview},
		{"all settled with one done", []State{StateDone, StateDropped, StateSuperseded}, StateDone},
		{"all superseded", []State{StateSuperseded, StateSuperseded}, StateSuperseded},
		{"all dropped", []State{StateDropped}, StateQueued},
		{"queued and done", []State{StateQueued, StateDone}, StateQueued},
		{"paused", []State{StatePaused, StateDone}, StateQueued},
		{"abandoned", []State{StateAbandoned, StateDone}, StateQueued},
	} {
		if got := RollupState(c.children); got != c.want {
			t.Errorf("%s: RollupState(%v) = %s, want %s", c.name, c.children, got, c.want)
		}
	}
}
