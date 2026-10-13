/// Where a goal or sub-goal stands, as its icon on a goal page.
public enum StateMark: Sendable, Hashable {
    case done, needsYou, working, waiting, superseded

    public var label: String {
        switch self {
        case .done: "Done"
        case .needsYou: "Needs you"
        case .working: "Working"
        case .waiting: "Waiting"
        case .superseded: "Superseded"
        }
    }
}

/// A badge's colour role.
public enum Tone: Sendable, Hashable {
    case attention, settled, live, waiting, decision
}

/// A badge: its words and its colour role.
public struct GoalBadge: Sendable, Hashable {
    public let text: String
    public let tone: Tone

    public init(_ text: String, _ tone: Tone) {
        self.text = text
        self.tone = tone
    }
}

extension WorkState {
    public var mark: StateMark {
        switch self {
        case .done: .done
        case .needsInput, .blocked: .needsYou
        case .running, .review, .softDone: .working
        case .queued, .briefed, .paused: .waiting
        case .dropped, .abandoned, .superseded: .superseded
        }
    }

    /// The badge beside a goal's title or a sub-goal's row.
    public var badge: GoalBadge {
        switch self {
        case .done: GoalBadge("Done", .settled)
        case .needsInput: GoalBadge("Needs you", .attention)
        case .blocked: GoalBadge("Blocked", .attention)
        case .running: GoalBadge("Working", .live)
        case .review: GoalBadge("In review", .live)
        case .softDone: GoalBadge("Ready to close", .live)
        case .queued, .briefed: GoalBadge("Waiting", .waiting)
        case .paused: GoalBadge("Paused", .waiting)
        case .dropped: GoalBadge("Dropped", .waiting)
        case .abandoned: GoalBadge("Abandoned", .waiting)
        case .superseded: GoalBadge("Superseded", .waiting)
        }
    }
}

/// Tint names a system colour; the app maps it, so this layer stays free of SwiftUI.
public enum Tint: String, Sendable {
    case gray, blue, indigo, orange, red, purple, teal, green, brown
}

/// How one state reads on screen. Each state has its own symbol and label,
/// so needs-input (a question waiting) is never mistaken for blocked (a stop).
public struct StateLook: Sendable, Hashable {
    public let label: String
    public let symbol: String
    public let tint: Tint
}

extension WorkState {
    public var look: StateLook {
        switch self {
        case .queued: StateLook(label: "Queued", symbol: "circle.dotted", tint: .gray)
        case .briefed: StateLook(label: "Briefed", symbol: "doc.text", tint: .gray)
        case .running: StateLook(label: "Running", symbol: "play.circle", tint: .blue)
        case .needsInput: StateLook(label: "Needs input", symbol: "questionmark.bubble", tint: .orange)
        case .blocked: StateLook(label: "Blocked", symbol: "hand.raised", tint: .red)
        case .review: StateLook(label: "In review", symbol: "eye", tint: .indigo)
        case .softDone: StateLook(label: "Ready to close", symbol: "checkmark.circle", tint: .teal)
        case .done: StateLook(label: "Done", symbol: "checkmark.seal", tint: .green)
        case .dropped: StateLook(label: "Dropped", symbol: "minus.circle", tint: .gray)
        case .abandoned: StateLook(label: "Abandoned", symbol: "xmark.octagon", tint: .brown)
        case .paused: StateLook(label: "Paused", symbol: "pause.circle", tint: .purple)
        case .superseded: StateLook(label: "Superseded", symbol: "minus.circle.fill", tint: .gray)
        }
    }
}

extension Band {
    public var label: String {
        switch self {
        case .needsYou: "Needs You"
        case .inFlight: "In Flight"
        case .paused: "Paused"
        case .readyToPush: "Ready to Push"
        case .inReview: "In Review"
        case .readyToClose: "Ready to Close"
        case .atRest: "At Rest"
        }
    }

    public var symbol: String {
        switch self {
        case .needsYou: "person.crop.circle.badge.exclamationmark"
        case .inFlight: "airplane"
        case .paused: "pause.circle"
        case .readyToPush: "arrow.up.circle"
        case .inReview: "eye"
        case .readyToClose: "checkmark.circle"
        case .atRest: "moon.zzz"
        }
    }
}

/// The look the person chose for the app: the system's, or always light or dark.
public enum AppearanceChoice: String, CaseIterable, Sendable {
    case system, light, dark

    public var title: String {
        switch self {
        case .system: "System"
        case .light: "Light"
        case .dark: "Dark"
        }
    }

    /// Whether it forces dark (true) or light (false); nil follows the system.
    public var dark: Bool? {
        switch self {
        case .system: nil
        case .light: false
        case .dark: true
        }
    }
}
