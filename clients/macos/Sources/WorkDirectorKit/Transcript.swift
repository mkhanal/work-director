import Foundation

// A session's conversation as one run of text: who spoke and when, each step's
// line, what it thought, said and was told, so a person selects across all of
// it at once. What is copied is the words as they read; Copy as Markdown is
// each item as it was written. The app only sets these pieces in its fonts and
// colours; the text, and so what a selection holds, is decided here.

/// The type a piece of text is set in, from the design's type table.
public enum Face: Hashable, Sendable {
    case body, bodyMedium, heading1, title, callout, calloutMedium, meta, caption, code, codeSmall, micro
}

/// The colour a piece of text is set in, from the design's palette.
public enum Ink: Hashable, Sendable {
    case text, text2, text3, link, failure
}

/// A speaker's mark beside their name.
public enum Avatar: Hashable, Sendable {
    case working, done
}

/// Something drawn in the text rather than written: it takes one character
/// and is never copied.
public enum Mark: Hashable, Sendable {
    case symbol(String)
    case avatar(Avatar)
    case chevron(open: Bool)
}

public struct Run: Hashable, Sendable {
    public var text: String
    public var face: Face
    public var ink: Ink
    public var bold = false
    public var italic = false
    public var strike = false
    /// A code span: set on a tint of its own.
    public var chip = false
    public var link: String?
    public var mark: Mark?
    /// Clicking this run folds or unfolds the item with this id.
    public var toggle: String?

    public init(_ text: String, _ face: Face, _ ink: Ink = .text, bold: Bool = false, italic: Bool = false,
                strike: Bool = false, chip: Bool = false, link: String? = nil, toggle: String? = nil) {
        self.text = text
        self.face = face
        self.ink = ink
        self.bold = bold
        self.italic = italic
        self.strike = strike
        self.chip = chip
        self.link = link
        self.toggle = toggle
    }

    public static func mark(_ mark: Mark, _ face: Face = .callout, _ ink: Ink = .text3, toggle: String? = nil) -> Run {
        var run = Run(Transcript.markCharacter, face, ink, toggle: toggle)
        run.mark = mark
        return run
    }
}

/// Space above a paragraph.
public enum Gap: Hashable, Sendable {
    /// Lines of one thing.
    case none
    /// A label and what it labels.
    case line
    /// A speaker's line and their first step.
    case row
    /// Blocks of one reply.
    case block
    /// One step and the next.
    case item
    /// A new speaker.
    case turn
}

/// One cell of a table.
public struct Cell: Hashable, Sendable {
    public let table: String
    public let row: Int
    public let column: Int
    public let columns: Int
    public let header: Bool
    public let align: MarkdownTable.Alignment
}

/// The box a paragraph sits in; consecutive paragraphs with the same box are
/// one box.
public enum Box: Hashable, Sendable {
    /// What the session was told, or its brief.
    case told(String)
    case brief(String)
    /// The director answering in the person's place.
    case director(String)
    /// A question already answered.
    case asked(String)
    case code(String)
    case quote(String)
    case rule(String)
    case cell(Cell)
}

public struct Para: Hashable, Sendable {
    public var runs: [Run]
    /// 1 inside a speaker's turn, past their mark.
    public var indent = 0
    /// How deep inside a list or a run of tool calls.
    public var nest = 0
    /// A list item: its marker hangs left of its text.
    public var hang = false
    public var gap = Gap.none
    public var box: Box?
    /// Cut to one line, the rest of it still copied.
    public var oneLine = false

    public init(_ runs: [Run], indent: Int = 0, nest: Int = 0, hang: Bool = false, gap: Gap = .none, box: Box? = nil, oneLine: Bool = false) {
        self.runs = runs
        self.indent = indent
        self.nest = nest
        self.hang = hang
        self.gap = gap
        self.box = box
        self.oneLine = oneLine
    }

    public var text: String { runs.map(\.text).joined() }

    /// What ends this paragraph when it is copied: a cell before the last of
    /// its row ends in a tab.
    var terminator: String {
        if case .cell(let c) = box, c.column < c.columns - 1 { return "\t" }
        return "\n"
    }
}

