import SwiftUI
import WorkDirectorKit

/// One goal or task, read again whenever the board is. A task leads with its
/// session's conversation; the ledger's record of it is one tab away. A goal
/// leads with whatever its tasks are waiting on you for, then the tasks, each
/// with what it is doing now.
struct WorkDetail: View {
    let store: Store
    let id: String
    @Binding var plan: Plan?
    /// Opens another work item in this pane: a goal's task, a task's goal.
    let open: (String) -> Void

    @State private var loaded: Loaded?
    @State private var failure: String?
    @State private var asking: Ask?
    /// A task's own session, as its page reads it.
    @State private var feed: Feed
    /// The agent whose conversation the centre shows instead of this page's
    /// own: a goal's task, or a subagent.
    @State private var focus: AgentCard?
    @State private var focusFeed: Feed?
    /// The gate this work waits on next, if any.
    @State private var gate: Gate?
    @AppStorage("detail.tab") private var tab: Tab = .conversation
    @AppStorage("detail.agents") private var showAgents = true

    init(store: Store, id: String, plan: Binding<Plan?>, open: @escaping (String) -> Void) {
        self.store = store
        self.id = id
        _plan = plan
        self.open = open
        _feed = State(initialValue: Feed(store: store, id: id))
    }

    enum Tab: String {
        case conversation, record
    }

    enum Loaded {
        case goal(GoalDetail)
        case work(Work, [Event])

        var work: Work {
            switch self {
            case .goal(let g): g.goal
            case .work(let w, _): w
            }
        }
    }

