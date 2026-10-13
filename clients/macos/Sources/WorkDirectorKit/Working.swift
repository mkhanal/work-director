import Foundation

/// A write the app started that has not come back yet.
public struct InFlight: Sendable, Hashable, Identifiable {
    public let id: UUID
    public let plan: Plan
    public let at: Date

    public init(id: UUID = UUID(), plan: Plan, at: Date) {
        self.id = id
        self.plan = plan
        self.at = at
    }

    /// The work it is about and what it is doing, for the writes a person
    /// waits on; nil for the rest.
    public var doing: (work: String, words: String)? {
        let a = plan.argv
        func arg(_ i: Int) -> String? { a.count > i ? a[i] : nil }
        switch (arg(0), arg(1)) {
        case ("send", let id?): return (id, "Sending your message")
        case ("answer", let id?): return (id, a.contains("--director") ? "Deciding in your place" : "Sending your answer")
        case ("decide", let id?): return (id, "Recording your decision")
        case ("goal", "plan"): return arg(2).map { ($0, "Re-planning") }
        case ("goal", "continue"): return arg(2).map { ($0, "Planning again") }
        case ("goal", "approve"): return arg(2).map { ($0, "Starting the plan") }
        default: return nil
        }
    }
}

/// One line of the working indicator: who is working, what it is doing now,
/// and since when.
public struct WorkingRow: Sendable, Hashable, Identifiable {
    public let id: String
    public let who: String
    public let doing: String
    /// When it started; nil when nothing says.
    public let since: Date?
}

/// The working indicator at the end of a conversation: newest first, the
/// first few shown and the rest counted.
public struct WorkingRows: Sendable, Hashable {
    public static let limit = 3
    public let shown: [WorkingRow]
    public let more: Int

    public var isEmpty: Bool { shown.isEmpty }

    public init(_ rows: [WorkingRow]) {
        let sorted = rows.sorted { ($0.since ?? .distantPast) > ($1.since ?? .distantPast) }
        shown = Array(sorted.prefix(Self.limit))
        more = max(0, sorted.count - Self.limit)
    }

    /// Every session under a goal that is running, the goal's own as the
    /// director, and every write in flight on the goal or one of its tasks.
    public init(goal: Work, tasks: [Work], activity: [String: Activity], inFlight: [InFlight]) {
        let works = [goal] + tasks
        func who(_ id: String) -> String {
            id == goal.id ? "Director" : works.first { $0.id == id }?.title ?? id
        }
        let sessions: [WorkingRow] = works.compactMap { w in
            guard let a = activity[w.id], Working.runs(a) else { return nil }
            return WorkingRow(id: w.id, who: who(w.id), doing: Working.doing(kind: a.kind, now: a.now),
                              since: (a.usage?.started ?? a.at).flatMap(AgentCard.instant))
        }
        let writes: [WorkingRow] = inFlight.compactMap { f in
            guard let (id, words) = f.doing, works.contains(where: { $0.id == id }) else { return nil }
            return WorkingRow(id: f.id.uuidString, who: who(id), doing: words, since: f.at)
        }
        self.init(sessions + writes)
    }

    /// A task's own session while it runs, and the writes in flight on it.
    public init(task: Work, status: SessionStatus, asking: Bool, last: Step?, usage: Usage?, inFlight: [InFlight]) {
        var rows: [WorkingRow] = []
        if status == .running && !asking {
            rows.append(WorkingRow(id: task.id, who: task.title,
                                   doing: last.map(Working.doing) ?? "Thinking",
                                   since: (usage?.started ?? last?.at).flatMap(AgentCard.instant)))
        }
        rows += inFlight.compactMap { f in
            guard let (id, words) = f.doing, id == task.id else { return nil }
            return WorkingRow(id: f.id.uuidString, who: task.title, doing: words, since: f.at)
        }
        self.init(rows)
    }
}

public enum Working {
    /// Whether a session is working: running, not waiting on a question, and
    /// read without error.
    public static func runs(_ a: Activity) -> Bool {
        a.status == .running && a.asking == nil && a.error == nil
    }

    /// What a session's latest step says it is doing, in the words a person
    /// uses. A served line names a tool as "Name · summary".
    public static func doing(kind: StepKind?, now: String?) -> String {
        guard kind == .tool, let now, !now.isEmpty else { return kind == nil ? "Working" : "Thinking" }
        let parts = now.components(separatedBy: " · ")
        return tool(parts[0], summary: parts.dropFirst().joined(separator: " · "))
    }

    public static func doing(_ step: Step) -> String {
        if step.kind == .tool, let t = step.tool { return tool(t.name, summary: t.summary) }
        return "Thinking"
    }

    /// A tool call as a person says it: Running `go test`, Reading a file.
    public static func tool(_ name: String, summary: String) -> String {
        let on = summary.trimmingCharacters(in: .whitespaces)
        func verb(_ v: String, _ code: Bool = false) -> String {
            on.isEmpty ? v : "\(v) " + (code ? "`\(on)`" : on)
        }
        switch name.lowercased() {
        case "bash", "shell", "local_shell", "exec_command", "exec": return on.isEmpty ? "Running a command" : verb("Running", true)
        case "read", "view", "read_file": return verb("Reading")
        case "write", "create": return verb("Writing")
        case "edit", "multiedit", "notebookedit", "apply_patch", "patch": return verb("Editing")
        case "grep", "glob", "search", "list", "ls", "find": return verb("Searching")
        case "webfetch", "fetch": return verb("Fetching")
        case "websearch": return on.isEmpty ? "Searching the web" : "Searching the web for \(on)"
        case "task", "agent": return on.isEmpty ? "Handing work to a subagent" : "Handing to a subagent: \(on)"
        case "todowrite", "todoread", "update_plan": return "Updating its plan"
        default: return on.isEmpty ? "Using \(name)" : "Using \(name) · \(on)"
        }
    }
}

extension SidebarRow {
    /// Whether the goal is under way with nothing waiting on the person.
    public var working: Bool { work.state == .running && needsYou == 0 && band != .needsYou }
}

extension GoalDetail {
    /// Whether a session runs in this work or anywhere under it.
    public func isWorking(_ id: String) -> Bool {
        if let a = activity[id], Working.runs(a) { return true }
        func under(_ ns: [TreeNode]) -> [TreeNode] { ns.flatMap { [$0] + under($0.children) } }
        guard let node = nodes.first(where: { $0.id == id }) else { return false }
        return under(node.children).contains { activity[$0.id].map(Working.runs) ?? false }
    }
}
