import SwiftUI
import WorkDirectorKit



/// A row of tabs under a page's header, the chosen one filled.
struct PageTabs<Tab: Hashable>: View {
    let tabs: [Tab]
    @Binding var selection: Tab
    let label: (Tab) -> String

    var body: some View {
        HStack(spacing: Space.s1) {
            ForEach(tabs, id: \.self) { tab in
                Button { selection = tab } label: {
                    Text(label(tab))
                        .font(selection == tab ? TypeScale.calloutStrong : TypeScale.callout)
                        .foregroundStyle(selection == tab ? Palette.text : Palette.text2)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(selection == tab ? Palette.inner : .clear, in: RoundedRectangle(cornerRadius: Radius.button))
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
    }
}

/// The chips a goal's conversation is narrowed by.
struct FilterChips: View {
    @Binding var filter: ThreadFilter
    let goal: GoalDetail

    var body: some View {
        FlowLayout(spacing: 6) {
            ForEach(Array(goal.filters.enumerated()), id: \.offset) { _, f in
                FilterChip(label: f.label, selected: filter == f.filter) { filter = f.filter }
                    .help(f.title)
            }
        }
    }
}

/// Where one day's turns begin.
enum DayLine {
    /// The day an ISO time falls on, as a reader names it.
    static func day(_ at: String) -> String? {
        guard let date = instant(at) else { return nil }
        if Calendar.current.isDateInToday(date) { return "Today" }
        if Calendar.current.isDateInYesterday(date) { return "Yesterday" }
        return date.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())
    }
}

/// How long ago an ISO time was, in the fewest characters.
func ago(_ stamp: String) -> String {
    guard let d = instant(stamp) else { return "" }
    let s = Int(Date().timeIntervalSince(d))
    switch s {
    case ..<60: return "now"
    case ..<3600: return "\(s / 60)m"
    case ..<86400: return "\(s / 3600)h"
    default: return "\(s / 86400)d"
    }
}

/// How long ago an ISO time was, in words: "4 min ago", "2h ago".
func agoWords(_ stamp: String) -> String {
    guard let d = instant(stamp) else { return "" }
    let s = Int(Date().timeIntervalSince(d))
    switch s {
    case ..<60: return "just now"
    case ..<3600: return "\(s / 60) min ago"
    case ..<86400: return "\(s / 3600)h ago"
    case ..<172800: return "yesterday"
    default: return "\(s / 86400) days ago"
    }
}

/// An ISO time as the hour and minute it was, for a record row.
func clock(_ stamp: String) -> String {
    guard let d = instant(stamp) else { return "" }
    return d.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits))
}

/// Chips laid out in rows, wrapping at the width they are given.
struct FlowLayout: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, line: CGFloat = 0, widest: CGFloat = 0
        for v in subviews {
            let size = v.sizeThatFits(.unspecified)
            if x > 0 && x + size.width > width {
                y += line + spacing
                x = 0
                line = 0
            }
            x += size.width + spacing
            line = max(line, size.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, width), height: y + line)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, line: CGFloat = 0
        for v in subviews {
            let size = v.sizeThatFits(.unspecified)
            if x > bounds.minX && x + size.width > bounds.maxX {
                y += line + spacing
                x = bounds.minX
                line = 0
            }
            v.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            line = max(line, size.height)
        }
    }
}
