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
    /// The open goal's breadcrumb, for the sidebar to show its path.
    @State private var chain: [ChainLink] = []

    init(store: Store) {
        self.store = store
        _writes = State(initialValue: Writes(store: store))
    }

    private var scope: String? { scopeName.isEmpty ? nil : scopeName }

    var body: some View {
        NavigationSplitView {
            ThreadSidebar(store: store, scopeName: $scopeName, place: $place, plan: $plan, asking: $asking, chain: chain)
                .navigationSplitViewColumnWidth(min: 240, ideal: Space.sidebarWidth, max: 380)
        } detail: {
            switch place {
            case .newGoal, nil:
                NewGoalPage(store: store, scope: scope) { id in place = .work(id) }
                    .id(scopeName)
            case .work(let id):
                WorkDetail(store: store, id: id, plan: $plan, chained: { chain = $0 }) { place = .work($0) }
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
            if case .work = place {} else { chain = [] }
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

/// Top goals banded by whether they need you, work or are completed, the open
/// one's path under its row.
struct ThreadSidebar: View {
    let store: Store
    @Binding var scopeName: String
    @Binding var place: Place?
    @Binding var plan: Plan?
    @Binding var asking: Ask?
    /// The open goal's breadcrumb, top goal first.
    let chain: [ChainLink]
    @AppStorage("sidebar.letGo") private var showLetGo = false
    @State private var search = ""

    private var scope: String? { scopeName.isEmpty ? nil : scopeName }

    /// The row that is selected: the open goal's top goal, else the open work.
    private var selected: String? {
        if let top = chain.first { return top.id }
        if case .work(let id) = place { return id }
        return nil
    }

    var body: some View {
        let bands = store.board?.sidebar(project: scope) ?? .none
        let matching = { (r: SidebarRow) in search.isEmpty || r.work.title.localizedCaseInsensitiveContains(search) || r.work.id.hasPrefix(search) }
        VStack(alignment: .leading, spacing: 0) {
            top
            ScrollView {
                VStack(alignment: .leading, spacing: 4) {
                    band("Needs you", bands.needsYou.filter(matching), first: true)
                    band("Working", bands.working.filter(matching))
                    band("Completed", bands.completed.filter(matching))
                    let letGo = bands.letGo.filter(matching)
                    if !letGo.isEmpty {
                        Button { showLetGo.toggle() } label: {
                            HStack(spacing: 4) {
                                MicroLabel("Let go · \(letGo.count)")
                                Image(systemName: showLetGo ? "chevron.down" : "chevron.right")
                                    .font(.system(size: IconSize.tiny, weight: .semibold)).foregroundStyle(Palette.text3)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .padding(EdgeInsets(top: 14, leading: 10, bottom: 6, trailing: 10))
                        if showLetGo {
                            ForEach(letGo) { row($0) }
                        }
                    }
                    MicroLabel("More").padding(EdgeInsets(top: 14, leading: 10, bottom: 6, trailing: 10))
                    more("Taste", "sparkles", .taste)
                    more("Archived", "archivebox", .archived)
                    if let scope {
                        more("What the loop decided", "checklist", .review(scope))
                    } else {
                        ForEach(store.projects) { p in more("Review \(p.name)", "checklist", .review(p.name)) }
                    }
                }
                .padding(.horizontal, 10)
                .padding(.bottom, 14)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Palette.sidebar)
        .overlay(alignment: .trailing) { Rectangle().fill(Palette.hairline).frame(width: 1) }
    }

    /// The product switcher, New Goal and the search field.
    private var top: some View {
        VStack(alignment: .leading, spacing: 0) {
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
                    Text(scope ?? "All products").font(TypeScale.calloutStrong).foregroundStyle(Palette.text).lineLimit(1)
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                .help("Show one product's work, or all of it")
                Spacer(minLength: 0)
                Button("+ New Goal") { place = .newGoal }
                    .buttonStyle(CanvasButtonStyle())
                    .controlSize(.small)
                    .help("New goal (⌘N)")
            }
            .padding(EdgeInsets(top: 4, leading: 6, bottom: 8, trailing: 6))
            TextField("Search goals", text: $search)
                .textFieldStyle(.plain)
                .font(TypeScale.callout)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(Palette.search, in: RoundedRectangle(cornerRadius: Radius.option))
                .padding(EdgeInsets(top: 0, leading: 4, bottom: 10, trailing: 4))
        }
        .padding(EdgeInsets(top: 14, leading: 10, bottom: 0, trailing: 10))
    }

    @ViewBuilder private func band(_ title: String, _ rows: [SidebarRow], first: Bool = false) -> some View {
        if !rows.isEmpty {
            MicroLabel(title).padding(EdgeInsets(top: first ? 6 : 14, leading: 10, bottom: 6, trailing: 10))
            ForEach(rows) { row($0) }
        }
    }

    private func row(_ r: SidebarRow) -> some View {
        SidebarRowView(row: r, selected: selected == r.id, path: r.openPath(chain), open: { place = .work($0) })
            .contextMenu { WorkMenu(work: r.work, run: { plan = $0 }, ask: { asking = $0 }) }
    }

    private func more(_ title: String, _ symbol: String, _ to: Place) -> some View {
        Button { place = to } label: {
            Label(title, systemImage: symbol)
                .font(TypeScale.callout)
                .foregroundStyle(Palette.text)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(place == to ? Palette.sidebarSelected : .clear, in: RoundedRectangle(cornerRadius: Radius.card))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// A goal as one row of the sidebar: dot, title, how far it is, and the open
/// sub-goal's path under it.
struct SidebarRowView: View {
    let row: SidebarRow
    let selected: Bool
    let path: [ChainLink]
    let open: (String) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Circle().fill(dot).frame(width: 8, height: 8).padding(.top, 5)
            VStack(alignment: .leading, spacing: 2) {
                Text(row.work.title)
                    .font(TypeScale.calloutMedium)
                    .foregroundStyle(Palette.text)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
                HStack(spacing: 6) {
                    if let proposed = row.proposedBadge { Badge(proposed, .attention) }
                    Text(row.meta + (row.needsYouBadge == nil ? "" : " ·"))
                        .font(TypeScale.caption)
                        .foregroundStyle(Palette.text3)
                        .lineLimit(1)
                    if let badge = row.needsYouBadge { Badge(badge, .attention) }
                }
                if !path.isEmpty {
                    VStack(alignment: .leading, spacing: 2) {
                        ForEach(path) { link in
                            Button(link.title) { open(link.id) }
                                .buttonStyle(.plain)
                                .font(TypeScale.meta)
                                .foregroundStyle(Palette.text)
                                .lineLimit(1)
                        }
                    }
                    .padding(EdgeInsets(top: 2, leading: 10, bottom: 2, trailing: 0))
                    .overlay(alignment: .leading) { Rectangle().fill(Color(light: 0xc9c9d1, dark: 0x4a4a50)).frame(width: 2) }
                    .padding(.top, 6)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(selected ? Palette.sidebarSelected : .clear, in: RoundedRectangle(cornerRadius: Radius.card))
        .contentShape(Rectangle())
        .onTapGesture { open(row.work.id) }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
    }

    private var dot: Color {
        if !row.work.atRest && (row.needsYou > 0 || row.band == .needsYou) { return Palette.orange }
        return switch row.work.state {
        case .done: Palette.green
        case .dropped, .abandoned, .superseded, .paused, .queued, .briefed: Palette.grey
        default: row.needsYou > 0 || row.band == .needsYou ? Palette.orange : Palette.accent
        }
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