/// One thing in the conversation: a speaker's line, a step, a reply.
public struct TranscriptItem: Hashable, Sendable {
    public let id: String
    public let paras: [Para]
    /// The item as it was written, for Copy as Markdown.
    public let source: String
    /// The call, when the item is one.
    public let tool: ToolCall?
    /// Its text: each paragraph and a newline after it.
    public let string: String
    /// Its length in UTF-16 units, as a text view counts.
    public let length: Int

    public init(id: String, paras: [Para], source: String, tool: ToolCall? = nil) {
        self.id = id
        self.paras = paras
        self.source = source
        self.tool = tool
        string = paras.map { $0.text + "\n" }.joined()
        length = string.utf16.count
    }
}

public struct Transcript: Hashable, Sendable {
    public let items: [TranscriptItem]

    public init(items: [TranscriptItem]) {
        self.items = items
    }

    /// The character a mark takes: a text attachment's.
    public static let markCharacter = "\u{FFFC}"
    /// A line break inside one paragraph.
    public static let lineBreak = "\u{2028}"

    public var string: String { items.map(\.string).joined() }
    public var length: Int { items.reduce(0) { $0 + $1.length } }

    /// Where each item starts, in UTF-16 units.
    public var starts: [Int] {
        var at = 0
        return items.map { item in
            defer { at += item.length }
            return at
        }
    }

    /// The text a range of the conversation holds, as it reads: no marks,
    /// each paragraph on its line, a table's cells separated by tabs.
    public func copied(_ range: Range<Int>) -> String {
        var out = ""
        var at = 0
        for item in items {
            guard at < range.upperBound else { break }
            if at + item.length <= range.lowerBound {
                at += item.length
                continue
            }
            for para in item.paras {
                let text = Array(para.text.utf16)
                let end = at + text.count
                let lo = max(range.lowerBound, at), hi = min(range.upperBound, end + 1)
                if lo < hi {
                    var words = String(decoding: text[(lo - at)..<min(hi - at, text.count)], as: UTF16.self)
                    if para.hang, let tab = words.firstIndex(of: "\t") { words.replaceSubrange(tab...tab, with: " ") }
                    out += words.replacingOccurrences(of: Self.markCharacter, with: "").replacingOccurrences(of: Self.lineBreak, with: "\n")
                    if hi > end { out += para.terminator }
                }
                at = end + 1
            }
        }
        while out.hasSuffix("\n") || out.hasSuffix("\t") { out.removeLast() }
        return out
    }

    /// Each item a range touches, as it was written, a blank line between.
    public func markdown(_ range: Range<Int>) -> String {
        var out: [String] = []
        var at = 0
        for item in items {
            let span = at..<(at + item.length)
            if span.overlaps(range) || (range.isEmpty && span.contains(range.lowerBound)) { out.append(item.source) }
            at += item.length
        }
        return out.joined(separator: "\n\n")
    }

    /// The item holding a character.
    public func item(at offset: Int) -> TranscriptItem? {
        var at = 0
        for item in items {
            if offset < at + item.length { return item }
            at += item.length
        }
        return nil
    }

    /// The first item that differs between two readings, or nil when they
    /// are the same: everything before it can stay as it is on screen.
    public static func firstChange(from old: Transcript, to new: Transcript) -> Int? {
        let shared = min(old.items.count, new.items.count)
        for i in 0..<shared where old.items[i] != new.items[i] { return i }
        return old.items.count == new.items.count ? nil : shared
    }
}

/// A conversation laid out: runs of text to select across, broken where a
/// question waits on the person.
public enum TranscriptPiece: Hashable, Sendable, Identifiable {
    case text(Transcript)
    case asking(index: Int, Question)

    public var id: String {
        switch self {
        case .text(let t): "text-" + (t.items.first?.id ?? "")
        case .asking(let i, _): "ask-\(i)"
        }
    }
}

