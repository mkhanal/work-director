package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wd/internal/core"
	"wd/internal/ledger"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	l, err := ledger.New(dir + "/ledger.db")
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return New(l, "")
}

func TestServe(t *testing.T) {
	t.Run("The Server Binds To Loopback", serverBindsToLoopback)
	t.Run("JSON Endpoints Serve The Board", jsonEndpointsServeTheBoard)
	t.Run("Work Items Serve Over HTTP", workItemsServeOverHTTP)
	t.Run("Actions Dispatch To The CLI", actionsDispatchToTheCLI)
	t.Run("WebSocket Serves Live Events", webSocketServesLiveEvents)
	t.Run("Empty Collections Serialize As Empty Arrays", emptyCollectionsSerializeAsEmptyArrays)
	t.Run("Errors Return JSON", errorsReturnJSON)
}

func serverBindsToLoopback(t *testing.T) {
	s := newTestServer(t)
	addr, _, err := s.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("addr = %s, want 127.0.0.1:port", addr)
	}
}

func jsonEndpointsServeTheBoard(t *testing.T) {
	s := newTestServer(t)
	epic, err := s.ledger.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkEpic})
	if err != nil {
		t.Fatalf("add epic: %v", err)
	}
	_, err = s.ledger.Add("p", "Task", ledger.AddOptions{Parent: &epic.ID})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	_, err = s.ledger.Add("p", "Standalone", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add standalone: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(s.handleBoard))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var board struct {
		Goals      []map[string]any `json:"goals"`
		Standalone []core.Work      `json:"standalone"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&board); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(board.Goals) != 1 {
		t.Fatalf("goals = %d, want 1", len(board.Goals))
	}
	if len(board.Standalone) != 1 {
		t.Fatalf("standalone = %d, want 1", len(board.Standalone))
	}
}

func workItemsServeOverHTTP(t *testing.T) {
	s := newTestServer(t)
	w, err := s.ledger.Add("p", "Work", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(s.handleWork))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var items []core.Work
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].ID != w.ID {
		t.Fatalf("items = %v, want [%s]", items, w.ID)
	}
}

func actionsDispatchToTheCLI(t *testing.T) {
	s := newTestServer(t)
	s.cliPath = "/bin/echo"
	srv := httptest.NewServer(http.HandlerFunc(s.handleAction))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"argv":["hello"]}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Code   int    `json:"code"`
		Stdout string `json:"stdout"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Code != 0 {
		t.Fatalf("code = %d, want 0", result.Code)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Fatalf("stdout = %q, want to contain 'hello'", result.Stdout)
	}
}

func webSocketServesLiveEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	l, err := ledger.New(path)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	w, err := l.Add("p", "Work", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := l.AddEvent(w.ID, core.EventNote, "before serve"); err != nil {
		t.Fatalf("event before serve: %v", err)
	}
	addr, _, err := New(l, "/bin/echo").Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	client := dialWS(t, addr)

	var board struct {
		Type string `json:"type"`
	}
	readWSJSON(t, client, &board)
	if board.Type != "board" {
		t.Fatalf("first message type = %q, want board", board.Type)
	}

	other, err := ledger.New(path)
	if err != nil {
		t.Fatalf("second ledger handle: %v", err)
	}
	t.Cleanup(func() { other.Close() })
	if err := other.AddEvent(w.ID, core.EventNote, "from another wd process"); err != nil {
		t.Fatalf("event from other process: %v", err)
	}

	var msg struct {
		Type string     `json:"type"`
		Data core.Event `json:"data"`
	}
	readWSJSON(t, client, &msg)
	if msg.Type != "event" {
		t.Fatalf("message type = %q, want event", msg.Type)
	}
	if msg.Data.Work != w.ID || msg.Data.Body != "from another wd process" {
		t.Fatalf("event = %+v, want the event added by the other process", msg.Data)
	}

	if err := client.writeText(`{"type":"action","argv":["hello"]}`); err != nil {
		t.Fatalf("send action: %v", err)
	}
	var action struct {
		Type   string `json:"type"`
		Code   int    `json:"code"`
		Stdout string `json:"stdout"`
	}
	readWSJSON(t, client, &action)
	if action.Type != "action" || action.Code != 0 || action.Stdout != "hello" {
		t.Fatalf("action reply = %+v, want the CLI's output", action)
	}
}

// dialWS opens a WebSocket client connection to the server at addr.
func dialWS(t *testing.T, addr string) *wsConn {
	t.Helper()
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { nc.Close() })
	if err := nc.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	fmt.Fprintf(nc, "GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", addr)
	// Byte by byte: a buffered reader would swallow the start of the first frame.
	var head []byte
	for !bytes.HasSuffix(head, []byte("\r\n\r\n")) {
		var b [1]byte
		if _, err := io.ReadFull(nc, b[:]); err != nil {
			t.Fatalf("handshake: %v (read %q)", err, head)
		}
		head = append(head, b[0])
	}
	if !bytes.HasPrefix(head, []byte("HTTP/1.1 101 ")) {
		t.Fatalf("handshake response = %q, want 101", head)
	}
	return &wsConn{conn: nc}
}

// readWSJSON reads one text frame from c and decodes it into v.
func readWSJSON(t *testing.T, c *wsConn, v any) {
	t.Helper()
	frame, err := c.readFrame()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if err := json.Unmarshal(frame.payload, v); err != nil {
		t.Fatalf("decode %q: %v", frame.payload, err)
	}
}

func emptyCollectionsSerializeAsEmptyArrays(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(http.HandlerFunc(s.handleWork))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var items []core.Work
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if items == nil {
		t.Fatal("items = nil, want []")
	}
	if len(items) != 0 {
		t.Fatalf("items = %d, want 0", len(items))
	}
}

func errorsReturnJSON(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(http.HandlerFunc(s.handleGoal))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == "" {
		t.Fatal("error = empty, want message")
	}
}
