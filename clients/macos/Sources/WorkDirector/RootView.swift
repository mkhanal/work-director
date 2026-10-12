import SwiftUI
import WorkDirectorKit

/// What the main pane shows: a new goal, one goal or task, the archive, or a
/// product's review.
enum Place: Hashable {
    case newGoal
    case work(String)
    case archived
    case taste
    case review(String)
}

/// Goals as threads on the left, the one chosen in the middle with its agents
/// beside it: the shape a coding agent's own app has, over the director's ledger.
struct RootView: View {
    let store: Store
    /// The product everything is scoped to; empty is all products.
    @AppStorage("scope") private var scopeName = ""
    @State private var place: Place? = .newGoal
    /// The work last open, so the app opens where it was left.
    @AppStorage("place.work") private var lastWork = ""
    @State private var plan: Plan?
    @State private var asking: Ask?
    @State private var writes: Writes
    @State private var typing = false
    @State private var addingProduct = false

    init(store: Store) {
        self.store = store
        _writes = State(initialValue: Writes(store: store))
    }

    private var scope: String? { scopeName.isEmpty ? nil : scopeName }

    var body: some View {
        NavigationSplitView {
            ThreadSidebar(store: store, scopeName: $scopeName, place: $place, plan: $plan, asking: $asking)
                .navigationSplitViewColumnWidth(min: 240, ideal: 290, max: 380)
        } detail: {
            switch place {
            case .newGoal, nil:
                NewGoalPage(store: store, scope: scope) { id in place = .work(id) }
                    .id(scopeName)
            case .work(let id):
                WorkDetail(store: store, id: id, plan: $plan) { place = .work($0) }
                    .id(id)
            case .archived:
                ArchivedList(store: store, scope: scope, selected: opened, plan: $plan)
            case .taste:
                TastePage(store: store, scope: scope, plan: $plan)
            case .review(let project):
                ReviewList(store: store, project: project, selected: opened, plan: $plan)
            }
        }
        // The page's own header carries its name; the toolbar keeps to actions.
        .toolbar(removing: .title)
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: 0) {
                if let done = writes.last {
                    WriteBanner(done: done, undo: { plan = $0 }) { writes.dismiss() }
                        .id(done.id)
                }
                if let failure = store.failure {
                    Label(failure, systemImage: "exclamationmark.triangle.fill")
                        .font(TypeScale.callout)
                        .foregroundStyle(.red)
                        .padding(Space.s2)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(.bar)
                }
            }
        }
        .onAppear {
            if !lastWork.isEmpty { place = .work(lastWork) }
        }
        .onChange(of: place) {
            if case .work(let id) = place { lastWork = id } else if place == .newGoal { lastWork = "" }
        }
        .onChange(of: scopeName) {
            if case .review = place { place = .newGoal }
        }
        .onReceive(NotificationCenter.default.publisher(for: .openCommandBar)) { _ in typing = true }
        .onReceive(NotificationCenter.default.publisher(for: .newGoal)) { _ in newGoal() }
        .onReceive(NotificationCenter.default.publisher(for: .openWork)) { note in
            if let id = note.object as? String { place = .work(id) }
        }
        .onReceive(NotificationCenter.default.publisher(for: .reload)) { _ in Task { await store.refresh() } }
        .sheet(isPresented: $addingProduct) {
            AddProductSheet(store: store) { name in scopeName = name }
        }
        .onReceive(NotificationCenter.default.publisher(for: .addProduct)) { _ in addingProduct = true }
        .sheet(isPresented: $typing) {
            CommandBar { typed in
                typing = false
                plan = typed
            }
        }
        .onChange(of: plan) {
            guard let chosen = plan else { return }
            plan = nil
            Task { await writes.run(chosen) }
        }
        .sheet(item: $asking) { ask in
            ReasonSheet(ask: ask) { made in
                asking = nil
                plan = made
            }
        }
    }

    /// A list that opens work does it by moving the main pane to it.
    private var opened: Binding<String?> {
        Binding(get: { nil }, set: { id in if let id { place = .work(id) } })
    }

    private func newGoal() { place = .newGoal }
}

/// Every goal as a thread: open ones most recently active first, settled ones
/// folded away, filtered by what they wait on.
struct ThreadSidebar: View {
    let store: Store
    @Binding var scopeName: String
    @Binding var place: Place?
    @Binding var plan: Plan?
    @Binding var asking: Ask?
    @AppStorage("sidebar.band") private var bandName = ""
    @AppStorage("sidebar.settled") private var showSettled = false
    @AppStorage("sidebar.queued") private var showQueued = false
    @State private var search = ""

