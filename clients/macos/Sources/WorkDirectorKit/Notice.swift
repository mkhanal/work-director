import Foundation

/// The board and, for each goal still moving or asking, its goal view: what
/// two reads in a row are compared on to tell the person what happened.
public struct Sighting: Sendable, Hashable {
    public let board: Board
    /// Goal views by top goal id; a goal at rest with nothing asked has none.
    public let goals: [String: GoalDetail]

    public init(board: Board, goals: [String: GoalDetail]) {
        self.board = board
        self.goals = goals
    }

    /// Which goals' views to read for `board`, and the views the earlier
    /// sighting already holds that nothing has changed since. A goal is read
    /// again when its board row changed or an event touched work under it.
    public static func reads(for board: Board, after earlier: Sighting?, touched: Set<String>) -> (read: [String], kept: [String: GoalDetail]) {
        var read: [String] = []
        var kept: [String: GoalDetail] = [:]
        for g in board.goals where !g.work.atRest || g.needsYou > 0 || g.proposed {
            let id = g.work.id
            if let earlier, let old = earlier.goals[id], earlier.board.goals.first(where: { $0.work.id == id }) == g,
               touched.isDisjoint(with: old.under) {
                kept[id] = old
            } else {
                read.append(id)
            }
        }
        return (read, kept)
    }
}

extension GoalDetail {
    /// The goal and every work under it.
    var under: Set<String> {
        Set([goal.id] + tasks.map(\.id) + paths.keys + nodes.map(\.work.id))
    }
}

/// One thing to tell the person outside the window: what happened, to which
/// goal, in plain words.
public struct Notice: Sendable, Hashable {
    public enum Kind: String, Sendable {
        case question, plan, course, finish, planChange, blocked, stopped, finished, review
    }

    /// One stop of one work, told once.
    static func stoppedKey(_ work: String, _ stopped: Stopped) -> String { "stopped:\(work):\(stopped.at)" }

    public let kind: Kind
    public let title: String
    public let body: String
    /// The goal, or standalone work, a click opens.
    public let work: String
    /// What it tells, so the same thing is never told twice; the first names it.
    public let keys: [String]
    /// The work and questions it is about, so a write by the person to any of
    /// them keeps it quiet.
    public let about: Set<String>

    /// How long after the person's own write what it changed stays untold.
    public static let quietFor: TimeInterval = 5

