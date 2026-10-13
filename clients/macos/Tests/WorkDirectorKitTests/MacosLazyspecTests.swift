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
    func wd(_ argv: String...) throws -> String { try wd(argv) }

    @discardableResult
    func wd(_ argv: [String]) throws -> String {
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

    /// Files a sub-goal under parent and returns its id.
    func sub(_ parent: String, _ title: String, _ extra: String...) throws -> String {
        let out = try wd(["goal", "add", parent, title, "--done-when", "\(title) is checked"] + extra + ["--json"])
        return try JSONDecoder().decode(Work.self, from: Data(out.utf8)).id
    }

    /// Writes what no wd command files by itself (an agent's steps, a
    /// supervisor's verdict, a session's question) straight into the ledger.
    func sql(_ statements: String) throws {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/sqlite3")
        p.arguments = [dir.appendingPathComponent("ledger.db").path, statements]
        try p.run()
        p.waitUntilExit()
        precondition(p.terminationStatus == 0, "sqlite3: \(statements)")
    }
}

func executable(_ url: URL, _ script: String) throws {
    try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    try script.write(to: url, atomically: true, encoding: .utf8)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
}

/// A goal view's tree fields for a goal with no sub-goals yet.
let treeless = #""chain":[],"tree":[],"paths":{},"done_when":null,"finish":null,"progress":{"done":0,"total":0,"superseded":0},"not_done_because":[],"needs_you":[],"course":null,"superseded":[],"proposed":false"#

/// A board goal's summary fields for a goal with no sub-goals yet.
let unplanned = #""progress":{"done":0,"total":0,"superseded":0},"needs_you":0,"finish":null,"finished":null"#

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

/// A goal tree as the canvas draws it: a done task, a sub-goal split in two
/// (one waiting on the person, one superseded), a working task with steps and
/// a task waiting on two siblings.
struct SeededTree {
    let home: Home
    let top, tagged, guide, note, agent, rollback, wiki: String

    init() throws {
        home = try Home()
        top = try home.goal("demo", "Release notes for every release")
        try home.wd("goal", "edit", top, "--done-when", "every release has a guide and a note")
        tagged = try home.sub(top, "Every release is tagged")
        guide = try home.sub(top, "Whoever deploys gets a guide", "--split")
        note = try home.sub(top, "Customers get a release note")
        agent = try home.sub(top, "Any agent can produce the notes", "--after", "\(tagged),\(guide)")
        rollback = try home.sub(guide, "The guide says how to roll back")
        wiki = try home.sub(guide, "Guides are published to the wiki")
        try home.wd("goal", "drop", wiki, "guides live with each release")
        try home.sql("""
        UPDATE work SET finish_line='complete PR', finish_source='taste', state='running' WHERE id='\(top)';
        UPDATE work SET state='done', runner='claude', session='s1' WHERE id='\(tagged)';
        UPDATE work SET state='running', runner='claude', session='s2' WHERE id='\(note)';
        UPDATE work SET state='needs-input', runner='codex', session='s3' WHERE id='\(rollback)';
        INSERT INTO step VALUES ('\(tagged)',1,'Tag','done'),('\(tagged)',2,'List','done');
        INSERT INTO step VALUES ('\(note)',1,'Read past notes','done'),('\(note)',2,'Write the template','doing'),('\(note)',3,'Generate a sample','todo'),('\(note)',4,'Old idea','dropped');
        INSERT INTO event (work,kind,body,at) VALUES ('\(top)','course','on course: the tag format suits the rest','2026-10-13T11:52:00Z');
        INSERT INTO event (work,kind,body,at) VALUES ('\(tagged)','judged','met: every release has a tag','2026-10-13T11:48:00Z');
        INSERT INTO event (work,kind,body,at) VALUES ('\(guide)','note','chore: refreshed dependencies','2026-10-13T11:31:00Z');
        INSERT INTO event (work,kind,body,at) VALUES ('\(rollback)','question','Cover down-migrations?
        - Only redeploy (recommended)
        - Both','2026-10-13T12:14:00Z');
        """)
    }
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
        #expect(draft.answers == ["Blue", "(no answer)"], "an item left without an answer is sent as no answer")
        draft.toggle("Green", item: 1)
        draft.toggle("Red", item: 1)
        draft.typed[1] = "a stripe"
        #expect(draft.answers == ["Blue", "Red, Green, a stripe"])
        draft.typed[0] = "teal"
        #expect(draft.answers?.first == "teal")

        let yesNo = Question(id: "q2", items: [QuestionItem(question: "Tag it?", options: [QuestionOption(label: "Yes (recommended)"), QuestionOption(label: "No")])])
        var picked = AnswerDraft(yesNo)
        picked.toggle("Yes (recommended)", item: 0)
        #expect(picked.answers == ["Yes"])
        #expect(AnswerDraft(yesNo).directorDecides == ["Yes"], "the director decides with its recommendation")
        #expect(AnswerDraft(q).directorDecides == ["Decide this yourself.", "Decide this yourself."])

