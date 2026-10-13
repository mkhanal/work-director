import Foundation

// Markdown as executors write it, read into blocks a screen can lay out:
// headings, paragraphs, lists nested by indentation, code, quotes, rules and
// GitHub pipe tables.

public enum MarkdownBlock: Hashable, Sendable {
    case heading(level: Int, text: String)
    /// Lines of one paragraph, each line kept as written.
    case paragraph(String)
    /// A list item: its marker (`•` or `1.`), how deep it is nested, and
    /// whether its box is ticked when it is a task.
    case bullet(String, marker: String, depth: Int, checked: Bool?)
    case code(String)
    case quote(String)
    case rule
    case table(MarkdownTable)
}

public struct MarkdownTable: Hashable, Sendable {
    public enum Alignment: Hashable, Sendable { case leading, center, trailing }

    public let header: [String]
    public let alignments: [Alignment]
    /// Every row has one cell per column.
    public let rows: [[String]]

    public var columns: Int { header.count }
}

/// A piece of a line as it reads: its words and how they are set.
public struct InlineRun: Hashable, Sendable {
    public var text: String
    public var bold = false
    public var italic = false
    public var code = false
    public var strike = false
    public var link: String?

    public init(_ text: String, bold: Bool = false, italic: Bool = false, code: Bool = false, strike: Bool = false, link: String? = nil) {
        self.text = text
        self.bold = bold
        self.italic = italic
        self.code = code
        self.strike = strike
        self.link = link
    }
}

public enum Markdown {
    public static func blocks(_ text: String) -> [MarkdownBlock] {
        var out: [MarkdownBlock] = []
        var paragraph: [String] = []
        var quote: [String] = []
        var code: [String]?
        var fence = ""
        // The indentation of each list item still open, outermost first.
        var listIndents: [Int] = []
        // Whether the line before was part of a list item, so an unmarked
        // line under it continues it.
        var inItem = false

        func flush() {
            if !paragraph.isEmpty { out.append(.paragraph(paragraph.joined(separator: "\n"))) }
            paragraph = []
            if !quote.isEmpty { out.append(.quote(quote.joined(separator: "\n"))) }
            quote = []
        }

        let lines = text.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
        var i = 0
        while i < lines.count {
            let raw = lines[i]
            let line = raw.trimmingCharacters(in: .whitespaces)
            defer { i += 1 }
            if let open = code {
                if line.hasPrefix(fence) && line.allSatisfy({ $0 == fence.first }) {
                    out.append(.code(open.joined(separator: "\n")))
                    code = nil
                } else {
                    code?.append(raw)
                }
                continue
            }
            if line.hasPrefix("```") || line.hasPrefix("~~~") {
                flush()
                inItem = false
                fence = String(line.prefix { $0 == line.first })
                code = []
                continue
            }
            if line.isEmpty {
                flush()
                inItem = false
                continue
            }
            if line.hasPrefix(">") {
                if !paragraph.isEmpty { flush() }
                inItem = false
                var rest = line.dropFirst()
                if rest.hasPrefix(" ") { rest = rest.dropFirst() }
                quote.append(String(rest))
                continue
            }
            if !quote.isEmpty { flush() }
            if let level = setext(line), !paragraph.isEmpty {
                let title = paragraph.joined(separator: " ")
                paragraph = []
                out.append(.heading(level: level, text: title))
                continue
            }
            if isRule(line) {
                flush()
                inItem = false
                listIndents = []
                out.append(.rule)
                continue
            }
            if let (level, title) = heading(line) {
                flush()
                inItem = false
                listIndents = []
                out.append(.heading(level: level, text: title))
                continue
            }
            if i + 1 < lines.count, let table = table(lines, at: i) {
                flush()
                inItem = false
                out.append(.table(table.table))
                i = table.next - 1
                continue
            }
            if let item = listItem(raw) {
                flush()
                while let last = listIndents.last, last >= item.indent { listIndents.removeLast() }
                out.append(.bullet(item.text, marker: item.marker, depth: listIndents.count, checked: item.checked))
                listIndents.append(item.indent)
                inItem = true
                continue
            }
            if inItem, case .bullet(let said, let marker, let depth, let checked)? = out.last {
                out[out.count - 1] = .bullet(said + "\n" + line, marker: marker, depth: depth, checked: checked)
                continue
            }
            listIndents = []
            paragraph.append(raw)
        }
        if let open = code { out.append(.code(open.joined(separator: "\n"))) }
        flush()
        return out
    }

    /// A line's words and their emphasis, code and links, the marks gone.
    public static func inline(_ text: String) -> [InlineRun] {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        guard let parsed = try? AttributedString(markdown: text, options: options) else { return [InlineRun(text)] }
        var out: [InlineRun] = []
        for run in parsed.runs {
            let intent = run.inlinePresentationIntent ?? []
            let piece = InlineRun(String(parsed[run.range].characters),
                                  bold: intent.contains(.stronglyEmphasized), italic: intent.contains(.emphasized),
                                  code: intent.contains(.code), strike: intent.contains(.strikethrough),
                                  link: run.link?.absoluteString)
            if let last = out.last, last.bold == piece.bold, last.italic == piece.italic, last.code == piece.code,
               last.strike == piece.strike, last.link == piece.link {
                out[out.count - 1].text += piece.text
            } else {
                out.append(piece)
            }
        }
        return out
    }

