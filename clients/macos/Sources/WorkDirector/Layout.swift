import SwiftUI

/// The measures every page reads by: the window's full width less its
/// gutters, room between what was said, and text sized and spaced to be read
/// at length.
enum Layout {
    /// Space from the window's edges to the column.
    static let gutter: CGFloat = 32
    /// Space between one thing said and the next.
    static let step: CGFloat = 22
    static let body = Font.system(size: 14)
    static let lineSpacing: CGFloat = 4
}

extension View {
    /// The page's column: the window's whole width, with room above and to
    /// the sides.
    func readingColumn() -> some View {
        frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Layout.gutter)
            .padding(.vertical, 28)
    }

    /// Text read at length.
    func readable() -> some View {
        font(Layout.body).lineSpacing(Layout.lineSpacing)
    }
}
