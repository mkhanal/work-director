import SwiftUI
import WorkDirectorKit

/// A write that needs words from the person: what reopened work is for, or
/// the decision itself. Nothing else asks; it runs, and can be undone.
struct Ask: Identifiable {
    enum Kind { case reopen, decide }
    let kind: Kind
    let work: Work

    var id: String { "\(kind)-\(work.id)" }

    var title: String {
        switch kind {
        case .reopen: "Reopen \(work.title)"
        case .decide: "Record a decision on \(work.title)"
        }
    }

    var prompt: String {
        switch kind {
        case .reopen: "What is being worked on? A question about finished work is answered without reopening it."
        case .decide: "The decision, in one line."
        }
    }

    func plan(_ text: String) throws -> Plan {
        switch kind {
        case .reopen: try Plan.reopen(work.id, reason: text)
        case .decide: try Plan.decide(work.id, text: text)
        }
    }
}

/// Every write work's state allows, as menu items: the toolbar's Actions menu
/// and a right-click on the work anywhere it is listed show the same ones.
struct WorkMenu: View {
    let work: Work
    let run: (Plan) -> Void
    let ask: (Ask) -> Void

    var body: some View {
        if work.state == .paused || work.state == .dropped {
            Button("Resume") { run(.resume(work.id, note: "")) }
        } else if work.live {
            Button("Pause") { run(.pause(work.id)) }
        }
        if work.kind.isGoal && open && work.state != .paused {
            Button("Drive") { run(.drive(work.id)) }
        }
        Button("Record a Decision…") { ask(Ask(kind: .decide, work: work)) }
        if work.state == .review {
            Button("Verify") { run(.verify(work.id)) }
            Button("Soft-Done") { run(.softDone(work.id)) }
        }
        if work.state == .softDone {
            Button("Done") { run(.done(work.id)) }
        }
        if work.state == .done {
            Button("Reopen…") { ask(Ask(kind: .reopen, work: work)) }
        }
        if open {
            Divider()
            Button("Cancel", role: .destructive) { run(.cancel(work.id)) }
        }
        if attempted {
            Button("Abandon", role: .destructive) { run(.abandon(work.id, detail: "")) }
        }
        if work.state == .abandoned {
            Button("Release") { run(.release(work.id)) }
        }
        Divider()
        if work.archived != nil {
            Button("Unarchive") { run(.unarchive(work.id)) }
        } else {
            Button(work.atRest ? "Archive" : "Drop and Archive") { run(.archive(work)) }
        }
        Divider()
        Button("Copy ID") {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(work.id, forType: .string)
        }
    }

    private var open: Bool {
        ![.done, .dropped, .abandoned].contains(work.state)
    }

    private var attempted: Bool {
        [.running, .needsInput, .review, .softDone, .blocked].contains(work.state)
    }
}

/// The toolbar's writes: pause or resume at hand, the rest under Actions.
struct Actions: ToolbarContent {
    let work: Work
    let run: (Plan) -> Void
    let ask: (Ask) -> Void

    var body: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            if work.state == .paused || work.state == .dropped {
                Button { run(.resume(work.id, note: "")) } label: { Label("Resume", systemImage: "play.fill") }
                    .help("Carry on where it left off: wd resume \(work.id)")
            } else if work.live {
                Button { run(.pause(work.id)) } label: { Label("Pause", systemImage: "pause.fill") }
                    .help("Stop the running sessions and keep their conversations: wd pause \(work.id)")
            }
            Menu {
                WorkMenu(work: work, run: run, ask: ask)
            } label: {
                Label("Actions", systemImage: "ellipsis.circle")
            }
        }
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

/// What a write did, shown where the person is: the command it ran, its
/// outcome, and the write that takes it back when there is one.
struct WriteBanner: View {
    let done: Writes.Done
    let undo: (Plan) -> Void
    let close: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: done.ok ? "checkmark.circle.fill" : "xmark.octagon.fill")
                .foregroundStyle(done.ok ? .green : .red)
            VStack(alignment: .leading, spacing: 2) {
                Text(done.plan.commandLine).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                if !done.text.isEmpty {
                    Text(done.text).font(.callout).lineLimit(4).textSelection(.enabled)
                }
            }
            Spacer(minLength: 8)
            if done.ok, let back = done.plan.undo {
                Button("Undo") { undo(back) }.help(back.commandLine)
            }
            Button(action: close) { Image(systemName: "xmark") }.buttonStyle(.plain).foregroundStyle(.secondary)
        }
        .padding(10)
        .background(.bar)
    }
}

/// Runs writes as they are chosen and keeps what the last one did.
@MainActor
@Observable
final class Writes {
    struct Done: Identifiable {
        let id = UUID()
        let plan: Plan
        let ok: Bool
        let text: String
    }

    private(set) var last: Done?
    private(set) var running: Plan?
    private let store: Store

    init(store: Store) { self.store = store }

    func run(_ plan: Plan) async {
        running = plan
        defer { running = nil }
        do {
            let r = try await store.run(plan)
            let text = [r.stdout, r.stderr].map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }.joined(separator: "\n")
            last = Done(plan: plan, ok: r.code == 0, text: text)
        } catch {
            last = Done(plan: plan, ok: false, text: "\(error)")
        }
        await store.refresh()
    }

    func dismiss() { last = nil }
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
