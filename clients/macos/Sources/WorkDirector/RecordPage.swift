import SwiftUI
import WorkDirectorKit

/// A goal's record, as the canvas draws it: five counts, kind chips and a
/// sub-goal picker over a table of what was decided and done, by day.
struct GoalRecordView: View {
    let store: Store
    let goal: GoalDetail
    @State private var record: GoalRecord?
    @State private var failure: String?
    @State private var kind: RecordKind = .everything
    @State private var place: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let record {
                HStack(alignment: .top, spacing: 10) {
                    ForEach(Array(record.summary.cards.enumerated()), id: \.offset) { _, card in
                        VStack(alignment: .leading, spacing: 0) {
                            Text("\(card.number)").font(TypeScale.figure).foregroundStyle(Palette.text).monospacedDigit()
                            Text(card.label).font(TypeScale.caption).foregroundStyle(Palette.text3).fixedSize(horizontal: false, vertical: true)
                        }
                        .canvasCard(padding: EdgeInsets(top: 10, leading: 12, bottom: 10, trailing: 12))
                        .frame(maxHeight: .infinity)
                    }
                }
                .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 6) {
                    ForEach(RecordKind.allCases, id: \.self) { k in
                        FilterChip(label: k.label, selected: kind == k) { kind = k }
                    }
                    Spacer(minLength: Space.s2)
                    Menu {
                        Button("All") { place = nil }
                        Divider()
                        ForEach(goal.recordPlaces, id: \.self) { p in Button(p) { place = p } }
                    } label: {
                        Text("Sub-goal: \(place ?? "all") ▾").font(TypeScale.caption).foregroundStyle(Palette.text3).lineLimit(1)
                    }
                    .menuStyle(.borderlessButton)
                    .menuIndicator(.hidden)
                    .fixedSize()
                }
                table(record.entries(kind, under: place))
            } else if let failure {
                Text(failure).font(TypeScale.callout).foregroundStyle(Palette.failure).textSelection(.enabled)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .task(id: "\(goal.goal.id)-\(goal.events.count)") {
            do {
                record = try await store.record(goal.goal.id)
                failure = nil
            } catch {
                failure = "\(error)"
            }
        }
    }

    private func table(_ entries: [RecordEntry]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            RecordRow(time: Text("Time"), kind: Text("Kind"), what: Text("What"), place: Text("Where"), by: Text("By"))
                .font(TypeScale.caption)
                .foregroundStyle(Palette.text3)
                .padding(.bottom, 9)
            Rectangle().fill(Palette.hairline).frame(height: 1)
            if entries.isEmpty {
                Text("Nothing here yet.").font(TypeScale.callout).foregroundStyle(Palette.text3).padding(.vertical, Space.s3)
            }
            ForEach(GoalRecord.days(entries)) { day in
                Text(day.day).font(TypeScale.caption.weight(.semibold)).foregroundStyle(Palette.stepText)
                    .padding(EdgeInsets(top: 14, leading: 0, bottom: 6, trailing: 0))
                ForEach(Array(day.entries.enumerated()), id: \.offset) { i, e in
                    RecordRow(time: Text(clock(e.at)).font(TypeScale.caption).foregroundStyle(Palette.text3),
                              kind: Badge(e.badge),
                              what: Text(e.text).font(TypeScale.callout).lineSpacing(TypeScale.calloutLeading).foregroundStyle(Palette.text).textSelection(.enabled),
                              place: Text(e.where).font(TypeScale.caption).foregroundStyle(Palette.text2),
                              by: Text(e.by).font(TypeScale.caption).foregroundStyle(Palette.text3))
                        .padding(.vertical, 9)
                    if i < day.entries.count - 1 { Rectangle().fill(Palette.rowSeparator).frame(height: 1) }
                }
            }
        }
    }
}

/// One row of the record's table: 52 · 96 · what · 220 · 90, 12 apart.
struct RecordRow<Time: View, Kind: View, What: View, Place: View, By: View>: View {
    let time: Time
    let kind: Kind
    let what: What
    let place: Place
    let by: By

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            time.frame(width: 52, alignment: .leading)
            kind.frame(width: 96, alignment: .leading)
            what.frame(maxWidth: .infinity, alignment: .leading)
            place.frame(width: 220, alignment: .leading)
            by.frame(width: 90, alignment: .trailing)
        }
    }
}