    /// A line as it reads, without the marks that style it.
    public static func plainLine(_ text: String) -> String {
        inline(text).map(\.text).joined()
    }

    // MARK: lines

    static func heading(_ line: String) -> (Int, String)? {
        let hashes = line.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes) else { return nil }
        let rest = line.dropFirst(hashes)
        guard rest.first == " " else { return nil }
        var title = rest.trimmingCharacters(in: .whitespaces)
        while title.hasSuffix("#") { title.removeLast() }
        return (hashes, title.trimmingCharacters(in: .whitespaces))
    }

    static func setext(_ line: String) -> Int? {
        guard line.count >= 2 else { return nil }
        if line.allSatisfy({ $0 == "=" }) { return 1 }
        if line.allSatisfy({ $0 == "-" }) { return 2 }
        return nil
    }

    static func isRule(_ line: String) -> Bool {
        let marks = line.filter { $0 != " " }
        guard marks.count >= 3, let first = marks.first, "-*_".contains(first) else { return false }
        return marks.allSatisfy { $0 == first }
    }

    struct ListItem {
        let indent: Int
        let marker: String
        let text: String
        let checked: Bool?
    }

    static func listItem(_ raw: String) -> ListItem? {
        var indent = 0
        for c in raw {
            if c == " " { indent += 1 } else if c == "\t" { indent += 4 } else { break }
        }
        let line = raw.drop { $0 == " " || $0 == "\t" }
        var marker: String
        var rest: Substring
        if let c = line.first, "-*+".contains(c), line.dropFirst().first == " " {
            marker = "•"
            rest = line.dropFirst(2)
        } else {
            let digits = line.prefix { $0.isNumber }
            guard !digits.isEmpty, digits.count <= 9 else { return nil }
            let after = line.dropFirst(digits.count)
            guard let close = after.first, close == "." || close == ")", after.dropFirst().first == " " else { return nil }
            marker = digits + "."
            rest = after.dropFirst(2)
        }
        var checked: Bool?
        if rest.hasPrefix("[ ] ") {
            checked = false
        } else if rest.hasPrefix("[x] ") || rest.hasPrefix("[X] ") {
            checked = true
        }
        if checked != nil {
            rest = rest.dropFirst(4)
            if marker == "•" { marker = "" }
        }
        return ListItem(indent: indent, marker: marker, text: rest.trimmingCharacters(in: .whitespaces), checked: checked)
    }

    /// A pipe table opening at line i: a header row, then a row of dashes
    /// with one cell per column. Its body runs to the first line without a
    /// pipe.
    static func table(_ lines: [String], at i: Int) -> (table: MarkdownTable, next: Int)? {
        let head = lines[i].trimmingCharacters(in: .whitespaces)
        guard head.contains("|") else { return nil }
        let header = cells(head)
        guard let alignments = delimiter(lines[i + 1].trimmingCharacters(in: .whitespaces)),
              alignments.count == header.count else { return nil }
        var rows: [[String]] = []
        var n = i + 2
        while n < lines.count {
            let line = lines[n].trimmingCharacters(in: .whitespaces)
            guard !line.isEmpty, line.contains("|") else { break }
            var row = cells(line)
            if row.count < header.count { row += Array(repeating: "", count: header.count - row.count) }
            rows.append(Array(row.prefix(header.count)))
            n += 1
        }
        return (MarkdownTable(header: header, alignments: alignments, rows: rows), n)
    }

    static func delimiter(_ line: String) -> [MarkdownTable.Alignment]? {
        guard line.contains("-") else { return nil }
        let parts = cells(line)
        var out: [MarkdownTable.Alignment] = []
        for part in parts {
            let dashes = part.trimmingCharacters(in: CharacterSet(charactersIn: ":"))
            guard !dashes.isEmpty, dashes.allSatisfy({ $0 == "-" }) else { return nil }
            switch (part.hasPrefix(":"), part.hasSuffix(":")) {
            case (true, true): out.append(.center)
            case (false, true): out.append(.trailing)
            default: out.append(.leading)
            }
        }
        return out.isEmpty ? nil : out
    }

    /// A table row's cells: split at each pipe that is neither escaped nor
    /// inside a code span, the outer pipes dropped.
    static func cells(_ line: String) -> [String] {
        var row = Substring(line)
        if row.hasPrefix("|") { row = row.dropFirst() }
        if row.hasSuffix("|") && !row.hasSuffix("\\|") { row = row.dropLast() }
        var out: [String] = []
        var cell = ""
        var inCode = false
        var escaped = false
        for c in row {
            if escaped {
                cell.append(c == "|" ? "|" : "\\\(c)")
                escaped = false
            } else if c == "\\" {
                escaped = true
            } else if c == "`" {
                inCode.toggle()
                cell.append(c)
            } else if c == "|" && !inCode {
                out.append(cell.trimmingCharacters(in: .whitespaces))
                cell = ""
            } else {
                cell.append(c)
            }
        }
        if escaped { cell.append("\\") }
        out.append(cell.trimmingCharacters(in: .whitespaces))
        return out
    }
}
