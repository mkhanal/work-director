import Foundation

/// Finding words on a page: what the person typed, wherever the page shows
/// it, with case and accents ignored.
public enum Find {
    /// Where `query` is found in `text`, in order and never overlapping; an
    /// empty query is found nowhere.
    public static func ranges(of query: String, in text: String) -> [NSRange] {
        guard !query.isEmpty else { return [] }
        let text = text as NSString
        var found: [NSRange] = []
        var from = 0
        while from < text.length {
            let r = text.range(of: query, options: [.caseInsensitive, .diacriticInsensitive], range: NSRange(location: from, length: text.length - from))
            if r.location == NSNotFound { break }
            found.append(r)
            from = r.location + r.length
        }
        return found
    }

    /// A place on a page: which of its texts, top to bottom, and how far into it.
    public struct Spot: Comparable, Hashable, Sendable {
        public let part: Int
        public let at: Int

        public init(part: Int, at: Int) {
            self.part = part
            self.at = at
        }

        public static func < (a: Spot, b: Spot) -> Bool {
            (a.part, a.at) < (b.part, b.at)
        }
    }

    /// The match that is current once the matches change, as the person types
    /// or the page refreshes: the first at or after where the current one
    /// was, else the last. `spots` are in page order.
    public static func keep(_ was: Spot, in spots: [Spot]) -> Int? {
        spots.firstIndex { $0 >= was } ?? spots.indices.last
    }

    /// The match `by` steps on from the current one, round from the last to
    /// the first and back; with none current, the first going forward and the
    /// last going back.
    public static func step(_ current: Int?, by: Int, count: Int) -> Int? {
        guard count > 0 else { return nil }
        guard let current else { return by >= 0 ? 0 : count - 1 }
        return ((current + by) % count + count) % count
    }

    /// How the find bar counts: "3 of 12", "No results", or nothing before
    /// anything is typed.
    public static func label(_ current: Int?, of count: Int, query: String) -> String {
        if query.isEmpty { return "" }
        if count == 0 { return "No results" }
        return "\((current ?? 0) + 1) of \(count)"
    }
}
