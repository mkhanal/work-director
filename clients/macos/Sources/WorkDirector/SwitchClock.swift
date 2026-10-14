import Foundation
import os

/// Times a switch from choosing work to seeing it: set WD_SWITCH_LOG=1 and
/// each switch logs, on standard error and as signposts under "switch", how
/// long until the page showed something and until it showed the fresh read,
/// and the longest pass of the main run loop in the 1.5 s after it.
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

    /// Counts each pass the main run loop makes: a press or keystroke that
    /// comes in waits at most one pass, so the longest is the worst a person
    /// would feel. Unlike a probe from outside, which the app may answer from
    /// a nested run loop mid-pass, this sees every pass whole, including the
    /// layout and drawing done just before it waits.
    private static func watchMainThread() {
        guard observer == nil else { return }
        func close() {
            guard let since = busySince else { return }
            busySince = nil
            guard var c = counting else { return }
            let span = ContinuousClock.now - since
            c.longest = max(c.longest, span)
            c.total += span
            counting = c
        }
        let begins = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.afterWaiting.rawValue | CFRunLoopActivity.beforeTimers.rawValue,
                                                        true, CFIndex.min) { _, _ in
            MainActor.assumeIsolated {
                close()
                busySince = .now
            }
        }
        // Last of all before the wait, after the window's layout and drawing.
        let ends = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, true, CFIndex.max) { _, _ in
            MainActor.assumeIsolated { close() }
        }
        observer = begins
        CFRunLoopAddObserver(CFRunLoopGetMain(), begins, .commonModes)
        CFRunLoopAddObserver(CFRunLoopGetMain(), ends, .commonModes)
    }
}
