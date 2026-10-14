import Foundation

// A goal's conversation as one run of text, as a session's is: the person's
// ask, the plan, each turn with who said it, its sub-goal and when, the lines
// for what happened, the cards for what was answered and reported. Only what
// waits on the person to act sits outside it, after it.

/// What a message the person sent says while the director is still busy with
/// the one before it.
public let waitingForDirectorWords = "Waiting for the director to finish the last one"

/// The scheme of a link that opens an agent's conversation.
public let openScheme = "wd-open"

extension GoalDetail {
    /// The conversation narrowed by a filter, as one text. A fold opens and
    /// closes by its id in `toggled`.
    /// - Parameters:
    ///   - time: an ISO time as the reader wants it.
    ///   - day: the day an ISO time falls on, as a reader names it.
    public func transcript(_ filter: ThreadFilter, toggled: Set<String> = [], time: @escaping (String) -> String, day: (String) -> String?) -> Transcript {
        var b = GoalTranscriptBuilder(goal: self, toggled: toggled, time: time)
        if case .subGoal = filter {} else {
            if !goal.detail.isEmpty { b.add(b.you(id: "ask", text: goal.detail, at: goal.created, label: "You", files: [])) }
            if !tree.isEmpty { b.add(b.plan()) }
        }
        let items = items(filter)
        var lastDay: String?
        for item in items {
            let at = switch item {
            case .step(let s): s.event.at
            case .updates(let run): run[0].event.at
            }
            if let d = day(at), d != lastDay {
                b.add(b.dayLine(d, id: "d\(item.id)"))
                lastDay = d
            }
            switch item {
            case .step(let step): for item in b.rows(step) { b.add(item) }
            case .updates(let run): b.add(b.updates(run))
            }
        }
        return Transcript(items: b.items)
    }

    /// Who said what a work's events say, by name.
    func speakerName(of id: String) -> String {
        if id == goal.id { return "Director" }
        if let task = tasks.first(where: { $0.id == id }) ?? nodes.first(where: { $0.id == id })?.work { return task.runner ?? task.title }
        return id
    }

    /// The mark beside a work's turns: the director's check, else its state.
    func speakerMark(of id: String) -> Avatar {
        if id == goal.id { return .done }
        if let task = tasks.first(where: { $0.id == id }) ?? nodes.first(where: { $0.id == id })?.work { return Avatar(task.state.mark) }
        return .working
    }
}

private struct GoalTranscriptBuilder {
    let goal: GoalDetail
    let toggled: Set<String>
    let time: (String) -> String
    var items: [TranscriptItem] = []

    init(goal: GoalDetail, toggled: Set<String>, time: @escaping (String) -> String) {
        self.goal = goal
        self.toggled = toggled
        self.time = time
    }

    /// Adds an item, the space above it a turn's unless it opens the text.
    mutating func add(_ item: TranscriptItem?) {
        guard let item, !item.paras.isEmpty else { return }
        var paras = item.paras
        paras[0].gap = items.isEmpty ? .none : .turn
        items.append(TranscriptItem(id: item.id, paras: paras, source: item.source, tool: item.tool))
    }

    func open(_ id: String) -> Bool { toggled.contains(id) }

    // MARK: turns

    /// A turn's first line: the speaker's mark and name, the sub-goal it was
    /// said in, when, and what kind of turn it is.
    func header(work: String?, avatar: Avatar? = nil, name: String? = nil, at: String?, detail: String? = nil, link: String? = nil) -> Para {
        let who = name ?? work.map(goal.speakerName) ?? "Director"
        var runs = [Run.mark(.avatar(avatar ?? work.map(goal.speakerMark) ?? .done), .meta)]
        var said = Run(who, .meta, .text2)
        if let link { said.link = openScheme + ":" + link }
        runs.append(said)
        if let work, let tag = goal.tag(of: work) {
            runs.append(Run(" ", .meta, .text2))
            runs += Run.pill(tag, .tag, after: "")
        }
        if let at { runs.append(Run(" · " + time(at), .meta, .text2)) }
        if let detail { runs.append(Run(" " + detail, .meta, .text3)) }
        return Para(runs)
    }

