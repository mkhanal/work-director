package serve

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"wd/internal/core"
)

// Who made a record entry.
const (
	byYou      = "you"
	byDirector = "director"
)

// recordSummary counts a goal's record by kind; Yours are the decisions the
// person made, Corrections the course checks that changed course.
type recordSummary struct {
	Decisions          int `json:"decisions"`
	Yours              int `json:"yours"`
	Course             int `json:"course"`
	Corrections        int `json:"corrections"`
	Questions          int `json:"questions"`
	AnsweredByDirector int `json:"answered_by_director"`
	Chores             int `json:"chores"`
	Judged             int `json:"judged"`
}

// recordEntry is one thing on a goal's record. Event is the event it was read
// from, 0 for the goal's first ask.
type recordEntry struct {
	Event int    `json:"event"`
	At    string `json:"at"`
	Kind  string `json:"kind"`
	What  string `json:"what"`
	Where string `json:"where"`
	By    string `json:"by"`
}

// recordView is a goal's record, newest first, with its counts.
type recordView struct {
	Summary recordSummary `json:"summary"`
	Entries []recordEntry `json:"entries"`
}

// record reads a goal's record from the events at every depth under it.
func (s *Server) record(id string) (recordView, error) {
	goal, err := s.work(id)
	if err != nil {
		return recordView{}, err
	}
	if !core.IsGoal(goal.Kind) {
		return recordView{}, notFound{fmt.Sprintf("%s is not a goal", id)}
	}
	top, err := s.topGoal(goal)
	if err != nil {
		return recordView{}, err
	}
	events, err := s.ledger.EventsUnder(id)
	if err != nil {
		return recordView{}, err
	}
	t, err := s.readTree(goal, events)
	if err != nil {
		return recordView{}, err
	}
	paths := t.paths()
	here := goal.Title
	if top {
		here += " (top)"
	}
	where := func(work string) string {
		if work == goal.ID {
			return here
		}
		return paths[work]
	}
	v := recordView{Entries: []recordEntry{}}
	for _, e := range events {
		kind, what, by, ok := recorded(e)
		if !ok {
			continue
		}
		v.Entries = append(v.Entries, recordEntry{Event: e.ID, At: e.At, Kind: kind, What: what, Where: where(e.Work), By: by})
		switch kind {
		case "decision", "finish":
			v.Summary.Decisions++
			if by == byYou {
				v.Summary.Yours++
			}
		case "course":
			v.Summary.Course++
			if verdict, _, _ := strings.Cut(what, ": "); verdict == "correct" || verdict == "pivot" {
				v.Summary.Corrections++
			}
		case "question":
			v.Summary.Questions++
		case "answer":
			if by == byDirector {
				v.Summary.AnsweredByDirector++
			}
		case "chore":
			v.Summary.Chores++
		case "judged":
			v.Summary.Judged++
		}
	}
	slices.SortStableFunc(v.Entries, func(a, b recordEntry) int { return b.Event - a.Event })
	if top {
		v.Entries = append(v.Entries, recordEntry{At: goal.Created, Kind: "asked", What: goal.Title, Where: here, By: byYou})
	}
	return v, nil
}

// recorded reads one event as a record entry: its kind, what it says and who
// made it, or false when it is not on the record.
func recorded(e core.Event) (kind, what, by string, ok bool) {
	firstLine := strings.TrimSpace(strings.SplitN(e.Body, "\n", 2)[0])
	switch e.Kind {
	case core.EventDecision:
		if strings.HasPrefix(e.Body, "finishes at: ") {
			by := byDirector
			if strings.HasSuffix(e.Body, "(from "+string(core.FinishFromPerson)+")") {
				by = byYou
			}
			return "finish", e.Body, by, true
		}
		return "decision", firstLine, decidedBy(e), true
	case core.EventCourse:
		return "course", e.Body, byDirector, true
	case core.EventQuestion:
		return "question", firstLine, byDirector, true
	case core.EventAnswer:
		by := byDirector
		if e.Decision != nil && e.Decision.Source == core.AnsweredByPerson {
			by = byYou
		}
		return "answer", firstLine, by, true
	case core.EventJudged:
		return "judged", e.Body, byDirector, true
	case core.EventReport:
		r := core.ParseReport(e.Body)
		what := strings.ToLower(r.Verdict)
		if r.Said != "" {
			what += ": " + r.Said
		}
		return "report", what, byDirector, true
	case core.EventPr:
		return "delivery", core.ParseLanding(e.Body).URL, byDirector, true
	case core.EventNote:
		if c, ok := strings.CutPrefix(e.Body, "chore: "); ok {
			return "chore", c, byDirector, true
		}
		if why, ok := strings.CutPrefix(e.Body, "superseded: "); ok {
			return "superseded", why, byDirector, true
		}
	}
	return "", "", "", false
}

// decidedBy is who made a decision: a structured one says by its source; a
// one-line one is what a person types, except the lines the ledger files for
// a done-when or a classification.
func decidedBy(e core.Event) string {
	if e.Decision != nil {
		if e.Decision.Source == core.AnsweredByPerson {
			return byYou
		}
		return byDirector
	}
	if strings.HasPrefix(e.Body, "done when: ") || strings.HasPrefix(e.Body, "classified ") {
		return byDirector
	}
	return byYou
}

// handleRecord serves one goal's record.
func (s *Server) handleRecord(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet) {
		return
	}
	v, err := s.record(strings.TrimPrefix(r.URL.Path, "/api/record/"))
	if err != nil {
		writeViewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
