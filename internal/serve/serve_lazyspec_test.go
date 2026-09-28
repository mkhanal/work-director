package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	return New(l, dir, "")
}

func TestServerBindsToLoopback(t *testing.T) {
	s := newTestServer(t)
	addr, err := s.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("addr = %s, want 127.0.0.1:port", addr)
	}
}

func TestJSONEndpointsServeTheBoard(t *testing.T) {
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

func TestWorkItemsServeOverHTTP(t *testing.T) {
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

func TestActionsDispatchToTheCLI(t *testing.T) {
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

func TestEmptyCollectionsSerializeAsEmptyArrays(t *testing.T) {
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

func TestErrorsReturnJSON(t *testing.T) {
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
