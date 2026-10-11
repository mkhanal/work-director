import Foundation
import Observation

/// Store is what the window shows, read from wd and nowhere else. It keeps no
/// state of its own that matters: quit and relaunch, and the ledger rebuilds it.
@MainActor
@Observable
public final class Store {
    public private(set) var board: Board?
    public private(set) var projects: [Project] = []
    /// The last failure to read from wd, shown until the next read succeeds.
    public private(set) var failure: String?
    /// How many times the board has been read; a view can watch it change.
    public private(set) var reads = 0

    private let channel: Channel
    private var following: Task<Void, Never>?
    private var refreshing = false
    private var stale = false

    public init(channel: Channel) {
        self.channel = channel
    }

    /// Reads the board and the projects, then follows every ledger event.
    public func start() async {
        await refresh()
        do {
            projects = try decode([Project].self, try await channel.action(["projects", "--json"]))
        } catch {
            failure = "\(error)"
        }
        following = Task { [weak self, channel] in
            for await _ in channel.events {
                await self?.refresh()
            }
            self?.failure = "wd stopped; relaunch the app"
        }
    }

    public func stop() async {
        following?.cancel()
        try? await channel.close()
    }

    /// Reads the board again. Events arriving during a read coalesce into one
    /// more read, so a burst of events never queues a burst of reads.
    public func refresh() async {
        if refreshing {
            stale = true
            return
        }
        refreshing = true
        defer { refreshing = false }
        repeat {
            stale = false
            do {
                board = try await channel.board()
                reads += 1
                failure = nil
            } catch {
                failure = "\(error)"
            }
        } while stale
    }

    public func goal(_ id: String) async throws -> GoalDetail {
        try await channel.goal(id)
    }

    public func work(_ id: String) async throws -> Work {
        try await channel.work(id)
    }

    public func events(of id: String) async throws -> [Event] {
        try await channel.events(of: id)
    }

    public func conversation(_ id: String, from: Int) async throws -> ConversationPage {
        try await channel.conversation(id, from: from)
    }

    public func review(project: String) async throws -> ReviewPass {
        let result = try await channel.action(["review", "--project", project, "--json"])
        return try decode(ReviewPass.self, result)
    }

    /// Work put out of sight, newest first as wd lists it.
    public func archived() async throws -> [Work] {
        try decode([Work].self, try await channel.action(["status", "--archived", "--json"]))
    }

    /// Asks one bounded model call which of a product's goals a request continues.
    public func find(project: String, text: String) async throws -> [Found] {
        try decode([Found].self, try await channel.action(["goal", "find", project, text, "--json"]))
    }

    /// Runs a start or continue plan and reads back the goal and its tasks.
    public func start(_ plan: Plan) async throws -> Started {
        let started = try decode(Started.self, try await channel.action(plan.argv))
        await refresh()
        return started
    }

    /// Runs a confirmed plan: the argv the confirmation showed, unchanged.
    public func run(_ plan: Plan) async throws -> ActionResult {
        try await channel.action(plan.argv)
    }

    private func decode<T: Decodable>(_ type: T.Type, _ result: ActionResult) throws -> T {
        guard result.code == 0 else {
            throw ChannelError.refused(kind: "exit \(result.code)", message: result.stderr.isEmpty ? result.stdout : result.stderr)
        }
        do {
            return try JSONDecoder().decode(T.self, from: Data(result.stdout.utf8))
        } catch {
            throw ChannelError.unreadable("\(T.self): \(error)")
        }
    }
}
