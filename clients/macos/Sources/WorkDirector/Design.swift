import SwiftUI

/// The measures of docs/design/desktop.md, the one design every desktop app
/// renders. Views take their space, type, shape and colour from here and
/// nowhere else, so the apps read as one product.
enum Space {
    static let s1: CGFloat = 4
    static let s2: CGFloat = 8
    static let s3: CGFloat = 12
    static let s4: CGFloat = 16
    static let s5: CGFloat = 24
    static let s6: CGFloat = 32
    static let s7: CGFloat = 48
    /// The avatar column every turn opens with.
    static let avatar: CGFloat = 24
    /// Where a turn's body starts: past the avatar and its gap.
    static let turnIndent: CGFloat = avatar + s3
}

enum TypeScale {
    static let display = Font.system(size: 22, weight: .semibold)
    static let title = Font.system(size: 16, weight: .semibold)
    static let body = Font.system(size: 14)
    static let callout = Font.system(size: 13)
    static let calloutMedium = Font.system(size: 13, weight: .medium)
    static let meta = Font.system(size: 12, weight: .medium)
    static let micro = Font.system(size: 11, weight: .semibold)
    static let code = Font.system(size: 12.5, design: .monospaced)
    /// Extra leading that brings each size to its line height in the design.
    static let bodyLeading: CGFloat = 5
    static let codeLeading: CGFloat = 4
}

enum Radius {
    static let chip: CGFloat = 6
    static let card: CGFloat = 10
    static let bubble: CGFloat = 14
}

enum Palette {
    static let raised = Color(nsColor: .controlBackgroundColor)
    static let sunken = Color.primary.opacity(0.045)
    static let separator = Color(nsColor: .separatorColor)
    static let attention = Color.orange
    static let settled = Color.green
    static let failure = Color.red
}

extension View {
    /// The conversation's column: the window's whole width, with the page's
    /// gutters and room above the first turn.
    func readingColumn() -> some View {
        frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Space.s6)
            .padding(.top, Space.s6)
            .padding(.bottom, Space.s5)
    }

    /// Text read at length: everything said in a conversation.
    func readable() -> some View {
        font(TypeScale.body).lineSpacing(TypeScale.bodyLeading)
    }

    /// A box that means something: a filled, bordered card in a role's tint.
    func card(_ tint: Color? = nil, radius: CGFloat = Radius.card, padding: CGFloat = Space.s4) -> some View {
        self.padding(padding)
            .background(tint.map { $0.opacity(0.08) } ?? Palette.raised, in: RoundedRectangle(cornerRadius: radius))
            .overlay(RoundedRectangle(cornerRadius: radius).strokeBorder(tint.map { $0.opacity(0.3) } ?? Palette.separator))
    }
}

/// A small upper-case section label.
struct MicroLabel: View {
    let text: String
    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text.uppercased())
            .font(TypeScale.micro)
            .tracking(0.4)
            .foregroundStyle(.tertiary)
    }
}
