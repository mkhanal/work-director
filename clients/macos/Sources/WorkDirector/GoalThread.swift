import SwiftUI
import WorkDirectorKit

/// A goal read as one conversation: what was asked, the plan it became, what
/// the director and its agents did since, each agent turn tagged with its
/// sub-goal, and what waits on you, last, next to where you answer. All of it
/// but what waits on you is one text, to select across and copy.
struct GoalThread: View {
    let store: Store
    let goal: GoalDetail
    /// Shows a task's conversation.
    let focus: (String) -> Void
    let answered: () -> Void
    let filter: ThreadFilter
    /// Updates, reports and long messages the person opened or closed.
    @State private var toggled: Set<String> = []

    var body: some View {
        let transcript = goal.transcript(filter, toggled: toggled, time: when, day: DayLine.day)
        VStack(alignment: .leading, spacing: Space.s5) {
            if transcript.items.isEmpty && filter != .all {
                Text("Nothing here yet.").font(TypeScale.callout).foregroundStyle(Palette.text3)
            }
            if !transcript.items.isEmpty {
                TranscriptText(transcript: transcript, key: "goal/\(goal.goal.id)/\(filter)", open: focus) { id in
                    toggled.formSymmetricDifference([id])
                }
            }
            if filter == .all {
                ForEach(store.outbox.filter { m in m.work == goal.goal.id || goal.nodes.contains { $0.id == m.work } }.filter { !$0.arrived(in: goal) }) { message in
                    OutgoingBubble(store: store, message: message)
                }
                Waiting(store: store, goal: goal, open: focus, answered: answered)
                WorkingIndicator(rows: WorkingRows(goal: goal.goal, tasks: goal.tasks, activity: goal.activity, inFlight: store.inFlight))
            }
        }
    }
}

/// One part of a question the director decided: what was asked, its answer and why.
struct DirectorChoiceView: View {
    let choice: DirectorChoice

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(choice.question).font(TypeScale.caption).foregroundStyle(Palette.text3)
            if let answer = choice.answer {
                Text(answer).font(TypeScale.bodyMedium).foregroundStyle(Palette.text)
            }
            if !choice.why.isEmpty {
                Text(choice.why.prefix(1).uppercased() + choice.why.dropFirst())
                    .font(TypeScale.callout).foregroundStyle(Palette.text2)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .textSelection(.enabled)
    }
}

/// Why the director would not answer one part of a question.
struct DeclinedNote: View {
    let choice: DirectorChoice

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(choice.question).font(TypeScale.caption).foregroundStyle(Palette.text3)
            Text(choice.why.prefix(1).uppercased() + choice.why.dropFirst())
                .font(TypeScale.callout).foregroundStyle(Palette.text)
                .fixedSize(horizontal: false, vertical: true)
        }
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
        .canvasCard(fill: Palette.plainCard, padding: EdgeInsets(top: 10, leading: 12, bottom: 10, trailing: 12))
    }
}

/// A choice as offered: its label, and whether it was recommended.
struct Choice {
    let label: String
    let recommended: Bool

    init(_ text: String) {
        let suffix = "(recommended)"
        if text.lowercased().hasSuffix(suffix) {
            label = String(text.dropLast(suffix.count)).trimmingCharacters(in: .whitespaces)
            recommended = true
        } else {
            label = text
            recommended = false
        }
    }
}

/// Where the goal's work reached, in a line, with its landings.
struct DeliveryLine: View {
    let delivery: Delivery
    let landings: [Landed]

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s1) {
            HStack(spacing: Space.s1 + 2) {
                Image(systemName: "shippingbox")
                Text(delivery.status)
                if !delivery.detail.isEmpty {
                    Text("· " + delivery.detail).lineLimit(1)
                }
            }
            if let error = delivery.error {
                Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(Palette.attention).textSelection(.enabled)
            }
            ForEach(landings, id: \.url) { LandingRow(landed: $0) }
        }
        .font(TypeScale.meta)
        .foregroundStyle(.secondary)
    }
}

