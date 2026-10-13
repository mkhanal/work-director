import Foundation

/// What the app puts on the pasteboard when something is copied.
public enum Clip {
    /// Markdown as it reads: the words without the marks that style them.
    public static func plain(markdown: String) -> String {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        var lines = markdown.components(separatedBy: "\n").filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("```") }
        // A table reads one row per line, its cells separated by tabs.
        var i = 0
        while i + 1 < lines.count {
            guard let table = Markdown.table(lines, at: i) else {
                i += 1
                continue
            }
            let rows = ([table.table.header] + table.table.rows).map { $0.map(Markdown.plainLine).joined(separator: "\t") }
            lines.replaceSubrange(i..<table.next, with: rows.map { Self.tableRow + $0 })
            i += rows.count
        }
        return lines.map { line in
            if line.hasPrefix(Self.tableRow) { return String(line.dropFirst()) }
            var l = Substring(line)
            let trimmed = l.drop { $0 == " " }
            if let hashes = trimmed.firstIndex(where: { $0 != "#" }), hashes != trimmed.startIndex, trimmed[hashes] == " " {
                l = trimmed[trimmed.index(after: hashes)...]
            }
            guard let attributed = try? AttributedString(markdown: String(l), options: options) else { return String(l) }
            return String(attributed.characters)
        }
        .joined(separator: "\n")
    }

    /// Marks a line already read as a table row.
    private static let tableRow = "\u{1}"

    /// A goal as one line: its title and its id.
    public static func work(_ w: Work) -> String { "\(w.title) (\(w.id))" }
}

/// The agents panel's width: its own column beside the page.
public enum PanelWidth {
    public static let min: Double = 260
    public static let max: Double = 480
    public static let standard: Double = 320

    /// A width as dragged, held within the panel's bounds.
    public static func clamped(_ w: Double) -> Double { Swift.min(max, Swift.max(min, w)) }
}
