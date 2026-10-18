import AppKit
import Foundation

/// One thing the app did, `times` in a row.
public struct Crumb: Codable, Equatable, Sendable {
    public let at: Date
    public let what: String
    public var times: Int
}

/// What the app shows: the work open, if any, and which view of it.
public struct Screen: Codable, Equatable, Sendable {
    public let work: String?
    public let view: String

    public init(work: String?, view: String) {
        self.work = work
        self.view = view
    }
}

/// An exception the app died of, with the stack it was thrown from.
public struct Thrown: Codable, Equatable, Sendable {
    public let name: String
    public let reason: String
    public let stack: [String]
}

/// note.json in a crash's folder under `$WD_HOME/crashes`, as `wd crashes` reads it.
public struct CrashNote: Codable, Equatable, Sendable {
    public var app = "macos"
    public let at: Date
    public let build: String
    public let pid: Int32
    public let exception: Thrown?
    public let screen: Screen
    public let crumbs: [Crumb]
    /// The system crash report's file name beside the note.
    public var report: String?
}

/// What a running app has done so far, kept in `crashes/running-macos-<pid>.json`
/// and removed on a clean exit. One found for a process that is gone means it
/// crashed: a Swift trap cannot be caught, so this is all it leaves.
struct Running: Codable {
    let pid: Int32
    let build: String
    let started: Date
    let screen: Screen
    let crumbs: [Crumb]
}

public enum CrashLogError: Error, CustomStringConvertible {
    case noCrashHook

    public var description: String {
        "AppKit has no +[NSApplication _crashOnException:]; an exception AppKit crashes on leaves no note"
    }
}

/// Keeps what the app shows and the last things it did, so a crash can say
/// where the person was; writes the note an exception leaves, and on the next
/// launch pairs each crash with the system's report of it.
public final class CrashLog: @unchecked Sendable {
    /// The log of the running app, for the exception hooks, which can capture nothing.
    nonisolated(unsafe) public static var current: CrashLog?

    static let kept = 30

    public let folder: URL
    let build: String
    let pid: Int32
    let started: Date
    private let lock = NSLock()
    private var screen = Screen(work: nil, view: "starting")
    private var crumbs: [Crumb] = []
    private var writing = false
    /// The app quit as asked; the running file is gone and stays gone.
    private var over = false
    private let queue = DispatchQueue(label: "crash-log", qos: .utility)

    /// home is `$WD_HOME`.
    public init(home: URL, build: String, pid: Int32 = getpid(), started: Date = Date()) {
        folder = home.appendingPathComponent("crashes")
        self.build = build
        self.pid = pid
        self.started = started
    }

