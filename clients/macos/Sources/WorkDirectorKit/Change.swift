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
    /// What each source of an unsure finish line said; a finish change only.
    public let sources: FinishSources?
    /// What was found that calls for a plan or course change.
    public let found: String?
    /// What the change does to each sub-goal, in tree order then new ones.
    public let subGoals: [SubGoalFate]?

    enum CodingKeys: String, CodingKey {
        case kind, actions, lines, now, proposed, why, choices, sources, found
        case subGoals = "sub_goals"
    }

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

/// What the goal, the person's taste and the repo each said about where a
/// goal finishes, when they disagree.
public struct FinishSources: Codable, Sendable, Hashable {
    public let goal: String?
    public let repo: String?
    public let taste: String?

    /// Each source that said something, under its name, in the canvas's order.
    public var rows: [(label: String, said: String)] {
        [("The goal", goal), ("Your taste", taste), ("This repo", repo)].compactMap { label, said in
            guard let said = said?.trimmingCharacters(in: .whitespacesAndNewlines), !said.isEmpty else { return nil }
            return (label, said.prefix(1).uppercased() + said.dropFirst())
        }
    }
}

/// One sub-goal as a change would leave it.
public struct SubGoalFate: Codable, Sendable, Hashable {
    public enum Status: String, Codable, Sendable {
        case kept, new, superseded
    }

    /// Nil for a sub-goal the change adds.
    public let work: String?
    public let title: String
    public let status: Status
    public let state: WorkState?
    /// Why it is superseded.
    public let why: String?

    public var badge: GoalBadge {
        switch status {
        case .new: GoalBadge("New", .live)
        case .superseded: GoalBadge("Superseded", .waiting)
        case .kept:
            switch state {
            case .done, .softDone: GoalBadge("Kept · done", .settled)
            case .queued, nil: GoalBadge("Kept · not started", .waiting)
            default: GoalBadge("Kept · working", .live)
            }
        }
    }
}

/// What each finish line means, as a finish-line question offers it.
public enum FinishMeaning {
    /// Every finish line, in the order a person climbs them.
    public static let lines = ["discovery", "research", "local build", "complete PR", "merged", "released"]

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

extension Question {
    /// A change's two answers: its first choice, which applies it, and the
    /// choice that keeps things as they are; nil without both.
    public var changeChoices: (change: String, keep: String)? {
        guard let options = items.first?.options, options.count >= 2 else { return nil }
        return (AnswerDraft.unmarked(options[0].label), AnswerDraft.unmarked(options[1].label))
    }
}

extension Plan {
    /// Answers a course change with its first choice, then sets on the goal
    /// what the person changed from the proposal; what they left is not written.
    public static func editChange(_ id: String, _ question: Question, proposed: GoalShape,
                                  title: String, doneWhen: String, finish: String) throws -> [Plan] {
        guard let choices = question.changeChoices else { throw PlanError.empty }
        let trim = { (s: String) in s.trimmingCharacters(in: .whitespacesAndNewlines) }
        var plans = [try answer(id, question, answers: [choices.change])]
        let newTitle = trim(title) == proposed.title ? nil : title
        let newDoneWhen = trim(doneWhen) == (proposed.doneWhen ?? "") ? nil : doneWhen
        if newTitle != nil || newDoneWhen != nil {
            plans.append(try editGoal(id, title: newTitle, doneWhen: newDoneWhen))
        }
        if finish.lowercased() != (proposed.finish ?? "").lowercased() {
            plans.append(Plan.finish(id, finish))
        }
        return plans
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
