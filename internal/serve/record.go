package serve

import (
	"fmt"
	"net/http"
	"regexp"
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
		if kind == "decision" {
			if what, err = s.reshapeWords(t, what); err != nil {
				return recordView{}, err
			}
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
		if m := finishDecidedRe.FindStringSubmatch(e.Body); m != nil {
			by := byDirector
			if m[2] == string(core.FinishFromPerson) {
				by = byYou
			}
			return "finish", "Finishes at " + finishWords(core.FinishLine(m[1])) + ", " + finishSourceWords[core.FinishSource(m[2])], by, true
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
		what, ok := reportWords[strings.ToLower(r.Verdict)]
		if !ok {
			what = "Reported " + strings.ToLower(r.Verdict)
		}
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

// reportWords opens a report's record entry by its verdict.
var reportWords = map[string]string{
	"done":        "Reported finished",
	"blocked":     "Reported stuck",
	"needs-input": "Asked for your input",
}

// finishDecidedRe reads the decision the ledger files when a finish line is
// set: "finishes at: <line> (from <source>)".
var finishDecidedRe = regexp.MustCompile(`^finishes at: (.+) \(from (\w+)\)$`)

// finishSourceWords says where a finish line came from.
var finishSourceWords = map[core.FinishSource]string{
	core.FinishFromGoal:   "as the goal asks",
	core.FinishFromRepo:   "as the repository is set up",
	core.FinishFromTaste:  "as your taste says",
	core.FinishFromPerson: "as you chose",
}

// finishWords names a finish line in a sentence.
func finishWords(line core.FinishLine) string {
	switch line {
	case core.FinishLocalBuild:
		return "a local build"
	case core.FinishPR:
		return "a complete pull request"
	case core.FinishDiscovery:
		return "a discovery report"
	}
	return string(line)
}

// The decisions the director files when it reshapes a goal or settles part
// of it, each naming work by id.
var (
	addedRe   = regexp.MustCompile(`^added ([0-9a-f]+): (.*)$`)
	editedRe  = regexp.MustCompile(`^edited ([0-9a-f]+): (.*)$`)
	droppedRe = regexp.MustCompile(`^dropped ([0-9a-f]+): (.*)$`)
	mergedRe  = regexp.MustCompile(`^merged ([0-9a-f]+) into ([0-9a-f]+)$`)
	splitRe   = regexp.MustCompile(`^split ([0-9a-f]+): [0-9a-f]+ is now a sub-goal holding it$`)
	waitsRe   = regexp.MustCompile(`^([0-9a-f]+) waits on: (.*)$`)
	courseRe  = regexp.MustCompile(`^changed course: (.*) \| done when: (.*) \| finish: (.*)$`)
)

// reshapeWords says a decision the director filed in plain words, naming work
// by its title; any other decision reads as it was written.
func (s *Server) reshapeWords(t tree, what string) (string, error) {
	quoted := func(id string) (string, error) {
		if w, ok := t.byID[id]; ok {
			return "“" + w.Title + "”", nil
		}
		known, err := s.ledger.Has(id)
		if err != nil || !known {
			return "a sub-goal that no longer exists", err
		}
		w, err := s.ledger.Get(id)
		return "“" + w.Title + "”", err
	}
	if m := addedRe.FindStringSubmatch(what); m != nil {
		return "Added the sub-goal “" + m[2] + "”", nil
	}
	if m := editedRe.FindStringSubmatch(what); m != nil {
		name, err := quoted(m[1])
		if err != nil {
			return "", err
		}
		parts := strings.Split(m[2], "; ")
		for i, p := range parts {
			if title, ok := strings.CutPrefix(p, "title: "); ok {
				parts[i] = "renamed “" + title + "”"
			}
		}
		return "Changed " + name + ": " + strings.Join(parts, "; "), nil
	}
	if m := droppedRe.FindStringSubmatch(what); m != nil {
		name, err := quoted(m[1])
		return "Dropped " + name + ": " + m[2], err
	}
	if m := mergedRe.FindStringSubmatch(what); m != nil {
		from, err := quoted(m[1])
		if err != nil {
			return "", err
		}
		into, err := quoted(m[2])
		return "Merged " + from + " into " + into, err
	}
	if m := splitRe.FindStringSubmatch(what); m != nil {
		name, err := quoted(m[1])
		return "Made " + name + " a sub-goal of its own, so more can sit beside it", err
	}
	if m := waitsRe.FindStringSubmatch(what); m != nil {
		name, err := quoted(m[1])
		if err != nil {
			return "", err
		}
		if m[2] == "none" {
			return name + " waits on nothing", nil
		}
		var names []string
		for _, id := range strings.Split(m[2], ", ") {
			n, err := quoted(id)
			if err != nil {
				return "", err
			}
			names = append(names, n)
		}
		return name + " waits on " + andList(names), nil
	}
	if m := courseRe.FindStringSubmatch(what); m != nil {
		return "Changed course to “" + m[1] + "”, done when " + m[2] + ", finishing at " + finishWords(core.FinishLine(m[3])), nil
	}
	if n, ok := strings.CutPrefix(what, core.ApprovedPrefix); ok {
		return "Approved the plan: " + n, nil
	}
	if gt, ok := strings.CutPrefix(what, "classified "); ok {
		return "Classified as " + gt, nil
	}
	if text, ok := strings.CutPrefix(what, "applied the plan change:"); ok && text == "" {
		return "Applied the plan change", nil
	}
	return what, nil
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
