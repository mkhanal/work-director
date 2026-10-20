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
	"regexp"
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
	return New(l, "", nil, nil, nil)
}

func TestServe(t *testing.T) {
	t.Run("The Server Binds To Loopback", serverBindsToLoopback)
	t.Run("JSON Endpoints Serve The Board", jsonEndpointsServeTheBoard)
	t.Run("A Goal Serves What The Loop Decided And Where It Reached", aGoalServesWhatTheLoopDecidedAndWhereItReached)
	t.Run("A Goal Serves Each Report In Sections", aGoalServesEachReportInSections)
	t.Run("A Goal Waiting On The Person Serves Its Question", aGoalWaitingOnThePersonServesItsQuestion)
	t.Run("A Goal's Delivery Is Read In The Goal's Own Directory", aGoalsDeliveryIsReadInTheGoalsOwnDirectory)
	t.Run("A Goal Read Again Does Not Wait On Git", aGoalReadAgainDoesNotWaitOnGit)
	t.Run("The Board Places Every Item In One Band", theBoardPlacesEveryItemInOneBand)
	t.Run("Archived Work Is Not On The Board", archivedWorkIsNotOnTheBoard)
	t.Run("A Client Can Hold The Server On Its Standard Streams", aClientCanHoldTheServerOnItsStandardStreams)
	t.Run("A Work Item Serves Its Session's Conversation", aWorkItemServesItsSessionsConversation)
	t.Run("A Goal Serves What Each Of Its Tasks Is Doing", aGoalServesWhatEachOfItsTasksIsDoing)
	t.Run("A Goal Serves Its Sub-Goals As A Tree", aGoalServesItsSubGoalsAsATree)
	t.Run("A Sub-Goal Whose Work Is All Finished Reads As To Review", aSubGoalWhoseWorkIsAllFinishedReadsAsToReview)
	t.Run("A Task Titled By Its Whole Brief Reads By Its First Sentence", aTaskTitledByItsWholeBriefReadsByItsFirstSentence)
	t.Run("A Goal Serves What Done Means And Why It Is Not Done Yet", aGoalServesWhatDoneMeansAndWhyItIsNotDoneYet)
	t.Run("A Goal Serves Everything Below It That Needs The Person", aGoalServesEverythingBelowItThatNeedsThePerson)
	t.Run("A Goal Serves Its Course", aGoalServesItsCourse)
	t.Run("A Proposed Breakdown Is Served As Proposed", aProposedBreakdownIsServedAsProposed)
	t.Run("A Goal Serves Why It Was Broken Down So", aGoalServesWhyItWasBrokenDownSo)
	t.Run("A Change Waiting For The Person Is Served With What Would Change", aChangeWaitingForThePersonIsServedWithWhatWouldChange)
	t.Run("A Course Change Serves What Was Found And What Happens To Each Sub-Goal", aCourseChangeServesWhatWasFoundAndWhatHappensToEachSubGoal)
	t.Run("A Goal Serves Its Record", aGoalServesItsRecord)
	t.Run("Sentences A Goal Serves Are Free Of Internal Terms", sentencesAGoalServesAreFreeOfInternalTerms)
	t.Run("A Goal Serves Why Work Stopped Or Is Blocked", aGoalServesWhyWorkStoppedOrIsBlocked)
	t.Run("A Goal Serves That The Project's Checks Fail On Main, With A Sub-Goal To Fix Them", aGoalServesThatTheProjectsChecksFailOnMainWithASubGoalToFixThem)
	t.Run("A Running Session's Question Puts Its Work In Needs You", aRunningSessionsQuestionPutsItsWorkInNeedsYou)
	t.Run("A Running Session's Report Moves Its Work On", aRunningSessionsReportMovesItsWorkOn)
	t.Run("Work Reads As Working Only While An Agent Is Working", workReadsAsWorkingOnlyWhileAnAgentIsWorking)
	t.Run("A Reply To The Person Reads As A Reply, Not A Stop", aReplyToThePersonReadsAsAReplyNotAStop)
	t.Run("A Message To A Task Reaches It Whatever State Its Goal Is In", aMessageToATaskReachesItWhateverStateItsGoalIsIn)
	t.Run("A Running Goal Is Supervised While Serving", aRunningGoalIsSupervisedWhileServing)
	t.Run("Serving Releases Finished Work's Worktrees At Start And Hourly", servingReleasesFinishedWorksWorktreesAtStartAndHourly)
	t.Run("Work Items Serve Over HTTP", workItemsServeOverHTTP)
	t.Run("A Work Item Serves The Next Gate It Waits On", aWorkItemServesTheNextGateItWaitsOn)
	t.Run("Actions Dispatch To The CLI", actionsDispatchToTheCLI)
	t.Run("WebSocket Serves Live Events", webSocketServesLiveEvents)
	t.Run("A Client That Stops Reading Never Delays The Others", aClientThatStopsReadingNeverDelaysTheOthers)
	t.Run("Empty Collections Serialize As Empty Arrays", emptyCollectionsSerializeAsEmptyArrays)
	t.Run("Errors Return JSON", errorsReturnJSON)
	t.Run("A Goal Serves The Files Attached Under It", aGoalServesTheFilesAttachedUnderIt)
	t.Run("A Goal Serves What The Person Said That Still Waits For The Director", aGoalServesWhatThePersonSaidThatStillWaitsForTheDirector)
}