extension Transcript {
    /// A session's steps as pieces. A run of the agent's steps opens with its
    /// name and when; a step folds and unfolds by its id in `toggled`.
    /// - Parameters:
    ///   - directorAnswered: prompts that are the director answering in the person's place.
    ///   - running: the session is working, so its newest thinking is open.
    ///   - time: an ISO time as the reader wants it.
    public static func pieces(_ steps: [Step], speaker: String, running: Bool, directorAnswered: Set<Int> = [],
                              toggled: Set<String> = [], time: @escaping (String) -> String) -> [TranscriptPiece] {
        var b = Builder(steps: steps, speaker: speaker, running: running, directorAnswered: directorAnswered, toggled: toggled, time: time)
        return b.build()
    }

    /// One reply on its own: markdown as it reads.
    public static func reply(_ markdown: String, id: String = "reply") -> Transcript {
        Transcript(items: [TranscriptItem(id: id, paras: Builder.markdown(markdown, id: id, indent: 0), source: markdown)])
    }

    /// The symbol a tool's line opens with.
    public static func symbol(tool name: String) -> String {
        switch name {
        case "Bash": "terminal"
        case "Read": "doc.text"
        case "Edit", "MultiEdit", "NotebookEdit": "pencil"
        case "Write": "square.and.pencil"
        case "Grep", "Glob": "magnifyingglass"
        case "WebFetch", "WebSearch": "globe"
        case "Agent", "Task": "person.2"
        case "TodoWrite": "checklist"
        case "Skill": "book"
        default: "wrench.and.screwdriver"
        }
    }

    /// "3 commands, 2 reads": a run of calls, each kind counted once.
    public static func summary(_ calls: [ToolCall]) -> String {
        var counts: [(String, Int)] = []
        for call in calls {
            let noun = noun(call.name)
            if let i = counts.firstIndex(where: { $0.0 == noun }) { counts[i].1 += 1 } else { counts.append((noun, 1)) }
        }
        return counts.map { "\($0.1) \($0.0)\($0.1 == 1 ? "" : "s")" }.joined(separator: ", ")
    }

    static func noun(_ name: String) -> String {
        switch name {
        case "Bash": "command"
        case "Read": "read"
        case "Edit", "MultiEdit", "Write", "NotebookEdit": "edit"
        case "Grep", "Glob": "search"
        case "WebFetch", "WebSearch": "web lookup"
        case "Agent", "Task": "subagent"
        default: "\(name) call"
        }
    }
}

private struct Builder {
    let steps: [Step]
    let speaker: String
    let running: Bool
    let directorAnswered: Set<Int>
    let toggled: Set<String>
    let time: (String) -> String

    func open(_ id: String, byDefault: Bool = false) -> Bool { byDefault != toggled.contains(id) }

    mutating func build() -> [TranscriptPiece] {
        let rows = ThreadRow.rows(steps)
        var pieces: [TranscriptPiece] = []
        var items: [TranscriptItem] = []
        var afterHeader = false
        for (n, row) in rows.enumerated() {
            let agents = !isPrompt(row)
            if agents && (n == 0 || isPrompt(rows[n - 1])) {
                items.append(header(n, at: at(row), first: items.isEmpty && pieces.isEmpty))
                afterHeader = true
            }
            if case .step(let i, let step) = row, step.kind == .question, let q = step.question, q.pending {
                if !items.isEmpty { pieces.append(.text(Transcript(items: items))) }
                items = []
                pieces.append(.asking(index: i, q))
                afterHeader = false
                continue
            }
            let gap: Gap = items.isEmpty && pieces.isEmpty ? .none : afterHeader ? .row : .item
            afterHeader = false
            if let item = item(row, gap: gap, indent: agents ? 1 : 0) { items.append(item) }
        }
        if !items.isEmpty { pieces.append(.text(Transcript(items: items))) }
        return pieces
    }

    func isPrompt(_ row: ThreadRow) -> Bool {
        if case .step(_, let step) = row { return step.kind == .prompt }
        return false
    }

    func at(_ row: ThreadRow) -> String? {
        if case .step(_, let step) = row { return step.at }
        return nil
    }

    func header(_ n: Int, at: String?, first: Bool) -> TranscriptItem {
        var runs = [Run.mark(.avatar(.working), .meta), Run(speaker, .meta, .text2)]
        if let at { runs.append(Run(" · " + time(at), .meta, .text2)) }
        let para = Para(runs, gap: first ? .none : .turn)
        return TranscriptItem(id: "h\(n)", paras: [para], source: para.text.replacingOccurrences(of: Transcript.markCharacter, with: ""))
    }

