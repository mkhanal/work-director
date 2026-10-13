import Foundation

/// The kinds a goal's record is narrowed to.
public enum RecordKind: String, CaseIterable, Sendable {
    case everything, decisions, course, questions, reports, chores, delivery

    public var label: String {
        switch self {
        case .course: "Direction checks"
        case .chores: "Housekeeping"
        default: rawValue.prefix(1).uppercased() + rawValue.dropFirst()
        }
    }

    /// Whether an entry of `kind`, as `wd` names it, is of this kind.
    public func holds(_ kind: String) -> Bool {
        switch self {
        case .everything: true
        case .decisions: ["decision", "finish", "asked"].contains(kind)
        case .course: ["course", "superseded"].contains(kind)
        case .questions: ["question", "answer"].contains(kind)
        case .reports: ["report", "judged"].contains(kind)
        case .chores: kind == "chore"
        case .delivery: kind == "delivery"
        }
    }
}

/// One day of a record, newest first.
public struct RecordDay: Sendable, Hashable, Identifiable {
    public let day: String
    public let entries: [RecordEntry]
    public var id: String { day }
}

private func plural(_ n: Int, _ one: String, _ many: String) -> String { n == 1 ? one : many }

extension RecordSummary {
    /// The five summary cards: a number and what it counts.
    public var cards: [(number: Int, label: String)] {
        [
            (decisions, plural(decisions, "decision", "decisions") + " · \(yours) yours"),
            (course, plural(course, "direction check", "direction checks") + " · \(corrections) " + plural(corrections, "correction", "corrections")),
            (questions, plural(questions, "question", "questions") + " · \(answeredByDirector) answered by the director"),
            (chores, plural(chores, "housekeeping task", "housekeeping tasks") + " the director ran"),
            (judged, plural(judged, "report", "reports") + " checked"),
        ]
    }
}

extension GoalRecord {
    /// The entries of one kind, at or under one place in the tree when one is
    /// named, newest first.
    public func entries(_ kind: RecordKind, under path: String? = nil) -> [RecordEntry] {
        entries.filter { e in
            kind.holds(e.kind) && (path == nil || e.where == path || e.where.hasPrefix(path! + " › "))
        }
    }

    /// Entries grouped by the day they fall on, as "Today", "Yesterday" or the date.
    public static func days(_ entries: [RecordEntry], now: Date = Date(), calendar: Calendar = .current) -> [RecordDay] {
        var out: [RecordDay] = []
        for e in entries {
            let day = Self.day(e.at, now: now, calendar: calendar)
            if let last = out.last, last.day == day {
                out[out.count - 1] = RecordDay(day: day, entries: last.entries + [e])
            } else {
                out.append(RecordDay(day: day, entries: [e]))
            }
        }
        return out
    }

    static func day(_ stamp: String, now: Date, calendar: Calendar) -> String {
        guard let d = instant(stamp) else { return "Earlier" }
        if calendar.isDate(d, inSameDayAs: now) { return "Today" }
        if let y = calendar.date(byAdding: .day, value: -1, to: now), calendar.isDate(d, inSameDayAs: y) { return "Yesterday" }
        return d.formatted(Date.FormatStyle(date: .abbreviated, time: .omitted).locale(Locale(identifier: "en_US_POSIX")))
    }
}

extension GoalDetail {
    /// The sub-goals a record can be narrowed to: each one directly under
    /// the goal, by the place its entries name.
    public var recordPlaces: [String] {
        tree.map { paths[$0.id] ?? $0.work.title }
    }
}