func aGoalServesThatTheProjectsChecksFailOnMainWithASubGoalToFixThem(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	top, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := l.Add("p", "Sub", ledger.AddOptions{Kind: core.WorkGoal, Parent: &top.ID})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	type fix struct {
		Outcome  string `json:"outcome"`
		DoneWhen string `json:"done_when"`
	}
	type checks struct {
		Concern int    `json:"concern"`
		Says    string `json:"says"`
		Fix     *fix   `json:"fix"`
	}
	served := func(id string) *checks {
		t.Helper()
		var view struct {
			Checks *checks `json:"checks_fail_on_main"`
		}
		getJSON(t, api.URL+"/api/goal/"+id, http.StatusOK, &view)
		return view.Checks
	}
	if got := served(top.ID); got != nil {
		t.Fatalf("checks_fail_on_main = %+v, want null with no concern", got)
	}
	if _, err := l.AddConcern(top.ID, "the planner wants a second look"); err != nil {
		t.Fatal(err)
	}
	says := core.ChecksConcern([]string{"lint", "validate"})
	cx, err := l.AddConcern(top.ID, says)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{top.ID, sub.ID} {
		got := served(id)
		if got == nil || got.Concern != cx.ID || got.Says != says || got.Fix == nil || got.Fix.Outcome != core.FixChecks || got.Fix.DoneWhen != core.FixChecksDoneWhen {
			t.Fatalf("checks_fail_on_main on %s = %+v, want the concern with its fix", id, got)
		}
	}
	fixing, err := l.Add("p", core.FixChecks, ledger.AddOptions{Parent: &top.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := served(top.ID); got == nil || got.Fix != nil {
		t.Fatalf("checks_fail_on_main while %s is open = %+v, want no fix offered", fixing.ID, got)
	}
	if _, err := l.ResolveConcern(cx.ID, "the checks pass again"); err != nil {
		t.Fatal(err)
	}
	if got := served(top.ID); got != nil {
		t.Fatalf("checks_fail_on_main once resolved = %+v, want null", got)
	}
}

func aGoalServesWhatThePersonSaidThatStillWaitsForTheDirector(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	goal, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	waiting := func() string {
		var view map[string]json.RawMessage
		getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &view)
		return string(view["waiting_for_director"])
	}
	if got := waiting(); got != "[]" {
		t.Fatalf("waiting_for_director = %s, want [] before the person says anything", got)
	}
	said, err := l.AddEvent(goal.ID, core.EventDecision, "cover the API too")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.QueueForSupervisor(goal.ID, said.ID, said.Body); err != nil {
		t.Fatal(err)
	}
	if got, want := waiting(), fmt.Sprintf("[%d]", said.ID); got != want {
		t.Fatalf("waiting_for_director = %s, want %s while the director has not been given it", got, want)
	}
	look, taken, ok, err := l.ClaimLook(goal.ID, 0, "the person wrote (1)")
	if err != nil || !ok || len(taken) != 1 {
		t.Fatalf("claim look = %v, %v, %v", taken, ok, err)
	}
	if got := waiting(); got != "[]" {
		t.Fatalf("waiting_for_director = %s, want [] once a look took it", got)
	}
	if err := l.ReleaseLook(look); err != nil {
		t.Fatal(err)
	}
	if got, want := waiting(), fmt.Sprintf("[%d]", said.ID); got != want {
		t.Fatalf("waiting_for_director = %s, want %s again once its look was taken back", got, want)
	}
}

func aGoalServesTheFilesAttachedUnderIt(t *testing.T) {
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
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var bare map[string]json.RawMessage
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &bare)
	if string(bare["attachments"]) != "[]" {
		t.Fatalf("attachments = %s, want [] before any file", bare["attachments"])
	}
	mock := core.Attachment{Name: "mock.png", Path: "/wd/attachments/" + goal.ID + "/1-mock.png", Size: 12, MediaType: "image/png"}
	notes := core.Attachment{Name: "notes.txt", Path: "/wd/attachments/" + task.ID + "/1-notes.txt", Size: 21, MediaType: "text/plain"}
	decided, err := l.AddEvent(goal.ID, core.EventDecision, "match this", mock)
	if err != nil {
		t.Fatal(err)
	}
	answered, err := l.Answer(task.ID, core.Decision{Question: "Which margins?", Answer: "these", Source: core.AnsweredByPerson}, notes)
	if err != nil {
		t.Fatal(err)
	}
	type served struct {
		Event     int    `json:"event"`
		Work      string `json:"work"`
		At        string `json:"at"`
		Name      string `json:"name"`
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		MediaType string `json:"media_type"`
	}
	var view struct {
		Attachments []served `json:"attachments"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &view)
	if len(view.Attachments) != 2 {
		t.Fatalf("attachments = %+v, want the goal's and its task's", view.Attachments)
	}
	for i, w := range []struct {
		with core.Event
		att  core.Attachment
	}{{decided, mock}, {answered, notes}} {
		got := view.Attachments[i]
		if got.Event == 0 || got.Work != w.with.Work || got.At != w.with.At || got.Name != w.att.Name || got.Path != w.att.Path || got.Size != w.att.Size || got.MediaType != w.att.MediaType {
			t.Fatalf("attachment %d = %+v, want %+v on the work and moment of %+v", i, got, w.att, w.with)
		}
	}
	resp, err := http.Get(api.URL + "/api/attachment/" + strconv.Itoa(view.Attachments[0].Event))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a file's bytes were served over HTTP: %d", resp.StatusCode)
	}
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
	addr, _, err := New(l, "/bin/echo", nil, nil, nil).Start(0)
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
	srv := New(l, "/bin/echo", nil, nil, nil)
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

func aGoalReadAgainDoesNotWaitOnGit(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	repo := t.TempDir()
	inRepo := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	inRepo("init", "-q", "-b", "main")
	inRepo("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	inRepo("switch", "-q", "-c", "first")
	goal, err := l.Add("p", "Goal", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	task, err := l.Add("p", "Task", ledger.AddOptions{Parent: &goal.ID})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := l.SetCwd(task.ID, repo); err != nil {
		t.Fatalf("cwd: %v", err)
	}
	branch := func() string {
		t.Helper()
		v, err := s.goal(goal.ID)
		if err != nil {
			t.Fatalf("goal: %v", err)
		}
		if v.Delivery == nil {
			t.Fatalf("goal has no delivery")
		}
		return v.Delivery.Branch
	}
	if got := branch(); got != "first" {
		t.Fatalf("first read branch = %q, want first", got)
	}

	inRepo("switch", "-q", "-c", "second")
	if got := branch(); got != "first" {
		t.Fatalf("read again branch = %q, want the delivery already read, on first", got)
	}

	s.delivered.mu.Lock()
	s.delivered.m[goal.ID].at = time.Now().Add(-3 * time.Second)
	s.delivered.mu.Unlock()
	if got := branch(); got != "first" {
		t.Fatalf("stale read branch = %q, want the one held while it is read again", got)
	}
	for deadline := time.Now().Add(5 * time.Second); branch() != "second"; {
		if time.Now().After(deadline) {
			t.Fatalf("delivery never read again in the background")
		}
		time.Sleep(20 * time.Millisecond)
	}

	inRepo("switch", "-q", "-c", "third")
	if _, err := l.AddEvent(goal.ID, core.EventPr, "commit 0000000"); err != nil {
		t.Fatalf("landing: %v", err)
	}
	if got := branch(); got != "third" {
		t.Fatalf("after a landing branch = %q, want it read anew on third", got)
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
	if qs, err := s.ledger.Events(quiet.ID, kindOf(core.EventQuestion)); err != nil || len(qs) != 0 {
		t.Fatalf("work asking nothing had questions filed: %+v (%v)", qs, err)
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
	labels := map[string]string{"": "", "verify": "Check it", "land": "Deliver it", "soft-done": "Confirm it's finished", "done": "Mark it done"}
	gate := func(want string, argv ...string) {
		t.Helper()
		var g gateView
		getJSON(t, ts.URL+"/api/work/"+w.ID+"/gate", http.StatusOK, &g)
		if g.Gate != want || !slices.Equal(g.Argv, argv) || (want != "" && g.Why == "") || g.Label != labels[want] {
			t.Fatalf("gate = %+v, want %q %v labelled %q with a reason", g, want, argv, labels[want])
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

func aSubGoalWhoseWorkIsAllFinishedReadsAsToReview(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	g, err := l.Add("p", "Fix today's issues", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	task := func(parent, heading string, states ...core.State) string {
		t.Helper()
		o := ledger.AddOptions{Parent: &parent}
		if heading != "" {
			o.Heading = &heading
		}
		w, err := l.Add("p", "Task", o)
		if err != nil {
			t.Fatal(err)
		}
		walk(t, l, w.ID, states...)
		return w.ID
	}
	review := []core.State{core.StateRunning, core.StateReview}
	task(g.ID, "Visibility (#490)", review...)
	task(g.ID, "Visibility (#490)", review...)
	task(g.ID, "Time format (#501)", review...)
	stopped := task(g.ID, "Time format (#501)", core.StateRunning, core.StateNeedsInput)
	event(t, l, stopped, core.EventNote, "stopped without a report: Shall I run all three?")
	task(g.ID, "Printing (#493)", core.StateRunning)
	task(g.ID, "Printing (#493)", review...)
	confirming := task(g.ID, "Signatures (#492)", review...)
	event(t, l, confirming, core.EventReport, "DONE\nSTATUS: DONE")
	event(t, l, confirming, core.EventVerify, "pass\ngo test → 0")
	if _, err := l.SoftDone(confirming, false); err != nil {
		t.Fatal(err)
	}
	task(g.ID, "Signatures (#492)", core.StateDone)
	sub, err := l.Add("p", "Stamps are never rewritten", ledger.AddOptions{Kind: core.WorkGoal, Parent: &g.ID})
	if err != nil {
		t.Fatal(err)
	}
	task(sub.ID, "", review...)
	task(sub.ID, "", core.StateDone)

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var view struct {
		Tree []struct {
			Work     core.Work `json:"work"`
			Group    bool      `json:"group"`
			NeedsYou int       `json:"needs_you"`
		} `json:"tree"`
	}
	getJSON(t, api.URL+"/api/goal/"+g.ID, http.StatusOK, &view)
	want := map[string]core.State{
		"Visibility (#490)": core.StateReview, "Time format (#501)": core.StateNeedsInput, "Printing (#493)": core.StateRunning,
		"Signatures (#492)": core.StateSoftDone, "Stamps are never rewritten": core.StateReview,
	}
	if len(view.Tree) != len(want) {
		t.Fatalf("tree = %+v, want %d nodes", view.Tree, len(want))
	}
	for _, n := range view.Tree {
		if n.Work.State != want[n.Work.Title] {
			t.Errorf("%s reads %s, want %s", n.Work.Title, n.Work.State, want[n.Work.Title])
		}
		wantYou := 0
		if n.Work.Title == "Time format (#501)" {
			wantYou = 1
		}
		if n.NeedsYou != wantYou {
			t.Errorf("%s needs_you = %d, want %d", n.Work.Title, n.NeedsYou, wantYou)
		}
	}
}

func aTaskTitledByItsWholeBriefReadsByItsFirstSentence(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	g, err := l.Add("p", "Release notes", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	brief := "Check the invoice export on the main branch: does a PDF with accented names print them whole? Add a test for it."
	short := "Check the invoice export on the main branch: does a PDF with accented names…"
	// A row written before titles were kept short.
	old, err := l.Add("p", "placeholder", ledger.AddOptions{Parent: &g.ID, Detail: "Work off main."})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetTitle(old.ID, brief); err != nil {
		t.Fatal(err)
	}
	after, err := l.Add("p", "Publish the guide", ledger.AddOptions{Parent: &g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetAfter(after.ID, []string{old.ID}); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var view struct {
		Tree []struct {
			Work      core.Work `json:"work"`
			WaitingOn []string  `json:"waiting_on"`
		} `json:"tree"`
		Paths map[string]string `json:"paths"`
	}
	getJSON(t, api.URL+"/api/goal/"+g.ID, http.StatusOK, &view)
	if len(view.Tree) != 2 {
		t.Fatalf("tree = %+v, want two nodes", view.Tree)
	}
	n := view.Tree[0].Work
	if n.Title != short || n.Detail != brief+"\n\nWork off main." {
		t.Fatalf("node title %q, detail %q; want its first sentence and the whole brief ahead of its detail", n.Title, n.Detail)
	}
	if view.Paths[old.ID] != short || !slices.Equal(view.Tree[1].WaitingOn, []string{short}) {
		t.Fatalf("paths = %v, waiting_on = %v; want the short title", view.Paths, view.Tree[1].WaitingOn)
	}
}

func workReadsAsWorkingOnlyWhileAnAgentIsWorking(t *testing.T) {
	s := newTestServer(t)
	said := "Wiring it in belongs to the collector task.\nShall I wire it in too?"
	r := &sessionRunner{steps: map[string][]runner.Entry{
		"quiet": {{Kind: runner.EntryText, Text: "working"}, {Kind: runner.EntryText, Text: said}},
		"noted": {{Kind: runner.EntryText, Text: "Ready to land. Waiting for your go."}},
	}}
	withSessions(s, r)
	goal, err := s.ledger.Add("p", "Collect metrics", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	quiet := inSession(t, s, "Quiet", "quiet", &goal.ID)
	// Noted by an earlier pass that left the work running.
	noted := inSession(t, s, "Noted", "noted", &goal.ID)
	if _, err := s.ledger.File(noted.ID, core.EventNote, "stopped without a report: Ready to land. Waiting for your go.", core.TranscriptMark{Session: "noted", Entries: 1}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.watchSessions(stop) }()
	deadline := time.Now().Add(10 * time.Second)
	for _, id := range []string{quiet.ID, noted.ID} {
		for {
			w, err := s.ledger.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if w.State == core.StateNeedsInput {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s is %s, want needs-input once its session stopped without a report", w.Title, w.State)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	time.Sleep(sessionWatch + time.Second)
	close(stop)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{quiet.ID: said, noted.ID: "Ready to land. Waiting for your go."} {
		notes, err := s.ledger.Events(id, kindOf(core.EventNote))
		if err != nil {
			t.Fatal(err)
		}
		if len(notes) != 1 || notes[0].Body != "stopped without a report: "+want {
			t.Fatalf("notes = %+v, want one note with what it last said", notes)
		}
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var view struct {
		NeedsYou []needYou `json:"needs_you"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &view)
	asked := map[string]runner.QuestionItem{}
	for _, n := range view.NeedsYou {
		if len(n.Question.Items) == 1 {
			asked[n.Work] = n.Question.Items[0]
		}
	}
	if len(view.NeedsYou) != 2 || len(asked) != 2 {
		t.Fatalf("needs_you = %+v, want both stopped tasks with one question each", view.NeedsYou)
	}
	if q := asked[quiet.ID]; q.Question != said || len(q.Options) != 2 || q.Options[0].Label != "Yes" || q.Options[1].Label != "No" {
		t.Fatalf("quiet asks %+v, want what it last said offered Yes and No", q)
	}
	if q := asked[noted.ID]; q.Question != "Ready to land. Waiting for your go." || len(q.Options) != 0 {
		t.Fatalf("noted asks %+v, want what it last said with no choices", q)
	}
}