    private var scope: String? { scopeName.isEmpty ? nil : scopeName }
    private var band: Band? { Band(rawValue: bandName) }

    var body: some View {
        let threads = store.board?.threads(project: scope, band: band) ?? .none
        let matching = { (t: BoardThread) in search.isEmpty || t.work.title.localizedCaseInsensitiveContains(search) || t.work.id.hasPrefix(search) }
        List(selection: $place) {
            Section {
                ForEach(threads.open.filter(matching)) { thread in
                    GoalThreadRow(thread: thread)
                        .tag(Place.work(thread.work.id))
                        .contextMenu { WorkMenu(work: thread.work, run: { plan = $0 }, ask: { asking = $0 }) }
                }
                if threads.open.filter(matching).isEmpty {
                    Text(band == nil ? "Nothing open." : "Nothing \(band!.label.lowercased()).")
                        .font(TypeScale.callout).foregroundStyle(.secondary)
                        .padding(.vertical, Space.s1)
                }
            } header: {
                HStack {
                    MicroLabel(band?.label ?? "Open")
                    Spacer()
                    Menu {
                        Picker("Show", selection: $bandName) {
                            Text("Everything open").tag("")
                            Divider()
                            ForEach(Band.allCases.filter { $0 != .atRest }, id: \.self) { b in
                                let n = store.board?.threads(project: scope, band: b).open.count ?? 0
                                Text("\(b.label) (\(n))").tag(b.rawValue)
                            }
                        }
                        .pickerStyle(.inline)
                    } label: {
                        Image(systemName: band == nil ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                    }
                    .menuStyle(.borderlessButton)
                    .fixedSize()
                    .help("Show only what waits on one thing")
                }
            }
            Section(isExpanded: $showQueued) {
                ForEach(threads.queued.filter(matching)) { thread in
                    GoalThreadRow(thread: thread)
                        .tag(Place.work(thread.work.id))
                        .contextMenu { WorkMenu(work: thread.work, run: { plan = $0 }, ask: { asking = $0 }) }
                }
            } header: {
                MicroLabel("Queued · \(threads.queued.count)")
            }
            Section(isExpanded: $showSettled) {
                ForEach(threads.settled.filter(matching)) { thread in
                    GoalThreadRow(thread: thread)
                        .tag(Place.work(thread.work.id))
                        .contextMenu { WorkMenu(work: thread.work, run: { plan = $0 }, ask: { asking = $0 }) }
                }
            } header: {
                MicroLabel("Settled · \(threads.settled.count)")
            }
            Section {
                Label("Taste", systemImage: "sparkles").tag(Place.taste)
                Label("Archived", systemImage: "archivebox").tag(Place.archived)
                if let scope {
                    Label("What the loop decided", systemImage: "checklist").tag(Place.review(scope))
                } else {
                    ForEach(store.projects) { project in
                        Label("Review \(project.name)", systemImage: "checklist").tag(Place.review(project.name))
                    }
                }
            } header: {
                MicroLabel("More")
            }
        }
        .listStyle(.sidebar)
        .searchable(text: $search, placement: .sidebar, prompt: "Search goals")
        .safeAreaInset(edge: .top) {
            HStack(spacing: Space.s2) {
                Menu {
                    Picker("Product", selection: $scopeName) {
                        Text("All products").tag("")
                        Divider()
                        ForEach(store.projects) { p in Text(p.name).tag(p.name) }
                    }
                    .pickerStyle(.inline)
                    Divider()
                    Button("Add Product…") { NotificationCenter.default.post(name: .addProduct, object: nil) }
                } label: {
                    HStack(spacing: Space.s1 + 2) {
                        Image(systemName: "shippingbox.fill").foregroundStyle(.secondary)
                        Text(scope ?? "All products").font(TypeScale.calloutStrong).lineLimit(1)
                        Image(systemName: "chevron.up.chevron.down").font(.system(size: IconSize.tiny, weight: .semibold)).foregroundStyle(.tertiary)
                    }
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                .help("Show one product's work, or all of it")
                Spacer(minLength: 0)
                Button { place = .newGoal } label: {
                    Image(systemName: "square.and.pencil").font(.system(size: IconSize.toolbar, weight: .medium))
                }
                .buttonStyle(.borderless)
                .help("New goal (⌘N)")
            }
            .padding(.horizontal, Space.s4)
            .padding(.top, Space.s1)
            .padding(.bottom, Space.s2)
        }
    }
}

/// A goal as one line of the sidebar: where it is, how far, how long ago.
struct GoalThreadRow: View {
    let thread: BoardThread

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Space.s2) {
            StateDot(state: thread.work.state, band: thread.band)
                .alignmentGuide(.firstTextBaseline) { d in d[VerticalAlignment.center] + 4 }
            VStack(alignment: .leading, spacing: Space.s1) {
                Text(thread.work.title)
                    .font(thread.work.kind.isGoal ? TypeScale.calloutMedium : TypeScale.callout)
                    .lineLimit(1)
                    .truncationMode(.tail)
                HStack(spacing: Space.s1) {
                    Text(thread.work.project)
                    if let r = thread.rollup, r.total > 0 {
                        Text("·")
                        Text("\(r.done)/\(r.total)").monospacedDigit()
                    }
                    Spacer(minLength: Space.s1)
                    Text(ago(thread.work.updated)).monospacedDigit()
                }
                .font(TypeScale.small)
                .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, Space.s1)
    }
}

/// Where a thread is, as a dot: waiting on you, working, at a gate, paused,
/// not started, or settled.
struct StateDot: View {
    let state: WorkState
    let band: Band