    /// Who opens a work's conversation: any work but the goal itself.
    func opener(_ work: String) -> String? { work == goal.goal.id ? nil : work }

    /// Words said in a turn: each line a paragraph past the mark.
    func lines(_ text: String, face: Face = .body, ink: Ink = .text, gap: Gap = .row, indent: Int = 1, box: Box? = nil, outer: Box? = nil) -> [Para] {
        text.components(separatedBy: "\n").enumerated().map { n, line in
            Para([Run(line, face, ink)], indent: indent, gap: n == 0 ? gap : .none, box: box, outer: outer)
        }
    }

    /// Markdown said in a turn, inside a card when given one.
    func markdown(_ text: String, id: String, gap: Gap = .row, outer: Box? = nil, lead: Double = 0) -> [Para] {
        var paras = Builder.markdown(text, id: id, indent: outer == nil ? 1 : 0)
        guard !paras.isEmpty else { return [] }
        if outer != nil || lead > 0 {
            paras = paras.map { p in
                var p = p
                if let outer {
                    p.outer = outer
                    p.indent = 1
                }
                p.lead = lead
                return p
            }
        }
        paras[0].gap = gap
        return paras
    }

    func you(id: String, text: String, at: String, label: String, files: [Attachment], waiting: Bool = false) -> TranscriptItem {
        let box = Box.bubble(id)
        var paras = [Para([Run("\(label) · \(time(at))", .caption, .text3)], align: .trailing)]
        if !text.isEmpty || files.isEmpty {
            let all = text.components(separatedBy: "\n")
            let folds = text.count > 800 || all.count > 8
            var shown = folds && !open(id) ? Array(all.prefix(8)) : all
            if folds && !open(id), let last = shown.popLast() { shown.append(String(last.prefix(400)) + "…") }
            paras += shown.enumerated().map { n, line in Para([Run(line, .body)], gap: n == 0 ? .line : .none, box: box) }
            if folds { paras.append(Para([Run(open(id) ? "Show less" : "Show all", .meta, .link, toggle: id)], gap: .line, align: .trailing)) }
        }
        for file in files {
            var name = Run(file.name, .callout, .link)
            name.link = URL(fileURLWithPath: file.path).absoluteString
            paras.append(Para([.mark(.symbol(file.isImage ? "photo" : "doc"), .callout, .text2), name, Run(" " + fileSize(file.size), .caption, .text3)],
                              gap: .line, align: .trailing))
        }
        if waiting { paras.append(Para([Run(waitingForDirectorWords, .caption, .text3)], gap: .line, align: .trailing)) }
        return TranscriptItem(id: id, paras: paras, source: text)
    }

    func plan() -> TranscriptItem {
        let n = goal.tree.count
        let said = "Planned it as \(n) sub-goal\(n == 1 ? "" : "s")" + (goal.finishWords.map { ", finishing at \($0.line.lowercased())." } ?? ".")
        let card = Box.plain("plan")
        var paras = [header(work: nil, avatar: .done, name: "Director", at: nil), Para([Run(said, .body)], indent: 1, gap: .row)]
        for (i, node) in goal.tree.enumerated() {
            let badge = node.needsYou > 0 ? GoalBadge("Needs you", .attention) : node.work.state.badge
            var title = Run(node.work.title, .callout)
            title.strike = node.work.state.mark == .superseded
            paras.append(Para(Run.pill(badge.text, .tone(badge.tone)) + [title], indent: 1, gap: i == 0 ? .block : .line, box: card))
        }
        let source = ([said] + goal.tree.map { "- \($0.work.title)" }).joined(separator: "\n")
        return TranscriptItem(id: "plan", paras: paras, source: source)
    }

    /// Something that happened, centred between hairlines.
    func eventLine(_ id: String, _ text: String, at: String?) -> TranscriptItem {
        var words = text
        if let at { words += " · " + time(at) }
        return TranscriptItem(id: id, paras: [Para([Run(words, .caption, .text3)], gap: .line, box: .event(id), oneLine: true, align: .center)], source: words)
    }

    func dayLine(_ day: String, id: String) -> TranscriptItem {
        TranscriptItem(id: id, paras: [Para([Run(day, .metaStrong, .text2)], box: .day(id))], source: day)
    }

