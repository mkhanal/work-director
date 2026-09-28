package tui

import (
	"time"
)

// Source is everything the TUI reads: the ledger and the live runner sessions.
// It is the boundary that keeps the TUI stateless and testable.
type Source interface {
	Board() (Board, error)
	Detail(id string) (Detail, error)
	Send(id, text string) error
}

// Terminal is the TUI's input and output boundary.
type Terminal interface {
	Size() (width, height int, err error)
	Keys() <-chan Key
	Write(lines []string) error
	Close() error
}

// App is the TUI: an ephemeral model over a Source and a Terminal.
type App struct {
	src   Source
	term  Terminal
	keys  <-chan Key
	tick  <-chan time.Time
	model Model
	// statusFromFetch marks a status line raised by a failed fetch, so the
	// next successful fetch clears it; action messages survive until the next
	// action or error.
	statusFromFetch bool
}

// New builds an App over a source and a terminal, refreshing every interval.
func New(src Source, term Terminal, interval time.Duration) *App {
	return &App{
		src:   src,
		term:  term,
		keys:  term.Keys(),
		tick:  time.NewTicker(interval).C,
		model: Model{View: ViewBoard, Width: 80, Height: 24},
	}
}

// Open starts the app on a work item's detail view (`wd tui <id>`).
func (a *App) Open(id string) {
	a.model.View = ViewDetail
	a.model.WorkID = id
}

// Run loops — fetch, render, wait for a key or a tick — until the user quits.
func (a *App) Run() error {
	for {
		s, err := a.fetch()
		if err != nil {
			a.model.Status = err.Error()
			a.statusFromFetch = true
		} else if a.statusFromFetch {
			a.model.Status = ""
			a.statusFromFetch = false
		}
		if err := a.render(s); err != nil {
			return err
		}
		select {
		case k := <-a.keys:
			if !a.handle(k, s) {
				return nil
			}
		case <-a.tick:
		}
	}
}

// fetch re-reads the current screen's data from the source.
func (a *App) fetch() (Screen, error) {
	if a.model.View == ViewDetail {
		d, err := a.src.Detail(a.model.WorkID)
		if err != nil {
			return Screen{}, err
		}
		return Screen{Detail: &d}, nil
	}
	b, err := a.src.Board()
	if err != nil {
		return Screen{}, err
	}
	return Screen{Board: &b}, nil
}

// Screen is the data one frame renders: the board or one work item's detail.
type Screen struct {
	Board  *Board
	Detail *Detail
}

// render draws one frame: the view's lines through the terminal.
func (a *App) render(s Screen) error {
	w, h, err := a.term.Size()
	if err == nil {
		a.model.Width, a.model.Height = w, h
	}
	var lines []string
	switch {
	case s.Detail != nil:
		lines = DetailFrame(*s.Detail, a.model)
	case s.Board != nil:
		lines = BoardFrame(*s.Board, a.model)
	default:
		lines = []string{"wd", a.model.Status}
	}
	return a.term.Write(lines)
}

// handle applies one key to the model. It returns false when the app quits.
func (a *App) handle(k Key, s Screen) bool {
	if k.Kind == KeyCtrlC {
		return false
	}
	if a.model.Sending {
		a.handleInput(k)
		return true
	}
	if d := movement(k); d != 0 {
		a.move(d, s)
		return true
	}
	switch k.Kind {
	case KeyEnter:
		if a.model.View == ViewBoard {
			a.openSelected(s.Board)
		}
	case KeyEscape:
		if a.model.View == ViewDetail {
			a.model.View = ViewBoard
		}
	}
	if k.Kind != KeyRune {
		return true
	}
	switch k.Rune {
	case 'q':
		return false
	case 'b':
		if a.model.View == ViewDetail {
			a.model.View = ViewBoard
		}
	case 's':
		if a.model.View == ViewDetail && s.Detail != nil && s.Detail.Session != nil {
			a.model.Sending = true
			a.model.Input = ""
		} else if a.model.View == ViewDetail {
			a.model.Status = "no session to send to"
		}
	}
	return true
}

// handleInput applies one key while composing a message.
func (a *App) handleInput(k Key) {
	switch k.Kind {
	case KeyEscape:
		a.model.Sending = false
		a.model.Input = ""
	case KeyEnter:
		text := a.model.Input
		a.model.Sending = false
		a.model.Input = ""
		if text == "" {
			return
		}
		if err := a.src.Send(a.model.WorkID, text); err != nil {
			a.model.Status = err.Error()
			return
		}
		a.model.Status = "sent"
	case KeyBackspace:
		if r := []rune(a.model.Input); len(r) > 0 {
			a.model.Input = string(r[:len(r)-1])
		}
	case KeyRune:
		a.model.Input += string(k.Rune)
	}
}

// move shifts the board selection by delta, clamped to the rows.
func (a *App) move(delta int, s Screen) {
	if a.model.View != ViewBoard || s.Board == nil {
		return
	}
	n := len(s.Board.Rows)
	if n == 0 {
		return
	}
	a.model.Selected = clamp(a.model.Selected+delta, 0, n-1)
}

// openSelected opens the selected board row's detail view.
func (a *App) openSelected(b *Board) {
	if b == nil || a.model.Selected >= len(b.Rows) {
		return
	}
	a.model.WorkID = b.Rows[a.model.Selected].Work.ID
	a.model.View = ViewDetail
}

// movement normalizes a key to a selection delta: arrows and j/k.
func movement(k Key) int {
	switch k.Kind {
	case KeyUp:
		return -1
	case KeyDown:
		return 1
	case KeyRune:
		switch k.Rune {
		case 'k':
			return -1
		case 'j':
			return 1
		}
	}
	return 0
}

func clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
