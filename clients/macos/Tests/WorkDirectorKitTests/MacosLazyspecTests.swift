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

    @Test("A Write Shows The Exact Command It Will Run")
    func writeShowsExactCommand() async throws {
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
    }

    @Test("Text In The Command Bar Becomes Argv Without A Shell")
    func commandBarSplitsWithoutAShell() throws {
        #expect(try Plan.typed(#"wd decide abc "two words" 'it said "hi"' $HOME ~ *"#).argv
            == ["decide", "abc", "two words", #"it said "hi""#, "$HOME", "~", "*"])
        #expect(try Plan.typed("  status   --json ").argv == ["status", "--json"])
        #expect(throws: PlanError.unclosedQuote) { try Plan.typed(#"decide abc "open"#) }
        #expect(throws: PlanError.empty) { try Plan.typed("wd") }
    }

    @Test("Reopening, Releasing And Dropping Need A Reason")
    func reopenReleaseAndDropNeedAReason() throws {
        let open = try JSONDecoder().decode(Work.self, from: Data(workJSON(state: "running").utf8))
        let rest = try JSONDecoder().decode(Work.self, from: Data(workJSON(state: "done").utf8))
        for blank in ["", "  ", "\n\t"] {
            #expect(throws: PlanError.reasonRequired("reopening")) { try Plan.reopen("g1", reason: blank) }
            #expect(throws: PlanError.reasonRequired("releasing")) { try Plan.release("g1", reason: blank) }
            #expect(throws: PlanError.reasonRequired("archiving open work")) { try Plan.archive(open, reason: blank) }
            #expect(throws: PlanError.reasonRequired("cancelling")) { try Plan.cancel("g1", reason: blank) }
        }
        #expect(try Plan.archive(rest, reason: "").argv == ["archive", "w1"])
        #expect(try Plan.archive(open, reason: " not wanted ").argv == ["archive", "w1", "not wanted"])
        #expect(try Plan.cancel("g1", reason: "client said stop").argv == ["cancel", "g1", "client said stop"])
        #expect(try Plan.reopen("g1", reason: " add search ").argv == ["reopen", "g1", "add search"])
        #expect(try Plan.release("g1", reason: "duplicate of g2").argv == ["release", "g1", "duplicate of g2"])
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
        #expect(fresh.goal.title == "A dark mode for the settings page" && fresh.tasks.count == 2)
        await store.stop()
    }
}
