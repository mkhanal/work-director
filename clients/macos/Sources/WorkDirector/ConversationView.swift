import SwiftUI
import WorkDirectorKit

/// A work item's session as it happens: what it was told, what it said and
/// thought, the tools it called, and the questions it is waiting on, answered
/// in place. The steps are its own; the ledger's record of the work is the
/// Record tab beside it.
struct ConversationView: View {
    let store: Store
    let work: Work
    @State private var feed: Feed

    init(store: Store, work: Work) {
        self.store = store
        self.work = work
        _feed = State(initialValue: Feed(store: store, id: work.id))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let failure = feed.failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
            }
            if !feed.loaded {
                ProgressView().frame(maxWidth: .infinity)
            } else if feed.steps.isEmpty {
                Text(feed.status == .none ? "No session yet: the conversation starts when the work is spawned." : "The session has said nothing yet.")
                    .foregroundStyle(.secondary)
            }
            let rows = ThreadRow.rows(feed.steps)
            ForEach(rows) { row in
                switch row {
                case .step(let index, let step):
                    StepView(store: store, work: work, step: step, first: index == 0,
                             latest: index == feed.steps.count - 1 && feed.status == .running,
                             answered: { Task { await feed.read() } })
                case .tools(_, let calls):
                    ToolGroup(calls: calls, working: feed.status == .running)
                }
            }
            if feed.status == .running {
                Label {
                    Text("Working…")
                } icon: {
                    ProgressView().controlSize(.small)
                }
                .foregroundStyle(.secondary)
                .font(.callout)
            }
        }
        .task { await feed.follow() }
    }
}

struct StepView: View {
    let store: Store
    let work: Work
    let step: Step
    /// The first step is the brief the session was spawned with.
    let first: Bool
    /// The newest step of a session still working.
    let latest: Bool
    let answered: () -> Void

    var body: some View {
        switch step.kind {
        case .prompt:
            PromptRow(text: step.text ?? "", brief: first, at: step.at)
        case .text:
            MarkdownText(step.text ?? "")
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        case .thinking:
            ThinkingRow(text: step.text ?? "", open: latest)
        case .tool:
            if let tool = step.tool { ToolRow(call: tool, working: latest) }
        case .question:
            if let q = step.question {
                QuestionCard(store: store, workID: work.id, question: q, answered: answered)
            }
        }
    }
}

/// What the session was told. A brief is long, so it opens folded.
struct PromptRow: View {
    let text: String
    let brief: Bool
    let at: String?
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: brief ? "doc.text" : "person.fill")
                Text(brief ? "Brief" : "Told").font(.caption.weight(.semibold))
                if let at { Text(when(at)).font(.caption).foregroundStyle(.tertiary) }
                Spacer()
                if folds {
                    Button(expanded ? "Show less" : "Show all") { expanded.toggle() }
                        .buttonStyle(.link)
                        .font(.caption)
                }
            }
            .foregroundStyle(.secondary)
            Text(text)
                .lineLimit(expanded || !folds ? nil : 4)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(10)
        .background(Color.accentColor.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
    }

    private var folds: Bool { text.count > 400 || text.split(separator: "\n").count > 4 }
}

/// The executor's reasoning, as its provider summarises it. Folded unless it
/// is what the session is doing right now.
struct ThinkingRow: View {
    let text: String
    @State private var expanded: Bool

    init(text: String, open: Bool) {
        self.text = text
        _expanded = State(initialValue: open)
    }

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            Text(text)
                .italic()
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 4)
        } label: {
            HStack(spacing: 6) {
                Image(systemName: "brain")
                Text("Thinking").fontWeight(.medium)
                if !expanded {
                    Text(firstLine).lineLimit(1).truncationMode(.tail).foregroundStyle(.tertiary)
                }
            }
            .font(.callout)
            .foregroundStyle(.secondary)
        }
    }

    private var firstLine: String {
        text.split(separator: "\n").first.map(String.init) ?? ""
    }
}

