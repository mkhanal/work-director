import SwiftUI
import WorkDirectorKit

/// One goal or task, read again whenever the board is.
struct WorkDetail: View {
    let store: Store
    let id: String
    @Binding var plan: Plan?

    @State private var loaded: Loaded?
    @State private var failure: String?
    @State private var asking: Ask?

    enum Loaded {
        case goal(GoalDetail)
        case work(Work, [Event])

        var work: Work {
            switch self {
            case .goal(let g): g.goal
            case .work(let w, _): w
            }
        }
    }

    var body: some View {
        Group {
            if let loaded {
                ScrollView {
                    VStack(alignment: .leading, spacing: 18) {
                        Header(work: loaded.work)
                        switch loaded {
                        case .goal(let goal): GoalSections(goal: goal)
                        case .work(_, let events): EventsSection(events: events, expanded: true)
                        }
                    }
                    .padding(24)
                    .frame(maxWidth: 820, alignment: .leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .toolbar { Actions(work: loaded.work, plan: $plan, asking: $asking) }
                .safeAreaInset(edge: .bottom) {
                    if !loaded.work.atRest {
                        MessageBar(store: store, work: loaded.work)
                    }
                }
            } else if let failure {
                ContentUnavailableView("Cannot read \(id)", systemImage: "exclamationmark.triangle", description: Text(failure))
            } else {
                ProgressView()
            }
        }
        .navigationTitle(loaded?.work.title ?? id)
        .task(id: store.reads) { await load() }
        .sheet(item: $asking) { ask in
            ReasonSheet(ask: ask) { made in
                asking = nil
                plan = made
            }
        }
    }

    private func load() async {
        do {
            let work = try await store.work(id)
            if work.kind.isGoal {
                loaded = .goal(try await store.goal(id))
            } else {
                loaded = .work(work, try await store.events(of: id))
            }
            failure = nil
        } catch {
            failure = "\(error)"
        }
    }
}

struct Header: View {
    let work: Work

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(work.title).font(.title2.weight(.semibold)).textSelection(.enabled)
            HStack(spacing: 8) {
                StatePill(state: work.state)
                if let type = work.goalType {
                    Text(type.rawValue).font(.caption).foregroundStyle(.secondary)
                }
                Text(work.project).font(.caption).foregroundStyle(.secondary)
                Text(work.id).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
            if !work.detail.isEmpty {
                Text(work.detail).foregroundStyle(.secondary).textSelection(.enabled)
            }
        }
    }
}

struct GoalSections: View {
    let goal: GoalDetail

    var body: some View {
        if let delivery = goal.delivery {
            GroupBox {
                LabeledContent("Where it reached") {
                    VStack(alignment: .trailing) {
                        Text(delivery.status)
                        if !delivery.detail.isEmpty {
                            Text(delivery.detail).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        Section {
            if goal.tasks.isEmpty {
                Text("No tasks yet.").foregroundStyle(.secondary)
            }
            ForEach(headings, id: \.self) { heading in
                VStack(alignment: .leading, spacing: 6) {
                    Text(heading).font(.subheadline.weight(.semibold)).foregroundStyle(.secondary)
                    ForEach(goal.tasks.filter { ($0.heading ?? "") == heading }) { task in
                        HStack(spacing: 8) {
                            StateSymbol(state: task.state)
                            Text(task.title).lineLimit(2)
                            Spacer()
                            Text(task.id).font(.caption.monospaced()).foregroundStyle(.tertiary)
                        }
                    }
                }
            }
        } header: {
            SectionTitle("Tasks", detail: "\(goal.rollup.done) of \(goal.rollup.total) done")
        }
        Section {
            if goal.claims.isEmpty {
                Text("The loop has decided nothing here.").foregroundStyle(.secondary)
            }
            ForEach(goal.claims, id: \.event.id) { claim in
                ClaimRow(claim: claim)
            }
        } header: {
            SectionTitle("Decisions", detail: "\(goal.claims.count)")
        }
        Section {
            if goal.landings.isEmpty {
                Text("Nothing has landed.").foregroundStyle(.secondary)
            }
            ForEach(goal.landings, id: \.url) { landed in
                LandingRow(landed: landed)
            }
        } header: {
            SectionTitle("Landings", detail: "\(goal.landings.count)")
        }
        EventsSection(events: goal.events, expanded: false)
    }

    private var headings: [String] {
        var seen: [String] = []
        for t in goal.tasks where !seen.contains(t.heading ?? "") { seen.append(t.heading ?? "") }
        return seen
    }
}

struct SectionTitle: View {
    let title: String
    let detail: String

    init(_ title: String, detail: String) {
        self.title = title
        self.detail = detail
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title).font(.headline)
            Text(detail).font(.subheadline).foregroundStyle(.secondary)
        }
        .padding(.top, 6)
    }
}

struct ClaimRow: View {
    let claim: Claim

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            if let d = claim.event.decision {
                Text(d.question).font(.callout.weight(.medium))
                Text(d.answer).strikethrough(!claim.stands)
                HStack(spacing: 6) {
                    Text(d.source.isEmpty ? "director" : d.source)
                    if !d.model.isEmpty { Text("· \(d.model)") }
                    if d.tokens > 0 { Text("· \(d.tokens) tokens") }
                    Text("· \(claim.event.at)")
                }
                .font(.caption).foregroundStyle(.secondary)
            } else {
                Text(claim.event.body).strikethrough(!claim.stands).textSelection(.enabled)
                Text(claim.event.at).font(.caption).foregroundStyle(.secondary)
            }
            if !claim.stands {
                Label("Reversed: \(claim.reversed)", systemImage: "arrow.uturn.backward").font(.caption).foregroundStyle(.orange)
            }
        }
        .padding(.vertical, 2)
    }
}

struct LandingRow: View {
    let landed: Landed

