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

    public static func release(_ id: String, reason: String) throws -> Plan {
        Plan(title: "Release", argv: ["release", id, try required(reason, for: "releasing")])
    }

    public static func decide(_ id: String, text: String) throws -> Plan {
        Plan(title: "Record decision", argv: ["decide", id, try required(text, for: "a decision")])
    }

    public static func abandon(_ id: String, detail: String) -> Plan {
        let detail = detail.trimmingCharacters(in: .whitespacesAndNewlines)
        return Plan(title: "Abandon", argv: ["abandon", id] + (detail.isEmpty ? [] : ["--reason", detail]))
    }

    public static func drive(_ id: String) -> Plan { Plan(title: "Drive", argv: ["drive", id]) }
    public static func softDone(_ id: String) -> Plan { Plan(title: "Soft-done", argv: ["soft-done", id]) }
    public static func done(_ id: String) -> Plan { Plan(title: "Done", argv: ["done", id]) }
    public static func verify(_ id: String) -> Plan { Plan(title: "Verify", argv: ["verify", id]) }
    public static func acknowledge(project: String) -> Plan {
        Plan(title: "Acknowledge review", argv: ["review", "--ack", "--project", project])
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
