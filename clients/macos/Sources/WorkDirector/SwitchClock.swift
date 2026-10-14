import Foundation
import os

/// Times a switch from choosing work to seeing it: set WD_SWITCH_LOG=1 and
/// each switch logs, on standard error and as signposts under "switch", how
/// long until the page showed something and until it showed the fresh read,
/// and how long the main thread was held at once in the 1.5 s after it.
@MainActor
enum SwitchClock {
    static let on = ProcessInfo.processInfo.environment["WD_SWITCH_LOG"] == "1"
    private static let log = OSLog(subsystem: "WorkDirector", category: "switch")
    private static var started: [String: (at: ContinuousClock.Instant, id: OSSignpostID)] = [:]
    /// The switch whose main-thread time is being counted, its longest
    /// single stretch of work and its total.
    private static var counting: (id: String, longest: Duration, total: Duration)?
    private static var busySince: ContinuousClock.Instant?
    private static var observer: CFRunLoopObserver?

    /// The person chose work id.
    static func start(_ id: String) {
        guard on else { return }
        let signpost = OSSignpostID(log: log)
        os_signpost(.begin, log: log, name: "switch", signpostID: signpost, "%{public}s", id)
        started[id] = (.now, signpost)
        watchMainThread()
        // The press itself is being handled now: it counts from here.
        busySince = busySince ?? .now
        counting = (id, .zero, .zero)
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            guard let c = counting, c.id == id else { return }
            counting = nil
            FileHandle.standardError.write(Data(String(format: "switch %@ main-thread longest %.1fms total %.1fms\n", id,
                                                       c.longest / .milliseconds(1), c.total / .milliseconds(1)).utf8))
        }
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

    /// Counts each stretch the main run loop works between waits. Unlike a
    /// probe from outside, which the app may answer from a nested run loop
    /// mid-stretch, this sees every stretch whole.
    private static func watchMainThread() {
        guard observer == nil else { return }
        let made = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.afterWaiting.rawValue | CFRunLoopActivity.beforeWaiting.rawValue,
                                                      true, 0) { _, activity in
            MainActor.assumeIsolated {
                if activity == .afterWaiting {
                    busySince = .now
                } else if let since = busySince {
                    busySince = nil
                    guard var c = counting else { return }
                    let span = ContinuousClock.now - since
                    c.longest = max(c.longest, span)
                    c.total += span
                    counting = c
                }
            }
        }
        observer = made
        CFRunLoopAddObserver(CFRunLoopGetMain(), made, .commonModes)
    }
}
