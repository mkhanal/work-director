import Foundation

/// The modifier keys held with Return.
public struct Chord: OptionSet, Sendable, Hashable {
    public let rawValue: Int
    public init(rawValue: Int) { self.rawValue = rawValue }

    public static let shift = Chord(rawValue: 1 << 0)
    public static let command = Chord(rawValue: 1 << 1)
    public static let option = Chord(rawValue: 1 << 2)
    public static let control = Chord(rawValue: 1 << 3)
}

/// What Return does in a message: send it, or start a new line.
public enum ReturnPress: Sendable, Hashable {
    case send, newline
}

/// The chord a person sends a message with. Every other chord with Return
/// starts a new line, so a long message never goes out half written.
public enum SendKey: String, CaseIterable, Sendable, Identifiable {
    case `return`, commandReturn, optionReturn

    public static let standard = SendKey.return

    public var id: String { rawValue }

    public var chord: Chord {
        switch self {
        case .return: []
        case .commandReturn: .command
        case .optionReturn: .option
        }
    }

    public var title: String {
        switch self {
        case .return: "Return sends, Shift-Return for a new line"
        case .commandReturn: "⌘Return sends, Return for a new line"
        case .optionReturn: "⌥Return sends, Return for a new line"
        }
    }

    /// The key shown beside the send button.
    public var symbol: String {
        switch self {
        case .return: "↩"
        case .commandReturn: "⌘↩"
        case .optionReturn: "⌥↩"
        }
    }

    public func press(_ held: Chord) -> ReturnPress {
        held == chord ? .send : .newline
    }
}
