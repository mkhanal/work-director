// Package tui is the director's terminal UI: a stateless adapter over the
// core and the ledger. Every frame is rendered from a fresh read of the
// ledger and the runners' own transcript stores; the TUI keeps no state of
// its own.
package tui

import (
	"fmt"
	"slices"
	"strings"

	"wd/internal/core"
	"wd/internal/runner"
)

// BoardRow is one board line: a goal or epic with its task rollup, or a
// standalone work item.
type BoardRow struct {
	Work   core.Work
	Rollup Rollup
	Epic   bool
}

// Rollup is a goal's per-state task counts.
type Rollup struct {
	Counts map[core.State]int
	Done   int
	Total  int
}

// Board is the board view's data: the rows and the director's home.
type Board struct {
	Rows []BoardRow
	Home string
}

// stateOrder is the fixed order rollups render states in.
var stateOrder = []core.State{
	core.StateRunning, core.StateReview, core.StateNeedsInput, core.StateBriefed,
	core.StateQueued, core.StateBlocked, core.StateSoftDone, core.StateDone, core.StateDropped,
}

// NewBoard rolls the ledger's work items into board rows: every goal and
// epic with its task rollup, then the standalone open work.
func NewBoard(items []core.Work, tasks func(string) ([]core.Work, error)) (Board, error) {
	b := Board{Rows: []BoardRow{}}
	for _, w := range items {
		if !core.IsEpic(w.Kind) {
			continue
		}
		ts, err := tasks(w.ID)
		if err != nil {
			return Board{}, err
		}
		b.Rows = append(b.Rows, BoardRow{Work: w, Rollup: rollup(ts), Epic: true})
	}
	for _, w := range items {
		if core.IsEpic(w.Kind) || w.Parent != nil {
			continue
		}
		if w.State == core.StateDone || w.State == core.StateDropped {
			continue
		}
		b.Rows = append(b.Rows, BoardRow{Work: w})
	}
	return b, nil
}

// rollup counts a goal's tasks per state.
func rollup(tasks []core.Work) Rollup {
	r := Rollup{Counts: map[core.State]int{}}
	for _, t := range tasks {
		r.Counts[t.State]++
		r.Total++
		if t.State == core.StateDone {
			r.Done++
		}
	}
	return r
}

// String renders the rollup: done/total plus the per-state counts.
func (r Rollup) String() string {
	if r.Total == 0 {
		return "no tasks"
	}
	parts := []string{fmt.Sprintf("%d/%d done", r.Done, r.Total)}
	for _, st := range stateOrder {
		if n := r.Counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", st, n))
		}
	}
	return strings.Join(parts, " · ")
}

// SessionView is the live picture of one attached session: the re-attached
// handle, the runner's status and the transcript's messages.
type SessionView struct {
	Handle   runner.Handle
	Status   runner.RunnerStatus
	Messages []string
	Err      string
}

// Detail is the detail view's data for one work item.
type Detail struct {
	Work     core.Work
	Tasks    []core.Work
	Events   []core.Event
	Concerns []core.Concern
	Session  *SessionView
}

// ViewName is which screen the TUI shows.
type ViewName string

const (
	ViewBoard  ViewName = "board"
	ViewDetail ViewName = "detail"
)

// Model is the TUI's ephemeral UI state. It holds no ledger data: every frame
// re-reads it from the source.
type Model struct {
	View     ViewName
	Selected int
	WorkID   string
	Input    string
	Sending  bool
	Status   string
	Width    int
	Height   int
}

// BoardFrame renders the board screen: the goal rows with their rollups, then
// the standalone work, then the key help. Rows beyond the terminal's height
// scroll so the selected row stays on screen.
func BoardFrame(b Board, m Model) []string {
	var body []string
	selStart, selEnd := -1, -1
	for _, sec := range []struct {
		title string
		epic  bool
	}{{"Goals", true}, {"Standalone", false}} {
		titled := false
		for i, r := range b.Rows {
			if r.Epic != sec.epic {
				continue
			}
			if !titled {
				body = append(body, sec.title)
				titled = true
			}
			if i == m.Selected {
				selStart = len(body)
			}
			body = append(body, boardRow(r, m, i)...)
			if i == m.Selected {
				selEnd = len(body)
			}
		}
	}
	out := []string{fmt.Sprintf("wd · board · %s", b.Home), ""}
	out = append(out, scroll(body, m.Height-len(out)-2, selStart, selEnd)...)
	return append(out, "", footer(m))
}

// scroll windows lines to at most height rows, keeping lines[lo:hi] (the
// selection) in view, and its first line when it is taller than the window.
// A negative lo means no selection.
func scroll(lines []string, height, lo, hi int) []string {
	height = max(height, 0)
	if len(lines) <= height {
		return lines
	}
	start := 0
	if hi > height {
		start = hi - height
	}
	if lo >= 0 && lo < start {
		start = lo
	}
	return lines[start:min(start+height, len(lines))]
}

// boardRow renders one selection-marked board row with its meta lines.
func boardRow(r BoardRow, m Model, i int) []string {
	mark := " "
	if i == m.Selected {
		mark = ">"
	}
	w := r.Work
	lines := []string{fmt.Sprintf("%s %s  %-11s %s", mark, w.ID, w.State, w.Title)}
	meta := fmt.Sprintf("  %s · %s", w.Project, w.Kind)
	if r.Epic {
		meta += " · " + r.Rollup.String()
	}
	lines = append(lines, meta)
	if w.Runner != nil && w.Session != nil {
		lines = append(lines, fmt.Sprintf("  %s:%s", *w.Runner, *w.Session))
	}
	return lines
}

