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
    /// Steps the person folded or unfolded.
    @State private var toggled: Set<String> = []

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s4) {
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
            // Everything said is one text to select across; only a question
            // waiting on the person breaks it.
            ForEach(pieces) { piece in
                switch piece {
                case .text(let transcript):
                    TranscriptText(transcript: transcript, key: "\(work.id)/\(feed.agent ?? "")/\(piece.id)") { id in
                        toggled.formSymmetricDifference([id])
                    }
                case .asking(_, let question):
                    QuestionCard(store: store, workID: work.id, question: question, product: work.project, answered: { Task { await feed.read() } })
                        .padding(.leading, Space.turnIndent)
                }
            }
            if feed.agent == nil {
                ForEach(store.outbox.filter { $0.work == work.id && !$0.arrived(in: feed.steps) }) { message in
                    OutgoingBubble(store: store, message: message, waiting: feed.status == .running ? "queued: read once it is ready" : "sent")
                }
            }
            WorkingIndicator(rows: WorkingRows(task: work, status: feed.status, asking: feed.asking != nil, last: feed.steps.last,
                                               usage: feed.usage, inFlight: feed.agent == nil ? store.inFlight : []))
                .padding(.leading, Space.turnIndent)
        }
        .task(id: ObjectIdentifier(feed)) { await feed.follow() }
    }

    private var pieces: [TranscriptPiece] {
        let answered = Set(feed.steps.indices.filter { i in
            feed.steps[i].kind == .prompt && directorAnswers.contains { (feed.steps[i].text ?? "").contains($0) }
        })
        return Transcript.pieces(feed.steps, speaker: speaker, running: feed.status == .running, directorAnswered: answered,
                                 toggled: toggled, time: when)
    }

    private var speaker: String {
        feed.agent.map { "Subagent \($0)" } ?? work.runner.map { $0.capitalized } ?? "Agent"
    }
}


/// What the person said, set apart from everything else said on the work so
/// it is found again scrolling back: on the right, in the accent colour.
struct YouBubble<Status: View>: View {
    let text: String
    let at: String
    var label = "You"
    /// The files sent with it.
    var files: [Attachment] = []
    @ViewBuilder var status: Status
    @State private var expanded = false

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            // A gap on the left keeps it on the right; it wraps within the
            // column it is given, never past it.
            Spacer(minLength: 96)
            VStack(alignment: .trailing, spacing: Space.s1) {
                HStack(spacing: Space.s1 + 2) {
                    status
                    Text("\(label) · \(at)")
                }
                .font(TypeScale.caption)
                .foregroundStyle(Palette.text3)
                if !text.isEmpty || files.isEmpty {
                    Text(text)
                        .readable()
                        .foregroundStyle(Palette.text)
                        .lineLimit(expanded || !folds ? nil : 8)
                        .textSelection(.enabled)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 10)
                        .background(Palette.bubbleFill, in: RoundedRectangle(cornerRadius: Radius.bubble))
                        .overlay(RoundedRectangle(cornerRadius: Radius.bubble).strokeBorder(Palette.bubbleBorder))
                        .copyable(text)
                }
                if !files.isEmpty { SentFiles(files: files) }
                if folds {
                    Button(expanded ? "Show less" : "Show all") { expanded.toggle() }
                        .buttonStyle(.link)
                        .font(TypeScale.meta)
                }
            }
        }
    }

    private var folds: Bool { text.count > 800 || text.split(separator: "\n").count > 8 }
}

