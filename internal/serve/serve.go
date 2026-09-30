package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"wd/internal/core"
	"wd/internal/ledger"
)

// DefaultPort is the port serve binds to when --port is not given.
const DefaultPort = 8787

// Server is the loopback HTTP and WebSocket adapter over the core and
// ledger. It serves the board, work items, events and actions, and
// broadcasts live events to WebSocket clients.
type Server struct {
	ledger  *ledger.Ledger
	cliPath string
	hub     *wsHub
}

// New creates a serve server over the given ledger; cliPath is the path to
// the wd CLI binary.
func New(l *ledger.Ledger, cliPath string) *Server {
	return &Server{
		ledger:  l,
		cliPath: cliPath,
		hub:     newWSHub(),
	}
}

// rollup is a goal's status rolled up from its children.
type rollup struct {
	Counts map[core.State]int `json:"counts"`
	Done   int                `json:"done"`
	Total  int                `json:"total"`
}

type boardGoal struct {
	Work   core.Work `json:"work"`
	Rollup rollup    `json:"rollup"`
}

// boardView is every goal with its rollup and every open standalone work item.
type boardView struct {
	Goals      []boardGoal `json:"goals"`
	Standalone []core.Work `json:"standalone"`
}

// goalView is one goal with its children, rollup and events.
type goalView struct {
	Goal   core.Work    `json:"goal"`
	Tasks  []core.Work  `json:"tasks"`
	Rollup rollup       `json:"rollup"`
	Events []core.Event `json:"events"`
}

// actionResult is a CLI run: its exit code and combined output.
type actionResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
}

type errorBody struct {
	Error string `json:"error"`
}

type wsBoard struct {
	Type string    `json:"type"`
	Data boardView `json:"data"`
}

type wsEvent struct {
	Type string     `json:"type"`
	Data core.Event `json:"data"`
}

type wsAction struct {
	Type string `json:"type"`
	actionResult
}

type wsError struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// Start binds the server to 127.0.0.1:port, serves HTTP and broadcasts
// every event added to the ledger, by any process, to WebSocket clients.
// It returns the bound address and a channel that receives the error that
// stopped serving.
func (s *Server) Start(port int) (string, <-chan error, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", nil, err
	}
	last, err := s.ledger.LastEventID()
	if err != nil {
		ln.Close()
		return "", nil, err
	}
	failed := make(chan error, 2)
	go func() { failed <- http.Serve(ln, s.routes()) }()
	go func() { failed <- s.broadcastEvents(last) }()
	return ln.Addr().String(), failed, nil
}

// routes maps every endpoint to its handler.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/board", s.handleBoard)
	mux.HandleFunc("/api/goals", s.handleBoard)
	mux.HandleFunc("/api/goal/", s.handleGoal)
	mux.HandleFunc("/api/work", s.handleWork)
	mux.HandleFunc("/api/work/", s.handleWorkItem)
	mux.HandleFunc("/api/action", s.handleAction)
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/", handleNotFound)
	return mux
}

// eventPoll is how often the ledger is read for new events. Events are
// written by other wd processes, which sqlite cannot notify across.
const eventPoll = 250 * time.Millisecond

// broadcastEvents sends every ledger event with an id above last to all
// WebSocket clients, in id order, until reading the ledger fails.
func (s *Server) broadcastEvents(last int) error {
	tick := time.NewTicker(eventPoll)
	defer tick.Stop()
	sent := last
	for {
		<-tick.C
		events, err := s.ledger.EventsAfter(sent)
		if err != nil {
			return err
		}
		for _, e := range events {
			if err := s.hub.broadcast(wsEvent{Type: "event", Data: e}); err != nil {
				return err
			}
			sent = e.ID
		}
	}
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

// allow answers 405 and returns false unless r uses method.
func allow(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

// handleBoard serves the board view.
func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet) {
		return
	}
	b, err := s.board()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleGoal serves one goal with its children, rollup and events.
func (s *Server) handleGoal(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/goal/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing goal id")
		return
	}
	goal, ok := s.work(w, id)
	if !ok {
		return
	}
	if !core.IsGoal(goal.Kind) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s is not a goal", id))
		return
	}
	children, err := s.ledger.Tasks(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.ledger.Events(id, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, goalView{Goal: goal, Tasks: children, Rollup: goalRollup(children), Events: events})
}

