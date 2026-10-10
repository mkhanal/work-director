import SwiftUI
import WorkDirectorKit

/// The home page: a new goal, laid out like any goal, typed into the same bar.
/// Before it starts, the goals it may continue are offered in its place.
struct NewGoalPage: View {
    let store: Store
    /// The product in scope, or nil for all products.
    let scope: String?
    let started: (String) -> Void

    @State private var flow: NewGoal
    @State private var text = ""
    @AppStorage("newGoal.product") private var chosen = ""

    init(store: Store, scope: String?, started: @escaping (String) -> Void) {
        self.store = store
        self.scope = scope
        self.started = started
        _flow = State(initialValue: NewGoal(store: store))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("New goal").font(.title2.weight(.semibold))
                    HStack(spacing: 8) {
                        Label("Product", systemImage: "shippingbox").font(.caption).foregroundStyle(.secondary)
                        if let scope {
                            Text(scope).font(.caption.weight(.medium))
                        } else {
                            Picker("Product", selection: Binding(get: { product }, set: { chosen = $0 })) {
                                ForEach(store.projects) { p in Text(p.name).tag(p.name) }
                            }
                            .labelsHidden()
                            .fixedSize()
                            .controlSize(.small)
                        }
                    }
                }
                phaseView(flow)
            }
            .padding(24)
            .frame(maxWidth: 820, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .safeAreaInset(edge: .bottom) {
            InputBar(
                text: $text,
                placeholder: "What do you want done? Work Director checks for a goal it continues, then plans and starts it",
                command: plan?.commandLine,
                busy: flow.phase == .finding || flow.phase == .starting
            ) { Task { await submit(flow) } }
        }
        .navigationTitle("New goal")
        .onChange(of: flow.phase) {
            if case .started(let s) = flow.phase {
                text = ""
                flow.reset()
                started(s.goal.id)
            }
        }
    }

    /// The product the goal is filed in: the one in scope, else the one last
    /// chosen, else the first there is.
    private var product: String {
        if let scope { return scope }
        if store.projects.contains(where: { $0.name == chosen }) { return chosen }
        return store.projects.first?.name ?? ""
    }

    private var plan: Plan? {
        product.isEmpty ? nil : try? Plan.start(project: product, text: text)
    }

    private func submit(_ flow: NewGoal) async {
        guard plan != nil else { return }
        await flow.submit(project: product, text: text)
    }

    @ViewBuilder private func phaseView(_ flow: NewGoal) -> some View {
        switch flow.phase {
        case .idle, .started:
            Text("Say what you want done, the way you would start a conversation. The first line becomes the title. If it continues a goal you already have, you can pick that goal up instead of starting a new one.")
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        case .finding:
            Label {
                Text("Looking for goals this continues…")
            } icon: {
                ProgressView().controlSize(.small)
            }
            .foregroundStyle(.secondary)
        case .offering(let found):
            VStack(alignment: .leading, spacing: 10) {
                SectionTitle("This may continue a goal you already have", detail: "\(found.count)")
                ForEach(found, id: \.goal.id) { f in
                    GroupBox {
                        VStack(alignment: .leading, spacing: 6) {
                            HStack {
                                Text(f.goal.title).font(.body.weight(.semibold))
                                Spacer()
                                StatePill(state: f.goal.state)
                            }
                            Text(f.why).font(.callout).foregroundStyle(.secondary)
                            HStack {
                                Text(f.goal.id).font(.caption.monospaced()).foregroundStyle(.tertiary)
                                if f.goal.archived != nil {
                                    Label("archived", systemImage: "archivebox").font(.caption).foregroundStyle(.tertiary)
                                }
                                Spacer()
                                Button("Continue this goal") { Task { await flow.continueGoal(f.goal.id, text: text) } }
                                    .buttonStyle(.borderedProminent)
                            }
                        }
                        .padding(4)
                    }
                }
                Button("Start a new goal instead") { Task { await flow.startNew(project: product, text: text) } }
            }
        case .starting:
            Label {
                Text("Planning and starting its tasks… this can take a minute.")
            } icon: {
                ProgressView().controlSize(.small)
            }
            .foregroundStyle(.secondary)
        case .failed(let reason):
            VStack(alignment: .leading, spacing: 8) {
                Label(reason, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
                Button("Try again") { flow.reset() }
            }
        }
    }
}

/// The goals of the product in scope, most recently active first: where an
/// older goal is found and opened.
struct RecentGoals: View {
    let store: Store
    let scope: String?
    @Binding var selected: String?

    var body: some View {
        let goals = store.board?.recentGoals(project: scope) ?? []
        Group {
            if goals.isEmpty {
                ContentUnavailableView("No goals yet", systemImage: "target", description: Text("Type what you want done to start one."))
            } else {
                List(goals, selection: $selected) { work in
                    WorkRow(work: work, rollup: store.board?.rollup(of: work.id))
                        .tag(work.id)
                }
            }
        }
        .navigationTitle("Goals")
        .navigationSubtitle(scope ?? "All products")
    }
}

/// Work put out of sight, with the way back.
struct ArchivedList: View {
    let store: Store
    let scope: String?
    @Binding var selected: String?
    @Binding var plan: Plan?

    @State private var items: [Work]?
    @State private var failure: String?

    var body: some View {
        let shown = items?.filter { scope == nil || $0.project == scope }
        Group {
            if let shown {
                if shown.isEmpty {
                    ContentUnavailableView("Nothing archived", systemImage: "archivebox")
                } else {
                    List(shown, selection: $selected) { work in
                        WorkRow(work: work, rollup: nil)
                            .tag(work.id)
                            .contextMenu {
                                Button("Unarchive") { plan = .unarchive(work.id) }
                            }
                    }
                }
            } else if let failure {
                ContentUnavailableView("Cannot read the archive", systemImage: "exclamationmark.triangle", description: Text(failure))
            } else {
                ProgressView()
            }
        }
        .navigationTitle("Archived")
        .navigationSubtitle(shown.map { "\($0.count) item\($0.count == 1 ? "" : "s")" } ?? "")
        .task(id: store.reads) {
            do {
                items = try await store.archived()
                failure = nil
            } catch {
                failure = String(describing: error)
            }
        }
    }
}
