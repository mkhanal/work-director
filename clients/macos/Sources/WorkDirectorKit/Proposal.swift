import Foundation

/// One sub-goal of a breakdown waiting for the person's approval, as the
/// proposal shows it: its number, what makes it done and its chips.
public struct ProposedRow: Sendable, Hashable, Identifiable {
    public let number: Int
    public let node: TreeNode
    /// "Starts first", "After 1 and 3"; then "Will be planned further" for a
    /// sub-goal that is a goal, else "One agent".
    public let chips: [String]

    public var id: String { node.id }
}

extension GoalDetail {
    /// The proposed breakdown, in order, while it waits for approval; nil otherwise.
    public var proposal: [ProposedRow]? {
        guard proposed else { return nil }
        let rows = tree.filter { !$0.group && $0.work.state != .superseded }
        let number = Dictionary(uniqueKeysWithValues: rows.enumerated().map { ($1.id, $0 + 1) })
        return rows.enumerated().map { i, node in
            let after = node.after.compactMap { number[$0] }.sorted()
            var chips: [String] = []
            if after.isEmpty {
                if i == 0 { chips.append("Starts first") }
            } else {
                chips.append("After " + Self.list(after.map(String.init)))
            }
            chips.append(node.work.kind.isGoal ? "Will be planned further" : "One agent")
            return ProposedRow(number: i + 1, node: node, chips: chips)
        }
    }

    static func list(_ words: [String]) -> String {
        words.count < 2 ? words.joined() : words.dropLast().joined(separator: ", ") + " and " + words.last!
    }
}

extension Plan {
    /// Starts a proposed breakdown.
    public static func approve(_ id: String) -> Plan { Plan(title: "Approve and start", argv: ["goal", "approve", id]) }

    /// Changes a goal's title or done-when; what is unchanged is left out.
    public static func editGoal(_ id: String, title: String? = nil, doneWhen: String? = nil) throws -> Plan {
        var argv = ["goal", "edit", id]
        if let title { argv += ["--title", try required(title, for: "a title")] }
        if let doneWhen { argv += ["--done-when", try required(doneWhen, for: "a done-when")] }
        guard argv.count > 3 else { throw PlanError.empty }
        return Plan(title: "Edit", argv: argv)
    }

    /// Turns a sub-goal into a goal of its own, planned when its turn comes.
    public static func splitGoal(_ id: String) -> Plan { Plan(title: "Split", argv: ["goal", "split", id]) }

    /// Supersedes a sub-goal, with why.
    public static func drop(_ id: String, why: String) throws -> Plan {
        Plan(title: "Drop", argv: ["goal", "drop", id, try required(why, for: "dropping a sub-goal")])
    }
}
