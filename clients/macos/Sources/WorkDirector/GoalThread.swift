import SwiftUI
import WorkDirectorKit

/// A goal read as one conversation: what was asked, the plan it became, what
/// its supervisor and its agents did since, and what waits on you, last, next
/// to where you answer. Every turn says who spoke.
struct GoalThread: View {
    let store: Store
    let goal: GoalDetail
    /// Shows a task's conversation.
    let focus: (String) -> Void
    let answered: () -> Void

    var body: some View {
        LazyVStack(alignment: .leading, spacing: Space.s5) {
            if !goal.goal.detail.isEmpty {
                YouBubble(text: goal.goal.detail, at: when(goal.goal.created), label: "You asked")
            }
            PlanTurn(goal: goal, focus: focus)
            ForEach(goal.thread) { step in
                row(step).id(step.id)
            }
            ForEach(store.outbox.filter { $0.work == goal.goal.id && !$0.arrived(in: goal) }) { message in
                OutgoingBubble(store: store, message: message)
            }
            Waiting(store: store, goal: goal, open: focus, answered: answered)
        }
    }

    @ViewBuilder private func row(_ step: ThreadStep) -> some View {
        switch step {
        case .event(let event) where goal.isYours(event):
            YouBubble(text: event.decision?.answer ?? event.answerText, at: when(event.at), label: event.kind == "answer" ? "You answered" : "You")
        case .event(let event) where event.kind == "report":
            Turn(speaker: speaker(of: event.work), at: event.at, open: opener(event.work)) {
                ReportCard(text: event.body)
            }
        case .event(let event) where event.kind == "answer":
            DirectorTurn(answer: event)
        case .event(let event) where event.kind == "sent":
            if event.work != goal.goal.id {
                EventLine(text: "Told \(speaker(of: event.work).name): \(firstLine(event.body))", symbol: "arrow.turn.down.right", at: event.at)
            }
        case .event(let event):
            EventLine(text: line(event), symbol: symbol(event), at: event.at, tint: tint(event))
        case .exchange(let question, let answer):
            // The question waiting on you is answered below, in place.
            if answer != nil || goal.asking?.id != "event-\(question.id)" {
                VStack(alignment: .leading, spacing: Space.s5) {
                    Turn(speaker: speaker(of: question.work), at: question.at, open: opener(question.work), detail: { Text("asked").foregroundStyle(.secondary) }) {
                        AskedBody(question: question)
                    }
                    if let answer {
                        if answer.answeredByDirector {
                            DirectorTurn(answer: answer)
                        } else {
                            YouBubble(text: answer.answerText, at: when(answer.at), label: "You answered")
                        }
                    }
                }
            }
        case .look(let event):
            EventLine(text: "Supervisor looking: \(event.body)", symbol: "eye", at: event.at)
        case .supervisor(let event, let reply):
            Turn(speaker: .supervisor, at: event.at, open: { focus(goal.goal.id) }) {
                SupervisorBody(reply: reply, goal: goal, focus: focus)
            }
        case .stopped(let event, let said):
            Turn(speaker: speaker(of: event.work), at: event.at, open: opener(event.work), detail: { Text("stopped without a report").foregroundStyle(.secondary) }) {
                Text(said).readable().foregroundStyle(.secondary).lineLimit(4).textSelection(.enabled)
            }
        }
    }

    private func speaker(of id: String) -> Speaker {
        if id == goal.goal.id { return goal.supervised ? .supervisor : .plan }
        if let task = goal.tasks.first(where: { $0.id == id }) { return .task(task) }
        return .agent(id)
    }

    private func opener(_ id: String) -> (() -> Void)? {
        id == goal.goal.id ? nil : { focus(id) }
    }

    private func firstLine(_ text: String) -> String {
        String(text.split(separator: "\n", maxSplits: 1).first ?? "")
    }

    private func line(_ e: Event) -> String {
        let who = e.work == goal.goal.id ? "Goal" : speaker(of: e.work).name
        switch e.kind {
        case "decision": return "Decided: \(firstLine(e.body))"
        case "verify": return "\(who) \(e.body.hasPrefix("pass") ? "passed its checks" : "failed its checks")"
        case "pr": return "\(who) landed"
        case "state": return "\(who) is now \(e.body)"
        default: return "\(who): \(firstLine(e.body))"
        }
    }

