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

/// What each finish line means, as a finish-line question offers it.
public enum FinishMeaning {
    /// How the director asks where a goal finishes.
    public static let question = "Where does this goal finish?"

    /// A finish line's title and what it hands back, or nil for a line outside the set.
    public static func of(_ line: String) -> (title: String, meaning: String)? {
        switch line.lowercased().trimmingCharacters(in: .whitespaces) {
        case "discovery": ("Discovery: the cause found and explained", "A findings report with its proof. No code changes.")
        case "research": ("Research complete: a written comparison and a pick", "Trade-offs and a recommendation with its reasons. No code changes.")
        case "local build": ("A local build you can try", "The work on a branch, with how to run it.")
        case "complete pr": ("A complete PR", "The change with its tests, ready to merge.")
        case "merged": ("Merged", "The pull request merged into the main branch.")
        case "released": ("Released", "Merged and published as a release.")
        default: nil
        }
    }
}

extension QuestionItem {
    /// Whether it asks where a goal finishes, so its choices are finish lines.
    public var asksFinish: Bool { head == FinishMeaning.question }

    /// What it asks, without the lines a change spells out under it.
    public var head: String {
        String(question.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).first ?? "")
    }
}
