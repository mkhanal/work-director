import SwiftUI
import WorkDirectorKit

/// A new goal, started the way a conversation is: say what you want, press start.
/// The exact command shows under the text, so starting is the confirmation.
struct Composer: View {
    let store: Store
    let started: (Started) -> Void

    @AppStorage("composer.project") private var project = ""
    @State private var text = ""
    @State private var running = false
    @State private var failure: String?
    @FocusState private var focused: Bool
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Text("New Goal").font(.title3.weight(.semibold))
                Spacer()
                Picker("Project", selection: $project) {
                    ForEach(store.projects) { p in
                        Text(p.name).tag(p.name)
                    }
                }
                .labelsHidden()
                .fixedSize()
            }
            ZStack(alignment: .topLeading) {
                if text.isEmpty {
                    Text("What do you want done? The first line becomes the title.")
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 8)
                        .allowsHitTesting(false)
                }
                TextEditor(text: $text)
                    .font(.body)
                    .scrollContentBackground(.hidden)
                    .padding(.vertical, 8)
                    .focused($focused)
                    .disabled(running)
            }
            .frame(minHeight: 150)
            .padding(4)
            .background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 8))

            if let plan {
                Text(plan.commandLine)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
            }
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(.callout)
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
            }
            HStack {
                if running {
                    ProgressView().controlSize(.small)
                    Text("Planning with \(runner)… this can take a minute.").font(.callout).foregroundStyle(.secondary)
                } else {
                    Text("wd plans the goal with \(runner) and starts its tasks.").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }
                    .keyboardShortcut(.cancelAction)
                    .disabled(running)
                Button("Start") { Task { await start() } }
                    .keyboardShortcut(.return, modifiers: .command)
                    .buttonStyle(.borderedProminent)
                    .disabled(plan == nil || running)
            }
        }
        .padding(20)
        .frame(width: 620)
        .onAppear {
            if !store.projects.contains(where: { $0.name == project }) {
                project = store.projects.first?.name ?? ""
            }
            focused = true
        }
    }

    private var plan: Plan? {
        guard !project.isEmpty else { return nil }
        return try? Plan.start(project: project, text: text)
    }

    private var runner: String {
        store.projects.first { $0.name == project }?.runner ?? "the project's runner"
    }

    private func start() async {
        guard let plan else { return }
        running = true
        defer { running = false }
        do {
            started(try await store.start(plan))
        } catch {
            failure = String(describing: error)
        }
    }
}

/// Work put out of sight, with the way back.
struct ArchivedList: View {
    let store: Store
    @Binding var selected: String?
    @Binding var plan: Plan?

    @State private var items: [Work]?
    @State private var failure: String?

    var body: some View {
        Group {
            if let items {
                if items.isEmpty {
                    ContentUnavailableView("Nothing archived", systemImage: "archivebox")
                } else {
                    List(items, selection: $selected) { work in
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
        .navigationSubtitle(items.map { "\($0.count) item\($0.count == 1 ? "" : "s")" } ?? "")
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