/// One tool call: what it acted on in a line, its input and result on demand.
struct ToolRow: View {
    let call: ToolCall
    let working: Bool
    @State private var expanded = false

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 8) {
                if !call.input.isEmpty { Mono(title: "Input", text: call.input) }
                if let result = call.result {
                    Mono(title: call.failed ? "Failed" : "Result", text: result.isEmpty ? "(no output)" : result, failed: call.failed)
                }
            }
            .padding(.top, 4)
        } label: {
            HStack(spacing: 8) {
                Image(systemName: Self.symbol(call.name)).frame(width: 16)
                Text(call.name).fontWeight(.medium)
                Text(call.summary)
                    .font(.callout.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 4)
                if call.result == nil {
                    if working { ProgressView().controlSize(.mini) }
                } else if call.failed {
                    Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
                }
            }
            .font(.callout)
        }
    }

    static func symbol(_ name: String) -> String {
        switch name {
        case "Bash": "terminal"
        case "Read": "doc.text"
        case "Edit", "MultiEdit", "NotebookEdit": "pencil"
        case "Write": "square.and.pencil"
        case "Grep", "Glob": "magnifyingglass"
        case "WebFetch", "WebSearch": "globe"
        case "Agent", "Task": "person.2"
        case "TodoWrite": "checklist"
        case "Skill": "book"
        default: "wrench.and.screwdriver"
        }
    }
}

/// A run of tool calls between two things said, folded into one row that
/// names what they did.
struct ToolGroup: View {
    let calls: [ToolCall]
    let working: Bool
    @State private var expanded = false

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(calls.enumerated()), id: \.offset) { i, call in
                    ToolRow(call: call, working: working && i == calls.count - 1)
                }
            }
            .padding(.leading, 8)
            .padding(.top, 4)
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "wrench.and.screwdriver").frame(width: 16)
                Text(summary).fontWeight(.medium)
                Spacer(minLength: 4)
                if failed > 0 {
                    Label("\(failed) failed", systemImage: "xmark.circle.fill").foregroundStyle(.red).font(.caption)
                }
                if working && calls.last?.result == nil { ProgressView().controlSize(.mini) }
            }
            .font(.callout)
        }
    }

    private var failed: Int { calls.filter(\.failed).count }

    /// "Ran 3 commands, read 2 files" — each kind of call counted once.
    private var summary: String {
        var counts: [(String, Int)] = []
        for call in calls {
            let noun = Self.noun(call.name)
            if let i = counts.firstIndex(where: { $0.0 == noun }) { counts[i].1 += 1 } else { counts.append((noun, 1)) }
        }
        return counts.map { "\($0.1) \($0.0)\($0.1 == 1 ? "" : "s")" }.joined(separator: ", ")
    }

    private static func noun(_ name: String) -> String {
        switch name {
        case "Bash": "command"
        case "Read": "read"
        case "Edit", "MultiEdit", "Write", "NotebookEdit": "edit"
        case "Grep", "Glob": "search"
        case "WebFetch", "WebSearch": "web lookup"
        case "Agent", "Task": "subagent"
        default: "\(name) call"
        }
    }
}

struct Mono: View {
    let title: String
    let text: String
    var failed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title).font(.caption.weight(.semibold)).foregroundStyle(failed ? .red : .secondary)
            ScrollView {
                Text(text)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(8)
            }
            .frame(maxHeight: 240)
            .fixedSize(horizontal: false, vertical: true)
            .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 6))
        }
    }
}

/// A question the session asked, laid out as it asked it: each item with its
/// options, picked in place and sent as the answer. Answered, it stays where it
/// was asked, showing what was chosen.
struct QuestionCard: View {
    let store: Store
    let workID: String
    let question: Question
    var title: String? = nil
    let answered: () -> Void

    @State private var draft: AnswerDraft
    @State private var sending = false
    @State private var failure: String?

