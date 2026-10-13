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
    /// Told the open goal's breadcrumb each time it is read.
    var chained: ([ChainLink]) -> Void = { _ in }
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
    @AppStorage("goal.view") private var goalView: GoalView = .overview
    @State private var filter: ThreadFilter = .all
    /// Who a message from a goal's page goes to: the goal, or one sub-goal.
    @State private var sendTo: String?
    /// Whether the reader is at the bottom of the conversation, where it
    /// follows what comes in.
    @State private var atBottom = true
    private static let bottom = "bottom"
    @AppStorage("detail.agents") private var showAgents = true
    @AppStorage("detail.agentsWidth") private var agentsWidth = PanelWidth.standard

    init(store: Store, id: String, plan: Binding<Plan?>, chained: @escaping ([ChainLink]) -> Void = { _ in }, open: @escaping (String) -> Void) {
        self.store = store
        self.id = id
        _plan = plan
        self.chained = chained
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
                ScrollViewReader { proxy in
                // The agents panel is a column of its own: the page narrows
                // beside it rather than running under it.
                HStack(spacing: 0) {
                // The bar sits under the scroll view, not over it: whatever is
                // last in the conversation ends above the bar however tall the
                // bar grows.
                VStack(spacing: 0) {
                if focus == nil {
                    pageTop(loaded)
                }
                ScrollView {
                    VStack(alignment: .leading, spacing: Space.s5) {
                        if let focus, let focusFeed, let task = focusedTask(focus, in: loaded) {
                            Breadcrumb(work: loaded.work, task: task, focus: focus) { self.focus = nil; self.focusFeed = nil }
                            ConversationView(store: store, work: task, feed: focusFeed, directorAnswers: directorAnswers(task.id, in: loaded))
                        } else {
                            page(loaded)
                        }
                    }
                    .textSelection(.enabled)
                    .readingColumn()
                    Color.clear.frame(height: 1).id(Self.bottom)
                }
                .defaultScrollAnchor(following ? .bottom : .top)
                // New content keeps the newest in view only while the reader
                // is at the bottom; scrolled up to read, it stays put.
                .defaultScrollAnchor(following && atBottom ? .bottom : .top, for: .sizeChanges)
                .onScrollGeometryChange(for: Bool.self) { g in
                    Follow.atBottom(offset: g.contentOffset.y, visible: g.containerSize.height, content: g.contentSize.height)
                } action: { _, now in
                    atBottom = now
                }
                .onChange(of: store.outbox.count) { was, now in
                    // What you just sent is where you look next.
                    if now > was { toBottom(proxy) }
                }
                .overlay(alignment: .bottomTrailing) {
                    if following && !atBottom {
                        Button { toBottom(proxy) } label: {
                            Image(systemName: "arrow.down")
                                .font(.system(size: IconSize.toolbar, weight: .semibold))
                                .frame(width: 34, height: 34)
                                .background(.regularMaterial, in: Circle())
                                .overlay(Circle().strokeBorder(.separator))
                                .shadow(color: .black.opacity(0.12), radius: 6, y: 2)
                        }
                        .buttonStyle(.plain)
                        .padding(.trailing, Space.s6)
                        .padding(.bottom, Space.s3)
                        .help("Jump to the latest")
                        .transition(.opacity.combined(with: .scale(scale: 0.8)))
                    }
                }
                .animation(.easeOut(duration: 0.15), value: atBottom)
                if let target = messaged(loaded), !target.atRest || (target.kind.isGoal && target.state == .done), composes(loaded) {
                    MessageBar(store: store, work: target, working: working(loaded), about: about(target, in: loaded),
                               addressees: focus == nil ? goalDetail(loaded)?.addressees ?? [] : [], sendTo: $sendTo)
                        .id(target.id)
                }
                }
                .frame(minWidth: 360)
                if showAgents {
                    PanelEdge(width: $agentsWidth)
                    AgentsPanel(roster: roster(loaded), focused: focus?.id ?? (loaded.work.kind.isGoal ? nil : loaded.work.id),
                                choose: { choose($0, in: loaded) }, stop: { plan = Plan.stop($0) },
                                menu: { card in AnyView(cardMenu(card, in: loaded)) })
                        .frame(width: PanelWidth.clamped(agentsWidth))
                        .frame(maxHeight: .infinity)
                        .background(Palette.sidebar)
                        .transition(.move(edge: .trailing))
                }
                }
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        if let gate, let pass = Plan.passing(gate) {
                            Button { plan = pass } label: { Label(pass.title, systemImage: gateSymbol(gate.gate)) }
                                .labelStyle(.titleAndIcon)
                                .buttonStyle(.borderedProminent)
                                .help("\(gate.why ?? "") — \(pass.commandLine)")
                        }
                    }
                    GroupGap()
                    Actions(work: loaded.work, run: { plan = $0 }, ask: { asking = $0 })
                    GroupGap()
                    ToolbarItemGroup(placement: .primaryAction) {
                        if focus == nil, goalView == .conversation, case .goal(let goal) = loaded, !goal.yours.isEmpty {
                            YoursMenu(yours: goal.yours) { id in
                                filter = .all
                                Task {
                                    try? await Task.sleep(for: .milliseconds(150))
                                    withAnimation { proxy.scrollTo(id, anchor: .top) }
                                }
                            }
                        }
                        if let target = openTarget(loaded) {
                            Button { plan = Plan.open(target) } label: { Label("Open in Editor", systemImage: "arrow.up.forward.app") }
                                .labelStyle(.titleAndIcon)
                                .help("Open where this work is done in your editor: wd open \(target) .")
                        }
                    }
                    GroupGap()
                    ToolbarItemGroup(placement: .primaryAction) {
                        Button { NotificationCenter.default.post(name: .openCommandBar, object: nil) } label: { Label("Command", systemImage: "command") }
                            .labelStyle(.iconOnly)
                            .help("Run any wd command (⌘K)")
                        Button { withAnimation(.easeOut(duration: 0.2)) { showAgents.toggle() } } label: { Label("Agents", systemImage: "sidebar.right") }
                            .labelStyle(.iconOnly)
                            .keyboardShortcut("0", modifiers: [.command, .option])
                            .help(showAgents ? "Hide the agents (⌥⌘0)" : "Show the agents (⌥⌘0)")
                    }
                }
                }
            } else if let failure {
                ContentUnavailableView("Cannot read \(id)", systemImage: "exclamationmark.triangle", description: Text(failure))
            } else {
                ProgressView()
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Palette.page)
        .navigationTitle(loaded?.work.title ?? id)
        .navigationSubtitle(loaded.map { "\($0.work.project) · \($0.work.state.look.label)" } ?? "")
        .task(id: store.reads) { await load() }
        .task(id: id) {
            // What a goal's tasks are doing lives in their sessions, which tell
            // the ledger nothing; while any task works, the goal is read again.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(3))
                if case .goal(let g) = loaded, !g.goal.atRest || g.tasks.contains(where: { !$0.atRest }) {
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
            let focusTask = { (id: String) in choose(id, in: loaded) }
            switch goalView {
            case .overview:
                GoalOverview(store: store, goal: goal, open: open, focus: focusTask,
                             answered: { Task { await load() } }, run: { plan = $0 })
            case .conversation:
                GoalThread(store: store, goal: goal, focus: focusTask, answered: { Task { await load() } }, filter: filter)
            case .record:
                GoalRecordView(store: store, goal: goal)
            }
        case .work(let work, let events):
            if hasSession(work) && tab == .conversation {
                ConversationView(store: store, work: work, feed: feed, directorAnswers: directorAnswers(work.id, in: loaded))
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
            AgentRoster(goal: g.goal, supervised: g.supervised, tasks: g.tasks, activity: g.activity)
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
        if card.agent == nil && card.task == loaded.work.id && !loaded.work.kind.isGoal {
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
        case .goal(let g): g.goal.id == id ? g.goal : g.tasks.first { $0.id == id }
        case .work(let w, _): w.id == id ? w : nil
        }
    }

    /// The page's fixed top: its name and where it stands, and the views it
    /// is read in. It stays while the view under it scrolls.
    @ViewBuilder private func pageTop(_ loaded: Loaded) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Header(work: loaded.work, goal: goalDetail(loaded), open: open,
                   rename: { title in plan = try? Plan.rename(loaded.work.id, title: title) })
            switch loaded {
            case .goal(let g):
                PageTabs(tabs: GoalView.allCases, selection: $goalView, label: \.label)
                if goalView == .conversation {
                    FilterChips(filter: $filter, goal: g).padding(.top, -2)
                }
            case .work(let w, _):
                if hasSession(w) {
                    PageTabs(tabs: [Tab.conversation, Tab.record], selection: $tab,
                             label: { $0 == .conversation ? "Conversation" : "Record" })
                }
            }
        }
        .padding(.horizontal, Space.gutter)
        .padding(.top, Space.section)
        .padding(.bottom, Space.s3)
        .frame(maxWidth: .infinity, alignment: .leading)
        Rectangle().fill(Palette.hairline).frame(height: 1)
    }

    private func goalDetail(_ loaded: Loaded) -> GoalDetail? {
        if case .goal(let g) = loaded { return g }
        return nil
    }

    /// What the director answered work in the person's place.
    private func directorAnswers(_ id: String, in loaded: Loaded) -> [String] {
        let events: [Event] = switch loaded {
        case .goal(let g): g.events
        case .work(_, let e): e
        }
        return events.filter { $0.work == id && $0.answeredByDirector }.map(\.body)
    }

    private func focusedTask(_ card: AgentCard, in loaded: Loaded) -> Work? { task(card.task, in: loaded) }

    /// Whether the page has a composer: a goal's only on its conversation.
    private func composes(_ loaded: Loaded) -> Bool {
        if focus != nil { return true }
        if case .goal = loaded { return goalView.composes }
        return true
    }

    /// Who a message goes to: the agent in focus's task, else this work.
    private func messaged(_ loaded: Loaded) -> Work? {
        if let focus { return task(focus.task, in: loaded) }
        if case .goal(let g) = loaded, let sendTo, let node = g.nodes.first(where: { $0.id == sendTo }) { return node.work }
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

    private func toBottom(_ proxy: ScrollViewProxy) {
        atBottom = true
        withAnimation(.easeOut(duration: 0.2)) { proxy.scrollTo(Self.bottom, anchor: .bottom) }
    }

    /// A conversation keeps its newest step in view as it grows.
    private var following: Bool {
        if focus != nil { return true }
        if case .goal = loaded { return goalView == .conversation }
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
                let goal = try await store.goal(id)
                loaded = .goal(goal)
                chained(goal.chain)
            } else {
                loaded = .work(work, try await store.events(of: id))
                chained([])
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
        VStack(alignment: .leading, spacing: Space.s2) {
            HStack(spacing: Space.s2) {
                Button(action: back) { Label(work.title, systemImage: "chevron.backward") }
                    .buttonStyle(.link)
                    .lineLimit(1)
                if focus.agent != nil {
                    Image(systemName: "chevron.forward").font(TypeScale.small).foregroundStyle(.tertiary)
                    Text(task.title).lineLimit(1).foregroundStyle(.secondary)
                }
            }
            .font(TypeScale.caption)
            Text(focus.title).font(TypeScale.title.weight(.semibold)).textSelection(.enabled)
            HStack(spacing: Space.s2) {
                if focus.agent == nil { StatePill(state: task.state) }
                Text(focus.kind).font(TypeScale.caption.monospaced()).foregroundStyle(.secondary)
                Text(focus.agent ?? task.id).font(TypeScale.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
        }
    }
}

/// The top of a work page: its name, which a double-click or Rename… makes
/// editable, and in a line under it where it stands.
struct Header: View {
    let work: Work
    var goal: GoalDetail? = nil
    let open: (String) -> Void
    let rename: (String) -> Void
    @State private var editing = false
    @State private var draft = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let goal {
                HStack(spacing: 6) {
                    Text(work.project)
                    ForEach(Array(goal.chain.enumerated()), id: \.element.id) { i, link in
                        Text("›")
                        if i == goal.chain.count - 1 {
                            Text(link.title).foregroundStyle(Palette.text)
                        } else {
                            Button(link.title) { open(link.id) }.buttonStyle(.plain).foregroundStyle(Palette.link)
                        }
                    }
                }
                .font(TypeScale.caption)
                .foregroundStyle(Palette.text3)
                .lineLimit(1)
            } else if let parent = work.parent {
                Button { open(parent) } label: {
                    Label("Goal", systemImage: "chevron.backward")
                }
                .buttonStyle(.link)
                .font(TypeScale.meta)
            }
            HStack(alignment: .center, spacing: Space.s3) {
            if editing {
                TextField("Name", text: $draft)
                    .textFieldStyle(.plain)
                    .font(TypeScale.display)
                    .focused($focused)
                    .onSubmit(save)
                    .onExitCommand { editing = false }
                    .onChange(of: focused) { _, now in if !now { editing = false } }
            } else {
                Text(work.title)
                    .font(TypeScale.display)
                    .textSelection(.enabled)
                    .onTapGesture(count: 2, perform: begin)
                    .contextMenu { Button("Rename…", action: begin) }
                    .help("Double-click to rename")
            }
                if let goal { Badge(goal.titleBadge) }
            }
            if goal == nil {
            HStack(spacing: Space.s2) {
                StatePill(state: work.state)
                Group {
                    Text(work.project)
                    if let goal, goal.rollup.total > 0 {
                        Text("·")
                        Text("\(goal.rollup.done) of \(goal.rollup.total) tasks done")
                    }
                    if let type = work.goalType {
                        Text("·")
                        Text(type.rawValue)
                    }
                    Text("·")
                    Text(work.id).monospaced().textSelection(.enabled)
                }
                .font(TypeScale.meta)
                .foregroundStyle(.secondary)
            }
            }
            if let goal, let delivery = goal.delivery, delivery.status != "none" || delivery.error != nil || !goal.landings.isEmpty {
                DeliveryLine(delivery: delivery, landings: goal.landings)
            }
            // A goal's request opens its thread; a task's detail is its brief.
            if !work.detail.isEmpty && !work.kind.isGoal {
                Text(work.detail).readable().foregroundStyle(.secondary).textSelection(.enabled).lineLimit(6)
            }
        }
    }

    private func begin() {
        draft = work.title
        editing = true
        focused = true
    }

    private func save() {
        let title = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        editing = false
        if !title.isEmpty && title != work.title { rename(title) }
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
        if let question = goal.asking {
            QuestionCard(store: store, workID: goal.goal.id, question: question,
                         title: goal.supervised ? "The supervisor asks you" : "Waiting on you",
                         change: change(question), product: goal.goal.project, answered: answered)
        }
        ForEach(goal.waitingTasks, id: \.question.id) { task, question in
            QuestionCard(store: store, workID: task.id, question: question, title: "asks you", asker: task.title,
                         change: change(question), product: goal.goal.project, answered: answered)
        }
        ForEach(asking, id: \.1.id) { task, question in
            QuestionCard(store: store, workID: task.id, question: question, title: "asks you", asker: task.title,
                         change: change(question), product: goal.goal.project, answered: answered)
        }
        ForEach(goal.otherNeeds) { need in
            QuestionCard(store: store, workID: need.work, question: need.question, title: "asks you",
                         asker: goal.nodes.first { $0.id == need.work }?.work.title ?? need.path,
                         change: need.change, product: goal.goal.project, answered: answered)
        }
    }
}

