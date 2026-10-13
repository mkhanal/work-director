import Foundation

/// The PATH every wd the app starts runs with. An app opened from Finder or the
/// Dock inherits launchd's bare PATH, where no runner CLI is found; the person's
/// login shell knows where theirs live.
public enum LoginPath {
    static let marker = "__WD_PATH__"

    /// What the login shell runs, interactive so it reads the rc file where PATH is often set: its PATH between markers, so rc noise around it is ignored.
    public static let script = #"printf "%s" "__WD_PATH__${PATH}__WD_PATH__""#

    /// Where runner CLIs usually live, tried in order when the login shell does not answer.
    static let usual = ["~/.local/bin", "~/.opencode/bin", "~/.cargo/bin", "~/bin", "/opt/homebrew/bin", "/usr/local/bin", "~/.npm-global/bin"]

    /// The text between the first two markers, or nil when they are missing or enclose nothing.
    public static func parse(_ output: String) -> String? {
        let parts = output.components(separatedBy: marker)
        guard parts.count >= 3, !parts[1].isEmpty else { return nil }
        return parts[1]
    }

    /// The inherited PATH plus each usual directory that exists.
    public static func fallback(inherited: String, home: String, exists: (String) -> Bool) -> String {
        let found = usual
            .map { $0.hasPrefix("~/") ? home + $0.dropFirst() : $0 }
            .filter(exists)
        return merge(inherited, found.joined(separator: ":"))
    }

    /// first's entries, then then's that first lacks; empty entries dropped.
    public static func merge(_ first: String, _ then: String) -> String {
        var seen = Set<String>()
        var out: [String] = []
        for entry in (first + ":" + then).split(separator: ":").map(String.init) where seen.insert(entry).inserted {
            out.append(entry)
        }
        return out.joined(separator: ":")
    }

    /// The PATH wd runs with: the login shell's, or the fallback, then the inherited one.
    public static func path(login: String?, inherited: String, home: String, exists: (String) -> Bool) -> String {
        merge(login ?? fallback(inherited: inherited, home: home, exists: exists), inherited)
    }

    /// $SHELL, else the account's shell, else zsh.
    public static func shell(environment: [String: String], account: String?) -> String {
        if let shell = environment["SHELL"], !shell.isEmpty { return shell }
        if let account, !account.isEmpty { return account }
        return "/bin/zsh"
    }

    /// environment with only PATH replaced.
    public static func environment(_ environment: [String: String], path: String) -> [String: String] {
        var env = environment
        env["PATH"] = path
        return env
    }

    /// Asks the login shell for its PATH and returns the environment every wd gets.
    /// Blocks for at most timeout; run it off the main thread.
    public static func resolve(environment: [String: String] = ProcessInfo.processInfo.environment, timeout: TimeInterval = 5) -> [String: String] {
        let account = getpwuid(getuid()).flatMap { $0.pointee.pw_shell }.map { String(cString: $0) }
        let login = ask(shell: shell(environment: environment, account: account), timeout: timeout)
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let path = path(login: login, inherited: environment["PATH"] ?? "", home: home) {
            var dir: ObjCBool = false
            return FileManager.default.fileExists(atPath: $0, isDirectory: &dir) && dir.boolValue
        }
        return Self.environment(environment, path: path)
    }

    private final class Output: @unchecked Sendable {
        let lock = NSLock()
        var data = Data()
    }

    /// The login shell's PATH, or nil when it fails or does not answer in time.
    private static func ask(shell: String, timeout: TimeInterval) -> String? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: shell)
        process.arguments = ["-l", "-i", "-c", script]
        process.standardInput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        let stdout = Pipe()
        process.standardOutput = stdout
        let output = Output()
        let answered = DispatchSemaphore(value: 0)
        // Done once both markers are in or the pipe closes: a child the rc file
        // leaves running may hold the pipe open long after the shell exits.
        stdout.fileHandleForReading.readabilityHandler = { handle in
            let chunk = handle.availableData
            output.lock.lock()
            output.data.append(chunk)
            let text = String(decoding: output.data, as: UTF8.self)
            output.lock.unlock()
            if chunk.isEmpty || parse(text) != nil {
                handle.readabilityHandler = nil
                answered.signal()
            }
        }
        do { try process.run() } catch { return nil }
        let timedOut = answered.wait(timeout: .now() + timeout) == .timedOut
        if timedOut {
            stdout.fileHandleForReading.readabilityHandler = nil
            process.terminate()
            return nil
        }
        output.lock.lock()
        defer { output.lock.unlock() }
        return parse(String(decoding: output.data, as: UTF8.self))
    }
}
