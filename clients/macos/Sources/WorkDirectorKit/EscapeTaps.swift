import Foundation

/// Escape pressed twice in quick succession: the deliberate way to stop work,
/// which a single stray Escape never is.
public struct EscapeTaps: Sendable {
    public static let window: TimeInterval = 0.8
    private var last: Date?

    public init() {}

    /// Records a press and says whether it completes a quick pair. A press
    /// that completes one starts nothing, so a third quick press stops nothing.
    public mutating func tap(at now: Date = Date()) -> Bool {
        if let last, now.timeIntervalSince(last) <= Self.window {
            self.last = nil
            return true
        }
        last = now
        return false
    }
}
