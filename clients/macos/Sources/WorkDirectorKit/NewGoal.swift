import Foundation
import Observation

/// Starting a goal, the way a conversation starts: say what you want. Before
/// anything starts, one model call looks for the goals it may continue, so the
/// person can pick one up instead of filing a duplicate.
@MainActor
@Observable
public final class NewGoal {
    public enum Phase: Equatable, Sendable {
        case idle
        case finding
        case offering([Found])
        case starting
        case started(Started)
        case failed(String)
    }

    public private(set) var phase: Phase = .idle
    private let store: Store

    public init(store: Store) {
        self.store = store
    }

    /// Looks for goals the request continues; with none, starts it at once.
    public func submit(project: String, text: String, files: [Attaching] = []) async {
        // Files alone say nothing to match a goal on.
        if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            await startNew(project: project, text: text, files: files)
            return
        }
        phase = .finding
        do {
            let found = try await store.find(project: project, text: text)
            if found.isEmpty {
                await startNew(project: project, text: text, files: files)
            } else {
                phase = .offering(found)
            }
        } catch {
            phase = .failed(String(describing: error))
        }
    }

    public func startNew(project: String, text: String, files: [Attaching] = []) async {
        await run { try Plan.start(project: project, text: text, files: files) }
    }

    public func continueGoal(_ id: String, text: String, files: [Attaching] = []) async {
        await run { try Plan.continueGoal(id, text: text, files: files) }
    }

    public func reset() {
        phase = .idle
    }

    private func run(_ plan: () throws -> Plan) async {
        phase = .starting
        do {
            phase = .started(try await store.start(try plan()))
        } catch {
            phase = .failed(String(describing: error))
        }
    }
}
