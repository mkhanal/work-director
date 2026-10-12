import Foundation

/// One card in the agents panel: a task's session, or a subagent one started.
public struct AgentCard: Sendable, Hashable, Identifiable {
    public enum Phase: Sendable, Hashable {
        /// Its process is up and it is working.
        case working
        /// It is waiting on a person's answer.
        case asking
        /// Its process is up and it has nothing to do.
        case resting
        /// It finished, or its session stopped.
        case settled
    }

    /// The task id, or "task/subagent" for a subagent.
    public let id: String
    public let task: String
    public let agent: String?
    public let title: String
    public let kind: String
    public let line: String?
    public let phase: Phase
    public let usage: Usage?
    public let subagents: [AgentCard]

    /// How long it has run: from its first record to now while it works, to
    /// its last record once it rests.
    public func elapsed(now: Date) -> TimeInterval? {
        guard let started = usage?.started.flatMap(AgentCard.instant) else { return nil }
        let end = phase == .working ? now : (usage?.last.flatMap(AgentCard.instant) ?? now)
        return max(0, end.timeIntervalSince(started))
    }

    static func instant(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }
}

/// The agents panel: every task of a goal with a session, each with the
/// subagents it started, and what they spent between them.
public struct AgentRoster: Sendable, Hashable {
    public let cards: [AgentCard]

    /// Cards for a goal's tasks, in the goal's order, after the goal's own
    /// session while it has one: its supervisor once it is supervised, else
    /// its planner.
    public init(goal: Work? = nil, supervised: Bool = false, tasks: [Work], activity: [String: Activity]) {
        let planning: [AgentCard] = goal.flatMap { g in
            activity[g.id].map { a in
                [AgentCard(id: g.id, task: g.id, agent: nil, title: supervised ? "Supervisor" : "Plan", kind: supervised ? "supervisor" : "planner",
                           line: a.error ?? a.now, phase: AgentRoster.phase(a.status, asking: a.asking != nil),
                           usage: a.usage, subagents: (a.subagents ?? []).map { AgentRoster.card(g.id, $0) })]
            }
        } ?? []
        cards = planning + tasks.compactMap { task in
            guard let a = activity[task.id] else { return nil }
            return AgentCard(
                id: task.id, task: task.id, agent: nil, title: task.title, kind: task.kind.rawValue,
                line: a.error ?? a.now, phase: AgentRoster.phase(a.status, asking: a.asking != nil),
                usage: a.usage,
                subagents: (a.subagents ?? []).map { AgentRoster.card(task.id, $0) })
        }
    }

    /// The one card for a task's own session, from its conversation.
    public init(task: Work, status: SessionStatus, asking: Bool, line: String?, usage: Usage?, subagents: [Subagent]) {
        cards = [AgentCard(
            id: task.id, task: task.id, agent: nil, title: task.title, kind: task.kind.rawValue,
            line: line, phase: AgentRoster.phase(status, asking: asking), usage: usage,
            subagents: subagents.map { AgentRoster.card(task.id, $0) })]
    }

    static func card(_ task: String, _ s: Subagent) -> AgentCard {
        AgentCard(id: task + "/" + s.id, task: task, agent: s.id, title: s.description.isEmpty ? s.id : s.description,
                  kind: s.type, line: s.said, phase: s.done ? .settled : .working, usage: s.usage, subagents: [])
    }

    /// Where a task's agent is, from its activity; settled with none.
    public static func phase(of activity: Activity?) -> AgentCard.Phase {
        guard let activity else { return .settled }
        return phase(activity.status, asking: activity.asking != nil)
    }

    static func phase(_ status: SessionStatus?, asking: Bool) -> AgentCard.Phase {
        if asking { return .asking }
        switch status {
        case .running: return .working
        case .idle, .waiting: return .resting
        default: return .settled
        }
    }

    private var all: [AgentCard] { cards.flatMap { [$0] + $0.subagents } }

    /// How many cards, subagents included, have settled.
    public var settled: Int { all.filter { $0.phase == .settled }.count }

    /// The tokens every card spent, subagents included.
    public var tokens: Int { all.compactMap(\.usage?.tokens).reduce(0, +) }
}

/// A count of tokens as a person reads it: 950, 12k, 1.1M.
public func tokenCount(_ n: Int) -> String {
    switch n {
    case ..<1000: "\(n)"
    case ..<1_000_000: "\(Int((Double(n) / 1000).rounded()))k"
    default: String(format: "%.1fM", Double(n) / 1_000_000)
    }
}

/// A span as a person reads it: 45s, 11m 10s, 2h 5m.
public func span(_ t: TimeInterval) -> String {
    let s = Int(t)
    if s < 60 { return "\(s)s" }
    if s < 3600 { return "\(s / 60)m \(s % 60)s" }
    return "\(s / 3600)h \((s % 3600) / 60)m"
}
