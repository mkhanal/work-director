import Foundation

/// The sidebar's bands: top goals and standalone work by whether they need
/// the person, work or are completed. Work let go of (dropped, abandoned,
/// superseded) is kept apart, folded.
public struct SidebarBands: Sendable, Hashable {
    public let needsYou: [SidebarRow]
    public let working: [SidebarRow]
    public let completed: [SidebarRow]
    public let letGo: [SidebarRow]

    public static let none = SidebarBands(needsYou: [], working: [], completed: [], letGo: [])
}

/// One top goal or standalone item as the sidebar shows it.
public struct SidebarRow: Sendable, Hashable, Identifiable {
    public let work: Work
    public let band: Band
    public let progress: TreeProgress?
    public let needsYou: Int
    public let finish: FinishLine?
    public let finished: String?

    public var id: String { work.id }

    /// The line under its title: where it finished once done, else how far
    /// its sub-goals have come, else where it is meant to finish.
    public var meta: String {
        if work.state == .done { return finished ?? "Done" }
        if let progress, progress.total > 0 { return "\(progress.done) of \(progress.total)" }
        if let finish { return finish.line.prefix(1).uppercased() + finish.line.dropFirst() }
        return work.kind.isGoal ? "Not planned yet" : work.state.look.label
    }

    /// The badge after the meta line while questions below it wait on the person.
    public var needsYouBadge: String? { needsYou > 0 ? "\(needsYou) needs you" : nil }

    /// The goals under this row down to the open one, when the open goal's
    /// breadcrumb starts here.
    public func openPath(_ chain: [ChainLink]) -> [ChainLink] {
        chain.first?.id == work.id ? Array(chain.dropFirst()) : []
    }
}

extension Board {
    /// The sidebar within one product when one is named, each band most
    /// recently active first.
    public func sidebar(project: String? = nil) -> SidebarBands {
        let rows = goals.map { SidebarRow(work: $0.work, band: $0.band, progress: $0.progress, needsYou: $0.needsYou, finish: $0.finish, finished: $0.finished) }
            + standalone.map { SidebarRow(work: $0.work, band: $0.band, progress: nil, needsYou: 0, finish: nil, finished: nil) }
        let shown = rows
            .filter { project == nil || $0.work.project == project }
            .sorted { $0.work.updated > $1.work.updated }
        let letGo: Set<WorkState> = [.dropped, .abandoned, .superseded]
        return SidebarBands(
            needsYou: shown.filter { !$0.work.atRest && ($0.band == .needsYou || $0.needsYou > 0) },
            working: shown.filter { !$0.work.atRest && $0.band != .needsYou && $0.needsYou == 0 },
            completed: shown.filter { $0.work.state == .done },
            letGo: shown.filter { letGo.contains($0.work.state) })
    }
}
