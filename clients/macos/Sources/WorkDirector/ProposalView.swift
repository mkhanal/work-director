import SwiftUI
import WorkDirectorKit

/// A breakdown waiting for the person's approval, as the canvas's proposal:
/// what done means and where it finishes, then each proposed sub-goal with
/// what makes it done, its order and a way to edit, split or drop it.
struct ProposalView: View {
    let goal: GoalDetail
    let rows: [ProposedRow]
    let run: (Plan) -> Void
    @State private var doneWhen: String
    @State private var sheet: Sheet?

    enum Sheet: Identifiable {
        case edit(TreeNode), drop(TreeNode), add
        var id: String {
            switch self {
            case .edit(let n): "edit-\(n.id)"
            case .drop(let n): "drop-\(n.id)"
            case .add: "add"
            }
        }
    }

    init(goal: GoalDetail, rows: [ProposedRow], run: @escaping (Plan) -> Void) {
        self.goal = goal
        self.rows = rows
        self.run = run
        _doneWhen = State(initialValue: goal.doneWhen ?? "")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 10) {
                Text("Nothing starts until you approve").font(TypeScale.caption).foregroundStyle(Palette.text3)
                Spacer()
                Button("Discard") { run(Plan.cancel(goal.goal.id, reason: "the proposed breakdown was discarded")) }.buttonStyle(.canvas)
                Button("Approve and start") { run(Plan.approve(goal.goal.id)) }.buttonStyle(.canvasPrimary)
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
                    .help("Where it finishes is decided at planning; tell the director to change it")
                    if let source = goal.finishWords?.source {
                        Text(source).font(TypeScale.caption).foregroundStyle(Palette.text3).fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            HStack(spacing: 10) {
                Text("Proposed sub-goals").font(TypeScale.title).foregroundStyle(Palette.text)
                Text("each is about one agent's work").font(TypeScale.caption).foregroundStyle(Palette.text3)
            }
            .padding(.top, 6)
            ForEach(rows) { row in
                ProposedCard(row: row, edit: { sheet = .edit(row.node) }, split: { run(Plan.splitGoal(row.node.id)) }, drop: { sheet = .drop(row.node) })
            }
            Button("+ Add sub-goal") { sheet = .add }.buttonStyle(.canvas)
        }
        .sheet(item: $sheet) { which in
            switch which {
            case .add:
                AddSubGoalSheet(goal: goal.goal) { plan in sheet = nil; if let plan { run(plan) } }
            case .edit(let node):
                EditSubGoalSheet(node: node) { plan in sheet = nil; if let plan { run(plan) } }
            case .drop(let node):
                DropSubGoalSheet(node: node) { plan in sheet = nil; if let plan { run(plan) } }
            }
        }
    }
}

/// One proposed sub-goal: its number, title, done-when and chips, with Edit,
/// Split (a task only) and Drop.
struct ProposedCard: View {
    let row: ProposedRow
    let edit: () -> Void
    let split: () -> Void
    let drop: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
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
                Button("Edit", action: edit)
                if !row.node.work.kind.isGoal { Button("Split", action: split) }
                Button("Drop", action: drop)
            }
            .buttonStyle(.plain)
            .font(TypeScale.caption)
            .foregroundStyle(Palette.link)
        }
        .canvasCard()
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
