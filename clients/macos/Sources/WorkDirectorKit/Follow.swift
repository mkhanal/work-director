import CoreGraphics

/// Whether a conversation keeps its newest message in view: it does while the
/// reader is at the bottom, stops when they scroll up to read, and starts
/// again once they are back.
public enum Follow {
    /// How far above the very bottom still counts as at the bottom, so a
    /// reader a few points short of it is not left behind.
    public static let slack: CGFloat = 48

    public static func atBottom(offset: CGFloat, visible: CGFloat, content: CGFloat) -> Bool {
        content <= visible || offset + visible >= content - slack
    }
}
