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
    /// A goal a course change let go of; its history stays readable.
    case superseded
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

    public var atRest: Bool { state == .done || state == .dropped || state == .abandoned || state == .superseded }

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
    public let progress: TreeProgress
    /// Open questions in the goal's whole tree.
    public let needsYou: Int
    public let finish: FinishLine?
    /// Where a done goal finished, nil before.
    public let finished: String?
    /// Whether its breakdown waits for the person's approval.
    public let proposed: Bool

    enum CodingKeys: String, CodingKey {
        case work, rollup, band, progress, finish, finished, proposed
        case needsYou = "needs_you"
    }
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
    /// What could not be read; the status says only what could.
    public let error: String?
}

public struct GoalDetail: Codable, Sendable, Hashable {
    public let goal: Work
    public let tasks: [Work]
    public let rollup: Rollup
    public let events: [Event]
    public let claims: [Claim]
    public let landings: [Landed]
    public let delivery: Delivery?
    /// What each task still working is doing now, by task id.
    public let activity: [String: Activity]
    /// The question the goal waits on the person for, with its choices.
    public let asking: Question?
    /// Each report read into its parts, by the id of the event that filed it.
    public let reports: [String: Report]
    /// Each task in needs-input whose session asks nothing, with what it waits
    /// on as a question.
    public let waiting: [WaitingTask]
    /// The breadcrumb, top goal first, ending at this goal.
    public let chain: [ChainLink]
    /// The goal's sub-goals, and every work under it named by its place in them.
    public let tree: [TreeNode]
    public let paths: [String: String]
    public let doneWhen: String?
    /// Where the top goal finishes.
    public let finish: FinishLine?
    public let progress: TreeProgress
    /// What holds the goal back from done, empty once done.
    public let notDoneBecause: [String]
    /// Every open question on the goal or under it.
    public let needsYou: [NeedYou]
    public let course: Course?
    public let superseded: [SupersededWork]
    /// Whether the goal's breakdown waits for the person's approval.
    public let proposed: Bool
    /// Why its latest planning broke it down so; nil when the planner said nothing.
    public let reasoning: Reasoning?
    /// The files attached under the goal, oldest first.
    let attachments: [Attachment]?
    /// The events holding what the person said that the director has not
    /// been given yet; nil from a `wd` that does not say.
    let waitingForDirector: [Int]?

    enum CodingKeys: String, CodingKey {
        case goal, tasks, rollup, events, claims, landings, delivery, activity, asking, reports, waiting
        case chain, tree, paths, finish, progress, course, superseded, proposed, reasoning, attachments
        case doneWhen = "done_when"
        case notDoneBecause = "not_done_because"
        case needsYou = "needs_you"
        case waitingForDirector = "waiting_for_director"
    }

    /// Whether what event `id` holds still waits for the director to finish
    /// what it is doing.
    public func waitsForDirector(_ id: Int) -> Bool { waitingForDirector?.contains(id) == true }
}

public struct WaitingTask: Codable, Sendable, Hashable {
    public let task: String
    public let question: Question
}

/// A task's report read into its parts, as wd serves it.
public struct Report: Codable, Sendable, Hashable {
    /// `done`, `blocked`, `needs-input`, or empty when the report gave none.
    public let verdict: String
    /// What the report said with its verdict.
    public let said: String
    public let files: [String]
    /// What the report said about its files beyond naming them.
    public let fileNote: String
    public let sections: [ReportSection]

    enum CodingKeys: String, CodingKey {
        case verdict, said, files, sections
        case fileNote = "file_note"
    }
}

public struct ReportSection: Codable, Sendable, Hashable {
    public let name: String
    public let text: String
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

/// The next gate on work's way to done, as wd works it out: verify, land,
/// soft-done or done, with the argv that passes it. Empty when there is none.
public struct Gate: Codable, Sendable, Hashable {
    public let work: String
    public let gate: String
    public let title: String?
    /// The button's words, in plain language, once wd serves them.
    public let label: String?
    public let argv: [String]
    public let why: String?
}

/// What `wd doctor` says the host offers: which runners are on PATH.
public struct Doctor: Codable, Sendable, Hashable {
    public struct Runner: Codable, Sendable, Hashable, Identifiable {
        public let runner: String
        public let detected: Bool
        public var id: String { runner }
    }
    public let runners: [Runner]
}

/// The taste every brief carries, and what feedback has gathered toward new
/// rules, as `wd taste` gives it.
public struct Taste: Codable, Sendable, Hashable {
    public struct Card: Codable, Sendable, Hashable, Identifiable {
        public let id: String
        public let title: String
        public let category: String
        public let scope: [String]
        public let statement: String
        public let always: Bool
        /// built-in, a checkout's cards directory, or yours.
        public let source: String
    }
    public struct Learned: Codable, Sendable, Hashable, Identifiable {
        public let key: String
        public let count: Int
        public let texts: [String]
        public var id: String { key }
    }
    public let cards: [Card]
    public let learned: [Learned]
}
