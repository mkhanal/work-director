import Foundation

// What a goal page shows of the tree `wd` serves: each row's mark, badge and
// line, which steps start open, and the cards above the rows. Nothing here
// counts or judges; it only says the served values in words.

private let supersededPrefix = "superseded: "

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

    /// The line under its title: who has it and what holds it, or what it
    /// was planned into.
    public func meta(superseded: [SupersededWork] = []) -> String {
        if work.state == .superseded {
            let why = superseded.first { $0.work == work.id }?.why
                ?? whyNotMoving.map { $0.hasPrefix(supersededPrefix) ? String($0.dropFirst(supersededPrefix.count)) : $0 }
            return ["Superseded" + (why.map { ": " + $0 } ?? ""), "its work stays readable"].joined(separator: " · ")
        }
        var parts: [String] = []
        if let plannedInto, work.kind.isGoal || group {
            parts.append("Planned into \(plannedInto) sub-goal\(plannedInto == 1 ? "" : "s")")
            if let progress, progress.total > 0 { parts.append("\(progress.done) of \(progress.total) done") }
        } else {
            if work.state == .done, let doneWhen { parts.append("Done when " + doneWhen) }
            if let owner { parts.append(owner.agent ?? owner.runner) }
            if !waitingOn.isEmpty {
                parts.append("Waits on: " + waitingOn.joined(separator: " · "))
            } else if work.state == .needsInput, let whyNotMoving {
                parts.append(whyNotMoving)
            } else if let doing {
                parts.append("now: " + doing)
            } else if !steps.isEmpty {
                parts.append("\(steps.count) step\(steps.count == 1 ? "" : "s")")
            }
            if let judged { parts.append("judged " + judged.verdict) }
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

    /// The counts beside the Sub-goals header.
    public var progressLine: String {
        var line = "\(progress.done) of \(progress.total) done"
        if progress.superseded > 0 { line += " · \(progress.superseded) superseded" }
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

    /// The finish line as a page says it: "A complete PR", with where it was
    /// decided.
    public var finishWords: (line: String, source: String)? {
        guard let finish else { return nil }
        let line = finish.line.hasPrefix("complete") || finish.line.hasPrefix("local") ? "A " + finish.line : finish.line.prefix(1).uppercased() + finish.line.dropFirst()
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
    /// Its kind as a badge: the verdict for a course check or a judgement.
    public var badge: GoalBadge {
        let verdict = what.components(separatedBy: ": ").first ?? ""
        switch kind {
        case "decision": return GoalBadge("Decision", .decision)
        case "finish": return GoalBadge("Finish line", .decision)
        case "asked": return GoalBadge("Asked", .decision)
        case "course": return Course(verdict: verdict, why: "", at: at).badge
        case "question": return GoalBadge("Question", .attention)
        case "answer": return GoalBadge("Answered", .settled)
        case "judged": return verdict == "met" ? GoalBadge("Judged met", .settled) : GoalBadge("Judged \(verdict)", .attention)
        case "report": return GoalBadge("Report", .live)
        case "delivery": return GoalBadge("Delivered", .settled)
        case "chore": return GoalBadge("Chore", .waiting)
        case "superseded": return GoalBadge("Superseded", .waiting)
        default: return GoalBadge(kind.prefix(1).uppercased() + kind.dropFirst(), .waiting)
        }
    }

    /// What it says, without the verdict its badge already shows.
    public var text: String {
        guard kind == "course" || kind == "judged", let colon = what.range(of: ": ") else { return what }
        return String(what[colon.upperBound...])
    }

    /// One line for a list of what happened: its kind, then what it says.
    public var sentence: String { "\(badge.text): \(text)" }
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
