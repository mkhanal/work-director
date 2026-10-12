import SwiftUI
import WorkDirectorKit

/// A goal read as one conversation: what was asked, the plan it became, what
/// its supervisor and its agents did since, and what waits on you, last, next
/// to where you answer.
struct GoalThread: View {
    let store: Store
    let goal: GoalDetail
    /// Shows a task's conversation.
    let focus: (String) -> Void
    let answered: () -> Void

    var body: some View {
        LazyVStack(alignment: .leading, spacing: Layout.step) {
            if let delivery = goal.delivery, delivery.status != "none" || delivery.error != nil || !goal.landings.isEmpty {
                DeliveryLine(delivery: delivery, landings: goal.landings)
            }
            if !goal.goal.detail.isEmpty {
                YouBubble(text: goal.goal.detail, at: when(goal.goal.created), label: "You asked")
            }
            PlanBlock(goal: goal, focus: focus)
            ForEach(goal.thread) { step in
                switch step {
                case .event(let event) where goal.isYours(event):
                    YouBubble(text: event.decision?.answer ?? event.body, at: when(event.at), label: event.kind == "answer" ? "You answered" : "You")
                case .event(let event):
                    TimelineRow(event: event, task: title(of: event.work), focus: event.work == goal.goal.id ? nil : { focus(event.work) })
                case .look(let event):
                    Label("Supervisor looking · \(event.body)", systemImage: "eye")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                case .supervisor(let event, let reply):
                    SupervisorRow(event: event, reply: reply, goal: goal, open: { focus(goal.goal.id) }, focus: focus)
                case .stopped(let event, let said):
                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 6) {
                            Image(systemName: "pause.circle")
                            Text("Stopped without a report").font(.caption.weight(.semibold))
                            if let task = title(of: event.work) {
                                Button(task) { focus(event.work) }.buttonStyle(.link).lineLimit(1)
                            }
                            Text(when(event.at)).foregroundStyle(.tertiary)
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        Text(said).foregroundStyle(.secondary).lineLimit(3).textSelection(.enabled)
                    }
                }
            }
            ForEach(store.outbox.filter { $0.work == goal.goal.id && !$0.arrived(in: goal) }) { message in
                OutgoingBubble(store: store, message: message)
            }
            Waiting(store: store, goal: goal, open: focus, answered: answered)
        }
    }

    private func title(of id: String) -> String? {
        id == goal.goal.id ? nil : goal.tasks.first { $0.id == id }?.title
    }
}

/// A supervisor's reply in the goal's conversation: what it said, then each
/// agent it told, started or filed, live, and what was refused.
struct SupervisorRow: View {
    let event: Event
    let reply: SupervisorReply
    let goal: GoalDetail
    /// Shows the supervisor's own conversation.
    let open: () -> Void
    let focus: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 6) {
                Image(systemName: "person.badge.shield.checkmark")
                Button("Supervisor", action: open).buttonStyle(.link).font(.caption.weight(.semibold))
                Text(when(event.at)).foregroundStyle(.tertiary)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            if !reply.said.isEmpty {
                MarkdownText(reply.said)
            }
            ForEach(Array(reply.actions.enumerated()), id: \.offset) { _, action in
                if let id = action.task, let task = goal.tasks.first(where: { $0.id == id }) {
                    Button { focus(id) } label: { AgentChip(task: task, activity: goal.activity[id], line: action.line) }
                        .buttonStyle(.plain)
                        .help("Show this agent's conversation")
                } else {
                    Label(action.line, systemImage: "checkmark.circle").font(.callout).foregroundStyle(.secondary)
                }
            }
            ForEach(reply.refused, id: \.self) { line in
                Label(line, systemImage: "xmark.circle").font(.caption).foregroundStyle(.orange).textSelection(.enabled)
            }
        }
        .padding(16)
        .background(.quaternary.opacity(0.3), in: RoundedRectangle(cornerRadius: 12))
    }
}

/// An agent inside the conversation: where its session is, what it is doing,
/// and what it was told.
struct AgentChip: View {
    let task: Work
    let activity: Activity?
    let line: String

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            PhaseMark(phase: AgentRoster.phase(of: activity)).padding(.top, 5)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(task.title).font(.callout.weight(.medium)).lineLimit(1)
                    StateSymbol(state: task.state)
                }
                Text(line).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                if let now = activity?.error ?? activity?.now, !now.isEmpty {
                    Text(now).font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right").font(.caption).foregroundStyle(.tertiary)
        }
        .padding(10)
        .background(.background.opacity(0.6), in: RoundedRectangle(cornerRadius: 8))
        .contentShape(Rectangle())
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
            .padding(16)
            .background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 12))
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
            if let error = delivery.error {
                Label(error, systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(.orange).textSelection(.enabled)
            }
            ForEach(landings, id: \.url) { LandingRow(landed: $0).font(.caption) }
        }
    }
}
