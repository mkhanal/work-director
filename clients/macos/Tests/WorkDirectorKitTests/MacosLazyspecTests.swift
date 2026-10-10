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

    func channel() throws -> Channel {
        try Channel(wd: builtWD, environment: environment)
    }
}

func executable(_ url: URL, _ script: String) throws {
    try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    try script.write(to: url, atomically: true, encoding: .utf8)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
}

func tempDir() -> URL {
    FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
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
        id() { echo "$1" | sed 's/.*"id":\([0-9]*\).*/\1/'; }
        arg() { echo "$1" | sed 's/.*"argv":\["\([^"]*\)".*/\1/'; }
        read -r a; read -r b
        echo "{\"id\":$(id "$b"),\"result\":{\"code\":0,\"stdout\":\"$(arg "$b")\"}}"
        echo "{\"id\":$(id "$a"),\"result\":{\"code\":0,\"stdout\":\"$(arg "$a")\"}}"
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

    @Test("Reopening And Releasing Need A Reason")
    func reopenAndReleaseNeedAReason() throws {
        for blank in ["", "  ", "\n\t"] {
            #expect(throws: PlanError.reasonRequired("reopening")) { try Plan.reopen("g1", reason: blank) }
            #expect(throws: PlanError.reasonRequired("releasing")) { try Plan.release("g1", reason: blank) }
        }
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
}
