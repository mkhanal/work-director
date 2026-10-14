// Watches which app is frontmost and fails if it ever changes, so a UI check
// can prove the app it launched never took the screen from the person using
// the Mac (and so never received their keystrokes). Start it before the
// launch: it prints READY once it has read the starting app, then a line per
// change. Given the launched binary's path, only that app coming to the front
// fails (the person may switch between their own apps meanwhile), and it is
// ended the moment it does. It stops after the seconds given, or once the file
// given as -until no longer exists.
//   swift frontmost.swift <seconds> [binary path] [-until <file>]
import AppKit

var args = Array(CommandLine.arguments.dropFirst())
var until: String?
if let i = args.firstIndex(of: "-until"), i + 1 < args.count {
    until = args[i + 1]
    args.removeSubrange(i...(i + 1))
}
guard let first = args.first, let seconds = Double(first) else {
    FileHandle.standardError.write("usage: frontmost.swift <seconds> [binary path] [-until <file>]\n".data(using: .utf8)!)
    exit(2)
}
let binary = args.count > 1 ? URL(fileURLWithPath: args[1]).resolvingSymlinksInPath().path : nil

func front() -> NSRunningApplication? {
    // frontmostApplication is updated from workspace notifications, which
    // arrive only while the main run loop turns.
    RunLoop.main.run(until: Date().addingTimeInterval(0.02))
    return NSWorkspace.shared.frontmostApplication
}

func name(_ app: NSRunningApplication?) -> String {
    guard let app else { return "none" }
    return "\(app.processIdentifier) \(app.bundleIdentifier ?? app.executableURL?.path ?? app.localizedName ?? "?")"
}

let start = front()
print("READY frontmost \(name(start))")
fflush(stdout)
var failed = false
var last = start?.processIdentifier
let end = Date().addingTimeInterval(seconds)
while Date() < end, until.map({ FileManager.default.fileExists(atPath: $0) }) ?? true {
    let now = front()
    guard now?.processIdentifier != last else { continue }
    last = now?.processIdentifier
    // The person may move between their own apps meanwhile: given the
    // binary, only it coming to the front fails.
    let mine = binary != nil && now?.executableURL?.resolvingSymlinksInPath().path == binary
    print("\(mine || binary == nil ? "CHANGED" : "moved") frontmost \(name(now))")
    if mine, let now {
        kill(now.processIdentifier, SIGKILL)
        print("ENDED \(now.processIdentifier): the app under test came to the front")
    }
    fflush(stdout)
    failed = failed || mine || binary == nil
}
exit(failed ? 1 : 0)
