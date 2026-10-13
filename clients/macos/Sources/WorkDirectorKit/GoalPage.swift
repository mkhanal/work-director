import Foundation

// What a goal page shows of the tree `wd` serves: each row's mark, badge and
// line, which steps start open, and the cards above the rows. Nothing here
// counts or judges; it only says the served values in words.

private let supersededPrefix = "superseded: "

/// The views a goal is read in: where it stands and its sub-goals, what was
/// said, and what was decided and done.
public enum GoalView: String, CaseIterable, Sendable {
    case overview, conversation, record

    /// Whether the view has a composer under it: only the conversation does.
    public var composes: Bool { self == .conversation }

    public var label: String {
        switch self {
        case .overview: "Overview"
        case .conversation: "Conversation"
        case .record: "Record"
        }
    }
}

extension TreeNode {
    /// Its icon: a question below it marks it as needing the person.
    public var mark: StateMark { needsYou > 0 ? .needsYou : work.state.mark }

    /// The badge at the end of its row.
    public var badge: GoalBadge {
        if needsYou > 0 && work.state != .needsInput { return GoalBadge("\(needsYou) needs you", .attention) }
        let planned = steps.filter { $0.state != .dropped }
        if work.state.mark == .working && !planned.isEmpty {
            return GoalBadge("Working · \(planned.filter { $0.state == .done }.count) of \(planned.count)", .live)
        }
        return work.state.badge
    }

    /// The line under its title: who has it and what holds it, or how many
    /// sub-goals it holds.
    public func meta(superseded: [SupersededWork] = []) -> String {
        if work.state == .superseded {
            let why = superseded.first { $0.work == work.id }?.why
                ?? whyNotMoving.map { $0.hasPrefix(supersededPrefix) ? String($0.dropFirst(supersededPrefix.count)) : $0 }
            return Words.replaced(why: why)
        }
        var parts: [String] = []
        if let plannedInto, work.kind.isGoal || group {
            let total = progress?.total ?? plannedInto
            parts.append(Words.subGoals(total))
            if let progress, progress.total > 0 { parts.append("\(progress.done) of \(progress.total) done") }
            if let progress, progress.superseded > 0 { parts.append(Words.replaced(count: progress.superseded)) }
        } else {
            if work.state == .done, let doneWhen { parts.append("Done when " + doneWhen) }
            if let owner { parts.append(owner.agent ?? owner.runner) }
            if !waitingOn.isEmpty {
                parts.append("Waits on: " + waitingOn.joined(separator: " · "))
            } else if work.state == .needsInput, let whyNotMoving {
                parts.append(whyNotMoving)
            } else if let doing {
                parts.append(Words.now(doing))
            } else if !steps.isEmpty {
                parts.append("\(steps.count) step\(steps.count == 1 ? "" : "s")")
            }
            if let judged { parts.append(Words.checked(judged.verdict)) }
        }
        return parts.joined(separator: " · ")
    }

    /// Whether choosing its row folds its steps open and closed.
    public var folds: Bool { !opensAsGoal && !steps.isEmpty }
}

extension GoalDetail {
    /// The row whose steps start open: the first working sub-goal with steps.
    public var openSteps: String? {
        tree.first { $0.folds && $0.work.state.mark == .working }?.id
    }

    /// When a question was asked, read from the event that asked it.
    public func askedAt(_ question: Question) -> String? {
        guard question.id.hasPrefix("event-"), let n = Int(question.id.dropFirst("event-".count)) else { return nil }
        return events.first { $0.id == n }?.at
    }

    /// What makes the goal done, as the Done when card says it.
    public var doneWhenSentence: String? { doneWhen?.sentence }

    /// The counts beside the Sub-goals header.
    public var progressLine: String {
        var line = "\(progress.done) of \(progress.total) done"
        if progress.superseded > 0 { line += " · " + Words.replaced(count: progress.superseded) }
        return line
    }

    /// The badge beside the goal's title: what waits below it shows first.
    public var titleBadge: GoalBadge {
        !needsYou.isEmpty && goal.state != .done ? GoalBadge("Needs you", .attention) : goal.state.badge
    }

    /// The question's place in the tree, each level with the goal page it
    /// opens, if it is one.
    public func pathParts(of question: NeedYou) -> [(title: String, opens: String?)] {
        let branch = branch(to: question.work)
        guard !branch.isEmpty else { return [(question.path, nil)] }
        return branch.map { ($0.work.title, $0.opensAsGoal ? $0.work.id : nil) }
    }

    /// The finish line as a page says it: "A complete pull request", with where it was
    /// decided.
    public var finishWords: (line: String, source: String)? {
        guard let finish else { return nil }
        let line = Words.finishLine(finish.line)
        let source: String = switch finish.source {
        case "goal": "From what the goal asks."
        case "repo": "From the repo: its mode, its workflows and where its past goals finished."
        case "taste": "From your taste."
        case "person": "You chose it."
        default: "From " + finish.source + "."
        }
        return (line, source)
    }
}

extension RecordEntry {
    /// Its kind as a badge: the verdict for a direction check or a check of done-when.
    public var badge: GoalBadge {
        let verdict = what.components(separatedBy: ": ").first ?? ""
        switch kind {
        case "decision": return GoalBadge("Decision", .decision)
        case "finish": return GoalBadge("Finish line", .decision)
        case "asked": return GoalBadge("Asked", .decision)
        case "course": return Course(verdict: verdict, why: "", at: at).badge
        case "question": return GoalBadge("Question", .attention)
        case "answer": return GoalBadge("Answered", .settled)
        case "judged": return GoalBadge(Words.checked(verdict), verdict == "met" ? .settled : .attention)
        case "report": return GoalBadge("Report", .live)
        case "delivery": return GoalBadge("Delivered", .settled)
        case "chore": return GoalBadge("Housekeeping", .waiting)
        case "superseded": return GoalBadge(Words.replaced, .waiting)
        default: return GoalBadge(kind.prefix(1).uppercased() + kind.dropFirst(), .waiting)
        }
    }

    /// What it says, without the verdict its badge already shows.
    public var text: String {
        guard kind == "course" || kind == "judged", let colon = what.range(of: ": ") else { return what }
        return String(what[colon.upperBound...]).sentence
    }

    /// One line for a list of what happened: its kind, then what it says.
    public var sentence: String { badge.text + (badge.text.contains(":") ? " — " : ": ") + text }
}

extension String {
    /// Ledger text set in a sentence's place: its first letter capitalised.
    public var sentence: String { prefix(1).uppercased() + dropFirst() }
}

extension Course {
    /// The verdict as a badge.
    public var badge: GoalBadge {
        switch verdict {
        case "on course": GoalBadge("On course", .settled)
        case "correct": GoalBadge("Corrected", .live)
        case "pivot": GoalBadge("Pivoted", .attention)
        default: GoalBadge(verdict.prefix(1).uppercased() + verdict.dropFirst(), .waiting)
        }
    }
}
