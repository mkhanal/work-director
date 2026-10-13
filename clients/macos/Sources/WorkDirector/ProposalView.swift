import SwiftUI
import WorkDirectorKit

/// A breakdown waiting for the person's approval, as the canvas's proposal:
/// what done means and where it finishes, then each proposed sub-goal with
/// what makes it done, its order and a way to edit, split, merge or drop it;
/// beside it, why the planner broke it down so.
struct ProposalView: View {
    let store: Store
    let goal: GoalDetail
    let rows: [ProposedRow]
    let run: (Plan) -> Void
    /// Reads the goal again after writes run here.
    let reload: () -> Void
    @State private var doneWhen: String
    @State private var sheet: Sheet?
    @State private var busy: String?
    @State private var failure: String?
    @State private var dropTarget: String?

    enum Sheet: Identifiable {
        case edit(TreeNode), drop(TreeNode), add, replan
        var id: String {
            switch self {
            case .edit(let n): "edit-\(n.id)"
            case .drop(let n): "drop-\(n.id)"
            case .add: "add"
            case .replan: "replan"
            }
        }
    }

    init(store: Store, goal: GoalDetail, rows: [ProposedRow], run: @escaping (Plan) -> Void, reload: @escaping () -> Void) {
        self.store = store
        self.goal = goal
        self.rows = rows
        self.run = run
        self.reload = reload
        _doneWhen = State(initialValue: goal.doneWhen ?? "")
    }

    var body: some View {
        HStack(alignment: .top, spacing: Space.s6) {
            main
            if let reasoning = goal.reasoning, !reasoning.sections.isEmpty {
                ReasoningPanel(reasoning: reasoning)
            }
        }
        .sheet(item: $sheet) { which in
            switch which {
            case .add:
                AddSubGoalSheet(goal: goal.goal) { plan in sheet = nil; if let plan { run(plan) } }
            case .edit(let node):
                EditSubGoalSheet(node: node) { plan in sheet = nil; if let plan { run(plan) } }
            case .drop(let node):
                DropSubGoalSheet(node: node) { plan in sheet = nil; if let plan { run(plan) } }
            case .replan:
                ReplanSheet(goal: goal.goal) { plans in
                    sheet = nil
                    if let plans { perform(plans, doing: "Planning it again…") }
                }
            }
        }
    }