    var body: some View {
        Group {
            if let loaded {
                ScrollView {
                    VStack(alignment: .leading, spacing: 18) {
                        if let focus, let focusFeed, let task = focusedTask(focus, in: loaded) {
                            Breadcrumb(work: loaded.work, task: task, focus: focus) { self.focus = nil; self.focusFeed = nil }
                            ConversationView(store: store, work: task, feed: focusFeed)
                        } else {
                            Header(work: loaded.work, open: open)
                            page(loaded)
                        }
                    }
                    .padding(24)
                    .frame(maxWidth: 860, alignment: .leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .defaultScrollAnchor(following ? .bottom : .top)
                .defaultScrollAnchor(following ? .bottom : .top, for: .sizeChanges)
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        if let gate, let pass = Plan.passing(gate) {
                            Button { plan = pass } label: { Label(pass.title, systemImage: gateSymbol(gate.gate)) }
                                .labelStyle(.titleAndIcon)
                                .buttonStyle(.borderedProminent)
                                .help("\(gate.why ?? "") — \(pass.commandLine)")
                        }
                    }
                    Actions(work: loaded.work, run: { plan = $0 }, ask: { asking = $0 })
                    ToolbarItem(placement: .primaryAction) {
                        if let target = openTarget(loaded) {
                            Button { plan = Plan.open(target) } label: { Label("Open", systemImage: "arrow.up.forward.app") }
                                .help("Open where this work is done in your editor: wd open \(target) .")
                        }
                    }
                    ToolbarItem(placement: .primaryAction) {
                        Button { showAgents.toggle() } label: { Label("Agents", systemImage: "sidebar.right") }
                            .help(showAgents ? "Hide the agents" : "Show the agents")
                    }
                }
                .inspector(isPresented: $showAgents) {
                    AgentsPanel(roster: roster(loaded), focused: focus?.id ?? (loaded.work.kind.isGoal ? nil : loaded.work.id),
                                choose: { choose($0, in: loaded) }, stop: { plan = Plan.stop($0) },
                                menu: { card in AnyView(cardMenu(card, in: loaded)) })
                        .inspectorColumnWidth(min: 260, ideal: 340, max: 480)
                }
                .safeAreaInset(edge: .bottom) {
                    if let target = messaged(loaded), !target.atRest || (target.kind.isGoal && target.state == .done) {
                        MessageBar(store: store, work: target, working: working(loaded), about: about(target, in: loaded))
                            .id(target.id)
                    }
                }
            } else if let failure {
                ContentUnavailableView("Cannot read \(id)", systemImage: "exclamationmark.triangle", description: Text(failure))
            } else {
                ProgressView()
            }
        }
        .navigationTitle(loaded?.work.title ?? id)
        .navigationSubtitle(loaded.map { "\($0.work.project) · \($0.work.state.look.label)" } ?? "")
        .task(id: store.reads) { await load() }
        .task(id: id) {
            // What a goal's tasks are doing lives in their sessions, which tell
            // the ledger nothing; while any task works, the goal is read again.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(3))
                if case .goal(let g) = loaded, g.tasks.contains(where: { !$0.atRest }) {
                    await load()
                }
            }
        }
        .sheet(item: $asking) { ask in
            ReasonSheet(ask: ask) { made in
                asking = nil
                plan = made
            }
        }
    }

    @ViewBuilder
    private func page(_ loaded: Loaded) -> some View {
        switch loaded {
        case .goal(let goal):
            GoalThread(store: store, goal: goal, focus: { id in choose(id, in: loaded) }) { Task { await load() } }
        case .work(let work, let events):
            if hasSession(work) {
                Picker("Show", selection: $tab) {
                    Text("Conversation").tag(Tab.conversation)
                    Text("Record").tag(Tab.record)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .fixedSize()
            }
            if hasSession(work) && tab == .conversation {
                ConversationView(store: store, work: work, feed: feed)
            } else {
                EventsSection(events: events, expanded: true)
            }
        }
    }

    /// The agents on this work: a goal's tasks, or a task's own session, each
    /// with the subagents it started.
    private func roster(_ loaded: Loaded) -> AgentRoster {
        switch loaded {
        case .goal(let g):
            AgentRoster(tasks: g.tasks, activity: g.activity)
        case .work(let w, _):
            hasSession(w)
                ? AgentRoster(task: w, status: feed.status, asking: feed.asking != nil,
                              line: feed.steps.last.map(Self.line), usage: feed.usage, subagents: feed.subagents)
                : AgentRoster(tasks: [], activity: [:])
        }
    }

    private static func line(_ step: Step) -> String {
        switch step.kind {
        case .tool: [step.tool?.name, step.tool?.summary].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
        case .question: step.question?.items.first?.question ?? ""
        default: (step.text ?? "").split(separator: "\n").first.map(String.init) ?? ""
        }
    }

    /// Shows an agent's conversation in the centre: a goal's task, or a
    /// subagent. A task's own card is its page.
    private func choose(_ card: AgentCard, in loaded: Loaded) {
        if card.agent == nil && card.task == loaded.work.id {
            focus = nil
            focusFeed = nil
            return
        }
        focus = card
        focusFeed = Feed(store: store, id: card.task, agent: card.agent)
    }

    private func choose(_ task: String, in loaded: Loaded) {
        // A task with no session has no conversation to show; its page has its record.
        if let card = roster(loaded).cards.first(where: { $0.task == task }) {
            choose(card, in: loaded)
        } else {
            open(task)
        }
    }

    @ViewBuilder
    private func cardMenu(_ card: AgentCard, in loaded: Loaded) -> some View {
        if card.agent == nil, let task = task(card.task, in: loaded) {
            Divider()
            Button("Open Task Page") { open(task.id) }
            WorkMenu(work: task, run: { plan = $0 }, ask: { asking = $0 })
        }
    }

    private func task(_ id: String, in loaded: Loaded) -> Work? {
        switch loaded {
        case .goal(let g): g.tasks.first { $0.id == id }
        case .work(let w, _): w.id == id ? w : nil
        }
    }

    private func focusedTask(_ card: AgentCard, in loaded: Loaded) -> Work? { task(card.task, in: loaded) }

    /// Who a message goes to: the agent in focus's task, else this work.
    private func messaged(_ loaded: Loaded) -> Work? {
        if let focus { return task(focus.task, in: loaded) }
        return loaded.work
    }

    /// Whether a session the message bar speaks to has a process up to stop.
    private func working(_ loaded: Loaded) -> Bool {
        if let focus, let focusFeed { return focus.agent == nil ? focusFeed.status.live : false }
        switch loaded {
        case .goal(let g): return g.activity.values.contains { $0.status?.live == true }
        case .work: return feed.status.live
        }
    }

    /// Chips for what a message reaches: runner, model, the folder it works in.
    private func about(_ target: Work, in loaded: Loaded) -> [String] {
        var chips: [String] = []
        if let runner = target.runner { chips.append(runner) }
        let usage: Usage? = {
            if let focus, let focusFeed, focus.agent == nil { return focusFeed.usage }
            switch loaded {
            case .goal(let g): return g.activity[target.id]?.usage
            case .work: return feed.usage
            }
        }()
        if let model = usage?.model, !model.isEmpty { chips.append(model.replacingOccurrences(of: "claude-", with: "")) }
        if let cwd = target.cwd ?? workingDir(loaded) { chips.append((cwd as NSString).lastPathComponent) }
        return chips
    }

    private func workingDir(_ loaded: Loaded) -> String? {
        if case .goal(let g) = loaded { return g.tasks.compactMap(\.cwd).first }
        return nil
    }

    /// Opens where the work is done in the editor: the task in focus, this
    /// task, or a goal's first task with a session.
    private func openTarget(_ loaded: Loaded) -> String? {
        if let focus { return focus.task }
        switch loaded {
        case .goal(let g): return g.tasks.first { hasSession($0) }?.id
        case .work(let w, _): return hasSession(w) ? w.id : nil
        }
    }

    private func hasSession(_ work: Work) -> Bool { work.session != nil || work.claim != nil }

    /// A conversation keeps its newest step in view as it grows.
    private var following: Bool {
        if focus != nil { return true }
        if case .work(let w, _) = loaded { return hasSession(w) && tab == .conversation }
        return false
    }

    private func gateSymbol(_ gate: String) -> String {
        switch gate {
        case "verify": "checkmark.shield"
        case "land": "arrow.triangle.merge"
        case "done": "checkmark.circle"
        default: "flag.checkered"
        }
    }

    private func load() async {
        do {
            let work = try await store.work(id)
            gate = try await store.gate(of: id)
            if work.kind.isGoal {
                loaded = .goal(try await store.goal(id))
            } else {
                loaded = .work(work, try await store.events(of: id))
            }
            failure = nil
        } catch {
            failure = "\(error)"
        }
    }
}

