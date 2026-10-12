import SwiftUI
import WorkDirectorKit

/// The views a goal is read in, each answering one question fast: where it
/// stands, what was said, what each task is doing, and the full record.
enum GoalView: String, CaseIterable {
    case overview, conversation, tasks, record

    var label: String {
        switch self {
        case .overview: "Overview"
        case .conversation: "Conversation"
        case .tasks: "Tasks"
        case .record: "Record"
        }
    }

    var symbol: String {
        switch self {
        case .overview: "square.grid.2x2"
        case .conversation: "bubble.left.and.bubble.right"
        case .tasks: "checklist"
        case .record: "list.bullet.rectangle"
        }
    }
}

/// A row of tabs under a page's header, each with what it holds as a count,
/// and a mark on the one that needs you.
struct PageTabs<Tab: Hashable>: View {
    let tabs: [Tab]
    @Binding var selection: Tab
    let label: (Tab) -> String
    let symbol: (Tab) -> String
    var count: (Tab) -> Int? = { _ in nil }
    var alert: (Tab) -> Bool = { _ in false }

    var body: some View {
        HStack(spacing: Space.s1) {
            ForEach(tabs, id: \.self) { tab in
                Button { selection = tab } label: {
                    HStack(spacing: Space.s1 + 2) {
                        Image(systemName: symbol(tab)).font(.system(size: IconSize.small, weight: .medium))
                        Text(label(tab))
                        if let n = count(tab), n > 0 {
                            Text("\(n)").monospacedDigit().foregroundStyle(.tertiary)
                        }
                        if alert(tab) {
                            Circle().fill(Palette.attention).frame(width: 6, height: 6)
                        }
                    }
                    .font(selection == tab ? TypeScale.calloutStrong : TypeScale.calloutMedium)
                    .foregroundStyle(selection == tab ? .primary : .secondary)
                    .padding(.horizontal, Space.s3)
                    .padding(.vertical, Space.s1 + 2)
                    .background(selection == tab ? Palette.sunken.opacity(2) : .clear, in: RoundedRectangle(cornerRadius: Radius.chip + 2))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
    }
}

/// Where a goal stands, at a glance: what waits on you, how far its tasks have
/// come, who is working now, the supervisor's latest word and what you decided.
struct GoalOverview: View {
    let store: Store
    let goal: GoalDetail
    let gate: Gate?
    let pass: (Plan) -> Void
    let focus: (String) -> Void
    let answered: () -> Void
    let show: (GoalView) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s6) {
            if goal.waitingOnYou > 0 {
                OverviewSection(title: "Needs you", tint: Palette.attention) {
                    Waiting(store: store, goal: goal, open: focus, answered: answered)
                }
            }
            if let gate, let plan = Plan.passing(gate) {
                OverviewSection(title: "Next step") {
                    HStack(alignment: .center, spacing: Space.s4) {
                        VStack(alignment: .leading, spacing: Space.s1) {
                            Text(plan.title).font(TypeScale.title)
                            if let why = gate.why { Text(why).font(TypeScale.callout).foregroundStyle(.secondary) }
                        }
                        Spacer()
                        Button(plan.title) { pass(plan) }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.large)
                            .help(plan.commandLine)
                    }
                    .card()
                }
            }
            OverviewSection(title: "Progress", trailing: {
                Button("All tasks") { show(.tasks) }.buttonStyle(.link)
            }) {
                ProgressCard(progress: goal.progress)
            }
            if !goal.working.isEmpty {
                OverviewSection(title: "Working now") {
                    VStack(alignment: .leading, spacing: Space.s2) {
                        ForEach(goal.working) { task in
                            Button { focus(task.id) } label: {
                                AgentChip(task: task, activity: goal.activity[task.id], line: "")
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
            }
            if let latest = goal.latestSupervision {
                OverviewSection(title: "Latest from the supervisor", trailing: {
                    Button("Conversation") { show(.conversation) }.buttonStyle(.link)
                }) {
                    VStack(alignment: .leading, spacing: Space.s2) {
                        Text(when(latest.event.at)).font(TypeScale.meta).foregroundStyle(.tertiary)
                        SupervisorBody(reply: latest.reply, goal: goal, focus: focus)
                    }
                    .card()
                }
            }
            let decisions = goal.yours.filter { $0.kind == "decision" }
            if !decisions.isEmpty {
                OverviewSection(title: "What you decided") {
                    VStack(alignment: .leading, spacing: Space.s3) {
                        ForEach(decisions.reversed()) { e in
                            HStack(alignment: .firstTextBaseline, spacing: Space.s3) {
                                Image(systemName: "checkmark.seal").foregroundStyle(Color.accentColor)
                                Text(e.body).font(TypeScale.callout).lineLimit(3).textSelection(.enabled)
                                Spacer(minLength: Space.s2)
                                Text(when(e.at)).font(TypeScale.meta).foregroundStyle(.tertiary)
                            }
                        }
                    }
                    .card()
                }
            }
            if !goal.goal.detail.isEmpty {
                OverviewSection(title: "What you asked") {
                    Text(goal.goal.detail).readable().textSelection(.enabled).lineLimit(10).card()
                }
            }
        }
    }
}

/// One section of the overview: a small title, then what it holds.
struct OverviewSection<Content: View, Trailing: View>: View {
    let title: String
    var tint: Color? = nil
    @ViewBuilder var trailing: Trailing
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            HStack {
                MicroLabel(title).foregroundStyle(tint ?? .secondary)
                Spacer()
                trailing.font(TypeScale.meta)
            }
            content
        }
    }
}

extension OverviewSection where Trailing == EmptyView {
    init(title: String, tint: Color? = nil, @ViewBuilder content: () -> Content) {
        self.init(title: title, tint: tint, trailing: { EmptyView() }, content: content)
    }
}

/// How far a goal's tasks have come: one bar, coloured by where each stands,
/// and the counts under it.
struct ProgressCard: View {
    let progress: TaskProgress

    private var parts: [(String, Int, Color)] {
        [("Done", progress.done, Palette.settled), ("Working", progress.working, .accentColor),
         ("Review", progress.review, .purple), ("Blocked", progress.blocked, Palette.failure),
         ("Waiting", progress.waiting, .secondary.opacity(0.5)), ("Dropped", progress.dropped, .secondary.opacity(0.25))]
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            HStack(alignment: .firstTextBaseline) {
                Text("\(progress.done) of \(progress.total)").font(TypeScale.title).monospacedDigit()
                Text("tasks done").font(TypeScale.callout).foregroundStyle(.secondary)
            }
            GeometryReader { g in
                HStack(spacing: 2) {
                    ForEach(parts.filter { $0.1 > 0 }, id: \.0) { part in
                        Rectangle().fill(part.2)
                            .frame(width: max(4, (g.size.width - 2 * CGFloat(parts.count)) * CGFloat(part.1) / CGFloat(max(progress.total, 1))))
                    }
                }
            }
            .frame(height: 8)
            .clipShape(Capsule())
            HStack(spacing: Space.s4) {
                ForEach(parts.filter { $0.1 > 0 }, id: \.0) { part in
                    HStack(spacing: Space.s1 + 2) {
                        Circle().fill(part.2).frame(width: 7, height: 7)
                        Text("\(part.1)").monospacedDigit()
                        Text(part.0).foregroundStyle(.secondary)
                    }
                }
            }
            .font(TypeScale.meta)
        }
        .card()
    }
}

/// The conversation's kinds of turn, as chips to narrow it to one.
struct FilterChips: View {
    @Binding var filter: ThreadFilter
    let counts: [ThreadFilter: Int]

