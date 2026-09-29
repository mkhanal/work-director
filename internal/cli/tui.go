package cli

import (
	"os"
	"time"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/runner"
	"wd/internal/tui"
)

// tuiSource adapts the CLI's ledger and projects to the TUI's source: every
// read goes back to the ledger, and every send re-attaches the session the
// ledger recorded — never a new spawn.
type tuiSource struct {
	*Cli
}

func (s *tuiSource) Board() (tui.Board, error) {
	items, err := s.Ledger.List(ledger.ListFilter{})
	if err != nil {
		return tui.Board{}, err
	}
	b, err := tui.NewBoard(items, s.Ledger.Tasks)
	if err != nil {
		return tui.Board{}, err
	}
	b.Home = s.Home
	return b, nil
}

func (s *tuiSource) Detail(id string) (tui.Detail, error) {
	w, err := s.Ledger.Get(id)
	if err != nil {
		return tui.Detail{}, err
	}
	d := tui.Detail{Work: w}
	if core.IsEpic(w.Kind) {
		if d.Tasks, err = s.Ledger.Tasks(id); err != nil {
			return tui.Detail{}, err
		}
	}
	if d.Events, err = s.Ledger.Events(id, nil); err != nil {
		return tui.Detail{}, err
	}
	concerns, err := s.Ledger.Concerns(&id)
	if err != nil {
		return tui.Detail{}, err
	}
	for _, c := range concerns {
		if c.Resolved == 0 {
			d.Concerns = append(d.Concerns, c)
		}
	}
	session, err := s.sessionView(id)
	if err != nil {
		return tui.Detail{}, err
	}
	d.Session = session
	return d, nil
}

// sessionView re-attaches the work item's recorded session: runner, session
// id and cwd come from the ledger. No session means no transcript panel.
func (s *tuiSource) sessionView(id string) (*tui.SessionView, error) {
	w, err := s.Ledger.Get(id)
	if err != nil {
		return nil, err
	}
	if w.Session == nil && w.Claim == nil {
		return nil, nil
	}
	h, err := s.handle(id)
	if err != nil {
		return nil, err
	}
	rn, err := runner.RunnerNamed(h.Runner)
	if err != nil {
		return nil, err
	}
	status, err := rn.Status(h)
	if err != nil {
		return &tui.SessionView{Handle: h, Err: err.Error()}, nil
	}
	messages, err := rn.Transcript(h)
	if err != nil {
		return &tui.SessionView{Handle: h, Status: status, Err: err.Error()}, nil
	}
	return &tui.SessionView{Handle: h, Status: status, Messages: messages}, nil
}

// Send continues the work item's recorded session, mirroring `wd send`.
func (s *tuiSource) Send(id, text string) error {
	return s.sendTo(id, text)
}

// tui runs the terminal UI: `wd tui [id]` opens the board, or the work item's
// detail view when an id is given.
func (c *Cli) tui(rest []string) error {
	var id string
	if len(rest) > 0 {
		id = rest[0]
	}
	term, err := tui.NewTerminal(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	defer term.Close()
	app := tui.New(&tuiSource{Cli: c}, term, 2*time.Second)
	if id != "" {
		app.Open(id)
	}
	return app.Run()
}
