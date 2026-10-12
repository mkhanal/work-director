import SwiftUI
import WorkDirectorKit

/// The taste every brief carries, rule by rule, and what corrections have
/// gathered toward new rules: where a person sees what the director learned.
struct TastePage: View {
    let store: Store
    let scope: String?
    @Binding var plan: Plan?
    @State private var taste: Taste?
    @State private var failure: String?
    @State private var note = ""

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Space.s5) {
                Text("Taste").font(TypeScale.display.weight(.semibold))
                Text("The rules every brief carries, and what your corrections have gathered toward new ones. Rules of your own live in ~/.work-director/taste/cards.")
                    .foregroundStyle(.secondary)
                if let failure {
                    Label(failure, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red).textSelection(.enabled)
                }
                if let taste {
                    learned(taste.learned)
                    ForEach(categories(taste.cards), id: \.self) { category in
                        VStack(alignment: .leading, spacing: Space.s2) {
                            Text(category.capitalized.replacingOccurrences(of: "-", with: " "))
                                .font(TypeScale.headline)
                            ForEach(taste.cards.filter { $0.category == category }) { card in
                                CardRow(card: card)
                            }
                        }
                    }
                } else if failure == nil {
                    ProgressView()
                }
            }
            .readingColumn()
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .safeAreaInset(edge: .bottom) {
            InputBar(text: $note,
                     placeholder: "Teach the director something… it is kept as feedback" + (scope.map { " on \($0)" } ?? ""),
                     command: (try? Plan.feedback(note, project: scope))?.commandLine,
                     busy: false) {
                if let made = try? Plan.feedback(note, project: scope) {
                    plan = made
                    note = ""
                }
            }
        }
        .navigationTitle("Taste")
        .task(id: store.reads) { await load() }
    }

    @ViewBuilder
    private func learned(_ groups: [Taste.Learned]) -> some View {
        if !groups.isEmpty {
            VStack(alignment: .leading, spacing: Space.s2) {
                Label("Learned, not yet a rule", systemImage: "lightbulb").font(TypeScale.headline)
                ForEach(groups) { g in
                    DisclosureGroup {
                        VStack(alignment: .leading, spacing: Space.s1) {
                            ForEach(g.texts, id: \.self) { t in
                                Text("• " + t).font(TypeScale.callout).textSelection(.enabled)
                            }
                        }
                        .padding(.top, Space.s1)
                    } label: {
                        HStack {
                            Text(g.key).font(TypeScale.callout.monospaced())
                            Text("×\(g.count)").font(TypeScale.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
            .padding(Space.s3)
            .background(.yellow.opacity(0.08), in: RoundedRectangle(cornerRadius: Radius.chip + 2))
        }
    }

    private func categories(_ cards: [Taste.Card]) -> [String] {
        var seen: [String] = []
        for c in cards where !seen.contains(c.category) { seen.append(c.category) }
        return seen
    }

    private func load() async {
        do {
            taste = try await store.taste()
            failure = nil
        } catch {
            failure = "\(error)"
        }
    }
}

private struct CardRow: View {
    let card: Taste.Card

    var body: some View {
        VStack(alignment: .leading, spacing: Space.s1) {
            HStack(spacing: Space.s2) {
                Text(card.title).font(TypeScale.callout.weight(.semibold))
                if card.source == "yours" {
                    Text("yours").font(TypeScale.small).padding(.horizontal, 5).padding(.vertical, 1)
                        .background(Color.accentColor.opacity(0.15), in: Capsule())
                }
                if card.always {
                    Text("always").font(TypeScale.small).foregroundStyle(.secondary)
                }
                Spacer()
                Text(card.scope.joined(separator: ", ")).font(TypeScale.caption.monospaced()).foregroundStyle(.tertiary)
            }
            Text(card.statement).font(TypeScale.callout).foregroundStyle(.secondary).textSelection(.enabled)
        }
        .padding(.vertical, 2)
    }
}
