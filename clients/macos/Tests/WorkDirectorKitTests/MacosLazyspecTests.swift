import Foundation
import Testing
@testable import WorkDirectorKit

/// A wd built from this checkout, so the app's tests read the wire the Go side
/// writes today.
let builtWD: URL = {
    let repo = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    let out = FileManager.default.temporaryDirectory
        .appendingPathComponent("wd-macos-tests-\(ProcessInfo.processInfo.processIdentifier)")
        .appendingPathComponent("wd")
    let go = Process()
    go.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    go.arguments = ["go", "build", "-o", out.path, "./cmd/wd"]
    go.currentDirectoryURL = repo
    do { try go.run() } catch { fatalError("go build: \(error)") }
    go.waitUntilExit()
    precondition(go.terminationStatus == 0, "go build ./cmd/wd failed in \(repo.path)")
    return out
}()

/// A ledger of its own, with one registered project named demo.
struct Home {
    let dir: URL
    let environment: [String: String]

    init() throws {
        dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let project = dir.appendingPathComponent("demo-repo")
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
        var env = ProcessInfo.processInfo.environment
        env["WD_HOME"] = dir.path
        env["WD_FAKE_STATE"] = dir.appendingPathComponent("fake-state").path
        env["PATH"] = dir.appendingPathComponent("bin").path + ":" + (env["PATH"] ?? "")
        environment = env
        try wd("projects", "add", "demo", project.path, "--runner", "claude", "--lazyspec", "n")
    }

    /// Runs wd as another process would, returning its trimmed stdout.
    @discardableResult
    func wd(_ argv: String...) throws -> String {
        let p = Process()
        p.executableURL = builtWD
        p.arguments = argv
        p.environment = environment
        let out = Pipe()
        p.standardOutput = out
        try p.run()
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        let text = String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
        guard p.terminationStatus == 0 else {
            throw ChannelError.refused(kind: "exit \(p.terminationStatus)", message: "wd \(argv.joined(separator: " ")): \(text)")
        }
        return text
    }

    /// Files a goal and returns its id.
    func goal(_ project: String, _ title: String) throws -> String {
        try JSONDecoder().decode(Work.self, from: Data(try wd("goal", "add", project, title, "--json").utf8)).id
    }

    func channel() throws -> Channel {
        try Channel(wd: builtWD, environment: environment)
    }
}

func executable(_ url: URL, _ script: String) throws {
    try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    try script.write(to: url, atomically: true, encoding: .utf8)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
}

func workJSON(state: String) -> String {
    """
    {"id":"w1","project":"demo","title":"T","detail":"","kind":"task","state":"\(state)",
     "runner":null,"session":null,"ref":null,"cwd":null,"created":"c","updated":"u",
     "parent":null,"heading":null,"claim":null,"impact":null,"goal_type":null,"archived":null}
    """
}

func tempDir() -> URL {
    FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
}

