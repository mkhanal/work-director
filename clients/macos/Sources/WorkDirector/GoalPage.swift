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
            }
        }
    }
}

/// Where one day's turns begin.
struct DayLine: View {
    let day: String

    var body: some View {
        HStack(spacing: Space.s3) {
            Text(day).font(TypeScale.meta.weight(.semibold)).foregroundStyle(.secondary)
            Rectangle().fill(Palette.separator).frame(height: 1)
        }
        .padding(.top, Space.s2)
    }

    /// The day an item falls on, when it is not the day of the one before.
    static func day(of item: ThreadItem, after previous: ThreadItem?) -> String? {
        guard let date = date(of: item) else { return nil }
        if let previous, let before = self.date(of: previous), Calendar.current.isDate(date, inSameDayAs: before) { return nil }
        if Calendar.current.isDateInToday(date) { return "Today" }
        if Calendar.current.isDateInYesterday(date) { return "Yesterday" }
        return date.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())
    }

    private static func date(of item: ThreadItem) -> Date? {
        let at: String = switch item {
        case .step(let s): s.event.at
        case .updates(let run): run[0].event.at
        }
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return parser.date(from: at) ?? ISO8601DateFormatter().date(from: at)
    }
}

/// A run of updates folded into one line, opened to each.
struct UpdatesRow: View {
    let run: [ThreadStep]
    let line: (ThreadStep) -> AnyView
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s2) {
            Button { withAnimation(.easeOut(duration: 0.15)) { open.toggle() } } label: {
                HStack(spacing: Space.s3) {
                    Rectangle().fill(Palette.separator).frame(height: 1)
                    HStack(spacing: Space.s1 + 2) {
                        Image(systemName: open ? "chevron.down" : "chevron.right").font(.system(size: IconSize.tiny, weight: .bold))
                        Text("\(run.count) updates")
                        Text("· " + when(run[0].event.at))
                    }
                    .font(TypeScale.meta)
                    .foregroundStyle(.tertiary)
                    .fixedSize()
                    Rectangle().fill(Palette.separator).frame(height: 1)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Show each update")
            if open {
                ForEach(run, id: \.id) { step in line(step) }
            }
        }
    }
}

/// How long ago an ISO time was, in the fewest characters.
func ago(_ stamp: String) -> String {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    guard let d = f.date(from: stamp) ?? ISO8601DateFormatter().date(from: stamp) else { return "" }
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
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    guard let d = f.date(from: stamp) ?? ISO8601DateFormatter().date(from: stamp) else { return "" }
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
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    guard let d = f.date(from: stamp) ?? ISO8601DateFormatter().date(from: stamp) else { return "" }
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
