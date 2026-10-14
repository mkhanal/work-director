import Foundation

// A goal's overview as one run of text, as its conversation is: the cards for
// what done means, where it finishes and its course; each sub-goal's row with
// its state, line, steps and badge; what still holds it from done; what the
// director did here. A person selects across all of it at once. The controls
// on the page are set in the text by their ids (`Mark.slot`, `Run.over`) and
// the app puts each control there; a sub-goal's title is the link that opens
// it, as its row did.

/// The controls an overview sets in its text, by id.
public enum OverviewSlot {
    /// The finish-line menu, over the line's words.
    public static let finish = "finish"
    /// The bar beside the Sub-goals header.
    public static let progress = "progress"
    /// Add sub-goal.
    public static let add = "add"
    /// A question waiting on the person, by the question's id.
    public static func ask(_ id: String) -> String { "ask/" + id }
    /// The pulse beside a sub-goal an agent works on now.
    public static func working(_ id: String) -> String { "working/" + id }
}

/// What a link in an overview asks the app to do.
public enum OverviewAct {
    /// Opens a sub-goal's own page.
    public static let open = "open"
    /// Shows a task's conversation.
    public static let focus = "focus"
    /// Asks a stopped agent to carry on.
    public static let carry = "carry"
}

extension GoalDetail {
    /// The overview as one text.
    /// - Parameters:
    ///   - opened: the rows whose steps or sub-goals are unfolded.
    ///   - working: the sub-goals an agent works on now, or under.
    ///   - asks: the questions waiting on the person, in order, each set as a control.
    ///   - did: what the director did here, shown on a sub-goal's page.
    ///   - ago: an ISO time as how long ago it was, in words.
    ///   - clock: an ISO time as the hour and minute it was.
    public func overview(opened: Set<String>, working: Set<String>, asks: [String], did: [RecordEntry],
                         ago: (String) -> String, clock: (String) -> String) -> Transcript {
        var items: [TranscriptItem] = []
        func add(_ item: TranscriptItem) {
            guard !item.paras.isEmpty else { return }
            var paras = item.paras
            if !items.isEmpty, paras[0].gap == .none { paras[0].gap = .section }
            items.append(TranscriptItem(id: item.id, paras: paras, source: item.source, copies: item.copies))
        }
        add(cards(ago: ago))
        for ask in asks {
            add(TranscriptItem(id: "ask-" + ask, paras: [Para([.block(OverviewSlot.ask(ask))])], source: ""))
        }
        add(subGoalsHeader)
        if tree.isEmpty {
            let said = goal.state == .queued ? "Not planned yet." : "No sub-goals: one agent takes it whole."
            add(TranscriptItem(id: "no-rows", paras: [Para([Run(said, .callout, .text3)], gap: .row, box: .card("no-rows"))], source: said))
        } else {
            let rows = flatRows(tree, depth: 0, opened: opened)
            for (n, row) in rows.enumerated() {
                var item = self.row(row.node, depth: row.depth, number: n, last: n == rows.count - 1, opened: opened, working: working)
                if n == 0 { item = item.gap(.row) }
                items.append(item)
            }
        }
        if !notDoneBecause.isEmpty { add(notDone) }
        if chain.count > 1 && !did.isEmpty {
            add(TranscriptItem(id: "did", paras: [Para([Run("What the director did here", .title)])], source: "What the director did here"))
            for (n, entry) in did.enumerated() {
                let table = "did"
                func cell(_ column: Int) -> Box {
                    var c = Cell(table: table, row: n, column: column, columns: 2, width: column == 0 ? 64 : nil, look: .bare)
                    c.last = n == did.count - 1
                    return .cell(c)
                }
                items.append(TranscriptItem(id: "did-\(entry.id)", paras: [
                    Para([Run(clock(entry.at), .caption, .text3)], gap: n == 0 ? .row : .none, box: cell(0)),
                    Para([Run(entry.sentence, .callout)], box: cell(1)),
                ], source: entry.sentence))
            }
        }
        return Transcript(items: items)
    }

