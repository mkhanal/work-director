import Foundation

// A proposed breakdown as one run of text, as the overview is: what to do
// with it, each proposed sub-goal's number, title, done-when and chips, and,
// as a text of its own beside it, why the planner broke it down so. The
// controls (the buttons, the done-when field and finish-line menu, each
// card's grip and actions) are set in the text by their ids.

/// The controls a proposal sets in its text, by id.
public enum ProposalSlot {
    /// Discard, Re-plan and Approve.
    public static let decide = "decide"
    /// The done-when field and the finish-line menu.
    public static let terms = "terms"
    /// Add sub-goal.
    public static let add = "add"
    /// A card's grip, dragged to reorder, with its number.
    public static func grip(_ id: String) -> String { "grip/" + id }
    /// A card's Edit, Split, Merge and Drop.
    public static func actions(_ id: String) -> String { "actions/" + id }
}

extension GoalDetail {
    /// The proposal as one text.
    /// - Parameters:
    ///   - rows: the proposed sub-goals, in order.
    ///   - status: what the proposal is doing now, or that nothing starts until approved.
    ///   - failure: why the last change to it was refused.
    public func proposalText(_ rows: [ProposedRow], status: String, failure: String?) -> Transcript {
        var items: [TranscriptItem] = []
        func head(_ column: Int) -> Box {
            .cell(Cell(table: "decide", row: 0, column: column, columns: 2, align: column == 1 ? .trailing : .leading, look: .bare, last: true))
        }
        items.append(TranscriptItem(id: "decide", paras: [Para([Run(status, .caption, .text3)], box: head(0)),
                                                          Para([.slot(ProposalSlot.decide)], box: head(1))], source: status))
        if let failure {
            items.append(TranscriptItem(id: "failure", paras: [Para([Run(failure, .meta, .failure)], gap: .row)], source: failure))
        }
        items.append(TranscriptItem(id: "terms", paras: [Para([.block(ProposalSlot.terms)], gap: .item)], source: ""))
        let title = "Proposed sub-goals"
        let hint = "drag to reorder · each is about one agent's work"
        items.append(TranscriptItem(id: "proposed", paras: [Para([Run(title, .title), Run("   " + hint, .caption, .text3)], gap: .section)],
                                    source: title))
        for (n, row) in rows.enumerated() {
            let table = "card-" + row.id
            func cell(_ column: Int) -> Box {
                .cell(Cell(table: table, row: 0, column: column, columns: 2, align: column == 1 ? .trailing : .leading, look: .row, last: true))
            }
            var title = Para([.slot(ProposalSlot.grip(row.id), .bodyMedium), Run(row.node.work.title, .bodyMedium)], gap: n == 0 ? .row : .block, box: cell(0), lead: 58)
            title.label = true
            var paras = [title]
            if let doneWhen = row.node.doneWhen {
                paras.append(Para([Run("Done when " + doneWhen, .caption, .text3)], gap: .line, box: cell(0), lead: 58))
            }
            if !row.chips.isEmpty {
                paras.append(Para(row.chips.enumerated().flatMap { i, chip in Run.pill(chip, .tag, after: i == row.chips.count - 1 ? "" : "  ") },
                                  gap: .row, box: cell(0), lead: 58))
            }
            paras.append(Para([.slot(ProposalSlot.actions(row.id), .caption)], box: cell(1)))
            let source = "\(row.number). \(row.node.work.title)" + (row.node.doneWhen.map { "\n   Done when " + $0 } ?? "")
            items.append(TranscriptItem(id: "card-" + row.id, paras: paras, source: source))
        }
        items.append(TranscriptItem(id: "add", paras: [Para([.slot(ProposalSlot.add, .title)], gap: .block)], source: ""))
        return Transcript(items: items)
    }
}

extension Reasoning {
    /// Why the planner broke the goal down so, as one text: each section's
    /// label and what it says.
    public var transcript: Transcript {
        Transcript(items: sections.enumerated().map { n, section in
            TranscriptItem(id: "why-\(n)", paras: [Para([Run(section.label.uppercased(), .micro, .text3)], gap: n == 0 ? .none : .item),
                                                   Para([Run(section.text, .callout)], gap: .row)],
                           source: section.label + "\n" + section.text)
        })
    }
}