// handleWork lists all work items.
func (s *Server) handleWork(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet) {
		return
	}
	items, err := s.ledger.List(ledger.ListFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleWorkItem serves /api/work/<id> and /api/work/<id>/events.
func (s *Server) handleWorkItem(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodGet) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/work/"), "/")
	id := parts[0]
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing work id")
		return
	}
	events := len(parts) == 2 && parts[1] == "events"
	if len(parts) > 1 && !events {
		handleNotFound(w, r)
		return
	}
	item, ok := s.work(w, id)
	if !ok {
		return
	}
	if !events {
		writeJSON(w, http.StatusOK, item)
		return
	}
	list, err := s.ledger.Events(id, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// work returns the work item id, or answers 404 for an unknown id and 500
// for a ledger failure and returns false.
func (s *Server) work(w http.ResponseWriter, id string) (core.Work, bool) {
	known, err := s.ledger.Has(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return core.Work{}, false
	}
	if !known {
		writeError(w, http.StatusNotFound, "no work "+id)
		return core.Work{}, false
	}
	item, err := s.ledger.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return core.Work{}, false
	}
	return item, true
}

// handleAction runs a wd CLI action and returns the result.
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	if !allow(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Argv []string `json:"argv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	res, err := s.runCLI(body.Argv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleWS upgrades to a WebSocket, sends the board, then streams events
// and answers action messages.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	key, err := wsKey(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "connection cannot be upgraded")
		return
	}
	c, err := wsHandshake(hj, key)
	if err != nil {
		// The connection is hijacked and closed: no response can be written.
		return
	}
	err = s.hub.join(c, func() error {
		b, err := s.board()
		if err != nil {
			return fmt.Errorf("board: %w", err)
		}
		return c.writeJSON(wsBoard{Type: "board", Data: b})
	})
	if err != nil {
		c.close(err)
		return
	}
	defer s.hub.remove(c)
	c.readLoop(func(payload []byte) error { return s.answerWS(c, payload) })
}

// answerWS answers one client message: an action runs the CLI; anything
// else gets an error reply.
func (s *Server) answerWS(c *wsConn, payload []byte) error {
	var msg struct {
		Type string   `json:"type"`
		Argv []string `json:"argv"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return c.writeJSON(wsError{Type: "error", Error: "invalid message: " + err.Error()})
	}
	if msg.Type != "action" {
		return c.writeJSON(wsError{Type: "error", Error: fmt.Sprintf("unknown message type %q", msg.Type)})
	}
	res, err := s.runCLI(msg.Argv)
	if err != nil {
		return c.writeJSON(wsError{Type: "error", Error: err.Error()})
	}
	return c.writeJSON(wsAction{Type: "action", actionResult: res})
}

// board returns every goal with its rollup and every open standalone work item.
func (s *Server) board() (boardView, error) {
	items, err := s.ledger.List(ledger.ListFilter{})
	if err != nil {
		return boardView{}, err
	}
	b := boardView{Goals: []boardGoal{}, Standalone: []core.Work{}}
	for _, w := range items {
		if core.IsGoal(w.Kind) {
			children, err := s.ledger.Tasks(w.ID)
			if err != nil {
				return boardView{}, err
			}
			b.Goals = append(b.Goals, boardGoal{Work: w, Rollup: goalRollup(children)})
		} else if w.Parent == nil && w.State != core.StateDone && w.State != core.StateDropped {
			b.Standalone = append(b.Standalone, w)
		}
	}
	return b, nil
}

// goalRollup counts a goal's children per state.
func goalRollup(children []core.Work) rollup {
	counts := map[core.State]int{}
	for _, c := range children {
		counts[c.State]++
	}
	return rollup{Counts: counts, Done: counts[core.StateDone], Total: len(children)}
}

// runCLI runs the wd CLI with argv. A non-zero exit is a result; failing to
// run the CLI at all is an error.
func (s *Server) runCLI(argv []string) (actionResult, error) {
	cmd := exec.Command(s.cliPath, argv...)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return actionResult{}, fmt.Errorf("run %s: %w", s.cliPath, err)
	}
	return actionResult{Code: cmd.ProcessState.ExitCode(), Stdout: strings.TrimSpace(string(out))}, nil
}

// writeJSON writes v as a JSON response, or a 500 when v cannot be encoded.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := jsonMarshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write means the client is gone; there is no one left to tell.
	w.Write(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// jsonMarshal encodes v without HTML escaping.
func jsonMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
