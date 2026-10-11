import Foundation

public enum ChannelError: Error, Equatable, CustomStringConvertible {
    /// wd exited while the request was outstanding, or before it was sent.
    case exited(status: Int32)
    /// wd answered the request with an error.
    case refused(kind: String, message: String)
    /// A line from wd was not the reply its request expected.
    case unreadable(String)

    public var description: String {
        switch self {
        case .exited(let status): "wd exited (status \(status))"
        case .refused(let kind, let message): "\(kind): \(message)"
        case .unreadable(let detail): "unreadable reply from wd: \(detail)"
        }
    }
}

/// Channel holds `wd serve --stdio` open: one JSON request per line in, one
/// reply or pushed event per line out.
public actor Channel {
    private let process: Process
    private let input: FileHandle
    private var nextID: Int64 = 1
    private var waiting: [Int64: CheckedContinuation<Data, any Error>] = [:]
    private var exitStatus: Int32?

    /// Every ledger event any wd process records, in id order, while wd runs.
    public nonisolated let events: AsyncStream<Event>
    private let eventSink: AsyncStream<Event>.Continuation

    /// Starts `wd serve --stdio`. environment replaces the inherited one when given.
    public init(wd: URL, environment: [String: String]? = nil) throws {
        let process = Process()
        process.executableURL = wd
        process.arguments = ["serve", "--stdio"]
        if let environment { process.environment = environment }
        let stdin = Pipe()
        let stdout = Pipe()
        process.standardInput = stdin
        process.standardOutput = stdout
        process.standardError = FileHandle.standardError
        (events, eventSink) = AsyncStream.makeStream(of: Event.self)
        self.process = process
        self.input = stdin.fileHandleForWriting

        // Chunks are yielded in the order the handler sees them, and one task
        // consumes them, so lines are read in the order wd wrote them.
        let (chunks, chunkSink) = AsyncStream.makeStream(of: Data.self)
        stdout.fileHandleForReading.readabilityHandler = { handle in
            let data = handle.availableData
            if data.isEmpty {
                handle.readabilityHandler = nil
                chunkSink.finish()
            } else {
                chunkSink.yield(data)
            }
        }
        process.terminationHandler = { [weak self] p in
            let status = p.terminationStatus
            Task { await self?.exited(status: status) }
        }
        try process.run()
        Task { [weak self] in
            var buffer = Data()
            for await chunk in chunks {
                buffer.append(chunk)
                while let newline = buffer.firstIndex(of: 0x0A) {
                    let line = buffer[buffer.startIndex..<newline]
                    buffer = Data(buffer[buffer.index(after: newline)...])
                    await self?.receive(Data(line))
                }
            }
        }
    }

    /// Ends wd by closing its input; it answers what it holds and exits.
    public func close() throws {
        try input.close()
    }

    public func board() async throws -> Board {
        try await request("board", .none)
    }

    public func goal(_ id: String) async throws -> GoalDetail {
        try await request("goal", .id(id))
    }

    public func work(_ id: String) async throws -> Work {
        try await request("work", .id(id))
    }

    public func events(of id: String) async throws -> [Event] {
        try await request("events", .id(id))
    }

    /// The work's session steps from index `from` on.
    public func conversation(_ id: String, from: Int) async throws -> ConversationPage {
        try await request("conversation", .conversation(id, from: from))
    }

    /// Runs `wd` with argv, exactly as given.
    public func action(_ argv: [String]) async throws -> ActionResult {
        try await request("action", .argv(argv))
    }

    private enum Params: Encodable {
        case none
        case id(String)
        case argv([String])
        case conversation(String, from: Int)

        enum CodingKeys: String, CodingKey { case id, argv, from }

        func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            switch self {
            case .none: break
            case .id(let id): try c.encode(id, forKey: .id)
            case .argv(let argv): try c.encode(argv, forKey: .argv)
            case .conversation(let id, let from):
                try c.encode(id, forKey: .id)
                try c.encode(from, forKey: .from)
            }
        }
    }

    private struct Request: Encodable {
        let id: Int64
        let method: String
        let params: Params
    }

    private struct Reply<T: Decodable>: Decodable {
        let result: T?
        let error: Refusal?
    }

    private struct Refusal: Decodable {
        let kind: String
        let message: String
    }

    private struct Head: Decodable {
        let id: Int64?
        let push: String?
    }

    private struct Push: Decodable {
        let data: Event
    }

    private func request<T: Decodable>(_ method: String, _ params: Params) async throws -> T {
        if let exitStatus { throw ChannelError.exited(status: exitStatus) }
        let id = nextID
        nextID += 1
        var line = try JSONEncoder().encode(Request(id: id, method: method, params: params))
        line.append(0x0A)
        let data = try await withCheckedThrowingContinuation { (c: CheckedContinuation<Data, any Error>) in
            waiting[id] = c
            do {
                try input.write(contentsOf: line)
            } catch {
                waiting[id] = nil
                c.resume(throwing: error)
            }
        }
        let reply: Reply<T>
        do {
            reply = try JSONDecoder().decode(Reply<T>.self, from: data)
        } catch {
            throw ChannelError.unreadable("\(method): \(error)")
        }
        if let refusal = reply.error {
            throw ChannelError.refused(kind: refusal.kind, message: refusal.message)
        }
        guard let result = reply.result else {
            throw ChannelError.unreadable("\(method): a reply with neither result nor error")
        }
        return result
    }

    private func receive(_ line: Data) {
        guard let head = try? JSONDecoder().decode(Head.self, from: line) else {
            // A line wd wrote that is not JSON at all: nothing can be matched
            // to it, so every waiting request learns the channel is broken.
            failAll(ChannelError.unreadable(String(decoding: line, as: UTF8.self)))
            return
        }
        if let id = head.id, let c = waiting.removeValue(forKey: id) {
            c.resume(returning: line)
        } else if head.push == "event" {
            do {
                eventSink.yield(try JSONDecoder().decode(Push.self, from: line).data)
            } catch {
                failAll(ChannelError.unreadable("pushed event: \(error)"))
            }
        }
    }

    private func exited(status: Int32) {
        exitStatus = status
        failAll(ChannelError.exited(status: status))
        eventSink.finish()
    }

    private func failAll(_ error: ChannelError) {
        let pending = waiting
        waiting = [:]
        for c in pending.values { c.resume(throwing: error) }
    }
}
