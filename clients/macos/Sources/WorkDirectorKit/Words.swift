import Foundation

// The words the app shows for what wd names in its own terms. Machine values
// (states, event kinds, gates, finish lines) stay as wd sends them; only what
// a person reads is said here, once.

public enum Words {
    /// A branch a change of plan let go of, as its badge says it.
    public static let replaced = "Replaced"

    /// The line under a replaced branch: why the plan let it go.
    public static func replaced(why: String?) -> String {
        "Replaced by a change of plan" + (why.map { ": " + $0 } ?? "")
    }

    /// How many sub-goals were replaced, counted apart.
    public static func replaced(count: Int) -> String { "\(count) replaced" }

    /// The director's check of a sub-goal's done-when.
    public static func checked(_ verdict: String) -> String {
        verdict == "met" ? "Checked: done" : "Checked: not done yet"
    }

    /// What work is doing now.
    public static func now(_ doing: String) -> String { "Now: " + doing }

    /// How many sub-goals a goal holds.
    public static func subGoals(_ n: Int) -> String { "\(n) sub-goal\(n == 1 ? "" : "s")" }

    /// The button that passes a gate, by the gate wd names.
    public static func gate(_ gate: String) -> String {
        switch gate {
        case "verify": "Check it"
        case "land": "Deliver it"
        case "soft-done": "Confirm it's finished"
        case "done": "Mark it done"
        default: gate.prefix(1).uppercased() + gate.dropFirst()
        }
    }

    /// A finish line as a page says it: "A complete pull request".
    public static func finishLine(_ line: String) -> String {
        switch line.lowercased().trimmingCharacters(in: .whitespaces) {
        case "local build": "A local build"
        case "complete pr": "A complete pull request"
        default: line.prefix(1).uppercased() + line.dropFirst()
        }
    }

    /// What an event in work's history is, by the kind wd files it under.
    public static func event(_ kind: String) -> String {
        switch kind {
        case "state": "Moved"
        case "report": "Report"
        case "verify": "Checks"
        case "pr": "Delivered"
        case "note": "Note"
        case "sent": "Message"
        case "spawn": "Started"
        case "attach": "Session"
        case "question": "Question"
        case "answer": "Answer"
        case "decision": "Decision"
        case "abandon": "Abandoned"
        case "look": "Looked"
        case "supervised": "Supervised"
        case "course": "Direction"
        case "judged": "Checked"
        case "attachment": "File"
        default: kind.prefix(1).uppercased() + kind.dropFirst()
        }
    }

    /// An event's text: a state change says the state in the app's words.
    public static func eventBody(kind: String, body: String) -> String {
        guard kind == "state", let state = WorkState(rawValue: body) else { return body }
        return state.look.label
    }

    /// wd's own terms, which no label the app shows may carry.
    public static let internalTerms = ["superseded", "supersede", "judged", "soft-done", "briefed", "needs-input", "verify", "land", "claim"]

    /// Whether a label is free of wd's own terms, "PR" included.
    public static func plain(_ label: String) -> Bool {
        let lower = label.lowercased()
        if internalTerms.contains(where: lower.contains) { return false }
        return label.range(of: #"\bPRs?\b"#, options: .regularExpression) == nil
    }
}