    func item(_ row: ThreadRow, gap: Gap, indent: Int) -> TranscriptItem? {
        switch row {
        case .tools(let first, let calls):
            return group(first, calls, gap: gap, indent: indent)
        case .step(let i, let step):
            let id = "s\(i)"
            let said = step.text ?? ""
            switch step.kind {
            case .prompt where directorAnswered.contains(i):
                return director(id, said, at: step.at, gap: gap)
            case .prompt:
                return prompt(id, said, brief: i == 0, at: step.at, gap: gap)
            case .text:
                var paras = Self.markdown(said, id: id, indent: indent)
                if !paras.isEmpty { paras[0].gap = gap }
                return TranscriptItem(id: id, paras: paras, source: said)
            case .thinking:
                return thinking(id, said, latest: running && i == steps.count - 1, gap: gap, indent: indent)
            case .tool:
                guard let call = step.tool else { return nil }
                return TranscriptItem(id: id, paras: tool(id, call, gap: gap, indent: indent, nest: 0), source: line(call), tool: call)
            case .question:
                guard let q = step.question else { return nil }
                return asked(id, q, at: step.at, gap: gap, indent: indent)
            }
        }
    }

    func line(_ call: ToolCall) -> String {
        [call.name, call.summary].filter { !$0.isEmpty }.joined(separator: " ")
    }

    func prompt(_ id: String, _ said: String, brief: Bool, at: String?, gap: Gap) -> TranscriptItem {
        let box: Box = brief ? .brief(id) : .told(id)
        let lines = said.components(separatedBy: "\n")
        let limit = brief ? 3 : 6
        let folds = said.count > 400 || lines.count > limit
        let expanded = open(id)
        var head = [Run(brief ? "Brief" : "Told", .meta)]
        if let at { head.append(Run(" " + time(at), .meta, .text3)) }
        if folds { head += [Run(" · ", .meta, .text3), Run(expanded ? "Show less" : "Show all", .meta, .link, toggle: id)] }
        var paras = [Para(head, gap: gap, box: box)]
        var shown = folds && !expanded ? Array(lines.prefix(limit)) : lines
        if folds && !expanded {
            var last = shown.removeLast()
            if last.count > 400 { last = String(last.prefix(400)) }
            shown.append(last + "…")
        }
        paras += shown.enumerated().map { n, line in
            Para([Run(line, .body, brief ? .text2 : .text)], gap: n == 0 ? .row : .none, box: box)
        }
        return TranscriptItem(id: id, paras: paras, source: said)
    }

    func director(_ id: String, _ said: String, at: String?, gap: Gap) -> TranscriptItem {
        var head = [Run.mark(.avatar(.done), .meta), Run("Director", .meta, .text2)]
        if let at { head.append(Run(" · " + time(at), .meta, .text2)) }
        head.append(Run(" answered from what was decided", .meta, .text3))
        var paras = [Para(head, gap: gap == .none ? .none : .turn)]
        paras += said.components(separatedBy: "\n").enumerated().map { n, line in
            Para([Run(line, .body)], indent: 1, gap: n == 0 ? .row : .none, box: .director(id))
        }
        return TranscriptItem(id: id, paras: paras, source: said)
    }

    func thinking(_ id: String, _ said: String, latest: Bool, gap: Gap, indent: Int) -> TranscriptItem {
        let expanded = open(id, byDefault: latest)
        var head = [Run.mark(.chevron(open: expanded), toggle: id), Run.mark(.symbol("brain"), .callout, .text2, toggle: id),
                    Run("Thinking", .calloutMedium, .text2, toggle: id)]
        if !expanded, let first = said.split(separator: "\n").first {
            head.append(Run(" " + first, .callout, .text3))
        }
        var paras = [Para(head, indent: indent, gap: gap, oneLine: !expanded)]
        if expanded {
            paras += said.components(separatedBy: "\n").enumerated().map { n, line in
                Para([Run(line, .body, .text2, italic: true)], indent: indent, nest: 1, gap: n == 0 ? .line : .none)
            }
        }
        return TranscriptItem(id: id, paras: paras, source: said)
    }

