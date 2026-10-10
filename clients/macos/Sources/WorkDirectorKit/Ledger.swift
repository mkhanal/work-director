import Foundation

// The ledger as `wd serve --stdio` serves it. The app runs the wd bundled with
// it, so these types track exactly one version of the wire; a value they cannot
// read is a failure to report, not a shape to guess.

public enum WorkState: String, Codable, Sendable, CaseIterable {
    case queued
    case briefed
    case running
    case needsInput = "needs-input"
    case review
    case softDone = "soft-done"
    case done
    case blocked
    case dropped
    case abandoned
    case paused
}

public enum WorkKind: String, Codable, Sendable {
    case task
    case evolution
    case workflow
    case goal
    case epic
    case roadmap
    case item

    public var isGoal: Bool { self == .goal || self == .epic }
}

public enum GoalType: String, Codable, Sendable {
    case query, build, fix, change, review
}

/// Band is where an item sits on the board: what it is waiting on.
public enum Band: String, Codable, Sendable, CaseIterable {
    case needsYou = "needs-you"
    case inFlight = "in-flight"
    case paused
    case readyToPush = "ready-to-push"
    case inReview = "in-review"
    case readyToClose = "ready-to-close"
    case atRest = "at-rest"
}

public struct Work: Codable, Sendable, Hashable, Identifiable {
    public let id: String
    public let project: String
    public let title: String
    public let detail: String
    public let kind: WorkKind
    public let state: WorkState
    public let runner: String?
    public let session: String?
    public let ref: String?
    public let cwd: String?
    public let created: String
    public let updated: String
    public let parent: String?
    public let heading: String?
    public let claim: String?
    public let impact: String?
    public let goalType: GoalType?
    /// When the work was put out of sight; nil while it is on the board.
    public let archived: String?

    /// Work a person can still pause, tell or cancel.
    public var live: Bool { [.running, .needsInput, .review, .blocked, .queued, .briefed].contains(state) }

    public var atRest: Bool { state == .done || state == .dropped || state == .abandoned }

    enum CodingKeys: String, CodingKey {
        case id, project, title, detail, kind, state, runner, session, ref, cwd
        case created, updated, parent, heading, claim, impact, archived
        case goalType = "goal_type"
    }
}

public struct Decision: Codable, Sendable, Hashable {
    public let question: String
    public let answer: String
    public let source: String
    public let runner: String
    public let model: String
    public let tokens: Int
    public let reverses: Int
    public let reversedBy: Int

    enum CodingKeys: String, CodingKey {
        case question, answer, source, runner, model, tokens, reverses
        case reversedBy = "reversed_by"
    }
}

public struct Event: Codable, Sendable, Hashable, Identifiable {
    public let id: Int
    public let work: String
    public let kind: String
    public let body: String
    public let at: String
    public let effective: String?
    public let decision: Decision?
}

public struct Rollup: Codable, Sendable, Hashable {
    public let counts: [String: Int]
    public let done: Int
    public let total: Int
}

public struct BoardGoal: Codable, Sendable, Hashable {
    public let work: Work
    public let rollup: Rollup
    public let band: Band
}

public struct BoardItem: Codable, Sendable, Hashable {
    public let work: Work
    public let band: Band
}

public struct Board: Codable, Sendable, Hashable {
    public let goals: [BoardGoal]
    public let standalone: [BoardItem]

    /// Every item in one band, goals first, within one product when one is named.
    public func items(in band: Band, project: String? = nil) -> [Work] {
        let mine = { (w: Work) in project == nil || w.project == project }
        return goals.filter { $0.band == band && mine($0.work) }.map(\.work)
            + standalone.filter { $0.band == band && mine($0.work) }.map(\.work)
    }

    /// Goals on the board, most recently active first, within one product when one is named.
    public func recentGoals(project: String? = nil) -> [Work] {
        goals.map(\.work)
            .filter { project == nil || $0.project == project }
            .sorted { $0.updated > $1.updated }
    }

    public func rollup(of id: String) -> Rollup? {
        goals.first { $0.work.id == id }?.rollup
    }
}

public struct Claim: Codable, Sendable, Hashable {
    public let event: Event
    public let work: Work?
    public let stands: Bool
    public let reversed: String
}

public enum LandingKind: String, Codable, Sendable {
    case commit
    case pullRequest = "pull-request"
    case unknown = ""
}

public struct Landed: Codable, Sendable, Hashable {
    public let work: Work?
    public let kind: LandingKind
    public let url: String
    public let at: String
}

public struct Delivery: Codable, Sendable, Hashable {
    public let status: String
    public let detail: String
    public let attention: [String]?
}

public struct GoalDetail: Codable, Sendable, Hashable {
    public let goal: Work
    public let tasks: [Work]
    public let rollup: Rollup
    public let events: [Event]
    public let claims: [Claim]
    public let landings: [Landed]
    public let delivery: Delivery?
}

public struct ActionResult: Codable, Sendable, Hashable {
    public let code: Int
    public let stdout: String
    public let stderr: String
}

public struct Project: Codable, Sendable, Hashable, Identifiable {
    public let name: String
    public let path: String
    public let runner: String
    public let mode: String

    public var id: String { name }
}

public struct Unshipped: Codable, Sendable, Hashable {
    public let work: Work
    public let reason: String
    public let at: String
}

public struct PromotedCard: Codable, Sendable, Hashable {
    public let card: String
    public let text: String
    public let project: String?
    public let source: String
    public let at: String
}

public struct ReviewPass: Codable, Sendable, Hashable {
    public let project: String
    public let since: Int
    public let through: Int
    public let claims: [Claim]
    public let cards: [PromotedCard]
    public let unshipped: [Unshipped]
    public let landed: [Landed]
    public let reversals: Int

    public var isEmpty: Bool { claims.isEmpty && cards.isEmpty && unshipped.isEmpty && landed.isEmpty }
}

/// A goal just started: filed, planned, and its tasks spawned.
public struct Started: Codable, Sendable, Hashable {
    public let goal: Work
    public let tasks: [Work]
}

/// A goal a request may continue, and why the model thinks so.
public struct Found: Codable, Sendable, Hashable {
    public let goal: Work
    public let why: String
}
