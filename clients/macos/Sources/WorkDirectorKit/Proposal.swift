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

/// Why a planner broke a goal down as it did.
public struct Reasoning: Codable, Sendable, Hashable {
    public let why: String?
    public let leftOut: String?
    public let judgeBy: String?

    enum CodingKeys: String, CodingKey {
        case why
        case leftOut = "left_out"
        case judgeBy = "judge_by"
    }

    /// Each part the planner said, under its label, in the canvas's order.
    public var sections: [(label: String, text: String)] {
        [("Why this breakdown", why), ("Left out", leftOut), ("How you'd judge it", judgeBy)]
            .compactMap { label, text in
                guard let text = text?.trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty else { return nil }
                return (label, text)
            }
    }
}

extension GoalDetail {
    /// The proposed breakdown while it waits for approval, nil otherwise: each
    /// sub-goal after every sibling it waits on, else in the order served.
    public var proposal: [ProposedRow]? {
        guard proposed else { return nil }
        let rows = Self.waitOrder(tree.filter { !$0.group && $0.work.state != .superseded })
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

    static func waitOrder(_ nodes: [TreeNode]) -> [TreeNode] {
        let ids = Set(nodes.map(\.id))
        var placed: [TreeNode] = []
        var done = Set<String>()
        var left = nodes
        while !left.isEmpty {
            // A cycle cannot be ordered; it keeps the order served.
            let i = left.firstIndex { $0.after.allSatisfy { !ids.contains($0) || done.contains($0) } } ?? 0
            let next = left.remove(at: i)
            placed.append(next)
            done.insert(next.id)
        }
        return placed
    }

    /// The writes that put a proposal in the order the person dropped it in,
    /// moving `id` to `index`: each sub-goal waits on the one before it and
    /// the first on none. Only waits that change are written, from the top,
    /// so no write makes a cycle.
    public func reorder(moving id: String, to index: Int) -> [Plan] {
        guard var rows = proposal?.map(\.node), let from = rows.firstIndex(where: { $0.id == id }) else { return [] }
        let node = rows.remove(at: from)
        rows.insert(node, at: max(0, min(index, rows.count)))
        return rows.enumerated().compactMap { i, node in
            let wants = i == 0 ? [] : [rows[i - 1].id]
            guard Set(node.after) != Set(wants) else { return nil }
            return Plan.after(node.id, wants)
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

    /// Sets what a sub-goal waits on; none starts it first.
    public static func after(_ id: String, _ waits: [String]) -> Plan {
        Plan(title: "Order", argv: ["goal", "after", id, waits.isEmpty ? "none" : waits.joined(separator: ",")])
    }

    /// Folds one proposed sub-goal into another: the earlier card is kept.
    public static func merge(_ a: ProposedRow, _ b: ProposedRow) -> Plan {
        let (keep, other) = a.number < b.number ? (a, b) : (b, a)
        return Plan(title: "Merge", argv: ["goal", "merge", keep.id, other.id])
    }

    /// Plans a goal again with a note the planner reads as a decision.
    public static func replan(_ id: String, note: String) throws -> [Plan] {
        [Plan(title: "Note for planning", argv: ["decide", id, try required(note, for: "re-planning")]),
         Plan(title: "Re-plan", argv: ["goal", "plan", id])]
    }

    /// Sets where a top goal finishes, as the person decided.
    public static func finish(_ id: String, _ line: String) -> Plan {
        Plan(title: "Finishes at", argv: ["goal", "finish", id, line])
    }

    /// Supersedes a sub-goal, with why.
    public static func drop(_ id: String, why: String) throws -> Plan {
        Plan(title: "Drop", argv: ["goal", "drop", id, try required(why, for: "dropping a sub-goal")])
    }
}
