import Foundation

/// The director's judgement on one part of a question put to the person: its
/// answer and why, or why it declined to answer.
public struct DirectorChoice: Codable, Sendable, Hashable {
    public let question: String
    /// Nil when it declined.
    public let answer: String?
    public let why: String

    public init(question: String, answer: String?, why: String) {
        self.question = question
        self.answer = answer
        self.why = why
    }
}

/// What `wd answer <id> --director --json` did: answered every part, or
/// declined some and answered nothing.
public struct DirectorOutcome: Codable, Sendable, Hashable {
    public let answered: Bool
    public let decided: [DirectorChoice]
    public let declined: [DirectorChoice]

    public static func read(_ stdout: String) throws -> DirectorOutcome {
        try JSONDecoder().decode(DirectorOutcome.self, from: Data(stdout.utf8))
    }
}

private let decidedPrefix = "the director decided "
private let declinedPrefix = "the director declined "

extension Event {
    /// Whether it is the director's decision on one part of a question it
    /// answered in the person's place, which is read with that answer.
    public var directorDecided: Bool {
        kind == "decision" && decision?.source == "director" && body.hasPrefix(decidedPrefix)
    }

    /// The part of a question the director declined to answer, and why.
    public var directorDeclined: DirectorChoice? {
        guard kind == "note", body.hasPrefix(declinedPrefix),
              let (question, rest) = Self.quoted(body.dropFirst(declinedPrefix.count)), rest.hasPrefix(": ") else { return nil }
        return DirectorChoice(question: question, answer: nil, why: String(rest.dropFirst(2)))
    }

    /// A Go-quoted string at the start of text, and what follows it.
    static func quoted(_ text: Substring) -> (String, Substring)? {
        guard text.first == "\"" else { return nil }
        var out = ""
        var i = text.index(after: text.startIndex)
        while i < text.endIndex {
            let ch = text[i]
            if ch == "\"" { return (out, text[text.index(after: i)...]) }
            if ch == "\\" {
                i = text.index(after: i)
                guard i < text.endIndex else { return nil }
                switch text[i] {
                case "n": out.append("\n")
                case "t": out.append("\t")
                default: out.append(text[i])
                }
            } else {
                out.append(ch)
            }
            i = text.index(after: i)
        }
        return nil
    }
}

extension GoalDetail {
    /// Each part the director decided for an answer it gave, with why: its
    /// decisions on the same work after the answer, before the next question.
    public func decided(for answer: Event) -> [DirectorChoice] {
        let next = events.filter { $0.work == answer.work && $0.kind == "question" && $0.id > answer.id }.map(\.id).min() ?? .max
        return events
            .filter { $0.work == answer.work && $0.directorDecided && $0.id > answer.id && $0.id < next }
            .compactMap { e in
                guard let d = e.decision else { return nil }
                let mark = ": \(d.answer), because "
                let why = e.body.range(of: mark).map { String(e.body[$0.upperBound...]) } ?? ""
                return DirectorChoice(question: d.question, answer: d.answer, why: why)
            }
    }
}