extension Waiting {
    /// What answering a question would change, as the goal serves it.
    private func change(_ question: Question) -> PendingChange? {
        goal.needsYou.first { $0.question.id == question.id }?.change
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
            Text(title).font(TypeScale.headline)
            Text(detail).font(TypeScale.callout).foregroundStyle(.secondary)
        }
        .padding(.top, 6)
    }
}

struct ClaimRow: View {
    let claim: Claim

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s1) {
            if let d = claim.event.decision {
                Text(d.question).font(TypeScale.callout.weight(.medium))
                Text(d.answer).strikethrough(!claim.stands)
                HStack(spacing: Space.s2) {
                    Text(d.source.isEmpty ? "director" : d.source)
                    if !d.model.isEmpty { Text("· \(d.model)") }
                    if d.tokens > 0 { Text("· \(d.tokens) tokens") }
                    Text("· \(claim.event.at)")
                }
                .font(TypeScale.caption).foregroundStyle(.secondary)
            } else {
                Text(claim.event.body).strikethrough(!claim.stands).textSelection(.enabled)
                Text(claim.event.at).font(TypeScale.caption).foregroundStyle(.secondary)
            }
            if !claim.stands {
                Label("Reversed: \(claim.reversed)", systemImage: "arrow.uturn.backward").font(TypeScale.caption).foregroundStyle(.orange)
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
            Text(kind).font(TypeScale.caption).foregroundStyle(.secondary)
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
            VStack(alignment: .leading, spacing: Space.s2) {
                ForEach(events.reversed()) { e in
                    HStack(alignment: .firstTextBaseline, spacing: Space.s3) {
                        Text(e.at.prefix(19).replacingOccurrences(of: "T", with: " "))
                            .font(TypeScale.caption.monospaced()).foregroundStyle(.secondary)
                            .frame(width: 140, alignment: .leading)
                        Text(Words.event(e.kind)).font(TypeScale.caption.weight(.medium)).frame(width: 70, alignment: .leading)
                        Text(Words.eventBody(kind: e.kind, body: e.body)).font(TypeScale.callout).textSelection(.enabled)
                    }
                }
            }
            .padding(.top, 6)
        } label: {
            SectionTitle("Events", detail: "\(events.count)")
        }
    }
}