    private var main: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 10) {
                if let busy {
                    ProgressView().controlSize(.small)
                    Text(busy).font(TypeScale.caption).foregroundStyle(Palette.text3)
                } else {
                    Text("Nothing starts until you approve").font(TypeScale.caption).foregroundStyle(Palette.text3)
                }
                Spacer()
                Button(ControlName.discard) { run(Plan.cancel(goal.goal.id, reason: "the proposed breakdown was discarded")) }
                    .buttonStyle(.canvas).accessibilityLabel(ControlName.discard)
                Button(ControlName.replan) { sheet = .replan }
                    .buttonStyle(.canvas).accessibilityLabel(ControlName.replan)
                Button(ControlName.approve) { run(Plan.approve(goal.goal.id)) }
                    .buttonStyle(.canvasPrimary).accessibilityLabel(ControlName.approve)
            }
            .disabled(busy != nil)
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(TypeScale.meta).foregroundStyle(Palette.failure).textSelection(.enabled)
            }
            SplitRow(spacing: Space.s3) {
                VStack(alignment: .leading, spacing: 4) {
                    CardLabel("Done when")
                    TextField("What makes this goal done", text: $doneWhen, axis: .vertical)
                        .textFieldStyle(.plain)
                        .font(TypeScale.callout)
                        .lineLimit(1...4)
                        .padding(.horizontal, 10).padding(.vertical, 8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Palette.page, in: RoundedRectangle(cornerRadius: Radius.option))
                        .overlay(RoundedRectangle(cornerRadius: Radius.option).strokeBorder(Palette.buttonBorder))
                        .onSubmit {
                            if doneWhen != (goal.doneWhen ?? ""), let plan = try? Plan.editGoal(goal.goal.id, doneWhen: doneWhen) { run(plan) }
                        }
                }
                VStack(alignment: .leading, spacing: 4) {
                    CardLabel("Finishes at")
                    FinishMenu(goal: goal.goal.id, current: goal.finish?.line, name: ControlName.finishMenu(goal.finishWords?.line), run: run) {
                        HStack {
                            Text(goal.finishWords?.line ?? "Not decided yet")
                                .font(TypeScale.calloutStrong)
                                .foregroundStyle(Palette.text)
                                .lineLimit(1)
                            Spacer(minLength: 4)
                            Text("▾").font(TypeScale.small).foregroundStyle(Palette.text2)
                        }
                        .padding(.horizontal, 10).padding(.vertical, 8)
                        .background(Palette.page, in: RoundedRectangle(cornerRadius: Radius.option))
                        .overlay(RoundedRectangle(cornerRadius: Radius.option).strokeBorder(Palette.buttonBorder))
                    }
                    if let source = goal.finishWords?.source {
                        Text(source).font(TypeScale.caption).foregroundStyle(Palette.text3).fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            HStack(spacing: 10) {
                Text("Proposed sub-goals").font(TypeScale.title).foregroundStyle(Palette.text)
                Text("drag to reorder · each is about one agent's work").font(TypeScale.caption).foregroundStyle(Palette.text3)
            }
            .padding(.top, 6)
            ForEach(Array(rows.enumerated()), id: \.element.id) { i, row in
                ProposedCard(row: row, others: rows.filter { $0.id != row.id },
                             edit: { sheet = .edit(row.node) }, split: { run(Plan.splitGoal(row.node.id)) },
                             merge: { other in run(Plan.merge(row, other)) }, drop: { sheet = .drop(row.node) })
                    .overlay(alignment: .top) {
                        if dropTarget == row.id { Rectangle().fill(Palette.accent).frame(height: 2).offset(y: -8) }
                    }
                    .draggable(row.id)
                    .dropDestination(for: String.self) { ids, _ in
                        guard let moved = ids.first, moved != row.id else { return false }
                        perform(goal.reorder(moving: moved, to: i), doing: "Reordering…")
                        return true
                    } isTargeted: { over in
                        dropTarget = over ? row.id : (dropTarget == row.id ? nil : dropTarget)
                    }
            }
            Button("+ Add sub-goal") { sheet = .add }.buttonStyle(.canvas).accessibilityLabel("Add sub-goal")
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// Runs writes in order, stopping at the first refused.
    private func perform(_ plans: [Plan], doing: String) {
        guard !plans.isEmpty else { return }
        busy = doing
        Task {
            defer { busy = nil }
            failure = nil
            for plan in plans {
                do {
                    let r = try await store.run(plan)
                    if r.code != 0 {
                        failure = plan.commandLine + ": " + (r.stderr.isEmpty ? r.stdout : r.stderr).trimmingCharacters(in: .whitespacesAndNewlines)
                        break
                    }
                } catch {
                    failure = "\(error)"
                    break
                }
            }
            await store.refresh()
            reload()
        }
    }
}

/// Why the planner broke the goal down so, beside the proposal.
struct ReasoningPanel: View {
    let reasoning: Reasoning

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s4) {
            ForEach(reasoning.sections, id: \.label) { section in
                VStack(alignment: .leading, spacing: Space.s2) {
                    MicroLabel(section.label)
                    Text(section.text)
                        .font(TypeScale.callout).lineSpacing(TypeScale.calloutLeading)
                        .foregroundStyle(Palette.text)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
            }
        }
        .frame(width: 280, alignment: .topLeading)
        .padding(.leading, Space.s5)
        .overlay(alignment: .leading) { Rectangle().fill(Palette.hairline).frame(width: 1) }
    }
}

/// The six finish lines as a menu, the current one checked; choosing another
/// sets it for the top goal.
struct FinishMenu<Label: View>: View {
    let goal: String
    let current: String?
    /// What it is announced as: Finishes at and the line it shows.
    let name: String
    let run: (Plan) -> Void
    @ViewBuilder let label: () -> Label

    var body: some View {
        Menu {
            ForEach(FinishMeaning.lines, id: \.self) { line in
                Toggle(FinishMeaning.of(line)?.title ?? line, isOn: Binding(
                    get: { current?.lowercased() == line.lowercased() },
                    set: { on in if on { run(Plan.finish(goal, line)) } }))
            }
        } label: {
            label()
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .help("Where this goal finishes")
        .accessibilityLabel(name)
    }
}

/// One proposed sub-goal: its grip, number, title, done-when and chips, with
/// Edit, Split (a task only), Merge with another card and Drop.
struct ProposedCard: View {
    let row: ProposedRow
    let others: [ProposedRow]
    let edit: () -> Void
    let split: () -> Void
    let merge: (ProposedRow) -> Void
    let drop: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "line.3.horizontal")
                .font(.system(size: IconSize.small))
                .foregroundStyle(Palette.grey)
                .frame(height: 22)
                .help("Drag to reorder")
            Text("\(row.number)")
                .font(TypeScale.meta.weight(.semibold))
                .foregroundStyle(Palette.text)
                .frame(width: 22, height: 22)
                .background(Palette.inner, in: Circle())
            VStack(alignment: .leading, spacing: 6) {
                Text(row.node.work.title).font(TypeScale.bodyMedium).foregroundStyle(Palette.text)
                if let doneWhen = row.node.doneWhen {
                    Text("Done when " + doneWhen).font(TypeScale.caption).foregroundStyle(Palette.text3)
                }
                HStack(spacing: 6) {
                    ForEach(row.chips, id: \.self) { SubGoalTag(text: $0) }
                }
            }
            Spacer(minLength: Space.s2)
            HStack(spacing: 8) {
                Button("Edit", action: edit).accessibilityLabel(ControlName.card("Edit", row))
                if !row.node.work.kind.isGoal { Button("Split", action: split).accessibilityLabel(ControlName.card("Split", row)) }
                if others.count == 1, let other = others.first {
                    Button("Merge with \(other.number)") { merge(other) }
                        .accessibilityLabel(ControlName.card("Merge", row, then: " with \(other.number)"))
                } else if !others.isEmpty {
                    Menu("Merge with…") {
                        ForEach(others) { other in
                            Button("\(other.number) · \(other.node.work.title)") { merge(other) }
                        }
                    }
                    .menuStyle(.button)
                    .menuIndicator(.hidden)
                    .fixedSize()
                    .accessibilityLabel(ControlName.card("Merge", row, then: " with…"))
                }
                Button("Drop", action: drop).accessibilityLabel(ControlName.card("Drop", row))
            }
            .buttonStyle(.plain)
            .font(TypeScale.caption)
            .foregroundStyle(Palette.link)
        }
        .canvasCard()
        .contentShape(Rectangle())
        .textSelection(.enabled)
        .copyable([row.node.work.title, row.node.doneWhen.map { "Done when " + $0 }].compactMap { $0 }.joined(separator: "\n"))
    }
}

