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
let unplanned = #""progress":{"done":0,"total":0,"superseded":0},"needs_you":0,"finish":null,"finished":null,"proposed":false"#

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

    @Test("Wd Runs With The Person's Login Shell PATH")
    func wdRunsWithLoginShellPath() async throws {
        #expect(LoginPath.parse("Last login: today\n__WD_PATH__/a:/b__WD_PATH__\nbye") == "/a:/b")
        #expect(LoginPath.parse("no markers here") == nil)
        #expect(LoginPath.parse("__WD_PATH____WD_PATH__") == nil)
        #expect(LoginPath.merge("/a:/b", "/b:/c::/a") == "/a:/b:/c")
        #expect(LoginPath.path(login: "/login:/usr/bin", inherited: "/usr/bin:/bin", home: "/h") { _ in true } == "/login:/usr/bin:/bin")
        let present: Set = ["/h/.opencode/bin", "/h/.local/bin", "/usr/local/bin", "/h/.npm-global/bin"]
        #expect(LoginPath.path(login: nil, inherited: "/usr/bin:/bin:/usr/local/bin", home: "/h") { present.contains($0) }
            == "/usr/bin:/bin:/usr/local/bin:/h/.local/bin:/h/.opencode/bin:/h/.npm-global/bin")
        #expect(LoginPath.shell(environment: ["SHELL": "/bin/fish"], account: "/bin/bash") == "/bin/fish")
        #expect(LoginPath.shell(environment: [:], account: "/bin/bash") == "/bin/bash")
        #expect(LoginPath.shell(environment: [:], account: nil) == "/bin/zsh")

        let home = try Home()
        let login = home.dir.appendingPathComponent("login-bin")
        try executable(login.appendingPathComponent("codex"), "#!/bin/sh\n")
        let shell = home.dir.appendingPathComponent("shell")
        // Answers only when run as an interactive login shell.
        try executable(shell, "#!/bin/sh\necho 'welcome back'\n[ \"$1 $2\" = '-l -i' ] && printf '__WD_PATH__\(login.path):/usr/bin__WD_PATH__'\necho 'rc noise'\n")
        var finder = home.environment
        finder["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin"
        finder["SHELL"] = shell.path
        let env = LoginPath.resolve(environment: finder)
        #expect(env["PATH"] == "\(login.path):/usr/bin:/bin:/usr/sbin:/sbin")
        #expect(env.filter { $0.key != "PATH" } == finder.filter { $0.key != "PATH" }, "nothing else changes")
        let store = await Store(channel: try Channel(wd: builtWD, environment: env))
        #expect(try await store.doctor().runners.contains { $0.runner == "codex" && $0.detected }, "wd finds a runner only the login shell's PATH has")
        await store.stop()

        try executable(shell, "#!/bin/sh\nsleep 30\n")
        let started = Date()
        let late = LoginPath.resolve(environment: finder, timeout: 0.5)
        #expect(Date().timeIntervalSince(started) < 5)
        #expect(late["PATH"]?.hasPrefix("/usr/bin:/bin:/usr/sbin:/sbin") == true, "a shell that does not answer falls back to the inherited PATH")
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

    @Test("A Goal Opened Again Shows As Last Read While It Is Read Again")
    func goalOpenedAgainShowsAsLastRead() async throws {
        let home = try Home()
        let goal = try home.goal("demo", "Ship the export")
        let store = await Store(channel: try home.channel())
        #expect(await store.lastRead(goal: goal) == nil, "nothing is shown as read before the goal has been")
        #expect(await store.lastRead(gate: goal) == nil)

        let first = try await store.goal(goal)
        let gate = try await store.gate(of: goal)
        #expect(await store.lastRead(goal: goal) == first)
        #expect(await store.lastRead(gate: goal) == gate)

        try home.wd("rename", goal, "Ship the CSV export")
        #expect(await store.lastRead(goal: goal)?.goal.title == "Ship the export", "until it is read again, it shows as it was")
        let again = try await store.goal(goal)
        #expect(again.goal.title == "Ship the CSV export")
        #expect(await store.lastRead(goal: goal) == again, "each read replaces the one kept")
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
        #expect(ControlName.option("Yes (recommended)", finish: false) == "Yes", "an option is announced by the title it shows")
        #expect(ControlName.option("research (recommended)", finish: true) == "Research complete: a written comparison and a pick")
        #expect([ControlName.answer, ControlName.directorDecides] == ["Answer", "Let the director decide"])

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
            event(14, "a", "sent", "hold off until the release ships", "10:30"),
            event(15, "a", "note", "replied: Understood, holding until the release ships.", "10:45"),
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
        #expect(steps.map(\.id) == [1, 3, 4, 5, 7, 8, 11, 14, 15], "oldest first; a state change that does not end work, a spawn and an answer's message are left out")
        guard case .exchange(_, let auto?) = steps[5] else { Issue.record("step 6 is \(steps[5])"); return }
        #expect(auto.id == 9 && auto.answeredByDirector && !goal.isYours(auto))
        guard case .exchange(let asked, let mine?) = steps[6] else { Issue.record("step 7 is \(steps[6])"); return }
        #expect(asked.askedItems.map(\.question) == ["Tag v1.0.0?"] && asked.askedItems.first?.choices == ["yes (recommended)", "no"])
        #expect(mine.id == 12 && !mine.answeredByDirector)
        #expect(goal.yours.map(\.id) == [1, 12])
        guard case .stopped(_, let said) = steps[1] else { Issue.record("step 2 is \(steps[1])"); return }
        #expect(said == "wiring belongs to the collector")
        guard case .replied(_, let replied) = steps[8] else { Issue.record("step 9 is \(steps[8])"); return }
        #expect(replied == "Understood, holding until the release ships.")
        guard case .look(let look) = steps[2] else { Issue.record("step 3 is \(steps[2])"); return }
        #expect(look.body == "a task was noted on (1)")
        guard case .supervisor(_, let reply) = steps[3] else { Issue.record("step 4 is \(steps[3])"); return }
        #expect(reply.said == "The collector never calls the detector.", "the action lines are not said twice")
        #expect(reply.actions.map(\.task) == ["a", "n", nil], "each action names the task it told or filed")
        #expect(reply.refused == ["START b — b is review, already under way"])

        let open = event(13, "b", "question", "which bucket?", "11:00")
        let waiting = #"[{"task":"b","question":{"id":"event-13","items":[{"header":"","question":"which bucket?","multi":false,"options":[]}],"answers":null,"reply":null}}]"#
        let pending = try JSONDecoder().decode(GoalDetail.self, from: Data(json
            .replacingOccurrences(of: "\(events)]", with: "\(events),\(open)]")
            .replacingOccurrences(of: #""waiting":[]"#, with: #""waiting":\#(waiting)"#).utf8))
        guard case .exchange(let question, nil) = pending.thread.last else { Issue.record("the open question is the last step"); return }
        #expect(pending.answeredInCard(question), "an open question a card answers is not shown as a turn too")
        #expect(!goal.answeredInCard(question), "with no card for it, it shows as a turn")
        #expect(pending.waitingTasks.map { $0.task.title } == ["Collector"], "its card names who asks")
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
        #expect(roster.finished == "2 of 5 finished", "subagents count among the cards")
        #expect(roster.spent == "1k tokens")
        #expect(roster.cards[0].spent == "m · 100 tokens · 1 tool call")
        #expect(roster.cards[0].tag == nil && roster.cards[0].subagents[0].tag == "fork", "only a subagent is tagged, with its type")
        let idle = AgentRoster(tasks: [a], activity: ["a": try activity("running", usage: #"{"model":null,"tokens":0,"tools":0,"started":null,"last":null}"#)])
        #expect(idle.cards[0].spent == nil && idle.spent == nil, "nothing spent shows nothing")
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
                  done: Int = 0, total: Int = 0, needsYou: Int = 0, finished: String = "null", proposed: Bool = false) -> String {
            #"{"work":\#(work(id, state, project: project, updated: updated)),"rollup":{"counts":{},"done":0,"total":0},"band":"\#(band)","progress":{"done":\#(done),"total":\#(total),"superseded":0},"needs_you":\#(needsYou),"finish":{"line":"discovery","source":"goal"},"finished":\#(finished),"proposed":\#(proposed)}"#
        }
        let task = workJSON(state: "running").replacingOccurrences(of: #""id":"w1""#, with: #""id":"fix""#).replacingOccurrences(of: #""updated":"u""#, with: #""updated":"2026-10-05""#)
        let board = try JSONDecoder().decode(Board.self, from: Data("""
        {"goals":[
          \(goal("old", "running", "in-flight", updated: "2026-10-01", done: 1, total: 4)),
          \(goal("asks", "needs-input", "needs-you", updated: "2026-10-09", done: 1, total: 4, needsYou: 1)),
          \(goal("planned", "queued", "needs-you", updated: "2026-10-08T12", total: 4, proposed: true)),
          \(goal("below", "running", "in-flight", updated: "2026-10-07", needsYou: 2)),
          \(goal("later", "queued", "in-flight", updated: "2026-10-08")),
          \(goal("shipped", "done", "at-rest", updated: "2026-10-10", done: 2, total: 2, finished: #""Complete PR #512""#)),
          \(goal("gone", "dropped", "at-rest", updated: "2026-10-10")),
          \(goal("elsewhere", "running", "in-flight", project: "other", updated: "2026-10-11"))
        ],"standalone":[{"work":\(task),"band":"in-flight"}]}
        """.utf8))
        let bands = board.sidebar(project: "demo")
        #expect(bands.needsYou.map(\.id) == ["asks", "planned", "below"], "a question below a goal, or a breakdown to approve, puts it in Needs you")
        let planned = bands.needsYou[1]
        #expect(planned.proposedBadge == "Proposed" && planned.meta == "4 sub-goals")
        #expect(bands.needsYou[0].proposedBadge == nil)
        #expect(bands.working.map(\.id) == ["later", "fix", "old"])
        #expect(bands.completed.map(\.id) == ["shipped"])
        #expect(bands.letGo.map(\.id) == ["gone"])
        #expect(board.sidebar().working.first?.id == "elsewhere")

        #expect(bands.needsYou[0].meta == "1 of 4")
        #expect(bands.needsYou[0].needsYouBadge == "1 needs you")
        #expect(bands.needsYou[2].needsYouBadge == "2 needs you")
        #expect(bands.completed[0].meta == "Complete PR #512")
        #expect(bands.working[0].meta == "Discovery", "an unplanned goal says where it finishes")
        #expect(bands.working[2].needsYouBadge == nil)

        let chain = [ChainLink(id: "asks", title: "Release notes", state: .needsInput), ChainLink(id: "g2", title: "Whoever deploys gets a guide", state: .needsInput)]
        #expect(bands.needsYou[0].openPath(chain).map(\.id) == ["g2"])
        #expect(bands.needsYou[2].openPath(chain).isEmpty)
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
        let gate = try await store.gate(of: id)
        #expect(gate.label == "Check it", "wd serves the button's words")
        let verify = try #require(Plan.passing(gate))
        #expect(verify.argv == ["verify", id])
        #expect(verify.title == "Check it", "named in plain words")
        let served = try JSONDecoder().decode(Gate.self, from: Data(#"{"work":"w","gate":"land","title":"Land","label":"Ship it","argv":["pr","w"]}"#.utf8))
        #expect(Plan.passing(served)?.title == "Ship it", "the words wd serves win")
    }

    @Test("A Notification Says What Happened And Where")
    func notificationSaysWhatHappened() throws {
        func work(_ id: String, _ title: String, _ state: String, kind: String = "goal", updated: String = "u0") -> [String: Any] {
            ["id": id, "project": "demo", "title": title, "detail": "", "kind": kind, "state": state,
             "runner": NSNull(), "session": NSNull(), "ref": NSNull(), "cwd": NSNull(), "created": "c", "updated": updated,
             "parent": NSNull(), "heading": NSNull(), "claim": NSNull(), "impact": NSNull(), "goal_type": NSNull(), "archived": NSNull()]
        }
        func row(_ w: [String: Any], _ band: String, needsYou: Int = 0, total: Int = 0, finished: String? = nil, proposed: Bool = false) -> [String: Any] {
            ["work": w, "rollup": ["counts": [String: Int](), "done": 0, "total": total], "band": band,
             "progress": ["done": 0, "total": total, "superseded": 0], "needs_you": needsYou, "finish": NSNull(),
             "finished": finished ?? NSNull(), "proposed": proposed]
        }
        func node(_ w: [String: Any], _ children: [[String: Any]] = [], stopped: [String: Any]? = nil, replied: [String: Any]? = nil, because: String? = nil) -> [String: Any] {
            ["work": w, "group": false, "done_when": NSNull(), "after": [String](), "waiting_on": [String](), "why_not_moving": NSNull(),
             "owner": NSNull(), "planned_into": NSNull(), "progress": NSNull(), "doing": NSNull(), "steps": [Any](), "judged": NSNull(),
             "needs_you": 0, "stopped": stopped ?? NSNull(), "replied": replied ?? NSNull(), "blocked_because": because ?? NSNull(), "children": children]
        }
        func ask(_ id: String, on work: String, _ text: String, change: [String: Any]? = nil) -> [String: Any] {
            ["work": work, "path": work, "change": change ?? NSNull(),
             "question": ["id": id, "items": [["header": "", "question": text, "multi": false,
                                               "options": [["label": "Yes", "description": ""]]]], "answers": NSNull(), "reply": NSNull()]]
        }
        func detail(_ w: [String: Any], tree: [[String: Any]] = [], asks: [[String: Any]] = []) -> [String: Any] {
            ["goal": w, "tasks": [Any](), "rollup": ["counts": [String: Int](), "done": 0, "total": 0], "events": [Any](), "claims": [Any](),
             "landings": [Any](), "delivery": NSNull(), "activity": [String: Any](), "asking": NSNull(), "reports": [String: Any](),
             "waiting": [Any](), "chain": [Any](), "tree": tree, "paths": [String: String](), "done_when": NSNull(), "finish": NSNull(),
             "progress": ["done": 0, "total": 0, "superseded": 0], "not_done_because": [String](), "needs_you": asks, "course": NSNull(),
             "superseded": [Any](), "proposed": false, "reasoning": NSNull()]
        }
        func sighting(_ goals: [[String: Any]], _ standalone: [[String: Any]], _ details: [String: [String: Any]]) throws -> Sighting {
            func decode<T: Decodable>(_ v: Any) throws -> T { try JSONDecoder().decode(T.self, from: JSONSerialization.data(withJSONObject: v)) }
            return Sighting(board: try decode(["goals": goals, "standalone": standalone]), goals: try details.mapValues { try decode($0) })
        }

        let export = work("g", "Ship the export", "running"), csv = work("s", "Export as CSV", "running")
        let audit = work("p", "Audit trail", "running"), sync = work("d", "Fix sync", "running")
        let search = work("r", "Speed up search", "running"), why = work("c", "Why does sync drop events?", "running")
        let keys = work("t", "Rotate keys", "running", kind: "task")
        let before = try sighting(
            [row(export, "in-flight"), row(audit, "in-flight", total: 3), row(sync, "in-flight"), row(search, "in-flight"), row(why, "in-flight")],
            [row(keys, "in-flight")],
            ["g": detail(export, tree: [node(csv), node(work("x", "Write the README", "running", kind: "task"))]), "p": detail(audit), "d": detail(sync), "r": detail(search), "c": detail(why)])

        let exportNow = work("g", "Ship the export", "needs-input", updated: "u1"), whyNow = work("c", "Why does sync drop events?", "needs-input", updated: "u1")
        let course: [String: Any] = ["kind": "course", "proposed": ["title": "Sync never drops events", "done_when": NSNull(), "finish": NSNull()]]
        let after = try sighting(
            [row(exportNow, "needs-you", needsYou: 2), row(audit, "needs-you", total: 3, proposed: true),
             row(work("d", "Fix sync", "done", updated: "u1"), "at-rest", finished: "Complete PR #512"),
             row(work("r", "Speed up search", "review", updated: "u1"), "ready-to-push"), row(whyNow, "needs-you", needsYou: 3)],
            [row(work("t", "Rotate keys", "blocked", kind: "task", updated: "u1"), "needs-you")],
            ["g": detail(exportNow, tree: [node(work("s", "Export as CSV", "needs-input"),
                                                [node(work("b", "Upload to S3", "blocked", kind: "task"), because: "the bucket's keys expired")]),
                                           node(work("x", "Write the README", "running", kind: "task"),
                                                stopped: ["said": "I ran out of room before the examples\nthen more", "at": "t1"])],
                         asks: [ask("q1", on: "s", "Which columns go in?\nThe export has forty."), ask("q2", on: "g", "Is UTF-8 fine?")]),
             "p": detail(audit), "r": detail(work("r", "Speed up search", "review")),
             "c": detail(whyNow, asks: [ask("q3", on: "c", "Turn this into fixing it?", change: course),
                                        ask("q4", on: "f", "Where does this goal finish?", change: ["kind": "finish"]),
                                        ask("q5", on: "c", "Add a retry sub-goal?", change: ["kind": "plan"])])])

        #expect(Notice.between(nil, after).isEmpty, "nothing is told from the first read")
        let told = Notice.between(before, after)
        #expect(told.map { [$0.title, $0.body, $0.work] } == [
            ["Question for you", "Ship the export\nWhich columns go in?\n1 more question", "g"],
            ["Blocked", "Ship the export — Upload to S3\nThe bucket's keys expired", "g"],
            ["Stopped without finishing", "Ship the export — Write the README\nI ran out of room before the examples", "g"],
            ["Approve the plan", "Audit trail · 3 sub-goals", "p"],
            ["Finished", "Fix sync — Complete PR #512", "d"],
            ["Ready for review", "Speed up search", "r"],
            ["Change of course proposed", "Why does sync drop events? — Sync never drops events", "c"],
            ["Choose where it finishes", "Why does sync drop events?", "c"],
            ["Plan change to approve", "Why does sync drop events?", "c"],
            ["Blocked", "Rotate keys", "t"],
        ])
        #expect(Notice.between(after, after).isEmpty, "staying put is not news")
        func readme(_ stopped: [String: Any]?) throws -> Sighting {
            try sighting([row(export, "in-flight")], [],
                         ["g": detail(export, tree: [node(work("x", "Write the README", "running", kind: "task"), stopped: stopped)])])
        }
        let first = try readme(["said": "halfway", "at": "t1"]), again = try readme(["said": "halfway", "at": "t2"])
        #expect(Notice.between(try readme(nil), first).map(\.keys) == [["stopped:x:t1"]])
        #expect(Notice.between(first, again).map(\.body) == ["Ship the export — Write the README\nhalfway"], "a later stop is told again")
        #expect(Notice.between(first, try readme(nil)).isEmpty, "carrying on is not news")
        let waits = try sighting([row(export, "needs-you", needsYou: 1)], [],
                                 ["g": detail(export, tree: [node(work("x", "Write the README", "needs-input", kind: "task"), stopped: ["said": "Shall I add examples?", "at": "t3"])],
                                              asks: [ask("event-9", on: "x", "Shall I add examples?")])])
        #expect(Notice.between(try readme(nil), waits).map(\.title) == ["Stopped without finishing"], "a stop waiting on the person is not told again as a question")
        func answered(_ replied: [String: Any]?, _ state: String = "paused", asks: [[String: Any]] = []) throws -> Sighting {
            try sighting([row(export, "in-flight")], [],
                         ["g": detail(export, tree: [node(work("x", "Write the README", state, kind: "task"), replied: replied)], asks: asks)])
        }
        let reply = try answered(["said": "Understood, holding until the release ships.\nMore detail", "at": "t4"])
        #expect(Notice.between(try answered(nil), reply).map { [$0.title, $0.body] } == [["Replied", "Ship the export — Write the README\nUnderstood, holding until the release ships."]],
                "a reply to the person is told as a reply")
        #expect(Notice.between(reply, reply).isEmpty, "a reply is told once")
        let asking = try answered(["said": "Shall I drop the other branch too?", "at": "t5"], "needs-input",
                                  asks: [ask("event-12", on: "x", "Shall I drop the other branch too?")])
        #expect(Notice.between(try answered(nil), asking).map(\.title) == ["Replied"], "a reply that asks is not told again as a question")
        #expect(Notice.between(before, after, told: Set(told.flatMap(\.keys))).isEmpty, "nothing is told twice")
        #expect(!Notice.between(before, after, quiet: ["g", "q3"]).contains { $0.work == "g" || $0.body.contains("Sync never") },
                "what the person just did is not told back to them")

        let now = Date()
        let answered = try Plan.decide("g", text: "csv only")
        #expect(Notice.quiet(running: [], ended: [(answered, now.addingTimeInterval(-2))], now: now).contains("g"))
        #expect(!Notice.quiet(running: [], ended: [(answered, now.addingTimeInterval(-10))], now: now).contains("g"))
        #expect(Notice.quiet(running: [answered], ended: [], now: now).contains("g"))

        #expect(Sighting.reads(for: before.board, after: before, touched: []).read.isEmpty, "a goal nothing touched is not read again")
        #expect(Sighting.reads(for: before.board, after: before, touched: ["s"]).read == ["g"], "an event under a goal reads it again")
        #expect(Set(Sighting.reads(for: after.board, after: before, touched: []).read) == Set(["g", "p", "r", "c"]),
                "a goal whose row changed is read again; one at rest with nothing asked is not read")
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
        #expect(goal.notDoneBecause.first == "3 sub-goals are still open")
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
        #expect(goal.finishWords?.line == "A complete pull request")
        #expect(goal.finishWords?.source == "From your taste.")
        #expect(goal.course?.badge == GoalBadge("On course", .settled))
        #expect(goal.progressLine == "1 of 4 done")
        #expect(goal.doneWhenSentence == "Every release has a guide and a note", "the Done when card starts with a capital")
        #expect(goal.course?.why.sentence == "The tag format suits the rest")

        let ask = try #require(goal.needsYou.first)
        #expect(goal.pathParts(of: ask).map(\.title) == ["Whoever deploys gets a guide", "The guide says how to roll back"])
        #expect(goal.pathParts(of: ask).map(\.opens) == [t.guide, nil], "a level that is a goal opens its page")

        let rows = Dictionary(uniqueKeysWithValues: goal.tree.map { ($0.id, $0) })
        let tagged = try #require(rows[t.tagged]), guide = try #require(rows[t.guide])
        let note = try #require(rows[t.note]), agent = try #require(rows[t.agent])
        #expect(tagged.mark == .done && tagged.badge == GoalBadge("Done", .settled))
        #expect(tagged.meta() == "Done when Every release is tagged is checked · claude · 2 steps · Checked: done")
        #expect(guide.mark == .needsYou && guide.badge == GoalBadge("1 needs you", .attention))
        #expect(guide.meta() == "1 sub-goal · 0 of 1 done · 1 replaced", "replaced ones are counted apart")
        #expect(guide.opensAsGoal && !guide.folds, "a goal row opens its page")
        #expect(note.mark == .working && note.badge == GoalBadge("Working · 1 of 3", .live), "dropped steps are not counted")
        #expect(note.meta() == "claude · Now: Write the template")
        #expect(note.folds, "a task row folds its steps")
        #expect(agent.mark == .waiting && agent.badge == GoalBadge("Waiting", .waiting))
        #expect(agent.meta() == "Waits on: Whoever deploys gets a guide")
        #expect(goal.openSteps == t.note, "the first working row starts open")

        let nested = try await store.goal(t.guide)
        let wiki = try #require(nested.tree.first { $0.id == t.wiki })
        #expect(wiki.mark == .superseded)
        #expect(wiki.meta(superseded: nested.superseded) == "Replaced by a change of plan: guides live with each release")
        let rollback = try #require(nested.tree.first { $0.id == t.rollback })
        #expect(rollback.meta() == "codex · waits for you")
        #expect(nested.progressLine == "0 of 1 done · 1 replaced")
        #expect(goal.notDoneBecause.count == 3, "open sub-goals, the done-when and the finish line")

        #expect(goal.askedAt(ask.question) == "2026-10-13T12:14:00Z")
        let add = try Plan.addSubGoal(t.top, outcome: " Notes are translated ", doneWhen: "each note has a French copy")
        #expect(add.argv == ["goal", "add", t.top, "Notes are translated", "--done-when", "each note has a French copy"])
        #expect(throws: PlanError.self) { try Plan.addSubGoal(t.top, outcome: "Notes", doneWhen: " ") }
        #expect(try await store.run(add).code == 0)
        #expect(try await store.goal(t.top).tree.last?.work.title == "Notes are translated")

        try fakeRunner(t.home)
        let halted = try t.home.wd("add", "demo", "Tag every release again")
        let stuck = try t.home.wd("add", "demo", "List the flags")
        try t.home.sql("""
        UPDATE work SET parent='\(t.top)', state='running', runner='fake', session='s4' WHERE id='\(halted)';
        UPDATE work SET parent='\(t.top)', state='blocked' WHERE id='\(stuck)';
        INSERT INTO event (work,kind,body,at) VALUES ('\(halted)','note','stopped without a report: I ran out of room before tagging v2','2026-10-13T12:20:00Z');
        INSERT INTO event (work,kind,body,at) VALUES ('\(stuck)','report','BLOCKED
        STATUS: BLOCKED the flag list lives in another repository','2026-10-13T12:21:00Z');
        """)
        let served = Dictionary(uniqueKeysWithValues: try await store.goal(t.top).tree.map { ($0.id, $0) })
        let stopped = try #require(served[halted]), blocked = try #require(served[stuck])
        #expect(stopped.stopped?.said == "I ran out of room before tagging v2")
        #expect(stopped.meta() == "fake · Stopped without finishing: I ran out of room before tagging v2")
        let carryOn = Plan.carryOn(halted)
        #expect(carryOn.title == "Tell it to carry on" && carryOn.argv == ["send", halted, "Carry on where you left off."])
        #expect(blocked.blockedBecause == "the flag list lives in another repository")
        #expect(blocked.meta() == "The flag list lives in another repository", "a blocked row says why")
        let sent = try await store.run(carryOn)
        #expect(sent.code == 0, "\(sent.stdout) \(sent.stderr)")
        #expect(try await store.goal(t.top).tree.first { $0.id == halted }?.stopped == nil, "told to carry on, it no longer reads as stopped")

        let here = try await store.record(t.guide)
        #expect(here.entries.contains { $0.kind == "chore" && $0.by == "director" }, "a nested page's record holds what the director did there")
        #expect(here.entries.first { $0.kind == "chore" }?.sentence == "Housekeeping: refreshed dependencies")
        #expect(try await store.record(t.top).entries.first { $0.kind == "course" }?.text == "The tag format suits the rest", "ledger text in a sentence's place starts with a capital")
    }

    @Test("A Sub-Goal Reads As Working Only While An Agent Works On It")
    func subGoalWorkingOnlyWhileAnAgentWorks() throws {
        func work(_ id: String, _ state: String, kind: String = "task") -> String {
            workJSON(state: state)
                .replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
                .replacingOccurrences(of: #""title":"T""#, with: #""title":"\#(id)""#)
                .replacingOccurrences(of: #""kind":"task""#, with: #""kind":"\#(kind)""#)
        }
        func node(_ w: String, group: Bool = false, steps: Bool = false, needsYou: Int = 0, stopped: String = "null", children: [String] = []) -> String {
            let planned = steps ? #"[{"n":1,"text":"Write it","state":"done"},{"n":2,"text":"Test it","state":"done"}]"# : "[]"
            return #"{"work":\#(w),"group":\#(group),"done_when":null,"after":[],"waiting_on":[],"why_not_moving":null,"owner":null,"planned_into":null,"progress":null,"doing":null,"steps":\#(planned),"judged":null,"needs_you":\#(needsYou),"stopped":\#(stopped),"blocked_because":null,"children":[\#(children.joined(separator: ","))]}"#
        }
        let stop = #"{"said":"Only part of the suite has run.\nShall I run all three?","at":"t1"}"#
        let tree = [
            node(work("finished", "review", kind: "goal"), group: true, children: [node(work("a", "review", kind: "task"), steps: true), node(work("b", "review"))]),
            node(work("asked", "needs-input", kind: "goal"), group: true, needsYou: 1, children: [node(work("c", "needs-input"), needsYou: 1, stopped: stop)]),
            node(work("busy", "running"), steps: true),
            node(work("idle", "running")),
        ].joined(separator: ",")
        let json = """
        {"goal":\(work("g", "running", kind: "goal")),"tasks":[],"rollup":{"counts":{},"done":0,"total":0},"events":[],"claims":[],"landings":[],
         "delivery":null,"activity":{"busy":{"status":"running"},"idle":{"status":"idle"},"c":{"status":"idle"},"a":{"status":"stopped"}},
         "reports":{},"waiting":[],"chain":[],"tree":[\(tree)],"paths":{},"done_when":null,"finish":null,
         "progress":{"done":0,"total":0,"superseded":0},"not_done_because":[],"needs_you":[],"course":null,"superseded":[],"proposed":false}
        """
        let goal = try JSONDecoder().decode(GoalDetail.self, from: Data(json.utf8))
        let rows = Dictionary(uniqueKeysWithValues: goal.nodes.map { ($0.id, $0) })
        let finished = try #require(rows["finished"]), a = try #require(rows["a"]), asked = try #require(rows["asked"])
        let c = try #require(rows["c"]), busy = try #require(rows["busy"])
        #expect(finished.badge == GoalBadge("To review", .live), "a group whose tasks all wait on review reads To review")
        #expect(a.badge == GoalBadge("To review", .live), "steps are counted only while it is working")
        #expect(busy.badge == GoalBadge("Working · 2 of 2", .live))
        #expect(asked.badge == GoalBadge("Needs you", .attention) && asked.mark == .needsYou)
        #expect(c.badge == GoalBadge("Needs you", .attention) && c.mark == .needsYou)
        #expect(c.meta() == "Stopped without finishing: Only part of the suite has run.")
        #expect(c.stopped != nil, "it offers Tell it to carry on")
        #expect(Plan.carryOn("c").argv == ["send", "c", "Carry on where you left off."])
        #expect(goal.isWorking("busy"), "a session that runs shows the moving mark")
        #expect(!goal.isWorking("idle") && !goal.isWorking("c") && !goal.isWorking("finished") && !goal.isWorking("asked"),
                "no session runs there, so no moving mark")
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
        #expect(cards[1].number == 1 && cards[1].label == "direction check · 0 corrections")
        #expect(cards[3].label == "housekeeping task the director ran")
        #expect(cards[4].label == "report checked")

        #expect(RecordKind.allCases.map(\.label) == ["Everything", "Decisions", "Direction checks", "Questions", "Reports", "Housekeeping", "Delivery"])
        #expect(record.entries(.everything) == record.entries, "everything, newest first, as served")
        #expect(record.entries(.course).map(\.kind) == ["course", "superseded"], "a replaced branch is a direction check")
        #expect(record.entries(.questions).map(\.kind) == ["question"])
        #expect(record.entries(.chores).map(\.text) == ["refreshed dependencies"])
        #expect(record.entries(.decisions).contains { $0.what == "Notes are written in English only" && $0.by == "you" })
        #expect(record.entries(.decisions).last?.kind == "asked", "the goal's first ask closes the record")

        #expect(goal.recordPlaces == ["Every release is tagged", "Whoever deploys gets a guide", "Customers get a release note", "Any agent can produce the notes"])
        let guide = record.entries(.everything, under: "Whoever deploys gets a guide")
        #expect(Set(guide.map(\.kind)).isSuperset(of: ["question", "chore", "superseded"]), "a sub-goal holds what happened under it")
        #expect(guide.allSatisfy { $0.where.hasPrefix("Whoever deploys gets a guide") })
        #expect(record.entries(.everything, under: "Every release is tagged").map(\.badge) .contains(GoalBadge("Checked: done", .settled)))

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
        #expect(rows[1].node.whyNotMoving == "Proposed, waiting for your approval")

        let edit = try Plan.editGoal(export, doneWhen: "the export is a signed PDF and a CSV")
        #expect(edit.argv == ["goal", "edit", export, "--done-when", "the export is a signed PDF and a CSV"])
        #expect(throws: PlanError.self) { try Plan.editGoal(export) }
        #expect(try await store.run(edit).code == 0)
        #expect(try await store.run(Plan.splitGoal(keeps)).code == 0)
        #expect(try await store.run(try Plan.drop(locked, why: "the export is read-only already")).code == 0)
        let after = try await store.goal(top)
        let changed = try #require(after.proposal)
        #expect(changed.first { $0.id == export }?.node.doneWhen == "the export is a signed PDF and a CSV")
        #expect(changed.first?.node.work.title == "Every record change keeps who and why", "a split sub-goal becomes a goal of its own")
        #expect(changed.first?.chips == ["Starts first", "Will be planned further"])
        #expect(changed.first { $0.id == export }?.chips == ["After 1", "One agent"], "what waited on it waits on the goal it became")
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
        #expect(course.table[2].now == "Discovery" && course.table[2].proposed == "A complete pull request")
        #expect(course.why == "knowing is done and the fix is one retry policy")
        let item = try #require(pivot.needsYou.first { $0.work == why }?.question.items.first)
        #expect(item.head == "The cause is found and the fix is small. Turn this into fixing it?", "the card asks only the question; the table says the rest")
        let finish = try #require(pivot.needsYou.first { $0.work == found }?.change)
        #expect(finish.kind == .finish)
        #expect(finish.choices == ["research (recommended)", "complete PR"])
        #expect(FinishMeaning.of("research")?.title == "Research complete: a written comparison and a pick")
        #expect(pivot.needsYou.first { $0.work == found }?.question.items.first?.asksFinish == true, "a finish-line question is known by what it asks, wherever it shows")
        #expect(pivot.needsYou.first { $0.work == why }?.question.items.first?.asksFinish == false)
        #expect(FinishMeaning.of("complete PR")?.title == "A complete pull request")
        #expect(["discovery", "research", "local build", "complete PR", "merged", "released"].allSatisfy { FinishMeaning.of($0) != nil })
        #expect(try Plan.answer(found, try #require(pivot.needsYou.first { $0.work == found }?.question), answers: ["research"]).argv == ["answer", found, "research"],
                "answering goes through the question's own answer path")
    }

    @Test("A Proposal Says Why It Is Broken Down So")
    func proposalReasoning() async throws {
        let home = try Home()
        let top = try home.goal("demo", "An inspector can get a full audit trail")
        _ = try home.sub(top, "Every record change keeps who and why")
        try home.sql("INSERT INTO event (work,kind,body,at) VALUES ('\(top)','note','proposed: 1 sub-goals','2026-10-13T10:00:00Z');")
        let store = await Store(channel: try home.channel())
        #expect(try await store.goal(top).reasoning == nil, "with no reasoning, nothing shows")
        try home.sql("""
        INSERT INTO event (work,kind,body,at,payload) VALUES ('\(top)','note','reasoning: why: three checks','2026-10-13T10:00:01Z',
          '{"why":"An inspector checks three things.","left_out":null,"judge_by":"Export last month for one org."}');
        """)
        let reasoning = try #require(try await store.goal(top).reasoning)
        #expect(reasoning.sections.map(\.label) == ["Why this breakdown", "How you'd judge it"], "a part the planner did not say is left out")
        #expect(reasoning.sections.map(\.text) == ["An inspector checks three things.", "Export last month for one org."])
    }

    @Test("A Proposal Is Reordered, Merged And Planned Again From Its Cards")
    func proposalReshaped() async throws {
        let home = try Home()
        let top = try home.goal("demo", "An inspector can get a full audit trail")
        let a = try home.sub(top, "Every record change keeps who and why")
        let b = try home.sub(top, "An admin can export the trail", "--after", a)
        let c = try home.sub(top, "The trail cannot be edited", "--after", a)
        let d = try home.sub(top, "A checklist maps to the export")
        try home.wd("goal", "after", a, d)
        try home.sql("INSERT INTO event (work,kind,body,at) VALUES ('\(top)','note','proposed: 4 sub-goals','2026-10-13T10:00:00Z');")
        let store = await Store(channel: try home.channel())
        var goal = try await store.goal(top)
        #expect(try #require(goal.proposal).map(\.id) == [d, a, b, c], "each after what it waits on, else as served")

        let moves = goal.reorder(moving: c, to: 1)
        #expect(moves.map(\.argv) == [["goal", "after", c, d], ["goal", "after", a, c]],
                "each waits on the one before it; only waits that change are written, from the top")
        #expect(goal.reorder(moving: d, to: 0).map(\.argv) == [["goal", "after", c, b]],
                "a drop in place still sets the order as shown")
        for plan in moves { #expect(try await store.run(plan).code == 0) }
        goal = try await store.goal(top)
        let rows = try #require(goal.proposal)
        #expect(rows.map(\.id) == [d, c, a, b])
        #expect(ControlName.card("Split", rows[1]) == "Split 2 · The trail cannot be edited", "a card's action is announced with its number and title")
        #expect(ControlName.card("Merge", rows[3], then: " with 3") == "Merge 4 · An admin can export the trail with 3")
        #expect(ControlName.card("Merge", rows[0], then: " with…") == "Merge 1 · A checklist maps to the export with…")
        #expect([ControlName.discard, ControlName.replan, ControlName.approve] == ["Discard", "Re-plan with a note…", "Approve and start"])
        #expect(goal.reorder(moving: a, to: 0).first?.argv == ["goal", "after", a, "none"], "the first waits on none")

        let merge = Plan.merge(rows[3], rows[2])
        #expect(merge.argv == ["goal", "merge", a, b], "the earlier card is kept")
        #expect(try await store.run(merge).code == 0)
        #expect(try #require(try await store.goal(top).proposal).map(\.id) == [d, c, a])

        let replan = try Plan.replan(top, note: "keep the export read-only")
        #expect(replan.map(\.argv) == [["decide", top, "keep the export read-only"], ["goal", "plan", top]])
        #expect(throws: PlanError.self) { try Plan.replan(top, note: "  ") }
    }

    @Test("A Person Changes Where A Top Goal Finishes")
    func finishChanged() async throws {
        let home = try Home()
        let top = try home.goal("demo", "Why does sync drop events?")
        let under = try home.sub(top, "The cause is found", "--split")
        let store = await Store(channel: try home.channel())
        #expect(FinishMeaning.lines == ["discovery", "research", "local build", "complete PR", "merged", "released"])
        #expect(FinishMeaning.lines.allSatisfy { FinishMeaning.of($0) != nil }, "each is named as a finish-line question names it")
        let plan = Plan.finish(top, "complete PR")
        #expect(plan.argv == ["goal", "finish", top, "complete PR"])
        #expect(try await store.run(plan).code == 0)
        let goal = try await store.goal(top)
        #expect(goal.finish?.line == "complete PR")
        #expect(goal.finishWords?.line == "A complete pull request")
        #expect(ControlName.finishMenu(goal.finishWords?.line) == "Finishes at: A complete pull request", "announced by the line it shows")
        #expect(ControlName.finishMenu(nil) == "Finishes at: Not decided yet")
        #expect(try await store.goal(under).chain.count > 1, "a sub-goal's page offers none")
        #expect(try await store.run(Plan.finish(under, "research")).code != 0)
    }

    @Test("A Course Change Shows What Was Found And What Happens To Each Sub-Goal")
    func courseChange() async throws {
        let home = try Home()
        try fakeRunner(home)
        /// A goal waiting on a course change that adds one sub-goal and supersedes another.
        func pivot() throws -> (goal: String, done: String, working: String, dropped: String) {
            let goal = try home.goal("demo", "Why does sync drop events?")
            let done = try home.sub(goal, "The cause is found and explained")
            let working = try home.sub(goal, "We know which tenants lost events")
            let dropped = try home.sub(goal, "Compare other sync vendors")
            let change = #"{"actions":["GOAL \#(goal): A rejected batch is retried until delivered | done when: a 429 batch is retried","SUPERSEDE \#(dropped): no longer needed: the vendor is fine once retried"],"title":"Sync never drops events","done_when":"a 429 batch is retried","finish":"complete PR","found":"The vendor SDK drops a batch on 429."}"#
            try home.sql("""
            UPDATE work SET state='done' WHERE id='\(done)';
            UPDATE work SET state='running' WHERE id='\(working)';
            UPDATE work SET state='needs-input', finish_line='discovery', finish_source='goal', done_when='the cause is found', runner='fake', session='s-sup' WHERE id='\(goal)';
            INSERT INTO event (work,kind,body,at,payload) VALUES ('\(goal)','question','Change course?
              Now: Why does sync drop events? | done when: the cause is found | finish: discovery
              Proposed: Sync never drops events | done when: a 429 batch is retried | finish: complete PR
              Found: The vendor SDK drops a batch on 429.
              Why: knowing is done and the fix is one retry policy
            - Change course (recommended)
            - Keep it a discovery','2026-10-13T11:40:00Z','\(change)');
            """)
            return (goal, done, working, dropped)
        }
        let store = await Store(channel: try home.channel())
        let first = try pivot()
        let ask = try #require(try await store.goal(first.goal).needsYou.first { $0.work == first.goal })
        let change = try #require(ask.change)
        #expect(change.found == "The vendor SDK drops a batch on 429.")
        #expect(change.why == "knowing is done and the fix is one retry policy")
        let fates = try #require(change.subGoals)
        #expect(fates.map(\.title) == ["The cause is found and explained", "We know which tenants lost events",
                                       "Compare other sync vendors", "A rejected batch is retried until delivered"])
        #expect(fates.map(\.badge.text) == ["Kept · done", "Kept · working", "Replaced", "New"])
        #expect(fates[2].why == "no longer needed: the vendor is fine once retried")
        let choices = try #require(ask.question.changeChoices)
        #expect(choices.change == "Change course" && choices.keep == "Keep it a discovery", "each button answers with its choice, labelled with it")
        #expect(ControlName.recommended(choices.change) == "Change course · Recommended", "its buttons are announced by their words")
        #expect(ControlName.editChange == "Edit the change…")
        #expect(try Plan.answer(first.goal, ask.question, answers: [choices.keep]).argv == ["answer", first.goal, "Keep it a discovery"])

        let proposed = try #require(change.proposed)
        let untouched = try Plan.editChange(first.goal, ask.question, proposed: proposed,
                                            title: "Sync never drops events", doneWhen: "a 429 batch is retried", finish: "complete PR")
        #expect(untouched.map(\.argv) == [["answer", first.goal, "Change course"]], "nothing is written for what was left as proposed")
        let edited = try Plan.editChange(first.goal, ask.question, proposed: proposed,
                                         title: "Sync never loses events", doneWhen: "a 429 batch is retried", finish: "merged")
        #expect(edited.map(\.argv) == [["answer", first.goal, "Change course"],
                                       ["goal", "edit", first.goal, "--title", "Sync never loses events"],
                                       ["goal", "finish", first.goal, "merged"]])
        for plan in edited {
            let r = try await store.run(plan)
            #expect(r.code == 0, "\(plan.commandLine): \(r.stderr)")
        }
        let after = try await store.goal(first.goal)
        #expect(after.goal.title == "Sync never loses events")
        #expect(after.doneWhen == "a 429 batch is retried")
        #expect(after.finish?.line == "merged")
        #expect(after.needsYou.isEmpty)
    }

    @Test("A Finish-Line Question Says Why It Asks And Can Be Remembered")
    func finishRemembered() async throws {
        let home = try Home()
        try fakeRunner(home)
        let store = await Store(channel: try home.channel())
        func asking(_ title: String, sources: String) throws -> String {
            let goal = try home.goal("demo", title)
            try home.sql("""
            UPDATE work SET state='needs-input', runner='fake', session='s-plan' WHERE id='\(goal)';
            INSERT INTO event (work,kind,body,at,payload) VALUES ('\(goal)','question','Where does this goal finish?
            - research (recommended)
            - complete PR','2026-10-13T14:02:00Z','\(sources)');
            """)
            return goal
        }
        let pick = try asking("Pick a queue library", sources: #"{"sources":{"goal":"\"compare and pick one\" reads as research","repo":null,"taste":"In auto mode, a complete PR is done"}}"#)
        let ask = try #require(try await store.goal(pick).needsYou.first)
        let sources = try #require(ask.change?.sources)
        #expect(sources.rows.map(\.label) == ["The goal", "Your taste"], "only the sources that said something, in the canvas's order")
        #expect(sources.rows.map(\.said) == ["\"compare and pick one\" reads as research", "In auto mode, a complete PR is done"])
        #expect(FinishMeaning.remember(in: "demo") == "Finish goals in demo this way from now on")

        let remembered = try Plan.answer(pick, ask.question, answers: ["research"], remember: true)
        #expect(remembered.argv == ["answer", pick, "research", "--remember"])
        #expect(try Plan.answer(pick, ask.question, answers: ["research"]).argv == ["answer", pick, "research"], "unchecked, nothing is remembered")
        let r = try await store.run(remembered)
        #expect(r.code == 0, "\(r.stderr)")
        let file = try String(contentsOf: home.dir.appendingPathComponent("projects/demo.md"), encoding: .utf8)
        #expect(file.contains("finish: research"))
        #expect(try await store.goal(pick).finish?.line == "research")
    }

    @Test("Letting The Director Decide Shows What It Decided Or Why It Declined")
    func directorDecides() async throws {
        #expect(Plan.directorDecides("g1").argv == ["answer", "g1", "--director", "--json"])
        let declined = try DirectorOutcome.read(#"""
        {"ok":true,"answered":false,"told":null,"answer":null,"applied":[],
         "decided":[{"question":"Where does this goal finish?","answer":"research","why":"the goal asks for a pick"}],
         "declined":[{"question":"Which vendor?","why":"only you know the contract terms"}]}
        """#)
        #expect(!declined.answered)
        #expect(declined.declined == [DirectorChoice(question: "Which vendor?", answer: nil, why: "only you know the contract terms")])
        let answered = try DirectorOutcome.read(#"""
        {"ok":true,"answered":true,"told":"g1","answer":"research","applied":["finishes at: research"],
         "decided":[{"question":"Where does this goal finish?","answer":"research","why":"the goal asks for a pick"}],"declined":[]}
        """#)
        #expect(answered.answered && answered.decided.first?.answer == "research")

        let home = try Home()
        let goal = try home.goal("demo", "Pick a queue library")
        let decision = #"{"question":"Where does this goal finish?","answer":"research","source":"director","runner":"claude","model":"opus","tokens":10,"reverses":0,"reversed_by":0}"#
        try home.sql("""
        INSERT INTO event (work,kind,body,at) VALUES ('\(goal)','question','Where does this goal finish?
        - research (recommended)
        - complete PR','2026-10-13T14:02:00Z');
        INSERT INTO event (work,kind,body,at,payload) VALUES ('\(goal)','answer','research','2026-10-13T14:03:00Z','\(decision)');
        INSERT INTO event (work,kind,body,at,payload) VALUES ('\(goal)','decision','the director decided "Where does this goal finish?": research, because the goal asks for a pick','2026-10-13T14:03:00Z','\(decision)');
        INSERT INTO event (work,kind,body,at) VALUES ('\(goal)','question','Which vendor?','2026-10-13T14:04:00Z');
        INSERT INTO event (work,kind,body,at) VALUES ('\(goal)','note','the director declined "Which vendor?": only you know the contract terms','2026-10-13T14:05:00Z');
        """)
        let store = await Store(channel: try home.channel())
        let detail = try await store.goal(goal)
        let steps = detail.thread
        guard case .exchange(_, let answer?) = steps.first else { Issue.record("the first question reads with its answer: \(steps)"); return }
        #expect(answer.answeredByDirector)
        #expect(detail.decided(for: answer) == [DirectorChoice(question: "Where does this goal finish?", answer: "research", why: "the goal asks for a pick")])
        #expect(!steps.contains { if case .event(let e) = $0 { e.kind == "decision" } else { false } }, "its decisions read with its answer, not apart")
        #expect(steps.contains { if case .declined(_, let c) = $0 { c == DirectorChoice(question: "Which vendor?", answer: nil, why: "only you know the contract terms") } else { false } })
    }

    @Test("Work Under Way Shows What It Is Doing And For How Long")
    func workUnderWay() throws {
        func work(_ id: String, _ title: String, _ state: String = "running") throws -> Work {
            try JSONDecoder().decode(Work.self, from: Data(workJSON(state: state)
                .replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
                .replacingOccurrences(of: #""title":"T""#, with: #""title":"\#(title)""#).utf8))
        }
        func activity(_ status: String, kind: String, now: String, started: String, asking: Bool = false) throws -> Activity {
            let q = asking ? #"{"id":"q1","items":[],"answers":null,"reply":null}"# : "null"
            return try JSONDecoder().decode(Activity.self, from: Data("""
            {"now":"\(now)","kind":"\(kind)","at":"\(started)","asking":\(q),"error":null,"status":"\(status)",
             "usage":{"model":"m","tokens":1,"tools":1,"started":"\(started)","last":"\(started)"},"subagents":[]}
            """.utf8))
        }
        let goal = try work("g", "Release notes")
        let a = try work("a", "Customers get a note"), b = try work("b", "A guide"), c = try work("c", "Tagged"), d = try work("d", "Asks")
        let rows = WorkingRows(goal: goal, tasks: [a, b, c, d], activity: [
            "g": try activity("running", kind: "thinking", now: "weighing it", started: "2026-10-13T10:00:00Z"),
            "a": try activity("running", kind: "tool", now: "Bash · go test ./...", started: "2026-10-13T10:05:00Z"),
            "b": try activity("idle", kind: "text", now: "done for now", started: "2026-10-13T10:06:00Z"),
            "d": try activity("running", kind: "question", now: "Which?", started: "2026-10-13T10:07:00Z", asking: true),
        ], inFlight: [])
        #expect(rows.shown.map(\.who) == ["Customers get a note", "Director"], "only running sessions not waiting on a person, newest first")
        #expect(rows.shown.map(\.doing) == ["Running `go test ./...`", "Thinking"])
        #expect(rows.shown[0].since == AgentCard.instant("2026-10-13T10:05:00Z"))

        #expect(Working.doing(kind: .tool, now: "Read · internal/serve/tree.go") == "Reading internal/serve/tree.go")
        #expect(Working.doing(kind: .tool, now: "Edit · a.go") == "Editing a.go")
        #expect(Working.doing(kind: .tool, now: "Grep · TODO") == "Searching TODO")
        #expect(Working.doing(kind: .tool, now: "Frobnicate · x") == "Using Frobnicate · x")
        #expect(Working.doing(kind: .prompt, now: "the brief") == "Thinking")

        let sent = Date(timeIntervalSince1970: 1_800_000_000)
        let writes = [InFlight(plan: try Plan.send("c", text: "hi"), at: sent),
                      InFlight(plan: Plan.directorDecides("g"), at: sent.addingTimeInterval(1)),
                      InFlight(plan: Plan(title: "Re-plan", argv: ["goal", "plan", "g"]), at: sent.addingTimeInterval(2)),
                      InFlight(plan: Plan.pause("g"), at: sent.addingTimeInterval(3)),
                      InFlight(plan: try Plan.send("elsewhere", text: "hi"), at: sent.addingTimeInterval(4))]
        let busy = WorkingRows(goal: goal, tasks: [a, b, c, d], activity: [:], inFlight: writes)
        #expect(busy.shown.map(\.doing) == ["Re-planning", "Deciding in your place", "Sending your message"], "only writes a person waits on, on this goal")
        #expect(busy.shown.map(\.who) == ["Director", "Director", "Tagged"])

        let many = WorkingRows((0..<5).map { WorkingRow(id: "\($0)", who: "w", doing: "Thinking", since: Date(timeIntervalSince1970: Double($0))) })
        #expect(many.shown.map(\.id) == ["4", "3", "2"] && many.more == 2, "three shown, the rest counted")

        let tool = try JSONDecoder().decode(Step.self, from: Data(#"{"kind":"tool","at":"2026-10-13T10:00:00Z","text":null,"tool":{"id":"t","name":"Bash","summary":"make","input":"","result":null,"failed":false},"question":null}"#.utf8))
        let own = WorkingRows(task: a, status: .running, asking: false, last: tool, usage: nil, inFlight: [])
        #expect(own.shown.map(\.doing) == ["Running `make`"])
        #expect(WorkingRows(task: a, status: .idle, asking: false, last: tool, usage: nil, inFlight: []).isEmpty)

        let row = { (state: String, needsYou: Int) in SidebarRow(work: try work("s", "S", state), band: .inFlight, progress: nil, needsYou: needsYou, finish: nil, finished: nil, proposed: false) }
        #expect(try row("running", 0).working)
        #expect(try !row("running", 1).working, "a goal waiting on the person is not shown working")
        #expect(try !row("queued", 0).working)
    }

    @Test("Any Text The App Shows Can Be Copied")
    func copied() throws {
        let said = "## Plan\nRun **the** `tests` and [read](https://x.y) it.\n- first\n```\ncode\n```"
        #expect(Clip.plain(markdown: said) == "Plan\nRun the tests and read it.\n- first\ncode")
        let w = try JSONDecoder().decode(Work.self, from: Data(workJSON(state: "running").utf8))
        #expect(Clip.work(w) == "T (w1)")
    }

    @Test("The Agents Panel Sits Beside The Page, Never Over It")
    func panelBeside() {
        #expect(PanelWidth.standard == 320)
        #expect(PanelWidth.clamped(100) == 260)
        #expect(PanelWidth.clamped(900) == 480)
        #expect(PanelWidth.clamped(333) == 333)
    }

    @Test("Files And Images Go With What The Person Sends")
    func filesGoWithWhatIsSent() async throws {
        let dir = tempDir()
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let notes = dir.appendingPathComponent("notes.txt")
        try "rollback notes".write(to: notes, atomically: true, encoding: .utf8)
        let file = try Attaching.file(notes)
        #expect(file.name == "notes.txt" && file.size == 14 && !file.isImage)
        #expect(throws: AttachError.folder(dir.lastPathComponent)) { try Attaching.file(dir) }
        let pasted = try Attaching.png(Data([0x89, 0x50, 0x4E, 0x47]), in: dir.appendingPathComponent("pasted"))
        #expect(pasted.isImage && pasted.name.hasSuffix(".png") && FileManager.default.fileExists(atPath: pasted.path), "a pasted image is a PNG file of its own")

        #expect(try Plan.send("g1", text: "see this", files: [file, pasted]).argv == ["send", "g1", "see this", "--attach", notes.path, "--attach", pasted.path])
        #expect(try Plan.send("g1", text: "  ", files: [file]).argv == ["send", "g1", "", "--attach", notes.path], "files alone can be sent")
        #expect(throws: PlanError.self) { try Plan.send("g1", text: " ") }
        #expect(try Plan.start(project: "demo", text: "", files: [file]).argv == ["goal", "start", "demo", "", "--detach", "--json", "--attach", notes.path])
        #expect(throws: PlanError.self) { try Plan.continueGoal("g1", text: "", files: [file]) }
        #expect(try Plan.send("g1", text: "x", files: [file]).attached == [notes.path])

        let home = try Home()
        try fakeRunner(home)
        let store = await Store(channel: try home.channel())
        let message = await store.post(try Plan.send("t1", text: "see this", files: [file]), to: "t1")
        #expect(message.text == "see this", "what was said is the text, not a file")

        let goal = try home.goal("demo", "Pick a queue library")
        try home.sql("""
        UPDATE work SET state='needs-input', runner='fake', session='s-plan' WHERE id='\(goal)';
        INSERT INTO event (work,kind,body,at) VALUES ('\(goal)','question','Where does this goal finish?
        - research (recommended)
        - complete PR','2026-10-13T14:02:00Z');
        INSERT INTO event (work,kind,body,at,payload) VALUES ('\(goal)','attachment','/x/1-alone.txt','2026-10-13T14:01:00Z','{"name":"alone.txt","path":"/x/1-alone.txt","size":3,"media_type":"text/plain"}');
        """)
        let asked = try await store.goal(goal)
        let question = try #require(asked.asking)
        let r = try await store.run(try Plan.answer(goal, question, answers: ["research"], files: [file]))
        #expect(r.code == 0, "\(r.stderr)")
        let detail = try await store.goal(goal)
        #expect(detail.files.map(\.name) == ["alone.txt", "notes.txt"])
        let answer = try #require(detail.events.first { $0.kind == "answer" })
        #expect(detail.files(with: answer).map(\.name) == ["notes.txt"], "a file shows on the message it was sent with")
        let alone = detail.thread.compactMap { step -> String? in
            if case .event(let e) = step, e.kind == "attachment" { return e.body } else { return nil }
        }
        #expect(alone == ["/x/1-alone.txt"], "a file no message carries shows on its own; one a message carries does not")
    }

    @Test("The App Speaks The Product's Language, Not wd's")
    func speaksTheProductsLanguage() async throws {
        #expect(!Words.plain("Superseded") && !Words.plain("judged met") && !Words.plain("A complete PR") && !Words.plain("Landed"),
                "the check catches each internal term")

        let t = try SeededTree()
        let store = await Store(channel: try t.home.channel())
        let goal = try await store.goal(t.top)
        let record = try await store.record(t.top)
        let entry = { (kind: String, what: String) in RecordEntry(event: 1, at: "2026-10-13T11:00:00Z", kind: kind, what: what, where: "w", by: "director") }
        let kinds = ["decision", "finish", "asked", "course", "question", "answer", "report", "delivery", "chore", "superseded"]
        let fates = [SubGoalFate.Status.kept, .new, .superseded].flatMap { status in
            ([nil] + WorkState.allCases).map { SubGoalFate(work: nil, title: "x", status: status, state: $0, why: nil).badge.text }
        }
        var shown: [String] = []
        shown += WorkState.allCases.flatMap { [$0.look.label, $0.badge.text] }
        shown += [StateMark.done, .needsYou, .working, .waiting, .superseded].map(\.label)
        shown += Band.allCases.map(\.label)
        shown += RecordKind.allCases.map(\.label) + record.summary.cards.map(\.label)
        shown += (kinds.map { entry($0, "on course: x") } + [entry("judged", "met: x"), entry("judged", "missed: x")]).flatMap { [$0.badge.text, $0.sentence] }
        shown += record.entries.map(\.sentence)
        shown += fates
        shown += goal.nodes.flatMap { [$0.badge.text, $0.meta(superseded: goal.superseded), $0.mark.label] } + [goal.progressLine]
        shown += ["verify", "land", "soft-done", "done"].map(Words.gate)
        shown += [Plan.verify("w"), Plan.softDone("w"), Plan.done("w")].map(\.title)
        shown += FinishMeaning.lines.compactMap { FinishMeaning.of($0) }.flatMap { [$0.title, $0.meaning] }
        shown += FinishMeaning.lines.map(Words.finishLine) + [goal.finishWords?.line ?? ""]
        shown += ["state", "report", "verify", "pr", "note", "sent", "spawn", "attach", "question", "answer", "decision",
                  "abandon", "look", "supervised", "course", "judged", "attachment"].map(Words.event)
        shown += WorkState.allCases.map { Words.eventBody(kind: "state", body: $0.rawValue) }
        #expect(shown.filter { !Words.plain($0) } == [], "no shown label uses wd's own terms")

        #expect(WorkState.briefed.look.label == "Ready to start" && WorkState.needsInput.look.label == "Needs you")
        #expect(WorkState.review.look.label == "To review" && WorkState.softDone.look.label == "Waiting to be confirmed")
        #expect(WorkState.superseded.badge.text == "Replaced")
        #expect(WorkState.allCases.allSatisfy { $0.badge.text == $0.look.label }, "a row's badge and its icon say the same")
        #expect(entry("judged", "met: x").badge.text == "Checked: done" && entry("judged", "missed: x").badge.text == "Checked: not done yet")
        #expect(Words.replaced(why: "guides live with each release") == "Replaced by a change of plan: guides live with each release")
    }

    @Test("A Table In A Reply Reads As A Table")
    func tableReadsAsTable() {
        let said = """
        Checks so far:

        | Check | Run? | Result |
        |---|:---:|---:|
        | `go test` | **yes** | [passed](https://ci.example/1) |
        | lint \\| vet | no |
        > Quoted from the brief
        > second line

        ---
        - outer
          - inner
            - deepest
        - [x] done box
        1. first
        """
        let blocks = Markdown.blocks(said)
        guard case .table(let table) = blocks[1] else { Issue.record("no table in \(blocks)"); return }
        #expect(table.header == ["Check", "Run?", "Result"])
        #expect(table.alignments == [.leading, .center, .trailing])
        #expect(table.rows == [["`go test`", "**yes**", "[passed](https://ci.example/1)"], ["lint | vet", "no", ""]])
        #expect(blocks[2] == .quote("Quoted from the brief\nsecond line"))
        #expect(blocks[3] == .rule)
        #expect(blocks[4] == .bullet("outer", marker: "•", depth: 0, checked: nil))
        #expect(blocks[5] == .bullet("inner", marker: "•", depth: 1, checked: nil))
        #expect(blocks[6] == .bullet("deepest", marker: "•", depth: 2, checked: nil))
        #expect(blocks[7] == .bullet("done box", marker: "", depth: 0, checked: true))
        #expect(blocks[8] == .bullet("first", marker: "1.", depth: 0, checked: nil))
        #expect(Markdown.blocks("Title\n---\nbody") == [.heading(level: 2, text: "Title"), .paragraph("body")])
        #expect(Markdown.blocks("a | b\nnot a table") == [.paragraph("a | b\nnot a table")], "a pipe alone is not a table")

        let reply = Transcript.reply(said)
        let cells = reply.items[0].paras.compactMap { p -> (Cell, Para)? in
            if case .cell(let c) = p.box { return (c, p) } else { return nil }
        }
        #expect(cells.count == 9)
        #expect(cells.filter { $0.0.header }.allSatisfy { $0.1.runs.allSatisfy(\.bold) }, "the header row is set apart")
        #expect(cells[1].0.align == .center && cells[2].0.align == .trailing)
        let goTest = cells[3].1.runs[0]
        #expect(goTest.text == "go test" && goTest.chip, "code in a cell reads as code")
        #expect(cells[5].1.runs.first?.link == "https://ci.example/1", "a link in a cell is a link")
        #expect(!reply.string.contains("|") || reply.string.contains("lint | vet"), "no pipe marks are shown")
        #expect(!reply.string.contains("**") && !reply.string.contains("> "))

        let copied = reply.copied(0..<reply.length)
        #expect(copied.contains("Check\tRun?\tResult\ngo test\tyes\tpassed\nlint | vet\tno\t\n"), "a table copies as tab-separated rows: \(copied)")
        #expect(copied.contains("• outer\n• inner") && copied.contains("☑ done box") && copied.contains("1. first"))
        #expect(Clip.plain(markdown: "| a | b |\n|---|---|\n| `1` | 2 |") == "a\tb\n1\t2")
    }

    @Test("Copying Across The Conversation Keeps Every Line It Spans")
    func copyingAcrossTheConversation() {
        func step(_ kind: StepKind, _ text: String? = nil, tool: ToolCall? = nil, question: Question? = nil) -> Step {
            Step(kind: kind, at: "2026-10-13T10:00:00Z", text: text, tool: tool, question: question)
        }
        let bash = ToolCall(id: "t1", name: "Bash", summary: "cd app && make test", input: "{\"command\":\"cd app && make test\"}", result: "ok\n12 passed", failed: false)
        let steps = [
            step(.prompt, "Make the tests pass"),
            step(.thinking, "The tests fail on a missing file.\nI will add it."),
            step(.tool, tool: bash),
            step(.text, "All **12** pass:\n\n| Suite | Result |\n|---|---|\n| unit | `ok` |"),
            step(.question, question: Question(id: "q1", items: [QuestionItem(question: "Ship it?", options: [QuestionOption(label: "Yes"), QuestionOption(label: "No")])])),
        ]
        let pieces = Transcript.pieces(steps, speaker: "Claude", running: false, time: { _ in "10:00" })
        #expect(pieces.count == 2, "the conversation is one text up to the question waiting on the person")
        guard case .text(let t) = pieces[0], case .asking(4, _) = pieces[1] else { Issue.record("\(pieces)"); return }

        let all = t.copied(0..<t.length)
        #expect(all == """
        Brief 10:00
        Make the tests pass
        Claude · 10:00
        Thinking The tests fail on a missing file.
        Bash cd app && make test
        All 12 pass:
        Suite\tResult
        unit\tok
        """, "every line reads as shown, with no symbols or marks")

        let start = t.starts
        let thinking = t.items.firstIndex { $0.id == "s1" }!, reply = t.items.firstIndex { $0.id == "s3" }!
        let span = (start[thinking] + 2)..<(start[reply] + 6)
        #expect(t.copied(span) == "Thinking The tests fail on a missing file.\nBash cd app && make test\nAll 12", "one drag spans a thinking line, a step and a reply")
        #expect(t.markdown(span) == "The tests fail on a missing file.\nI will add it.\n\nBash cd app && make test\n\nAll **12** pass:\n\n| Suite | Result |\n|---|---|\n| unit | `ok` |")

        let open = Transcript.pieces(steps, speaker: "Claude", running: false, toggled: ["s1", "s2"], time: { _ in "10:00" })
        guard case .text(let o) = open[0] else { return }
        let unfolded = o.copied(0..<o.length)
        #expect(unfolded.contains("Thinking\nThe tests fail on a missing file.\nI will add it.\nBash cd app && make test\nINPUT\n{\"command\":\"cd app && make test\"}\nRESULT\nok\n12 passed"),
                "an unfolded step copies its full text: \(unfolded)")
        #expect(o.items.first { $0.id == "s2" }?.paras.first?.oneLine == false && t.items.first { $0.id == "s2" }?.paras.first?.oneLine == true,
                "a folded step is one line")

        let more = Transcript.pieces(steps.prefix(4) + [step(.text, "And one more thing.")], speaker: "Claude", running: true, time: { _ in "10:00" })
        guard case .text(let m) = more[0] else { return }
        #expect(Transcript.firstChange(from: t, to: m) == t.items.count, "a new step changes nothing before it")
        #expect(Transcript.firstChange(from: t, to: t) == nil)

        // A goal's conversation reads as one text too.
        func event(_ id: Int, _ work: String, _ kind: String, _ body: String, decision: String = "null") -> String {
            let quoted = String(data: try! JSONEncoder().encode(body), encoding: .utf8)!
            return #"{"id":\#(id),"work":"\#(work)","kind":"\#(kind)","body":\#(quoted),"at":"2026-10-12T01:\#(String(format: "%02d", id)):00Z","effective":null,"decision":\#(decision)}"#
        }
        func work(_ id: String, _ title: String, _ state: String, runner: String? = nil) -> String {
            workJSON(state: state).replacingOccurrences(of: #""id":"w1""#, with: #""id":"\#(id)""#)
                .replacingOccurrences(of: #""title":"T""#, with: #""title":"\#(title)""#)
                .replacingOccurrences(of: #""runner":null"#, with: runner.map { #""runner":"\#($0)""# } ?? #""runner":null"#)
        }
        let director = #"{"question":"q","answer":"forward only","source":"director","runner":"","model":"","tokens":0,"reverses":0,"reversed_by":0}"#
        let mine = #"{"question":"","answer":"English only","source":"person","runner":"","model":"","tokens":0,"reverses":0,"reversed_by":0}"#
        let events = [
            event(1, "g", "decision", "English only", decision: mine),
            event(2, "g", "supervised", "Told the note agent to map names.\n\nDone:\n- SEND a: map names\nRefused:\n- START b — already under way"),
            event(3, "a", "question", "Down scripts?"),
            event(4, "a", "answer", "forward only", decision: director),
            event(5, "g", "look", "a task asked"),
            event(6, "a", "note", "stopped without a report: halfway through billing"),
        ].joined(separator: ",")
        let json = """
        {"goal":\(work("g", "Release notes", "running")),"tasks":[\(work("a", "Customer note", "running", runner: "claude"))],
         "rollup":{"counts":{},"done":0,"total":1},"events":[\(events)],"claims":[],"landings":[],"delivery":null,"activity":{},"reports":{},"waiting":[],\(treeless)}
        """
        let goal = try! JSONDecoder().decode(GoalDetail.self, from: Data(json.utf8))
        let thread = goal.transcript(.all, time: { _ in "10:00" }, day: { _ in "Monday" })
        #expect(thread.copied(0..<thread.length) == """
        Monday
        You · 10:00
        English only
        Director · 10:00
        Told the note agent to map names.
        Customer note · Working
        SEND a: map names
        START b — already under way
        claude · 10:00
        Asked the director
        Down scripts?
        Director answered · forward only
        Director looking: a task asked · 10:00
        claude · 10:00 stopped without a report
        halfway through billing
        """, "every header, line and card of a goal's conversation is one text: \(thread.copied(0..<thread.length))")
        let begins = thread.starts
        let look = thread.items.firstIndex { $0.id == "e5" }!
        #expect(thread.copied(begins[1]..<(begins[look] + 16)).hasSuffix("Director answered · forward only\nDirector looking"),
                "one drag spans the person's message, the director's turn, a card and a line")

        // So does a goal's record.
        let rows = [RecordEntry(event: 2, at: "2026-10-12T01:02:00Z", kind: "decision", what: "English only", where: "Release notes (top)", by: "you"),
                    RecordEntry(event: 1, at: "2026-10-12T01:01:00Z", kind: "chore", what: "refreshed dependencies", where: "Guide", by: "director")]
        let record = GoalRecord.transcript(rows, time: { _ in "01:0x" })
        let day = GoalRecord.days(rows)[0].day
        #expect(record.copied(0..<record.length) == """
        Time\tKind\tWhat\tWhere\tBy
        \(day)
        01:0x\tDecision\tEnglish only\tRelease notes (top)\tyou
        01:0x\tHousekeeping\trefreshed dependencies\tGuide\tdirector
        """, "the record copies as its rows: \(record.copied(0..<record.length))")
    }

    @Test("A Message Waiting For The Director Says So")
    func aMessageWaitingForTheDirectorSaysSo() {
        let mine = #"{"question":"","answer":"Cover the API too","source":"person","runner":"","model":"","tokens":0,"reverses":0,"reversed_by":0}"#
        let said = #"{"id":1,"work":"w1","kind":"decision","body":"Cover the API too","at":"2026-10-12T01:01:00Z","effective":null,"decision":\#(mine)}"#
        func goal(waiting: String) -> GoalDetail {
            let json = """
            {"goal":\(workJSON(state: "running")),"tasks":[],"rollup":{"counts":{},"done":0,"total":0},"events":[\(said)],"claims":[],"landings":[],
             "delivery":null,"activity":{},"reports":{},"waiting":[],\(treeless)\(waiting)}
            """
            return try! JSONDecoder().decode(GoalDetail.self, from: Data(json.utf8))
        }
        func text(_ g: GoalDetail) -> String {
            let t = g.transcript(.all, time: { _ in "10:00" }, day: { _ in nil })
            return t.copied(0..<t.length)
        }
        #expect(text(goal(waiting: #","waiting_for_director":[1]"#)).hasSuffix("Cover the API too\nWaiting for the director to finish the last one"),
                "a message the director has not been given says it waits")
        #expect(!text(goal(waiting: #","waiting_for_director":[]"#)).contains("Waiting for the director"), "once given, it no longer waits")
        #expect(!text(goal(waiting: "")).contains("Waiting for the director"), "a wd that does not say shows nothing waiting")
    }
}
