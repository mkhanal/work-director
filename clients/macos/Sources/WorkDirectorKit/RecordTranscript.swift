import Foundation

extension GoalRecord {
    /// A record's entries as one text: its column names, then each day and
    /// its rows of time, kind, what, where and by whom. Copied, a row reads
    /// one line with its cells separated by tabs.
    /// - Parameter time: an ISO time as the hour and minute it was.
    public static func transcript(_ entries: [RecordEntry], time: (String) -> String) -> Transcript {
        let widths: [Double?] = [52, 96, nil, 220, 90]
        func cell(_ table: String, _ row: Int, _ column: Int, header: Bool = false, last: Bool = false) -> Box {
            var c = Cell(table: table, row: row, column: column, columns: 5, header: header, align: column == 4 ? .trailing : .leading)
            c.width = widths[column]
            c.lined = true
            c.last = last
            return .cell(c)
        }
        let names = ["Time", "Kind", "What", "Where", "By"]
        var items = [TranscriptItem(id: "head", paras: names.enumerated().map { n, name in
            Para([Run(name, .caption, .text3)], box: cell("head", 0, n, header: true))
        }, source: names.joined(separator: " | "))]
        for day in days(entries) {
            items.append(TranscriptItem(id: "day-\(day.day)", paras: [Para([Run(day.day, .metaStrong, .stepText)], gap: .item)], source: day.day))
            for (r, e) in day.entries.enumerated() {
                let table = "rows-\(day.day)", last = r == day.entries.count - 1
                let paras = [
                    Para([Run(time(e.at), .caption, .text3)], gap: r == 0 ? .row : .none, box: cell(table, r, 0, last: last)),
                    Para(Run.pill(e.badge.text, .tone(e.badge.tone), after: ""), box: cell(table, r, 1, last: last)),
                    Para([Run(e.text, .callout)], box: cell(table, r, 2, last: last)),
                    Para([Run(e.where, .caption, .text2)], box: cell(table, r, 3, last: last)),
                    Para([Run(e.by, .caption, .text3)], box: cell(table, r, 4, last: last)),
                ]
                items.append(TranscriptItem(id: e.id, paras: paras, source: e.sentence))
            }
        }
        return Transcript(items: items)
    }
}