    func tool(_ id: String, _ call: ToolCall, gap: Gap, indent: Int, nest: Int) -> [Para] {
        let expanded = open(id)
        var head = [Run.mark(.chevron(open: expanded), toggle: id), Run.mark(.symbol(Transcript.symbol(tool: call.name)), .callout, .text2, toggle: id),
                    Run(call.name, .calloutMedium, toggle: id)]
        if call.failed { head.append(Run.mark(.symbol("xmark.circle.fill"), .callout, .failure)) }
        if !call.summary.isEmpty {
            head.append(Run(" " + (expanded ? call.summary : call.summary.replacingOccurrences(of: "\n", with: " ")), .codeSmall, .text2))
        }
        var paras = [Para(head, indent: indent, nest: nest, gap: gap, oneLine: !expanded)]
        guard expanded else { return paras }
        func block(_ title: String, _ text: String, ink: Ink, part: String) {
            paras.append(Para([Run(title, .micro, ink)], indent: indent, nest: nest + 1, gap: .row))
            paras.append(Para([Run(text.replacingOccurrences(of: "\n", with: Transcript.lineBreak), .codeSmall)],
                              indent: indent, nest: nest + 1, gap: .line, box: .code(id + part)))
        }
        if !call.input.isEmpty { block("INPUT", call.input, ink: .text3, part: "i") }
        if let result = call.result {
            block(call.failed ? "FAILED" : "RESULT", result.isEmpty ? "(no output)" : result, ink: call.failed ? .failure : .text3, part: "r")
        }
        return paras
    }

    func group(_ first: Int, _ calls: [ToolCall], gap: Gap, indent: Int) -> TranscriptItem {
        let id = "g\(first)"
        let expanded = open(id)
        var head = [Run.mark(.chevron(open: expanded), toggle: id), Run.mark(.symbol("wrench.and.screwdriver"), .callout, .text2, toggle: id),
                    Run(Transcript.summary(calls), .calloutMedium, .text2, toggle: id)]
        let failed = calls.filter(\.failed).count
        if failed > 0 { head.append(Run(" · \(failed) failed", .meta, .failure)) }
        var paras = [Para(head, indent: indent, gap: gap)]
        if expanded {
            for (k, call) in calls.enumerated() {
                paras += tool("s\(first + k)", call, gap: .row, indent: indent, nest: 1)
            }
        }
        return TranscriptItem(id: id, paras: paras, source: calls.map(line).joined(separator: "\n"))
    }

    func asked(_ id: String, _ q: Question, at: String?, gap: Gap, indent: Int) -> TranscriptItem {
        let box = Box.asked(id)
        var head = [Run("Asked", .meta, .text3)]
        if let at { head.append(Run(" · " + time(at), .meta, .text3)) }
        var paras = [Para(head, indent: indent, gap: gap, box: box)]
        for (i, item) in q.items.enumerated() {
            let answer = q.answers.flatMap { $0.indices.contains(i) ? $0[i] : nil }
            let parts = answer?.components(separatedBy: ", ") ?? []
            if !item.header.isEmpty { paras.append(Para([Run(item.header, .caption, .text3)], indent: indent, gap: .row, box: box)) }
            paras.append(Para([Run(item.question, .bodyMedium)], indent: indent, gap: item.header.isEmpty ? .row : .line, box: box))
            for option in item.options {
                let label = AnswerDraft.unmarked(option.label)
                let chosen = parts.contains(label)
                let mark = item.multi ? (chosen ? "☑" : "☐") : (chosen ? "◉" : "○")
                var runs = [Run(mark + " ", .calloutMedium), Run(label, chosen ? .calloutMedium : .callout, chosen ? .text : .text2)]
                if !option.description.isEmpty { runs.append(Run(" — " + option.description, .caption, .text3)) }
                paras.append(Para(runs, indent: indent, gap: .line, box: box))
            }
            if let answer, !item.options.contains(where: { parts.contains(AnswerDraft.unmarked($0.label)) }) {
                paras.append(Para([Run(answer, .callout)], indent: indent, gap: .line, box: box))
            }
        }
        if let reply = q.reply { paras.append(Para([Run(reply, .callout, .text2)], indent: indent, gap: .row, box: box)) }
        let source = q.items.map(\.question).joined(separator: "\n\n") + (q.answers.map { "\n\n" + $0.joined(separator: "\n") } ?? "")
        return TranscriptItem(id: id, paras: paras, source: source)
    }

