package serve

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"wd/internal/core"
	"wd/internal/ledger"
)

// DefaultPort is the port serve binds to when --port is not given.
const DefaultPort = 8787

// Server is the loopback HTTP and WebSocket adapter over the core and
// ledger. It serves the board, work items, events and actions, and
// broadcasts live events to WebSocket clients.
type Server struct {
	addr   string
	ledger *ledger.Ledger
	root   string
	cliPath string
	hub    *wsHub
	mu     sync.Mutex
}

// New creates a serve server over the given ledger. root is the repo root
// (where taste/cards lives); cliPath is the path to the wd CLI binary.
func New(l *ledger.Ledger, root, cliPath string) *Server {
	return &Server{
		ledger:  l,
		root:    root,
		cliPath: cliPath,
		hub:     newWSHub(),
	}
}

// Start binds the server to 127.0.0.1:port and serves until the process
// exits. It returns the bound address.
func (s *Server) Start(port int) (string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	s.addr = addr
	mux := http.NewServeMux()
	mux.HandleFunc("/api/board", s.handleBoard)
	mux.HandleFunc("/api/goals", s.handleGoals)
	mux.HandleFunc("/api/goal/", s.handleGoal)
	mux.HandleFunc("/api/work", s.handleWork)
	mux.HandleFunc("/api/work/", s.handleWorkItem)
	mux.HandleFunc("/api/action", s.handleAction)
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/", s.handleRoot)
	go http.Serve(ln, mux)
	return addr, nil
}

// handleRoot serves the root path and 404s everything else.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.NotFound(w, r)
}

// handleBoard serves the board view: every epic with its rollup and
// every open standalone work item.
func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeJSON(w, s.board())
}

// handleGoals serves the same data as the board.
func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeJSON(w, s.board())
}

// handleGoal serves one epic with its children, rollup and events.
func (s *Server) handleGoal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/goal/")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing goal id")
		return
	}
	epic, err := s.ledger.Get(id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !core.IsEpic(epic.Kind) {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("%s is not an epic", id))
		return
	}
	children, err := s.ledger.Tasks(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.ledger.Events(id, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, map[string]any{
		"epic":    epic,
		"tasks":   children,
		"rollup":  goalRollup(children),
		"events":  events,
	})
}

// handleWork lists all work items.
func (s *Server) handleWork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items, err := s.ledger.List(ledger.ListFilter{})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, items)
}

// handleWorkItem serves one work item and its events.
func (s *Server) handleWorkItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/work/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing work id")
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		events, err := s.ledger.Events(id, nil)
		if err != nil {
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.writeJSON(w, events)
		return
	}
	item, err := s.ledger.Get(id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeJSON(w, item)
}

// handleAction runs a wd CLI action and returns the result.
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Argv []string `json:"argv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	code, stdout := s.runCLI(body.Argv)
	s.writeJSON(w, map[string]any{"code": code, "stdout": stdout})
}

// handleWS upgrades the connection to a WebSocket and serves live events.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := wsUpgrade(w, r)
	if err != nil {
		return
	}
	s.hub.add(c)
	defer func() {
		s.hub.remove(c)
		c.close()
	}()
	// Send the current board state on connect.
	c.writeJSON(map[string]any{"type": "board", "data": s.board()})
	// Read loop: handle incoming action messages.
	c.readLoop(func(payload []byte) error {
		var msg struct {
			Type string   `json:"type"`
			Argv []string `json:"argv"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			return nil
		}
		if msg.Type == "action" {
			code, stdout := s.runCLI(msg.Argv)
			c.writeJSON(map[string]any{"type": "action", "code": code, "stdout": stdout})
		}
		return nil
	})
}

// board returns the board view: every epic with its rollup and every
// open standalone work item.
func (s *Server) board() map[string]any {
	items, err := s.ledger.List(ledger.ListFilter{})
	if err != nil {
		return map[string]any{"goals": []any{}, "standalone": []any{}}
	}
	var goals []map[string]any
	var standalone []core.Work
	for _, w := range items {
		if core.IsEpic(w.Kind) {
			children, err := s.ledger.Tasks(w.ID)
			if err != nil {
				continue
			}
			goals = append(goals, map[string]any{
				"work":   w,
				"rollup": goalRollup(children),
			})
		} else if w.Parent == nil && w.State != core.StateDone && w.State != core.StateDropped {
			standalone = append(standalone, w)
		}
	}
	return map[string]any{"goals": goals, "standalone": standalone}
}

// goalRollup rolls up a goal's children into a goal-level status: counts
// per state + done fraction.
func goalRollup(children []core.Work) map[string]any {
	counts := map[string]int{}
	for _, c := range children {
		counts[string(c.State)]++
	}
	done := counts[string(core.StateDone)]
	return map[string]any{
		"counts": counts,
		"done":   done,
		"total":  len(children),
	}
}

// runCLI runs the wd CLI with the given arguments and returns the exit
// code and combined output.
func (s *Server) runCLI(argv []string) (int, string) {
	cli := s.cliPath
	if cli == "" {
		cli = os.Args[0]
	}
	cmd := exec.Command(cli, argv...)
	cmd.Dir = s.root
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return code, strings.TrimSpace(string(out))
}

// writeJSON writes a JSON response with the given status code.
func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// jsonMarshal is a helper to marshal JSON with HTML escaping disabled.
func jsonMarshal(v any) ([]byte, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buf.String(), "\n")), nil
}

// findCLIPath locates the wd CLI binary: the executable's directory, then
// the working directory.
func findCLIPath() string {
	exe, err := os.Executable()
	if err == nil {
		return filepath.Dir(exe)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// logf logs a message to stderr.
func logf(format string, args ...any) {
	log.Printf(format, args...)
}
