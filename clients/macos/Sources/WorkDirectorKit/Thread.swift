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

    public var event: Event {
        switch self {
        case .event(let e), .look(let e), .supervisor(let e, _), .stopped(let e, _): e
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

    /// The goal as one conversation, oldest first.
    public var thread: [ThreadStep] {
        events
            .sorted { $0.at == $1.at ? $0.id < $1.id : $0.at < $1.at }
            .compactMap { e -> ThreadStep? in
                switch e.kind {
                case "decision", "report", "sent", "answer", "verify", "pr", "question":
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