    /// Replies already read: a conversation is laid out again on every
    /// read of it, and what was said never changes.
    private static let read = ParaCache()

    /// A reply's blocks as paragraphs.
    static func markdown(_ text: String, id: String, indent: Int) -> [Para] {
        let key = ParaCache.Key(text: text, id: id, indent: indent)
        if let known = read[key] { return known }
        let paras = parse(text, id: id, indent: indent)
        read[key] = paras
        return paras
    }

    static func parse(_ text: String, id: String, indent: Int) -> [Para] {
        var paras: [Para] = []
        for (b, block) in Markdown.blocks(text).enumerated() {
            let gap: Gap = b == 0 ? .none : .block
            switch block {
            case .heading(let level, let title):
                paras.append(Para(inline(title, level <= 1 ? .heading1 : .title), indent: indent, gap: b == 0 ? .none : .item))
            case .paragraph(let said):
                paras.append(Para(inline(said.replacingOccurrences(of: "\n", with: Transcript.lineBreak), .body), indent: indent, gap: gap))
            case .bullet(let said, let marker, let depth, let checked):
                let mark = checked.map { $0 ? "☑" : "☐" } ?? marker
                let lead = marker.isEmpty || checked == nil ? mark : marker + " " + mark
                let prior = paras.last.map { $0.hang } ?? false
                paras.append(Para([Run(lead + "\t", .body, .text3)] + inline(said.replacingOccurrences(of: "\n", with: Transcript.lineBreak), .body),
                                  indent: indent, nest: depth, hang: true, gap: prior ? .line : gap))
            case .code(let code):
                paras.append(Para([Run(code.replacingOccurrences(of: "\n", with: Transcript.lineBreak), .code)],
                                  indent: indent, gap: gap, box: .code("\(id)-\(b)")))
            case .quote(let said):
                paras.append(Para(inline(said.replacingOccurrences(of: "\n", with: Transcript.lineBreak), .body, ink: .text2),
                                  indent: indent, gap: gap, box: .quote("\(id)-\(b)")))
            case .rule:
                paras.append(Para([], indent: indent, gap: gap, box: .rule("\(id)-\(b)")))
            case .table(let table):
                let name = "\(id)-\(b)"
                let all = [table.header] + table.rows
                for (r, row) in all.enumerated() {
                    for (c, cell) in row.enumerated() {
                        let place = Cell(table: name, row: r, column: c, columns: table.columns, header: r == 0, align: table.alignments[c])
                        paras.append(Para(inline(cell, r == 0 ? .bodyMedium : .callout, bold: r == 0), indent: indent,
                                          gap: r == 0 && c == 0 ? gap : .none, box: .cell(place)))
                    }
                }
            }
        }
        return paras
    }

    static func inline(_ text: String, _ face: Face, ink: Ink = .text, bold: Bool = false) -> [Run] {
        Markdown.inline(text).map { r in
            Run(r.text, r.code ? .code : face, r.link != nil ? .link : ink, bold: bold || r.bold, italic: r.italic,
                strike: r.strike, chip: r.code, link: r.link)
        }
    }
}

/// Paragraphs by what they were read from; forgets everything once full, as
/// each is cheap to read again.
private final class ParaCache: @unchecked Sendable {
    struct Key: Hashable {
        let text: String
        let id: String
        let indent: Int
    }

    private var store: [Key: [Para]] = [:]
    private let lock = NSLock()
    private let limit = 4000

    subscript(key: Key) -> [Para]? {
        get { lock.withLock { store[key] } }
        set {
            lock.withLock {
                if store.count >= limit { store.removeAll(keepingCapacity: true) }
                store[key] = newValue
            }
        }
    }
}
