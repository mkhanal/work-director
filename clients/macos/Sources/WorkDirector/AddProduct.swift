import AppKit
import SwiftUI
import WorkDirectorKit

/// Registers a folder as a product: where it is, what it is called, which
/// agent does its work, and whether the director asks before acting.
struct AddProductSheet: View {
    let store: Store
    let added: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var path = ""
    @State private var name = ""
    @State private var runner = ""
    @State private var mode = "auto"
    @State private var runners: [Doctor.Runner] = []
    @State private var failure: String?
    @State private var adding = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Add a product").font(.headline)
            Text("A product is a repository the director plans and works in. Its checks and specifications are read from the repository itself.")
                .font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            Form {
                LabeledContent("Folder") {
                    HStack {
                        Text(path.isEmpty ? "None chosen" : path)
                            .lineLimit(1).truncationMode(.head)
                            .foregroundStyle(path.isEmpty ? .secondary : .primary)
                        Spacer()
                        Button("Choose…", action: choose)
                    }
                }
                TextField("Name", text: $name)
                Picker("Agent", selection: $runner) {
                    ForEach(runners) { r in
                        Text(r.detected ? r.runner : "\(r.runner) (not installed)").tag(r.runner)
                    }
                }
                Picker("Mode", selection: $mode) {
                    Text("Auto: spawn work and report back").tag("auto")
                    Text("Ask: confirm before spawning and landing").tag("ask")
                }
                .pickerStyle(.radioGroup)
            }
            if let failure {
                Text(failure).font(.caption).foregroundStyle(.red).textSelection(.enabled)
            }
            HStack {
                if let plan { Text(plan.commandLine).font(.caption2.monospaced()).foregroundStyle(.tertiary).lineLimit(1).truncationMode(.middle) }
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Add") { Task { await add() } }
                    .keyboardShortcut(.defaultAction)
                    .disabled(plan == nil || adding)
            }
        }
        .padding(20)
        .frame(width: 520)
        .task {
            do {
                runners = try await store.doctor().runners
                runner = runners.first(where: \.detected)?.runner ?? runners.first?.runner ?? ""
            } catch {
                failure = "\(error)"
            }
        }
    }

    private var plan: Plan? { try? Plan.addProduct(name: name, path: path, runner: runner, mode: mode) }

    private func choose() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Choose"
        guard panel.runModal() == .OK, let url = panel.url else { return }
        path = url.path
        if name.isEmpty { name = Plan.productName(forFolder: url.path) }
    }

    private func add() async {
        guard let plan else { return }
        adding = true
        defer { adding = false }
        do {
            let result = try await store.run(plan)
            guard result.code == 0 else {
                failure = result.stderr.isEmpty ? result.stdout : result.stderr
                return
            }
            try await store.reloadProjects()
            added(name)
            dismiss()
        } catch {
            failure = "\(error)"
        }
    }
}