/// Where the centre is: the work, the task in focus, and the subagent, each
/// a way back.
struct Breadcrumb: View {
    let work: Work
    let task: Work
    let focus: AgentCard
    let back: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Button(action: back) { Label(work.title, systemImage: "chevron.backward") }
                    .buttonStyle(.link)
                    .lineLimit(1)
                if focus.agent != nil {
                    Image(systemName: "chevron.forward").font(.caption2).foregroundStyle(.tertiary)
                    Text(task.title).lineLimit(1).foregroundStyle(.secondary)
                }
            }
            .font(.caption)
            Text(focus.title).font(.title3.weight(.semibold)).textSelection(.enabled)
            HStack(spacing: 8) {
                if focus.agent == nil { StatePill(state: task.state) }
                Text(focus.kind).font(.caption.monospaced()).foregroundStyle(.secondary)
                Text(focus.agent ?? task.id).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
        }
    }
}

struct Header: View {
    let work: Work
    let open: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let parent = work.parent {
                Button { open(parent) } label: {
                    Label("Goal \(parent)", systemImage: "chevron.backward")
                }
                .buttonStyle(.link)
                .font(.caption)
            }
            Text(work.title).font(.title2.weight(.semibold)).textSelection(.enabled)
            HStack(spacing: 8) {
                StatePill(state: work.state)
                if let type = work.goalType {
                    Text(type.rawValue).font(.caption).foregroundStyle(.secondary)
                }
                Text(work.project).font(.caption).foregroundStyle(.secondary)
                Text(work.id).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
            // A goal's request opens its thread; a task's detail is its brief.
            if !work.detail.isEmpty && !work.kind.isGoal {
                Text(work.detail).foregroundStyle(.secondary).textSelection(.enabled)
            }
        }
    }
}

/// The questions a goal's tasks are waiting on, lifted to the top of the goal
/// so they are answered without opening each task.
struct Waiting: View {
    let store: Store
    let goal: GoalDetail
    let open: (String) -> Void
    let answered: () -> Void

    var body: some View {
        let asking = goal.tasks.compactMap { t in goal.activity[t.id]?.asking.map { (t, $0) } }
        if !asking.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(asking, id: \.1.id) { task, question in
                    VStack(alignment: .leading, spacing: 4) {
                        Button { open(task.id) } label: {
                            Label(task.title, systemImage: "arrow.up.forward.square")
                        }
                        .buttonStyle(.link)
                        .font(.caption)
                        QuestionCard(store: store, workID: task.id, question: question, answered: answered)
                    }
                }
            }
        }
    }
}

struct SectionTitle: View {
    let title: String
    let detail: String

