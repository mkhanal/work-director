import SwiftUI
import WorkDirectorKit

/// A goal read as a conversation: what was asked, the plan it became, what
/// happened since across its tasks, and what waits on you, last, next to
/// where you answer.
struct GoalThread: View {
    let store: Store
    let goal: GoalDetail
    /// Shows a task's conversation.
    let focus: (String) -> Void
    let answered: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            if let delivery = goal.delivery, delivery.status != "none" || !goal.landings.isEmpty {
                DeliveryLine(delivery: delivery, landings: goal.landings)
            }
            if !goal.goal.detail.isEmpty {
                PromptRow(text: goal.goal.detail, brief: false, at: goal.goal.created, label: "Asked")
            }
            PlanBlock(goal: goal, focus: focus)
            ForEach(timeline, id: \.id) { event in
                TimelineRow(event: event, task: title(of: event.work), focus: event.work == goal.goal.id ? nil : { focus(event.work) })
            }
            Waiting(store: store, goal: goal, open: focus, answered: answered)
        }
    }

    /// What happened, oldest first: what was said and decided, reported,
    /// verified and landed. A state change is shown only where it ends work.
    private var timeline: [Event] {
        goal.events
            .filter { e in
                switch e.kind {
                case "decision", "report", "sent", "answer", "verify", "pr", "question": true
                case "state": ["done", "dropped", "abandoned", "paused", "blocked"].contains(e.body)
                default: false
                }
            }
            .sorted { $0.at == $1.at ? $0.id < $1.id : $0.at < $1.at }
    }

    private func title(of id: String) -> String? {
        id == goal.goal.id ? nil : goal.tasks.first { $0.id == id }?.title
    }
}

/// The tasks a goal was planned into, grouped as planned, each a way into its
/// conversation.
struct PlanBlock: View {
    let goal: GoalDetail
    let focus: (String) -> Void

    var body: some View {
        if goal.tasks.isEmpty {
            Label("Not planned yet.", systemImage: "list.bullet.rectangle").foregroundStyle(.secondary)
        } else {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label("Plan", systemImage: "list.bullet.rectangle").font(.caption.weight(.semibold))
                    Text("\(goal.rollup.done) of \(goal.rollup.total) done").font(.caption)
                }
                .foregroundStyle(.secondary)
                ForEach(headings, id: \.self) { heading in
                    if !heading.isEmpty && heading != "(no heading)" {
                        Text(heading).font(.caption.weight(.medium)).foregroundStyle(.secondary).padding(.top, 2)
                    }
                    ForEach(goal.tasks.filter { ($0.heading ?? "") == heading }) { task in
                        Button { focus(task.id) } label: {
                            HStack(spacing: 8) {
                                StateSymbol(state: task.state)
                                Text(task.title).lineLimit(2).multilineTextAlignment(.leading)
                                Spacer(minLength: 0)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .help("Show this task's conversation")
                    }
                }
            }
            .padding(12)
            .background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    private var headings: [String] {
        var seen: [String] = []
        for t in goal.tasks where !seen.contains(t.heading ?? "") { seen.append(t.heading ?? "") }
        return seen
    }
}

/// One thing that happened on a goal or one of its tasks.
struct TimelineRow: View {
    let event: Event
    /// The task it happened on, or nil for the goal itself.
    let task: String?
    let focus: (() -> Void)?

    var body: some View {
        switch event.kind {
        case "sent":
            PromptRow(text: event.body, brief: false, at: event.at)
        case "report":
            VStack(alignment: .leading, spacing: 6) {
                label("Reported", "doc.text.magnifyingglass")
                MarkdownText(event.body)
            }
        default:
            VStack(alignment: .leading, spacing: 4) {
                label(title, symbol)
                Text(event.decision?.answer ?? event.body)
                    .foregroundStyle(event.kind == "state" ? .secondary : .primary)
                    .textSelection(.enabled)
                    .lineLimit(6)
            }
        }
    }

    private func label(_ text: String, _ symbol: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: symbol)
            Text(text).font(.caption.weight(.semibold))
            if let task {
                if let focus {
                    Button(task, action: focus).buttonStyle(.link).lineLimit(1)
                } else {
                    Text(task).lineLimit(1)
                }
            }
            Text(when(event.at)).foregroundStyle(.tertiary)
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }

    private var title: String {
        switch event.kind {
        case "decision": "Decided"
        case "answer": "Answered"
        case "question": "Asked"
        case "verify": event.body.hasPrefix("pass") ? "Verified" : "Verify failed"
        case "pr": "Landed"
        case "state": "Now \(event.body)"
        default: event.kind.capitalized
        }
    }

    private var symbol: String {
        switch event.kind {
        case "decision": "checkmark.seal"
        case "answer": "arrowshape.turn.up.left"
        case "question": "questionmark.bubble"
        case "verify": event.body.hasPrefix("pass") ? "checkmark.shield" : "xmark.shield"
        case "pr": "arrow.triangle.pull"
        default: "circle.dashed"
        }
    }
}

/// Where the goal's work reached, in a line, with its landings.
struct DeliveryLine: View {
    let delivery: Delivery
    let landings: [Landed]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: "shippingbox")
                Text(delivery.status).font(.callout.weight(.medium))
                if !delivery.detail.isEmpty {
                    Text(delivery.detail).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            ForEach(landings, id: \.url) { LandingRow(landed: $0).font(.caption) }
        }
    }
}
