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
                .font(.system(size: IconSize.turn))
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
            VStack(alignment: .trailing, spacing: Space.s1) {
                HStack(spacing: Space.s1 + 2) {
                    status
                    Text("\(label) · \(at)")
                }
                .font(TypeScale.caption)
                .foregroundStyle(Palette.text3)
                Text(text)
                    .readable()
                    .foregroundStyle(Palette.text)
                    .lineLimit(expanded || !folds ? nil : 8)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Palette.bubbleFill, in: RoundedRectangle(cornerRadius: Radius.bubble))
                    .overlay(RoundedRectangle(cornerRadius: Radius.bubble).strokeBorder(Palette.bubbleBorder))
                if folds {
                    Button(expanded ? "Show less" : "Show all") { expanded.toggle() }
                        .buttonStyle(.link)
                        .font(TypeScale.meta)
                }
            }
            .containerRelativeFrame(.horizontal, alignment: .trailing) { width, _ in width * 0.7 }
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
                    .font(TypeScale.codeSmall)
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
                    .font(TypeScale.codeSmall)
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
    /// Who asks, named before the title.
    var asker: String? = nil
    /// Where in the goal's tree it was asked, each level a way to its page when it has one.
    var path: [PathPart] = []
    /// When it was asked.
    var at: String? = nil
    /// What answering it with its first choice would change.
    var change: PendingChange? = nil
    let answered: () -> Void

    @State private var draft: AnswerDraft
    @State private var page = 0
    @State private var sending = false
    @State private var failure: String?

    init(store: Store, workID: String, question: Question, title: String? = nil, asker: String? = nil,
         path: [PathPart] = [], at: String? = nil, change: PendingChange? = nil, answered: @escaping () -> Void) {
        self.store = store
        self.workID = workID
        self.question = question
        self.title = title
        self.asker = asker
        self.path = path
        self.at = at
        self.change = change
        self.answered = answered
        _draft = State(initialValue: AnswerDraft(question))
    }

    private var many: Bool { question.items.count > 1 }

    var body: some View {
        if question.pending {
            VStack(alignment: .leading, spacing: 0) {
                header
                itemView(question.items[page], at: page)
                    .id(page)
                footer
            }
            .questionFrame()
        } else {
            VStack(alignment: .leading, spacing: Space.s3) {
                header
                ForEach(Array(question.items.enumerated()), id: \.offset) { i, item in
                    itemView(item, at: i)
                }
                if let reply = question.reply {
                    Text(reply).font(TypeScale.callout).foregroundStyle(Palette.text2).lineLimit(4).textSelection(.enabled)
                }
            }
            .canvasCard(fill: Palette.plainCard)
        }
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: Space.s2) {
            HStack(spacing: 0) {
                if let asker {
                    Text(asker + " ").fontWeight(.semibold).lineLimit(1).truncationMode(.tail).layoutPriority(-1)
                }
                Text(change?.heading ?? title ?? (question.pending ? "Needs you" : "Asked"))
                if !path.isEmpty && change?.kind != .course && change?.kind != .finish {
                    Text(" · in ")
                    ForEach(Array(path.enumerated()), id: \.offset) { i, part in
                        if i > 0 { Text(" › ") }
                        if let open = part.open {
                            Button(part.title, action: open).buttonStyle(.plain).foregroundStyle(Palette.link)
                        } else {
                            Text(part.title)
                        }
                    }
                }
            }
            .font(TypeScale.meta)
            .foregroundStyle(question.pending ? BadgeTone.attention.text : Palette.text3)
            .lineLimit(1)
            Spacer(minLength: Space.s2)
            if many && question.pending {
                Text("\(page + 1) of \(question.items.count)").font(TypeScale.caption).foregroundStyle(Palette.text3).monospacedDigit()
            }
            if let at {
                Text("\(change == nil ? "asked" : "proposed") \(agoWords(at))").font(TypeScale.caption).foregroundStyle(Palette.text3)
            }
        }
    }

    @ViewBuilder private func itemView(_ item: QuestionItem, at i: Int) -> some View {
        let answer = question.answers.flatMap { $0.indices.contains(i) ? $0[i] : nil }
        VStack(alignment: .leading, spacing: 0) {
            if !item.header.isEmpty || item.multi {
                HStack(spacing: Space.s2) {
                    if !item.header.isEmpty { Badge(item.header, .waiting) }
                    if item.multi { Text("Pick any").font(TypeScale.caption).foregroundStyle(Palette.text3) }
                }
                .padding(.top, 6)
            }
            Text(change == nil ? item.question : item.head)
                .font(change?.kind == .course ? TypeScale.title : TypeScale.bodyMedium)
                .lineSpacing(TypeScale.bodyLeading)
                .foregroundStyle(Palette.text)
                .textSelection(.enabled)
                .padding(.top, 6)
                .padding(.bottom, 8)
            if let change, i == 0 { ChangeBlock(change: change).padding(.bottom, 10) }
            VStack(spacing: 0) {
                ForEach(item.options, id: \.label) { option in
                    optionRow(option, item: item, at: i, chosen: chosen(option.label, answer: answer, item: i))
                }
                if question.pending {
                    HStack(spacing: 10) {
                        Text(draft.typed[i].isEmpty ? "○" : "◉").font(TypeScale.calloutMedium).foregroundStyle(Palette.text)
                        TextField(item.options.isEmpty ? "Your answer…" : "Something else…", text: $draft.typed[i], axis: .vertical)
                            .textFieldStyle(.plain)
                            .font(TypeScale.calloutMedium)
                            .lineLimit(1...6)
                            .onSubmit { advance() }
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .background(draft.typed[i].isEmpty ? Color.clear : Palette.page, in: RoundedRectangle(cornerRadius: Radius.option))
                    .overlay(RoundedRectangle(cornerRadius: Radius.option).strokeBorder(draft.typed[i].isEmpty ? Color.clear : Palette.questionBorder))
                } else if let answer, !item.options.contains(where: { answerParts(answer).contains(Choice($0.label).label) }) {
                    Text(answer).font(TypeScale.callout).foregroundStyle(Palette.text).textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 10).padding(.vertical, 7)
                }
            }
        }
    }

    private func optionRow(_ option: QuestionOption, item: QuestionItem, at i: Int, chosen: Bool) -> some View {
        let choice = Choice(option.label)
        return Button {
            draft.toggle(option.label, item: i)
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(item.multi ? (chosen ? "☑" : "☐") : (chosen ? "◉" : "○")).foregroundStyle(Palette.text)
                VStack(alignment: .leading, spacing: 2) {
                    Text(choice.label).foregroundStyle(Palette.text)
                    if !option.description.isEmpty {
                        Text(option.description).font(TypeScale.caption).foregroundStyle(Palette.text3)
                    }
                }
                Spacer(minLength: Space.s2)
                if choice.recommended { Badge("Recommended", .settled) }
            }
            .font(TypeScale.calloutMedium)
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .background(chosen ? Palette.page : Color.clear, in: RoundedRectangle(cornerRadius: Radius.option))
            .overlay(RoundedRectangle(cornerRadius: Radius.option).strokeBorder(chosen ? Palette.questionBorder : Color.clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!question.pending || sending)
    }

    private func chosen(_ label: String, answer: String?, item: Int) -> Bool {
        if question.pending { return draft.picked[item].contains(label) }
        return answer.map { answerParts($0).contains(Choice(label).label) } ?? false
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

    /// Parts with no answer yet, sent as no answer.
    private var unanswered: Int {
        question.items.indices.filter { draft.picked[$0].isEmpty && draft.typed[$0].trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }.count
    }

    private var footer: some View {
        VStack(alignment: .trailing, spacing: Space.s2) {
            HStack(spacing: Space.s2) {
                if many {
                    Button("Back") { page -= 1 }.buttonStyle(.canvas).disabled(page == 0)
                    Button("Next") { page += 1 }.buttonStyle(.canvas).disabled(page == question.items.count - 1)
                }
                Spacer(minLength: Space.s2)
                Button("Let the director decide") {
                    Task { await send(try? Plan.answer(workID, question, answers: draft.directorDecides)) }
                }
                .buttonStyle(.canvas)
                .disabled(sending)
                .help("Answers with the director's recommendation, or leaves it to its judgement")
                Button {
                    Task { await send(plan) }
                } label: {
                    if sending { ProgressView().controlSize(.small) } else { Text(many && unanswered > 0 ? "Answer (\(unanswered) left open)" : "Answer") }
                }
                .buttonStyle(.canvasPrimary)
                .keyboardShortcut(.defaultAction)
                .disabled(plan == nil || sending)
                .help(plan?.commandLine ?? "Pick an answer")
            }
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(TypeScale.meta)
                    .foregroundStyle(Palette.failure)
                    .textSelection(.enabled)
            }
        }
        .padding(.top, 6)
    }

    private func send(_ plan: Plan?) async {
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

/// What a change would do, inside its question card: a course change as the
/// goal now and as proposed, then the plan's changes in sentences.
struct ChangeBlock: View {
    let change: PendingChange

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let why = change.why, !why.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    CardLabel("Why change course", color: Palette.text2)
                    Text(why).font(TypeScale.callout).lineSpacing(TypeScale.calloutLeading).foregroundStyle(Palette.text)
                }
            }
            if !change.table.isEmpty {
                VStack(spacing: 0) {
                    row("", "Now", "Proposed", header: true)
                    ForEach(Array(change.table.enumerated()), id: \.offset) { _, r in
                        Rectangle().fill(Palette.inner).frame(height: 1)
                        row(r.label, r.now, r.proposed, header: false)
                    }
                }
                .background(Palette.page, in: RoundedRectangle(cornerRadius: Radius.card))
                .overlay(RoundedRectangle(cornerRadius: Radius.card).strokeBorder(Palette.questionBorder))
            }
            if let lines = change.lines, !lines.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    CardLabel(change.kind == .course ? "The plan after the change" : "What would change", color: Palette.text2)
                    ForEach(lines, id: \.self) { line in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text("•").foregroundStyle(Palette.text3)
                            Text(line).foregroundStyle(Palette.text)
                        }
                        .font(TypeScale.callout)
                    }
                }
            }
        }
    }

    private func row(_ label: String, _ now: String, _ proposed: String, header: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label).foregroundStyle(Palette.text3).frame(width: 108, alignment: .leading)
            Text(now).strikethrough(!header).foregroundStyle(header ? Palette.text2 : Palette.text2)
                .frame(maxWidth: .infinity, alignment: .leading)
            Text(proposed).foregroundStyle(header ? Palette.text2 : Palette.text)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(header ? TypeScale.meta : TypeScale.callout)
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }
}

/// One level of where something sits in a goal's tree, and the way to its page.
struct PathPart {
    let title: String
    var open: (() -> Void)? = nil
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
                    HStack(alignment: .firstTextBaseline, spacing: Space.s2) {
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
