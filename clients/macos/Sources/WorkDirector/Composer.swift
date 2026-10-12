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
            VStack(alignment: .leading, spacing: Space.s5) {
                VStack(alignment: .leading, spacing: Space.s2) {
                    Text("What should get done?").font(TypeScale.hero)
                    Text("Say it the way you would to a lead engineer. It is checked against goals it may continue, then planned into tasks and started.")
                        .font(TypeScale.body)
                        .foregroundStyle(.secondary)
                        .lineSpacing(TypeScale.bodyLeading)
                    HStack(spacing: Space.s2) {
                        Image(systemName: "shippingbox.fill").foregroundStyle(.secondary)
                        if let scope {
                            Text(scope).font(TypeScale.calloutMedium)
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
                if store.projects.isEmpty {
                    ContentUnavailableView {
                        Label("No product yet", systemImage: "shippingbox")
                    } description: {
                        Text("Add the repository you want worked on, then say what you want done.")
                    } actions: {
                        Button("Add Product…") { NotificationCenter.default.post(name: .addProduct, object: nil) }
                            .buttonStyle(.borderedProminent)
                    }
                }
                phaseView(flow)
            }
            .frame(maxWidth: 680, alignment: .leading)
            .padding(.horizontal, Space.s6)
            .padding(.top, Space.s7 + Space.s5)
            .frame(maxWidth: .infinity)
        }
        .safeAreaInset(edge: .bottom) {
            InputBar(
                text: $text,
                placeholder: "Describe the goal…",
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
            VStack(alignment: .leading, spacing: Space.s3) {
                SectionTitle("This may continue a goal you already have", detail: "\(found.count)")
                ForEach(found, id: \.goal.id) { f in
                    GroupBox {
                        VStack(alignment: .leading, spacing: Space.s2) {
                            HStack {
                                Text(f.goal.title).font(TypeScale.body.weight(.semibold))
                                Spacer()
                                StatePill(state: f.goal.state)
                            }
                            Text(f.why).font(TypeScale.callout).foregroundStyle(.secondary)
                            HStack {
                                Text(f.goal.id).font(TypeScale.caption.monospaced()).foregroundStyle(.tertiary)
                                if f.goal.archived != nil {
                                    Label("archived", systemImage: "archivebox").font(TypeScale.caption).foregroundStyle(.tertiary)
                                }
                                Spacer()
                                Button("Continue this goal") { Task { await flow.continueGoal(f.goal.id, text: text) } }
                                    .buttonStyle(.borderedProminent)
                            }
                        }
                        .padding(Space.s1)
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
            VStack(alignment: .leading, spacing: Space.s2) {
                Label(reason, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
                HStack(spacing: Space.s2) {
                    Button("Start a new goal") { Task { await flow.startNew(project: product, text: text) } }
                    Button("Try again") { flow.reset() }
                }
            }
        }
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
                            .contextMenu { WorkMenu(work: work, run: { plan = $0 }, ask: { _ in selected = work.id }) }
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