    private func symbol(_ e: Event) -> String {
        switch e.kind {
        case "decision": "checkmark.seal"
        case "verify": e.body.hasPrefix("pass") ? "checkmark.shield" : "xmark.shield"
        case "pr": "arrow.triangle.pull"
        case "state": "circle.dashed"
        default: "circle"
        }
    }

    private func tint(_ e: Event) -> Color {
        switch e.kind {
        case "verify": e.body.hasPrefix("pass") ? Palette.settled : Palette.failure
        case "pr": Palette.settled
        default: .secondary
        }
    }
}

/// What the supervisor said, then each agent it told, started or filed, live,
/// then what was refused.
struct SupervisorBody: View {
    let reply: SupervisorReply
    let goal: GoalDetail
    let focus: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            if !reply.said.isEmpty {
                MarkdownText(reply.said)
            }
            if !reply.actions.isEmpty {
                VStack(alignment: .leading, spacing: Space.s2) {
                    ForEach(Array(reply.actions.enumerated()), id: \.offset) { _, action in
                        if let id = action.task, let task = goal.tasks.first(where: { $0.id == id }) {
                            Button { focus(id) } label: { AgentChip(task: task, activity: goal.activity[id], line: action.line) }
                                .buttonStyle(.plain)
                                .help("Show this agent's conversation")
                        } else {
                            Label(action.line, systemImage: "checkmark.circle")
                                .font(TypeScale.callout)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            ForEach(reply.refused, id: \.self) { line in
                Label(line, systemImage: "xmark.circle")
                    .font(TypeScale.callout)
                    .foregroundStyle(Palette.failure)
                    .textSelection(.enabled)
            }
        }
    }
}

/// An agent inside the conversation: where its session is, what it is doing,
/// and what it was told.
struct AgentChip: View {
    let task: Work
    let activity: Activity?
    let line: String

    var body: some View {
        HStack(alignment: .top, spacing: Space.s3) {
            PhaseMark(phase: AgentRoster.phase(of: activity)).padding(.top, 5)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: Space.s2) {
                    Text(task.title).font(TypeScale.calloutMedium).lineLimit(1)
                    StateSymbol(state: task.state)
                }
                Text(line).font(TypeScale.callout).foregroundStyle(.secondary).lineLimit(2)
                if let now = activity?.error ?? activity?.now, !now.isEmpty {
                    Text(now).font(TypeScale.meta).foregroundStyle(.tertiary).lineLimit(1)
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right").font(TypeScale.meta).foregroundStyle(.tertiary).padding(.top, 3)
        }
        .card(padding: Space.s3)
        .contentShape(Rectangle())
    }
}

/// The plan a goal became: its tasks grouped as planned, each a way into its
/// conversation.
struct PlanTurn: View {
    let goal: GoalDetail
    let focus: (String) -> Void

    var body: some View {
        Turn(speaker: .plan, at: nil, detail: {
            Text(goal.tasks.isEmpty ? "not planned yet" : "\(goal.rollup.done) of \(goal.rollup.total) done").foregroundStyle(.secondary)
        }) {
            if !goal.tasks.isEmpty {
                VStack(alignment: .leading, spacing: Space.s2) {
                    ForEach(headings, id: \.self) { heading in
                        if !heading.isEmpty && heading != "(no heading)" {
                            MicroLabel(heading).padding(.top, Space.s1)
                        }
                        ForEach(goal.tasks.filter { ($0.heading ?? "") == heading }) { task in
                            Button { focus(task.id) } label: {
                                HStack(spacing: Space.s2) {
                                    StateSymbol(state: task.state)
                                    Text(task.title)
                                        .font(TypeScale.callout)
                                        .strikethrough(task.state == .dropped)
                                        .foregroundStyle(task.atRest ? .secondary : .primary)
                                        .lineLimit(2)
                                        .multilineTextAlignment(.leading)
                                    Spacer(minLength: 0)
                                }
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .help("Show this task's conversation")
                        }
                    }
                }
            }
        }
    }

    private var headings: [String] {
        var seen: [String] = []
        for t in goal.tasks where !seen.contains(t.heading ?? "") { seen.append(t.heading ?? "") }
        return seen
    }
}

/// A question as it was asked: each part, and the choices it offered.
struct AskedBody: View {
    let question: Event

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            ForEach(Array(question.askedItems.enumerated()), id: \.offset) { _, item in
                VStack(alignment: .leading, spacing: Space.s2) {
                    Text(item.question).readable().textSelection(.enabled)
                    if !item.choices.isEmpty {
                        ChoiceChips(choices: item.choices)
                    }
                }
            }
        }
    }
}

/// The choices a question offered, read-only.
struct ChoiceChips: View {
    let choices: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s1 + 2) {
            ForEach(choices, id: \.self) { choice in
                HStack(spacing: Space.s2) {
                    Image(systemName: "circle").font(.system(size: 9)).foregroundStyle(.tertiary)
                    Text(Choice(choice).label).font(TypeScale.callout).foregroundStyle(.secondary)
                    if Choice(choice).recommended { RecommendedTag() }
                }
            }
        }
    }
}