    func updates(_ run: [ThreadStep]) -> TranscriptItem {
        let id = "u\(run[0].id)"
        let expanded = open(id)
        let head = Para([.mark(.chevron(open: expanded), .meta, toggle: id), Run("\(run.count) updates", .meta, .text3, toggle: id),
                         Run(" · " + time(run[0].event.at), .meta, .text3)], box: .event(id), align: .center)
        var paras = [head]
        var source = ["\(run.count) updates"]
        if expanded {
            for item in run.flatMap(rows) {
                var ps = item.paras
                ps[0].gap = .row
                paras += ps
                source.append(item.source)
            }
        }
        return TranscriptItem(id: id, paras: paras, source: source.joined(separator: "\n"))
    }

    // MARK: rows

    /// A step as items: an exchange is its question, then its answer.
    func rows(_ step: ThreadStep) -> [TranscriptItem] {
        if case .exchange(let q, let answer) = step { return exchange("e\(step.id)", q, answer) }
        return row(step).map { [$0] } ?? []
    }

    func row(_ step: ThreadStep) -> TranscriptItem? {
        let id = "e\(step.id)"
        switch step {
        case .event(let e) where goal.isYours(e):
            return you(id: id, text: e.decision?.answer ?? e.answerText, at: e.at, label: e.kind == "answer" ? "You answered" : "You", files: goal.files(with: e),
                       waiting: goal.waitsForDirector(e.id))
        case .event(let e) where e.kind == "attachment":
            return you(id: id, text: "", at: e.at, label: e.work == goal.goal.id ? "You attached" : "You attached to \(name(of: e.work))", files: goal.files(with: e))
        case .event(let e) where e.kind == "report":
            return report(id, e)
        case .event(let e) where e.kind == "answer":
            return directorTurn(id, e)
        case .event(let e) where e.kind == "sent":
            return e.work == goal.goal.id ? nil : eventLine(id, "Told \(name(of: e.work)): \(firstLine(e.body))", at: e.at)
        case .event(let e):
            return eventLine(id, line(e), at: e.at)
        case .exchange(let q, let answer):
            return exchange(id, q, answer).first
        case .look(let e):
            return eventLine(id, "Director looking: \(e.body)", at: e.at)
        case .declined(let e, let choice):
            let card = Box.plain(id)
            var paras = [header(work: nil, avatar: .done, name: "Director", at: e.at, detail: "left it to you")]
            paras.append(Para([Run(choice.question, .caption, .text3)], indent: 1, gap: .row, box: card))
            paras.append(Para([Run(capitalised(choice.why), .callout)], indent: 1, gap: .none, box: card))
            return TranscriptItem(id: id, paras: paras, source: choice.question + "\n" + choice.why)
        case .supervisor(let e, let reply):
            return supervisor(id, e, reply)
        case .stopped(let e, let said):
            var paras = [header(work: e.work, at: e.at, detail: "stopped without a report", link: opener(e.work))]
            let all = said.components(separatedBy: "\n")
            let folds = said.count > 400 || all.count > 4
            var shown = folds && !open(id) ? Array(all.prefix(4)) : all
            if folds && !open(id), let last = shown.popLast() { shown.append(String(last.prefix(400)) + "…") }
            paras += lines(shown.joined(separator: "\n"), ink: .text2)
            if folds { paras.append(Para([Run(open(id) ? "Show less" : "Show all", .meta, .link, toggle: id)], indent: 1, gap: .line)) }
            return TranscriptItem(id: id, paras: paras, source: said)
        }
    }

