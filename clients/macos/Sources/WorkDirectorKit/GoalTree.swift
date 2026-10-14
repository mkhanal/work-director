import Foundation

// A goal as a tree of sub-goals, as `wd serve --stdio` serves it. Everything
// here is read, never worked out: progress, paths, why a sub-goal is not
// moving and the record are computed in Go.

/// One level of a goal's breadcrumb.
public struct ChainLink: Codable, Sendable, Hashable, Identifiable {
    public let id: String
    public let title: String
    public let state: WorkState
}

/// Direct sub-goals done of total; superseded ones counted apart.
public struct TreeProgress: Codable, Sendable, Hashable {
    public let done: Int
    public let total: Int
    public let superseded: Int

    public init(done: Int, total: Int, superseded: Int) {
        self.done = done
        self.total = total
        self.superseded = superseded
    }
}

/// Where a goal finishes and who decided it: the goal, the repo, taste or the person.
public struct FinishLine: Codable, Sendable, Hashable {
    public let line: String
    public let source: String
}

/// The agent working a sub-goal.
public struct Owner: Codable, Sendable, Hashable {
    public let runner: String
    public let agent: String?
}

/// The latest judgement of a sub-goal's done-when.
public struct Judgement: Codable, Sendable, Hashable {
    public let verdict: String
    public let why: String
}

public enum PlanStepState: String, Codable, Sendable {
    case todo, doing, done, dropped
}

/// One step of an agent's own plan.
public struct PlanStep: Codable, Sendable, Hashable {
    public let n: Int
    public let text: String
    public let state: PlanStepState
}

/// One sub-goal in a goal's tree. A group is the tasks of an older plan that
/// share a heading, gathered under a row of their own.
public struct TreeNode: Codable, Sendable, Hashable, Identifiable {
    public let work: Work
    public let group: Bool
    public let doneWhen: String?
    public let after: [String]
    public let waitingOn: [String]
    public let whyNotMoving: String?
    public let owner: Owner?
    public let plannedInto: Int?
    public let progress: TreeProgress?
    public let doing: String?
    public let steps: [PlanStep]
    public let judged: Judgement?
    public let needsYou: Int
    /// Set while its work runs, or waits on the person, since its agent
    /// stopped without reporting.
    public let stopped: Stopped?
    /// Set while its agent's latest word is a reply to the person, until
    /// it is at rest: what it replied and when.
    public let replied: Stopped?
    /// Why blocked work is blocked, when wd knows.
    public let blockedBecause: String?
    public let children: [TreeNode]

    public var id: String { work.id }

    /// Whether choosing it opens a goal page of its own.
    public var opensAsGoal: Bool { work.kind.isGoal && !group }

    enum CodingKeys: String, CodingKey {
        case work, group, after, owner, progress, doing, steps, judged, children, stopped, replied
        case doneWhen = "done_when"
        case waitingOn = "waiting_on"
        case whyNotMoving = "why_not_moving"
        case plannedInto = "planned_into"
        case needsYou = "needs_you"
        case blockedBecause = "blocked_because"
    }
}

/// What running work last said when its agent stopped without reporting, and
/// when that was noted.
public struct Stopped: Codable, Sendable, Hashable {
    public let said: String
    public let at: String

    /// The first line of what it said.
    public var firstLine: String {
        said.split(separator: "\n").first.map(String.init) ?? ""
    }
}

/// A question somewhere under a goal that waits on the person.
public struct NeedYou: Codable, Sendable, Hashable, Identifiable {
    public let work: String
    public let path: String
    public let question: Question
    /// What answering it with its first choice would change; nil when nothing by itself.
    public let change: PendingChange?

    public var id: String { question.id }
}

/// The top goal's supervisor's latest verdict on whether it is on course.
public struct Course: Codable, Sendable, Hashable {
    public let verdict: String
    public let why: String
    public let at: String
}

/// A branch a course change let go of, and why.
public struct SupersededWork: Codable, Sendable, Hashable, Identifiable {
    public let work: String
    public let title: String
    public let why: String
    public let at: String

    public var id: String { work }
}

/// A goal's record counted by kind.
public struct RecordSummary: Codable, Sendable, Hashable {
    public let decisions: Int
    public let yours: Int
    public let course: Int
    public let corrections: Int
    public let questions: Int
    public let answeredByDirector: Int
    public let chores: Int
    public let judged: Int

    enum CodingKeys: String, CodingKey {
        case decisions, yours, course, corrections, questions, chores, judged
        case answeredByDirector = "answered_by_director"
    }
}

/// One thing on a goal's record. `event` is 0 for the goal's first ask.
public struct RecordEntry: Codable, Sendable, Hashable, Identifiable {
    public let event: Int
    public let at: String
    public let kind: String
    public let what: String
    public let `where`: String
    public let by: String

    public var id: String { "\(event)-\(kind)-\(at)" }
}

/// A goal's record, newest first, with its counts.
public struct GoalRecord: Codable, Sendable, Hashable {
    public let summary: RecordSummary
    public let entries: [RecordEntry]
}

extension GoalDetail {
    /// Every node of the tree, depth first, in the order served.
    public var nodes: [TreeNode] {
        func walk(_ ns: [TreeNode]) -> [TreeNode] { ns.flatMap { [$0] + walk($0.children) } }
        return walk(tree)
    }

    /// The nodes from the top of the tree down to the one holding work,
    /// empty when it is not in the tree.
    public func branch(to work: String) -> [TreeNode] {
        func find(_ ns: [TreeNode]) -> [TreeNode]? {
            for n in ns {
                if n.work.id == work { return [n] }
                if let below = find(n.children) { return [n] + below }
            }
            return nil
        }
        return find(tree) ?? []
    }
}
