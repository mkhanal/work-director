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

    public var event: Event {
        switch self {
        case .event(let e), .look(let e), .supervisor(let e, _), .stopped(let e, _), .exchange(let e, _): e
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
        case "decision", "report", "sent", "answer", "verify", "pr":
            return .event(e)
        case "state":
            return ["done", "dropped", "abandoned", "paused", "blocked"].contains(e.body) ? .event(e) : nil
        case "look":
            return .look(e)
        case "supervised":
            return .supervisor(e, reply(e.body))
        case "note" where e.body.hasPrefix(stoppedPrefix):
            return .stopped(e, said: String(e.body.dropFirst(stoppedPrefix.count)))
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

/// What a goal's conversation is narrowed to.
public enum ThreadFilter: String, CaseIterable, Sendable {
    case all, you, supervisor, reports, questions

    public var label: String {
        switch self {
        case .all: "Everything"
        case .you: "You"
        case .supervisor: "Supervisor"
        case .reports: "Reports"
        case .questions: "Questions"
        }
    }
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

/// How far a goal's tasks have come, by where each stands.
public struct TaskProgress: Sendable, Hashable {
    public var done = 0, working = 0, review = 0, blocked = 0, waiting = 0, dropped = 0
    public var total: Int { done + working + review + blocked + waiting + dropped }
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

    private func shows(_ step: ThreadStep, _ filter: ThreadFilter) -> Bool {
        switch (filter, step) {
        case (.all, _): true
        case (.you, .event(let e)): isYours(e)
        case (.you, .exchange(_, let a?)): isYours(a)
        case (.supervisor, .supervisor): true
        case (.supervisor, .exchange(let q, _)): q.work == goal.id
        case (.reports, .event(let e)): e.kind == "report"
        case (.questions, .exchange): true
        default: false
        }
    }

    private func isUpdate(_ step: ThreadStep) -> Bool {
        switch step {
        case .look: true
        case .event(let e): ["state", "verify", "pr", "sent"].contains(e.kind)
        default: false
        }
    }

    /// Where the goal's tasks stand.
    public var progress: TaskProgress {
        var p = TaskProgress()
        for t in tasks {
            switch t.state {
            case .done, .softDone: p.done += 1
            case .running, .needsInput: p.working += 1
            case .review: p.review += 1
            case .blocked: p.blocked += 1
            case .dropped, .abandoned: p.dropped += 1
            case .queued, .briefed, .paused: p.waiting += 1
            }
        }
        return p
    }

    /// The supervisor's latest reply, the freshest word on where the goal is.
    public var latestSupervision: (event: Event, reply: SupervisorReply)? {
        for step in thread.reversed() {
            if case .supervisor(let e, let r) = step { return (e, r) }
        }
        return nil
    }

    /// The tasks whose sessions are running now.
    public var working: [Work] {
        tasks.filter { activity[$0.id]?.status == .running }
    }

    /// What waits on the person: the goal's own question, each task's session
    /// question, and each task waiting on them some other way.
    public var waitingOnYou: Int {
        (asking == nil ? 0 : 1) + tasks.filter { activity[$0.id]?.asking != nil }.count + waitingTasks.count
    }

    /// Tasks waiting on the person with no question open in their session: what
    /// they last asked, or what their report said they need, to answer in words
    /// or by the choices offered.
    public var waitingTasks: [(task: Work, question: Question)] {
        tasks.compactMap { t in
            guard t.state == .needsInput, activity[t.id]?.asking == nil else { return nil }
            for e in events.reversed() where e.work == t.id {
                switch e.kind {
                case "answer", "decision":
                    return nil
                case "question":
                    let items = e.askedItems.map { QuestionItem(question: $0.question, options: $0.choices.map { QuestionOption(label: $0) }) }
                    return (t, Question(id: "event-\(e.id)", items: items))
                case "report":
                    let r = reports[String(e.id)]
                    let asked = [r?.said, r?.sections.first { $0.name == "NOTES" }?.text].compactMap { $0 }.first { !$0.isEmpty } ?? e.body
                    return (t, Question(id: "event-\(e.id)", items: [QuestionItem(question: asked)]))
                default:
                    continue
                }
            }
            return nil
        }
    }
}
