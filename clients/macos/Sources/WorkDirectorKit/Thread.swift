import Foundation

/// One step of a goal read as a conversation.
public enum ThreadStep: Sendable, Hashable, Identifiable {
    /// Something decided, said, asked, answered, reported, verified or
    /// landed, or a state change that ends work.
    case event(Event)
    /// The director asked the goal's supervisor to look, and why.
    case look(Event)
    /// The supervisor's reply, carried out.
    case supervisor(Event, SupervisorReply)
    /// A task's session stopped without a report, and what it last said.
    case stopped(Event, said: String)
    /// A question asked on the goal or a task, with its answer once it has one.
    case exchange(Event, answer: Event?)
    /// The director declined to answer part of a question put to the person, and why.
    case declined(Event, DirectorChoice)

    public var event: Event {
        switch self {
        case .event(let e), .look(let e), .supervisor(let e, _), .stopped(let e, _), .exchange(let e, _), .declined(let e, _): e
        }
    }

    public var id: Int { event.id }
}

/// What a supervisor said, what was done because of it and what was refused.
public struct SupervisorReply: Sendable, Hashable {
    /// Its words, without its action lines.
    public let said: String
    public let actions: [SupervisorAction]
    public let refused: [String]
}

/// One thing done on a supervisor's word.
public struct SupervisorAction: Sendable, Hashable {
    /// The action as the supervisor wrote it.
    public let line: String
    /// The task it told, started or filed, if any.
    public let task: String?
}

private let stoppedPrefix = "stopped without a report: "
private let supervisePrefix = "supervise "

extension GoalDetail {
    /// Whether a supervisor has been started on the goal.
    public var supervised: Bool {
        events.contains { $0.work == goal.id && $0.kind == "spawn" && $0.body.hasPrefix(supervisePrefix) }
    }

    /// The goal as one conversation, oldest first: each answer read with the
    /// question it answers.
    public var thread: [ThreadStep] {
        var steps: [ThreadStep] = []
        var open: [String: Int] = [:]
        // An answer reaches its session as a message too; it is read once, as the answer.
        let answers = Set(events.filter { $0.kind == "answer" }.map { "\($0.work)\n\($0.body)" })
        for e in events.sorted(by: { $0.at == $1.at ? $0.id < $1.id : $0.at < $1.at }) {
            if e.kind == "sent", answers.contains("\(e.work)\n\(e.body)") { continue }
            if e.kind == "question" {
                open[e.work] = steps.count
                steps.append(.exchange(e, answer: nil))
                continue
            }
            if e.kind == "answer", let i = open.removeValue(forKey: e.work), case .exchange(let q, nil) = steps[i] {
                steps[i] = .exchange(q, answer: e)
                continue
            }
            if let step = step(e) { steps.append(step) }
        }
        return steps
    }

    private func step(_ e: Event) -> ThreadStep? {
        switch e.kind {
        case "decision" where e.reshapes || e.directorDecided:
            return nil
        case "decision", "report", "sent", "answer", "verify", "pr", "judged":
            return .event(e)
        case "state":
            return ["done", "dropped", "abandoned", "paused", "blocked"].contains(e.body) ? .event(e) : nil
        case "look":
            return .look(e)
        case "supervised":
            return .supervisor(e, reply(e.body))
        case "note" where e.body.hasPrefix(stoppedPrefix):
            return .stopped(e, said: String(e.body.dropFirst(stoppedPrefix.count)))
        case "note":
            return e.directorDeclined.map { .declined(e, $0) }
        default:
            return nil
        }
    }

    /// A supervised event's body: the reply, then `Done:` and `Refused:`
    /// lists, each line `- ` and one action.
    func reply(_ body: String) -> SupervisorReply {
        var said: [String] = [], done: [String] = [], refused: [String] = []
        var section = 0
        var inActions = false
        for line in body.components(separatedBy: "\n") {
            switch line {
            case "Done:": section = 1; continue
            case "Refused:": section = 2; continue
            default: break
            }
            if section > 0 {
                guard line.hasPrefix("- ") else { continue }
                if section == 1 { done.append(String(line.dropFirst(2))) } else { refused.append(String(line.dropFirst(2))) }
                continue
            }
            if line.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "*")) == "ACTIONS:" {
                inActions = true
            }
            if !inActions { said.append(line) }
        }
        return SupervisorReply(
            said: said.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines),
            actions: done.map { SupervisorAction(line: $0, task: task(of: $0)) },
            refused: refused)
    }

    /// The task an action line acts on: the one it names, or the one it filed.
    func task(of line: String) -> String? {
        let ids = Set(tasks.map(\.id))
        for verb in ["SEND ", "START "] where line.hasPrefix(verb) {
            let rest = line.dropFirst(verb.count)
            let id = String(rest.prefix { $0 != ":" && $0 != " " })
            return ids.contains(id) ? id : nil
        }
        if line.hasPrefix("TASK: ") {
            let title = String(line.dropFirst("TASK: ".count))
            return tasks.last { $0.title == title }?.id
        }
        return nil
    }
}

extension GoalDetail {
    /// What the person said on the goal itself, oldest first: its decisions
    /// and answers.
    public var yours: [Event] {
        thread.compactMap { step in
            switch step {
            case .event(let e) where isYours(e): e
            case .exchange(_, let a?) where isYours(a): a
            default: nil
            }
        }
    }

    /// Whether the person said it: a decision on the goal, or an answer the
    /// director did not give.
    public func isYours(_ e: Event) -> Bool {
        switch e.kind {
        case "decision": e.work == goal.id
        case "answer": !e.answeredByDirector
        default: false
        }
    }
}

