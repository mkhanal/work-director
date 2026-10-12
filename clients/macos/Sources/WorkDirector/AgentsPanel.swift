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
                LazyVStack(alignment: .leading, spacing: Space.s1) {
                    MicroLabel("Agents")
                        .padding(.horizontal, Space.s4)
                        .padding(.top, Space.s4)
                        .padding(.bottom, Space.s2)
                    if roster.cards.isEmpty {
                        Text("No agent has started on this yet.")
                            .font(TypeScale.callout)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, Space.s4)
                    }
                    ForEach(roster.cards) { card in
                        row(card, depth: 0)
                        ForEach(card.subagents) { sub in row(sub, depth: 1) }
                    }
                }
                .padding(.bottom, Space.s3)
            }
            Divider()
            HStack {
                Text("\(roster.settled) of \(roster.cards.count) settled")
                Spacer()
                Text("\(tokenCount(roster.tokens)) tokens")
            }
            .font(TypeScale.meta.monospacedDigit())
            .foregroundStyle(.secondary)
            .padding(.horizontal, Space.s4)
            .padding(.vertical, Space.s3)
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
        HStack(alignment: .top, spacing: Space.s3) {
            PhaseMark(phase: card.phase).padding(.top, 5)
            VStack(alignment: .leading, spacing: Space.s1) {
                HStack(alignment: .firstTextBaseline, spacing: Space.s2) {
                    Text(card.title).font(TypeScale.calloutStrong).lineLimit(1)
                    Text(card.kind)
                        .font(TypeScale.tag)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(Palette.sunken, in: RoundedRectangle(cornerRadius: 4))
                        .foregroundStyle(.secondary)
                    Spacer(minLength: Space.s1)
                    TimelineView(.periodic(from: .now, by: 1)) { context in
                        if let t = card.elapsed(now: context.date) {
                            Text(span(t)).font(TypeScale.meta.monospacedDigit()).foregroundStyle(.secondary)
                        }
                    }
                    if card.phase == .settled {
                        Image(systemName: "checkmark").font(TypeScale.meta).foregroundStyle(.secondary)
                    }
                }
                if let line = card.line, !line.isEmpty {
                    Text(line).font(TypeScale.callout).foregroundStyle(card.phase == .asking ? Palette.attention : .secondary).lineLimit(1)
                }
                if let u = card.usage {
                    Text(meta(u)).font(TypeScale.small.monospacedDigit()).foregroundStyle(.tertiary)
                }
            }
            if let stop, hovering {
                Button(action: stop) { Image(systemName: "stop.circle.fill") }
                    .buttonStyle(.plain)
                    .foregroundStyle(.red)
                    .help("Stop this agent's session")
            }
        }
        .padding(.horizontal, Space.s3)
        .padding(.vertical, Space.s2 + 2)
        .background(focused ? Color.accentColor.opacity(0.14) : (hovering ? Color.primary.opacity(0.05) : .clear),
                    in: RoundedRectangle(cornerRadius: Radius.card))
        .padding(.horizontal, Space.s2)
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
