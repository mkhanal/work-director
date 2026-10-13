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
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/runner"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	l, err := ledger.New(dir + "/ledger.db")
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return New(l, "", nil, nil)
}

func TestServe(t *testing.T) {
	t.Run("The Server Binds To Loopback", serverBindsToLoopback)
	t.Run("JSON Endpoints Serve The Board", jsonEndpointsServeTheBoard)
	t.Run("A Goal Serves What The Loop Decided And Where It Reached", aGoalServesWhatTheLoopDecidedAndWhereItReached)
	t.Run("A Goal Serves Each Report In Sections", aGoalServesEachReportInSections)
	t.Run("A Goal Waiting On The Person Serves Its Question", aGoalWaitingOnThePersonServesItsQuestion)
	t.Run("A Goal's Delivery Is Read In The Goal's Own Directory", aGoalsDeliveryIsReadInTheGoalsOwnDirectory)
	t.Run("The Board Places Every Item In One Band", theBoardPlacesEveryItemInOneBand)
	t.Run("Archived Work Is Not On The Board", archivedWorkIsNotOnTheBoard)
	t.Run("A Client Can Hold The Server On Its Standard Streams", aClientCanHoldTheServerOnItsStandardStreams)
	t.Run("A Work Item Serves Its Session's Conversation", aWorkItemServesItsSessionsConversation)
	t.Run("A Goal Serves What Each Of Its Tasks Is Doing", aGoalServesWhatEachOfItsTasksIsDoing)
	t.Run("A Goal Serves Its Sub-Goals As A Tree", aGoalServesItsSubGoalsAsATree)
	t.Run("A Goal Serves What Done Means And Why It Is Not Done Yet", aGoalServesWhatDoneMeansAndWhyItIsNotDoneYet)
	t.Run("A Goal Serves Everything Below It That Needs The Person", aGoalServesEverythingBelowItThatNeedsThePerson)
	t.Run("A Goal Serves Its Course", aGoalServesItsCourse)
	t.Run("A Proposed Breakdown Is Served As Proposed", aProposedBreakdownIsServedAsProposed)
	t.Run("A Goal Serves Why It Was Broken Down So", aGoalServesWhyItWasBrokenDownSo)
	t.Run("A Change Waiting For The Person Is Served With What Would Change", aChangeWaitingForThePersonIsServedWithWhatWouldChange)
	t.Run("A Goal Serves Its Record", aGoalServesItsRecord)
	t.Run("A Running Session's Question Puts Its Work In Needs You", aRunningSessionsQuestionPutsItsWorkInNeedsYou)
	t.Run("A Running Session's Report Moves Its Work On", aRunningSessionsReportMovesItsWorkOn)
	t.Run("A Running Session That Stops Without A Report Is Noted", aRunningSessionThatStopsWithoutAReportIsNoted)
	t.Run("A Running Goal Is Supervised While Serving", aRunningGoalIsSupervisedWhileServing)
	t.Run("Work Items Serve Over HTTP", workItemsServeOverHTTP)
	t.Run("A Work Item Serves The Next Gate It Waits On", aWorkItemServesTheNextGateItWaitsOn)
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

	l := s.ledger
	r := newReleaseTree(t, l)
	walk(t, l, r.rollback, core.StateRunning, core.StateNeedsInput)
	event(t, l, r.rollback, core.EventQuestion, "Which environments?")
	if err := l.SetFinish(r.top, core.FinishPR, core.FinishFromTaste); err != nil {
		t.Fatal(err)
	}
	shipped := func(title string, line core.FinishLine, landing string) string {
		t.Helper()
		g, err := l.Add("p", title, ledger.AddOptions{Kind: core.WorkGoal})
		if err != nil {
			t.Fatal(err)
		}
		if err := l.SetFinish(g.ID, line, core.FinishFromGoal); err != nil {
			t.Fatal(err)
		}
		if landing != "" {
			event(t, l, g.ID, core.EventPr, landing)
		}
		walk(t, l, g.ID, core.StateDone)
		return g.ID
	}
	pr := shipped("Changelog", core.FinishPR, "pull-request https://github.com/o/r/pull/512")
	merged := shipped("Hotfix", core.FinishMerged, "commit https://github.com/o/r/commit/5a5f3bb9e2")
	research := shipped("Pick a queue", core.FinishResearch, "")
	type summary struct {
		Work     core.Work    `json:"work"`
		Progress treeProgress `json:"progress"`
		NeedsYou int          `json:"needs_you"`
		Finish   *struct {
			Line   core.FinishLine   `json:"line"`
			Source core.FinishSource `json:"source"`
		} `json:"finish"`
		Finished *string `json:"finished"`
	}
	var tree struct {
		Goals []summary `json:"goals"`
	}
	getJSON(t, api.URL+"/api/board", http.StatusOK, &tree)
	byID := map[string]summary{}
	for _, g := range tree.Goals {
		byID[g.Work.ID] = g
	}
	if len(tree.Goals) != 5 {
		t.Fatalf("board goals = %d, want the 5 top goals and no sub-goal", len(tree.Goals))
	}
	if _, ok := byID[r.guide]; ok {
		t.Fatalf("the sub-goal %s is on the board", r.guide)
	}
	top := byID[r.top]
	if top.Progress != (treeProgress{Total: 5, Superseded: 1}) || top.NeedsYou != 2 || top.Finish == nil || top.Finish.Line != core.FinishPR || top.Finish.Source != core.FinishFromTaste || top.Finished != nil {
		t.Fatalf("top goal on the board = %+v", top)
	}
	for id, want := range map[string]string{pr: "Complete PR #512", merged: "Merged 5a5f3bb", research: "Research"} {
		if g := byID[id]; g.Finished == nil || *g.Finished != want {
			t.Fatalf("%s finished = %v, want %q", g.Work.Title, g.Finished, want)
		}
	}
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
	addr, _, err := New(l, "/bin/echo", nil, nil).Start(0)
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
	srv := New(l, "/bin/echo", nil, nil)
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

func aGoalServesEachReportInSections(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	goal, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	task, err := l.Add("p", "Task", ledger.AddOptions{Parent: &goal.ID})
	if err != nil {
		t.Fatal(err)
	}
	body := "DONE\nSTATUS: DONE (validate not fully green)\nFILES: scripts/a.ts, `b/c.go`, plus my earlier files (uncommitted)\nVERIFY: `go test` → 0\n  27/27 pass\nPR: none\nNOTES: all good"
	e, err := l.AddEvent(task.ID, core.EventReport, body)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var view struct {
		Reports map[string]core.Report `json:"reports"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &view)
	r, ok := view.Reports[strconv.Itoa(e.ID)]
	if !ok {
		t.Fatalf("reports = %+v, want the task's report by its event id", view.Reports)
	}
	if r.Verdict != "done" || r.Said != "(validate not fully green)" {
		t.Fatalf("verdict = %q, said %q", r.Verdict, r.Said)
	}
	if !slices.Equal(r.Files, []string{"scripts/a.ts", "b/c.go"}) || r.FileNote != "uncommitted; plus my earlier files" {
		t.Fatalf("files = %q, note %q", r.Files, r.FileNote)
	}
	want := []core.ReportSection{{Name: "VERIFY", Text: "`go test` → 0\n  27/27 pass"}, {Name: "PR", Text: "none"}, {Name: "NOTES", Text: "all good"}}
	if !slices.Equal(r.Sections, want) {
		t.Fatalf("sections = %+v, want %+v", r.Sections, want)
	}
}

func aGoalWaitingOnThePersonServesItsQuestion(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	goal, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var bare map[string]json.RawMessage
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &bare)
	if a, ok := bare["asking"]; ok {
		t.Fatalf("a goal asking nothing has asking %s", a)
	}
	for _, st := range []core.State{core.StateRunning, core.StateNeedsInput} {
		if _, err := l.Transition(goal.ID, st); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.AddEvent(goal.ID, core.EventQuestion, "Tag v1.0.0?\n- yes (recommended)\n- no\nWho cherry-picks?"); err != nil {
		t.Fatal(err)
	}
	var view struct {
		Asking *runner.Question `json:"asking"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &view)
	if q := view.Asking; q == nil || len(q.Items) != 2 || len(q.Items[0].Options) != 2 || q.Items[0].Options[0].Label != "yes (recommended)" || q.Items[1].Question != "Who cherry-picks?" {
		t.Fatalf("asking = %+v, want both questions, the first with its choices", view.Asking)
	}
	if len(view.Asking.Items[1].Options) != 0 {
		t.Fatalf("an open question offers %+v, want none", view.Asking.Items[1].Options)
	}

	// A task in needs-input waits on what it asked, else on what its report needs.
	asked, err := l.Add("p", "Asked", ledger.AddOptions{Parent: &goal.ID})
	if err != nil {
		t.Fatal(err)
	}
	reported, err := l.Add("p", "Reported", ledger.AddOptions{Parent: &goal.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{asked.ID, reported.ID} {
		for _, st := range []core.State{core.StateRunning, core.StateNeedsInput} {
			if _, err := l.Transition(w, st); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := l.AddEvent(asked.ID, core.EventQuestion, "Should the banner stay?"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AddEvent(reported.ID, core.EventReport, "NEEDS-INPUT\nSTATUS: NEEDS-INPUT which port should it use?\nNOTES: 8080 or 9090"); err != nil {
		t.Fatal(err)
	}
	var waiting struct {
		Waiting []struct {
			Task     string          `json:"task"`
			Question runner.Question `json:"question"`
		} `json:"waiting"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &waiting)
	if len(waiting.Waiting) != 2 {
		t.Fatalf("waiting = %+v, want both tasks", waiting.Waiting)
	}
	byTask := map[string]runner.Question{}
	for _, w := range waiting.Waiting {
		byTask[w.Task] = w.Question
	}
	if q := byTask[asked.ID]; len(q.Items) != 1 || q.Items[0].Question != "Should the banner stay?" || len(q.Items[0].Options) != 2 {
		t.Fatalf("the asking task waits on %+v, want its question with Yes and No", q)
	}
	if q := byTask[reported.ID]; len(q.Items) != 1 || q.Items[0].Question != "which port should it use?" {
		t.Fatalf("the reporting task waits on %+v, want what its report said it needs", q)
	}

	if _, err := l.Answer(goal.ID, core.Decision{Question: "Tag v1.0.0?", Answer: "yes", Source: core.AnsweredByPerson}); err != nil {
		t.Fatal(err)
	}
	bare = nil
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &bare)
	if a, ok := bare["asking"]; ok {
		t.Fatalf("an answered goal still has asking %s", a)
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

// sessionRunner is a runner whose conversations are canned per session.
type sessionRunner struct {
	runner.Runner
	steps  map[string][]runner.Entry
	broken map[string]bool
	sent   []string
}

func (r *sessionRunner) Conversation(h runner.Handle) ([]runner.Entry, error) {
	if r.broken[h.Session] {
		return nil, fmt.Errorf("store of %s unreadable", h.Session)
	}
	return r.steps[h.Session], nil
}

func (r *sessionRunner) Status(h runner.Handle) (runner.RunnerStatus, error) {
	return runner.StatusWaiting, nil
}

func (r *sessionRunner) Usage(h runner.Handle) (runner.Usage, error) {
	return runner.Usage{Model: "claude-opus-5-5", Tokens: 1200, Tools: 3}, nil
}

func (r *sessionRunner) Subagents(h runner.Handle) ([]runner.Subagent, error) {
	return []runner.Subagent{{ID: "sub1", Description: "Probe", Type: "fork", Done: true}}, nil
}

func (r *sessionRunner) SubagentConversation(h runner.Handle, agent string) ([]runner.Entry, error) {
	if agent != "sub1" {
		return nil, fmt.Errorf("session %s has no subagent %s", h.Session, agent)
	}
	return []runner.Entry{{Kind: runner.EntryText, Text: "probing"}}, nil
}

func (r *sessionRunner) Send(h *runner.Handle, text string) error {
	r.sent = append(r.sent, text)
	return nil
}

// withSessions gives s sessions: work whose session is set runs in it on r.
func withSessions(s *Server, r *sessionRunner) {
	s.sessions = func(w core.Work) (runner.Runner, runner.Handle, bool, error) {
		if w.Session == nil {
			return nil, runner.Handle{}, false, nil
		}
		return r, runner.Handle{Runner: "fake", Session: *w.Session}, true, nil
	}
}

func colourQuestion() *runner.Question {
	return &runner.Question{ID: "q1", Items: []runner.QuestionItem{{Header: "Colour", Question: "Which colour?", Options: []runner.Option{{Label: "Red"}, {Label: "Blue"}}}}}
}

// inSession adds running work attached to session.
func inSession(t *testing.T, s *Server, title, session string, parent *string) core.Work {
	t.Helper()
	w, err := s.ledger.Add("p", title, ledger.AddOptions{Parent: parent})
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []core.State{core.StateBriefed, core.StateRunning} {
		if _, err := s.ledger.Transition(w.ID, st); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ledger.SetSession(w.ID, ledger.SessionInfo{Runner: "fake", Session: session}); err != nil {
		t.Fatal(err)
	}
	w, err = s.ledger.Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func aWorkItemServesItsSessionsConversation(t *testing.T) {
	s := newTestServer(t)
	r := &sessionRunner{steps: map[string][]runner.Entry{"ses-1": {
		{Kind: runner.EntryPrompt, Text: "the brief"},
		{Kind: runner.EntryThinking, Text: "Weighing it"},
		{Kind: runner.EntryTool, Tool: &runner.ToolCall{ID: "t1", Name: "Bash", Summary: "go test ./..."}},
		{Kind: runner.EntryQuestion, Question: colourQuestion()},
	}}}
	withSessions(s, r)
	w := inSession(t, s, "Asking", "ses-1", nil)
	idle, err := s.ledger.Add("p", "No session", ledger.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	var all conversationView
	getJSON(t, ts.URL+"/api/conversation/"+w.ID, http.StatusOK, &all)
	if all.Total != 4 || len(all.Entries) != 4 || all.Entries[1].Text != "Weighing it" || all.Status != runner.StatusWaiting {
		t.Fatalf("conversation = %+v, want its four steps and status", all)
	}
	if all.Asking == nil || all.Asking.Items[0].Question != "Which colour?" {
		t.Fatalf("asking = %+v, want the colour question", all.Asking)
	}
	var tail conversationView
	getJSON(t, ts.URL+"/api/conversation/"+w.ID+"?from=2", http.StatusOK, &tail)
	if tail.From != 2 || tail.Total != 4 || len(tail.Entries) != 2 || tail.Entries[0].Tool.Name != "Bash" {
		t.Fatalf("from 2 = %+v, want the last two steps", tail)
	}
	if all.Usage == nil || all.Usage.Tools != 3 || len(all.Subagents) != 1 {
		t.Fatalf("conversation = %+v, want the session's usage and subagents", all)
	}
	var sub conversationView
	getJSON(t, ts.URL+"/api/conversation/"+w.ID+"?agent=sub1", http.StatusOK, &sub)
	if len(sub.Entries) != 1 || sub.Entries[0].Text != "probing" || sub.Agent != "sub1" {
		t.Fatalf("subagent conversation = %+v, want its own step", sub)
	}
	getJSON(t, ts.URL+"/api/conversation/"+w.ID+"?agent=nope", http.StatusNotFound, &errorBody{})
	var none conversationView
	getJSON(t, ts.URL+"/api/conversation/"+idle.ID, http.StatusOK, &none)
	if none.Status != "none" || none.Entries == nil || len(none.Entries) != 0 || none.Asking != nil {
		t.Fatalf("no session = %+v, want status none and no steps", none)
	}
	getJSON(t, ts.URL+"/api/conversation/nope", http.StatusNotFound, &errorBody{})
	getJSON(t, ts.URL+"/api/conversation/"+w.ID+"?from=-1", http.StatusBadRequest, &errorBody{})
	getJSON(t, ts.URL+"/api/conversation/"+w.ID+"?from=x", http.StatusBadRequest, &errorBody{})

	c := startStdio(t, s)
	c.send(`{"id": 2, "method": "conversation", "params": {"id": "` + w.ID + `", "agent": "sub1"}}`)
	var subViaStdio conversationView
	if err := json.Unmarshal(c.reply(2)["result"], &subViaStdio); err != nil || len(subViaStdio.Entries) != 1 {
		t.Fatalf("stdio subagent conversation = %+v (%v)", subViaStdio, err)
	}
	c.send(`{"id": 1, "method": "conversation", "params": {"id": "` + w.ID + `", "from": 3}}`)
	var viaStdio conversationView
	if err := json.Unmarshal(c.reply(1)["result"], &viaStdio); err != nil || len(viaStdio.Entries) != 1 || viaStdio.Entries[0].Kind != runner.EntryQuestion {
		t.Fatalf("stdio conversation = %+v (%v), want the question step", viaStdio, err)
	}
}

func aGoalServesWhatEachOfItsTasksIsDoing(t *testing.T) {
	s := newTestServer(t)
	r := &sessionRunner{
		steps: map[string][]runner.Entry{
			"tool":  {{Kind: runner.EntryTool, Tool: &runner.ToolCall{Name: "Bash", Summary: "go test ./..."}}},
			"ask":   {{Kind: runner.EntryQuestion, Question: colourQuestion()}},
			"think": {{Kind: runner.EntryThinking, Text: "Weighing it\nand more"}},
		},
		broken: map[string]bool{"broken": true},
	}
	withSessions(s, r)
	goal, err := s.ledger.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	var empty goalView
	if empty, err = s.goal(goal.ID); err != nil || empty.Activity == nil || len(empty.Activity) != 0 {
		t.Fatalf("activity with no working task = %v (%v), want {}", empty.Activity, err)
	}
	tool := inSession(t, s, "Tool", "tool", &goal.ID)
	ask := inSession(t, s, "Ask", "ask", &goal.ID)
	think := inSession(t, s, "Think", "think", &goal.ID)
	broken := inSession(t, s, "Broken", "broken", &goal.ID)
	done := inSession(t, s, "Done", "tool", &goal.ID)
	if _, err := s.ledger.Transition(done.ID, core.StateReview); err != nil {
		t.Fatal(err)
	}
	g, err := s.goal(goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a := g.Activity[tool.ID]; a.Now != "Bash · go test ./..." || a.Kind != runner.EntryTool || a.Asking != nil || a.Status != runner.StatusWaiting {
		t.Fatalf("tool activity = %+v", a)
	}
	if a := g.Activity[ask.ID]; a.Now != "Which colour? (Red / Blue)" || a.Asking == nil {
		t.Fatalf("question activity = %+v", a)
	}
	if a := g.Activity[think.ID]; a.Now != "Weighing it" {
		t.Fatalf("thinking activity = %+v", a)
	}
	if a := g.Activity[broken.ID]; !strings.Contains(a.Error, "unreadable") {
		t.Fatalf("unreadable activity = %+v, want its error", a)
	}
	if _, ok := g.Activity[goal.ID]; ok {
		t.Fatal("a goal with no planning session has activity")
	}
	planned, err := s.ledger.Add("p", "Planned goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ledger.SetSession(planned.ID, ledger.SessionInfo{Runner: "fake", Session: "think"}); err != nil {
		t.Fatal(err)
	}
	if pg, err := s.goal(planned.ID); err != nil || pg.Activity[planned.ID].Now != "Weighing it" {
		t.Fatalf("planning activity = %+v (%v), want the goal's own session read", pg.Activity, err)
	}
	if a, ok := g.Activity[done.ID]; !ok || a.Now != "Bash · go test ./..." {
		t.Fatalf("a task in review = %+v, want its activity too", a)
	}
	if a := g.Activity[tool.ID]; a.Usage == nil || a.Usage.Tokens != 1200 || len(a.Subagents) != 1 || a.Subagents[0].Description != "Probe" {
		t.Fatalf("tool activity = %+v, want the session's usage and subagent", a)
	}
}

func aRunningSessionsQuestionPutsItsWorkInNeedsYou(t *testing.T) {
	s := newTestServer(t)
	r := &sessionRunner{steps: map[string][]runner.Entry{
		"ask":   {{Kind: runner.EntryText, Text: "looking"}, {Kind: runner.EntryQuestion, Question: colourQuestion()}},
		"quiet": {{Kind: runner.EntryText, Text: "working"}},
	}}
	withSessions(s, r)
	ask := inSession(t, s, "Ask", "ask", nil)
	quiet := inSession(t, s, "Quiet", "quiet", nil)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- s.watchSessions(stop) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		w, err := s.ledger.Get(ask.ID)
		if err != nil {
			t.Fatal(err)
		}
		if w.State == core.StateNeedsInput {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("asking work is still %s", w.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Back to running with the same question still last: a later pass must not
	// file it again.
	if _, err := s.ledger.Transition(ask.ID, core.StateRunning); err != nil {
		t.Fatal(err)
	}
	time.Sleep(sessionWatch + time.Second)
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	qs, err := s.ledger.Events(ask.ID, kindOf(core.EventQuestion))
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 || qs[0].Body != "Which colour? (Red / Blue)" {
		t.Fatalf("questions filed = %+v, want the one question once", qs)
	}
	if w, _ := s.ledger.Get(ask.ID); w.State != core.StateRunning {
		t.Fatalf("a filed question was escalated again: %s", w.State)
	}
	if w, _ := s.ledger.Get(quiet.ID); w.State != core.StateRunning {
		t.Fatalf("work asking nothing moved to %s", w.State)
	}
	if len(r.sent) != 0 {
		t.Fatalf("the watcher sent %q", r.sent)
	}
}

func kindOf(k core.EventKind) *core.EventKind { return &k }

func aWorkItemServesTheNextGateItWaitsOn(t *testing.T) {
	s := newTestServer(t)
	w, err := s.ledger.Add("p", "Gated", ledger.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.routes())
	defer ts.Close()
	gate := func(want string, argv ...string) {
		t.Helper()
		var g gateView
		getJSON(t, ts.URL+"/api/work/"+w.ID+"/gate", http.StatusOK, &g)
		if g.Gate != want || !slices.Equal(g.Argv, argv) || (want != "" && g.Why == "") {
			t.Fatalf("gate = %+v, want %q %v with a reason", g, want, argv)
		}
	}
	event := func(kind core.EventKind, body string) {
		t.Helper()
		if _, err := s.ledger.AddEvent(w.ID, kind, body); err != nil {
			t.Fatal(err)
		}
	}
	gate("")
	for _, st := range []core.State{core.StateRunning, core.StateReview} {
		if _, err := s.ledger.Transition(w.ID, st); err != nil {
			t.Fatal(err)
		}
	}
	event(core.EventReport, "DONE\nSTATUS: DONE")
	gate("verify", "verify", w.ID)
	event(core.EventVerify, "fail\ngo test → 1")
	gate("verify", "verify", w.ID)
	event(core.EventVerify, "pass\ngo test → 0")
	gate("land", "pr", w.ID)
	event(core.EventReport, "DONE\nSTATUS: DONE\nNOTES: again")
	gate("verify", "verify", w.ID)
	event(core.EventVerify, "pass\ngo test → 0")
	event(core.EventPr, "commit https://example.test/c/1")
	gate("soft-done", "soft-done", w.ID)
	if _, err := s.ledger.SoftDone(w.ID, false); err != nil {
		t.Fatal(err)
	}
	gate("done", "done", w.ID)
	getJSON(t, ts.URL+"/api/work/nope/gate", http.StatusNotFound, &errorBody{})

	c := startStdio(t, s)
	c.send(`{"id": 1, "method": "gate", "params": {"id": "` + w.ID + `"}}`)
	var viaStdio gateView
	if err := json.Unmarshal(c.reply(1)["result"], &viaStdio); err != nil || viaStdio.Gate != "done" {
		t.Fatalf("stdio gate = %+v (%v)", viaStdio, err)
	}
}

func aRunningSessionsReportMovesItsWorkOn(t *testing.T) {
	s := newTestServer(t)
	r := &sessionRunner{steps: map[string][]runner.Entry{
		"done":    {{Kind: runner.EntryText, Text: "working"}, {Kind: runner.EntryText, Text: "STATUS: DONE\nFILES: a.go"}},
		"blocked": {{Kind: runner.EntryText, Text: "STATUS: BLOCKED\nNOTES: no access"}},
		"needs":   {{Kind: runner.EntryText, Text: "STATUS: NEEDS-INPUT\nNOTES: which db?"}},
	}}
	withSessions(s, r)
	done := inSession(t, s, "Done", "done", nil)
	blocked := inSession(t, s, "Blocked", "blocked", nil)
	needs := inSession(t, s, "Needs", "needs", nil)
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.watchSessions(stop) }()
	want := map[string]core.State{done.ID: core.StateReview, blocked.ID: core.StateBlocked, needs.ID: core.StateNeedsInput}
	deadline := time.Now().Add(10 * time.Second)
	for id, st := range want {
		for {
			w, err := s.ledger.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if w.State == st {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s is %s, want %s", w.Title, w.State, st)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Told something, the work runs again while its transcript still ends on
	// the same report: a later pass must not file it again.
	if _, err := s.ledger.Transition(done.ID, core.StateRunning); err != nil {
		t.Fatal(err)
	}
	time.Sleep(sessionWatch + time.Second)
	close(stop)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	reports, err := s.ledger.Events(done.ID, kindOf(core.EventReport))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || !strings.HasPrefix(reports[0].Body, "DONE\nSTATUS: DONE") {
		t.Fatalf("reports = %+v, want the one report once", reports)
	}
	if w, _ := s.ledger.Get(done.ID); w.State != core.StateRunning {
		t.Fatalf("a filed report moved the work again: %s", w.State)
	}
}

func aRunningSessionThatStopsWithoutAReportIsNoted(t *testing.T) {
	s := newTestServer(t)
	r := &sessionRunner{steps: map[string][]runner.Entry{
		"quiet": {{Kind: runner.EntryText, Text: "working"}, {Kind: runner.EntryText, Text: "Wiring it in belongs to the collector task.\nSay so if I should."}},
	}}
	withSessions(s, r)
	quiet := inSession(t, s, "Quiet", "quiet", nil)
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.watchSessions(stop) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		notes, err := s.ledger.Events(quiet.ID, kindOf(core.EventNote))
		if err != nil {
			t.Fatal(err)
		}
		if len(notes) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no note that the session stopped without a report")
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(sessionWatch + time.Second)
	close(stop)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	notes, err := s.ledger.Events(quiet.ID, kindOf(core.EventNote))
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Body != "stopped without a report: Wiring it in belongs to the collector task." {
		t.Fatalf("notes = %+v, want one note with the first line it said", notes)
	}
	if w, _ := s.ledger.Get(quiet.ID); w.State != core.StateRunning {
		t.Fatalf("a stop without a report moved the work to %s", w.State)
	}
}

func aRunningGoalIsSupervisedWhileServing(t *testing.T) {
	s := newTestServer(t)
	withSessions(s, &sessionRunner{steps: map[string][]runner.Entry{}})
	running, err := s.ledger.Add("p", "Running goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ledger.Transition(running.ID, core.StateRunning); err != nil {
		t.Fatal(err)
	}
	failing, err := s.ledger.Add("p", "Failing goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ledger.Transition(failing.ID, core.StateRunning); err != nil {
		t.Fatal(err)
	}
	queued, err := s.ledger.Add("p", "Queued goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	nested, err := s.ledger.Add("p", "Nested goal", ledger.AddOptions{Kind: core.WorkGoal, Parent: &running.ID})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := s.ledger.Add("p", "Leaf", ledger.AddOptions{Parent: &nested.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []core.State{core.StateBriefed, core.StateRunning} {
		if _, err := s.ledger.Transition(leaf.ID, st); err != nil {
			t.Fatal(err)
		}
	}
	if w, err := s.ledger.Get(nested.ID); err != nil || w.State != core.StateRunning {
		t.Fatalf("nested goal = %+v, %v; want it running", w, err)
	}
	var mu sync.Mutex
	passes := map[string]int{}
	s.supervise = func(goal core.Work) error {
		mu.Lock()
		defer mu.Unlock()
		passes[goal.ID]++
		if goal.ID == failing.ID {
			return fmt.Errorf("no runner named overseer")
		}
		return nil
	}
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.watchSessions(stop) }()
	time.Sleep(3*sessionWatch + time.Second)
	close(stop)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if passes[running.ID] < 2 || passes[queued.ID] != 0 || passes[nested.ID] != 0 {
		t.Fatalf("passes = %v, want the running top goal supervised every few seconds, the queued one and the sub-goal not", passes)
	}
	notes, err := s.ledger.Events(failing.ID, kindOf(core.EventNote))
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Body != "supervising failed: no runner named overseer" {
		t.Fatalf("notes on the failing goal = %+v, want the failure filed once", notes)
	}
}

// releaseTree is a top goal planned into sub-goals at two depths, plus tasks
// of an older plan that share a heading:
//
//	Release notes
//	├─ Deployment guide (goal, done when: a guide covers rollback)
//	│  ├─ Collector (running, in a session, on its second step)
//	│  └─ Rollback (queued, after Collector)
//	├─ Customer note (needs-input, asking)
//	├─ Old route (superseded)
//	├─ Draft (heading Docs)  ┐ one group
//	├─ Polish (heading Docs) ┘
//	└─ Loose (heading "(no heading)")
type releaseTree struct {
	top, guide, collector, rollback, note, old, draft, polish, loose string
}

func newReleaseTree(t *testing.T, l *ledger.Ledger) releaseTree {
	t.Helper()
	add := func(title string, o ledger.AddOptions) string {
		t.Helper()
		w, err := l.Add("p", title, o)
		if err != nil {
			t.Fatal(err)
		}
		return w.ID
	}
	var r releaseTree
	r.top = add("Release notes", ledger.AddOptions{Kind: core.WorkGoal})
	walk(t, l, r.top, core.StateRunning)
	r.guide = add("Deployment guide", ledger.AddOptions{Kind: core.WorkGoal, Parent: &r.top})
	if err := l.SetDoneWhen(r.guide, "a guide covers rollback", core.AnsweredByDirector); err != nil {
		t.Fatal(err)
	}
	r.collector = add("Collector", ledger.AddOptions{Parent: &r.guide})
	r.rollback = add("Rollback", ledger.AddOptions{Parent: &r.guide})
	if err := l.SetAfter(r.rollback, []string{r.collector}); err != nil {
		t.Fatal(err)
	}
	walk(t, l, r.collector, core.StateBriefed, core.StateRunning)
	if err := l.SetSession(r.collector, ledger.SessionInfo{Runner: "claude", Session: "s-collector"}); err != nil {
		t.Fatal(err)
	}
	if err := l.SetSteps(r.collector, []core.Step{{Text: "Read the changes", State: core.StepDone}, {Text: "Write the customer note template", State: core.StepDoing}}); err != nil {
		t.Fatal(err)
	}
	r.note = add("Customer note", ledger.AddOptions{Parent: &r.top})
	walk(t, l, r.note, core.StateRunning, core.StateNeedsInput)
	event(t, l, r.note, core.EventQuestion, "Which tone?\n- formal\n- casual")
	r.old = add("Old route", ledger.AddOptions{Parent: &r.top})
	if _, err := l.Supersede(r.old, "the guide covers it"); err != nil {
		t.Fatal(err)
	}
	docs, none := "Docs", "(no heading)"
	r.draft = add("Draft", ledger.AddOptions{Parent: &r.top, Heading: &docs})
	r.polish = add("Polish", ledger.AddOptions{Parent: &r.top, Heading: &docs})
	r.loose = add("Loose", ledger.AddOptions{Parent: &r.top, Heading: &none})
	return r
}

type treeProgress struct {
	Done       int `json:"done"`
	Total      int `json:"total"`
	Superseded int `json:"superseded"`
}

type treeNode struct {
	Work         core.Work `json:"work"`
	Group        bool      `json:"group"`
	DoneWhen     *string   `json:"done_when"`
	After        []string  `json:"after"`
	WaitingOn    []string  `json:"waiting_on"`
	WhyNotMoving *string   `json:"why_not_moving"`
	Owner        *struct {
		Runner string  `json:"runner"`
		Agent  *string `json:"agent"`
	} `json:"owner"`
	PlannedInto *int          `json:"planned_into"`
	Progress    *treeProgress `json:"progress"`
	Doing       *string       `json:"doing"`
	Steps       []core.Step   `json:"steps"`
	Judged      *struct {
		Verdict string `json:"verdict"`
		Why     string `json:"why"`
	} `json:"judged"`
	NeedsYou int        `json:"needs_you"`
	Children []treeNode `json:"children"`
}

func aGoalServesItsSubGoalsAsATree(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	r := newReleaseTree(t, l)
	event(t, l, r.collector, core.EventJudged, "missed: rollback is not covered")
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var view struct {
		Chain []struct {
			ID    string     `json:"id"`
			Title string     `json:"title"`
			State core.State `json:"state"`
		} `json:"chain"`
		Tree  []treeNode        `json:"tree"`
		Paths map[string]string `json:"paths"`
	}
	getJSON(t, api.URL+"/api/goal/"+r.top, http.StatusOK, &view)
	if len(view.Chain) != 1 || view.Chain[0].ID != r.top || view.Chain[0].Title != "Release notes" || view.Chain[0].State != core.StateRunning {
		t.Fatalf("chain of the top goal = %+v", view.Chain)
	}
	var titles []string
	for _, n := range view.Tree {
		titles = append(titles, n.Work.Title)
	}
	if !slices.Equal(titles, []string{"Deployment guide", "Customer note", "Old route", "Docs", "Loose"}) {
		t.Fatalf("tree = %q, want sub-goals, the Docs group and the loose task", titles)
	}
	guide, note, old, group, loose := view.Tree[0], view.Tree[1], view.Tree[2], view.Tree[3], view.Tree[4]
	if guide.Group || guide.DoneWhen == nil || *guide.DoneWhen != "a guide covers rollback" || guide.PlannedInto == nil || *guide.PlannedInto != 2 || guide.Owner != nil {
		t.Fatalf("guide = %+v, want its done-when and planned into 2", guide)
	}
	if guide.Progress == nil || *guide.Progress != (treeProgress{Total: 2}) || len(guide.Children) != 2 {
		t.Fatalf("guide progress = %+v, children %d", guide.Progress, len(guide.Children))
	}
	collector, rollback := guide.Children[0], guide.Children[1]
	if collector.Owner == nil || collector.Owner.Runner != "claude" || collector.Owner.Agent != nil || collector.PlannedInto != nil || collector.Progress != nil {
		t.Fatalf("collector = %+v, want owned by claude", collector)
	}
	if collector.Doing == nil || *collector.Doing != "Write the customer note template" || len(collector.Steps) != 2 || collector.WhyNotMoving != nil {
		t.Fatalf("collector doing %v, steps %+v, why %v", collector.Doing, collector.Steps, collector.WhyNotMoving)
	}
	if collector.Judged == nil || collector.Judged.Verdict != "missed" || collector.Judged.Why != "rollback is not covered" {
		t.Fatalf("collector judged = %+v", collector.Judged)
	}
	if !slices.Equal(rollback.After, []string{r.collector}) || !slices.Equal(rollback.WaitingOn, []string{"Collector"}) || rollback.WhyNotMoving == nil || *rollback.WhyNotMoving != "waits on Collector" {
		t.Fatalf("rollback after %v, waiting on %v, why %v", rollback.After, rollback.WaitingOn, rollback.WhyNotMoving)
	}
	if rollback.Owner != nil || rollback.Doing != nil || rollback.Steps == nil || rollback.Judged != nil || rollback.Children == nil {
		t.Fatalf("rollback = %+v, want no owner, doing or judgement and empty steps and children", rollback)
	}
	if note.WhyNotMoving == nil || *note.WhyNotMoving != "waits for you" || note.NeedsYou != 1 || guide.NeedsYou != 0 {
		t.Fatalf("note why %v needs %d; guide needs %d", note.WhyNotMoving, note.NeedsYou, guide.NeedsYou)
	}
	if old.WhyNotMoving == nil || *old.WhyNotMoving != "superseded: the guide covers it" {
		t.Fatalf("old why = %v", old.WhyNotMoving)
	}
	if !group.Group || group.Work.ID != "group:"+r.top+":1" || group.Work.Kind != core.WorkGoal || group.Work.State != core.StateQueued || group.DoneWhen != nil || len(group.Children) != 2 || group.Children[0].Work.ID != r.draft || group.Children[1].Work.ID != r.polish {
		t.Fatalf("group = %+v, want Draft and Polish under one grouped node", group)
	}
	if loose.Group || loose.Work.ID != r.loose {
		t.Fatalf("loose = %+v, want the task itself", loose)
	}
	if view.Paths[r.rollback] != "Deployment guide › Rollback" || view.Paths[r.guide] != "Deployment guide" || view.Paths[r.draft] != "Draft" || len(view.Paths) != 8 {
		t.Fatalf("paths = %v", view.Paths)
	}

	var nested struct {
		Chain []struct {
			ID string `json:"id"`
		} `json:"chain"`
		Tree  []treeNode        `json:"tree"`
		Paths map[string]string `json:"paths"`
	}
	getJSON(t, api.URL+"/api/goal/"+r.guide, http.StatusOK, &nested)
	if len(nested.Chain) != 2 || nested.Chain[0].ID != r.top || nested.Chain[1].ID != r.guide {
		t.Fatalf("chain of the nested goal = %+v, want the top goal first", nested.Chain)
	}
	if len(nested.Tree) != 2 || nested.Paths[r.rollback] != "Rollback" || len(nested.Paths) != 2 {
		t.Fatalf("nested tree %d nodes, paths %v", len(nested.Tree), nested.Paths)
	}
}

type doneView struct {
	DoneWhen *string `json:"done_when"`
	Finish   *struct {
		Line   core.FinishLine   `json:"line"`
		Source core.FinishSource `json:"source"`
	} `json:"finish"`
	Progress       treeProgress `json:"progress"`
	NotDoneBecause []string     `json:"not_done_because"`
}

func aGoalServesWhatDoneMeansAndWhyItIsNotDoneYet(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	r := newReleaseTree(t, l)
	if err := l.SetDoneWhen(r.top, "the notes are out", core.AnsweredByDirector); err != nil {
		t.Fatal(err)
	}
	if err := l.SetFinish(r.top, core.FinishPR, core.FinishFromTaste); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var top doneView
	getJSON(t, api.URL+"/api/goal/"+r.top, http.StatusOK, &top)
	if top.DoneWhen == nil || *top.DoneWhen != "the notes are out" || top.Finish == nil || top.Finish.Line != core.FinishPR || top.Finish.Source != core.FinishFromTaste {
		t.Fatalf("top done-when %v, finish %+v", top.DoneWhen, top.Finish)
	}
	if top.Progress != (treeProgress{Done: 0, Total: 5, Superseded: 1}) {
		t.Fatalf("top progress = %+v, want 0 of 5 with 1 superseded apart", top.Progress)
	}
	want := []string{"5 sub-goals open", "done-when not judged met yet", "finish line (complete PR) not reached: DONE report, passing verify, pull request"}
	if !slices.Equal(top.NotDoneBecause, want) {
		t.Fatalf("top not done because %q, want %q", top.NotDoneBecause, want)
	}

	var guide doneView
	getJSON(t, api.URL+"/api/goal/"+r.guide, http.StatusOK, &guide)
	if guide.DoneWhen == nil || *guide.DoneWhen != "a guide covers rollback" || guide.Finish == nil || guide.Finish.Line != core.FinishPR {
		t.Fatalf("nested done-when %v, finish %+v; want its own done-when and its top goal's finish", guide.DoneWhen, guide.Finish)
	}
	if want := []string{"2 sub-goals open", "done-when not judged met yet"}; !slices.Equal(guide.NotDoneBecause, want) {
		t.Fatalf("nested not done because %q, want %q", guide.NotDoneBecause, want)
	}

	walk(t, l, r.rollback, core.StateDone)
	walk(t, l, r.collector, core.StateReview)
	event(t, l, r.collector, core.EventReport, "DONE\nSTATUS: DONE")
	if _, err := l.SoftDone(r.collector, false); err != nil {
		t.Fatal(err)
	}
	walk(t, l, r.collector, core.StateDone)
	getJSON(t, api.URL+"/api/goal/"+r.guide, http.StatusOK, &guide)
	if guide.NotDoneBecause == nil || len(guide.NotDoneBecause) != 0 || guide.Progress != (treeProgress{Done: 2, Total: 2}) {
		t.Fatalf("a done sub-goal: not done because %q, progress %+v; want [] and 2 of 2", guide.NotDoneBecause, guide.Progress)
	}
}

type needsYouView struct {
	NeedsYou []struct {
		Work     string          `json:"work"`
		Path     string          `json:"path"`
		Question runner.Question `json:"question"`
	} `json:"needs_you"`
}

func aGoalServesEverythingBelowItThatNeedsThePerson(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	r := newReleaseTree(t, l)
	walk(t, l, r.rollback, core.StateRunning, core.StateNeedsInput)
	event(t, l, r.rollback, core.EventReport, "NEEDS-INPUT\nSTATUS: NEEDS-INPUT which environments roll back?")
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var top needsYouView
	getJSON(t, api.URL+"/api/goal/"+r.top, http.StatusOK, &top)
	if len(top.NeedsYou) != 2 {
		t.Fatalf("needs you = %+v, want the note and the rollback", top.NeedsYou)
	}
	note, rollback := top.NeedsYou[0], top.NeedsYou[1]
	if note.Work != r.note || note.Path != "Customer note" || len(note.Question.Items) != 1 || note.Question.Items[0].Question != "Which tone?" || len(note.Question.Items[0].Options) != 2 {
		t.Fatalf("note = %+v", note)
	}
	if rollback.Work != r.rollback || rollback.Path != "Deployment guide › Rollback" || rollback.Question.Items[0].Question != "which environments roll back?" {
		t.Fatalf("rollback = %+v", rollback)
	}
	var guide needsYouView
	getJSON(t, api.URL+"/api/goal/"+r.guide, http.StatusOK, &guide)
	if len(guide.NeedsYou) != 1 || guide.NeedsYou[0].Work != r.rollback || guide.NeedsYou[0].Path != "Rollback" {
		t.Fatalf("nested needs you = %+v, want only the rollback", guide.NeedsYou)
	}
	if _, err := l.Answer(r.note, core.Decision{Question: "Which tone?", Answer: "formal", Source: core.AnsweredByPerson}); err != nil {
		t.Fatal(err)
	}
	var none map[string]json.RawMessage
	getJSON(t, api.URL+"/api/goal/"+r.top, http.StatusOK, &top)
	if len(top.NeedsYou) != 1 || top.NeedsYou[0].Work != r.rollback {
		t.Fatalf("after the answer needs you = %+v", top.NeedsYou)
	}
	quiet, err := l.Add("p", "Quiet", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	getJSON(t, api.URL+"/api/goal/"+quiet.ID, http.StatusOK, &none)
	if string(none["needs_you"]) != "[]" || string(none["superseded"]) != "[]" || string(none["course"]) != "null" {
		t.Fatalf("a quiet goal: needs_you %s, superseded %s, course %s", none["needs_you"], none["superseded"], none["course"])
	}
}

func aGoalServesItsCourse(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	r := newReleaseTree(t, l)
	event(t, l, r.top, core.EventCourse, "pivot: the guide replaces the old route")
	event(t, l, r.top, core.EventCourse, "on course: the guide is half written")
	api := httptest.NewServer(s.routes())
	defer api.Close()
	type courseView struct {
		Course *struct {
			Verdict string `json:"verdict"`
			Why     string `json:"why"`
			At      string `json:"at"`
		} `json:"course"`
		Superseded []struct {
			Work  string `json:"work"`
			Title string `json:"title"`
			Why   string `json:"why"`
			At    string `json:"at"`
		} `json:"superseded"`
	}
	for _, id := range []string{r.top, r.guide} {
		var v courseView
		getJSON(t, api.URL+"/api/goal/"+id, http.StatusOK, &v)
		if v.Course == nil || v.Course.Verdict != "on course" || v.Course.Why != "the guide is half written" || v.Course.At == "" {
			t.Fatalf("course of %s = %+v, want the latest verdict", id, v.Course)
		}
		if id == r.top && (len(v.Superseded) != 1 || v.Superseded[0].Work != r.old || v.Superseded[0].Title != "Old route" || v.Superseded[0].Why != "the guide covers it" || v.Superseded[0].At == "") {
			t.Fatalf("superseded = %+v, want the old route with why", v.Superseded)
		}
		if id == r.guide && len(v.Superseded) != 0 {
			t.Fatalf("nested superseded = %+v, want none", v.Superseded)
		}
	}
	event(t, l, r.top, core.EventCourse, "unstated")
	var v courseView
	getJSON(t, api.URL+"/api/goal/"+r.top, http.StatusOK, &v)
	if v.Course == nil || v.Course.Verdict != "unstated" || v.Course.Why != "" {
		t.Fatalf("an unstated course = %+v", v.Course)
	}
}

type servedEntry struct {
	Event int    `json:"event"`
	At    string `json:"at"`
	Kind  string `json:"kind"`
	What  string `json:"what"`
	Where string `json:"where"`
	By    string `json:"by"`
}

type servedSummary struct {
	Decisions          int `json:"decisions"`
	Yours              int `json:"yours"`
	Course             int `json:"course"`
	Corrections        int `json:"corrections"`
	Questions          int `json:"questions"`
	AnsweredByDirector int `json:"answered_by_director"`
	Chores             int `json:"chores"`
	Judged             int `json:"judged"`
}

func aProposedBreakdownIsServedAsProposed(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	goal, err := l.Add("p", "Release notes", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	var subs []string
	for _, title := range []string{"Every release is tagged", "Customers read what changed"} {
		w, err := l.Add("p", title, ledger.AddOptions{Parent: &goal.ID})
		if err != nil {
			t.Fatal(err)
		}
		subs = append(subs, w.ID)
	}
	if err := l.SetAfter(subs[1], []string{subs[0]}); err != nil {
		t.Fatal(err)
	}
	event(t, l, goal.ID, core.EventNote, core.ProposedPrefix+"2 sub-goals")

	api := httptest.NewServer(s.routes())
	defer api.Close()
	type view struct {
		Proposed bool       `json:"proposed"`
		Tree     []treeNode `json:"tree"`
	}
	var v view
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &v)
	if !v.Proposed || len(v.Tree) != 2 {
		t.Fatalf("goal = %+v, want proposed with two sub-goals", v)
	}
	for _, n := range v.Tree {
		if n.WhyNotMoving == nil || *n.WhyNotMoving != "proposed — needs approval" {
			t.Fatalf("%s why_not_moving = %v, want proposed — needs approval", n.Work.ID, n.WhyNotMoving)
		}
	}

	type entry struct {
		Work     core.Work `json:"work"`
		Proposed bool      `json:"proposed"`
		Band     Band      `json:"band"`
	}
	onBoard := func() entry {
		var b struct {
			Goals []entry `json:"goals"`
		}
		getJSON(t, api.URL+"/api/board", http.StatusOK, &b)
		for _, g := range b.Goals {
			if g.Work.ID == goal.ID {
				return g
			}
		}
		t.Fatalf("goal %s not on the board", goal.ID)
		return entry{}
	}
	if e := onBoard(); !e.Proposed || e.Band != BandNeedsYou {
		t.Fatalf("board entry = %+v, want proposed in needs-you", e)
	}

	event(t, l, goal.ID, core.EventDecision, core.ApprovedPrefix+"2 sub-goals")
	walk(t, l, subs[0], core.StateRunning)
	if e := onBoard(); e.Proposed || e.Band == BandNeedsYou {
		t.Fatalf("board entry after approval = %+v, want neither proposed nor needs-you", e)
	}
	v = view{}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &v)
	if v.Proposed {
		t.Fatal("proposed after approval, want false")
	}
	if n := v.Tree[0]; n.WhyNotMoving != nil {
		t.Fatalf("a started sub-goal's why_not_moving = %q, want none", *n.WhyNotMoving)
	}
	if n := v.Tree[1]; n.WhyNotMoving == nil || *n.WhyNotMoving != "waits on Every release is tagged" {
		t.Fatalf("a waiting sub-goal's why_not_moving = %v, want it waiting on its sibling", n.WhyNotMoving)
	}
}

func aChangeWaitingForThePersonIsServedWithWhatWouldChange(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	add := func(title string, o ledger.AddOptions) core.Work {
		t.Helper()
		w, err := l.Add("p", title, o)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	type served struct {
		NeedsYou []struct {
			Work   string          `json:"work"`
			Change json.RawMessage `json:"change"`
		} `json:"needs_you"`
	}
	changeOn := func(goal, work string) map[string]any {
		t.Helper()
		var v served
		getJSON(t, api.URL+"/api/goal/"+goal, http.StatusOK, &v)
		for _, n := range v.NeedsYou {
			if n.Work != work {
				continue
			}
			var ch map[string]any
			if err := json.Unmarshal(n.Change, &ch); err != nil {
				t.Fatalf("change on %s = %s: %v", work, n.Change, err)
			}
			return ch
		}
		t.Fatalf("no needs_you on %s in %+v", work, v.NeedsYou)
		return nil
	}
	same := func(what string, got any, want string) {
		t.Helper()
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != want {
			t.Errorf("%s = %s, want %s", what, raw, want)
		}
	}

	// A plan change in ask mode, beside a plain question under the goal.
	top := add("Release notes", ledger.AddOptions{Kind: core.WorkGoal})
	collector := add("Collector", ledger.AddOptions{Parent: &top.ID})
	old := add("Old route", ledger.AddOptions{Parent: &top.ID})
	note := add("Customer note", ledger.AddOptions{Parent: &top.ID})
	walk(t, l, note.ID, core.StateRunning, core.StateNeedsInput)
	event(t, l, note.ID, core.EventQuestion, "Which tone?\n- formal\n- casual")
	actions := []string{
		coordinator.Action{Kind: coordinator.ActGoal, Work: top.ID, Title: "Rollback guide", DoneWhen: "rollback is covered", After: []string{collector.ID}}.Line(),
		coordinator.Action{Kind: coordinator.ActSupersede, Work: old.ID, Text: "the guide covers it"}.Line(),
	}
	walk(t, l, top.ID, core.StateRunning, core.StateNeedsInput)
	ask := coordinator.Action{Kind: coordinator.ActAsk, Text: "Change the plan?\n" + strings.Join(actions, "\n"), Options: []string{"Apply these changes (recommended)", "Keep the plan as it is"}}
	if _, err := l.Ask(top.ID, coordinator.QuestionBody([]coordinator.Action{ask}), core.Change{Actions: actions}); err != nil {
		t.Fatal(err)
	}
	plan := changeOn(top.ID, top.ID)
	same("plan change kind", plan["kind"], `"plan"`)
	wantActions, err := json.Marshal(actions)
	if err != nil {
		t.Fatal(err)
	}
	same("plan change actions", plan["actions"], string(wantActions))
	same("plan change lines", plan["lines"], `["Add “Rollback guide” under Release notes, done when rollback is covered, after Collector","Drop Old route: the guide covers it"]`)
	if raw := func() string {
		var v served
		getJSON(t, api.URL+"/api/goal/"+top.ID, http.StatusOK, &v)
		for _, n := range v.NeedsYou {
			if n.Work == note.ID {
				return string(n.Change)
			}
		}
		return "missing"
	}(); raw != "null" {
		t.Errorf("a plain question's change = %s, want null", raw)
	}

	// A top-goal change, whatever the mode.
	course := add("Release notes for engineers", ledger.AddOptions{Kind: core.WorkGoal})
	if err := l.SetDoneWhen(course.ID, "engineers read what changed", core.AnsweredByDirector); err != nil {
		t.Fatal(err)
	}
	if err := l.SetFinish(course.ID, core.FinishPR, core.FinishFromTaste); err != nil {
		t.Fatal(err)
	}
	walk(t, l, course.ID, core.StateRunning, core.StateNeedsInput)
	text := "Change course?\nNow: Release notes for engineers | done when: engineers read what changed | finish: complete PR\nProposed: Release notes for everyone | done when: customers read it too | finish: merged\nWhy: customers ask for it"
	ask = coordinator.Action{Kind: coordinator.ActAsk, Text: text, Options: []string{"Change course (recommended)", "Keep the goal as it is"}}
	if _, err := l.Ask(course.ID, coordinator.QuestionBody([]coordinator.Action{ask}), core.Change{Title: "Release notes for everyone", DoneWhen: "customers read it too", Finish: core.FinishMerged}); err != nil {
		t.Fatal(err)
	}
	ch := changeOn(course.ID, course.ID)
	same("course change", ch, `{"kind":"course","now":{"done_when":"engineers read what changed","finish":"complete PR","title":"Release notes for engineers"},"proposed":{"done_when":"customers read it too","finish":"merged","title":"Release notes for everyone"},"why":"customers ask for it"}`)

	// A top-goal change asked together with a plan change carries both.
	both := add("Release notes for operators", ledger.AddOptions{Kind: core.WorkGoal})
	stale := add("Stale route", ledger.AddOptions{Parent: &both.ID})
	walk(t, l, both.ID, core.StateRunning, core.StateNeedsInput)
	dropStale := coordinator.Action{Kind: coordinator.ActSupersede, Work: stale.ID, Text: "nobody reads it"}.Line()
	asks := []coordinator.Action{
		{Kind: coordinator.ActAsk, Text: "Change course?\nNow: Release notes for operators | done when: not set | finish: not decided\nProposed: Runbooks for operators | done when: each release has a runbook | finish: merged\nWhy: operators read runbooks", Options: []string{"Change course (recommended)", "Keep the goal as it is"}},
		{Kind: coordinator.ActAsk, Text: "Change the plan?\n" + dropStale, Options: []string{"Apply these changes (recommended)", "Keep the plan as it is"}},
	}
	if _, err := l.Ask(both.ID, coordinator.QuestionBody(asks), core.Change{Actions: []string{dropStale}, Title: "Runbooks for operators", DoneWhen: "each release has a runbook", Finish: core.FinishMerged}); err != nil {
		t.Fatal(err)
	}
	ch = changeOn(both.ID, both.ID)
	same("combined change kind", ch["kind"], `"course"`)
	same("combined change why", ch["why"], `"operators read runbooks"`)
	same("combined change now", ch["now"], `{"done_when":null,"finish":null,"title":"Release notes for operators"}`)
	same("combined change lines", ch["lines"], `["Drop Stale route: nobody reads it"]`)

	// The finish-line question a planner unsure where a goal finishes leaves.
	finish := add("Find why the cache misses", ledger.AddOptions{Kind: core.WorkGoal})
	walk(t, l, finish.ID, core.StateRunning, core.StateNeedsInput)
	ask = coordinator.Action{Kind: coordinator.ActAsk, Text: coordinator.FinishQuestion, Options: []string{"research (recommended)", "complete PR"}}
	event(t, l, finish.ID, core.EventQuestion, coordinator.QuestionBody([]coordinator.Action{ask}))
	same("finish change", changeOn(finish.ID, finish.ID), `{"choices":["research (recommended)","complete PR"],"kind":"finish","sources":null}`)

	// With what each source said.
	said := add("Compare and pick one", ledger.AddOptions{Kind: core.WorkGoal})
	walk(t, l, said.ID, core.StateRunning, core.StateNeedsInput)
	goalSaid, tasteSaid := `"Compare and pick one" reads as research`, "In auto mode, a complete PR is done"
	if _, err := l.Ask(said.ID, coordinator.QuestionBody([]coordinator.Action{ask}), core.Change{Sources: &core.FinishSaid{Goal: &goalSaid, Taste: &tasteSaid}}); err != nil {
		t.Fatal(err)
	}
	same("finish sources", changeOn(said.ID, said.ID)["sources"], `{"goal":"\"Compare and pick one\" reads as research","repo":null,"taste":"In auto mode, a complete PR is done"}`)
}

func aGoalServesWhyItWasBrokenDownSo(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	goal, err := l.Add("p", "Release notes", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	reasoning := func() string {
		t.Helper()
		var v struct {
			Reasoning json.RawMessage `json:"reasoning"`
		}
		getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &v)
		return string(v.Reasoning)
	}
	if got := reasoning(); got != "null" {
		t.Errorf("reasoning before any planning = %s, want null", got)
	}
	why, left := "each outcome is one handover", "a release dashboard"
	event(t, l, goal.ID, core.EventNote, core.ProposedPrefix+"2 sub-goals")
	if _, err := l.Reason(goal.ID, core.Reasoning{Why: &why, LeftOut: &left}); err != nil {
		t.Fatal(err)
	}
	if got, want := reasoning(), `{"why":"each outcome is one handover","left_out":"a release dashboard","judge_by":null}`; got != want {
		t.Errorf("reasoning = %s, want %s", got, want)
	}
	event(t, l, goal.ID, core.EventNote, "plan: 2 sub-goals")
	if got := reasoning(); got != "null" {
		t.Errorf("reasoning after a planning that gave none = %s, want null", got)
	}
}

func aGoalServesItsRecord(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	r := newReleaseTree(t, l)
	event(t, l, r.top, core.EventDecision, "use the changelog as the source")
	if err := l.SetDoneWhen(r.note, "customers read what changed", core.AnsweredByPerson); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Decide(r.top, "added "+r.note+": Customer note", core.Decision{Question: "the sub-goals of " + r.top, Answer: "added " + r.note + ": Customer note", Source: core.AnsweredByDirector}, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.SetFinish(r.top, core.FinishPR, core.FinishFromPerson); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Decide(r.collector, "group by type", core.Decision{Question: "how to group?", Answer: "by type", Source: "judge"}, nil); err != nil {
		t.Fatal(err)
	}
	event(t, l, r.top, core.EventCourse, "correct: narrow it to rollback")
	if _, err := l.Answer(r.note, core.Decision{Question: "Which tone?", Answer: "formal", Source: core.AnsweredByPerson}); err != nil {
		t.Fatal(err)
	}
	event(t, l, r.rollback, core.EventQuestion, "Which environments?")
	if _, err := l.Answer(r.rollback, core.Decision{Question: "Which environments?", Answer: "staging and production", Source: core.AnsweredByDirector}); err != nil {
		t.Fatal(err)
	}
	event(t, l, r.collector, core.EventReport, "DONE\nSTATUS: DONE the collector groups changes")
	event(t, l, r.collector, core.EventJudged, "met: changes are grouped")
	event(t, l, r.top, core.EventNote, "chore: refresh the lockfile")
	event(t, l, r.top, core.EventNote, "not on the record")
	event(t, l, r.top, core.EventPr, "pull-request https://github.com/o/r/pull/9")

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var rec struct {
		Summary servedSummary `json:"summary"`
		Entries []servedEntry `json:"entries"`
	}
	getJSON(t, api.URL+"/api/record/"+r.top, http.StatusOK, &rec)
	want := servedSummary{Decisions: 6, Yours: 3, Course: 1, Corrections: 1, Questions: 2, AnsweredByDirector: 1, Chores: 1, Judged: 1}
	if rec.Summary != want {
		t.Fatalf("summary = %+v, want %+v", rec.Summary, want)
	}
	var kinds []string
	for _, e := range rec.Entries {
		kinds = append(kinds, e.Kind)
	}
	wantKinds := []string{"delivery", "chore", "judged", "report", "answer", "question", "answer", "course", "decision", "finish", "decision", "decision", "decision", "superseded", "question", "decision", "asked"}
	if !slices.Equal(kinds, wantKinds) {
		t.Fatalf("kinds newest first = %q, want %q", kinds, wantKinds)
	}
	byKind := func(kind, what string) servedEntry {
		t.Helper()
		for _, e := range rec.Entries {
			if e.Kind == kind && e.What == what {
				return e
			}
		}
		t.Fatalf("no %s entry %q in %+v", kind, what, rec.Entries)
		return servedEntry{}
	}
	for _, c := range []struct{ kind, what, where, by string }{
		{"asked", "Release notes", "Release notes (top)", "you"},
		{"decision", "use the changelog as the source", "Release notes (top)", "you"},
		{"finish", "finishes at: complete PR (from person)", "Release notes (top)", "you"},
		{"decision", "done when: a guide covers rollback", "Deployment guide", "director"},
		{"decision", "done when: customers read what changed", "Customer note", "you"},
		{"decision", "added " + r.note + ": Customer note", "Release notes (top)", "director"},
		{"decision", "group by type", "Deployment guide › Collector", "director"},
		{"course", "correct: narrow it to rollback", "Release notes (top)", "director"},
		{"answer", "formal", "Customer note", "you"},
		{"answer", "staging and production", "Deployment guide › Rollback", "director"},
		{"report", "done: the collector groups changes", "Deployment guide › Collector", "director"},
		{"judged", "met: changes are grouped", "Deployment guide › Collector", "director"},
		{"chore", "refresh the lockfile", "Release notes (top)", "director"},
		{"delivery", "https://github.com/o/r/pull/9", "Release notes (top)", "director"},
		{"superseded", "the guide covers it", "Old route", "director"},
	} {
		e := byKind(c.kind, c.what)
		if e.Where != c.where || e.By != c.by || e.At == "" {
			t.Fatalf("%s %q = %+v, want where %q by %s", c.kind, c.what, e, c.where, c.by)
		}
		if c.kind != "asked" && e.Event == 0 {
			t.Fatalf("%s %q carries no event", c.kind, c.what)
		}
	}

	c := startStdio(t, s)
	c.send(fmt.Sprintf(`{"id": 1, "method": "record", "params": {"id": %q}}`, r.top))
	var viaStdio struct {
		Summary servedSummary `json:"summary"`
	}
	if err := json.Unmarshal(c.reply(1)["result"], &viaStdio); err != nil || viaStdio.Summary != want {
		t.Fatalf("record over stdio = %+v, %v", viaStdio, err)
	}
	var missing map[string]string
	getJSON(t, api.URL+"/api/record/"+r.collector, http.StatusNotFound, &missing)
}
