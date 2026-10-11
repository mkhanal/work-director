import Foundation
import Observation

// A work item's session as `wd serve --stdio` serves it: what it was told,
// what it said and thought, the tools it called and the questions it asked.

public enum StepKind: String, Codable, Sendable {
    case prompt, text, thinking, tool, question
}

public struct ToolCall: Codable, Sendable, Hashable {
    public let id: String
    public let name: String
    public let summary: String
    public let input: String
    /// Nil while the tool is still running.
    public let result: String?
    public let failed: Bool
}

public struct QuestionOption: Codable, Sendable, Hashable {
    public let label: String
    public let description: String
}

public struct QuestionItem: Codable, Sendable, Hashable {
    public let header: String
    public let question: String
    public let multi: Bool
    public let options: [QuestionOption]
}

public struct Question: Codable, Sendable, Hashable, Identifiable {
    public let id: String
    public let items: [QuestionItem]
    /// One answer per item once answered.
    public let answers: [String]?
    /// What the session was told instead, when that was not a set of answers.
    public let reply: String?

    public var pending: Bool { answers == nil && reply == nil }

    public init(id: String, items: [QuestionItem], answers: [String]? = nil, reply: String? = nil) {
        self.id = id
        self.items = items
        self.answers = answers
        self.reply = reply
    }
}

public struct Step: Codable, Sendable, Hashable {
    public let kind: StepKind
    public let at: String?
    public let text: String?
    public let tool: ToolCall?
    public let question: Question?

    /// A step that can still change: a tool waiting for its result, a question
    /// waiting for its answer.
    public var settled: Bool {
        switch kind {
        case .tool: tool?.result != nil
        case .question: question?.pending == false
        default: true
        }
    }
}

/// What a session reports about itself: working, idle, waiting on a person,
/// gone, or no session at all.
public enum SessionStatus: String, Codable, Sendable {
    case running, idle, waiting, exited, unknown, none
}

public struct ConversationPage: Codable, Sendable, Hashable {
    public let work: String
    public let runner: String?
    public let session: String?
    public let status: SessionStatus
    public let from: Int
    public let total: Int
    public let entries: [Step]
    public let asking: Question?
}

/// What a task's session is doing now, in a line.
public struct Activity: Codable, Sendable, Hashable {
    public let now: String?
    public let kind: StepKind?
    public let at: String?
    public let asking: Question?
    public let error: String?
}

/// One row of a conversation as it reads: a step on its own, or a run of tool
/// calls folded into one row.
public enum ThreadRow: Hashable, Sendable, Identifiable {
    case step(index: Int, Step)
    case tools(first: Int, [ToolCall])

    public var id: Int {
        switch self {
        case .step(let i, _): i
        case .tools(let first, _): first
        }
    }

    /// Steps in conversation order, with every run of two or more tool calls
    /// folded into one row: the calls between two things said are one piece of
    /// work to a reader.
    public static func rows(_ steps: [Step]) -> [ThreadRow] {
        var out: [ThreadRow] = []
        var run: [ToolCall] = []
        var runStart = 0
        func flush() {
            if run.count == 1 {
                out.append(.step(index: runStart, steps[runStart]))
            } else if run.count > 1 {
                out.append(.tools(first: runStart, run))
            }
            run = []
        }
        for (i, step) in steps.enumerated() {
            if step.kind == .tool, let tool = step.tool {
                if run.isEmpty { runStart = i }
                run.append(tool)
                continue
            }
            flush()
            out.append(.step(index: i, step))
        }
        flush()
        return out
    }
}

/// A person's answer to a question while they pick it: the options chosen
/// for each item and anything typed instead.
public struct AnswerDraft: Sendable, Hashable {
    public private(set) var picked: [Set<String>]
    public var typed: [String]
    public let question: Question

    public init(_ question: Question) {
        self.question = question
        picked = Array(repeating: [], count: question.items.count)
        typed = Array(repeating: "", count: question.items.count)
    }

    /// Picks an option: the only one for a single choice, one more for a multiple.
    public mutating func toggle(_ label: String, item: Int) {
        if question.items[item].multi {
            if picked[item].contains(label) { picked[item].remove(label) } else { picked[item].insert(label) }
        } else {
            picked[item] = picked[item] == [label] ? [] : [label]
            typed[item] = ""
        }
    }

    /// Each item's answer: its picks in the order offered, then anything typed;
    /// on a single choice, typed words replace a pick. Nil until every item has one.
    public var answers: [String]? {
        var out: [String] = []
        for (i, item) in question.items.enumerated() {
            let words = typed[i].trimmingCharacters(in: .whitespacesAndNewlines)
            var parts = item.options.map(\.label).filter { picked[i].contains($0) }
            if !words.isEmpty {
                parts = item.multi ? parts + [words] : [words]
            }
            guard !parts.isEmpty else { return nil }
            out.append(parts.joined(separator: ", "))
        }
        return out
    }
}

/// Feed follows one work item's conversation: it reads only from the first
/// step that can still change, so a long session costs one short read a beat.
@MainActor
@Observable
public final class Feed {
    public private(set) var steps: [Step] = []
    public private(set) var status: SessionStatus = .none
    public private(set) var asking: Question?
    public private(set) var failure: String?
    public private(set) var loaded = false

    private let store: Store
    private let id: String

    public init(store: Store, id: String) {
        self.store = store
        self.id = id
    }

    /// The first step that can still change; everything before it is final.
    var stable: Int { steps.firstIndex { !$0.settled } ?? steps.count }

    public func read() async {
        let from = stable
        do {
            let page = try await store.conversation(id, from: from)
            // A store that shrank (a session moved, a file replaced) is read
            // again whole rather than spliced onto steps it no longer has.
            if page.total < from {
                steps = try await store.conversation(id, from: 0).entries
            } else {
                steps = Array(steps.prefix(from)) + page.entries
            }
            status = page.status
            asking = page.asking
            failure = nil
        } catch {
            failure = "\(error)"
        }
        loaded = true
    }

    /// Reads until cancelled: often while the session works, seldom while it
    /// rests. A session's store is a file the ledger is never told about, so
    /// following it is reading it.
    public func follow() async {
        while !Task.isCancelled {
            await read()
            let pause: Duration = status == .running ? .seconds(1.5) : .seconds(4)
            try? await Task.sleep(for: pause)
        }
    }
}
