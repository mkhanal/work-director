import Foundation

public enum PlanError: Error, Equatable, CustomStringConvertible {
    case reasonRequired(String)
    case unclosedQuote
    case empty

    public var description: String {
        switch self {
        case .reasonRequired(let what): "\(what) needs a reason"
        case .unclosedQuote: "a quote is not closed"
        case .empty: "nothing to run"
        }
    }
}

/// Plan is one write the person is asked to confirm: the exact argv `wd` runs.
public struct Plan: Hashable, Sendable, Identifiable {
    public let title: String
    public let argv: [String]

    public var id: [String] { argv }

    /// The command as the confirmation shows it, quoted so it reads back as typed.
    public var commandLine: String {
        (["wd"] + argv.map(Self.quoted)).joined(separator: " ")
    }

    /// Quotes a word that would not read back as one word. A word holding both
    /// quote characters is shown in single quotes and cannot be typed back in;
    /// the argv it stands for is still exactly the one that runs.
    private static func quoted(_ word: String) -> String {
        let plain = !word.isEmpty && word.allSatisfy { $0.isLetter || $0.isNumber || "-_./:=@,+%".contains($0) }
        if plain { return word }
        return word.contains("'") && !word.contains("\"") ? "\"\(word)\"" : "'\(word)'"
    }

    /// Text typed in the command bar, split into words without a shell.
    public static func typed(_ text: String) throws -> Plan {
        var argv = try split(text)
        if argv.first == "wd" { argv.removeFirst() }
        guard !argv.isEmpty else { throw PlanError.empty }
        return Plan(title: "Run", argv: argv)
    }

    public static func reopen(_ id: String, reason: String) throws -> Plan {
        Plan(title: "Reopen", argv: ["reopen", id, try required(reason, for: "reopening")])
    }

    public static func release(_ id: String, reason: String = "") -> Plan {
        Plan(title: "Release", argv: ["release", id] + optional(reason))
    }

    public static func decide(_ id: String, text: String) throws -> Plan {
        Plan(title: "Record decision", argv: ["decide", id, try required(text, for: "a decision")])
    }

    public static func abandon(_ id: String, detail: String) -> Plan {
        let detail = detail.trimmingCharacters(in: .whitespacesAndNewlines)
        return Plan(title: "Abandon", argv: ["abandon", id] + (detail.isEmpty ? [] : ["--reason", detail]))
    }

    /// Starts a goal from what the person typed: wd files it, plans it and
    /// spawns its tasks.
    public static func start(project: String, text: String) throws -> Plan {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { throw PlanError.empty }
        // Planning runs in its own process, so the goal opens while it is
        // planned and its planning can be watched and stopped.
        return Plan(title: "Start goal", argv: ["goal", "start", project, text, "--detach", "--json"])
    }

    /// Picks a goal up where it stands with what the person typed.
    public static func continueGoal(_ id: String, text: String) throws -> Plan {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { throw PlanError.empty }
        return Plan(title: "Continue goal", argv: ["goal", "continue", id, text, "--json"])
    }

    /// Archives work. Work at rest needs no reason; open work is dropped first,
    /// which is a choice someone has to state.
    public static func archive(_ work: Work, reason: String = "") -> Plan {
        Plan(title: work.atRest ? "Archive" : "Drop and archive", argv: ["archive", work.id] + optional(reason))
    }

    public static func unarchive(_ id: String) -> Plan { Plan(title: "Unarchive", argv: ["unarchive", id]) }

    /// Something said to work while it runs: a goal passes it to every open
    /// task and keeps it as a decision; a running session gets it when idle.
    public static func send(_ id: String, text: String) throws -> Plan {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { throw PlanError.empty }
        return Plan(title: "Send", argv: ["send", id, text])
    }

    /// A new title a person chose; a goal renamed keeps it when it is planned.
    public static func rename(_ id: String, title: String) throws -> Plan {
        let title = title.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !title.isEmpty else { throw PlanError.empty }
        return Plan(title: "Rename", argv: ["rename", id, title])
    }

    /// The answers to the question a session is waiting on, one per item in
    /// the order asked.
    public static func answer(_ id: String, _ question: Question, answers: [String]) throws -> Plan {
        let answers = answers.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        guard answers.count == question.items.count, !answers.contains(where: \.isEmpty) else { throw PlanError.empty }
        return Plan(title: "Answer", argv: ["answer", id] + answers)
    }