/// Plans a goal again, with a note the planner reads first.
struct ReplanSheet: View {
    let goal: Work
    let done: ([Plan]?) -> Void
    @State private var note = ""

    private var plans: [Plan]? { try? Plan.replan(goal.id, note: note) }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            Text("Re-plan \(goal.title)").font(TypeScale.headline)
            TextField("What the planner should do differently", text: $note, axis: .vertical).lineLimit(3...6)
            if let plans {
                Text(plans.map(\.commandLine).joined(separator: "\n")).font(TypeScale.codeTiny).foregroundStyle(Palette.text3).lineLimit(4)
            }
            HStack {
                Spacer()
                Button("Cancel") { done(nil) }.keyboardShortcut(.cancelAction)
                Button("Re-plan") { done(plans) }.keyboardShortcut(.defaultAction).disabled(plans == nil)
            }
        }
        .textFieldStyle(.roundedBorder)
        .padding(Space.s5)
        .frame(width: 460)
    }
}

/// Changes a sub-goal's title or done-when.
struct EditSubGoalSheet: View {
    let node: TreeNode
    let done: (Plan?) -> Void
    @State private var title: String
    @State private var doneWhen: String

    init(node: TreeNode, done: @escaping (Plan?) -> Void) {
        self.node = node
        self.done = done
        _title = State(initialValue: node.work.title)
        _doneWhen = State(initialValue: node.doneWhen ?? "")
    }

    private var plan: Plan? {
        try? Plan.editGoal(node.id, title: title == node.work.title ? nil : title,
                           doneWhen: doneWhen == (node.doneWhen ?? "") ? nil : doneWhen)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            Text("Edit sub-goal").font(TypeScale.headline)
            TextField("An outcome a person would say", text: $title)
            TextField("Done when…", text: $doneWhen, axis: .vertical).lineLimit(2...4)
            if let plan { Text(plan.commandLine).font(TypeScale.codeTiny).foregroundStyle(Palette.text3).lineLimit(2) }
            HStack {
                Spacer()
                Button("Cancel") { done(nil) }.keyboardShortcut(.cancelAction)
                Button("Save") { done(plan) }.keyboardShortcut(.defaultAction).disabled(plan == nil)
            }
        }
        .textFieldStyle(.roundedBorder)
        .padding(Space.s5)
        .frame(width: 460)
    }
}

/// Supersedes a sub-goal, asking why.
struct DropSubGoalSheet: View {
    let node: TreeNode
    let done: (Plan?) -> Void
    @State private var why = ""

    private var plan: Plan? { try? Plan.drop(node.id, why: why) }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s3) {
            Text("Drop \(node.work.title)").font(TypeScale.headline)
            TextField("Why it is not needed", text: $why, axis: .vertical).lineLimit(2...4)
            if let plan { Text(plan.commandLine).font(TypeScale.codeTiny).foregroundStyle(Palette.text3).lineLimit(2) }
            HStack {
                Spacer()
                Button("Cancel") { done(nil) }.keyboardShortcut(.cancelAction)
                Button("Drop") { done(plan) }.keyboardShortcut(.defaultAction).disabled(plan == nil)
            }
        }
        .textFieldStyle(.roundedBorder)
        .padding(Space.s5)
        .frame(width: 460)
    }
}

/// Two views side by side, the first twice as wide as the second, both
/// top-aligned: the canvas's 2:1 grid.
struct SplitRow: Layout {
    var spacing: CGFloat = 12

    private func widths(_ total: CGFloat) -> (CGFloat, CGFloat) {
        let unit = max(0, total - spacing) / 3
        return (unit * 2, unit)
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? 600
        let (a, b) = widths(width)
        let heights = zip(subviews, [a, b]).map { $0.sizeThatFits(ProposedViewSize(width: $1, height: nil)).height }
        return CGSize(width: width, height: heights.max() ?? 0)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let (a, b) = widths(bounds.width)
        var x = bounds.minX
        for (view, w) in zip(subviews, [a, b]) {
            view.place(at: CGPoint(x: x, y: bounds.minY), proposal: ProposedViewSize(width: w, height: nil))
            x += w + spacing
        }
    }
}
