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

    private var overview: some View {
        VStack(alignment: .leading, spacing: Space.section) {
            cards
            ForEach(goal.needsYou) { ask in
                QuestionCard(store: store, workID: ask.work, question: ask.question, title: "Needs you",
                             path: goal.pathParts(of: ask).map { part in PathPart(title: part.title, open: part.opens.map { id in { open(id) } }) },
                             at: goal.askedAt(ask.question), change: ask.change, product: goal.goal.project, answered: answered)
                    .id(ask.question.id)
            }
            let asked = Set(goal.needsYou.map(\.work))
            ForEach(goal.tasks.filter { !asked.contains($0.id) }) { task in
                if let question = goal.activity[task.id]?.asking {
                    QuestionCard(store: store, workID: task.id, question: question, title: "Needs you",
                                 path: [PathPart(title: goal.paths[task.id] ?? task.title, open: { focus(task.id) })], answered: answered)
                }
            }
            subGoals
            if !goal.notDoneBecause.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    CardLabel("Not done yet, because")
                    FlowLayout(spacing: 24) {
                        ForEach(Array(goal.notDoneBecause.enumerated()), id: \.offset) { i, why in
                            HStack(spacing: 4) {
                                Text("○").foregroundStyle(i == 0 && why.contains("sub-goal") ? Palette.orange : Palette.grey)
                                Text(why.prefix(1).uppercased() + why.dropFirst()).foregroundStyle(Palette.text)
                            }
                            .font(TypeScale.callout)
                        }
                    }
                }
                .canvasCard(padding: EdgeInsets(top: 12, leading: Space.cardH, bottom: 12, trailing: Space.cardH))
            }
            if nested && !did.isEmpty {
                VStack(alignment: .leading, spacing: 0) {
                    Text("What the director did here").font(TypeScale.title).foregroundStyle(Palette.text).padding(.bottom, 6)
                    ForEach(did) { entry in
                        HStack(alignment: .firstTextBaseline, spacing: Space.s3) {
                            Text(clock(entry.at)).font(TypeScale.caption).foregroundStyle(Palette.text3).frame(width: 52, alignment: .leading)
                            Text(entry.sentence).font(TypeScale.callout).lineSpacing(TypeScale.calloutLeading).foregroundStyle(Palette.text).textSelection(.enabled)
                        }
                        .padding(.vertical, 6)
                    }
                }
            }
        }
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

    /// Done when, Finishes at (top goals only) and Course, side by side.
    private var cards: some View {
        HStack(alignment: .top, spacing: Space.s3) {
            VStack(alignment: .leading, spacing: 4) {
                CardLabel("Done when")
                Text(goal.doneWhenSentence ?? "Not set yet.")
                    .font(TypeScale.callout).lineSpacing(TypeScale.calloutLeading)
                    .foregroundStyle(goal.doneWhen == nil ? Palette.text3 : Palette.text)
                    .textSelection(.enabled)
            }
            .canvasCard(stretch: true)
            if !nested {
                VStack(alignment: .leading, spacing: 4) {
                    CardLabel("Finishes at")
                    FinishMenu(goal: goal.goal.id, current: goal.finish?.line, run: run) {
                        HStack(spacing: 4) {
                            if let finish = goal.finishWords {
                                Text(finish.line).font(TypeScale.headline).foregroundStyle(Palette.text)
                            } else {
                                Text("Not decided yet").font(TypeScale.callout).foregroundStyle(Palette.text3)
                            }
                            Text("▾").font(TypeScale.small).foregroundStyle(Palette.text2)
                        }
                    }
                    .disabled(goal.goal.atRest)
                    if let finish = goal.finishWords {
                        Text(finish.source).font(TypeScale.caption).foregroundStyle(Palette.text3)
                    }
                }
                .canvasCard(stretch: true)
            }
            VStack(alignment: .leading, spacing: 4) {
                CardLabel("Course")
                if let course = goal.course {
                    HStack(spacing: 6) {
                        Badge(course.badge)
                        Text("checked \(agoWords(course.at))").font(TypeScale.caption).foregroundStyle(Palette.text3)
                    }
                    if !course.why.isEmpty {
                        Text(course.why.sentence).font(TypeScale.caption).foregroundStyle(Palette.text3).padding(.top, 2)
                    }
                } else {
                    Text("Not checked yet").font(TypeScale.callout).foregroundStyle(Palette.text3)
                }
            }
            .canvasCard(stretch: true)
        }
        .fixedSize(horizontal: false, vertical: true)
    }

    private var subGoals: some View {
        VStack(alignment: .leading, spacing: Space.s2) {
            HStack(spacing: Space.s3) {
                Text("Sub-goals").font(TypeScale.title).foregroundStyle(Palette.text)
                if !nested && goal.progress.total > 0 {
                    ProgressBar(done: goal.progress.done, total: goal.progress.total)
                }
                if goal.progress.total + goal.progress.superseded > 0 {
                    Text(goal.progressLine).font(TypeScale.caption).foregroundStyle(Palette.text3)
                }
                Spacer()
                if !goal.goal.atRest {
                    Button("+ Add sub-goal") { adding = true }.buttonStyle(.canvas)
                }
            }
            if goal.tree.isEmpty {
                Text(goal.goal.state == .queued ? "Not planned yet." : "No sub-goals: one agent takes it whole.")
                    .font(TypeScale.callout).foregroundStyle(Palette.text3)
                    .canvasCard()
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(goal.tree.enumerated()), id: \.element.id) { i, node in
                        if i > 0 { Rectangle().fill(Palette.inner).frame(height: 1) }
                        SubGoalRow(node: node, depth: 0, superseded: goal.superseded, opened: openedRows, toggle: toggle, open: open, focus: focus)
                    }
                }
                .background(Palette.page, in: RoundedRectangle(cornerRadius: Radius.card))
                .clipShape(RoundedRectangle(cornerRadius: Radius.card))
                .overlay(RoundedRectangle(cornerRadius: Radius.card).strokeBorder(Palette.hairline))
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

/// One sub-goal: its icon, title, line and badge; a goal opens its page, a
/// task folds its steps, a group folds the tasks it gathers.
struct SubGoalRow: View {
    let node: TreeNode
    let depth: Int
    let superseded: [SupersededWork]
    let opened: Set<String>
    let toggle: (String) -> Void
    let open: (String) -> Void
    let focus: (String) -> Void

    private var isOpen: Bool { opened.contains(node.id) }
    private var gone: Bool { node.work.state.mark == .superseded }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: Space.s3) {
                StateIcon(mark: node.mark).padding(.top, 1)
                VStack(alignment: .leading, spacing: 2) {
                    Text(node.work.title + (node.opensAsGoal ? " ›" : ""))
                        .font(TypeScale.bodyMedium)
                        .strikethrough(gone)
                        .foregroundStyle(gone ? Palette.text2 : Palette.text)
                        .multilineTextAlignment(.leading)
                    HStack(spacing: 0) {
                        Text(node.meta(superseded: superseded))
                        if node.folds && !isOpen && node.work.state.mark == .working {
                            Text(" · ")
                            Text("Show steps (\(node.steps.filter { $0.state == .done }.count) of \(node.steps.filter { $0.state != .dropped }.count))").foregroundStyle(Palette.link)
                        }
                    }
                    .font(TypeScale.caption)
                    .foregroundStyle(Palette.text3)
                    .lineLimit(2)
                }
                Spacer(minLength: Space.s2)
                Badge(node.badge)
            }
            .padding(EdgeInsets(top: 14, leading: 16 + CGFloat(depth) * 32, bottom: 14, trailing: 16))
            .contentShape(Rectangle())
            .onTapGesture { choose() }
            .help(node.opensAsGoal ? "Open this sub-goal" : node.folds || node.group ? (isOpen ? "Hide" : "Show") : "Show this agent's conversation")
            if isOpen && node.folds {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(node.steps, id: \.n) { StepLine(step: $0) }
                    HStack(spacing: 0) {
                        Text("The agent's own plan · ")
                        Button("Hide steps") { toggle(node.id) }.buttonStyle(.plain).foregroundStyle(Palette.link)
                    }
                    .font(TypeScale.caption).foregroundStyle(Palette.text3).padding(.top, 4)
                }
                .padding(EdgeInsets(top: 0, leading: 48 + CGFloat(depth) * 32, bottom: 14, trailing: 16))
            }
            if isOpen && node.group {
                ForEach(node.children) { child in
                    Rectangle().fill(Palette.inner).frame(height: 1)
                    SubGoalRow(node: child, depth: depth + 1, superseded: superseded, opened: opened, toggle: toggle, open: open, focus: focus)
                }
            }
        }
        .background(node.work.state == .needsInput ? Palette.needsYouRow : .clear)
        .opacity(gone ? 0.75 : 1)
    }

    private func choose() {
        if node.opensAsGoal { open(node.id) } else if node.folds || node.group { toggle(node.id) } else { focus(node.id) }
    }
}

/// One step of an agent's own plan.
struct StepLine: View {
    let step: PlanStep

    var body: some View {
        HStack(spacing: 10) {
            Text(glyph).foregroundStyle(color)
            Text(step.text).strikethrough(step.state == .dropped).foregroundStyle(text)
        }
        .font(step.state == .doing ? TypeScale.calloutStrong : TypeScale.callout)
        .lineSpacing(TypeScale.calloutLeading)
    }

    private var glyph: String {
        switch step.state {
        case .done: "✓"
        case .doing: "●"
        case .todo, .dropped: "○"
        }
    }

    private var color: Color {
        switch step.state {
        case .done: Palette.green
        case .doing: BadgeTone.live.text
        case .todo, .dropped: Palette.text3
        }
    }

    private var text: Color {
        switch step.state {
        case .done: Palette.stepText
        case .doing: BadgeTone.live.text
        case .todo, .dropped: Palette.text3
        }
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