    var body: some View {
        Group {
            if band == .needsYou {
                Circle().strokeBorder(Palette.attention, lineWidth: 2).background(Circle().fill(Palette.attention.opacity(0.25)))
            } else {
                switch state {
                case .running: Circle().fill(.green)
                case .review, .softDone: Circle().fill(.blue)
                case .paused: Circle().stroke(.secondary, lineWidth: 1.5)
                case .queued, .briefed: Circle().stroke(.tertiary, lineWidth: 1)
                default: Circle().fill(.secondary.opacity(0.4))
                }
            }
        }
        .frame(width: 8, height: 8)
        .help(state.look.label)
    }
}

struct WorkRow: View {
    let work: Work
    let rollup: Rollup?

    var body: some View {
        HStack(alignment: .top, spacing: Space.s3) {
            StateSymbol(state: work.state)
                .font(TypeScale.title)
                .frame(width: 22)
            VStack(alignment: .leading, spacing: Space.s1) {
                Text(work.title)
                    .font(TypeScale.body.weight(work.kind.isGoal ? .semibold : .regular))
                    .lineLimit(2)
                HStack(spacing: Space.s2) {
                    Text(work.project)
                    Text("·")
                    Text(work.kind.isGoal ? "goal" : work.kind.rawValue)
                    Text("·")
                    Text(work.id).monospaced()
                }
                .font(TypeScale.caption)
                .foregroundStyle(.secondary)
                if let rollup, rollup.total > 0 {
                    ProgressView(value: Double(rollup.done), total: Double(rollup.total)) {
                        Text("\(rollup.done) of \(rollup.total) tasks done").font(TypeScale.small).foregroundStyle(.secondary)
                    }
                    .progressViewStyle(.linear)
                    .controlSize(.small)
                }
            }
        }
        .padding(.vertical, 3)
    }
}

/// A state's own symbol in its own colour, labelled for VoiceOver.
struct StateSymbol: View {
    let state: WorkState

    var body: some View {
        Image(systemName: state.look.symbol)
            .foregroundStyle(state.look.tint.color)
            .accessibilityLabel(state.look.label)
            .help(state.look.label)
    }
}

/// A state as a pill: symbol and label. Delivery is never drawn as one.
struct StatePill: View {
    let state: WorkState

    var body: some View {
        Label(state.look.label, systemImage: state.look.symbol)
            .font(TypeScale.caption.weight(.medium))
            .padding(.horizontal, Space.s2)
            .padding(.vertical, 3)
            .foregroundStyle(state.look.tint.color)
            .background(state.look.tint.color.opacity(0.12), in: Capsule())
    }
}

extension Tint {
    var color: Color {
        switch self {
        case .gray: .gray
        case .blue: .blue
        case .indigo: .indigo
        case .orange: .orange
        case .red: .red
        case .purple: .purple
        case .teal: .teal
        case .green: .green
        case .brown: .brown
        }
    }
}
