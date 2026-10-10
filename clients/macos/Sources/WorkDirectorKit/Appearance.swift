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