    var body: some View {
        HStack(spacing: Space.s2) {
            ForEach(ThreadFilter.allCases, id: \.self) { f in
                Button { filter = f } label: {
                    HStack(spacing: Space.s1) {
                        Text(f.label)
                        if let n = counts[f] { Text("\(n)").monospacedDigit().foregroundStyle(filter == f ? .secondary : .tertiary) }
                    }
                    .font(TypeScale.meta)
                    .padding(.horizontal, Space.s3)
                    .padding(.vertical, 5)
                    .background(filter == f ? Color.accentColor.opacity(0.14) : Palette.sunken, in: Capsule())
                    .overlay(Capsule().strokeBorder(filter == f ? Color.accentColor.opacity(0.4) : .clear))
                    .foregroundStyle(filter == f ? Color.accentColor : .primary)
                }
                .buttonStyle(.plain)
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

/// Every task of a goal in one table: where it stands, what its agent is
/// doing, its last verdict, when it last moved and what it spent.
struct GoalTasks: View {
    let goal: GoalDetail
    let focus: (String) -> Void
    @State private var sort: Sort = .plan

    enum Sort: String, CaseIterable { case plan = "Plan order", state = "State", recent = "Recent" }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            HStack {
                Text("\(goal.tasks.count) tasks").font(TypeScale.meta).foregroundStyle(.secondary)
                Spacer()
                Picker("Sort", selection: $sort) {
                    ForEach(Sort.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .fixedSize()
                .labelsHidden()
            }
            VStack(spacing: 0) {
                header
                ForEach(rows) { task in
                    Divider()
                    TaskRow(task: task, activity: goal.activity[task.id], verdict: verdict(of: task.id))
                        .contentShape(Rectangle())
                        .onTapGesture { focus(task.id) }
                        .help("Show this task's conversation")
                }
            }
            .background(Palette.raised, in: RoundedRectangle(cornerRadius: Radius.card))
            .overlay(RoundedRectangle(cornerRadius: Radius.card).strokeBorder(Palette.separator))
        }
    }

    private var header: some View {
        HStack(spacing: Space.s3) {
            Text("Task").frame(maxWidth: .infinity, alignment: .leading)
            Text("Now").frame(width: 240, alignment: .leading)
            Text("Report").frame(width: 76, alignment: .leading)
            Text("Moved").frame(width: 64, alignment: .trailing)
            Text("Tokens").frame(width: 64, alignment: .trailing)
        }
        .font(TypeScale.micro)
        .foregroundStyle(.tertiary)
        .padding(.horizontal, Space.s4)
        .padding(.vertical, Space.s2)
    }

    private var rows: [Work] {
        switch sort {
        case .plan: goal.tasks
        case .state: goal.tasks.sorted { rank($0.state) < rank($1.state) }
        case .recent: goal.tasks.sorted { $0.updated > $1.updated }
        }
    }

    private func rank(_ s: WorkState) -> Int {
        [.needsInput, .blocked, .running, .review, .queued, .briefed, .paused, .softDone, .done, .dropped, .abandoned].firstIndex(of: s) ?? 99
    }

    private func verdict(of id: String) -> String? {
        goal.events.last { $0.work == id && $0.kind == "report" }.flatMap { goal.reports[String($0.id)]?.verdict }
    }
}

struct TaskRow: View {
    let task: Work
    let activity: Activity?
    let verdict: String?

    var body: some View {
        HStack(spacing: Space.s3) {
            HStack(spacing: Space.s2) {
                StateSymbol(state: task.state)
                Text(task.title).font(TypeScale.callout).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Text(activity?.error ?? activity?.now ?? "—")
                .font(TypeScale.callout)
                .foregroundStyle(activity?.asking != nil ? Palette.attention : .secondary)
                .lineLimit(1)
                .frame(width: 240, alignment: .leading)
            Group {
                if let verdict, !verdict.isEmpty { VerdictPill(verdict: verdict) } else { Text("—").foregroundStyle(.tertiary) }
            }
            .frame(width: 76, alignment: .leading)
            Text(ago(task.updated)).font(TypeScale.meta.monospacedDigit()).foregroundStyle(.secondary)
                .frame(width: 64, alignment: .trailing)
            Text(activity?.usage.map { tokenCount($0.tokens) } ?? "—").font(TypeScale.meta.monospacedDigit()).foregroundStyle(.secondary)
                .frame(width: 64, alignment: .trailing)
        }
        .padding(.horizontal, Space.s4)
        .padding(.vertical, Space.s2 + 2)
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
