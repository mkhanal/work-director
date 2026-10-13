import Foundation
import Synchronization

/// The moment an ISO 8601 time wd serves names, with or without fractional
/// seconds; nil when it names none. A page reads the same few hundred times
/// each time it is drawn, so each is parsed once and remembered.
public func instant(_ iso: String) -> Date? {
    if let known = Stamps.read.withLock({ $0[iso] }) { return known }
    let date = Stamps.fractional.date(from: iso) ?? Stamps.whole.date(from: iso)
    Stamps.read.withLock { read in
        if read.count >= Stamps.kept { read.removeAll(keepingCapacity: true) }
        read[iso] = date
    }
    return date
}

private enum Stamps {
    /// How many times stay remembered before all are let go.
    static let kept = 50_000
    static let read = Mutex<[String: Date?]>([:])
    nonisolated(unsafe) static let fractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
    nonisolated(unsafe) static let whole = ISO8601DateFormatter()
}