func aReplyToThePersonReadsAsAReplyNotAStop(t *testing.T) {
	s := newTestServer(t)
	said := map[string]string{
		"review": "Nothing changed here; the commit to review is on the goal.",
		"held":   "Understood, I've stopped work on the flaky test for this session.",
		"asks":   "Dropped it.\nShall I drop the other branch too?",
		"bossed": "OK.",
	}
	steps := map[string][]runner.Entry{}
	for session, text := range said {
		steps[session] = []runner.Entry{{Kind: runner.EntryText, Text: "working"}, {Kind: runner.EntryText, Text: text}}
	}
	withSessions(s, &sessionRunner{steps: steps})
	goal, err := s.ledger.Add("p", "Ship the release", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	tell := func(w core.Work, text string, by core.Sender) {
		t.Helper()
		m, err := s.ledger.QueueMessage(w.ID, text, by)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ledger.MarkDelivered(m.ID); err != nil {
			t.Fatal(err)
		}
		if w.State != core.StateRunning {
			if _, err := s.ledger.Transition(w.ID, core.StateRunning); err != nil {
				t.Fatal(err)
			}
		}
	}
	review := inSession(t, s, "Tag the release", "review", &goal.ID)
	if review, err = s.ledger.Transition(review.ID, core.StateReview); err != nil {
		t.Fatal(err)
	}
	tell(review, "what do you want me to review", core.SentByPerson)
	held := inSession(t, s, "Fix the flaky test", "held", &goal.ID)
	tell(held, "ignore this bug for this session", core.SentByPerson)
	asks := inSession(t, s, "Prune branches", "asks", &goal.ID)
	tell(asks, "drop the stale branch", core.SentByPerson)
	bossed := inSession(t, s, "Write the changelog", "bossed", &goal.ID)
	tell(bossed, "Your sub-goal's done-when is not met yet: dates", core.SentByDirector)

	want := map[string]core.State{review.ID: core.StateReview, held.ID: core.StatePaused, asks.ID: core.StateNeedsInput, bossed.ID: core.StateNeedsInput}
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.watchSessions(stop) }()
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
	time.Sleep(sessionWatch + time.Second)
	close(stop)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	for id, note := range map[string]string{
		review.ID: "replied: " + said["review"],
		held.ID:   "replied: " + said["held"],
		asks.ID:   "replied: " + said["asks"],
		bossed.ID: "stopped without a report: " + said["bossed"],
	} {
		notes, err := s.ledger.Events(id, kindOf(core.EventNote))
		if err != nil {
			t.Fatal(err)
		}
		if len(notes) != 1 || notes[0].Body != note {
			t.Fatalf("notes = %+v, want the one note %q", notes, note)
		}
		if w, _ := s.ledger.Get(id); w.State != want[id] {
			t.Fatalf("%s is %s, want it still %s", w.Title, w.State, want[id])
		}
	}

	api := httptest.NewServer(s.routes())
	defer api.Close()
	type stop2 struct {
		Said string `json:"said"`
		At   string `json:"at"`
	}
	var view struct {
		Tree []struct {
			Work    core.Work `json:"work"`
			Stopped *stop2    `json:"stopped"`
			Replied *stop2    `json:"replied"`
		} `json:"tree"`
		NeedsYou []needYou `json:"needs_you"`
	}
	getJSON(t, api.URL+"/api/goal/"+goal.ID, http.StatusOK, &view)
	for _, n := range view.Tree {
		switch n.Work.ID {
		case review.ID, held.ID, asks.ID:
			if n.Stopped != nil || n.Replied == nil || n.Work.Session == nil || n.Replied.Said != said[*n.Work.Session] {
				t.Errorf("%s serves stopped %+v, replied %+v; want its reply and no stop", n.Work.Title, n.Stopped, n.Replied)
			}
		case bossed.ID:
			if n.Replied != nil || n.Stopped == nil {
				t.Errorf("%s serves stopped %+v, replied %+v; want a stop", n.Work.Title, n.Stopped, n.Replied)
			}
		}
	}
	asked := map[string]runner.QuestionItem{}
	for _, n := range view.NeedsYou {
		if len(n.Question.Items) == 1 {
			asked[n.Work] = n.Question.Items[0]
		}
	}
	if q, ok := asked[asks.ID]; !ok || q.Question != said["asks"] || len(q.Options) != 2 {
		t.Fatalf("needs_you = %+v, want the reply's question offered Yes and No", view.NeedsYou)
	}
	if _, ok := asked[held.ID]; ok {
		t.Fatalf("needs_you = %+v, want no ask for a reply that asked nothing", view.NeedsYou)
	}
}

