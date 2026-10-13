import Foundation
import os

/// Times a switch from choosing work to seeing it: set WD_SWITCH_LOG=1 and
/// each switch logs, on standard error and as signposts under "switch", how
/// long until the page showed something and until it showed the fresh read.
@MainActor
enum SwitchClock {
    static let on = ProcessInfo.processInfo.environment["WD_SWITCH_LOG"] == "1"
    private static let log = OSLog(subsystem: "WorkDirector", category: "switch")
    private static var started: [String: (at: ContinuousClock.Instant, id: OSSignpostID)] = [:]

    /// The person chose work id.
    static func start(_ id: String) {
        guard on else { return }
        let signpost = OSSignpostID(log: log)
        os_signpost(.begin, log: log, name: "switch", signpostID: signpost, "%{public}s", id)
        started[id] = (.now, signpost)
    }

    /// The page for id reached a stage: what it shows first, or the fresh read.
    static func mark(_ id: String, _ stage: String) {
        guard on, let s = started[id] else { return }
        let ms = (ContinuousClock.now - s.at) / .milliseconds(1)
        os_signpost(.event, log: log, name: "switch", signpostID: s.id, "%{public}s %{public}s", id, stage)
        FileHandle.standardError.write(Data(String(format: "switch %@ %@ %.1fms\n", id, stage, ms).utf8))
        if stage == "fresh" {
            os_signpost(.end, log: log, name: "switch", signpostID: s.id, "%{public}s", id)
            started[id] = nil
        }
    }
}