    init(store: Store, workID: String, question: Question, title: String? = nil, answered: @escaping () -> Void) {
        self.store = store
        self.workID = workID
        self.question = question
        self.title = title
        self.answered = answered
        _draft = State(initialValue: AnswerDraft(question))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 6) {
                Image(systemName: question.pending ? "questionmark.bubble.fill" : "checkmark.bubble")
                Text(title ?? (question.pending ? "Waiting on you" : "Asked"))
                    .font(.caption.weight(.semibold))
            }
            .foregroundStyle(question.pending ? Color.orange : Color.secondary)
            ForEach(Array(question.items.enumerated()), id: \.offset) { i, item in
                itemView(item, at: i)
            }
            if question.pending {
                footer
            } else if let reply = question.reply {
                Label {
                    Text(reply).lineLimit(4).textSelection(.enabled)
                } icon: {
                    Image(systemName: "arrowshape.turn.up.left")
                }
                .font(.callout)
                .foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .background(question.pending ? Color.orange.opacity(0.07) : Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(question.pending ? Color.orange.opacity(0.5) : Color.clear))
    }

    @ViewBuilder private func itemView(_ item: QuestionItem, at i: Int) -> some View {
        let answer = question.answers.flatMap { $0.indices.contains(i) ? $0[i] : nil }
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                if !item.header.isEmpty {
                    Text(item.header)
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(.quaternary, in: Capsule())
                }
                if item.multi { Text("pick any").font(.caption2).foregroundStyle(.tertiary) }
            }
            Text(item.question).font(.body.weight(.medium)).textSelection(.enabled)
            ForEach(item.options, id: \.label) { option in
                optionRow(option, item: item, at: i, chosen: chosen(option.label, answer: answer, item: i))
            }
            if question.pending {
                TextField(item.multi ? "Add something else…" : "Or answer in your own words…", text: $draft.typed[i])
                    .textFieldStyle(.roundedBorder)
                    .font(.callout)
            } else if let answer, !item.options.contains(where: { answerParts(answer).contains($0.label) }) {
                Label(answer, systemImage: "text.bubble").font(.callout).textSelection(.enabled)
            }
        }
    }

    private func optionRow(_ option: QuestionOption, item: QuestionItem, at i: Int, chosen: Bool) -> some View {
        Button {
            draft.toggle(option.label, item: i)
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: item.multi ? (chosen ? "checkmark.square.fill" : "square") : (chosen ? "largecircle.fill.circle" : "circle"))
                    .foregroundStyle(chosen ? Color.accentColor : Color.secondary)
                VStack(alignment: .leading, spacing: 1) {
                    Text(option.label).fontWeight(chosen ? .semibold : .regular)
                    if !option.description.isEmpty {
                        Text(option.description).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 0)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!question.pending || sending)
    }

    private func chosen(_ label: String, answer: String?, item: Int) -> Bool {
        if question.pending { return draft.picked[item].contains(label) }
        return answer.map { answerParts($0).contains(label) } ?? false
    }

    private func answerParts(_ answer: String) -> [String] {
        answer.components(separatedBy: ", ")
    }

    private var plan: Plan? {
        draft.answers.flatMap { try? Plan.answer(workID, question, answers: $0) }
    }

    private var footer: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Button {
                    Task { await send() }
                } label: {
                    if sending { ProgressView().controlSize(.small) } else { Text("Answer") }
                }
                .buttonStyle(.borderedProminent)
                .disabled(plan == nil || sending)
                if let command = plan?.commandLine {
                    Text(command)
                        .font(.caption2.monospaced())
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(.caption)
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
            }
        }
    }

    private func send() async {
        guard let plan else { return }
        sending = true
        defer { sending = false }
        do {
            let result = try await store.run(plan)
            if result.code == 0 {
                failure = nil
                answered()
            } else {
                failure = result.stderr.isEmpty ? result.stdout : result.stderr
            }
        } catch {
            failure = "\(error)"
        }
        await store.refresh()
    }
}