// DetailFrame renders the detail screen: the work item's ledger — tasks,
// events, concerns — and, when it has a session, the live transcript. The
// sections share the terminal's height (see share).
func DetailFrame(d Detail, m Model) []string {
	w := d.Work
	out := []string{
		fmt.Sprintf("wd · %s · %s", w.ID, w.State),
		w.Title,
		fmt.Sprintf("%s · %s", w.Project, w.Kind),
		"",
	}
	budget := m.Height - len(out) - 2 // blank + footer
	if m.Status != "" {
		budget -= 2
	}
	panels := []panel{
		{title: "Tasks", rows: taskRows(d.Tasks), rank: 3},
		{title: "Events", rows: eventRows(d.Events), rank: 2, newest: true},
		{title: "Concerns", rows: concernRows(d.Concerns), rank: 0},
	}
	if d.Session != nil {
		panels = append(panels, panel{title: "Live transcript · " + sessionLabel(d.Session), rows: transcriptRows(d.Session, m.Width), rank: 1, newest: true})
	}
	for _, p := range share(panels, budget) {
		out = append(out, p.title)
		out = append(out, p.rows...)
	}
	if m.Status != "" {
		out = append(out, "", m.Status)
	}
	out = append(out, "", footer(m))
	return out
}

// panel is one titled detail section. rank orders which panels keep a place
// when the height cannot fit them all (0 first); newest panels keep their last
// rows when cut.
type panel struct {
	title  string
	rows   []string
	rank   int
	newest bool
}

// share fits the non-empty panels into height rows: by rank, each claims its
// title and one row; the rest is dealt one row per round to the panels with
// rows left, so short panels show whole and long ones split the remainder.
// A cut panel's title says how many rows it shows.
func share(panels []panel, height int) []panel {
	var live []panel
	for _, p := range panels {
		if len(p.rows) > 0 {
			live = append(live, p)
		}
	}
	order := make([]int, len(live))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return live[a].rank - live[b].rank })
	shown := make([]int, len(live))
	left := height
	for _, i := range order {
		if left < 2 {
			break
		}
		shown[i] = 1
		left -= 2
	}
	for dealt := true; dealt && left > 0; {
		dealt = false
		for _, i := range order {
			if left > 0 && shown[i] > 0 && shown[i] < len(live[i].rows) {
				shown[i]++
				left--
				dealt = true
			}
		}
	}
	var out []panel
	for i, p := range live {
		n := shown[i]
		if n == 0 {
			continue
		}
		if n < len(p.rows) {
			p.title += fmt.Sprintf(" · %d of %d", n, len(p.rows))
			if p.newest {
				p.rows = p.rows[len(p.rows)-n:]
			} else {
				p.rows = p.rows[:n]
			}
		}
		out = append(out, p)
	}
	return out
}

// sessionLabel names the transcript panel: runner, session and status.
func sessionLabel(s *SessionView) string {
	label := s.Handle.Runner + " · " + s.Handle.Session + " · " + string(s.Status)
	if s.Err != "" {
		label += " · error"
	}
	return label
}

// taskRows renders the epic's tasks.
func taskRows(tasks []core.Work) []string {
	var out []string
	for _, t := range tasks {
		out = append(out, fmt.Sprintf("  %s  %-11s %s", t.ID, t.State, t.Title))
	}
	return out
}

// eventRows renders the work item's events, one line each.
func eventRows(events []core.Event) []string {
	var out []string
	for _, e := range events {
		head := e.Body
		if i := strings.IndexByte(e.Body, '\n'); i >= 0 {
			head = e.Body[:i]
		}
		out = append(out, fmt.Sprintf("  %-8s %s", e.Kind, head))
	}
	return out
}

// concernRows renders the open concerns.
func concernRows(concerns []core.Concern) []string {
	if len(concerns) == 0 {
		return []string{"  (none open)"}
	}
	var out []string
	for _, c := range concerns {
		out = append(out, "  "+c.Text)
	}
	return out
}

// transcriptRows renders the transcript's messages, wrapped to the width.
func transcriptRows(s *SessionView, width int) []string {
	if s.Err != "" {
		return wrap("  "+s.Err, width)
	}
	if len(s.Messages) == 0 {
		return []string{"  (no messages yet)"}
	}
	var out []string
	for _, msg := range s.Messages {
		out = append(out, wrap("  "+msg, width)...)
	}
	return out
}

// footer renders the key help, or the composing line while sending.
func footer(m Model) string {
	if m.Sending {
		return "> " + m.Input + "   [enter] send · [esc] cancel"
	}
	if m.View == ViewDetail {
		return "[s]end · [b]ack · [q] quit"
	}
	return "[j/k] move · [enter] open · [q] quit"
}

// wrap breaks text into lines of at most width runes.
func wrap(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		r := []rune(para)
		if len(r) == 0 {
			out = append(out, "")
			continue
		}
		for len(r) > width {
			out = append(out, string(r[:width]))
			r = r[width:]
		}
		out = append(out, string(r))
	}
	return out
}