    func exchange(_ id: String, _ q: Event, _ answer: Event?) -> [TranscriptItem] {
        guard answer != nil || !goal.answeredInCard(q) else { return [] }
        let asked = q.askedItems
        let question = asked.map(\.question).joined(separator: "\n")
        if let answer, answer.answeredByDirector, q.work != goal.goal.id {
            let card = Box.card(id), inner = Box.answered(id)
            var paras = [header(work: q.work, at: q.at, link: opener(q.work))]
            paras.append(Para([Run("Asked the director", .meta, .text3)], indent: 1, gap: .row, box: card))
            paras += lines(question, face: .callout, gap: .line, box: card)
            paras.append(Para([Run("Director answered", .calloutMedium, .settled), Run(" · " + answer.answerText, .callout)],
                              indent: 1, gap: .block, box: inner, outer: card))
            return [TranscriptItem(id: id, paras: paras, source: question + "\n\nDirector answered: " + answer.answerText)]
        }
        var paras = [header(work: q.work, at: q.at, detail: "asked", link: opener(q.work))]
        for (n, item) in asked.enumerated() {
            paras.append(Para([Run(item.question, .body)], indent: 1, gap: n == 0 ? .row : .block))
            let finish = item.question == FinishMeaning.question
            for choice in item.choices {
                let (label, recommended) = Self.choice(choice)
                let meaning = finish ? FinishMeaning.of(label) : nil
                var runs = [Run("○ ", .callout, .text3), Run(meaning?.title ?? label, .callout, .text2)]
                if recommended { runs.append(Run(" ", .callout)); runs += Run.pill("Recommended", .tone(.settled), after: "") }
                paras.append(Para(runs, indent: 1, gap: .line))
                if let meaning { paras.append(Para([Run(meaning.meaning, .caption, .text3)], indent: 1, lead: 16)) }
            }
        }
        let asking = TranscriptItem(id: id, paras: paras, source: q.body)
        guard let answer else { return [asking] }
        let said = answer.answeredByDirector ? directorTurn("e\(answer.id)", answer)
            : you(id: "e\(answer.id)", text: answer.answerText, at: answer.at, label: "You answered", files: goal.files(with: answer),
                  waiting: goal.waitsForDirector(answer.id))
        return [asking, said]
    }

    func directorTurn(_ id: String, _ answer: Event) -> TranscriptItem {
        let detail = answer.decision.map { $0.model.isEmpty ? "answered from what was decided" : "answered · \($0.model)" } ?? "answered"
        let card = Box.settled(id)
        var paras = [header(work: nil, avatar: .done, name: "Director", at: answer.at, detail: detail)]
        let decided = goal.decided(for: answer)
        if decided.isEmpty {
            paras += lines(answer.answerText, box: card)
        } else {
            for (n, choice) in decided.enumerated() {
                paras.append(Para([Run(choice.question, .caption, .text3)], indent: 1, gap: n == 0 ? .row : .block, box: card))
                if let a = choice.answer { paras.append(Para([Run(a, .bodyMedium)], indent: 1, box: card)) }
                if !choice.why.isEmpty { paras.append(Para([Run(capitalised(choice.why), .callout, .text2)], indent: 1, box: card)) }
            }
        }
        return TranscriptItem(id: id, paras: paras, source: answer.answerText)
    }

    func supervisor(_ id: String, _ e: Event, _ reply: SupervisorReply) -> TranscriptItem {
        var paras = [header(work: nil, avatar: .done, name: "Director", at: e.at, link: goal.goal.id)]
        if !reply.said.isEmpty { paras += markdown(reply.said, id: id) }
        for (n, action) in reply.actions.enumerated() {
            let gap: Gap = n == 0 ? .block : .row
            if let task = action.task, let work = goal.tasks.first(where: { $0.id == task }) {
                let card = Box.agent("\(id)-\(n)")
                var title = Run(work.title, .calloutMedium)
                title.link = openScheme + ":" + task
                paras.append(Para([.mark(.avatar(Avatar(work.state.mark)), .calloutMedium), title, Run(" · " + work.state.look.label, .caption, .text3)],
                                  indent: 1, gap: gap, box: card))
                if !action.line.isEmpty { paras.append(Para([Run(action.line, .callout, .text2)], indent: 1, gap: .line, box: card)) }
                if let now = goal.activity[task].flatMap({ $0.error ?? $0.now }), !now.isEmpty {
                    paras.append(Para([Run(now, .meta, .text3)], indent: 1, gap: .line, box: card, oneLine: true))
                }
            } else {
                paras.append(Para([.mark(.symbol("checkmark.circle"), .callout, .text2), Run(action.line, .callout, .text2)], indent: 1, gap: gap))
            }
        }
        for line in reply.refused {
            paras.append(Para([.mark(.symbol("xmark.circle"), .callout, .failure), Run(line, .callout, .failure)], indent: 1, gap: .row))
        }
        return TranscriptItem(id: id, paras: paras, source: e.body)
    }