/// What a task's session is doing now, in a line under the task.
struct ActivityLine: View {
    let activity: Activity

    var body: some View {
        if let error = activity.error {
            Label(error, systemImage: "exclamationmark.triangle").foregroundStyle(.red).lineLimit(1)
        } else if let now = activity.now, !now.isEmpty {
            Label {
                Text(now).lineLimit(1).truncationMode(.tail)
            } icon: {
                Image(systemName: symbol)
            }
            .foregroundStyle(activity.asking != nil ? Color.orange : Color.secondary)
        }
    }

    private var symbol: String {
        switch activity.kind {
        case .thinking: "brain"
        case .tool: "wrench.and.screwdriver"
        case .question: "questionmark.bubble"
        case .prompt: "person"
        default: "text.bubble"
        }
    }
}

/// Markdown as an executor writes it: headings, lists, code blocks and
/// paragraphs, each with its inline emphasis, code and links.
struct MarkdownText: View {
    let blocks: [Block]

    enum Block: Hashable {
        case heading(Int, String)
        case bullet(String, ordered: String?)
        case code(String)
        case paragraph(String)
    }

    init(_ text: String) { blocks = Self.parse(text) }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                switch block {
                case .heading(let level, let text):
                    inline(text).font(level <= 1 ? .title3.weight(.semibold) : .headline)
                case .bullet(let text, let ordered):
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(ordered ?? "•").foregroundStyle(.secondary)
                        inline(text)
                    }
                case .code(let text):
                    Text(text)
                        .font(.callout.monospaced())
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(8)
                        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 6))
                case .paragraph(let text):
                    inline(text)
                }
            }
        }
    }

    private func inline(_ text: String) -> Text {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        if let attributed = try? AttributedString(markdown: text, options: options) {
            return Text(attributed)
        }
        return Text(text)
    }

    static func parse(_ text: String) -> [Block] {
        var out: [Block] = []
        var paragraph: [String] = []
        var code: [String]?
        func flush() {
            if !paragraph.isEmpty { out.append(.paragraph(paragraph.joined(separator: "\n"))) }
            paragraph = []
        }
        for raw in text.components(separatedBy: "\n") {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.hasPrefix("```") {
                if let open = code {
                    out.append(.code(open.joined(separator: "\n")))
                    code = nil
                } else {
                    flush()
                    code = []
                }
                continue
            }
            if code != nil {
                code?.append(raw)
                continue
            }
            if line.isEmpty {
                flush()
            } else if let hashes = line.firstIndex(where: { $0 != "#" }), hashes != line.startIndex, line[hashes] == " " {
                flush()
                out.append(.heading(line.distance(from: line.startIndex, to: hashes), String(line[hashes...]).trimmingCharacters(in: .whitespaces)))
            } else if line.hasPrefix("- ") || line.hasPrefix("* ") {
                flush()
                out.append(.bullet(String(line.dropFirst(2)), ordered: nil))
            } else if let dot = line.firstIndex(of: "."), line[..<dot].allSatisfy(\.isNumber), !line[..<dot].isEmpty,
                      line[line.index(after: dot)...].hasPrefix(" ") {
                flush()
                out.append(.bullet(String(line[line.index(dot, offsetBy: 2)...]), ordered: String(line[...dot])))
            } else {
                paragraph.append(raw)
            }
        }
        if let open = code { out.append(.code(open.joined(separator: "\n"))) }
        flush()
        return out
    }
}

/// An ISO time as a reader wants it: the time today, the date otherwise.
func when(_ iso: String) -> String {
    let parser = ISO8601DateFormatter()
    parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    guard let date = parser.date(from: iso) ?? ISO8601DateFormatter().date(from: iso) else { return iso }
    return Calendar.current.isDateInToday(date)
        ? date.formatted(date: .omitted, time: .shortened)
        : date.formatted(date: .abbreviated, time: .shortened)
}
