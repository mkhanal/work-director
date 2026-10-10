import SwiftUI
import WorkDirectorKit

/// A write that needs words from the person before it can be planned.
struct Ask: Identifiable {
    enum Kind { case reopen, release, decide, abandon }
    let kind: Kind
    let work: Work

    var id: String { "\(kind)-\(work.id)" }

    var title: String {
        switch kind {
        case .reopen: "Reopen \(work.title)"
        case .release: "Release \(work.title)"
        case .decide: "Record a decision on \(work.title)"
        case .abandon: "Abandon \(work.title)"
        }
    }

    var prompt: String {
        switch kind {
        case .reopen: "What is being worked on? A question about finished work is answered without reopening it."
        case .release: "What was true instead? The stop stays on the record; this says it was a choice."
        case .decide: "The decision, in one line."
        case .abandon: "Detail (optional). The reason, no-pr or unmerged, is worked out from the ledger."
        }
    }

    func plan(_ text: String) throws -> Plan {
        switch kind {
        case .reopen: try Plan.reopen(work.id, reason: text)
        case .release: try Plan.release(work.id, reason: text)
        case .decide: try Plan.decide(work.id, text: text)
        case .abandon: Plan.abandon(work.id, detail: text)
        }
    }
}

/// The writes a state allows, each going through the same confirmation.
struct Actions: ToolbarContent {
    let work: Work
    @Binding var plan: Plan?
    @Binding var asking: Ask?

    var body: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            if work.kind.isGoal && open {
                Button { plan = .drive(work.id) } label: { Label("Drive", systemImage: "steeringwheel") }
                    .help("Run the goal's loop until a stop condition")
            }
            Menu {
                Button("Record a Decision…") { asking = Ask(kind: .decide, work: work) }
                if work.state == .review {
                    Button("Verify") { plan = .verify(work.id) }
                    Button("Soft-Done") { plan = .softDone(work.id) }
                }
                if work.state == .softDone {
                    Button("Done") { plan = .done(work.id) }
                }
                if work.state == .done {
                    Button("Reopen…") { asking = Ask(kind: .reopen, work: work) }
                }
                if attempted {
                    Divider()
                    Button("Abandon…", role: .destructive) { asking = Ask(kind: .abandon, work: work) }
                }
                if work.state == .abandoned {
                    Button("Release…") { asking = Ask(kind: .release, work: work) }
                }
            } label: {
                Label("Actions", systemImage: "ellipsis.circle")
            }
        }
    }

    private var open: Bool {
        ![.done, .dropped, .abandoned].contains(work.state)
    }

    private var attempted: Bool {
        [.running, .needsInput, .review, .softDone, .blocked].contains(work.state)
    }
}

struct ReasonSheet: View {
    let ask: Ask
    let planned: (Plan) -> Void
    @State private var text = ""
    @State private var refusal: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(ask.title).font(.headline)
            Text(ask.prompt).font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            TextField("", text: $text, axis: .vertical)
                .lineLimit(3...6)
                .textFieldStyle(.roundedBorder)
                .onSubmit(next)
            if let refusal {
                Text(refusal).font(.caption).foregroundStyle(.red)
            }
            HStack {
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("Continue", action: next)
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .frame(width: 460)
    }

    private func next() {
        do {
            planned(try ask.plan(text))
        } catch {
            refusal = "\(error)"
        }
    }
}

/// Shows the exact command a write runs, and runs only that, on confirm.
struct ConfirmSheet: View {
    let store: Store
    let plan: Plan
    @State private var running = false
    @State private var result: Result<ActionResult, any Error>?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(plan.title).font(.headline)
            GroupBox {
                Text(plan.commandLine)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            }
            switch result {
            case nil:
                Text("This runs exactly the command above.").font(.caption).foregroundStyle(.secondary)
            case .success(let r):
                Output(code: r.code, text: r.stdout)
            case .failure(let error):
                Output(code: nil, text: "\(error)")
            }
            HStack {
                if running { ProgressView().controlSize(.small) }
                Spacer()
                if result == nil {
                    Button("Cancel", role: .cancel) { dismiss() }
                        .keyboardShortcut(.cancelAction)
                    Button("Run") { Task { await run() } }
                        .keyboardShortcut(.defaultAction)
                        .disabled(running)
                } else {
                    Button("Close") { dismiss() }
                        .keyboardShortcut(.defaultAction)
                }
            }
        }
        .padding(20)
        .frame(width: 560)
    }

    private func run() async {
        running = true
        defer { running = false }
        do {
            result = .success(try await store.run(plan))
        } catch {
            result = .failure(error)
        }
        await store.refresh()
    }
}

struct Output: View {
    let code: Int?
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if code == 0 {
                Label("Done", systemImage: "checkmark.circle.fill").foregroundStyle(.green)
            } else {
                Label(code.map { "Exited \($0)" } ?? "Could not run", systemImage: "xmark.octagon.fill").foregroundStyle(.red)
            }
            if !text.isEmpty {
                ScrollView {
                    Text(text)
                        .font(.callout.monospaced())
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(maxHeight: 220)
            }
        }
    }
}

/// The command bar: a wd command typed as words, previewed as the argv it becomes.
struct CommandBar: View {
    let planned: (Plan) -> Void
    @State private var text = ""
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 6) {
                Text("wd").font(.title3.monospaced()).foregroundStyle(.secondary)
                TextField("status   ·   goal status <id>   ·   decide <id> \"…\"", text: $text)
                    .textFieldStyle(.plain)
                    .font(.title3.monospaced())
                    .onSubmit(submit)
            }
            .padding(10)
            .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 8))
            preview
            HStack {
                Text("Nothing runs until you confirm the next step.").font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Continue", action: submit).keyboardShortcut(.defaultAction).disabled(parsed == nil)
            }
        }
        .padding(16)
        .frame(width: 620)
    }

    private var parsed: Plan? { try? Plan.typed(text) }

    @ViewBuilder private var preview: some View {
        if text.trimmingCharacters(in: .whitespaces).isEmpty {
            EmptyView()
        } else {
            switch Result(catching: { try Plan.typed(text) }) {
            case .success(let plan):
                FlowRow(words: plan.argv)
            case .failure(let error):
                Text(String(describing: error)).font(.caption).foregroundStyle(.red)
            }
        }
    }

    private func submit() {
        guard let parsed else { return }
        planned(parsed)
    }
}

/// The argv a typed command becomes, one capsule per word.
struct FlowRow: View {
    let words: [String]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(Array(words.enumerated()), id: \.offset) { _, word in
                    Text(word)
                        .font(.callout.monospaced())
                        .padding(.horizontal, 8)
                        .padding(.vertical, 3)
                        .background(.tint.opacity(0.12), in: Capsule())
                }
            }
        }
    }
}
