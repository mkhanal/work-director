import SwiftUI
import WorkDirectorKit

/// One review pass: what the loop decided since the last acknowledgement.
struct ReviewList: View {
    let store: Store
    let project: String
    @Binding var selected: String?
    @Binding var plan: Plan?

    /// A row is one fact in the pass; several can name the same work, so the row
    /// is selected and the work it names is what the detail shows.
    @State private var row: Row?

    enum Row: Hashable {
        case claim(event: Int, work: String)
        case landed(url: String, work: String?)
        case unshipped(work: String)

        var work: String? {
            switch self {
            case .claim(_, let work), .unshipped(let work): work
            case .landed(_, let work): work
            }
        }
    }
    @State private var pass: ReviewPass?
    @State private var failure: String?

    var body: some View {
        Group {
            if let pass {
                if pass.isEmpty {
                    ContentUnavailableView("Nothing since the last review", systemImage: "checkmark.circle", description: Text("The loop has decided nothing new in \(project)."))
                } else {
                    List(selection: $row) {
                        if !pass.claims.isEmpty {
                            Section("Decided (\(pass.claims.count))") {
                                ForEach(pass.claims, id: \.event.id) { claim in
                                    ClaimRow(claim: claim).tag(Row.claim(event: claim.event.id, work: claim.event.work))
                                }
                            }
                        }
                        if !pass.landed.isEmpty {
                            Section("Landed (\(pass.landed.count))") {
                                ForEach(pass.landed, id: \.url) { landed in
                                    LandingRow(landed: landed).tag(Row.landed(url: landed.url, work: landed.work?.id))
                                }
                            }
                        }
                        if !pass.unshipped.isEmpty {
                            Section("Stopped without shipping (\(pass.unshipped.count))") {
                                ForEach(pass.unshipped, id: \.work.id) { u in
                                    VStack(alignment: .leading) {
                                        Text(u.work.title)
                                        Text(u.reason).font(.caption).foregroundStyle(.secondary)
                                    }
                                    .tag(Row.unshipped(work: u.work.id))
                                }
                            }
                        }
                        if !pass.cards.isEmpty {
                            Section("Taste promoted (\(pass.cards.count))") {
                                ForEach(pass.cards, id: \.card) { card in
                                    VStack(alignment: .leading) {
                                        Text(card.card).font(.callout.weight(.medium))
                                        Text(card.text).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
            } else if let failure {
                ContentUnavailableView("Cannot read the review", systemImage: "exclamationmark.triangle", description: Text(failure))
            } else {
                ProgressView()
            }
        }
        .navigationTitle("Review · \(project)")
        .navigationSubtitle(pass.map { "events \($0.since + 1)–\($0.through)" } ?? "")
        .toolbar {
            ToolbarItem {
                Button { plan = .acknowledge(project: project) } label: {
                    Label("Acknowledge", systemImage: "checkmark.circle")
                }
                .help("Mark this pass as seen; the next one starts after it")
                .disabled(pass?.isEmpty ?? true)
            }
        }
        .task(id: "\(project)-\(store.reads)") { await load() }
        .onChange(of: row) {
            selected = row?.work
        }
    }

    private func load() async {
        do {
            pass = try await store.review(project: project)
            failure = nil
        } catch {
            failure = "\(error)"
        }
    }
}
