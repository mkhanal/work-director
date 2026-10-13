package serve

import (
	"strings"

	"wd/internal/core"
)

// gateView is the next gate on work's way to done: its name, the argv that
// passes it, and why it is next. Gate is empty when the work waits on none.
type gateView struct {
	Work  string   `json:"work"`
	Gate  string   `json:"gate"`
	Title string   `json:"title,omitempty"`
	Argv  []string `json:"argv"`
	Why   string   `json:"why,omitempty"`
}

// gate reads the work's current run for the gate it waits on: a report is
// verified, a verified run lands, a landed run is soft-done, and soft-done
// work is done.
func (s *Server) gate(id string) (gateView, error) {
	w, err := s.work(id)
	if err != nil {
		return gateView{}, err
	}
	view := gateView{Work: w.ID, Argv: []string{}}
	switch w.State {
	case core.StateSoftDone:
		view.Gate, view.Title, view.Argv, view.Why = "done", "Done", []string{"done", w.ID}, "its checks passed and it is delivered; marking it done closes it"
		return view, nil
	case core.StateReview:
	default:
		return view, nil
	}
	run, err := s.ledger.CurrentRun(w.ID)
	if err != nil {
		return gateView{}, err
	}
	reported, verified, landed := -1, false, false
	for i, e := range run {
		switch e.Kind {
		case core.EventReport:
			reported, verified, landed = i, false, false
		case core.EventVerify:
			verified = i > reported && strings.HasPrefix(e.Body, "pass")
		case core.EventPr:
			landed = true
		}
	}
	switch {
	case !verified:
		view.Gate, view.Title, view.Argv, view.Why = "verify", "Verify", []string{"verify", w.ID}, "its checks haven't passed since it was reported finished"
	case !landed:
		view.Gate, view.Title, view.Argv, view.Why = "land", "Land", []string{"pr", w.ID}, "its checks passed, and it isn't delivered as a pull request or a commit yet"
	default:
		view.Gate, view.Title, view.Argv, view.Why = "soft-done", "Soft-Done", []string{"soft-done", w.ID}, "its checks passed and it is delivered; next, confirm it is finished"
	}
	return view, nil
}
