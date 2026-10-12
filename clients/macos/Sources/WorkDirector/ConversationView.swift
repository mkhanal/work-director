import SwiftUI
import WorkDirectorKit

/// A work item's session as it happens: what it was told, what it said and
/// thought, the tools it called, and the questions it is waiting on, answered
/// in place. The agent's steps read as its turns: its mark and name open each
/// run of them, and its words sit flush beside the mark.
struct ConversationView: View {
    let store: Store
    let work: Work
    /// The session, or one of its subagents, as it is read; whoever shows the
    /// conversation owns it, so the agents panel reads the same reads.
    let feed: Feed
    /// What the director answered this work in its place, so those prompts
    /// read as the director's.
    var directorAnswers: [String] = []

    var body: some View {
        // Lazy: a long session has thousands of steps, and only those on
        // screen are laid out on each read.
        LazyVStack(alignment: .leading, spacing: Space.s4) {
            if let failure = feed.failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(Palette.failure)
                    .textSelection(.enabled)
            }
            if !feed.loaded {
                ProgressView().frame(maxWidth: .infinity).padding(.top, Space.s7)
            } else if feed.steps.isEmpty {
                ContentUnavailableView(feed.status == .none ? "No session yet" : "Nothing said yet",
                                       systemImage: "bubble.left.and.text.bubble.right",
                                       description: Text(feed.status == .none ? "The conversation starts when the work is spawned." : "The session is starting."))
            }
            let rows = ThreadRow.rows(feed.steps)
            ForEach(Array(rows.enumerated()), id: \.element.id) { n, row in
                let agentsTurn = !isPrompt(row)
                let opensRun = agentsTurn && (n == 0 || isPrompt(rows[n - 1]))
                VStack(alignment: .leading, spacing: Space.s2) {
                    if opensRun {
                        Turn(speaker: speaker, at: at(row)) { EmptyView() }
                            .padding(.top, n == 0 ? 0 : Space.s2)
                    }
                    rowView(row)
                        .padding(.leading, agentsTurn ? Space.turnIndent : 0)
                }
            }
            if feed.agent == nil {
                ForEach(store.outbox.filter { $0.work == work.id && !$0.arrived(in: feed.steps) }) { message in
                    OutgoingBubble(store: store, message: message, waiting: feed.status == .running ? "queued: read once it is ready" : "sent")
                }
            }
            if feed.status == .running {
                HStack(spacing: Space.s2) {
                    ProgressView().controlSize(.small)
                    Text("Working…")
                }
                .font(TypeScale.callout)
                .foregroundStyle(.secondary)
                .padding(.leading, Space.turnIndent)
            }
        }
        .task(id: ObjectIdentifier(feed)) { await feed.follow() }
    }

    @ViewBuilder private func rowView(_ row: ThreadRow) -> some View {
        switch row {
        case .step(let index, let step):
            StepView(store: store, work: work, step: step, first: index == 0,
                     latest: index == feed.steps.count - 1 && feed.status == .running,
                     directorAnswered: step.kind == .prompt && directorAnswers.contains { (step.text ?? "").contains($0) },
                     answered: { Task { await feed.read() } })
        case .tools(_, let calls):
            ToolGroup(calls: calls, working: feed.status == .running)
        }
    }

    private var speaker: Speaker {
        .agent(feed.agent.map { "Subagent \($0)" } ?? work.runner.map { $0.capitalized } ?? "Agent")
    }

    private func isPrompt(_ row: ThreadRow) -> Bool {
        if case .step(_, let step) = row { return step.kind == .prompt }
        return false
    }

    private func at(_ row: ThreadRow) -> String? {
        if case .step(_, let step) = row { return step.at }
        return nil
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
    /// Whether this prompt is the director answering in the person's place.
    var directorAnswered = false
    let answered: () -> Void

    var body: some View {
        switch step.kind {
        case .prompt where directorAnswered:
            Turn(speaker: .director, at: step.at, detail: { Text("answered from what was decided").foregroundStyle(.secondary) }) {
                Text(step.text ?? "")
                    .readable()
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .card(Palette.settled, padding: Space.s3)
            }
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

/// What the session was told: its brief, folded, or a message after it.
struct PromptRow: View {
    let text: String
    let brief: Bool
    let at: String?
    var label: String? = nil
    @State private var expanded = false

    var body: some View {
        HStack(alignment: .top, spacing: Space.s3) {
            Image(systemName: brief ? "doc.text.fill" : "arrow.turn.down.right")
                .font(.system(size: 15))
                .foregroundStyle(.secondary)
                .frame(width: Space.avatar, height: 18)
            VStack(alignment: .leading, spacing: Space.s2) {
                HStack(spacing: Space.s1 + 2) {
                    Text(label ?? (brief ? "Brief" : "Told"))
                    if let at { Text(when(at)).foregroundStyle(.tertiary) }
                    Spacer()
                    if folds {
                        Button(expanded ? "Show less" : "Show all") { expanded.toggle() }
                            .buttonStyle(.link)
                    }
                }
                .font(TypeScale.meta)
                Text(text)
                    .readable()
                    .foregroundStyle(brief ? .secondary : .primary)
                    .lineLimit(expanded || !folds ? nil : (brief ? 3 : 6))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .card(padding: Space.s3)
        }
    }

    private var folds: Bool { text.count > 400 || text.split(separator: "\n").count > (brief ? 3 : 6) }
}

/// What the person said, set apart from everything else said on the work so
/// it is found again scrolling back: on the right, in the accent colour.
struct YouBubble<Status: View>: View {
    let text: String
    let at: String
    var label = "You"
    @ViewBuilder var status: Status
    @State private var expanded = false

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            Spacer(minLength: 0)
            VStack(alignment: .trailing, spacing: Space.s2) {
                HStack(spacing: Space.s1 + 2) {
                    status
                    Text(at).foregroundStyle(.tertiary)
                    Text(label).foregroundStyle(.primary)
                    Speaker.you.mark.font(.system(size: 15))
                }
                .font(TypeScale.meta)
                .foregroundStyle(.secondary)
                Text(text)
                    .readable()
                    .lineLimit(expanded || !folds ? nil : 8)
                    .textSelection(.enabled)
                    .padding(.horizontal, Space.s4 - 2)
                    .padding(.vertical, Space.s3 - 1)
                    .background(Color.accentColor.opacity(0.14), in: RoundedRectangle(cornerRadius: Radius.bubble))
                    .overlay(RoundedRectangle(cornerRadius: Radius.bubble).strokeBorder(Color.accentColor.opacity(0.3)))
                if folds {
                    Button(expanded ? "Show less" : "Show all") { expanded.toggle() }
                        .buttonStyle(.link)
                        .font(TypeScale.meta)
                }
            }
            .containerRelativeFrame(.horizontal, alignment: .trailing) { width, _ in width * 0.8 }
        }
    }

    private var folds: Bool { text.count > 800 || text.split(separator: "\n").count > 8 }
}

extension YouBubble where Status == EmptyView {
    init(text: String, at: String, label: String = "You") {
        self.init(text: text, at: at, label: label) { EmptyView() }
    }
}

/// A message on its way: sending, waiting to be read, or refused with why and
/// a way to send it again.
struct OutgoingBubble: View {
    let store: Store
    let message: Outgoing
    /// What a message `wd` took waits on before it shows in the record.
    var waiting = "sent"

    var body: some View {
        VStack(alignment: .trailing, spacing: Space.s1) {
            YouBubble(text: message.text, at: message.at.formatted(date: .omitted, time: .shortened)) {
                switch message.state {
                case .sending:
                    ProgressView().controlSize(.mini)
                    Text("Sending…")
                case .sent:
                    Image(systemName: "checkmark")
                    Text(waiting)
                case .failed:
                    Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(Palette.failure)
                    Text("Not sent").foregroundStyle(Palette.failure)
                }
            }
            if case .failed(let why, _) = message.state {
                HStack(spacing: Space.s3) {
                    Text(why).foregroundStyle(Palette.failure).lineLimit(3).textSelection(.enabled)
                    Button("Retry") { store.retry(message.id) }.buttonStyle(.link)
                    Button("Discard") { store.discard(message.id) }.buttonStyle(.link)
                }
                .font(TypeScale.meta)
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
    }
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
                .readable()
                .italic()
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, Space.s2)
        } label: {
            HStack(spacing: Space.s2) {
                Image(systemName: "brain")
                Text("Thinking").font(TypeScale.calloutMedium)
                if !expanded {
                    Text(firstLine).lineLimit(1).truncationMode(.tail).foregroundStyle(.tertiary)
                }
            }
            .font(TypeScale.callout)
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
            VStack(alignment: .leading, spacing: Space.s2) {
                if !call.input.isEmpty { Mono(title: "Input", text: call.input) }
                if let result = call.result {
                    Mono(title: call.failed ? "Failed" : "Result", text: result.isEmpty ? "(no output)" : result, failed: call.failed)
                }
            }
            .padding(.top, Space.s2)
        } label: {
            HStack(spacing: Space.s2) {
                Image(systemName: Self.symbol(call.name)).frame(width: 16).foregroundStyle(.secondary)
                Text(call.name).font(TypeScale.calloutMedium)
                Text(call.summary)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 4)
                if call.result == nil {
                    if working { ProgressView().controlSize(.mini) }
                } else if call.failed {
                    Image(systemName: "xmark.circle.fill").foregroundStyle(Palette.failure)
                }
            }
            .font(TypeScale.callout)
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
            VStack(alignment: .leading, spacing: Space.s2) {
                ForEach(Array(calls.enumerated()), id: \.offset) { i, call in
                    ToolRow(call: call, working: working && i == calls.count - 1)
                }
            }
            .padding(.leading, Space.s2)
            .padding(.top, Space.s2)
        } label: {
            HStack(spacing: Space.s2) {
                Image(systemName: "wrench.and.screwdriver").frame(width: 16)
                Text(summary).font(TypeScale.calloutMedium)
                Spacer(minLength: 4)
                if failed > 0 {
                    Label("\(failed) failed", systemImage: "xmark.circle.fill").foregroundStyle(Palette.failure).font(TypeScale.meta)
                }
                if working && calls.last?.result == nil { ProgressView().controlSize(.mini) }
            }
            .font(TypeScale.callout)
            .foregroundStyle(.secondary)
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
        VStack(alignment: .leading, spacing: Space.s1) {
            MicroLabel(title).foregroundStyle(failed ? Palette.failure : .secondary)
            ScrollView {
                Text(text)
                    .font(.system(size: 12, design: .monospaced))
                    .lineSpacing(TypeScale.codeLeading)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(Space.s3)
            }
            .frame(maxHeight: 280)
            .fixedSize(horizontal: false, vertical: true)
            .background(Palette.sunken, in: RoundedRectangle(cornerRadius: Radius.card))
        }
    }
}

/// A question asked of the person, laid out as it was asked: one part at a
/// time, its choices as rows to pick, and words of their own always possible.
/// Answered, it stays where it was asked, showing what was chosen.
struct QuestionCard: View {
    let store: Store
    let workID: String
    let question: Question
    var title: String? = nil
    let answered: () -> Void

    @State private var draft: AnswerDraft
    @State private var page = 0
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

    private var tint: Color { question.pending ? Palette.attention : .secondary }
    private var many: Bool { question.items.count > 1 }

    var body: some View {
        HStack(spacing: 0) {
            Rectangle().fill(tint.opacity(question.pending ? 1 : 0.3)).frame(width: 3)
            VStack(alignment: .leading, spacing: Space.s4) {
                HStack(spacing: Space.s2) {
                    Image(systemName: question.pending ? "questionmark.bubble.fill" : "checkmark.bubble")
                    Text(title ?? (question.pending ? "Waiting on you" : "Asked"))
                    Spacer()
                    if many && question.pending {
                        Text("\(page + 1) of \(question.items.count)").foregroundStyle(.secondary).monospacedDigit()
                    }
                }
                .font(TypeScale.meta)
                .foregroundStyle(tint)
                if question.pending {
                    itemView(question.items[page], at: page)
                        .id(page)
                    footer
                } else {
                    ForEach(Array(question.items.enumerated()), id: \.offset) { i, item in
                        itemView(item, at: i)
                    }
                    if let reply = question.reply {
                        Label {
                            Text(reply).readable().lineLimit(4).textSelection(.enabled)
                        } icon: {
                            Image(systemName: "arrowshape.turn.up.left")
                        }
                        .foregroundStyle(.secondary)
                    }
                }
            }
            .padding(Space.s4)
        }
        .background(tint.opacity(question.pending ? 0.06 : 0.04))
        .clipShape(RoundedRectangle(cornerRadius: Radius.card))
        .overlay(RoundedRectangle(cornerRadius: Radius.card).strokeBorder(tint.opacity(question.pending ? 0.45 : 0.2)))
    }

    @ViewBuilder private func itemView(_ item: QuestionItem, at i: Int) -> some View {
        let answer = question.answers.flatMap { $0.indices.contains(i) ? $0[i] : nil }
        VStack(alignment: .leading, spacing: Space.s3) {
            if !item.header.isEmpty || item.multi {
                HStack(spacing: Space.s2) {
                    if !item.header.isEmpty {
                        Text(item.header)
                            .font(TypeScale.micro)
                            .padding(.horizontal, Space.s2).padding(.vertical, 2)
                            .background(.quaternary, in: RoundedRectangle(cornerRadius: Radius.chip))
                    }
                    if item.multi { Text("Pick any").font(TypeScale.meta).foregroundStyle(.tertiary) }
                }
            }
            Text(item.question)
                .font(.system(size: 14, weight: .medium))
                .lineSpacing(TypeScale.bodyLeading)
                .textSelection(.enabled)
            if !item.options.isEmpty {
                VStack(spacing: Space.s1) {
                    ForEach(item.options, id: \.label) { option in
                        optionRow(option, item: item, at: i, chosen: chosen(option.label, answer: answer, item: i))
                    }
                }
            }
            if question.pending {
                TextField(item.options.isEmpty ? "Your answer…" : (item.multi ? "Something else too…" : "Or in your own words…"),
                          text: $draft.typed[i], axis: .vertical)
                    .textFieldStyle(.plain)
                    .font(TypeScale.body)
                    .lineLimit(1...6)
                    .padding(.horizontal, Space.s3)
                    .padding(.vertical, Space.s2)
                    .background(Palette.raised, in: RoundedRectangle(cornerRadius: Radius.chip + 2))
                    .overlay(RoundedRectangle(cornerRadius: Radius.chip + 2).strokeBorder(Palette.separator))
                    .onSubmit { advance() }
            } else if let answer, !item.options.contains(where: { answerParts(answer).contains($0.label) }) {
                Label(answer, systemImage: "text.bubble").font(TypeScale.callout).textSelection(.enabled)
            }
        }
    }

    private func optionRow(_ option: QuestionOption, item: QuestionItem, at i: Int, chosen: Bool) -> some View {
        let choice = Choice(option.label)
        return Button {
            draft.toggle(option.label, item: i)
        } label: {
            HStack(alignment: .top, spacing: Space.s3) {
                Image(systemName: item.multi ? (chosen ? "checkmark.square.fill" : "square") : (chosen ? "largecircle.fill.circle" : "circle"))
                    .font(.system(size: 15))
                    .foregroundStyle(chosen ? Color.accentColor : Color.secondary)
                    .padding(.top, 1)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: Space.s2) {
                        Text(choice.label).font(TypeScale.calloutMedium).foregroundStyle(.primary)
                        if choice.recommended { RecommendedTag() }
                    }
                    if !option.description.isEmpty {
                        Text(option.description).font(TypeScale.callout).foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, Space.s3)
            .padding(.vertical, Space.s2 + 1)
            .frame(minHeight: 36)
            .background(chosen ? Color.accentColor.opacity(0.1) : Palette.raised.opacity(0.6), in: RoundedRectangle(cornerRadius: Radius.chip + 2))
            .overlay(RoundedRectangle(cornerRadius: Radius.chip + 2).strokeBorder(chosen ? Color.accentColor.opacity(0.45) : Palette.separator.opacity(0.6)))
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

    /// Whether the part on screen has an answer, so Next can move on.
    private var pageAnswered: Bool {
        !draft.picked[page].isEmpty || !draft.typed[page].trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func advance() {
        if page < question.items.count - 1 && pageAnswered { page += 1 }
    }

    private var footer: some View {
        VStack(alignment: .leading, spacing: Space.s2) {
            HStack(spacing: Space.s2) {
                if let command = plan?.commandLine {
                    Text(command)
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer(minLength: Space.s2)
                if many {
                    Button("Back") { page -= 1 }
                        .disabled(page == 0)
                    if page < question.items.count - 1 {
                        Button("Next") { advance() }
                            .disabled(!pageAnswered)
                            .keyboardShortcut(.defaultAction)
                    }
                }
                if !many || page == question.items.count - 1 {
                    Button {
                        Task { await send() }
                    } label: {
                        if sending { ProgressView().controlSize(.small) } else { Text("Answer") }
                    }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(plan == nil || sending)
                }
            }
            .controlSize(.large)
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(TypeScale.meta)
                    .foregroundStyle(Palette.failure)
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

    init(_ text: String) {
        if let parsed = Self.parsed[text] {
            blocks = parsed
        } else {
            blocks = Self.parse(text)
            Self.parsed[text] = blocks
        }
    }

    /// Each text is parsed once: a step is drawn again on every read of its
    /// conversation, and its text never changes. Bounded, so a long-running
    /// app does not keep every text it ever showed.
    @MainActor private static var parsed = BoundedCache<String, [Block]>(limit: 2000)

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                switch block {
                case .heading(let level, let text):
                    inline(text).font(level <= 1 ? .system(size: 17, weight: .semibold) : TypeScale.title).padding(.top, Space.s2)
                case .bullet(let text, let ordered):
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(ordered ?? "•").foregroundStyle(.secondary).frame(minWidth: 12, alignment: .trailing)
                        inline(text)
                    }
                case .code(let text):
                    Text(text)
                        .font(TypeScale.code)
                        .lineSpacing(TypeScale.codeLeading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(Space.s3)
                        .background(Palette.sunken, in: RoundedRectangle(cornerRadius: Radius.card))
                case .paragraph(let text):
                    inline(text)
                }
            }
        }
        .readable()
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

/// A dictionary that forgets everything once it holds limit entries; enough for
/// a cache whose entries are cheap to make again.
struct BoundedCache<Key: Hashable, Value> {
    let limit: Int
    private var store: [Key: Value] = [:]

    init(limit: Int) { self.limit = limit }

    subscript(key: Key) -> Value? {
        get { store[key] }
        set {
            if store.count >= limit { store.removeAll(keepingCapacity: true) }
            store[key] = newValue
        }
    }
}