/// Typing to work while it runs, as in a conversation. A goal keeps the
/// message as a decision for its supervisor; a session that is busy reads it
/// when it is ready. Sending never holds the field: the message shows in the
/// conversation at once. While something works it can be stopped, and
/// whatever it had not read yet comes back here to be said again.
struct MessageBar: View {
    let store: Store
    let work: Work
    /// Whether a session under this work is working now.
    let working: Bool
    var about: [String] = []
    /// Who else on a goal's page a message can go to, the whole goal first.
    var addressees: [(id: String, title: String)] = []
    @Binding var sendTo: String?
    @State private var text = ""
    @State private var sending = false
    @State private var outcome: String?
    @State private var failed = false
    /// Whether what is said is also kept as feedback, to learn from.
    @State private var remember = false
    @State private var files: [Attaching] = []

    var body: some View {
        InputBar(
            text: $text,
            placeholder: continuing ? "Continue this goal… say what is being worked on; it is planned and started again"
                : work.kind.isGoal ? "Message the director about this goal…" : "Message this task… it reads it when it is ready",
            command: plan?.commandLine,
            busy: sending,
            files: $files,
            sendingNow: store.outbox.contains { $0.work == work.id && $0.state == .sending },
            note: outcome,
            noteIsError: failed,
            stop: working && !continuing ? Plan.stop(work.id) : nil,
            about: about,
            addressee: addressees.count > 1 ? Addressee(options: addressees, chosen: $sendTo) : nil,
            remember: continuing ? nil : $remember,
            stopped: { Task { await stop() } }
        ) { Task { await send() } }
    }

