package serve

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
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
	t.Run("A Goal Serves What The Loop Decided And Where It Reached", aGoalServesWhatTheLoopDecidedAndWhereItReached)
	t.Run("A Goal's Delivery Is Read In The Goal's Own Directory", aGoalsDeliveryIsReadInTheGoalsOwnDirectory)
	t.Run("The Board Places Every Item In One Band", theBoardPlacesEveryItemInOneBand)
	t.Run("Archived Work Is Not On The Board", archivedWorkIsNotOnTheBoard)
	t.Run("A Client Can Hold The Server On Its Standard Streams", aClientCanHoldTheServerOnItsStandardStreams)
	t.Run("Work Items Serve Over HTTP", workItemsServeOverHTTP)
	t.Run("Actions Dispatch To The CLI", actionsDispatchToTheCLI)
	t.Run("WebSocket Serves Live Events", webSocketServesLiveEvents)
	t.Run("A Client That Stops Reading Never Delays The Others", aClientThatStopsReadingNeverDelaysTheOthers)
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
	goal, err := s.ledger.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkEpic})
	if err != nil {
		t.Fatalf("add goal: %v", err)
	}
	task, err := s.ledger.Add("p", "Task", ledger.AddOptions{Parent: &goal.ID})
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
		Standalone []struct {
			Work core.Work `json:"work"`
		} `json:"standalone"`
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

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var goals struct {
		Goals []map[string]any `json:"goals"`
	}
	getJSON(t, api.URL+"/api/goals", http.StatusOK, &goals)
	if len(goals.Goals) != 1 {
		t.Fatalf("/api/goals goals = %d, want 1", len(goals.Goals))
	}
	var one struct {
		Goal  core.Work   `json:"goal"`
		Tasks []core.Work `json:"tasks"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &one)
	if one.Goal.ID != goal.ID || len(one.Tasks) != 1 || one.Tasks[0].ID != task.ID {
		t.Fatalf("goal = %+v, want goal %s with task %s", one, goal.ID, task.ID)
	}
	var missing map[string]string
	getJSON(t, api.URL+"/api/goal/"+task.ID, http.StatusNotFound, &missing)
	getJSON(t, api.URL+"/api/goal/nope", http.StatusNotFound, &missing)
}

// Under autonomy the one thing a person opens a goal for is what the loop
// decided in their place and where the work reached, so both come with the goal
// itself — and both come out of the events already on the response, because a
// client that rendered the decisions and the event log from two reads of the
// ledger would be free to disagree with itself.
func aGoalServesWhatTheLoopDecidedAndWhereItReached(t *testing.T) {
	s := newTestServer(t)
	kind := core.GoalBuild
	goal, err := s.ledger.Add("p", "Autonomous factory", ledger.AddOptions{Kind: core.WorkEpic, GoalType: &kind})
	if err != nil {
		t.Fatalf("add goal: %v", err)
	}
	task, err := s.ledger.Add("p", "Drive the goal loop", ledger.AddOptions{Parent: &goal.ID})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	answered, err := s.ledger.Decide(task.ID, "the loop decided",
		core.Decision{Question: "which port?", Answer: "8080", Source: "judge"}, nil)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if _, err := s.ledger.AddEvent(task.ID, core.EventPr, "commit https://github.test/o/r/commit/5a5f3bb"); err != nil {
		t.Fatalf("pr: %v", err)
	}
	if _, err := s.ledger.AddEvent(goal.ID, core.EventPr, "https://github.test/o/r/pull/7"); err != nil {
		t.Fatalf("legacy pr: %v", err)
	}

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var one struct {
		Goal   core.Work `json:"goal"`
		Claims []struct {
			Event  core.Event `json:"event"`
			Work   *core.Work `json:"work"`
			Stands bool       `json:"stands"`
		} `json:"claims"`
		Landings []struct {
			Work *core.Work       `json:"work"`
			Kind core.LandingKind `json:"kind"`
			URL  string           `json:"url"`
		} `json:"landings"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &one)

	if one.Goal.GoalType == nil || *one.Goal.GoalType != core.GoalBuild {
		t.Fatalf("goal type = %v, want build: a goal whose kind is not on it reads as untyped", one.Goal.GoalType)
	}
	if len(one.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(one.Claims))
	}
	// Which work a claim is about travels with it, so a reader sees what it was
	// about and not only which row it sits on.
	if one.Claims[0].Event.ID != answered.ID || one.Claims[0].Work == nil || one.Claims[0].Work.ID != task.ID {
		t.Errorf("claim = %+v, want event %d resolved to task %s", one.Claims[0], answered.ID, task.ID)
	}
	if !one.Claims[0].Stands {
		t.Errorf("claim stands = false, want true: nothing reversed it")
	}
	if len(one.Landings) != 2 {
		t.Fatalf("landings = %d, want both", len(one.Landings))
	}
	// The kind is what separates a change that went into the product from one
	// still waiting on a merge — the difference between "reviewed" and "landed".
	if one.Landings[0].Kind != core.LandingCommit || one.Landings[0].URL != "https://github.test/o/r/commit/5a5f3bb" {
		t.Errorf("first landing = %+v, want the commit and its link", one.Landings[0])
	}
	if one.Landings[0].Work == nil || one.Landings[0].Work.ID != task.ID {
		t.Errorf("first landing work = %+v, want task %s", one.Landings[0].Work, task.ID)
	}
	// A landing filed before kinds were recorded is a real link of unknown kind,
	// and the link is still the fact.
	if one.Landings[1].Kind != "" || one.Landings[1].URL != "https://github.test/o/r/pull/7" {
		t.Errorf("legacy landing = %+v, want the link kept and the kind unknown", one.Landings[1])
	}

	// A goal with nothing decided and nowhere landed sends two empty arrays, not
	// two nulls: a client should never have to write the null check the review
	// spec already told it not to.
	empty, err := s.ledger.Add("p", "Nothing yet", ledger.AddOptions{Kind: core.WorkEpic})
	if err != nil {
		t.Fatalf("add empty goal: %v", err)
	}
	var none struct {
		Claims   []any `json:"claims"`
		Landings []any `json:"landings"`
	}
	getJSON(t, api.URL+"/api/goal/"+empty.ID, http.StatusOK, &none)
	if none.Claims == nil || len(none.Claims) != 0 || none.Landings == nil || len(none.Landings) != 0 {
		t.Errorf("empty goal served %+v, want empty arrays", none)
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

	if _, err := s.ledger.AddEvent(w.ID, core.EventNote, "noted"); err != nil {
		t.Fatalf("add event: %v", err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var item core.Work
	getJSON(t, api.URL+"/api/work/"+w.ID, http.StatusOK, &item)
	if item.ID != w.ID {
		t.Fatalf("item = %s, want %s", item.ID, w.ID)
	}
	var events []core.Event
	getJSON(t, api.URL+"/api/work/"+w.ID+"/events", http.StatusOK, &events)
	if len(events) == 0 || events[len(events)-1].Body != "noted" {
		t.Fatalf("events = %+v, want them to end with the note", events)
	}
	for path, want := range map[string]string{
		"/api/work/nope":             "no work nope",
		"/api/work/nope/events":      "no work nope",
		"/api/work/" + w.ID + "/foo": "not found",
	} {
		var body map[string]string
		getJSON(t, api.URL+path, http.StatusNotFound, &body)
		if body["error"] != want {
			t.Fatalf("%s error = %q, want %q", path, body["error"], want)
		}
	}
}

// getJSON GETs url, requires status and a JSON content type, and decodes the body into v.
func getJSON(t *testing.T, url string, status int, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	requireJSON(t, resp, status, v)
}

// requireJSON requires resp to carry status and a JSON body, and decodes it into v.
func requireJSON(t *testing.T, resp *http.Response, status int, v any) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", resp.Request.URL, err)
	}
	if resp.StatusCode != status {
		t.Fatalf("%s %s status = %d, want %d (body %q)", resp.Request.Method, resp.Request.URL, resp.StatusCode, status, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s content type = %q, want application/json (body %q)", resp.Request.URL, ct, body)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode %q: %v", body, err)
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
		Stderr string `json:"stderr"`
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

	s.cliPath = "/bin/sh"
	resp, err = http.Post(srv.URL, "application/json", strings.NewReader(`{"argv":["-c","echo out; echo progress >&2; exit 3"]}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	requireJSON(t, resp, http.StatusOK, &result)
	if result.Code != 3 || result.Stdout != "out" || result.Stderr != "progress" {
		t.Fatalf("result = %+v, want code 3 with stdout and stderr kept apart", result)
	}

	s.cliPath = filepath.Join(t.TempDir(), "missing-wd")
	resp, err = http.Post(srv.URL, "application/json", strings.NewReader(`{"argv":["status"]}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var failed map[string]string
	requireJSON(t, resp, http.StatusInternalServerError, &failed)
	if !strings.Contains(failed["error"], "missing-wd") {
		t.Fatalf("error = %q, want the CLI start failure", failed["error"])
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
	if _, err := l.AddEvent(w.ID, core.EventNote, "before serve"); err != nil {
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
	if _, err := other.AddEvent(w.ID, core.EventNote, "from another wd process"); err != nil {
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

	if err := client.writeFrame(opText, []byte(`{"type":"action","argv":["hello"]}`)); err != nil {
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

	var reply struct {
		Type  string `json:"type"`
		Error string `json:"error"`
	}
	for _, bad := range []string{`{"type":`, `{"type":"dance"}`} {
		if err := client.writeFrame(opText, []byte(bad)); err != nil {
			t.Fatalf("send %s: %v", bad, err)
		}
		reply.Type, reply.Error = "", ""
		readWSJSON(t, client, &reply)
		if reply.Type != "error" || reply.Error == "" {
			t.Fatalf("reply to %s = %+v, want an error", bad, reply)
		}
	}

	resp, err := http.Get("http://" + addr + "/ws")
	if err != nil {
		t.Fatalf("plain get /ws: %v", err)
	}
	defer resp.Body.Close()
	var refused map[string]string
	requireJSON(t, resp, http.StatusBadRequest, &refused)
	if refused["error"] == "" {
		t.Fatal("plain GET /ws error = empty, want why the upgrade was refused")
	}
}

func aClientThatStopsReadingNeverDelaysTheOthers(t *testing.T) {
	l, err := ledger.New(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	w, err := l.Add("p", "Work", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	srv := New(l, "/bin/echo")
	addr, _, err := srv.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Never read: once its socket buffers fill, every write to it blocks.
	dialWS(t, addr)
	healthy := dialWS(t, addr)
	var board struct {
		Type string `json:"type"`
	}
	readWSJSON(t, healthy, &board)

	// Far more than a loopback socket buffers, so writes to the stalled
	// client block long before the last event is sent.
	const events = 128
	body := strings.Repeat("x", 256<<10)
	for i := 0; i < events; i++ {
		if _, err := l.AddEvent(w.ID, core.EventNote, fmt.Sprintf("%d %s", i, body)); err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
	}
	// Well inside the write timeout a blocked broadcast would wait out.
	if err := healthy.conn.SetReadDeadline(time.Now().Add(wsWriteTimeout / 2)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	for i := 0; i < events; i++ {
		var msg struct {
			Data core.Event `json:"data"`
		}
		readWSJSON(t, healthy, &msg)
		if want := fmt.Sprintf("%d ", i); !strings.HasPrefix(msg.Data.Body, want) {
			t.Fatalf("event %d body starts %q, want %q", i, msg.Data.Body[:8], want)
		}
	}

	// A client joining while the stalled one is still connected gets its board
	// as promptly.
	srv.hub.mu.Lock()
	n := len(srv.hub.conns)
	srv.hub.mu.Unlock()
	if n != 2 {
		t.Fatalf("%d clients connected, want the stalled one still there", n)
	}
	joined := dialWS(t, addr)
	if err := joined.conn.SetReadDeadline(time.Now().Add(wsWriteTimeout / 2)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	board.Type = ""
	readWSJSON(t, joined, &board)
	if board.Type != "board" {
		t.Fatalf("joining client's first message type = %q, want board", board.Type)
	}

	// Reading from the stalled client would unstall it, so watch the hub.
	for deadline := time.Now().Add(3 * wsWriteTimeout); ; time.Sleep(20 * time.Millisecond) {
		srv.hub.mu.Lock()
		n := len(srv.hub.conns)
		srv.hub.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d clients still connected, want the stalled one dropped", n)
		}
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
	return newWSConn(nc, wsWriteTimeout)
}

// readWSJSON reads one text frame from c and decodes it into v.
func readWSJSON(t *testing.T, c *wsConn, v any) {
	t.Helper()
	frame, err := readFrame(c.conn)
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

	api := httptest.NewServer(s.routes())
	defer api.Close()
	for _, path := range []string{"/api/board", "/api/goals"} {
		var board map[string]json.RawMessage
		getJSON(t, api.URL+path, http.StatusOK, &board)
		for _, key := range []string{"goals", "standalone"} {
			if string(board[key]) != "[]" {
				t.Fatalf("%s %s = %s, want []", path, key, board[key])
			}
		}
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

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var unknown map[string]string
	getJSON(t, api.URL+"/nope", http.StatusNotFound, &unknown)
	if unknown["error"] != "not found" {
		t.Fatalf("unknown path error = %q, want not found", unknown["error"])
	}
	var noWork map[string]string
	getJSON(t, api.URL+"/api/work/w-404", http.StatusNotFound, &noWork)
	if noWork["error"] != "no work w-404" {
		t.Fatalf("unknown work error = %q, want no work w-404", noWork["error"])
	}

	resp, err = http.Post(api.URL+"/api/action", "application/json", strings.NewReader(`{"argv":`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var bad map[string]string
	requireJSON(t, resp, http.StatusBadRequest, &bad)
	if bad["error"] == "" {
		t.Fatal("bad request error = empty, want message")
	}

	resp, err = http.Post(api.URL+"/api/board", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var method map[string]string
	requireJSON(t, resp, http.StatusMethodNotAllowed, &method)
	if method["error"] == "" {
		t.Fatal("method not allowed error = empty, want message")
	}
}

// walk moves work through each state in turn, failing the test on the first
// transition the ledger refuses.
func walk(t *testing.T, l *ledger.Ledger, id string, states ...core.State) {
	t.Helper()
	for _, st := range states {
		if _, err := l.Transition(id, st); err != nil {
			t.Fatalf("%s -> %s: %v", id, st, err)
		}
	}
}

func event(t *testing.T, l *ledger.Ledger, id string, kind core.EventKind, body string) {
	t.Helper()
	if _, err := l.AddEvent(id, kind, body); err != nil {
		t.Fatalf("event %s on %s: %v", kind, id, err)
	}
}

func theBoardPlacesEveryItemInOneBand(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	add := func(title string, o ledger.AddOptions) core.Work {
		t.Helper()
		w, err := l.Add("p", title, o)
		if err != nil {
			t.Fatalf("add %s: %v", title, err)
		}
		return w
	}
	queued := add("queued", ledger.AddOptions{})
	running := add("running", ledger.AddOptions{})
	walk(t, l, running.ID, core.StateBriefed, core.StateRunning)
	asking := add("asking", ledger.AddOptions{})
	walk(t, l, asking.ID, core.StateRunning, core.StateNeedsInput)
	blocked := add("blocked", ledger.AddOptions{})
	walk(t, l, blocked.ID, core.StateRunning, core.StateBlocked)
	unpushed := add("unpushed", ledger.AddOptions{})
	walk(t, l, unpushed.ID, core.StateRunning, core.StateReview)
	pushed := add("pushed", ledger.AddOptions{})
	walk(t, l, pushed.ID, core.StateRunning, core.StateReview)
	event(t, l, pushed.ID, core.EventPr, "pull-request https://github.com/o/r/pull/1")
	held := add("held", ledger.AddOptions{})
	walk(t, l, held.ID, core.StateRunning, core.StatePaused)
	closing := add("closing", ledger.AddOptions{})
	walk(t, l, closing.ID, core.StateRunning, core.StateReview)
	event(t, l, closing.ID, core.EventReport, "DONE")
	event(t, l, closing.ID, core.EventVerify, "pass")
	event(t, l, closing.ID, core.EventPr, "pull-request https://github.com/o/r/pull/2")
	if _, err := l.SoftDone(closing.ID, true); err != nil {
		t.Fatalf("soft-done: %v", err)
	}
	// Landed once, closed, reopened and back in review with nothing new pushed:
	// the old landing belongs to the run before.
	again := add("again", ledger.AddOptions{})
	walk(t, l, again.ID, core.StateRunning, core.StateReview)
	event(t, l, again.ID, core.EventReport, "DONE")
	event(t, l, again.ID, core.EventVerify, "pass")
	event(t, l, again.ID, core.EventPr, "pull-request https://github.com/o/r/pull/3")
	if _, err := l.SoftDone(again.ID, true); err != nil {
		t.Fatalf("soft-done: %v", err)
	}
	walk(t, l, again.ID, core.StateDone)
	if _, err := l.Reopen(again.ID, "a second half"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	walk(t, l, again.ID, core.StateReview)

	quiet := add("quiet goal", ledger.AddOptions{Kind: core.WorkGoal})
	walk(t, l, quiet.ID, core.StateRunning)
	add("calm task", ledger.AddOptions{Parent: &quiet.ID})
	stuck := add("stuck goal", ledger.AddOptions{Kind: core.WorkGoal})
	walk(t, l, stuck.ID, core.StateRunning)
	task := add("stuck task", ledger.AddOptions{Parent: &stuck.ID})
	walk(t, l, task.ID, core.StateRunning, core.StateBlocked)
	finished := add("finished goal", ledger.AddOptions{Kind: core.WorkGoal})
	walk(t, l, finished.ID, core.StateDone)

	b, err := s.board()
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	got := map[string]Band{}
	for _, g := range b.Goals {
		got[g.Work.ID] = g.Band
	}
	for _, w := range b.Standalone {
		got[w.Work.ID] = w.Band
	}
	want := map[string]Band{
		queued.ID: BandInFlight, running.ID: BandInFlight, held.ID: BandPaused,
		asking.ID: BandNeedsYou, blocked.ID: BandNeedsYou,
		unpushed.ID: BandReadyToPush, pushed.ID: BandInReview, again.ID: BandReadyToPush,
		closing.ID: BandReadyToClose,
		quiet.ID:   BandInFlight, stuck.ID: BandNeedsYou, finished.ID: BandAtRest,
	}
	for id, band := range want {
		if got[id] != band {
			t.Errorf("%s band = %q, want %q", id, got[id], band)
		}
	}

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var wire struct {
		Goals      []map[string]any `json:"goals"`
		Standalone []map[string]any `json:"standalone"`
	}
	getJSON(t, api.URL+"/api/board", http.StatusOK, &wire)
	for _, g := range wire.Goals {
		for _, k := range []string{"work", "rollup", "band"} {
			if _, ok := g[k]; !ok {
				t.Errorf("goal on the wire lacks %q: %v", k, g)
			}
		}
	}
	for _, w := range wire.Standalone {
		for _, k := range []string{"work", "band"} {
			if _, ok := w[k]; !ok {
				t.Errorf("standalone item on the wire lacks %q: %v", k, w)
			}
		}
	}
}

// stdioClient speaks the line protocol to a server reading in and writing out.
type stdioClient struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Scanner
	done chan error
}

func startStdio(t *testing.T, s *Server) *stdioClient {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := s.ServeStdio(inR, outW)
		outW.Close()
		done <- err
	}()
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	return &stdioClient{t: t, in: inW, out: sc, done: done}
}

func (c *stdioClient) send(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, line+"\n"); err != nil {
		c.t.Fatalf("write %s: %v", line, err)
	}
}

// next reads messages until keep accepts one, returning it.
func (c *stdioClient) next(keep func(map[string]json.RawMessage) bool) map[string]json.RawMessage {
	c.t.Helper()
	deadline := time.After(5 * time.Second)
	lines := make(chan map[string]json.RawMessage)
	go func() {
		for c.out.Scan() {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(c.out.Bytes(), &m); err != nil {
				c.t.Errorf("line %q is not JSON: %v", c.out.Text(), err)
				continue
			}
			if keep(m) {
				lines <- m
				return
			}
		}
		close(lines)
	}()
	select {
	case m, ok := <-lines:
		if !ok {
			c.t.Fatalf("stdout ended before the expected message")
		}
		return m
	case <-deadline:
		c.t.Fatalf("no expected message within 5s")
	}
	return nil
}

func (c *stdioClient) reply(id int) map[string]json.RawMessage {
	c.t.Helper()
	want := fmt.Sprint(id)
	return c.next(func(m map[string]json.RawMessage) bool { return string(m["id"]) == want })
}

func replyErrorKind(t *testing.T, m map[string]json.RawMessage) string {
	t.Helper()
	var e struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(m["error"], &e); err != nil || e.Message == "" {
		t.Fatalf("error = %s, want {kind, message}", m["error"])
	}
	return e.Kind
}

func aClientCanHoldTheServerOnItsStandardStreams(t *testing.T) {
	s := newTestServer(t)
	s.cliPath = "/bin/echo"
	goal, err := s.ledger.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatalf("add goal: %v", err)
	}
	task, err := s.ledger.Add("p", "Task", ledger.AddOptions{Parent: &goal.ID})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	c := startStdio(t, s)

	c.send(`{"id": 1, "method": "board"}`)
	var board boardView
	if err := json.Unmarshal(c.reply(1)["result"], &board); err != nil || len(board.Goals) != 1 || board.Goals[0].Band != BandInFlight {
		t.Fatalf("board = %+v (%v), want the one goal in flight", board, err)
	}
	c.send(`{"id": 2, "method": "goal", "params": {"id": "` + goal.ID + `"}}`)
	var g goalView
	if err := json.Unmarshal(c.reply(2)["result"], &g); err != nil || g.Goal.ID != goal.ID || len(g.Tasks) != 1 {
		t.Fatalf("goal = %+v (%v), want %s with its task", g, err, goal.ID)
	}
	c.send(`{"id": 3, "method": "work", "params": {"id": "` + task.ID + `"}}`)
	var w core.Work
	if err := json.Unmarshal(c.reply(3)["result"], &w); err != nil || w.ID != task.ID {
		t.Fatalf("work = %+v (%v), want %s", w, err, task.ID)
	}
	c.send(`{"id": 4, "method": "events", "params": {"id": "` + task.ID + `"}}`)
	var evs []core.Event
	if err := json.Unmarshal(c.reply(4)["result"], &evs); err != nil || len(evs) == 0 {
		t.Fatalf("events = %v (%v), want the task's state event", evs, err)
	}
	c.send(`{"id": 5, "method": "action", "params": {"argv": ["status", "--json"]}}`)
	var act actionResult
	if err := json.Unmarshal(c.reply(5)["result"], &act); err != nil || act.Code != 0 || act.Stdout != "status --json" {
		t.Fatalf("action = %+v (%v), want the CLI run with the argv", act, err)
	}

	c.send(`{"id": 6, "method": "goal", "params": {"id": "nope"}}`)
	if k := replyErrorKind(t, c.reply(6)); k != "not-found" {
		t.Fatalf("unknown goal kind = %q, want not-found", k)
	}
	c.send(`{"id": 7, "method": "goal", "params": {"id": "` + task.ID + `"}}`)
	if k := replyErrorKind(t, c.reply(7)); k != "not-found" {
		t.Fatalf("goal on a task kind = %q, want not-found", k)
	}
	c.send(`{"id": 8, "method": "teleport"}`)
	if k := replyErrorKind(t, c.reply(8)); k != "bad-request" {
		t.Fatalf("unknown method kind = %q, want bad-request", k)
	}
	c.send(`{"id": 9, "method": "work"}`)
	if k := replyErrorKind(t, c.reply(9)); k != "bad-request" {
		t.Fatalf("missing id kind = %q, want bad-request", k)
	}
	c.send(`not json`)
	m := c.next(func(m map[string]json.RawMessage) bool { _, isErr := m["error"]; return isErr })
	if _, hasID := m["id"]; hasID {
		t.Fatalf("reply to a line that is not a request = %v, want an error with no id", m)
	}
	if k := replyErrorKind(t, m); k != "bad-request" {
		t.Fatalf("malformed line kind = %q, want bad-request", k)
	}

	// Written by the ledger directly, as another wd process would.
	e, err := s.ledger.AddEvent(task.ID, core.EventNote, "from elsewhere")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	push := c.next(func(m map[string]json.RawMessage) bool { return string(m["push"]) == `"event"` })
	var pushed core.Event
	if err := json.Unmarshal(push["data"], &pushed); err != nil || pushed.ID != e.ID {
		t.Fatalf("push = %v (%v), want event %d", push, err, e.ID)
	}

	c.in.Close()
	select {
	case err := <-c.done:
		if err != nil {
			t.Fatalf("serve after end of stdin = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("serve still running 5s after end of stdin")
	}
}

func archivedWorkIsNotOnTheBoard(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	goal, err := l.Add("p", "Old goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	walk(t, l, goal.ID, core.StateDone)
	loose, err := l.Add("p", "Unwanted", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	kept, err := l.Add("p", "Kept", ledger.AddOptions{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	for id, why := range map[string]string{goal.ID: "", loose.ID: "not wanted"} {
		if _, err := l.Archive(id, why); err != nil {
			t.Fatalf("archive %s: %v", id, err)
		}
	}
	b, err := s.board()
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if len(b.Goals) != 0 || len(b.Standalone) != 1 || b.Standalone[0].Work.ID != kept.ID {
		t.Fatalf("board = %+v, want only %s", b, kept.ID)
	}
}

func aGoalsDeliveryIsReadInTheGoalsOwnDirectory(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
		{"switch", "-q", "-c", "goal-branch"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	goal, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	task, err := l.Add("p", "Task", ledger.AddOptions{Parent: &goal.ID})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var bare map[string]json.RawMessage
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &bare)
	if d, ok := bare["delivery"]; ok {
		t.Fatalf("a goal with no directory has delivery %s, want no delivery key", d)
	}

	if err := l.SetCwd(task.ID, repo); err != nil {
		t.Fatalf("cwd: %v", err)
	}
	var placed struct {
		Delivery *struct {
			Branch string `json:"branch"`
		} `json:"delivery"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &placed)
	if placed.Delivery == nil || placed.Delivery.Branch != "goal-branch" {
		t.Fatalf("delivery = %+v, want it read in the task's directory on goal-branch", placed.Delivery)
	}
}