    /// `$WD_HOME`, as wd reads it.
    public static func home(environment: [String: String] = ProcessInfo.processInfo.environment) -> URL {
        if let home = environment["WD_HOME"], !home.isEmpty { return URL(fileURLWithPath: home) }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".work-director")
    }

    /// The build as `wd --version` names it: the release, or dev, and the commit.
    public static func build(of bundle: Bundle) -> String {
        guard let short = bundle.infoDictionary?["CFBundleShortVersionString"] as? String else { return "dev" }
        let release = short == "dev" ? "dev" : "v" + short
        guard let commit = bundle.infoDictionary?["CFBundleVersion"] as? String else { return release }
        return "\(release) (\(commit))"
    }

    var runningFile: URL { folder.appendingPathComponent("running-macos-\(pid).json") }

    /// The person now sees screen.
    public func show(_ screen: Screen) {
        lock.lock()
        let changed = self.screen != screen
        if changed {
            self.screen = screen
            add("showed " + [screen.work, screen.view].compactMap { $0 }.joined(separator: ", "))
        }
        lock.unlock()
        if changed { flush() }
    }

    /// The app did what: a request to wd, wd exiting. A repeat of the last counts it.
    public func did(_ what: String) {
        lock.lock()
        add(what)
        lock.unlock()
        flush()
    }

    private func add(_ what: String) {
        if crumbs.last?.what == what {
            crumbs[crumbs.count - 1].times += 1
        } else {
            crumbs.append(Crumb(at: Date(), what: what, times: 1))
            if crumbs.count > Self.kept { crumbs.removeFirst(crumbs.count - Self.kept) }
        }
    }

    /// Writes the running file off the caller's thread; changes made while a
    /// write waits ride on it.
    private func flush() {
        lock.lock()
        let idle = !writing
        writing = true
        lock.unlock()
        guard idle else { return }
        queue.async {
            self.lock.lock()
            self.writing = false
            self.lock.unlock()
            do {
                try self.writeNow()
            } catch {
                FileHandle.standardError.write(Data("crash log: \(error)\n".utf8))
            }
        }
    }

    /// Writes the running file, after any write already on its way.
    public func write() throws {
        try queue.sync { try writeNow() }
    }

    private func writeNow() throws {
        lock.lock()
        if over {
            lock.unlock()
            return
        }
        let running = Running(pid: pid, build: build, started: started, screen: screen, crumbs: crumbs)
        lock.unlock()
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        try Self.encoder.encode(running).write(to: runningFile, options: .atomic)
    }

    /// The app is quitting as asked: nothing to pair on the next launch.
    public func ended() throws {
        lock.lock()
        over = true
        lock.unlock()
        // After any write already on its way, which then writes nothing.
        try queue.sync { try FileManager.default.removeItem(at: runningFile) }
    }

    /// Writes the note of an exception the app is about to die of.
    @discardableResult
    public func died(of thrown: Thrown, at: Date = Date()) throws -> URL {
        lock.lock()
        let note = CrashNote(at: at, build: build, pid: pid, exception: thrown, screen: screen, crumbs: crumbs)
        lock.unlock()
        return try Self.keep(note, in: folder)
    }

    /// Makes every exception the app dies of leave a note first: one AppKit
    /// catches and crashes on, and one nothing catches.
    public static func catchExceptions() throws {
        // Without this an exception in an action is logged and the app runs
        // on in whatever state it left; with it, AppKit crashes on it too.
        UserDefaults.standard.register(defaults: ["NSApplicationCrashOnExceptions": true])
        // AppKit crashes on an exception it catches (one thrown while drawing,
        // always) by calling this, never the uncaught-exception handler.
        let selector = NSSelectorFromString("_crashOnException:")
        guard let meta = object_getClass(NSApplication.self),
              let method = class_getInstanceMethod(meta, selector) else { throw CrashLogError.noCrashHook }
        typealias Crash = @convention(c) (AnyObject, Selector, NSException) -> Void
        let original = unsafeBitCast(method_getImplementation(method), to: Crash.self)
        let hook: @convention(block) (AnyObject, NSException) -> Void = { app, exception in
            leaveNote(exception)
            original(app, selector, exception)
        }
        method_setImplementation(method, imp_implementationWithBlock(hook))
        NSSetUncaughtExceptionHandler { CrashLog.leaveNote($0) }
    }

    private static func leaveNote(_ exception: NSException) {
        let thrown = Thrown(name: exception.name.rawValue, reason: exception.reason ?? "", stack: exception.callStackSymbols)
        do {
            try current?.died(of: thrown)
        } catch {
            FileHandle.standardError.write(Data("crash log: the note of \(thrown.name) was not written: \(error)\n".utf8))
        }
    }

    /// Pairs every crash of an earlier run with the system's report of it:
    /// each running file whose process is gone becomes a note, or joins the
    /// note its exception left, with its `.ips` from reports copied beside
    /// it. Returns the crash folders it wrote.
    public static func gather(home: URL, reports: URL, alive: (Int32) -> Bool) throws -> [URL] {
        let fm = FileManager.default
        let folder = home.appendingPathComponent("crashes")
        guard fm.fileExists(atPath: folder.path) else { return [] }
        let names = try fm.contentsOfDirectory(atPath: folder.path)
        var gathered: [URL] = []
        for name in names where name.hasPrefix("running-macos-") && name.hasSuffix(".json") {
            let file = folder.appendingPathComponent(name)
            let running = try decoder.decode(Running.self, from: Data(contentsOf: file))
            if alive(running.pid) { continue }
            let report = try findReport(of: running, in: reports)
            let existing = try names.first { candidate in
                guard candidate.hasSuffix("-macos-\(running.pid)") else { return false }
                let note = try decoder.decode(CrashNote.self, from: Data(contentsOf: folder.appendingPathComponent(candidate).appendingPathComponent("note.json")))
                return note.at >= running.started
            }
            var dir: URL
            var note: CrashNote
            if let existing {
                dir = folder.appendingPathComponent(existing)
                note = try decoder.decode(CrashNote.self, from: Data(contentsOf: dir.appendingPathComponent("note.json")))
            } else {
                // When it died: the system's report of it, else its last write.
                let at = try (report ?? file).resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate ?? Date()
                note = CrashNote(at: at, build: running.build, pid: running.pid, exception: nil, screen: running.screen, crumbs: running.crumbs)
                dir = try keep(note, in: folder)
            }
            if let report {
                try fm.copyItem(at: report, to: dir.appendingPathComponent(report.lastPathComponent))
                note.report = report.lastPathComponent
                try encoder.encode(note).write(to: dir.appendingPathComponent("note.json"), options: .atomic)
            }
            try fm.removeItem(at: file)
            gathered.append(dir)
        }
        return gathered
    }

    /// The system's crash report of running's process: a WorkDirector-*.ips
    /// written since it started whose body names its pid.
    static func findReport(of running: Running, in reports: URL) throws -> URL? {
        let fm = FileManager.default
        guard fm.fileExists(atPath: reports.path) else { return nil }
        struct Body: Decodable { let pid: Int32 }
        for name in try fm.contentsOfDirectory(atPath: reports.path) where name.hasPrefix("WorkDirector-") && name.hasSuffix(".ips") {
            let url = reports.appendingPathComponent(name)
            guard let modified = try url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate,
                  modified >= running.started else { continue }
            let text = try String(contentsOf: url, encoding: .utf8)
            guard let newline = text.firstIndex(of: "\n") else { continue }
            let body = try JSONDecoder().decode(Body.self, from: Data(text[text.index(after: newline)...].utf8))
            if body.pid == running.pid { return url }
        }
        return nil
    }

    /// Writes note into a folder of its own, named for when and which process.
    static func keep(_ note: CrashNote, in folder: URL) throws -> URL {
        let stamp = DateFormatter()
        stamp.locale = Locale(identifier: "en_US_POSIX")
        stamp.timeZone = TimeZone(identifier: "UTC")
        stamp.dateFormat = "yyyyMMdd-HHmmss"
        let dir = folder.appendingPathComponent("\(stamp.string(from: note.at))-macos-\(note.pid)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try encoder.encode(note).write(to: dir.appendingPathComponent("note.json"), options: .atomic)
        return dir
    }

    static var encoder: JSONEncoder {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        e.outputFormatting = [.prettyPrinted, .sortedKeys]
        return e
    }

    static var decoder: JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }
}
