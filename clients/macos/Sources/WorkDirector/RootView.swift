import SwiftUI
import WorkDirectorKit

enum Place: Hashable {
    case band(Band)
    case archived
    case review(String)
}

struct RootView: View {
    let store: Store
    @State private var place: Place? = .band(.needsYou)
    @State private var selected: String?
    @State private var plan: Plan?
    @State private var typing = false
    @State private var composing = false

    var body: some View {
        NavigationSplitView {
            Sidebar(store: store, place: $place, compose: { composing = true })
                .navigationSplitViewColumnWidth(min: 200, ideal: 230)
        } content: {
            Group {
                switch place {
                case .band(let band):
                    BandList(store: store, band: band, selected: $selected)
                case .archived:
                    ArchivedList(store: store, selected: $selected, plan: $plan)
                case .review(let project):
                    ReviewList(store: store, project: project, selected: $selected, plan: $plan)
                case nil:
                    ContentUnavailableView("Choose a band", systemImage: "sidebar.left")
                }
            }
            .navigationSplitViewColumnWidth(min: 300, ideal: 380)
        } detail: {
            if let selected {
                WorkDetail(store: store, id: selected, plan: $plan)
                    .id(selected)
            } else {
                ContentUnavailableView("Nothing selected", systemImage: "square.dashed", description: Text("Choose a goal or a task."))
            }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button { typing = true } label: { Label("Run a wd Command", systemImage: "terminal") }
                    .help("Run a wd command (⌘K)")
            }
        }
        .safeAreaInset(edge: .bottom) {
            if let failure = store.failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(.callout)
                    .foregroundStyle(.red)
                    .padding(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.bar)
            }
        }
        .onChange(of: place) { selected = nil }
        .onReceive(NotificationCenter.default.publisher(for: .openCommandBar)) { _ in typing = true }
        .onReceive(NotificationCenter.default.publisher(for: .newGoal)) { _ in composing = true }
        .sheet(isPresented: $composing) {
            Composer(store: store) { started in
                composing = false
                place = .band(.inFlight)
                Task { @MainActor in selected = started.goal.id }
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .reload)) { _ in Task { await store.refresh() } }
        .sheet(isPresented: $typing) {
            CommandBar { typed in
                typing = false
                plan = typed
            }
        }
        .sheet(item: $plan) { plan in
            ConfirmSheet(store: store, plan: plan)
        }
    }
}

struct Sidebar: View {
    let store: Store
    @Binding var place: Place?
    let compose: () -> Void

    var body: some View {
        List(selection: $place) {
            Button(action: compose) {
                Label("New Goal", systemImage: "square.and.pencil")
            }
            .buttonStyle(.plain)
            .help("Start a goal (⌘N)")
            .padding(.vertical, 4)

            Section("Board") {
                ForEach(Band.allCases, id: \.self) { band in
                    let count = store.board?.items(in: band).count ?? 0
                    Label(band.label, systemImage: band.symbol)
                        .badge(band == .atRest ? 0 : count)
                        .tag(Place.band(band))
                }
                Label("Archived", systemImage: "archivebox")
                    .tag(Place.archived)
            }
            if !store.projects.isEmpty {
                Section("Review") {
                    ForEach(store.projects) { project in
                        Label(project.name, systemImage: "checklist")
                            .tag(Place.review(project.name))
                    }
                }
            }
        }
        .listStyle(.sidebar)
    }
}

struct BandList: View {
    let store: Store
    let band: Band
    @Binding var selected: String?

    var body: some View {
        let items = store.board?.items(in: band) ?? []
        Group {
            if items.isEmpty {
                ContentUnavailableView("Nothing \(band.label.lowercased())", systemImage: band.symbol)
            } else {
                List(items, selection: $selected) { work in
                    WorkRow(work: work, rollup: store.board?.rollup(of: work.id))
                        .tag(work.id)
                }
            }
        }
        .navigationTitle(band.label)
        .navigationSubtitle("\(items.count) item\(items.count == 1 ? "" : "s")")
    }
}

struct WorkRow: View {
    let work: Work
    let rollup: Rollup?

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            StateSymbol(state: work.state)
                .font(.title3)
                .frame(width: 22)
            VStack(alignment: .leading, spacing: 3) {
                Text(work.title)
                    .font(.body.weight(work.kind.isGoal ? .semibold : .regular))
                    .lineLimit(2)
                HStack(spacing: 6) {
                    Text(work.project)
                    Text("·")
                    Text(work.kind.isGoal ? "goal" : work.kind.rawValue)
                    Text("·")
                    Text(work.id).monospaced()
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                if let rollup, rollup.total > 0 {
                    ProgressView(value: Double(rollup.done), total: Double(rollup.total)) {
                        Text("\(rollup.done) of \(rollup.total) tasks done").font(.caption2).foregroundStyle(.secondary)
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
            .font(.caption.weight(.medium))
            .padding(.horizontal, 8)
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