    init(_ title: String, detail: String) {
        self.title = title
        self.detail = detail
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title).font(.headline)
            Text(detail).font(.subheadline).foregroundStyle(.secondary)
        }
        .padding(.top, 6)
    }
}

struct ClaimRow: View {
    let claim: Claim

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            if let d = claim.event.decision {
                Text(d.question).font(.callout.weight(.medium))
                Text(d.answer).strikethrough(!claim.stands)
                HStack(spacing: 6) {
                    Text(d.source.isEmpty ? "director" : d.source)
                    if !d.model.isEmpty { Text("· \(d.model)") }
                    if d.tokens > 0 { Text("· \(d.tokens) tokens") }
                    Text("· \(claim.event.at)")
                }
                .font(.caption).foregroundStyle(.secondary)
            } else {
                Text(claim.event.body).strikethrough(!claim.stands).textSelection(.enabled)
                Text(claim.event.at).font(.caption).foregroundStyle(.secondary)
            }
            if !claim.stands {
                Label("Reversed: \(claim.reversed)", systemImage: "arrow.uturn.backward").font(.caption).foregroundStyle(.orange)
            }
        }
        .padding(.vertical, 2)
    }
}

struct LandingRow: View {
    let landed: Landed

    var body: some View {
        HStack {
            Image(systemName: landed.kind == .commit ? "arrow.triangle.merge" : landed.kind == .pullRequest ? "arrow.triangle.pull" : "questionmark.circle")
            if let url = URL(string: landed.url) {
                Link(landed.url, destination: url).lineLimit(1).truncationMode(.middle)
            } else {
                Text(landed.url)
            }
            Spacer()
            Text(kind).font(.caption).foregroundStyle(.secondary)
        }
    }

    private var kind: String {
        switch landed.kind {
        case .commit: "commit"
        case .pullRequest: "pull request"
        case .unknown: "unknown"
        }
    }
}

struct EventsSection: View {
    let events: [Event]
    @State var expanded: Bool

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(events.reversed()) { e in
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(e.at.prefix(19).replacingOccurrences(of: "T", with: " "))
                            .font(.caption.monospaced()).foregroundStyle(.secondary)
                            .frame(width: 140, alignment: .leading)
                        Text(e.kind).font(.caption.weight(.medium)).frame(width: 70, alignment: .leading)
                        Text(e.body).font(.callout).textSelection(.enabled)
                    }
                }
            }
            .padding(.top, 6)
        } label: {
            SectionTitle("Events", detail: "\(events.count)")
        }
    }
}

/// Typing to work while it runs, as in a conversation. A goal passes the
/// message to every open task and keeps it as a decision; a session that is
/// busy reads it when it is ready. While something works it can be stopped, and
/// whatever it had not read yet comes back here to be said again.
struct MessageBar: View {
    let store: Store
    let work: Work
    /// Whether a session under this work is working now.
    let working: Bool
    var about: [String] = []
    @State private var text = ""
    @State private var sending = false
    @State private var outcome: String?
    @State private var failed = false

    var body: some View {
        InputBar(
            text: $text,
            placeholder: continuing ? "Continue this goal… say what is being worked on; it is planned and started again"
                : work.kind.isGoal ? "Tell this goal… every task gets it, and it is kept as a decision" : "Message this task… it reads it when it is ready",
            command: plan?.commandLine,
            busy: sending,
            note: outcome,
            noteIsError: failed,
            stop: working && !continuing ? Plan.stop(work.id) : nil,
            about: about,
            stopped: { Task { await stop() } }
        ) { Task { await send() } }
    }

    /// A finished goal is picked up again rather than told: continuing it
    /// reopens it with this text as the reason.
    private var continuing: Bool { work.kind.isGoal && work.state == .done }

    private var plan: Plan? {
        continuing ? try? Plan.continueGoal(work.id, text: text) : try? Plan.send(work.id, text: text)
    }

    private func send() async {
        guard let plan else { return }
        sending = true
        defer { sending = false }
        do {
            if continuing {
                _ = try await store.start(plan)
                failed = false
                outcome = "continued: planned and started again"
                text = ""
                return
            }
            let result = try await store.run(plan)
            failed = result.code != 0
            outcome = failed ? (result.stderr.isEmpty ? result.stdout : result.stderr) : result.stdout
            if !failed { text = "" }
        } catch {
            failed = true
            outcome = String(describing: error)
        }
        await store.refresh()
    }