    /// Done when, Finishes at (top goals only) and Course, side by side.
    private func cards(ago: (String) -> String) -> TranscriptItem {
        let top = chain.count <= 1
        let columns = top ? 3 : 2
        var paras: [Para] = []
        var source: [String] = []
        func cell(_ column: Int) -> Box { .cell(Cell(table: "cards", row: 0, column: column, columns: columns, look: .card)) }
        func label(_ text: String, _ column: Int) -> Para { Para([Run(text, .meta, .text3)], box: cell(column)) }

        paras.append(label("Done when", 0))
        let doneWhen = doneWhenSentence ?? "Not set yet."
        paras.append(Para([Run(doneWhen, .callout, self.doneWhen == nil ? .text3 : .text)], gap: .line, box: cell(0)))
        source.append("Done when: " + doneWhen)
        var column = 1
        if top {
            paras.append(label("Finishes at", column))
            var line = Run(finishWords?.line ?? "Not decided yet", finishWords == nil ? .callout : .headline, finishWords == nil ? .text3 : .text)
            line.over = OverviewSlot.finish
            var space = Run.mark(.space(4), .caption)
            space.over = OverviewSlot.finish
            var arrow = Run.mark(.menu, .caption, .text2)
            arrow.over = OverviewSlot.finish
            paras.append(Para([line, space, arrow], gap: .line, box: cell(column)))
            if let finish = finishWords {
                paras.append(Para([Run(finish.source, .caption, .text3)], gap: .line, box: cell(column)))
            }
            source.append("Finishes at: " + (finishWords.map { $0.line + ". " + $0.source } ?? "Not decided yet"))
            column += 1
        }
        paras.append(label("Course", column))
        if let course {
            paras.append(Para(Run.pill(course.badge.text, .tone(course.badge.tone)) + [Run("checked " + ago(course.at), .caption, .text3)], gap: .line, box: cell(column)))
            if !course.why.isEmpty {
                paras.append(Para([Run(course.why.sentence, .caption, .text3)], gap: .line, box: cell(column)))
            }
            source.append("Course: \(course.badge.text), checked \(ago(course.at))" + (course.why.isEmpty ? "" : ". " + course.why.sentence))
        } else {
            paras.append(Para([Run("Not checked yet", .callout, .text3)], gap: .line, box: cell(column)))
            source.append("Course: Not checked yet")
        }
        return TranscriptItem(id: "cards", paras: paras, source: source.joined(separator: "\n"))
    }

    /// Sub-goals, the bar and counts beside it, and Add sub-goal at its end.
    private var subGoalsHeader: TranscriptItem {
        let top = chain.count <= 1
        func cell(_ column: Int) -> Box {
            .cell(Cell(table: "head", row: 0, column: column, columns: 2, align: column == 1 ? .trailing : .leading, look: .bare, last: true))
        }
        var runs = [Run("Sub-goals", .title)]
        if progress.total + progress.superseded > 0 || top && progress.total > 0 { runs += [Run(" ", .caption), .mark(.space(8), .caption)] }
        if top && progress.total > 0 { runs += [.slot(OverviewSlot.progress, .title), .mark(.space(12), .caption)] }
        if progress.total + progress.superseded > 0 { runs.append(Run(progressLine, .caption, .text3)) }
        var paras = [Para(runs, box: cell(0))]
        paras.append(Para(goal.atRest ? [] : [.slot(OverviewSlot.add, .title)], box: cell(1)))
        let source = "Sub-goals" + (progress.total + progress.superseded > 0 ? " · " + progressLine : "")
        return TranscriptItem(id: "sub-goals", paras: paras, source: source)
    }

    private func flatRows(_ nodes: [TreeNode], depth: Int, opened: Set<String>) -> [(node: TreeNode, depth: Int)] {
        nodes.flatMap { node -> [(node: TreeNode, depth: Int)] in
            [(node, depth)] + (node.group && opened.contains(node.id) ? flatRows(node.children, depth: depth + 1, opened: opened) : [])
        }
    }