    var body: some View {
        HStack {
            Image(systemName: landed.kind == .commit ? "arrow.triangle.merge" : landed.kind == .pullRequest ? "arrow.triangle.pull" : "questionmark.circle")
            if let url = URL(string: landed.url) {
                Link(landed.url, destination: url).lineLimit(1).truncationMode(.middle)
            } else {
                Text(landed.url)
            }
            Spacer()
            Text(kind).font(.caption).foregroundStyle(.secondary)
        }
    }

    private var kind: String {
        switch landed.kind {
        case .commit: "commit"
        case .pullRequest: "pull request"
        case .unknown: "unknown"
        }
    }
}

struct EventsSection: View {
    let events: [Event]
    @State var expanded: Bool

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(events.reversed()) { e in
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(e.at.prefix(19).replacingOccurrences(of: "T", with: " "))
                            .font(.caption.monospaced()).foregroundStyle(.secondary)
                            .frame(width: 140, alignment: .leading)
                        Text(e.kind).font(.caption.weight(.medium)).frame(width: 70, alignment: .leading)
                        Text(e.body).font(.callout).textSelection(.enabled)
                    }
                }
            }
            .padding(.top, 6)
        } label: {
            SectionTitle("Events", detail: "\(events.count)")
        }
    }
}

/// Typing to work while it runs, as in a conversation. A goal passes the
/// message to every open task and keeps it as a decision; a session that is
/// busy reads it when it is ready.
struct MessageBar: View {
    let store: Store
    let work: Work
    @State private var text = ""
    @State private var sending = false
    @State private var outcome: String?
    @State private var failed = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let outcome {
                Label(outcome, systemImage: failed ? "exclamationmark.triangle.fill" : "paperplane")
                    .font(.caption)
                    .foregroundStyle(failed ? .red : .secondary)
                    .lineLimit(2)
            }
            HStack(alignment: .bottom, spacing: 8) {
                TextField(work.kind.isGoal ? "Tell this goal… every task gets it, and it is kept as a decision" : "Message this task… it reads it when it is ready",
                          text: $text, axis: .vertical)
                    .textFieldStyle(.plain)
                    .lineLimit(1...6)
                    .focused($focused)
                    .onSubmit { Task { await send() } }
                    .disabled(sending)
                Button { Task { await send() } } label: {
                    Image(systemName: "arrow.up.circle.fill").font(.title2)
                }
                .buttonStyle(.plain)
                .foregroundStyle(plan == nil ? Color.secondary : Color.accentColor)
                .disabled(plan == nil || sending)
                .help(plan?.commandLine ?? "Type a message")
            }
            .padding(10)
            .background(.background, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(.quaternary))
            if let plan {
                Text(plan.commandLine)
                    .font(.caption2.monospaced())
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(.bar)
    }

    private var plan: Plan? { try? Plan.send(work.id, text: text) }

    private func send() async {
        guard let plan else { return }
        sending = true
        defer { sending = false }
        do {
            let result = try await store.run(plan)
            failed = result.code != 0
            outcome = failed ? (result.stderr.isEmpty ? result.stdout : result.stderr) : result.stdout
            if !failed { text = "" }
        } catch {
            failed = true
            outcome = String(describing: error)
        }
        await store.refresh()
    }
}