    /// Interrupts what work's sessions are doing and takes back the messages
    /// they have not read; the work keeps its state.
    public static func stop(_ id: String) -> Plan { Plan(title: "Stop", argv: ["stop", id, "--json"]) }

    /// The write that passes a gate, exactly as wd served it.
    public static func passing(_ gate: Gate) -> Plan? {
        gate.argv.isEmpty ? nil : Plan(title: gate.title ?? gate.gate, argv: gate.argv)
    }

    /// Keeps something said to work as feedback, the evidence rules are
    /// distilled from.
    public static func remember(_ text: String, work: Work) throws -> Plan {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { throw PlanError.empty }
        return Plan(title: "Remember", argv: ["feedback", "add", text, "--work", work.id, "--project", work.project, "--source", "director"])
    }

    /// Files feedback on no work in particular: a rule-to-be in a person's words.
    public static func feedback(_ text: String, project: String?) throws -> Plan {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { throw PlanError.empty }
        return Plan(title: "Feedback", argv: ["feedback", "add", text] + (project.map { ["--project", $0] } ?? []))
    }

    /// Registers a folder as a product the director works in.
    public static func addProduct(name: String, path: String, runner: String, mode: String) throws -> Plan {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, !path.isEmpty, !runner.isEmpty else { throw PlanError.empty }
        return Plan(title: "Add product", argv: ["projects", "add", name, path, "--runner", runner, "--mode", mode, "--lazyspec", "n", "--json"])
    }

    /// The name a product in a folder is offered: the folder's own name, in
    /// lower case, its words joined by hyphens.
    public static func productName(forFolder path: String) -> String {
        let base = (path as NSString).lastPathComponent.lowercased()
        let words = base.split { !$0.isLetter && !$0.isNumber }
        return words.joined(separator: "-")
    }

    /// Opens the directory work runs in, in the editor wd finds.
    public static func open(_ id: String) -> Plan { Plan(title: "Open", argv: ["open", id, "."]) }

    public static func pause(_ id: String) -> Plan { Plan(title: "Pause", argv: ["pause", id]) }

    public static func resume(_ id: String, note: String) -> Plan {
        let note = note.trimmingCharacters(in: .whitespacesAndNewlines)
        return Plan(title: "Resume", argv: ["resume", id] + (note.isEmpty ? [] : [note]))
    }

    /// Stops and drops work; `resume` brings it back, so it asks nothing.
    public static func cancel(_ id: String, reason: String = "") -> Plan {
        Plan(title: "Cancel", argv: ["cancel", id] + optional(reason))
    }

    /// The write that takes this one back, when there is one: a cancel or a
    /// pause is resumed, an archive unarchived.
    public var undo: Plan? {
        guard argv.count >= 2 else { return nil }
        let id = argv[1]
        switch argv[0] {
        case "cancel", "pause": return Plan.resume(id, note: "")
        case "archive": return Plan.unarchive(id)
        default: return nil
        }
    }

    public static func drive(_ id: String) -> Plan { Plan(title: "Drive", argv: ["drive", id]) }
    public static func softDone(_ id: String) -> Plan { Plan(title: "Soft-done", argv: ["soft-done", id]) }
    public static func done(_ id: String) -> Plan { Plan(title: "Done", argv: ["done", id]) }
    public static func verify(_ id: String) -> Plan { Plan(title: "Verify", argv: ["verify", id]) }
    public static func acknowledge(project: String) -> Plan {
        Plan(title: "Acknowledge review", argv: ["review", "--ack", "--project", project])
    }

    private static func optional(_ text: String) -> [String] {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return text.isEmpty ? [] : [text]
    }

    private static func required(_ text: String, for what: String) throws -> String {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { throw PlanError.reasonRequired(what) }
        return text
    }

    /// Splits on whitespace; single or double quotes keep a phrase together.
    /// Nothing is expanded.
    static func split(_ text: String) throws -> [String] {
        var words: [String] = []
        var word = ""
        var inWord = false
        var quote: Character?
        for ch in text {
            if let q = quote {
                if ch == q { quote = nil } else { word.append(ch) }
            } else if ch == "\"" || ch == "'" {
                quote = ch
                inWord = true
            } else if ch.isWhitespace {
                if inWord { words.append(word); word = ""; inWord = false }
            } else {
                word.append(ch)
                inWord = true
            }
        }
        if quote != nil { throw PlanError.unclosedQuote }
        if inWord { words.append(word) }
        return words
    }
}