/// A runner that matches requests to goals and plans goals the way a model
/// would, with no model: it answers a find brief with the goal mentioning CSV,
/// and any other brief with a two-task plan.
func fakeRunner(_ home: Home) throws {
    let bin = home.dir.appendingPathComponent("bin")
    try executable(bin.appendingPathComponent("fake"), #"""
    #!/bin/sh
    state="$WD_FAKE_STATE"; mkdir -p "$state"
    case "$1" in
      run)
        n=$(( $(cat "$state/n" 2>/dev/null || echo 0) + 1 )); echo "$n" > "$state/n"
        printf '%s' "$2" > "$state/brief-$n"; echo "session=s$n";;
      export)
        brief="$state/brief-${2#s}"
        if grep -q '^You match a request' "$brief"; then
          hit=""
          grep -q 'CSV export' "$brief" && hit=$(grep -m1 '^- .*CSV' "$brief" | sed 's/^- \([0-9a-f]*\) .*/\1/')
          if [ -n "$hit" ]; then echo "MATCH: $hit | extends the CSV export"; else echo NONE; fi
        else
          printf '## Work\n- [ ] first step\n- [ ] second step\n'
        fi;;
      status) echo 'state: exited';;
      *) echo ok;;
    esac
    """#)
    try FileManager.default.createDirectory(at: home.dir.appendingPathComponent("runners"), withIntermediateDirectories: true)
    try """
    name = "fake"
    spawn = "fake run {brief}"
    session_id = 'session=([A-Za-z0-9-]+)'
    send = "fake send {session} {text}"
    status = "fake status {session}"
    running = 'state: *running'
    waiting = 'state: *waiting'
    exited = 'state: *exited'
    transcript = "fake export {session}"
    models = "fake models"
    attach = "fake attach {session}"
    stop = "fake stop {session}"
    """.write(to: home.dir.appendingPathComponent("runners/fake.toml"), atomically: true, encoding: .utf8)
}

@Suite struct Macos {
    @Test("The App Runs The wd Bundled Beside It")
    func appRunsBundledWD() throws {
        let app = tempDir().appendingPathComponent("Work Director.app")
        try FileManager.default.createDirectory(at: app.appendingPathComponent("Contents"), withIntermediateDirectories: true)
        try """
        <?xml version="1.0" encoding="UTF-8"?>
        <plist version="1.0"><dict>
        <key>CFBundleIdentifier</key><string>test.workdirector</string>
        <key>CFBundleExecutable</key><string>WorkDirector</string>
        </dict></plist>
        """.write(to: app.appendingPathComponent("Contents/Info.plist"), atomically: true, encoding: .utf8)
        let elsewhere = ["WD_BIN": "/elsewhere/wd"]

        let empty = try #require(Bundle(url: app))
        #expect(throws: LocatorError.notInBundle(empty.bundleURL.path)) { try locateWD(bundle: empty, environment: elsewhere) }

        let bundled = app.appendingPathComponent("Contents/MacOS/wd")
        try executable(bundled, "#!/bin/sh\n")
        let bundle = try #require(Bundle(url: app))
        #expect(try locateWD(bundle: bundle, environment: elsewhere).resolvingSymlinksInPath() == bundled.resolvingSymlinksInPath())

        let dev = tempDir()
        try FileManager.default.createDirectory(at: dev, withIntermediateDirectories: true)
        let plain = try #require(Bundle(url: dev))
        #expect(try locateWD(bundle: plain, environment: elsewhere).path == "/elsewhere/wd")

        let onPath = tempDir()
        try executable(onPath.appendingPathComponent("wd"), "#!/bin/sh\n")
        #expect(throws: LocatorError.noWD) { try locateWD(bundle: plain, environment: ["PATH": onPath.path]) }
    }

    @Test("A Reply Reaches The Request That Carries Its Id")
    func replyReachesItsRequest() async throws {
        // Answers the first two requests in reverse, refuses the third, and
        // exits while the fourth is outstanding.
        let fake = tempDir().appendingPathComponent("wd")
        try executable(fake, #"""
        #!/bin/sh
        id() { echo "$1" | sed 's/.*"id":\([0-9][0-9]*\).*/\1/'; }
        arg() { echo "$1" | sed 's/.*"argv":\["\([^"]*\)".*/\1/'; }
        read -r a; read -r b
        echo "{\"id\":$(id "$b"),\"result\":{\"code\":0,\"stdout\":\"$(arg "$b")\",\"stderr\":\"\"}}"
        echo "{\"id\":$(id "$a"),\"result\":{\"code\":0,\"stdout\":\"$(arg "$a")\",\"stderr\":\"\"}}"
        read -r c
        echo "{\"id\":$(id "$c"),\"error\":{\"kind\":\"not-found\",\"message\":\"no work x\"}}"
        read -r d
        exit 3
        """#)
        let channel = try Channel(wd: fake)
        async let one = channel.action(["one"])
        async let two = channel.action(["two"])
        let (first, second) = try await (one, two)
        #expect(first.stdout == "one")
        #expect(second.stdout == "two")
        await #expect(throws: ChannelError.refused(kind: "not-found", message: "no work x")) {
            try await channel.work("x")
        }
        await #expect(throws: ChannelError.exited(status: 3)) {
            try await channel.board()
        }
    }

    @Test("A Write Runs At Once And Shows The Command It Ran")
    func writeRunsAtOnce() async throws {
        let home = try Home()
        let id = try home.wd("add", "demo", "Pick a database")
        let plan = try Plan.decide(id, text: "use sqlite, it's what we know")
        #expect(plan.commandLine == #"wd decide \#(id) "use sqlite, it's what we know""#)
        #expect(try Plan.split(plan.commandLine) == ["wd"] + plan.argv)

        let store = await Store(channel: try home.channel())
        let result = try await store.run(plan)
        #expect(result.code == 0)
        let decisions = try await store.events(of: id).filter { $0.kind == "decision" }
        #expect(decisions.map(\.body) == ["use sqlite, it's what we know"])

        #expect(plan.undo == nil)
        #expect(Plan.cancel(id).undo?.argv == ["resume", id])
        #expect(Plan.pause(id).undo?.argv == ["resume", id])
        let cancelled = try await store.run(Plan.cancel(id))
        #expect(cancelled.code == 0)
        let undone = try await store.run(try #require(Plan.cancel(id).undo))
        #expect(undone.code == 0)
        #expect(try await store.work(id).state != .dropped)
    }

    @Test("Text In The Command Bar Becomes Argv Without A Shell")
    func commandBarSplitsWithoutAShell() throws {
        #expect(try Plan.typed(#"wd decide abc "two words" 'it said "hi"' $HOME ~ *"#).argv
            == ["decide", "abc", "two words", #"it said "hi""#, "$HOME", "~", "*"])
        #expect(try Plan.typed("  status   --json ").argv == ["status", "--json"])
        #expect(throws: PlanError.unclosedQuote) { try Plan.typed(#"decide abc "open"#) }
        #expect(throws: PlanError.empty) { try Plan.typed("wd") }
    }

    @Test("Only Reopening Asks For Words")
    func onlyReopeningAsksForWords() throws {
        let open = try JSONDecoder().decode(Work.self, from: Data(workJSON(state: "running").utf8))
        let rest = try JSONDecoder().decode(Work.self, from: Data(workJSON(state: "done").utf8))
        for blank in ["", "  ", "\n\t"] {
            #expect(throws: PlanError.reasonRequired("reopening")) { try Plan.reopen("g1", reason: blank) }
            #expect(Plan.cancel("g1", reason: blank).argv == ["cancel", "g1"])
            #expect(Plan.release("g1", reason: blank).argv == ["release", "g1"])
            #expect(Plan.archive(open, reason: blank).argv == ["archive", "w1"])
        }
        #expect(Plan.archive(rest).argv == ["archive", "w1"])
        #expect(Plan.archive(open, reason: " not wanted ").argv == ["archive", "w1", "not wanted"])
        #expect(Plan.cancel("g1", reason: "client said stop").argv == ["cancel", "g1", "client said stop"])
        #expect(try Plan.reopen("g1", reason: " add search ").argv == ["reopen", "g1", "add search"])
        #expect(Plan.release("g1", reason: "duplicate of g2").argv == ["release", "g1", "duplicate of g2"])
    }

    @Test("Needs-Input And Blocked Never Look The Same")
    func statesLookDistinct() {
        let looks = WorkState.allCases.map(\.look)
        #expect(Set(looks.map(\.label)).count == WorkState.allCases.count)
        #expect(Set(looks.map(\.symbol)).count == WorkState.allCases.count)
        #expect(WorkState.needsInput.look.tint != WorkState.blocked.look.tint)
    }

    @Test("A Change Made Elsewhere Reaches The Board")
    func changeElsewhereReachesBoard() async throws {
        let home = try Home()
        let store = await Store(channel: try home.channel())
        await store.start()
        #expect(await store.board?.standalone.isEmpty == true)

        let id = try home.wd("add", "demo", "Filed from a terminal")
        let deadline = Date().addingTimeInterval(2)
        var seen = false
        while Date() < deadline {
            if await store.board?.items(in: .inFlight).contains(where: { $0.id == id }) == true {
                seen = true
                break
            }
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(seen, "work filed by another wd process is not on the board within 2s")
        await store.stop()
    }

    @Test("A Product Scope Shows Only That Product's Work")
    func productScope() async throws {
        let home = try Home()
        let other = home.dir.appendingPathComponent("other-repo")
        try FileManager.default.createDirectory(at: other, withIntermediateDirectories: true)
        try home.wd("projects", "add", "other", other.path, "--runner", "claude", "--lazyspec", "n")
        let mine = try home.goal("demo", "Mine")
        let theirs = try home.goal("other", "Theirs")
        let task = try home.wd("add", "other", "Their task")
        let store = await Store(channel: try home.channel())
        await store.start()
        let board = try #require(await store.board)
        #expect(board.items(in: .inFlight, project: "demo").map(\.id) == [mine])
        #expect(Set(board.items(in: .inFlight, project: "other").map(\.id)) == Set([theirs, task]))
        #expect(board.items(in: .inFlight).count == 3)
        #expect(board.recentGoals(project: "other").map(\.id) == [theirs])
        #expect(board.recentGoals().count == 2)
        await store.stop()
    }

    @Test("A New Goal First Offers The Goals It May Continue")
    func newGoalOffersFirst() async throws {
        let home = try Home()
        try fakeRunner(home)
        let repo = home.dir.appendingPathComponent("fake-repo")
        try FileManager.default.createDirectory(at: repo, withIntermediateDirectories: true)
        for args in [["init", "-q", "-b", "main"], ["-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"]] {
            let git = Process()
            git.executableURL = URL(fileURLWithPath: "/usr/bin/env")
            git.arguments = ["git"] + args
            git.currentDirectoryURL = repo
            try git.run()
            git.waitUntilExit()
        }
        try home.wd("projects", "add", "tracker", repo.path, "--runner", "fake", "--lazyspec", "n")
        let csv = try home.goal("tracker", "Export tasks to CSV")
        try home.wd("done", csv, "--cancelled", "the first version shipped")
        let store = await Store(channel: try home.channel())
        await store.start()
        let flow = await NewGoal(store: store)

        await flow.submit(project: "tracker", text: "add tags to the CSV export")
        guard case .offering(let found) = await flow.phase else {
            Issue.record("phase = \(await flow.phase), want the CSV goal offered")
            return
        }
        #expect(found.map(\.goal.id) == [csv])
        #expect(try home.wd("status", "--all").contains("\(csv)\tdone"))

        await flow.continueGoal(csv, text: "add tags to the CSV export")
        guard case .started(let continued) = await flow.phase else {
            Issue.record("phase = \(await flow.phase), want the CSV goal continued")
            return
        }
        #expect(continued.goal.id == csv && continued.tasks.count == 2)

        await flow.reset()
        await flow.submit(project: "tracker", text: "A dark mode for the settings page")
        guard case .started(let fresh) = await flow.phase else {
            Issue.record("phase = \(await flow.phase), want a new goal started in one step")
            return
        }
        #expect(fresh.goal.title == "A dark mode for the settings page" && fresh.tasks.isEmpty, "the goal returns before it is planned")
        let deadline = Date().addingTimeInterval(20)
        var planned = 0
        while Date() < deadline {
            planned = try await store.goal(fresh.goal.id).tasks.count
            if planned == 2 { break }
            try await Task.sleep(for: .milliseconds(200))
        }
        #expect(planned == 2, "the background planning gave the goal its tasks")
        await store.stop()
    }

    @Test("A Session's Conversation Follows It, Reading Only What Can Still Change")
    func conversationFollowsTheSession() async throws {
        let home = try Home()
        var env = home.environment
        env["HOME"] = home.dir.path
        let bin = home.dir.appendingPathComponent("bin")
        // A claude that knows no live session: the store is read from its file.
        try executable(bin.appendingPathComponent("claude"), "#!/bin/sh\necho '[]'\n")
        let id = try home.wd("add", "demo", "Talked to")
        let session = "c0ffee00-0000-4000-8000-000000000001"
        try home.wd("attach", id, session, "--runner", "claude")
        let store = home.dir.appendingPathComponent(".claude/projects/anywhere/\(session).jsonl")
        try FileManager.default.createDirectory(at: store.deletingLastPathComponent(), withIntermediateDirectories: true)
        func lines(_ records: [String]) -> String { records.map { $0 + "\n" }.joined() }
        let opening = [
            #"{"type":"user","message":{"content":"the brief"}}"#,
            #"{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Weighing it"},{"type":"text","text":"Looking"}]}}"#,
            #"{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}"#,
        ]
        try lines(opening).write(to: store, atomically: true, encoding: .utf8)

        let channel = try Channel(wd: builtWD, environment: env)
        let shared = await Store(channel: channel)
        let feed = await Feed(store: shared, id: id)
        await feed.read()
        #expect(await feed.failure == nil)
        #expect(await feed.steps.map(\.kind) == [.prompt, .thinking, .text, .tool])
        #expect(await feed.stable == 3, "the running tool call is where the next read starts")

        let later = opening + [
            #"{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}"#,
            #"{"type":"assistant","message":{"content":[{"type":"tool_use","id":"q1","name":"AskUserQuestion","input":{"questions":[{"header":"Colour","question":"Which colour?","multiSelect":false,"options":[{"label":"Red","description":""},{"label":"Blue","description":""}]}]}}]}}"#,
        ]
        try lines(later).write(to: store, atomically: true, encoding: .utf8)
        await feed.read()
        let steps = await feed.steps
        #expect(steps.map(\.kind) == [.prompt, .thinking, .text, .tool, .question])
        #expect(steps[3].tool?.result == "ok")
        #expect(await feed.asking?.items.first?.question == "Which colour?")
        #expect(await feed.stable == 4, "the question waiting for its answer is where the next read starts")
        try await channel.close()
    }

    @Test("A Question Is Answered In Place, One Answer Per Item")
    func questionAnsweredInPlace() throws {
        let opts = [QuestionOption(label: "Red", description: ""), QuestionOption(label: "Blue", description: ""), QuestionOption(label: "Green", description: "")]
        let q = Question(id: "q1", items: [
            QuestionItem(header: "Colour", question: "Which colour?", multi: false, options: opts),
            QuestionItem(header: "Extras", question: "Which extras?", multi: true, options: opts),
        ])
        var draft = AnswerDraft(q)
        #expect(draft.answers == nil)
        draft.toggle("Red", item: 0)
        draft.toggle("Blue", item: 0)
        #expect(draft.answers == nil, "an item without an answer holds the whole answer back")
        draft.toggle("Green", item: 1)
        draft.toggle("Red", item: 1)
        draft.typed[1] = "a stripe"
        #expect(draft.answers == ["Blue", "Red, Green, a stripe"])
        draft.typed[0] = "teal"
        #expect(draft.answers?.first == "teal")

        let plan = try Plan.answer("w1", q, answers: ["teal", "Red, Green"])
        #expect(plan.argv == ["answer", "w1", "teal", "Red, Green"])
        #expect(throws: PlanError.empty) { try Plan.answer("w1", q, answers: ["teal"]) }
        #expect(throws: PlanError.empty) { try Plan.answer("w1", q, answers: ["teal", "  "]) }
    }

    @Test("The Key That Sends A Message Is The Person's Choice")
    func sendKeyIsAChoice() {
        #expect(SendKey.standard == .return)
        #expect(SendKey.return.press([]) == .send)
        #expect(SendKey.return.press(.shift) == .newline)
        for key in [SendKey.commandReturn, .optionReturn] {
            #expect(key.press(key.chord) == .send)
            #expect(key.press([]) == .newline, "Return starts a new line when \(key) sends")
        }
        #expect(SendKey.commandReturn.press(.option) == .newline)
        #expect(SendKey.optionReturn.press([.option, .shift]) == .newline)
        #expect(SendKey.return.press(.control) == .newline)
    }

    @Test("Stopping A Session Puts Back What It Had Not Read")
    func stopPutsBackWhatWasNotRead() async throws {
        let home = try Home()
        var env = home.environment
        env["HOME"] = home.dir.path
        // A claude with no live session: nothing is working, so stopping only
        // takes back what was waiting.
        try executable(home.dir.appendingPathComponent("bin/claude"), "#!/bin/sh\necho '[]'\n")
        let id = try home.wd("add", "demo", "Told the wrong thing")
        try home.wd("attach", id, "c0ffee00-0000-4000-8000-000000000002", "--runner", "claude")
        try home.wd("pause", id)
        try home.wd("send", id, "drop the tables")
        try home.wd("send", id, "all of them")

        #expect(Plan.stop(id).argv == ["stop", id, "--json"])
        let channel = try Channel(wd: builtWD, environment: env)
        let store = await Store(channel: channel)
        let back = try await store.stop(Plan.stop(id))
        #expect(back == "drop the tables\n\nall of them")
        try await channel.close()
    }

    @Test("Escape Pressed Twice Quickly Stops The Work")
    func escapeTwiceStops() {
        let t0 = Date(timeIntervalSince1970: 1000)
        var taps = EscapeTaps()
        #expect(taps.tap(at: t0) == false, "a lone Escape stops nothing")
        #expect(taps.tap(at: t0.addingTimeInterval(0.5)) == true)
        #expect(taps.tap(at: t0.addingTimeInterval(0.7)) == false, "a third quick Escape does not stop again")
        var slow = EscapeTaps()
        _ = slow.tap(at: t0)
        #expect(slow.tap(at: t0.addingTimeInterval(1.2)) == false, "a second Escape after the window stops nothing")
    }

    @Test("The Agents Panel Shows Each Task And The Subagents It Started")
    func agentsPanel() async throws {
        func task(_ id: String, _ state: String) throws -> Work {
            try JSONDecoder().decode(Work.self, from: Data(workJSON(state: state).replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#).utf8))
        }
        let a = try task("a", "running"), b = try task("b", "needs-input"), c = try task("c", "done"), quiet = try task("q", "queued")
        func activity(_ status: String, asking: Bool = false, usage: String, subagents: String = "[]") throws -> Activity {
            let q = asking ? #"{"id":"q1","items":[],"answers":null,"reply":null}"# : "null"
            return try JSONDecoder().decode(Activity.self, from: Data("""
            {"now":"working","kind":"text","at":null,"asking":\(q),"error":null,"status":"\(status)","usage":\(usage),"subagents":\(subagents)}
            """.utf8))
        }
        let usage = { (tok: Int, from: String, to: String) in #"{"model":"m","tokens":\#(tok),"tools":1,"started":"\#(from)","last":"\#(to)"}"# }
        let sub = #"[{"id":"s1","description":"Probe","type":"fork","done":true,"said":"found","usage":\#(usage(5, "2026-10-11T10:00:00Z", "2026-10-11T10:02:00Z"))},{"id":"s2","description":"Dig","type":"fork","done":false,"said":null,"usage":\#(usage(7, "2026-10-11T10:00:00Z", "2026-10-11T10:01:00Z"))}]"#
        let roster = AgentRoster(tasks: [a, b, c, quiet], activity: [
            "a": try activity("running", usage: usage(100, "2026-10-11T10:00:00Z", "2026-10-11T10:05:00Z"), subagents: sub),
            "b": try activity("waiting", asking: true, usage: usage(10, "2026-10-11T10:00:00Z", "2026-10-11T10:01:00Z")),
            "c": try activity("stopped", usage: usage(1000, "2026-10-11T09:00:00Z", "2026-10-11T09:30:00Z")),
        ])
        #expect(roster.cards.map(\.task) == ["a", "b", "c"], "a task with no session has no card")
        #expect(roster.cards.map(\.phase) == [.working, .asking, .settled])
        #expect(roster.cards[0].subagents.map(\.phase) == [.settled, .working])
        #expect(roster.settled == 2)
        #expect(roster.tokens == 1122)
        let now = AgentCard.instant("2026-10-11T10:10:00Z")!
        #expect(roster.cards[0].elapsed(now: now) == 600, "a working card runs to now")
        #expect(roster.cards[2].elapsed(now: now) == 1800, "a settled card stops at its last record")
        let goal = try task("g", "queued")
        let planned = AgentRoster(goal: goal, tasks: [a], activity: [
            "g": try activity("running", usage: usage(9, "2026-10-11T10:00:00Z", "2026-10-11T10:00:30Z")),
            "a": try activity("running", usage: usage(1, "2026-10-11T10:00:00Z", "2026-10-11T10:00:30Z")),
        ])
        #expect(planned.cards.map(\.title) == ["Plan", "T"], "a goal's planning comes first")
        #expect(AgentRoster(goal: goal, tasks: [a], activity: ["a": try activity("running", usage: usage(1, "2026-10-11T10:00:00Z", "2026-10-11T10:00:30Z"))]).cards.count == 1)

        let home = try Home()
        var env = home.environment
        env["HOME"] = home.dir.path
        try executable(home.dir.appendingPathComponent("bin/claude"), "#!/bin/sh\necho '[]'\n")
        let id = try home.wd("add", "demo", "Delegates")
        let session = "c0ffee00-0000-4000-8000-000000000003"
        try home.wd("attach", id, session, "--runner", "claude")
        let dir = home.dir.appendingPathComponent(".claude/projects/anywhere")
        try FileManager.default.createDirectory(at: dir.appendingPathComponent("\(session)/subagents"), withIntermediateDirectories: true)
        try #"{"type":"user","timestamp":"2026-10-11T10:00:00Z","message":{"content":"the brief"}}"#.appending("\n")
            .write(to: dir.appendingPathComponent("\(session).jsonl"), atomically: true, encoding: .utf8)
        try #"{"type":"user","isSidechain":true,"timestamp":"2026-10-11T10:01:00Z","message":{"content":"probe the parser"}}"#.appending("\n")
            .write(to: dir.appendingPathComponent("\(session)/subagents/agent-s9.jsonl"), atomically: true, encoding: .utf8)
        try #"{"agentType":"fork","description":"Probe"}"#.write(to: dir.appendingPathComponent("\(session)/subagents/agent-s9.meta.json"), atomically: true, encoding: .utf8)
        let channel = try Channel(wd: builtWD, environment: env)
        let store = await Store(channel: channel)
        let main = await Feed(store: store, id: id)
        await main.read()
        #expect(await main.subagents.map(\.id) == ["s9"])
        let probe = await Feed(store: store, id: id, agent: "s9")
        await probe.read()
        #expect(await probe.failure == nil)
        #expect(await probe.steps.map(\.text) == ["probe the parser"])
        try await channel.close()
    }

    @Test("The Sidebar Lists Work As Threads, Most Recently Active First")
    func sidebarThreads() throws {
        func work(_ id: String, _ state: String, project: String = "demo", updated: String) -> String {
            workJSON(state: state)
                .replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
                .replacingOccurrences(of: #""project":"demo""#, with: #""project":"\#(project)""#)
                .replacingOccurrences(of: #""updated":"u""#, with: #""updated":"\#(updated)""#)
        }
        let board = try JSONDecoder().decode(Board.self, from: Data("""
        {"goals":[
          {"work":\(work("old", "running", updated: "2026-10-01")),"rollup":{"counts":{},"done":0,"total":1},"band":"in-flight"},
          {"work":\(work("new", "needs-input", updated: "2026-10-09")),"rollup":{"counts":{},"done":0,"total":1},"band":"needs-you"},
          {"work":\(work("later", "queued", updated: "2026-10-08")),"rollup":{"counts":{},"done":0,"total":0},"band":"in-flight"},
          {"work":\(work("shipped", "done", updated: "2026-10-10")),"rollup":{"counts":{},"done":1,"total":1},"band":"at-rest"},
          {"work":\(work("elsewhere", "running", project: "other", updated: "2026-10-11")),"rollup":{"counts":{},"done":0,"total":1},"band":"in-flight"}
        ],"standalone":[
          {"work":\(work("fix", "running", updated: "2026-10-05")),"band":"in-flight"}
        ]}
        """.utf8))
        let t = board.threads(project: "demo")
        #expect(t.open.map(\.id) == ["new", "fix", "old"])
        #expect(t.queued.map(\.id) == ["later"])
        #expect(t.settled.map(\.id) == ["shipped"])
        #expect(board.threads().open.first?.id == "elsewhere")
        #expect(board.threads(project: "demo", band: .needsYou).open.map(\.id) == ["new"])
    }

    @Test("The Next Gate Is One Button")
    func nextGateIsOneButton() async throws {
        let home = try Home()
        let id = try home.wd("add", "demo", "Gated")
        let store = await Store(channel: try home.channel())
        #expect(Plan.passing(try await store.gate(of: id)) == nil, "queued work waits on no gate")
        try home.wd("set", id, "running")
        try home.wd("set", id, "review")
        try home.wd("report", id, "DONE\nSTATUS: DONE")
        let verify = try #require(Plan.passing(try await store.gate(of: id)))
        #expect(verify.argv == ["verify", id])
        #expect(verify.title == "Verify")
    }

    @Test("Work That Comes To Wait On You Is Announced Once")
    func arrivalsAnnouncedOnce() throws {
        func board(_ rows: [(String, String, String)]) throws -> Board {
            let goals = rows.map { id, state, band in
                #"{"work":\#(workJSON(state: state).replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)),"rollup":{"counts":{},"done":0,"total":1},"band":"\#(band)"}"#
            }
            return try JSONDecoder().decode(Board.self, from: Data(#"{"goals":[\#(goals.joined(separator: ","))],"standalone":[]}"#.utf8))
        }
        let first = try board([("a", "running", "in-flight"), ("b", "needs-input", "needs-you"), ("c", "running", "in-flight")])
        #expect(first.arrivals(since: nil).isEmpty, "nothing is announced from the first read")
        let second = try board([("a", "needs-input", "needs-you"), ("b", "needs-input", "needs-you"), ("c", "review", "ready-to-push"), ("d", "paused", "paused")])
        #expect(Set(second.arrivals(since: first).map(\.work.id)) == ["a", "c"])
        #expect(second.arrivals(since: second).isEmpty, "staying put is not news")
    }

    @Test("A Product Is Added From A Folder")
    func productAddedFromFolder() async throws {
        #expect(Plan.productName(forFolder: "/Users/me/code/My App_v2") == "my-app-v2")
        let home = try Home()
        let folder = home.dir.appendingPathComponent("My App")
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let store = await Store(channel: try home.channel())
        await store.start()
        let runners = try await store.doctor().runners.map(\.runner)
        #expect(Set(["claude", "opencode", "codex"]).isSubset(of: Set(runners)))
        let plan = try Plan.addProduct(name: Plan.productName(forFolder: folder.path), path: folder.path, runner: "claude", mode: "ask")
        #expect(plan.argv.prefix(7) == ["projects", "add", "my-app", folder.path, "--runner", "claude", "--mode"])
        #expect(try await store.run(plan).code == 0)
        try await store.reloadProjects()
        #expect(await store.projects.contains { $0.name == "my-app" && $0.mode == "ask" })
        #expect(throws: PlanError.empty) { try Plan.addProduct(name: " ", path: folder.path, runner: "claude", mode: "auto") }
        await store.stop()
    }
}