func aMessageToATaskReachesItWhateverStateItsGoalIsIn(t *testing.T) {
	s := newTestServer(t)
	r := &sessionRunner{steps: map[string][]runner.Entry{"tagger": {{Kind: runner.EntryText, Text: "Tagged v2."}}}}
	withSessions(s, r)
	goal, err := s.ledger.Add("p", "Release v2", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	tagger := inSession(t, s, "Tag the release", "tagger", &goal.ID)
	paused := inSession(t, s, "Write the changelog", "changelog", &goal.ID)
	for id, st := range map[string]core.State{goal.ID: core.StateRunning, tagger.ID: core.StateReview, paused.ID: core.StatePaused} {
		if _, err := s.ledger.Transition(id, st); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ledger.Transition(goal.ID, core.StateReview); err != nil {
		t.Fatal(err)
	}
	// Queued a moment after the session finished its turn, while it still read as working.
	for _, id := range []string{tagger.ID, paused.ID} {
		if _, err := s.ledger.QueueMessage(id, "Also tag v2.1", core.SentByPerson); err != nil {
			t.Fatal(err)
		}
	}
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.watchSessions(stop) }()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		waiting, err := s.ledger.Undelivered(tagger.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(waiting) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the message to a task under a goal in review was never delivered")
		}
	}
	close(stop)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.sent, []string{"Also tag v2.1"}) {
		t.Fatalf("sent = %q, want the one message to the idle task, none to paused work", r.sent)
	}
	if w, _ := s.ledger.Get(tagger.ID); w.State != core.StateRunning {
		t.Fatalf("a task in review given a message is %s, want running", w.State)
	}
	if waiting, _ := s.ledger.Undelivered(paused.ID); len(waiting) != 1 {
		t.Fatalf("paused work's message = %+v, want it still waiting", waiting)
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
	want := []string{"5 sub-goals are still open", "It hasn't been checked against what done means yet", "Not finished yet: the work isn't reported done, its checks haven't passed, and there's no pull request"}
	if !slices.Equal(top.NotDoneBecause, want) {
		t.Fatalf("top not done because %q, want %q", top.NotDoneBecause, want)
	}

	var guide doneView
	getJSON(t, api.URL+"/api/goal/"+r.guide, http.StatusOK, &guide)
	if guide.DoneWhen == nil || *guide.DoneWhen != "a guide covers rollback" || guide.Finish == nil || guide.Finish.Line != core.FinishPR {
		t.Fatalf("nested done-when %v, finish %+v; want its own done-when and its top goal's finish", guide.DoneWhen, guide.Finish)
	}
	if want := []string{"2 sub-goals are still open", "It hasn't been checked against what done means yet"}; !slices.Equal(guide.NotDoneBecause, want) {
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
		if n.WhyNotMoving == nil || *n.WhyNotMoving != "Proposed, waiting for your approval" {
			t.Fatalf("%s why_not_moving = %v, want Proposed, waiting for your approval", n.Work.ID, n.WhyNotMoving)
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
	same("course change", ch, `{"found":null,"kind":"course","now":{"done_when":"engineers read what changed","finish":"complete PR","title":"Release notes for engineers"},"proposed":{"done_when":"customers read it too","finish":"merged","title":"Release notes for everyone"},"sub_goals":[],"why":"customers ask for it"}`)

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

func aCourseChangeServesWhatWasFoundAndWhatHappensToEachSubGoal(t *testing.T) {
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
	top := add("Events reach the warehouse", ledger.AddOptions{Kind: core.WorkGoal})
	done := add("Events are batched", ledger.AddOptions{Parent: &top.ID})
	walk(t, l, done.ID, core.StateDone)
	working := add("Batches are retried", ledger.AddOptions{Parent: &top.ID})
	walk(t, l, working.ID, core.StateRunning)
	gone := add("An old route", ledger.AddOptions{Parent: &top.ID})
	if _, err := l.Supersede(gone.ID, "replaced earlier"); err != nil {
		t.Fatal(err)
	}
	dropped := add("Retries are tuned by hand", ledger.AddOptions{Parent: &top.ID})
	actions := []string{
		coordinator.Action{Kind: coordinator.ActSupersede, Work: dropped.ID, Text: "the SDK fix makes it moot"}.Line(),
		coordinator.Action{Kind: coordinator.ActGoal, Work: top.ID, Title: "The SDK retries a throttled batch", DoneWhen: "no batch is dropped on 429"}.Line(),
	}
	found := "The vendor SDK drops a batch when the API answers 429 and never retries it."
	walk(t, l, top.ID, core.StateRunning, core.StateNeedsInput)
	asks := []coordinator.Action{
		{Kind: coordinator.ActAsk, Text: "Change course?\nNow: Events reach the warehouse | done when: not set | finish: not decided\nProposed: No event is lost | done when: every event lands | finish: complete PR\nFound: " + found + "\nWhy: losing events is the real problem", Options: []string{"Change course (recommended)", "Keep the goal as it is"}},
		{Kind: coordinator.ActAsk, Text: "Change the plan?\n" + strings.Join(actions, "\n"), Options: []string{"Apply these changes (recommended)", "Keep the plan as it is"}},
	}
	if _, err := l.Ask(top.ID, coordinator.QuestionBody(asks), core.Change{Actions: actions, Title: "No event is lost", DoneWhen: "every event lands", Finish: core.FinishPR, Found: found}); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(s.routes())
	defer api.Close()
	var v struct {
		NeedsYou []struct {
			Change struct {
				Kind     string          `json:"kind"`
				Found    *string         `json:"found"`
				SubGoals json.RawMessage `json:"sub_goals"`
			} `json:"change"`
		} `json:"needs_you"`
	}
	getJSON(t, api.URL+"/api/goal/"+top.ID, http.StatusOK, &v)
	if len(v.NeedsYou) != 1 {
		t.Fatalf("needs_you = %+v, want the one change", v.NeedsYou)
	}
	ch := v.NeedsYou[0].Change
	if ch.Kind != "course" || ch.Found == nil || *ch.Found != found {
		t.Fatalf("change = %s found %v, want a course change with what was found", ch.Kind, ch.Found)
	}
	want := fmt.Sprintf(`[{"work":%q,"title":"Events are batched","status":"kept","state":"done","why":null},`+
		`{"work":%q,"title":"Batches are retried","status":"kept","state":"running","why":null},`+
		`{"work":%q,"title":"Retries are tuned by hand","status":"superseded","state":"queued","why":"the SDK fix makes it moot"},`+
		`{"work":null,"title":"The SDK retries a throttled batch","status":"new","state":null,"why":null}]`, done.ID, working.ID, dropped.ID)
	if string(ch.SubGoals) != want {
		t.Errorf("sub_goals = %s\nwant %s", ch.SubGoals, want)
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
		{"finish", "Finishes at a complete pull request, as you chose", "Release notes (top)", "you"},
		{"decision", "done when: a guide covers rollback", "Deployment guide", "director"},
		{"decision", "done when: customers read what changed", "Customer note", "you"},
		{"decision", "Added the sub-goal “Customer note”", "Release notes (top)", "director"},
		{"decision", "group by type", "Deployment guide › Collector", "director"},
		{"course", "correct: narrow it to rollback", "Release notes (top)", "director"},
		{"answer", "formal", "Customer note", "you"},
		{"answer", "staging and production", "Deployment guide › Rollback", "director"},
		{"report", "Reported finished: the collector groups changes", "Deployment guide › Collector", "director"},
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

// internalTerms are words of wd's own, and of its agents' protocol, that no
// sentence written for the person may carry.
var internalTerms = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bDONE report\b`),
	regexp.MustCompile(`(?i)\bverif(y|ied|ies)\b`),
	regexp.MustCompile(`(?i)\bsoft-done\b`),
	regexp.MustCompile(`(?i)\bsupersed(e|es|ing)\b|\b(is|was|were|be|been|are|got) superseded\b|\bsuperseded (by|it|them)\b`),
	regexp.MustCompile(`(?i)\bclaim(s|ed)?\b`),
	regexp.MustCompile(`(?i)\bjudged\b`),
	regexp.MustCompile(`\bACTIONS\b|\bSTEPS\b`),
	regexp.MustCompile(`(?i)\bpayload\b`),
	regexp.MustCompile(`(?i)\bworktree\b`),
	regexp.MustCompile(`\w\(s\)`),
	regexp.MustCompile(`(?i)\bdone-when\b`),
	regexp.MustCompile(`(?i)\bses_[A-Za-z0-9]+|\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-`),
	regexp.MustCompile(`\b[0-9a-f]{8}\b`),
}

func sentencesAGoalServesAreFreeOfInternalTerms(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	add := func(title string, o ledger.AddOptions) string {
		t.Helper()
		w, err := l.Add("metariki-eqms", title, o)
		if err != nil {
			t.Fatal(err)
		}
		return w.ID
	}
	sub := func(parent, title, doneWhen string, kind core.WorkKind) string {
		t.Helper()
		id := add(title, ledger.AddOptions{Kind: kind, Parent: &parent})
		if err := l.SetDoneWhen(id, doneWhen, core.AnsweredByDirector); err != nil {
			t.Fatal(err)
		}
		return id
	}
	reshape := func(on, body, by string) {
		t.Helper()
		if _, err := l.Decide(on, body, core.Decision{Question: "the sub-goals of " + on, Answer: body, Source: by}, nil); err != nil {
			t.Fatal(err)
		}
	}

	// The goal on the main screen, as scripts/ui/seed-goal-tree.sh seeds it.
	g := add("Release notes for every release", ledger.AddOptions{Kind: core.WorkGoal})
	if err := l.SetDoneWhen(g, "For every release, whoever deploys gets a guide and customers get a plain-language note", core.AnsweredByDirector); err != nil {
		t.Fatal(err)
	}
	if err := l.SetFinish(g, core.FinishPR, core.FinishFromTaste); err != nil {
		t.Fatal(err)
	}
	s1 := sub(g, "Every release is tagged and records what changed since the last one", "each release has a version tag and a change list grouped by type", core.WorkTask)
	s2 := sub(g, "Whoever deploys gets a guide for each release", "a release's guide lists its migrations, flags and how to roll it back", core.WorkGoal)
	s3 := sub(g, "Customers get a plain-language release note", "a customer note in plain language is produced for each release", core.WorkTask)
	s4 := sub(g, "Any agent can produce the notes from the documented workflow", "a fresh agent following the workflow produces both notes", core.WorkTask)
	if err := l.SetAfter(s4, []string{s2, s3}); err != nil {
		t.Fatal(err)
	}
	s2a := sub(s2, "The guide lists migrations and env changes", "every migration since the last tag is listed", core.WorkTask)
	s2b := sub(s2, "The guide lists feature flags", "every flag change is listed", core.WorkTask)
	s2c := sub(s2, "The guide says how to roll back", "the guide has rollback steps", core.WorkTask)
	s2d := sub(s2, "Guides are published to the team wiki", "the guide is on the wiki", core.WorkTask)
	if _, err := l.Supersede(s2d, "guides live with each release in the docs"); err != nil {
		t.Fatal(err)
	}
	s2e := sub(s2, "The guide covers staging", "staging is covered", core.WorkTask)
	if _, err := l.Supersede(s2e, "merged into "+s2c); err != nil {
		t.Fatal(err)
	}
	reshape(g, "added "+s4+": Any agent can produce the notes from the documented workflow", core.AnsweredByPerson)
	reshape(g, "edited "+s3+": title: Customers get a plain-language release note; done when: a customer note is produced", core.AnsweredByPerson)
	reshape(s2, "dropped "+s2d+": guides live with each release in the docs", core.AnsweredByPerson)
	reshape(g, s4+" waits on: "+s2+", "+s3, core.AnsweredByDirector)
	reshape(s2, "merged "+s2d+" into "+s2c, core.AnsweredByPerson)
	reshape(g, "split "+s1+": "+s2+" is now a sub-goal holding it", core.AnsweredByPerson)
	reshape(g, core.ApprovedPrefix+"4 sub-goals", core.AnsweredByPerson)
	event(t, l, g, core.EventDecision, "Release notes are written in English only for now")
	walk(t, l, g, core.StateRunning)
	for _, done := range []string{s1, s2a} {
		event(t, l, done, core.EventReport, "DONE\nSTATUS: DONE the change list is grouped by type")
		walk(t, l, done, core.StateDone)
		event(t, l, done, core.EventJudged, "met: the change list is grouped by type")
	}
	walk(t, l, s3, core.StateRunning)
	if err := l.SetSteps(s3, []core.Step{{N: 1, Text: "Read how past releases were announced", State: core.StepDone}, {N: 2, Text: "Write the customer note template", State: core.StepDoing}}); err != nil {
		t.Fatal(err)
	}
	walk(t, l, s2b, core.StateRunning, core.StateBlocked)
	event(t, l, s2b, core.EventReport, "BLOCKED\nSTATUS: BLOCKED the flag list lives in another repository")
	walk(t, l, s2c, core.StateRunning, core.StateNeedsInput)
	event(t, l, s2c, core.EventQuestion, "Should rollback cover database changes, or only redeploying the previous build?\n- Only redeploying the previous build (recommended)\n- Redeploy and undo database changes")
	walk(t, l, s4, core.StateRunning, core.StateNeedsInput)
	event(t, l, s4, core.EventReport, "NEEDS-INPUT\nSTATUS: NEEDS-INPUT which environments does a guide cover?")
	event(t, l, g, core.EventCourse, "correct: guides live with each release, not in the wiki")
	event(t, l, g, core.EventNote, "chore: Refreshed dependencies so the flags agent could run its checks")

	// A course change waiting on the person.
	d := add("Why does sync drop events?", ledger.AddOptions{Kind: core.WorkGoal})
	if err := l.SetFinish(d, core.FinishDiscovery, core.FinishFromGoal); err != nil {
		t.Fatal(err)
	}
	d1 := sub(d, "The cause is found and explained", "a findings report names the cause, with proof", core.WorkTask)
	d3 := sub(d, "Compare other sync vendors", "a comparison of two vendors", core.WorkTask)
	walk(t, l, d1, core.StateDone)
	reshape(d, "changed course: Sync never drops events | done when: every event lands | finish: complete PR", core.AnsweredByPerson)
	walk(t, l, d, core.StateRunning, core.StateNeedsInput)
	actions := []string{
		coordinator.Action{Kind: coordinator.ActGoal, Work: d, Title: "A rejected batch is retried until delivered", DoneWhen: "a throttled batch is retried until delivered", After: []string{d1}}.Line(),
		coordinator.Action{Kind: coordinator.ActSupersede, Work: d3, Text: "no longer needed: the vendor is fine once retried"}.Line(),
		coordinator.Action{Kind: coordinator.ActEdit, Work: d1, DoneWhen: "the report names the cause"}.Line(),
		coordinator.Action{Kind: coordinator.ActAfter, Work: d3, After: []string{"0badf00d"}}.Line(),
		coordinator.Action{Kind: coordinator.ActStart, Work: d3}.Line(),
	}
	asks := []coordinator.Action{
		{Kind: coordinator.ActAsk, Text: "Change course?\nNow: Why does sync drop events? | done when: not set | finish: discovery\nProposed: Sync never drops events | done when: every event lands | finish: complete PR\nWhy: the fix is small", Options: []string{"Change course (recommended)", "Keep the goal as it is"}},
		{Kind: coordinator.ActAsk, Text: "Change the plan?\n" + strings.Join(actions, "\n"), Options: []string{"Apply these changes (recommended)", "Keep the plan as it is"}},
	}
	if _, err := l.Ask(d, coordinator.QuestionBody(asks), core.Change{Actions: actions, Title: "Sync never drops events", DoneWhen: "every event lands", Finish: core.FinishPR}); err != nil {
		t.Fatal(err)
	}

	// A breakdown proposed in ask mode.
	a := add("An inspector can get a full audit trail", ledger.AddOptions{Kind: core.WorkGoal})
	a1 := sub(a, "Every record change keeps who, when, what and why", "a change stores its before and after", core.WorkTask)
	a2 := sub(a, "An admin can export the trail for a date range", "the export is a PDF and a spreadsheet", core.WorkTask)
	if err := l.SetAfter(a2, []string{a1}); err != nil {
		t.Fatal(err)
	}
	event(t, l, a, core.EventNote, core.ProposedPrefix+"2 sub-goals")

	// A goal finished at a pull request.
	p := add("Printed forms keep every signature", ledger.AddOptions{Kind: core.WorkGoal})
	if err := l.SetFinish(p, core.FinishPR, core.FinishFromPerson); err != nil {
		t.Fatal(err)
	}
	event(t, l, p, core.EventPr, "pull-request https://github.com/metariki/metariki-eqms/pull/512")
	walk(t, l, p, core.StateDone)

	ids := []string{g, s1, s2, s3, s4, s2a, s2b, s2c, s2d, s2e, d, d1, d3, a, a1, a2, p, "0badf00d"}
	var seen []string
	check := func(where, sentence string) {
		t.Helper()
		seen = append(seen, sentence)
		for _, id := range ids {
			if strings.Contains(sentence, id) {
				t.Errorf("%s names the id %s: %q", where, id, sentence)
			}
		}
		for _, re := range internalTerms {
			if m := re.FindString(sentence); m != "" {
				t.Errorf("%s says %q: %q", where, m, sentence)
			}
		}
	}

	api := httptest.NewServer(s.routes())
	defer api.Close()
	var board struct {
		Goals []struct {
			Finished *string `json:"finished"`
		} `json:"goals"`
	}
	getJSON(t, api.URL+"/api/board", http.StatusOK, &board)
	for _, bg := range board.Goals {
		if bg.Finished != nil {
			check("finished", *bg.Finished)
		}
	}
	var walkTree func(nodes []treeNode)
	walkTree = func(nodes []treeNode) {
		for _, n := range nodes {
			if n.WhyNotMoving != nil {
				check("why_not_moving of "+n.Work.Title, *n.WhyNotMoving)
			}
			walkTree(n.Children)
		}
	}
	for _, goal := range []string{g, s2, d, a, p} {
		var v struct {
			NotDoneBecause []string   `json:"not_done_because"`
			Tree           []treeNode `json:"tree"`
			NeedsYou       []struct {
				Change *struct {
					Lines []string `json:"lines"`
				} `json:"change"`
			} `json:"needs_you"`
		}
		getJSON(t, api.URL+"/api/goal/"+goal, http.StatusOK, &v)
		for _, why := range v.NotDoneBecause {
			check("not_done_because", why)
		}
		walkTree(v.Tree)
		for _, n := range v.NeedsYou {
			if n.Change != nil {
				for _, line := range n.Change.Lines {
					check("a change line", line)
				}
			}
		}
		var rec struct {
			Entries []servedEntry `json:"entries"`
		}
		getJSON(t, api.URL+"/api/record/"+goal, http.StatusOK, &rec)
		for _, e := range rec.Entries {
			if e.Kind != "delivery" {
				check("the record's "+e.Kind, e.What)
			}
		}
	}

	for _, want := range []string{
		"Not finished yet: the work isn't reported done, its checks haven't passed, and there's no pull request",
		"It hasn't been checked against what done means yet",
		"Proposed, waiting for your approval",
		"Compare other sync vendors waits on a sub-goal that no longer exists",
		"Finishes at a complete pull request, as your taste says",
		"Merged “Guides are published to the team wiki” into “The guide says how to roll back”",
		"“Any agent can produce the notes from the documented workflow” waits on “Whoever deploys gets a guide for each release” and “Customers get a plain-language release note”",
		"Reported stuck: the flag list lives in another repository",
		"superseded: merged into “The guide says how to roll back”",
		"merged into “The guide says how to roll back”",
	} {
		if !slices.Contains(seen, want) {
			t.Errorf("no sentence %q among %q", want, seen)
		}
	}

	gated := add("Gated", ledger.AddOptions{})
	gate := func() {
		t.Helper()
		var v gateView
		getJSON(t, api.URL+"/api/work/"+gated+"/gate", http.StatusOK, &v)
		check("the "+v.Gate+" gate", v.Why)
	}
	walk(t, l, gated, core.StateRunning, core.StateReview)
	event(t, l, gated, core.EventReport, "DONE\nSTATUS: DONE")
	gate()
	event(t, l, gated, core.EventVerify, "pass\ngo test → 0")
	gate()
	event(t, l, gated, core.EventPr, "commit https://example.test/c/1")
	gate()
	if _, err := l.SoftDone(gated, false); err != nil {
		t.Fatal(err)
	}
	gate()
}

func aGoalServesWhyWorkStoppedOrIsBlocked(t *testing.T) {
	s := newTestServer(t)
	l := s.ledger
	g, err := l.Add("p", "Release notes", ledger.AddOptions{Kind: core.WorkGoal})
	if err != nil {
		t.Fatal(err)
	}
	sub := func(title string, states ...core.State) string {
		t.Helper()
		w, err := l.Add("p", title, ledger.AddOptions{Parent: &g.ID})
		if err != nil {
			t.Fatal(err)
		}
		walk(t, l, w.ID, states...)
		return w.ID
	}
	stopped := sub("Tag every release", core.StateRunning)
	event(t, l, stopped, core.EventNote, "stopped without a report: I ran out of room before tagging v2")
	waiting := sub("Tag the hotfix", core.StateRunning, core.StateNeedsInput)
	event(t, l, waiting, core.EventNote, "stopped without a report: Tagged it.\nShall I push the tag?")
	cleared := sub("Group the change list", core.StateRunning)
	event(t, l, cleared, core.EventNote, "stopped without a report: halfway through")
	event(t, l, cleared, core.EventSent, "carry on")
	reported := sub("List the flags", core.StateRunning, core.StateBlocked)
	event(t, l, reported, core.EventReport, "BLOCKED\nSTATUS: BLOCKED the flag list lives in another repository")
	noted := sub("Cover rollback", core.StateRunning, core.StateBlocked)
	event(t, l, noted, core.EventReport, "BLOCKED\nSTATUS: BLOCKED\nNOTES: no access to staging")
	concerned := sub("Publish the guide", core.StateRunning, core.StateBlocked)
	if _, err := l.AddConcern(concerned, "the docs site is down"); err != nil {
		t.Fatal(err)
	}
	if c, err := l.AddConcern(concerned, "an old worry"); err != nil {
		t.Fatal(err)
	} else if _, err := l.ResolveConcern(c.ID, "settled"); err != nil {
		t.Fatal(err)
	}
	sub("Write the customer note", core.StateRunning, core.StateBlocked)
	running := sub("Announce it", core.StateRunning)
	if _, err := l.AddConcern(running, "the channel may be wrong"); err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer(s.routes())
	defer api.Close()
	type stop struct {
		Said string `json:"said"`
		At   string `json:"at"`
	}
	var view struct {
		Tree []struct {
			Work           core.Work `json:"work"`
			Stopped        *stop     `json:"stopped"`
			BlockedBecause *string   `json:"blocked_because"`
		} `json:"tree"`
	}
	getJSON(t, api.URL+"/api/goal/"+g.ID, http.StatusOK, &view)
	evs, err := l.Events(stopped, nil)
	if err != nil {
		t.Fatal(err)
	}
	noteAt := evs[len(evs)-1].At
	wantBlocked := map[string]string{reported: "the flag list lives in another repository", noted: "no access to staging", concerned: "the docs site is down"}
	if len(view.Tree) != 8 {
		t.Fatalf("tree has %d nodes, want 8", len(view.Tree))
	}
	for _, n := range view.Tree {
		id := n.Work.ID
		if id == stopped {
			if n.Stopped == nil || n.Stopped.Said != "I ran out of room before tagging v2" || n.Stopped.At != noteAt {
				t.Errorf("stopped of %s = %+v, want what it last said at %s", n.Work.Title, n.Stopped, noteAt)
			}
		} else if id == waiting {
			if n.Stopped == nil || n.Stopped.Said != "Tagged it.\nShall I push the tag?" {
				t.Errorf("stopped of %s = %+v, want all it last said while it waits on the person", n.Work.Title, n.Stopped)
			}
		} else if n.Stopped != nil {
			t.Errorf("stopped of %s = %+v, want null", n.Work.Title, n.Stopped)
		}
		want, blocked := wantBlocked[id]
		switch {
		case blocked && (n.BlockedBecause == nil || *n.BlockedBecause != want):
			t.Errorf("blocked_because of %s = %v, want %q", n.Work.Title, n.BlockedBecause, want)
		case !blocked && n.BlockedBecause != nil:
			t.Errorf("blocked_because of %s = %q, want null", n.Work.Title, *n.BlockedBecause)
		}
	}
}

func servingReleasesFinishedWorksWorktreesAtStartAndHourly(t *testing.T) {
	s := newTestServer(t)
	withSessions(s, &sessionRunner{})
	var mu sync.Mutex
	tidied := 0
	s.tidy = func() error {
		mu.Lock()
		defer mu.Unlock()
		tidied++
		return nil
	}
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.watchSessions(stop) }()
	// Long enough for several watches.
	time.Sleep(2*sessionWatch + time.Second)
	close(stop)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if tidied != 1 {
		t.Fatalf("worktrees released %d times over several watches, want once at start", tidied)
	}
	if tidyEvery != time.Hour {
		t.Fatalf("worktrees are released every %s, want hourly", tidyEvery)
	}
}
