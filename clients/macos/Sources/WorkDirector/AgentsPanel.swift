import SwiftUI
import WorkDirectorKit

/// Where the work is being done: every agent on it, live, with what it is
/// doing and what it has spent. Choosing one shows its conversation.
struct AgentsPanel: View {
    let roster: AgentRoster
    /// The card whose conversation is shown, if any.
    let focused: String?
    let choose: (AgentCard) -> Void
    /// Stops a task's session.
    let stop: (String) -> Void
    var menu: (AgentCard) -> AnyView = { _ in AnyView(EmptyView()) }

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2) {
                    Text("AGENTS")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 12)
                        .padding(.top, 10)
                        .padding(.bottom, 4)
                    if roster.cards.isEmpty {
                        Text("No agent has started on this yet.")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .padding(12)
                    }
                    ForEach(roster.cards) { card in
                        row(card, depth: 0)
                        ForEach(card.subagents) { sub in row(sub, depth: 1) }
                    }
                }
                .padding(.bottom, 8)
            }
            Divider()
            HStack {
                Text("\(roster.settled) settled")
                Spacer()
                Text("Σ \(tokenCount(roster.tokens)) tok")
            }
            .font(.caption.monospacedDigit())
            .foregroundStyle(.secondary)
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .frame(minWidth: 280)
    }

    private func row(_ card: AgentCard, depth: Int) -> some View {
        Button { choose(card) } label: {
            AgentRow(card: card, focused: focused == card.id, stop: card.agent == nil && card.phase != .settled ? { stop(card.task) } : nil)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(card.title), \(card.kind)")
        .padding(.leading, CGFloat(depth) * 16)
        .contextMenu {
                Button("Show Conversation") { choose(card) }
                if card.agent == nil && card.phase != .settled {
                    Button("Stop") { stop(card.task) }
                }
                menu(card)
            }
    }
}

struct AgentRow: View {
    let card: AgentCard
    let focused: Bool
    let stop: (() -> Void)?
    @State private var hovering = false

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            PhaseMark(phase: card.phase).padding(.top, 4)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(card.title).font(.callout.weight(.semibold)).lineLimit(1)
                    Text(card.kind)
                        .font(.caption2.monospaced())
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(.quaternary, in: RoundedRectangle(cornerRadius: 4))
                        .foregroundStyle(.secondary)
                    Spacer(minLength: 4)
                    TimelineView(.periodic(from: .now, by: 1)) { context in
                        if let t = card.elapsed(now: context.date) {
                            Text(span(t)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                        }
                    }
                    if card.phase == .settled {
                        Image(systemName: "checkmark").font(.caption).foregroundStyle(.secondary)
                    }
                }
                if let line = card.line, !line.isEmpty {
                    Text(line).font(.caption).foregroundStyle(card.phase == .asking ? .orange : .secondary).lineLimit(1)
                }
                if let u = card.usage {
                    Text(meta(u)).font(.caption2.monospaced()).foregroundStyle(.tertiary)
                }
            }
            if let stop, hovering {
                Button(action: stop) { Image(systemName: "stop.circle.fill") }
                    .buttonStyle(.plain)
                    .foregroundStyle(.red)
                    .help("Stop this agent's session")
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .background(focused ? Color.accentColor.opacity(0.14) : (hovering ? Color.primary.opacity(0.05) : .clear),
                    in: RoundedRectangle(cornerRadius: 7))
        .padding(.horizontal, 4)
        .onHover { hovering = $0 }
    }

    private func meta(_ u: Usage) -> String {
        var parts: [String] = []
        if let m = u.model, !m.isEmpty { parts.append(m.replacingOccurrences(of: "claude-", with: "")) }
        parts.append("\(tokenCount(u.tokens)) tok")
        parts.append("\(u.tools) tools")
        return parts.joined(separator: " · ")
    }
}

/// Where an agent is: working, waiting on a person, resting, or settled.
struct PhaseMark: View {
    let phase: AgentCard.Phase

    var body: some View {
        switch phase {
        case .working:
            Circle().fill(.green).frame(width: 7, height: 7)
                .overlay(Circle().stroke(.green.opacity(0.4), lineWidth: 3))
                .help("Working")
        case .asking:
            Circle().fill(.orange).frame(width: 7, height: 7).help("Waiting on you")
        case .resting:
            Circle().stroke(.green, lineWidth: 1.5).frame(width: 7, height: 7).help("Idle")
        case .settled:
            Circle().fill(.secondary.opacity(0.5)).frame(width: 7, height: 7).help("Settled")
        }
    }
}