    /// A finished goal is picked up again rather than told: continuing it
    /// reopens it with this text as the reason.
    private var continuing: Bool { work.kind.isGoal && work.state == .done }

    private var plan: Plan? {
        continuing ? try? Plan.continueGoal(work.id, text: text, files: files) : try? Plan.send(work.id, text: text, files: files)
    }

    private func send() async {
        guard let plan else { return }
        if continuing {
            sending = true
            defer { sending = false }
            do {
                _ = try await store.start(plan)
                failed = false
                outcome = "continued: planned and started again"
                text = ""
                files = []
            } catch {
                failed = true
                outcome = String(describing: error)
            }
            return
        }
        // The field is free again at once; the message shows in the
        // conversation until the work's record holds it.
        store.post(plan, to: work.id, then: remember && !text.isEmpty ? try? Plan.remember(text, work: work) : nil)
        text = ""
        files = []
        outcome = nil
        failed = false
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
    /// Files waiting to go with the message; nil where nothing can be attached.
    var files: Binding<[Attaching]>? = nil
    /// A message from here is on its way; the field stays free.
    var sendingNow = false
    var note: String? = nil
    var noteIsError = false
    /// What stopping runs, while there is something to stop.
    var stop: Plan? = nil
    /// What the message goes to: runner, model, where it works.
    var about: [String] = []
    /// On a goal's page, the menu that sends to the whole goal or one sub-goal.
    var addressee: Addressee? = nil
    /// When given, a toggle to keep the message as feedback too.
    var remember: Binding<Bool>? = nil
    var stopped: () -> Void = {}
    let submit: () -> Void
    @AppStorage("sendKey") private var sendKey: SendKey = .standard
    @State private var height = MessageField.font.boundingRectForFont.height
    @State private var taps = EscapeTaps()
    @State private var escapeMonitor: Any?
    @State private var escapeStops = EscapeTarget()
    /// Shown after one Escape, while a second one would stop.
    @State private var armed = false
    @State private var dropping = false

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s2) {
            if let note {
                Label(note, systemImage: noteIsError ? "exclamationmark.triangle.fill" : "paperplane")
                    .font(TypeScale.meta)
                    .foregroundStyle(noteIsError ? Palette.failure : .secondary)
                    .lineLimit(3)
                    .textSelection(.enabled)
                    .padding(.horizontal, Space.s1)
            }
            VStack(alignment: .leading, spacing: Space.s3) {
                if let files, !files.wrappedValue.isEmpty {
                    AttachingChips(files: files)
                }
                MessageField(
                    text: $text,
                    placeholder: placeholder,
                    sendKey: sendKey,
                    enabled: !busy,
                    height: $height,
                    attach: files.map { files in { files.wrappedValue += $0.filter { f in !files.wrappedValue.contains(f) } } },
                    send: { if command != nil && !busy { submit() } }
                )
                .frame(height: height)
                HStack(spacing: Space.s2) {
                    ForEach(about, id: \.self) { chip in
                        Text(chip)
                            .font(TypeScale.meta)
                            .padding(.horizontal, Space.s2)
                            .padding(.vertical, 3)
                            .background(Palette.sunken, in: RoundedRectangle(cornerRadius: Radius.chip))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    Spacer(minLength: Space.s2)
                    if let addressee {
                        Menu {
                            ForEach(Array(addressee.options.enumerated()), id: \.offset) { i, option in
                                Button(option.title) { addressee.chosen.wrappedValue = i == 0 ? nil : option.id }
                            }
                        } label: {
                            Text("to: \(addressee.title) ▾").font(TypeScale.caption).foregroundStyle(Palette.text3).lineLimit(1)
                        }
                        .menuStyle(.borderlessButton)
                        .menuIndicator(.hidden)
                        .fixedSize()
                        .help("Send to the whole goal, or to one sub-goal")
                    }
                    if busy {
                        ProgressView().controlSize(.small)
                    } else {
                        if let remember {
                            Toggle(isOn: remember) { Image(systemName: "graduationcap") }
                                .toggleStyle(.button)
                                .buttonStyle(.borderless)
                                .foregroundStyle(remember.wrappedValue ? Color.accentColor : Color.secondary)
                                .help(remember.wrappedValue ? "This message is also kept as feedback, so the director can learn a rule from it" : "Keep this message as feedback too, so the director can learn a rule from it")
                        }
                        if let stop {
                            Button(action: stopped) {
                                Image(systemName: "stop.fill")
                                    .font(.system(size: IconSize.small - 0.5, weight: .bold))
                                    .frame(width: 28, height: 28)
                                    .background(Palette.failure.opacity(0.14), in: Circle())
                                    .foregroundStyle(Palette.failure)
                            }
                            .buttonStyle(.plain)
                            .help("Stop (Esc Esc): \(stop.commandLine)\nWhat it has not read yet comes back here to edit")
                        }
                        if sendingNow {
                            ProgressView().controlSize(.mini).help("Sending…")
                        }
                        Button(action: submit) {
                            Image(systemName: "arrow.up")
                                .font(.system(size: IconSize.control, weight: .bold))
                                .frame(width: 28, height: 28)
                                .background(command == nil ? AnyShapeStyle(Palette.sunken) : AnyShapeStyle(Color.accentColor), in: Circle())
                                .foregroundStyle(command == nil ? Color.secondary : Color.white)
                        }
                        .buttonStyle(.plain)
                        .disabled(command == nil)
                        .help(command.map { "\(sendKey.symbol) \($0)" } ?? "Type what you want")
                    }
                }
            }
            .padding(.horizontal, Space.s4 - 2)
            .padding(.vertical, Space.s3)
            .background(Palette.raised, in: RoundedRectangle(cornerRadius: Radius.bubble))
            .overlay(RoundedRectangle(cornerRadius: Radius.bubble).strokeBorder(dropping ? Color.accentColor : Palette.separator, lineWidth: dropping ? 2 : 1))
            .onDrop(of: [.fileURL], isTargeted: files == nil ? nil : $dropping) { providers in
                guard let files else { return false }
                for provider in providers {
                    _ = provider.loadObject(ofClass: URL.self) { url, _ in
                        guard let url, let file = try? Attaching.file(url) else { return }
                        Task { @MainActor in if !files.wrappedValue.contains(file) { files.wrappedValue.append(file) } }
                    }
                }
                return true
            }
            .shadow(color: .black.opacity(0.07), radius: 10, y: 2)
            HStack(spacing: Space.s2) {
                if let command {
                    Text(command)
                        .font(TypeScale.codeTiny)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                Spacer(minLength: 0)
                Text(armed ? "Press Esc again to stop" : sendKey.title + (stop == nil ? "" : " · Esc Esc stops"))
                    .font(TypeScale.small)
                    .foregroundStyle(armed ? AnyShapeStyle(Palette.failure) : AnyShapeStyle(.tertiary))
                    .fixedSize()
            }
            .padding(.horizontal, Space.s1)
        }
        .padding(.horizontal, Space.s6)
        .padding(.top, Space.s3)
        .padding(.bottom, Space.s4)
        .background(.background)
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

/// Who a message from a goal's page goes to, and the choices.
struct Addressee {
    let options: [(id: String, title: String)]
    let chosen: Binding<String?>

    var title: String { options.first { $0.id == chosen.wrappedValue }?.title ?? options.first?.title ?? "" }
}

/// What a double Escape stops right now. The key monitor outlives the view
/// value that installed it, so it reads this rather than that value.
@MainActor
final class EscapeTarget {
    var stop: (() -> Void)?
}

/// What the person said on a goal, newest first, each a jump back to where it
/// was said.
struct YoursMenu: View {
    let yours: [Event]
    let jump: (Int) -> Void

    var body: some View {
        Menu {
            ForEach(yours.reversed()) { event in
                Button {
                    jump(event.id)
                } label: {
                    Text("\(when(event.at)) · \(String((event.decision?.answer ?? event.body).prefix(80)))")
                }
            }
        } label: {
            Label("Your messages", systemImage: "person.crop.circle")
        }
        .labelStyle(.titleAndIcon)
        .help("Jump to something you said on this goal")
    }
}

/// The agents panel's leading edge: a hairline that is dragged to resize it.
struct PanelEdge: View {
    @Binding var width: Double
    @State private var start: Double?

    var body: some View {
        Rectangle()
            .fill(Palette.hairline)
            .frame(width: 1)
            .frame(maxHeight: .infinity)
            .overlay {
                Color.clear
                    .frame(width: 9)
                    .contentShape(Rectangle())
                    .onHover { inside in
                        if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() }
                    }
                    .gesture(DragGesture(minimumDistance: 1, coordinateSpace: .global)
                        .onChanged { drag in
                            let from = start ?? width
                            start = from
                            width = PanelWidth.clamped(from - drag.translation.width)
                        }
                        .onEnded { _ in start = nil })
            }
            .accessibilityHidden(true)
    }
}