    func report(_ id: String, _ e: Event) -> TranscriptItem {
        let card = Box.card(id)
        var paras = [header(work: e.work, at: e.at, link: opener(e.work))]
        guard let report = goal.reports[String(e.id)] else {
            return TranscriptItem(id: id, paras: paras + markdown(e.body, id: id, outer: card), source: e.body)
        }
        var head: [Run] = [.mark(.symbol("doc.text"), .calloutMedium, .text2), Run("Report", .calloutMedium)]
        if !report.verdict.isEmpty {
            head.append(Run(" ", .calloutMedium))
            let pill: Pill = switch report.verdict {
            case "done": .tone(.settled)
            case "blocked": .failure
            default: .tone(.attention)
            }
            head += Run.pill(report.verdict, pill, after: "")
        }
        if !report.said.isEmpty { head.append(Run(" " + report.said, .callout, .text2)) }
        paras.append(Para(head, indent: 1, gap: .row, box: card))
        let label = 76.0
        if !report.files.isEmpty || !report.fileNote.isEmpty {
            var runs = [Run("Files", .meta, .text2), Run("\t", .meta)]
            for (n, path) in report.files.enumerated() {
                if n > 0 { runs.append(Run("  ", .codeSmall)) }
                runs.append(Run(path, .codeSmall, chip: true))
            }
            var files = Para(runs, indent: 1, gap: .block, box: card, lead: label)
            files.label = true
            paras.append(files)
            if !report.fileNote.isEmpty { paras.append(Para([Run(report.fileNote, .callout, .text2)], indent: 1, gap: .line, box: card, lead: label)) }
        }
        let shown = open(id) ? report.sections : Array(report.sections.prefix(2))
        for (s, section) in shown.enumerated() {
            let name = section.name.count <= 3 ? section.name : section.name.capitalized
            var body = markdown(section.text, id: "\(id)-\(s)", gap: .block, outer: card, lead: label)
            guard !body.isEmpty else { continue }
            if body[0].box == nil && !body[0].hang {
                body[0].runs = [Run(name, .meta, .text2), Run("\t", .meta)] + body[0].runs
                body[0].label = true
            } else {
                body.insert(Para([Run(name, .meta, .text2)], indent: 1, gap: .block, outer: card), at: 0)
                body[1].gap = .line
            }
            paras += body
        }
        if report.sections.count > 2 {
            paras.append(Para([Run(open(id) ? "Show less" : "Show \(report.sections.count - 2) more", .meta, .link, toggle: id)],
                              indent: 1, gap: .block, box: card, lead: label))
        }
        return TranscriptItem(id: id, paras: paras, source: e.body)
    }

    // MARK: words

    func name(of id: String) -> String {
        goal.tag(of: id) ?? (id == goal.goal.id ? "the goal" : goal.speakerName(of: id))
    }

    func firstLine(_ text: String) -> String {
        String(text.split(separator: "\n", maxSplits: 1).first ?? "")
    }

    func capitalised(_ text: String) -> String {
        text.prefix(1).uppercased() + text.dropFirst()
    }

    func line(_ e: Event) -> String {
        let who = e.work == goal.goal.id ? "Goal" : name(of: e.work)
        if goal.finishes(e) { return "\(who) · done" }
        switch e.kind {
        case "decision": return "Decided: \(firstLine(e.body))"
        case "judged": return "\(who) · " + Words.checked(e.body.components(separatedBy: ": ").first ?? e.body)
        case "verify": return "\(who) \(e.body.hasPrefix("pass") ? "passed its checks" : "failed its checks")"
        case "pr": return "\(who) delivered"
        case "state": return "\(who) · " + Words.eventBody(kind: e.kind, body: e.body)
        default: return "\(who): \(firstLine(e.body))"
        }
    }

    /// A choice as offered: its label, and whether it was recommended.
    static func choice(_ text: String) -> (String, Bool) {
        let suffix = "(recommended)"
        guard text.lowercased().hasSuffix(suffix) else { return (text, false) }
        return (String(text.dropLast(suffix.count)).trimmingCharacters(in: .whitespaces), true)
    }
}
