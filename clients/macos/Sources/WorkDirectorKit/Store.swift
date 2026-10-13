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
    /// Told about each work that comes to wait on a person, once.
    public var arrived: ((Arrival) -> Void)?

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
                let earlier = board
                let now = try await channel.board()
                board = now
                reads += 1
                for a in now.arrivals(since: earlier) { arrived?(a) }
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

    public func record(_ id: String) async throws -> GoalRecord {
        try await channel.record(id)
    }

    /// The next gate on the work's way to done.
    public func gate(of id: String) async throws -> Gate {
        try await channel.gate(of: id)
    }

    public func conversation(_ id: String, from: Int, agent: String? = nil) async throws -> ConversationPage {
        try await channel.conversation(id, from: from, agent: agent)
    }

    public func review(project: String) async throws -> ReviewPass {
        let result = try await channel.action(["review", "--project", project, "--json"])
        return try decode(ReviewPass.self, result)
    }

    /// The rules in effect and what feedback has gathered.
    public func taste() async throws -> Taste {
        try decode(Taste.self, try await channel.action(["taste", "--json"]))
    }

    /// The runners this host can use.
    public func doctor() async throws -> Doctor {
        try decode(Doctor.self, try await channel.action(["doctor", "--json"]))
    }

    /// Reads the products again, after one is added.
    public func reloadProjects() async throws {
        projects = try decode([Project].self, try await channel.action(["projects", "--json"]))
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
        let started = try decode(Started.self, try await run(plan))
        await refresh()
        return started
    }

    /// Runs a stop plan and returns what it took back unread, oldest first, as
    /// one text to edit and send again; empty when nothing was waiting.
    public func stop(_ plan: Plan) async throws -> String {
        struct Stopped: Decodable { let withdrawn: [String] }
        let stopped = try decode(Stopped.self, try await channel.action(plan.argv))
        await refresh()
        return stopped.withdrawn.joined(separator: "\n\n")
    }

    /// Runs a confirmed plan: the argv the confirmation showed, unchanged.
    public func run(_ plan: Plan) async throws -> ActionResult {
        let flight = InFlight(plan: plan, at: Date())
        inFlight.append(flight)
        defer { inFlight.removeAll { $0.id == flight.id } }
        return try await channel.action(plan.argv)
    }

    /// Writes started here that have not come back, oldest first.
    public private(set) var inFlight: [InFlight] = []

    /// Messages sent to work, oldest first, from the moment they are sent.
    public private(set) var outbox: [Outgoing] = []

    /// Sends text to work without the person waiting on it: it is in the
    /// outbox at once, and `then` runs once `wd` has taken it.
    @discardableResult
    public func post(_ plan: Plan, to work: String, then: Plan? = nil) -> Outgoing {
        let message = Outgoing(id: UUID(), work: work, text: plan.argv.last ?? "", at: Date(), state: .sending, plan: plan, then: then)
        outbox.append(message)
        Task { await deliver(message.id) }
        return message
    }

    /// Sends a refused message again; one `wd` took is not sent twice.
    public func retry(_ id: UUID) {
        guard let i = outbox.firstIndex(where: { $0.id == id }), case .failed = outbox[i].state else { return }
        Task { await deliver(id) }
    }

    public func discard(_ id: UUID) {
        outbox.removeAll { $0.id == id }
    }

    private func deliver(_ id: UUID) async {
        guard let i = outbox.firstIndex(where: { $0.id == id }) else { return }
        let message = outbox[i]
        let alreadySent = if case .failed(_, sent: true) = message.state { true } else { false }
        outbox[i].state = .sending
        var state = Outgoing.State.sent
        do {
            if !alreadySent {
                let sent = try await run(message.plan)
                if sent.code != 0 {
                    state = .failed(sent.stderr.isEmpty ? sent.stdout : sent.stderr, sent: false)
                }
            }
            if case .sent = state, let then = message.then {
                let kept = try await run(then)
                if kept.code != 0 {
                    state = .failed("sent, but not remembered: " + (kept.stderr.isEmpty ? kept.stdout : kept.stderr), sent: true)
                }
            }
        } catch {
            state = .failed("\(error)", sent: alreadySent)
        }
        if let i = outbox.firstIndex(where: { $0.id == id }) {
            outbox[i].state = state
        }
        await refresh()
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
