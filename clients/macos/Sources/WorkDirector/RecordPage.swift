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
                        .canvasCard(padding: EdgeInsets(top: 10, leading: 12, bottom: 10, trailing: 12), stretch: true)
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

    /// The table as one text: a drag or Select All spans its days and rows.
    @ViewBuilder private func table(_ entries: [RecordEntry]) -> some View {
        TranscriptText(transcript: GoalRecord.transcript(entries, time: clock), key: "record/\(goal.goal.id)/\(kind)/\(place ?? "")")
        if entries.isEmpty {
            Text("Nothing here yet.").font(TypeScale.callout).foregroundStyle(Palette.text3).padding(.vertical, Space.s3)
        }
    }
}