        let plan = try Plan.answer("w1", q, answers: ["teal", "Red, Green"])
        #expect(plan.argv == ["answer", "w1", "teal", "Red, Green"])
        #expect(throws: PlanError.empty) { try Plan.answer("w1", q, answers: ["teal"]) }
        #expect(throws: PlanError.empty) { try Plan.answer("w1", q, answers: ["teal", "  "]) }
    }

    @Test("The Person Chooses A Light, Dark Or The System's Appearance")
    func appearanceChosen() {
        #expect(AppearanceChoice.allCases.map(\.title) == ["System", "Light", "Dark"])
        #expect(AppearanceChoice(rawValue: "dark")?.dark == true, "`-appearance dark` on the command line forces dark")
        #expect(AppearanceChoice(rawValue: "light")?.dark == false)
        #expect(AppearanceChoice.system.dark == nil, "the system's look is followed by default")
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

    @Test("A Goal Reads As One Conversation With Its Supervisor And Its Agents")
    func goalConversation() throws {
        func event(_ id: Int, _ work: String, _ kind: String, _ body: String, _ at: String) -> String {
            let quoted = String(data: try! JSONEncoder().encode(body), encoding: .utf8)!
            return #"{"id":\#(id),"work":"\#(work)","kind":"\#(kind)","body":\#(quoted),"at":"2026-10-12T01:\#(at)Z","effective":null,"decision":null}"#
        }
        func answer(_ id: Int, _ work: String, _ body: String, by source: String, _ at: String) -> String {
            let decision = #"{"question":"q","answer":"\#(body)","source":"\#(source)","runner":"","model":"","tokens":0,"reverses":0,"reversed_by":0}"#
            return #"{"id":\#(id),"work":"\#(work)","kind":"answer","body":"\#(body)","at":"2026-10-12T01:\#(at)Z","effective":null,"decision":\#(decision)}"#
        }
        func work(_ id: String, _ title: String, _ state: String) -> String {
            workJSON(state: state).replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
                .replacingOccurrences(of: #""title":"T""#, with: #""title":"\#(title)""#)
        }
        let supervised = "The collector never calls the detector.\nACTIONS:\nSEND a: wire it in\nTASK: Weekly note\n\nDone:\n- SEND a: wire it in\n- TASK: Weekly note\n- RESOLVE 18: a wires it in\n\nRefused:\n- START b — b is review, already under way"
        let events = [
            event(5, "g", "supervised", supervised, "05:00"),
            event(1, "g", "decision", "keep the js script", "01:00"),
            event(2, "a", "state", "running", "02:00"),
            event(3, "a", "note", "stopped without a report: wiring belongs to the collector", "03:00"),
            event(4, "g", "look", "a task was noted on (1)", "04:00"),
            event(6, "g", "spawn", "supervise claude s1", "04:00"),
            event(7, "b", "state", "dropped", "06:00"),
            event(8, "a", "question", "which driver?", "07:00"),
            answer(9, "a", "sqlite", by: "director", "08:00"),
            event(10, "a", "sent", "sqlite", "08:00"),
            event(11, "g", "question", "Tag v1.0.0?\n- yes (recommended)\n- no", "09:00"),
            answer(12, "g", "yes", by: "person", "10:00"),
        ].joined(separator: ",")
        let asking = #"{"id":"event-11","items":[{"header":"","question":"Tag v1.0.0?","multi":false,"options":[{"label":"yes (recommended)","description":""},{"label":"no","description":""}]}],"answers":null,"reply":null}"#
        let json = """
        {"goal":\(work("g", "Release notes", "running")),"tasks":[\(work("a", "Impacts", "running")),\(work("b", "Collector", "review")),\(work("n", "Weekly note", "running"))],
         "rollup":{"counts":{},"done":0,"total":3},"events":[\(events)],"claims":[],"landings":[],"delivery":null,"activity":{},"asking":\(asking),"reports":{},"waiting":[],\(treeless)}
        """
        let goal = try JSONDecoder().decode(GoalDetail.self, from: Data(json.utf8))
        #expect(goal.supervised)
        #expect(goal.asking?.items.first?.options.map(\.label) == ["yes (recommended)", "no"])
        let steps = goal.thread
        #expect(steps.map(\.id) == [1, 3, 4, 5, 7, 8, 11], "oldest first; a state change that does not end work, a spawn and an answer's message are left out")
        guard case .exchange(_, let auto?) = steps[5] else { Issue.record("step 6 is \(steps[5])"); return }
        #expect(auto.id == 9 && auto.answeredByDirector && !goal.isYours(auto))
        guard case .exchange(let asked, let mine?) = steps[6] else { Issue.record("step 7 is \(steps[6])"); return }
        #expect(asked.askedItems.map(\.question) == ["Tag v1.0.0?"] && asked.askedItems.first?.choices == ["yes (recommended)", "no"])
        #expect(mine.id == 12 && !mine.answeredByDirector)
        #expect(goal.yours.map(\.id) == [1, 12])
        guard case .stopped(_, let said) = steps[1] else { Issue.record("step 2 is \(steps[1])"); return }
        #expect(said == "wiring belongs to the collector")
        guard case .look(let look) = steps[2] else { Issue.record("step 3 is \(steps[2])"); return }
        #expect(look.body == "a task was noted on (1)")
        guard case .supervisor(_, let reply) = steps[3] else { Issue.record("step 4 is \(steps[3])"); return }
        #expect(reply.said == "The collector never calls the detector.", "the action lines are not said twice")
        #expect(reply.actions.map(\.task) == ["a", "n", nil], "each action names the task it told or filed")
        #expect(reply.refused == ["START b — b is review, already under way"])
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
        let overseen = AgentRoster(goal: goal, supervised: true, tasks: [a], activity: [
            "g": try activity("idle", usage: usage(9, "2026-10-11T10:00:00Z", "2026-10-11T10:00:30Z")),
        ])
        #expect(overseen.cards.map(\.title) == ["Supervisor"], "a supervised goal's own session is its supervisor")
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

    @Test("The Sidebar Bands Top Goals By Whether They Need You, Work Or Are Completed")
    func sidebarBands() throws {
        func work(_ id: String, _ state: String, project: String = "demo", updated: String) -> String {
            workJSON(state: state)
                .replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
                .replacingOccurrences(of: #""kind":"task""#, with: #""kind":"goal""#)
                .replacingOccurrences(of: #""project":"demo""#, with: #""project":"\#(project)""#)
                .replacingOccurrences(of: #""updated":"u""#, with: #""updated":"\#(updated)""#)
        }
        func goal(_ id: String, _ state: String, _ band: String, project: String = "demo", updated: String,
                  done: Int = 0, total: Int = 0, needsYou: Int = 0, finished: String = "null") -> String {
            #"{"work":\#(work(id, state, project: project, updated: updated)),"rollup":{"counts":{},"done":0,"total":0},"band":"\#(band)","progress":{"done":\#(done),"total":\#(total),"superseded":0},"needs_you":\#(needsYou),"finish":{"line":"discovery","source":"goal"},"finished":\#(finished)}"#
        }
        let task = workJSON(state: "running").replacingOccurrences(of: #""id":"w1""#, with: #""id":"fix""#).replacingOccurrences(of: #""updated":"u""#, with: #""updated":"2026-10-05""#)
        let board = try JSONDecoder().decode(Board.self, from: Data("""
        {"goals":[
          \(goal("old", "running", "in-flight", updated: "2026-10-01", done: 1, total: 4)),
          \(goal("asks", "needs-input", "needs-you", updated: "2026-10-09", done: 1, total: 4, needsYou: 1)),
          \(goal("below", "running", "in-flight", updated: "2026-10-07", needsYou: 2)),
          \(goal("later", "queued", "in-flight", updated: "2026-10-08")),
          \(goal("shipped", "done", "at-rest", updated: "2026-10-10", done: 2, total: 2, finished: #""Complete PR #512""#)),
          \(goal("gone", "dropped", "at-rest", updated: "2026-10-10")),
          \(goal("elsewhere", "running", "in-flight", project: "other", updated: "2026-10-11"))
        ],"standalone":[{"work":\(task),"band":"in-flight"}]}
        """.utf8))
        let bands = board.sidebar(project: "demo")
        #expect(bands.needsYou.map(\.id) == ["asks", "below"], "a question below a goal puts it in Needs you")
        #expect(bands.working.map(\.id) == ["later", "fix", "old"])
        #expect(bands.completed.map(\.id) == ["shipped"])
        #expect(bands.letGo.map(\.id) == ["gone"])
        #expect(board.sidebar().working.first?.id == "elsewhere")

        #expect(bands.needsYou[0].meta == "1 of 4")
        #expect(bands.needsYou[0].needsYouBadge == "1 needs you")
        #expect(bands.needsYou[1].needsYouBadge == "2 needs you")
        #expect(bands.completed[0].meta == "Complete PR #512")
        #expect(bands.working[0].meta == "Discovery", "an unplanned goal says where it finishes")
        #expect(bands.working[2].needsYouBadge == nil)

        let chain = [ChainLink(id: "asks", title: "Release notes", state: .needsInput), ChainLink(id: "g2", title: "Whoever deploys gets a guide", state: .needsInput)]
        #expect(bands.needsYou[0].openPath(chain).map(\.id) == ["g2"])
        #expect(bands.needsYou[1].openPath(chain).isEmpty)
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
                #"{"work":\#(workJSON(state: state).replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)),"rollup":{"counts":{},"done":0,"total":1},"band":"\#(band)",\#(unplanned)}"#
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

    @Test("A Correction Can Be Kept As Feedback")
    func correctionKeptAsFeedback() async throws {
        let home = try Home()
        let id = try home.wd("add", "demo", "Taught")
        let store = await Store(channel: try home.channel())
        let work = try await store.work(id)
        let plan = try Plan.remember("  always pin versions ", work: work)
        #expect(plan.argv == ["feedback", "add", "always pin versions", "--work", id, "--project", "demo", "--source", "director"])
        #expect(try await store.run(plan).code == 0)
        let listed = try home.wd("feedback", "list")
        #expect(listed.contains("always pin versions"))
        #expect(throws: PlanError.empty) { try Plan.remember(" ", work: work) }
    }

    @Test("A Message Shows At Once And The Field Is Free Again")
    func messageShowsAtOnce() async throws {
        let home = try Home()
        let goal = try home.goal("demo", "Pick a database")
        let store = await Store(channel: try home.channel())
        func settled(_ id: UUID) async throws -> Outgoing {
            let deadline = Date().addingTimeInterval(5)
            while Date() < deadline {
                if let m = await store.outbox.first(where: { $0.id == id }), m.state != .sending { return m }
                try await Task.sleep(for: .milliseconds(20))
            }
            throw CancellationError()
        }

        let taken = await store.post(try Plan.decide(goal, text: "use sqlite"), to: goal)
        #expect(taken.state == .sending && taken.text == "use sqlite")
        #expect(await store.outbox.map(\.id) == [taken.id])
        #expect(try await settled(taken.id).state == .sent)
        let detail = try await store.goal(goal)
        #expect(taken.arrived(in: detail))
        #expect(detail.yours.map(\.body) == ["use sqlite"])
        #expect(detail.isYours(try #require(detail.yours.first)))

        let refused = await store.post(try Plan.send("nope", text: "hello"), to: "nope")
        guard case .failed(let why, sent: false) = try await settled(refused.id).state else {
            Issue.record("a message to unknown work was not refused")
            return
        }
        #expect(!why.isEmpty)
        await store.discard(refused.id)
        #expect(await store.outbox.contains { $0.id == refused.id } == false)

        let half = await store.post(try Plan.decide(goal, text: "keep it local"), to: goal, then: try Plan.typed("wd decide nope x"))
        guard case .failed(_, sent: true) = try await settled(half.id).state else {
            Issue.record("a message whose follow-up failed is not marked taken")
            return
        }
        await store.retry(half.id)
        try await Task.sleep(for: .milliseconds(50))
        _ = try await settled(half.id)
        let again = try await store.goal(goal)
        #expect(again.yours.map(\.body) == ["use sqlite", "keep it local"])
    }

    @Test("A Conversation Keeps The Newest In View Until You Scroll Up To Read")
    func followsUntilScrolledUp() {
        #expect(Follow.atBottom(offset: 0, visible: 600, content: 400), "nothing to scroll")
        #expect(Follow.atBottom(offset: 1400, visible: 600, content: 2000), "at the very bottom")
        #expect(Follow.atBottom(offset: 1360, visible: 600, content: 2000), "a few points short still follows")
        #expect(!Follow.atBottom(offset: 1300, visible: 600, content: 2000), "scrolled up to read")
        #expect(!Follow.atBottom(offset: 1400, visible: 600, content: 2200), "more came in while reading further up")
    }

    @Test("Work Is Renamed From Its Page Or Its Menu")
    func renamedFromPageOrMenu() async throws {
        let home = try Home()
        let goal = try home.goal("demo", "fix the config")
        let plan = try Plan.rename(goal, title: "  Config fixes ")
        #expect(plan.argv == ["rename", goal, "Config fixes"])
        #expect(throws: PlanError.empty) { try Plan.rename(goal, title: "  ") }
        let store = await Store(channel: try home.channel())
        #expect(try await store.run(plan).code == 0)
        #expect(try await store.work(goal).title == "Config fixes")
    }

    @Test("A Goal Is Read As Its Overview, Its Conversation Or Its Record")
    func goalViews() throws {
        func event(_ id: Int, _ work: String, _ kind: String, _ body: String, decision: String = "null") -> String {
            let quoted = String(data: try! JSONEncoder().encode(body), encoding: .utf8)!
            return #"{"id":\#(id),"work":"\#(work)","kind":"\#(kind)","body":\#(quoted),"at":"2026-10-12T01:\#(String(format: "%02d", id)):00Z","effective":null,"decision":\#(decision)}"#
        }
        func work(_ id: String, _ state: String) -> String {
            workJSON(state: state).replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
        }
        let person = #"{"question":"q","answer":"yes","source":"person","runner":"","model":"","tokens":0,"reverses":0,"reversed_by":0}"#
        let events = [
            event(1, "g", "decision", "keep the js script"),
            event(2, "a", "state", "paused"),
            event(3, "g", "look", "a task reported (1)"),
            event(4, "a", "verify", "pass"),
            event(5, "a", "report", "DONE\nSTATUS: DONE\nFILES: x.ts"),
            event(6, "g", "supervised", "All fits.\nACTIONS:\nWAIT"),
            event(7, "g", "question", "Tag it?\n- yes\n- no"),
            event(8, "g", "answer", "yes", decision: person),
            event(9, "b", "state", "blocked"),
        ].joined(separator: ",")
        let report = #"{"verdict":"done","said":"","files":["x.ts"],"file_note":"","sections":[{"name":"VERIFY","text":"go test → 0"}]}"#
        let asking = #"{"id":"event-1","items":[],"answers":null,"reply":null}"#
        let json = """
        {"goal":\(work("g", "running")),"tasks":[\(work("a", "done")),\(work("b", "blocked")),\(work("c", "running")),\(work("d", "queued")),\(work("e", "dropped")),\(work("f", "review"))],
         "rollup":{"counts":{},"done":1,"total":6},"events":[\(events)],"claims":[],"landings":[],"delivery":null,
         "activity":{"c":{"now":"Bash","kind":"tool","at":null,"asking":\(asking),"error":null,"status":"running","usage":null,"subagents":null}},
         "asking":\(asking),"reports":{"5":\(report)},"waiting":[],\(treeless)}
        """
        let goal = try JSONDecoder().decode(GoalDetail.self, from: Data(json.utf8))
        #expect(GoalView.allCases.map(\.label) == ["Overview", "Conversation", "Record"])

        let all = goal.items(.all)
        #expect(all.map(\.id) == [1, 2, 5, 6, 7, 9], "updates 2–4 fold into one row; a lone update stays a step")
        guard case .updates(let run) = all[1] else { Issue.record("row 2 is \(all[1])"); return }
        #expect(run.map(\.id) == [2, 3, 4])

        let r = try #require(goal.reports["5"])
        #expect(r.verdict == "done")
        #expect(r.files == ["x.ts"])
        #expect(r.sections.map(\.name) == ["VERIFY"])
    }
    @Test("The Kit Reads A Goal As A Tree")
    func goalAsTree() async throws {
        let t = try SeededTree()
        let (home, top, tagged, guide, note, agent, rollback, wiki) = (t.home, t.top, t.tagged, t.guide, t.note, t.agent, t.rollback, t.wiki)
        let store = await Store(channel: try home.channel())
        let goal = try await store.goal(top)
        #expect(goal.chain.map(\.id) == [top])
        #expect(goal.doneWhen == "every release has a guide and a note")
        #expect(goal.finish == FinishLine(line: "complete PR", source: "taste"))
        #expect(goal.progress == TreeProgress(done: 1, total: 4, superseded: 0))
        #expect(goal.notDoneBecause.first == "3 sub-goals open")
        #expect(goal.course?.verdict == "on course")
        #expect(goal.course?.why == "the tag format suits the rest")
        #expect(goal.tree.map(\.id) == [tagged, guide, note, agent])
        let guideNode = goal.tree[1]
        #expect(guideNode.opensAsGoal && !goal.tree[0].opensAsGoal)
        #expect(guideNode.children.map(\.id) == [rollback, wiki])
        #expect(guideNode.plannedInto == 2)
        #expect(guideNode.needsYou == 1)
        #expect(guideNode.children[1].work.state == .superseded)
        #expect(goal.tree[3].waitingOn == ["Whoever deploys gets a guide"], "a settled sibling is no longer waited on")
        #expect(goal.tree[2].steps.map(\.state) == [.done, .doing, .todo, .dropped])
        #expect(goal.tree[2].doing == "Write the template")
        #expect(goal.paths[rollback] == "Whoever deploys gets a guide › The guide says how to roll back")
        #expect(goal.branch(to: rollback).map(\.id) == [guide, rollback])
        #expect(goal.needsYou.map(\.work) == [rollback])
        #expect(goal.needsYou.first?.question.items.first?.options.first?.label == "Only redeploy (recommended)")
        #expect(goal.superseded.map(\.work) == [wiki])
        #expect(goal.superseded.first?.why == "guides live with each release")

        let nested = try await store.goal(guide)
        #expect(nested.chain.map(\.id) == [top, guide], "a node that is a goal opens as a page of its own")
        #expect(nested.tree.map(\.id) == [rollback, wiki])

        await store.refresh()
        let board = try #require(await store.board)
        let row = try #require(board.goals.first { $0.work.id == top })
        #expect(board.goals.map(\.work.id) == [top], "only top goals are on the board")
        #expect(row.progress == TreeProgress(done: 1, total: 4, superseded: 0))
        #expect(row.needsYou == 1)
        #expect(row.finish?.line == "complete PR")
        #expect(row.finished == nil)

        let record = try await store.record(top)
        #expect(record.summary.course == 1)
        #expect(record.entries.first?.kind == "question")
        #expect(record.entries.first?.where == "Whoever deploys gets a guide › The guide says how to roll back")
        #expect(record.entries.last?.kind == "asked")
    }

    @Test("A Goal Page Shows What Done Means, What Needs You And Its Sub-Goals")
    func goalPage() async throws {
        let t = try SeededTree()
        let store = await Store(channel: try t.home.channel())
        let goal = try await store.goal(t.top)
        #expect(goal.titleBadge == GoalBadge("Needs you", .attention), "a question below the goal shows on its title")
        #expect(goal.finishWords?.line == "A complete PR")
        #expect(goal.finishWords?.source == "From your taste.")
        #expect(goal.course?.badge == GoalBadge("On course", .settled))
        #expect(goal.progressLine == "1 of 4 done")

        let ask = try #require(goal.needsYou.first)
        #expect(goal.pathParts(of: ask).map(\.title) == ["Whoever deploys gets a guide", "The guide says how to roll back"])
        #expect(goal.pathParts(of: ask).map(\.opens) == [t.guide, nil], "a level that is a goal opens its page")

        let rows = Dictionary(uniqueKeysWithValues: goal.tree.map { ($0.id, $0) })
        let tagged = try #require(rows[t.tagged]), guide = try #require(rows[t.guide])
        let note = try #require(rows[t.note]), agent = try #require(rows[t.agent])
        #expect(tagged.mark == .done && tagged.badge == GoalBadge("Done", .settled))
        #expect(tagged.meta() == "Done when Every release is tagged is checked · claude · 2 steps · judged met")
        #expect(guide.mark == .needsYou && guide.badge == GoalBadge("1 needs you", .attention))
        #expect(guide.meta() == "Planned into 1 sub-goal · 0 of 1 done · 1 superseded", "superseded ones are counted apart")
        #expect(guide.opensAsGoal && !guide.folds, "a goal row opens its page")
        #expect(note.mark == .working && note.badge == GoalBadge("Working · 1 of 3", .live), "dropped steps are not counted")
        #expect(note.meta() == "claude · now: Write the template")
        #expect(note.folds, "a task row folds its steps")
        #expect(agent.mark == .waiting && agent.badge == GoalBadge("Waiting", .waiting))
        #expect(agent.meta() == "Waits on: Whoever deploys gets a guide")
        #expect(goal.openSteps == t.note, "the first working row starts open")

        let nested = try await store.goal(t.guide)
        let wiki = try #require(nested.tree.first { $0.id == t.wiki })
        #expect(wiki.mark == .superseded)
        #expect(wiki.meta(superseded: nested.superseded) == "Superseded: guides live with each release · its work stays readable")
        let rollback = try #require(nested.tree.first { $0.id == t.rollback })
        #expect(rollback.meta() == "codex · waits for you")
        #expect(nested.progressLine == "0 of 1 done · 1 superseded")
        #expect(goal.notDoneBecause.count == 3, "open sub-goals, the done-when and the finish line")

        #expect(goal.askedAt(ask.question) == "2026-10-13T12:14:00Z")
        let add = try Plan.addSubGoal(t.top, outcome: " Notes are translated ", doneWhen: "each note has a French copy")
        #expect(add.argv == ["goal", "add", t.top, "Notes are translated", "--done-when", "each note has a French copy"])
        #expect(throws: PlanError.self) { try Plan.addSubGoal(t.top, outcome: "Notes", doneWhen: " ") }
        #expect(try await store.run(add).code == 0)
        #expect(try await store.goal(t.top).tree.last?.work.title == "Notes are translated")

        let here = try await store.record(t.guide)
        #expect(here.entries.contains { $0.kind == "chore" && $0.by == "director" }, "a nested page's record holds what the director did there")
        #expect(here.entries.first { $0.kind == "chore" }?.sentence == "Chore: refreshed dependencies")
        #expect(try await store.record(t.top).entries.first { $0.kind == "course" }?.text == "The tag format suits the rest", "ledger text in a sentence's place starts with a capital")
    }

    @Test("The Record Lists What Was Decided And Done, Filtered By Kind And Sub-Goal")
    func goalRecord() async throws {
        let t = try SeededTree()
        try t.home.wd("decide", t.top, "Notes are written in English only")
        let store = await Store(channel: try t.home.channel())
        let goal = try await store.goal(t.top)
        let record = try await store.record(t.top)
        let cards = record.summary.cards
        #expect(cards.count == 5)
        #expect(cards[1].number == 1 && cards[1].label == "course check · 0 corrections")
        #expect(cards[3].label == "chore the director ran")
        #expect(cards[4].label == "report judged")

        #expect(RecordKind.allCases.map(\.label) == ["Everything", "Decisions", "Course", "Questions", "Reports", "Chores", "Delivery"])
        #expect(record.entries(.everything) == record.entries, "everything, newest first, as served")
        #expect(record.entries(.course).map(\.kind) == ["course", "superseded"], "a superseded branch is a course change")
        #expect(record.entries(.questions).map(\.kind) == ["question"])
        #expect(record.entries(.chores).map(\.text) == ["refreshed dependencies"])
        #expect(record.entries(.decisions).contains { $0.what == "Notes are written in English only" && $0.by == "you" })
        #expect(record.entries(.decisions).last?.kind == "asked", "the goal's first ask closes the record")

        #expect(goal.recordPlaces == ["Every release is tagged", "Whoever deploys gets a guide", "Customers get a release note", "Any agent can produce the notes"])
        let guide = record.entries(.everything, under: "Whoever deploys gets a guide")
        #expect(Set(guide.map(\.kind)).isSuperset(of: ["question", "chore", "superseded"]), "a sub-goal holds what happened under it")
        #expect(guide.allSatisfy { $0.where.hasPrefix("Whoever deploys gets a guide") })
        #expect(record.entries(.everything, under: "Every release is tagged").map(\.badge) .contains(GoalBadge("Judged met", .settled)))

        var gmt = Calendar(identifier: .gregorian)
        gmt.timeZone = TimeZone(identifier: "UTC")!
        let now = ISO8601DateFormatter().date(from: "2026-10-13T18:00:00Z")!
        let e = { (at: String) in RecordEntry(event: 1, at: at, kind: "chore", what: "x", where: "w", by: "director") }
        let days = GoalRecord.days([e("2026-10-13T12:00:00Z"), e("2026-10-13T09:00:00Z"), e("2026-10-12T10:00:00Z"), e("2026-10-01T10:00:00Z")], now: now, calendar: gmt)
        #expect(days.map(\.day) == ["Today", "Yesterday", "Oct 1, 2026"])
        #expect(days[0].entries.count == 2)
    }

    @Test("The Conversation Tags Each Turn With Its Sub-Goal And Filters By It")
    func conversationTags() throws {
        func event(_ id: Int, _ work: String, _ kind: String, _ body: String, decision: String = "null") -> String {
            let quoted = String(data: try! JSONEncoder().encode(body), encoding: .utf8)!
            return #"{"id":\#(id),"work":"\#(work)","kind":"\#(kind)","body":\#(quoted),"at":"2026-10-12T01:\#(String(format: "%02d", id)):00Z","effective":null,"decision":\#(decision)}"#
        }
        func work(_ id: String, _ state: String, kind: String = "task", parent: String? = "g") -> String {
            workJSON(state: state)
                .replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
                .replacingOccurrences(of: #""title":"T""#, with: #""title":"\#(id.uppercased())""#)
                .replacingOccurrences(of: #""kind":"task""#, with: #""kind":"\#(kind)""#)
                .replacingOccurrences(of: #""parent":null"#, with: parent.map { #""parent":"\#($0)""# } ?? #""parent":null"#)
        }
        func node(_ w: String, children: [String] = []) -> String {
            #"{"work":\#(w),"group":false,"done_when":null,"after":[],"waiting_on":[],"why_not_moving":null,"owner":null,"planned_into":null,"progress":null,"doing":null,"steps":[],"judged":null,"needs_you":0,"children":[\#(children.joined(separator: ","))]}"#
        }
        let director = #"{"question":"q","answer":"no","source":"director","runner":"","model":"","tokens":0,"reverses":0,"reversed_by":0}"#
        let events = [
            event(1, "g", "decision", "keep it small"),
            event(2, "a", "state", "done"),
            event(3, "a", "judged", "met: it is tagged"),
            event(4, "b1", "question", "Down scripts?"),
            event(5, "b1", "answer", "no", decision: director),
            event(6, "g", "supervised", "All fits."),
            event(7, "b1", "report", "DONE\nSTATUS: DONE"),
        ].joined(separator: ",")
        let tree = [node(work("a", "done")), node(work("b", "running", kind: "goal"), children: [node(work("b1", "running", parent: "b"))])].joined(separator: ",")
        let json = """
        {"goal":\(work("g", "running", kind: "goal", parent: nil)),"tasks":[\(work("a", "done"))],
         "rollup":{"counts":{},"done":1,"total":2},"events":[\(events)],"claims":[],"landings":[],"delivery":null,
         "activity":{},"reports":{},"waiting":[],
         "chain":[],"tree":[\(tree)],"paths":{"a":"A","b":"B","b1":"B › B1"},"done_when":null,"finish":null,
         "progress":{"done":1,"total":2,"superseded":0},"not_done_because":[],"needs_you":[],"course":null,"superseded":[],"proposed":false}
        """
        let goal = try JSONDecoder().decode(GoalDetail.self, from: Data(json.utf8))
        #expect(goal.filters.map(\.label) == ["Everything", "You and the director", "A", "B"])
        #expect(GoalDetail.chip("Whoever deploys gets a guide for each release") == "Whoever deploys gets a guid…", "a long title is cut to 28 characters")
        let reshapes = ["added 1a2b3c4d: X", "done when: y", "dropped 1a2b3c4d: no", "edited 1a2b3c4d: title", "1a2b3c4d waits on: 9f9f9f9f", "finishes at: merged (from goal)"]
        for body in reshapes {
            let e = Event(id: 1, work: "g", kind: "decision", body: body, at: "", effective: nil, decision: nil)
            #expect(e.reshapes, "\(body) belongs to the record, not the conversation")
        }
        #expect(!Event(id: 1, work: "g", kind: "decision", body: "keep it small", at: "", effective: nil, decision: nil).reshapes)
        #expect(goal.tag(of: "b1") == "B › B1")
        #expect(goal.tag(of: "g") == nil, "the goal's own turns carry no tag")
        #expect(goal.items(.subGoal("b")).map(\.id) == [4, 7], "a sub-goal's filter holds what was said under it")
        #expect(goal.items(.subGoal("a")).map(\.id) == [2, 3])
        #expect(goal.items(.yours).map(\.id) == [1, 4, 6], "you, the director's turns, and what the director answered")
        #expect(goal.items(.all).map(\.id) == [1, 2, 3, 4, 6, 7], "a sub-goal finishing is a line of its own, not folded")
        guard case .step(.exchange(_, let answer?)) = goal.items(.all)[3] else { Issue.record("no exchange"); return }
        #expect(answer.answeredByDirector, "an agent's question and the director's answer read as one")
        #expect(goal.addressees.map(\.id) == ["g", "b", "b1"], "the whole goal, then each open sub-goal")
        #expect(GoalView.allCases.filter(\.composes) == [.conversation], "Overview and Record have no composer")
        #expect(try Plan.send("b1", text: "use forward-only").argv.prefix(2) == ["send", "b1"])
    }

    @Test("A Change Waiting For The Person Shows What Would Change")
    func proposalShown() async throws {
        let home = try Home()
        let top = try home.goal("demo", "An inspector can get a full audit trail")
        let keeps = try home.sub(top, "Every record change keeps who and why")
        let export = try home.sub(top, "An admin can export the trail", "--after", keeps)
        let locked = try home.sub(top, "The trail cannot be edited", "--after", keeps)
        let checklist = try home.sub(top, "A checklist maps to the export", "--after", "\(export),\(locked)")
        try home.sql("INSERT INTO event (work,kind,body,at) VALUES ('\(top)','note','proposed: 4 sub-goals','2026-10-13T10:00:00Z');")
        let store = await Store(channel: try home.channel())
        let goal = try await store.goal(top)
        let rows = try #require(goal.proposal)
        #expect(rows.map(\.number) == [1, 2, 3, 4])
        #expect(rows.map(\.node.id) == [keeps, export, locked, checklist])
        #expect(rows[0].chips == ["Starts first", "One agent"])
        #expect(rows[3].chips == ["After 2 and 3", "One agent"])
        #expect(rows[1].node.whyNotMoving == "proposed — needs approval")

        let edit = try Plan.editGoal(export, doneWhen: "the export is a signed PDF and a CSV")
        #expect(edit.argv == ["goal", "edit", export, "--done-when", "the export is a signed PDF and a CSV"])
        #expect(throws: PlanError.self) { try Plan.editGoal(export) }
        #expect(try await store.run(edit).code == 0)
        #expect(try await store.run(Plan.splitGoal(keeps)).code == 0)
        #expect(try await store.run(try Plan.drop(locked, why: "the export is read-only already")).code == 0)
        let after = try await store.goal(top)
        let changed = try #require(after.proposal)
        #expect(changed.first { $0.id == export }?.node.doneWhen == "the export is a signed PDF and a CSV")
        #expect(changed.last?.node.work.title == "Every record change keeps who and why", "a split sub-goal becomes a goal of its own")
        #expect(changed.last?.chips == ["Will be planned further"])
        #expect(changed.first { $0.id == export }?.chips == ["After \(changed.count)", "One agent"], "what waited on it waits on the goal it became")
        #expect(!changed.contains { $0.id == locked }, "a dropped sub-goal leaves the proposal")
        #expect(Plan.approve(top).argv == ["goal", "approve", top])

        try home.sql("INSERT INTO event (work,kind,body,at) VALUES ('\(top)','decision','approved: 3 sub-goals','2026-10-13T10:05:00Z');")
        #expect(try await store.goal(top).proposal == nil, "an approved breakdown is no longer a proposal")

        let why = try home.goal("demo", "Why does sync drop events?")
        let found = try home.sub(why, "The cause is found and explained")
        try home.sql("""
        UPDATE work SET state='needs-input', finish_line='discovery', finish_source='goal', done_when='the cause is found' WHERE id='\(why)';
        INSERT INTO event (work,kind,body,at,payload) VALUES ('\(why)','question','The cause is found and the fix is small. Turn this into fixing it?
          Now: Why does sync drop events? | done when: the cause is found | finish: discovery
          Proposed: Sync never drops events | done when: a 429 batch is retried | finish: complete PR
          Why: knowing is done and the fix is one retry policy
        - Change course (recommended)
        - Keep it a discovery','2026-10-13T11:40:00Z','{"title":"Sync never drops events","done_when":"a 429 batch is retried","finish":"complete PR"}');
        UPDATE work SET state='needs-input' WHERE id='\(found)';
        INSERT INTO event (work,kind,body,at) VALUES ('\(found)','question','Where does this goal finish?
        - research (recommended)
        - complete PR','2026-10-13T11:41:00Z');
        """)
        let pivot = try await store.goal(why)
        let course = try #require(pivot.needsYou.first { $0.work == why }?.change)
        #expect(course.kind == .course)
        #expect(course.heading == "Course change on the top goal · always yours to decide")
        #expect(course.table.map(\.label) == ["Goal", "Done when", "Finishes at"])
        #expect(course.table[0].now == "Why does sync drop events?" && course.table[0].proposed == "Sync never drops events")
        #expect(course.table[2].now == "Discovery" && course.table[2].proposed == "Complete PR")
        #expect(course.why == "knowing is done and the fix is one retry policy")
        let item = try #require(pivot.needsYou.first { $0.work == why }?.question.items.first)
        #expect(item.head == "The cause is found and the fix is small. Turn this into fixing it?", "the card asks only the question; the table says the rest")
        let finish = try #require(pivot.needsYou.first { $0.work == found }?.change)
        #expect(finish.kind == .finish)
        #expect(finish.choices == ["research (recommended)", "complete PR"])
        #expect(FinishMeaning.of("research")?.title == "Research complete: a written comparison and a pick")
        #expect(FinishMeaning.of("complete PR")?.title == "A complete PR")
        #expect(["discovery", "research", "local build", "complete PR", "merged", "released"].allSatisfy { FinishMeaning.of($0) != nil })
        #expect(AnswerDraft(try #require(pivot.needsYou.first { $0.work == found }?.question)).directorDecides == ["research"],
                "answering goes through the question's own answer path")
    }
}
