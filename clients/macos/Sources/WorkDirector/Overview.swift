import SwiftUI
import WorkDirectorKit

/// A goal at a glance, as the canvas draws it: what done means, where it
/// finishes and whether it is on course; every question below it, answered in
/// place; its sub-goals with their steps; and what still holds it from done.
struct GoalOverview: View {
    let store: Store
    let goal: GoalDetail
    /// Opens another goal's page.
    let open: (String) -> Void
    /// Shows a task's conversation.
    let focus: (String) -> Void
    let answered: () -> Void
    let run: (Plan) -> Void
    @State private var opened: Set<String>?
    @State private var adding = false
    @State private var did: [RecordEntry] = []

    private var nested: Bool { goal.chain.count > 1 }

    var body: some View {
        if let proposal = goal.proposal {
            ProposalView(store: store, goal: goal, rows: proposal, run: run, reload: answered)
        } else {
            overview
        }
    }

    /// The whole overview is one text, its controls set in it: a drag or
    /// Select All spans the cards, every sub-goal's row and what follows.
    private var overview: some View {
        let asked = Set(goal.needsYou.map(\.work))
        let taskAsks = goal.tasks.filter { !asked.contains($0.id) }.compactMap { task in goal.activity[task.id]?.asking.map { (task, $0) } }
        let asks = goal.needsYou.map(\.question.id) + taskAsks.map(\.1.id)
        let working = Set(goal.nodes.map(\.id).filter(goal.isWorking))
        let transcript = goal.overview(opened: openedRows, working: working, asks: asks, did: nested ? did : [], ago: agoWords, clock: clock)
        var slots: [String: AnyView] = [:]
        for ask in goal.needsYou {
            slots[OverviewSlot.ask(ask.question.id)] = AnyView(
                QuestionCard(store: store, workID: ask.work, question: ask.question, title: "Needs you",
                             path: goal.pathParts(of: ask).map { part in PathPart(title: part.title, open: part.opens.map { id in { open(id) } }) },
                             at: goal.askedAt(ask.question), change: ask.change, product: goal.goal.project, answered: answered))
        }
        for (task, question) in taskAsks {
            slots[OverviewSlot.ask(question.id)] = AnyView(
                QuestionCard(store: store, workID: task.id, question: question, title: "Needs you",
                             path: [PathPart(title: goal.paths[task.id] ?? task.title, open: { focus(task.id) })], answered: answered))
        }
        if !nested && goal.progress.total > 0 {
            slots[OverviewSlot.progress] = AnyView(ProgressBar(done: goal.progress.done, total: goal.progress.total))
        }
        if !goal.goal.atRest {
            slots[OverviewSlot.add] = AnyView(Button("+ Add sub-goal") { adding = true }.buttonStyle(.canvas).accessibilityLabel("Add sub-goal"))
        }
        for id in working { slots[OverviewSlot.working(id)] = AnyView(WorkingMark(size: 4)) }
        var overs: [String: AnyView] = [:]
        if !nested {
            overs[OverviewSlot.finish] = AnyView(
                FinishMenu(goal: goal.goal.id, current: goal.finish?.line, name: ControlName.finishMenu(goal.finishWords?.line), run: run) {
                    Color.clear.contentShape(Rectangle()).frame(maxWidth: .infinity, maxHeight: .infinity)
                }
                .disabled(goal.goal.atRest))
        }
        return TranscriptText(transcript: transcript, key: "overview/\(goal.goal.id)", act: { verb, id in
            switch verb {
            case OverviewAct.open: open(id)
            case OverviewAct.focus: focus(id)
            case OverviewAct.carry: run(.carryOn(id))
            case OverviewAct.fixChecks:
                if let fix = goal.checksFailOnMain?.fix, let plan = try? Plan.addSubGoal(id, outcome: fix.outcome, doneWhen: fix.doneWhen) { run(plan) }
            default: break
            }
        }, slots: slots, overs: overs, toggle: toggle)
        .task(id: "\(goal.goal.id)-\(goal.events.count)") {
            guard nested else { return }
            did = (try? await store.record(goal.goal.id))?.entries.filter { $0.by == "director" } ?? []
        }
        .sheet(isPresented: $adding) {
            AddSubGoalSheet(goal: goal.goal) { plan in
                adding = false
                if let plan { run(plan) }
            }
        }
    }

    private var openedRows: Set<String> { opened ?? Set([goal.openSteps].compactMap { $0 }) }

    private func toggle(_ id: String) {
        var now = openedRows
        if now.contains(id) { now.remove(id) } else { now.insert(id) }
        withAnimation(.easeOut(duration: 0.15)) { opened = now }
    }
}

/// Asks for a new sub-goal's outcome and what makes it done.
struct AddSubGoalSheet: View {
    let goal: Work
    let done: (Plan?) -> Void
    @State private var outcome = ""
    @State private var doneWhen = ""

    private var plan: Plan? { try? Plan.addSubGoal(goal.id, outcome: outcome, doneWhen: doneWhen) }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            Text("Add a sub-goal to \(goal.title)").font(TypeScale.headline)
            TextField("An outcome a person would say", text: $outcome)
            TextField("Done when…", text: $doneWhen, axis: .vertical).lineLimit(2...4)
            if let plan {
                Text(plan.commandLine).font(TypeScale.codeTiny).foregroundStyle(Palette.text3).lineLimit(2)
            }
            HStack {
                Spacer()
                Button("Cancel") { done(nil) }.keyboardShortcut(.cancelAction)
                Button("Add") { done(plan) }.keyboardShortcut(.defaultAction).disabled(plan == nil)
            }
        }
        .textFieldStyle(.roundedBorder)
        .padding(Space.s5)
        .frame(width: 460)
    }
}
