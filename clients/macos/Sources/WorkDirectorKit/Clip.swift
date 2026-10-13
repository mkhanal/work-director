import Foundation

/// What the app puts on the pasteboard when something is copied.
public enum Clip {
    /// Markdown as it reads: the words without the marks that style them.
    public static func plain(markdown: String) -> String {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        let lines = markdown.components(separatedBy: "\n").filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("```") }
        return lines.map { line in
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