    /// What happened between two sightings. With no earlier sighting there is
    /// nothing to compare, so nothing has happened. A notice whose keys were
    /// all told, or that is about anything in `quiet`, is left out.
    public static func between(_ earlier: Sighting?, _ now: Sighting, told: Set<String> = [], quiet: Set<String> = []) -> [Notice] {
        guard let earlier else { return [] }
        var out: [Notice] = []
        let beforeGoals = Dictionary(earlier.board.goals.map { ($0.work.id, $0) }, uniquingKeysWith: { a, _ in a })
        let beforeItems = Dictionary(earlier.board.standalone.map { ($0.work.id, $0) }, uniquingKeysWith: { a, _ in a })

        for g in now.board.goals {
            let w = g.work
            let before = beforeGoals[w.id]
            var said: [Notice] = []
            let detail = now.goals[w.id]

            if g.proposed && !(before?.proposed ?? false) {
                let n = g.progress.total
                said.append(Notice(kind: .plan, title: "Approve the plan",
                                   body: n > 0 ? "\(w.title) · \(n) sub-goal\(n == 1 ? "" : "s")" : w.title,
                                   work: w.id, keys: ["plan:\(w.id):\(w.updated)"], about: [w.id]))
            }

            if let detail, let known = earlier.knownQuestions(w.id) {
                let fresh = detail.needsYou.filter { !known.contains($0.question.id) && $0.question.pending }
                // A stop waiting on the person is told as a stop, not again as a question.
                let stopped = Set(detail.nodes.filter { $0.stopped != nil }.map(\.work.id))
                let asked = fresh.filter { $0.change == nil && !stopped.contains($0.work) }
                let items = asked.flatMap(\.question.items)
                if let first = items.first {
                    var lines = [w.title, first.head]
                    if items.count > 1 { lines.append("\(items.count - 1) more question\(items.count == 2 ? "" : "s")") }
                    said.append(Notice(kind: .question, title: "Question for you", body: lines.joined(separator: "\n"),
                                       work: w.id, keys: asked.map { "question:\($0.question.id)" },
                                       about: Set([w.id] + asked.map(\.work) + asked.map(\.question.id))))
                }
                for ask in fresh {
                    guard let change = ask.change else { continue }
                    let (kind, title, body): (Kind, String, String) = switch change.kind {
                    case .course: (.course, "Change of course proposed", change.proposed.map { "\(w.title) — \($0.title)" } ?? w.title)
                    case .finish: (.finish, "Choose where it finishes", w.title)
                    case .plan: (.planChange, "Plan change to approve", w.title)
                    }
                    said.append(Notice(kind: kind, title: title, body: body, work: w.id,
                                       keys: ["change:\(ask.question.id)"], about: [w.id, ask.work, ask.question.id]))
                }
            }

            if w.state == .blocked && before != nil && before?.work.state != .blocked {
                said.append(Notice(kind: .blocked, title: "Blocked", body: w.title, work: w.id,
                                   keys: ["blocked:\(w.id):\(w.updated)"], about: [w.id]))
            }
            if let detail, let known = earlier.knownBlocked(w.id) {
                for node in detail.nodes where node.work.state == .blocked && !known.contains(node.work.id) {
                    let lines = ["\(w.title) — \(node.work.title)"] + (node.blockedBecause.map { $0.isEmpty ? [] : [$0.sentence] } ?? [])
                    said.append(Notice(kind: .blocked, title: "Blocked", body: lines.joined(separator: "\n"), work: w.id,
                                       keys: ["blocked:\(node.work.id):\(node.work.updated)"], about: [w.id, node.work.id]))
                }
            }
            if let detail, let known = earlier.knownStopped(w.id) {
                for node in detail.nodes {
                    guard let stopped = node.stopped, !known.contains(Self.stoppedKey(node.work.id, stopped)) else { continue }
                    let lines = ["\(w.title) — \(node.work.title)"] + (stopped.firstLine.isEmpty ? [] : [stopped.firstLine])
                    said.append(Notice(kind: .stopped, title: "Stopped without finishing", body: lines.joined(separator: "\n"), work: w.id,
                                       keys: [Self.stoppedKey(node.work.id, stopped)], about: [w.id, node.work.id]))
                }
            }

            if w.state == .done, let before, before.work.state != .done {
                said.append(Notice(kind: .finished, title: "Finished", body: g.finished.map { "\(w.title) — \($0)" } ?? w.title,
                                   work: w.id, keys: ["finished:\(w.id):\(w.updated)"], about: [w.id]))
            }

            if Self.forReview.contains(g.band) && !Self.forReview.contains(before?.band ?? .atRest) {
                said.append(Notice(kind: .review, title: "Ready for review", body: w.title, work: w.id,
                                   keys: ["review:\(w.id):\(w.updated)"], about: [w.id]))
            }

            // Work came to wait on the person with nothing above to say why:
            // a session waiting without asking, or a question not yet read.
            if said.isEmpty && g.band == .needsYou && before.map({ $0.band != .needsYou }) ?? true {
                said.append(Notice(kind: .question, title: "Question for you", body: w.title, work: w.id,
                                   keys: ["waiting:\(w.id):\(w.updated)"], about: [w.id]))
            }
            out += said
        }

        for item in now.board.standalone {
            let w = item.work
            let before = beforeItems[w.id]
            if w.state == .needsInput && before?.work.state != .needsInput {
                out.append(Notice(kind: .question, title: "Question for you", body: w.title, work: w.id,
                                  keys: ["question:\(w.id):\(w.updated)"], about: [w.id]))
            }
            if w.state == .blocked && before?.work.state != .blocked {
                out.append(Notice(kind: .blocked, title: "Blocked", body: w.title, work: w.id,
                                  keys: ["blocked:\(w.id):\(w.updated)"], about: [w.id]))
            }
            if Self.forReview.contains(item.band) && !Self.forReview.contains(before?.band ?? .atRest) {
                out.append(Notice(kind: .review, title: "Ready for review", body: w.title, work: w.id,
                                  keys: ["review:\(w.id):\(w.updated)"], about: [w.id]))
            }
        }

        return out.filter { !$0.keys.allSatisfy(told.contains) && $0.about.isDisjoint(with: quiet) }
    }

    /// The bands where work waits for the person to look it over and land it.
    static let forReview: Set<Band> = [.readyToPush, .inReview, .readyToClose]

    /// The words of the person's writes still running, or ended within
    /// `quietFor` of `now`: the ids they name.
    public static func quiet(running: [Plan], ended: [(plan: Plan, at: Date)], now: Date) -> Set<String> {
        let recent = ended.filter { now.timeIntervalSince($0.at) < quietFor }.map(\.plan)
        return Set((running + recent).flatMap(\.argv))
    }
}

extension Sighting {
    /// The question ids open under a goal at this sighting; nil when it had
    /// questions this sighting did not read, so none can be called new.
    func knownQuestions(_ id: String) -> Set<String>? {
        if let d = goals[id] { return Set(d.needsYou.map(\.question.id)) }
        if let g = board.goals.first(where: { $0.work.id == id }), g.needsYou > 0 { return nil }
        return []
    }

    /// The work blocked under a goal at this sighting; nil when it may have had
    /// some this sighting did not read.
    func knownBlocked(_ id: String) -> Set<String>? {
        if let d = goals[id] { return Set(d.nodes.filter { $0.work.state == .blocked }.map(\.work.id)) }
        if let g = board.goals.first(where: { $0.work.id == id }), (g.rollup.counts["blocked"] ?? 0) > 0 || !g.work.atRest {
            return nil
        }
        return []
    }

    /// The stops already seen under a goal at this sighting; nil when work
    /// under it may have stopped where this sighting did not read.
    func knownStopped(_ id: String) -> Set<String>? {
        if let d = goals[id] { return Set(d.nodes.compactMap { n in n.stopped.map { Notice.stoppedKey(n.work.id, $0) } }) }
        if let g = board.goals.first(where: { $0.work.id == id }), !g.work.atRest { return nil }
        return []
    }
}
