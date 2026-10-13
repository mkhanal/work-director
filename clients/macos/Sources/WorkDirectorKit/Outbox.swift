import Foundation

/// A message a person sent to work, shown from the moment it is sent until the
/// work's own record holds it.
public struct Outgoing: Sendable, Hashable, Identifiable {
    public let id: UUID
    public let work: String
    /// What was said, as sent.
    public let text: String
    public let at: Date
    public internal(set) var state: State
    let plan: Plan
    /// What runs once it is sent, as part of sending it: keeping it as feedback.
    let then: Plan?

    /// The paths of the files it carries.
    public var files: [String] { plan.attached }

    public enum State: Sendable, Hashable {
        case sending
        /// `wd` took it; it shows here until the work's record does.
        case sent
        /// Refused, with why. `sent` says whether `wd` took the message itself
        /// and only what ran after it failed.
        case failed(String, sent: Bool)
    }

    /// Whether the record now holds it: a decision on a goal, said in so many
    /// words, or a prompt in a session's conversation that contains it.
    public func arrived(in goal: GoalDetail) -> Bool {
        goal.events.contains { $0.work == work && $0.kind == "decision" && $0.body == text }
    }

    public func arrived(in steps: [Step]) -> Bool {
        steps.contains { $0.kind == .prompt && ($0.text ?? "").contains(text) }
    }
}