extension Event {
    /// Whether it is a decision the ledger files when a goal is reshaped or
    /// settled (a sub-goal added, edited, dropped, merged, split or ordered; a
    /// done-when, finish line, approval or classification), which belongs to
    /// the record rather than the conversation.
    public var reshapes: Bool {
        guard kind == "decision" else { return false }
        let id = "[0-9a-f]+"
        let patterns = ["^(added|edited|dropped|split) \(id): ", "^merged \(id) into ", "^\(id) waits on: ",
                        "^(done when|finishes at|approved): ", "^classified "]
        return patterns.contains { body.range(of: $0, options: .regularExpression) != nil }
    }

    /// Whether the director gave this answer, from what was decided or by its
    /// judgement, rather than the person.
    public var answeredByDirector: Bool { kind == "answer" && decision?.source == "director" }

    /// An answer's words, one `question → answer` line per item when it
    /// answers several.
    public var answerText: String {
        let lines = body.components(separatedBy: "\n")
        guard lines.first == "Answers to your questions:" else { return body }
        return lines.dropFirst().joined(separator: "\n")
    }

    /// A question's parts: each line a question, the `- ` lines under it its
    /// choices.
    public var askedItems: [(question: String, choices: [String])] {
        var items: [(question: String, choices: [String])] = []
        for raw in body.components(separatedBy: "\n") {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.isEmpty { continue }
            if line.hasPrefix("- "), !items.isEmpty {
                items[items.count - 1].choices.append(String(line.dropFirst(2)))
            } else {
                items.append((line, []))
            }
        }
        return items
    }
}

/// What a goal's conversation is narrowed to: everything, what passed
/// between the person and the director, or what was said at or under one
/// sub-goal.
public enum ThreadFilter: Hashable, Sendable {
    case all, yours
    case subGoal(String)
}

/// One row of a goal's conversation as it reads: a step, or a run of
/// updates folded into one line that opens.
public enum ThreadItem: Sendable, Hashable, Identifiable {
    case step(ThreadStep)
    case updates([ThreadStep])

    public var id: Int {
        switch self {
        case .step(let s): s.id
        case .updates(let run): run[0].id
        }
    }
}

extension GoalDetail {
    /// The conversation narrowed to one kind of turn. Everything folds each
    /// run of two or more updates — looks, state changes, checks, landings,
    /// messages told to a task — into one row.
    public func items(_ filter: ThreadFilter) -> [ThreadItem] {
        var out: [ThreadItem] = []
        var run: [ThreadStep] = []
        func flush() {
            if run.count == 1 { out.append(.step(run[0])) } else if run.count > 1 { out.append(.updates(run)) }
            run = []
        }
        for step in thread where shows(step, filter) {
            if filter == .all && isUpdate(step) {
                run.append(step)
                continue
            }
            flush()
            out.append(.step(step))
        }
        flush()
        return out
    }

    /// The chips a conversation is narrowed by: everything, you and the
    /// director, then each sub-goal directly under the goal.
    public var filters: [(filter: ThreadFilter, label: String, title: String)] {
        [(.all, "Everything", "Everything"), (.yours, "You and the director", "You and the director")]
            + tree.filter { !$0.group }.map { (.subGoal($0.id), Self.chip($0.work.title), $0.work.title) }
    }

    /// A chip's label: a title cut to 28 characters with an ellipsis.
    static func chip(_ title: String) -> String {
        title.count <= 28 ? title : title.prefix(27).trimmingCharacters(in: .whitespaces) + "…"
    }

    /// Where in the goal's tree work sits, by the titles down to it; nil for
    /// the goal itself.
    public func tag(of work: String) -> String? { paths[work] }

    /// Who a message from the goal's page can go to: the whole goal, or one
    /// sub-goal still open at any depth.
    public var addressees: [(id: String, title: String)] {
        [(goal.id, "whole goal")] + nodes.filter { !$0.group && !$0.work.atRest }.map { ($0.id, paths[$0.id] ?? $0.work.title) }
    }

    private func shows(_ step: ThreadStep, _ filter: ThreadFilter) -> Bool {
        switch filter {
        case .all:
            return true
        case .yours:
            switch step {
            case .event(let e): return e.work == goal.id || isYours(e) || e.answeredByDirector
            case .exchange(let q, let a): return q.work == goal.id || a.map { isYours($0) || $0.answeredByDirector } == true
            case .supervisor, .look, .declined: return true
            case .stopped: return false
            }
        case .subGoal(let id):
            return within(step.event.work, id)
        }
    }

    /// Whether work is the sub-goal or anywhere under it.
    private func within(_ work: String, _ id: String) -> Bool {
        guard work != id else { return true }
        guard let mine = paths[work], let theirs = paths[id] else { return false }
        return mine.hasPrefix(theirs + " › ")
    }

    private func isUpdate(_ step: ThreadStep) -> Bool {
        switch step {
        case .look: true
        case .event(let e): ["state", "verify", "pr", "sent"].contains(e.kind) && !finishes(e)
        default: false
        }
    }

    /// Whether the event is a sub-goal finishing, which reads as its own line.
    public func finishes(_ e: Event) -> Bool {
        e.kind == "state" && e.body == "done" && e.work != goal.id
    }

    /// Tasks waiting on the person with no question open in their session,
    /// each with what it waits on.
    public var waitingTasks: [(task: Work, question: Question)] {
        waiting.compactMap { w in tasks.first { $0.id == w.task }.map { ($0, w.question) } }
    }
}