/// The director answering in your place, from what was already decided or by
/// its judgement.
struct DirectorTurn: View {
    let answer: Event

    var body: some View {
        Turn(speaker: .director, at: answer.at, detail: {
            Text(answer.decision.map { $0.model.isEmpty ? "answered from what was decided" : "answered · \($0.model)" } ?? "answered")
                .foregroundStyle(.secondary)
        }) {
            Text(answer.answerText)
                .readable()
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .card(Palette.settled, padding: Space.s3)
        }
    }
}

/// A task's report: its verdict and what it said, folded when long.
struct ReportCard: View {
    let text: String
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            HStack(spacing: Space.s2) {
                Image(systemName: "doc.text").foregroundStyle(.secondary)
                Text("Report").font(TypeScale.calloutMedium)
                if let verdict {
                    Text(verdict)
                        .font(TypeScale.meta)
                        .padding(.horizontal, Space.s2).padding(.vertical, 2)
                        .background(verdictTint.opacity(0.14), in: RoundedRectangle(cornerRadius: Radius.chip))
                        .foregroundStyle(verdictTint)
                }
                Spacer(minLength: 0)
                if folds {
                    Button(expanded ? "Show less" : "Show all") { expanded.toggle() }
                        .buttonStyle(.link)
                        .font(TypeScale.meta)
                }
            }
            MarkdownText(shown)
        }
        .card()
    }

    /// The report without the verdict the pill already shows: a bare verdict
    /// line, and the `STATUS:` word, keeping anything said after it.
    private var lines: [String] {
        var out: [String] = []
        for (i, raw) in text.components(separatedBy: "\n").enumerated() {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if i == 0, ["DONE", "BLOCKED", "NEEDS-INPUT"].contains(line) { continue }
            if let rest = line.range(of: #"^STATUS:\s*(DONE|BLOCKED|NEEDS-INPUT)\s*"#, options: .regularExpression) {
                let said = line[rest.upperBound...].trimmingCharacters(in: .whitespaces)
                if !said.isEmpty { out.append(said) }
                continue
            }
            out.append(raw)
        }
        return out
    }
    private var folds: Bool { lines.count > 12 }
    private var shown: String { (expanded || !folds ? lines : Array(lines.prefix(12))).joined(separator: "\n") }

    private var verdict: String? {
        for word in ["DONE", "BLOCKED", "NEEDS-INPUT"] where text.contains("STATUS: \(word)") || text.hasPrefix(word) {
            return word.lowercased()
        }
        return nil
    }

    private var verdictTint: Color {
        switch verdict {
        case "done": Palette.settled
        case "blocked": Palette.failure
        default: Palette.attention
        }
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

struct RecommendedTag: View {
    var body: some View {
        Text("Recommended")
            .font(.system(size: 10.5, weight: .semibold))
            .padding(.horizontal, 6).padding(.vertical, 1.5)
            .background(Color.accentColor.opacity(0.14), in: RoundedRectangle(cornerRadius: Radius.chip))
            .foregroundStyle(Color.accentColor)
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