extension YouBubble where Status == EmptyView {
    init(text: String, at: String, label: String = "You", files: [Attachment] = []) {
        self.init(text: text, at: at, label: label, files: files) { EmptyView() }
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
            if !message.files.isEmpty {
                Label(message.files.map { ($0 as NSString).lastPathComponent }.joined(separator: ", "), systemImage: "paperclip")
                    .font(TypeScale.meta)
                    .foregroundStyle(Palette.text3)
                    .lineLimit(2)
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
    /// The product the work is in, named where a finish choice can be remembered.
    var product: String? = nil
    let answered: () -> Void

    @State private var draft: AnswerDraft
    @State private var page = 0
    @State private var sending = false
    @State private var failure: String?
    @State private var editing = false
    @State private var remember = false
    /// What the director did when the question was left to it.
    @State private var outcome: DirectorOutcome?

    init(store: Store, workID: String, question: Question, title: String? = nil, asker: String? = nil,
         path: [PathPart] = [], at: String? = nil, change: PendingChange? = nil, product: String? = nil, answered: @escaping () -> Void) {
        self.store = store
        self.workID = workID
        self.question = question
        self.title = title
        self.asker = asker
        self.path = path
        self.at = at
        self.change = change
        self.product = product
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
                if let outcome, !outcome.declined.isEmpty {
                    VStack(alignment: .leading, spacing: Space.s2) {
                        Text("The director left this to you").font(TypeScale.meta).foregroundStyle(Palette.text2)
                        ForEach(Array(outcome.declined.enumerated()), id: \.offset) { _, choice in DeclinedNote(choice: choice) }
                    }
                    .padding(.top, 8)
                }
                footer
            }
            .questionFrame()
            .copyable(question.items.map(\.question).joined(separator: "\n\n"))
        } else {
            VStack(alignment: .leading, spacing: Space.s3) {
                header
                ForEach(Array(question.items.enumerated()), id: \.offset) { i, item in
                    itemView(item, at: i)
                }
                if let reply = question.reply {
                    Text(reply).font(TypeScale.callout).foregroundStyle(Palette.text2).lineLimit(4).textSelection(.enabled)
                }
                if let outcome, outcome.answered {
                    ForEach(Array(outcome.decided.enumerated()), id: \.offset) { _, choice in DirectorChoiceView(choice: choice) }
                }
            }
            .canvasCard(fill: Palette.plainCard)
            .copyable(question.items.map(\.question).joined(separator: "\n\n"))
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
            if !courseButtons {
                VStack(spacing: 0) {
                    ForEach(item.options, id: \.label) { option in
                        optionRow(option, item: item, at: i, chosen: chosen(option.label, answer: answer, item: i))
                    }
                    if question.pending {
                        HStack(spacing: 10) {
                            Text(draft.typed[i].isEmpty ? "○" : "◉").font(TypeScale.calloutMedium).foregroundStyle(Palette.text)
                            TextField(item.options.isEmpty ? "Your answer…" : "Something else…", text: $draft.typed[i], axis: .vertical)
                                .accessibilityLabel(item.options.isEmpty ? "Your answer" : ControlName.somethingElse)
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
            if i == 0, question.pending, let rows = change?.sources?.rows, !rows.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Rectangle().fill(Palette.questionBorder).frame(height: 1).padding(.vertical, 6)
                    CardLabel("Why I'm asking: these disagree", color: Palette.text2)
                    ForEach(rows, id: \.label) { row in
                        HStack(alignment: .firstTextBaseline, spacing: Space.s3) {
                            Text(row.label).foregroundStyle(Palette.text3).frame(width: 108, alignment: .leading)
                            Text(row.said).foregroundStyle(Palette.text).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                        }
                        .font(TypeScale.caption)
                    }
                }
            }
        }
    }

    /// A pending finish-line question of one part can keep its choice as the product's setting.
    private var remembers: Bool {
        question.pending && question.items.count == 1 && (change?.kind == .finish || question.items[0].asksFinish)
    }

    /// A pending course change is answered by its three buttons, not by picking.
    private var courseButtons: Bool {
        question.pending && change?.kind == .course && question.changeChoices != nil
    }

    private func optionRow(_ option: QuestionOption, item: QuestionItem, at i: Int, chosen: Bool) -> some View {
        let choice = Choice(option.label)
        let finish = change?.kind == .finish || item.asksFinish
        let meaning = finish ? FinishMeaning.of(choice.label) : nil
        return Button {
            draft.toggle(option.label, item: i)
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(item.multi ? (chosen ? "☑" : "☐") : (chosen ? "◉" : "○")).foregroundStyle(Palette.text)
                VStack(alignment: .leading, spacing: 2) {
                    Text(meaning?.title ?? choice.label).foregroundStyle(Palette.text)
                    if let description = meaning?.meaning ?? (option.description.isEmpty ? nil : option.description) {
                        Text(description).font(TypeScale.caption).fontWeight(.regular).foregroundStyle(Palette.text2)
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
        .accessibilityLabel(ControlName.option(option.label, finish: finish))
        .accessibilityHint(meaning?.meaning ?? option.description)
        .accessibilityAddTraits(item.multi ? (chosen ? [.isToggle, .isSelected] : .isToggle) : (chosen ? .isSelected : []))
    }

    private func chosen(_ label: String, answer: String?, item: Int) -> Bool {
        if question.pending { return draft.picked[item].contains(label) }
        return answer.map { answerParts($0).contains(Choice(label).label) } ?? false
    }

    private func answerParts(_ answer: String) -> [String] {
        answer.components(separatedBy: ", ")
    }

    private var plan: Plan? {
        draft.answers.flatMap { try? Plan.answer(workID, question, answers: $0, remember: remembers && remember) }
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
            if courseButtons, let choices = question.changeChoices {
                HStack(spacing: Space.s2) {
                    Spacer(minLength: Space.s2)
                    Button(choices.keep) { Task { await send([try? Plan.answer(workID, question, answers: [choices.keep])]) } }
                        .buttonStyle(.canvas)
                        .accessibilityLabel(choices.keep)
                    Button(ControlName.editChange) { editing = true }
                        .buttonStyle(.canvas)
                        .disabled(change?.proposed == nil)
                        .accessibilityLabel(ControlName.editChange)
                    Button {
                        Task { await send([try? Plan.answer(workID, question, answers: [choices.change])]) }
                    } label: {
                        if sending { ProgressView().controlSize(.small) } else { Text(ControlName.recommended(choices.change)) }
                    }
                    .buttonStyle(.canvasPrimary)
                    .accessibilityLabel(ControlName.recommended(choices.change))
                    .keyboardShortcut(.defaultAction)
                }
                .disabled(sending)
            } else {
                HStack(spacing: Space.s2) {
                    if many {
                        Button("Back") { page -= 1 }.buttonStyle(.canvas).disabled(page == 0).accessibilityLabel("Back")
                        Button("Next") { page += 1 }.buttonStyle(.canvas).disabled(page == question.items.count - 1).accessibilityLabel("Next")
                    }
                    if remembers {
                        Toggle(FinishMeaning.remember(in: product ?? "this product"), isOn: $remember)
                            .toggleStyle(.checkbox)
                            .accessibilityLabel(FinishMeaning.remember(in: product ?? "this product"))
                            .font(TypeScale.callout)
                            .foregroundStyle(Palette.text)
                    }
                    Spacer(minLength: Space.s2)
                    Button(ControlName.directorDecides) {
                        Task { await decide() }
                    }
                    .buttonStyle(.canvas)
                    .accessibilityLabel(ControlName.directorDecides)
                    .disabled(sending)
                    .help(Plan.directorDecides(workID).commandLine)
                    Button {
                        Task { await send(plan) }
                    } label: {
                        if sending { ProgressView().controlSize(.small) } else { Text(many && unanswered > 0 ? "\(ControlName.answer) (\(unanswered) left open)" : ControlName.answer) }
                    }
                    .buttonStyle(.canvasPrimary)
                    .keyboardShortcut(.defaultAction)
                    .disabled(plan == nil || sending)
                    .help(plan?.commandLine ?? "Pick an answer")
                    .accessibilityLabel(many && unanswered > 0 ? "\(ControlName.answer) (\(unanswered) left open)" : ControlName.answer)
                }
            }
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(TypeScale.meta)
                    .foregroundStyle(Palette.failure)
                    .textSelection(.enabled)
            }
        }
        .padding(.top, 6)
        .sheet(isPresented: $editing) {
            if let proposed = change?.proposed {
                EditChangeSheet(workID: workID, question: question, proposed: proposed) { plans in
                    editing = false
                    if let plans { Task { await send(plans) } }
                }
            }
        }
    }

    private func send(_ plan: Plan?) async {
        await send([plan])
    }

    /// Leaves the question to the director: it answers every part, or none
    /// and says why for each it declined.
    private func decide() async {
        sending = true
        defer { sending = false }
        failure = nil
        do {
            let result = try await store.run(Plan.directorDecides(workID))
            if result.code != 0 {
                failure = result.stderr.isEmpty ? result.stdout : result.stderr
            } else {
                let read = try DirectorOutcome.read(result.stdout)
                outcome = read
                if read.answered { answered() }
            }
        } catch {
            failure = "\(error)"
        }
        await store.refresh()
    }

    /// Runs writes in order, stopping at the first refused; a nil write sends nothing.
    private func send(_ plans: [Plan?]) async {
        guard !plans.isEmpty, !plans.contains(where: { $0 == nil }) else { return }
        sending = true
        defer { sending = false }
        failure = nil
        for case let plan? in plans {
            do {
                let result = try await store.run(plan)
                if result.code != 0 {
                    failure = result.stderr.isEmpty ? result.stdout : result.stderr
                    break
                }
            } catch {
                failure = "\(error)"
                break
            }
        }
        if failure == nil { answered() }
        await store.refresh()
    }
}

/// What a change would do, inside its question card: a course change as the
/// goal now and as proposed, then the plan's changes in sentences.
struct ChangeBlock: View {
    let change: PendingChange

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            let found = change.found.flatMap { $0.isEmpty ? nil : $0 }
            let why = change.why.flatMap { $0.isEmpty ? nil : $0 }
            if found != nil || why != nil {
                HStack(alignment: .top, spacing: Space.s5) {
                    if let found { said("What was found", found) }
                    if let why { said(change.kind == .course ? "Why change course" : "Why change the plan", why) }
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
            if change.kind == .course, let fates = change.subGoals, !fates.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    CardLabel("Sub-goals after the change", color: Palette.text2)
                    ForEach(Array(fates.enumerated()), id: \.offset) { _, fate in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Badge(fate.badge)
                            Text(fate.title)
                                .strikethrough(fate.status == .superseded)
                                .foregroundStyle(fate.status == .superseded ? Palette.text2 : Palette.text)
                            if fate.status == .superseded, let why = fate.why {
                                Text(why).font(TypeScale.caption).foregroundStyle(Palette.text3)
                            }
                        }
                        .font(TypeScale.callout)
                    }
                }
            } else if let lines = change.lines, !lines.isEmpty {
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

    private func said(_ label: String, _ text: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            CardLabel(label, color: Palette.text2)
            Text(text).font(TypeScale.callout).lineSpacing(TypeScale.calloutLeading).foregroundStyle(Palette.text)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
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

/// A course change as the person would make it instead: its proposed title,
/// done-when and finish line to edit, applied after the change is.
struct EditChangeSheet: View {
    let workID: String
    let question: Question
    let proposed: GoalShape
    let done: ([Plan]?) -> Void
    @State private var title: String
    @State private var doneWhen: String
    @State private var finish: String

    init(workID: String, question: Question, proposed: GoalShape, done: @escaping ([Plan]?) -> Void) {
        self.workID = workID
        self.question = question
        self.proposed = proposed
        self.done = done
        _title = State(initialValue: proposed.title)
        _doneWhen = State(initialValue: proposed.doneWhen ?? "")
        _finish = State(initialValue: FinishMeaning.lines.first { $0.lowercased() == proposed.finish?.lowercased() } ?? "complete PR")
    }

    private var plans: [Plan]? {
        try? Plan.editChange(workID, question, proposed: proposed, title: title, doneWhen: doneWhen, finish: finish)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            Text("Edit the change").font(TypeScale.headline)
            TextField("The goal", text: $title)
            TextField("Done when…", text: $doneWhen, axis: .vertical).lineLimit(2...4)
            Picker("Finishes at", selection: $finish) {
                ForEach(FinishMeaning.lines, id: \.self) { Text(FinishMeaning.of($0)?.title ?? $0).tag($0) }
            }
            if let plans {
                Text(plans.map(\.commandLine).joined(separator: "\n")).font(TypeScale.codeTiny).foregroundStyle(Palette.text3).lineLimit(4)
            }
            HStack {
                Spacer()
                Button("Cancel") { done(nil) }.keyboardShortcut(.cancelAction)
                Button("Change course") { done(plans) }.keyboardShortcut(.defaultAction).disabled(plans == nil)
            }
        }
        .textFieldStyle(.roundedBorder)
        .padding(Space.s5)
        .frame(width: 500)
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

/// Markdown as an executor writes it: headings, lists, quotes, code, rules,
/// tables and paragraphs, each with its inline emphasis, code and links, as
/// one native text to select and copy.
struct MarkdownText: View {
    let text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        TranscriptText(transcript: .reply(text))
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