    private func stop() async {
        sending = true
        defer { sending = false }
        do {
            let back = try await store.stop(Plan.stop(work.id))
            failed = false
            if back.isEmpty {
                outcome = "stopped: your next message continues it"
            } else {
                text = text.isEmpty ? back : back + "\n\n" + text
                outcome = "stopped: what it had not read is back here to edit and send"
            }
        } catch {
            failed = true
            outcome = String(describing: error)
        }
    }
}

/// The one place a person types to Work Director: a new goal and a running one
/// read the same. The command it will run shows under it, so sending is the
/// confirmation. The key that sends is the person's choice, in Settings.
struct InputBar: View {
    @Binding var text: String
    let placeholder: String
    let command: String?
    let busy: Bool
    var note: String? = nil
    var noteIsError = false
    /// What stopping runs, while there is something to stop.
    var stop: Plan? = nil
    /// What the message goes to: runner, model, where it works.
    var about: [String] = []
    var stopped: () -> Void = {}
    let submit: () -> Void
    @AppStorage("sendKey") private var sendKey: SendKey = .standard
    @State private var height = MessageField.font.boundingRectForFont.height
    @State private var taps = EscapeTaps()
    @State private var escapeMonitor: Any?
    @State private var escapeStops = EscapeTarget()
    /// Shown after one Escape, while a second one would stop.
    @State private var armed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let note {
                Label(note, systemImage: noteIsError ? "exclamationmark.triangle.fill" : "paperplane")
                    .font(.caption)
                    .foregroundStyle(noteIsError ? .red : .secondary)
                    .lineLimit(3)
                    .textSelection(.enabled)
            }
            HStack(alignment: .bottom, spacing: 8) {
                MessageField(
                    text: $text,
                    placeholder: placeholder,
                    sendKey: sendKey,
                    enabled: !busy,
                    height: $height,
                    send: { if command != nil && !busy { submit() } }
                )
                .frame(height: height)
                if busy {
                    ProgressView().controlSize(.small)
                } else {
                    if let stop {
                        Button(action: stopped) {
                            Image(systemName: "stop.circle.fill").font(.title2)
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(.red)
                        .help("Stop (Esc Esc): \(stop.commandLine)\nWhat it has not read yet comes back here to edit")
                    }
                    Button(action: submit) {
                        Image(systemName: "arrow.up.circle.fill").font(.title2)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(command == nil ? Color.secondary : Color.accentColor)
                    .disabled(command == nil)
                    .help(command.map { "\(sendKey.symbol) \($0)" } ?? "Type what you want")
                }
            }
            .padding(10)
            .background(.background, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(.quaternary))
            HStack(spacing: 8) {
                ForEach(about, id: \.self) { chip in
                    Text(chip)
                        .font(.caption2)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(.quaternary.opacity(0.6), in: Capsule())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                if let command {
                    Text(command)
                        .font(.caption2.monospaced())
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer(minLength: 0)
                Text(armed ? "Press Esc again to stop" : sendKey.title + (stop == nil ? "" : " · Esc Esc stops"))
                    .font(.caption2)
                    .foregroundStyle(armed ? AnyShapeStyle(.red) : AnyShapeStyle(.tertiary))
                    .fixedSize()
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(.bar)
        .onAppear { watchEscape() }
        .onChange(of: stop != nil && !busy, initial: true) { _, can in
            escapeStops.stop = can ? stopped : nil
        }
        .onDisappear {
            if let escapeMonitor { NSEvent.removeMonitor(escapeMonitor) }
            escapeMonitor = nil
        }
    }

    /// Escape anywhere in the window counts, not only in the text, so work can
    /// be stopped while reading its conversation. A sheet's Escape is its own.
    private func watchEscape() {
        guard escapeMonitor == nil else { return }
        escapeMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            guard event.keyCode == 53, event.window?.attachedSheet == nil, event.window?.isSheet == false,
                  let stop = escapeStops.stop else { return event }
            if taps.tap() {
                armed = false
                stop()
            } else {
                armed = true
                Task {
                    try? await Task.sleep(for: .seconds(EscapeTaps.window))
                    armed = false
                }
            }
            return nil
        }
    }
}

/// What a double Escape stops right now. The key monitor outlives the view
/// value that installed it, so it reads this rather than that value.
@MainActor
final class EscapeTarget {
    var stop: (() -> Void)?
}
