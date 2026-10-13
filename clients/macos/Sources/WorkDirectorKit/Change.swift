import Foundation

/// A goal's title, done-when and finish line, before or after a course change.
public struct GoalShape: Codable, Sendable, Hashable {
    public let title: String
    public let doneWhen: String?
    public let finish: String?

    enum CodingKeys: String, CodingKey {
        case title, finish
        case doneWhen = "done_when"
    }
}

/// What answering a question with its first choice would change: the plan,
/// the top goal's course, or where the goal finishes.
public struct PendingChange: Codable, Sendable, Hashable {
    public enum Kind: String, Codable, Sendable {
        case plan, course, finish
    }

    public let kind: Kind
    /// The plan's actions as written, and each as a sentence.
    public let actions: [String]?
    public let lines: [String]?
    public let now: GoalShape?
    public let proposed: GoalShape?
    public let why: String?
    /// The finish lines offered, the recommended one marked.
    public let choices: [String]?

    /// The line heading its question card.
    public var heading: String {
        switch kind {
        case .plan: "Plan change · yours to decide"
        case .course: "Course change on the top goal · always yours to decide"
        case .finish: "Before I plan it · where does this goal finish?"
        }
    }

    /// A course change as rows of what it is now and what it would be.
    public var table: [(label: String, now: String, proposed: String)] {
        guard let now, let proposed else { return [] }
        return [
            ("Goal", now.title, proposed.title),
            ("Done when", now.doneWhen ?? "Not set", proposed.doneWhen ?? "Not set"),
            ("Finishes at", Self.finish(now.finish), Self.finish(proposed.finish)),
        ]
    }

    static func finish(_ line: String?) -> String {
        guard let line, !line.isEmpty else { return "Not decided" }
        return line.prefix(1).uppercased() + line.dropFirst()
    }
}

extension QuestionItem {
    /// What it asks, without the lines a change spells out under it.
    public var head: String {
        String(question.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).first ?? "")
    }
}