    /// One sub-goal: its state, title, line and badge; a task's steps under
    /// it when unfolded. Its title does what choosing its row did: a goal
    /// opens its page, a task folds its steps, a group folds the tasks it
    /// gathers, any other shows its agent's conversation.
    private func row(_ node: TreeNode, depth: Int, number: Int, last: Bool, opened: Set<String>, working: Set<String>) -> TranscriptItem {
        let gone = node.work.state.mark == .superseded
        let open = opened.contains(node.id)
        func cell(_ column: Int) -> Box {
            var c = Cell(table: "rows", row: number, column: column, columns: 2, align: column == 1 ? .trailing : .leading, look: .row, last: last,
                         tint: node.work.state == .needsInput ? .needsYou : nil)
            if column == 0 { c.inset = Double(depth) * 32 }
            return .cell(c)
        }
        var title = Run(node.work.title + (node.opensAsGoal ? " ›" : ""), .bodyMedium, gone ? .text2 : .text, strike: gone)
        if node.opensAsGoal {
            title.link = actLink(OverviewAct.open, node.id)
            title.help = "Open this sub-goal"
        } else if node.folds || node.group {
            title.toggle = node.id
            title.help = open ? "Hide" : "Show"
        } else {
            title.link = actLink(OverviewAct.focus, node.id)
            title.help = "Show this agent's conversation"
        }
        var head = Para([.mark(.state(Avatar(node.mark)), .bodyMedium), title], box: cell(0), lead: 32)
        head.label = true
        var paras = [head]
        var line = [Run(node.meta(superseded: superseded), .caption, .text3)]
        if node.folds && !open && node.work.state.mark == .working {
            let done = node.steps.filter { $0.state == .done }.count, planned = node.steps.filter { $0.state != .dropped }.count
            line += [Run(" · ", .caption, .text3), Run("Show steps (\(done) of \(planned))", .caption, .link, toggle: node.id)]
        }
        if node.stopped != nil {
            var carry = Run("Tell it to carry on", .caption, .link, link: actLink(OverviewAct.carry, node.id))
            carry.help = "Ask its agent to pick up where it stopped"
            line += [Run(" · ", .caption, .text3), carry]
        }
        if !line.map(\.text).joined().isEmpty { paras.append(Para(line, box: cell(0), lead: 32)) }
        if open && node.folds {
            for (n, step) in node.steps.enumerated() {
                let (glyph, mark, ink): (String, Ink, Ink) = switch step.state {
                case .done: ("✓", .settled, .stepText)
                case .doing: ("●", .live, .live)
                case .todo, .dropped: ("○", .text3, .text3)
                }
                paras.append(Para([Run(glyph + "\t", .callout, mark), Run(step.text, step.state == .doing ? .calloutStrong : .callout, ink, strike: step.state == .dropped)],
                                  hang: true, gap: n == 0 ? .row : .line, box: cell(0), lead: 32))
            }
            paras.append(Para([Run("The agent's own plan · ", .caption, .text3), Run("Hide steps", .caption, .link, toggle: node.id)],
                              gap: .row, box: cell(0), lead: 32))
        }
        var badge: [Run] = working.contains(node.id) ? [.slot(OverviewSlot.working(node.id), .caption), .mark(.space(6), .caption)] : []
        badge += Run.pill(node.badge.text, .tone(node.badge.tone), after: "")
        paras.append(Para(badge, box: cell(1)))
        var copies = [MenuCopy("Copy Title and ID", Clip.work(node.work))]
        if let doneWhen = node.doneWhen { copies.append(MenuCopy("Copy Done When", doneWhen)) }
        let source = "- \(node.work.title) — \(node.badge.text)" + (node.meta(superseded: superseded).isEmpty ? "" : "\n  " + node.meta(superseded: superseded))
        return TranscriptItem(id: "row-" + node.id, paras: paras, source: source, copies: copies)
    }

    /// What still holds the goal from done, in a card.
    private var notDone: TranscriptItem {
        var runs: [Run] = []
        for (i, why) in notDoneBecause.enumerated() {
            if i > 0 { runs.append(Run("      ", .callout)) }
            runs.append(Run("○ ", .callout, i == 0 && why.contains("sub-goal") ? .attention : .grey))
            runs.append(Run(why.sentence, .callout))
        }
        let box = Box.card("not-done")
        return TranscriptItem(id: "not-done", paras: [Para([Run("Not done yet, because", .meta, .text3)], box: box),
                                                      Para(runs, gap: .line, box: box)],
                              source: "Not done yet, because:\n" + notDoneBecause.map { "- " + $0.sentence }.joined(separator: "\n"))
    }
}

extension TranscriptItem {
    /// The item with the space above its first paragraph changed.
    func gap(_ gap: Gap) -> TranscriptItem {
        var paras = self.paras
        if !paras.isEmpty { paras[0].gap = gap }
        return TranscriptItem(id: id, paras: paras, source: source, tool: tool, copies: copies)
    }
}
