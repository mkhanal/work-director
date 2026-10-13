import AppKit
import SwiftUI
import WorkDirectorKit

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
    /// The page's sides, and the gap between its sections.
    static let gutter: CGFloat = 32
    static let section: CGFloat = 20
    /// Inside a card: top and bottom, then the sides.
    static let cardV: CGFloat = 14
    static let cardH: CGFloat = 16
    static let sidebarWidth: CGFloat = 268
    static let toolbarHeight: CGFloat = 52
}

enum TypeScale {
    /// The new-goal page's question.
    static let hero = Font.system(size: 26, weight: .semibold)
    static let display = Font.system(size: 22, weight: .semibold)
    /// A summary card's number.
    static let figure = Font.system(size: 20, weight: .semibold)
    static let title = Font.system(size: 16, weight: .semibold)
    static let headline = Font.system(size: 14, weight: .semibold)
    static let body = Font.system(size: 14)
    static let bodyMedium = Font.system(size: 14, weight: .medium)
    static let callout = Font.system(size: 13)
    static let calloutMedium = Font.system(size: 13, weight: .medium)
    static let calloutStrong = Font.system(size: 13, weight: .semibold)
    static let caption = Font.system(size: 12)
    static let meta = Font.system(size: 12, weight: .medium)
    static let small = Font.system(size: 11)
    /// A sub-goal tag on a turn.
    static let tagMedium = Font.system(size: 11, weight: .medium)
    static let micro = Font.system(size: 11, weight: .semibold)
    /// Pills and tags: a verdict, a kind, Recommended.
    static let tag = Font.system(size: 10.5, weight: .semibold)
    static let code = Font.system(size: 12.5, design: .monospaced)
    static let codeSmall = Font.system(size: 12, design: .monospaced)
    static let codeTiny = Font.system(size: 11, design: .monospaced)
    /// Extra leading that brings each size to its line height in the design.
    static let bodyLeading: CGFloat = 5
    static let codeLeading: CGFloat = 4
    /// 13/19 and 13/20 lines: done-when text, steps, record rows.
    static let calloutLeading: CGFloat = 4
}

/// Symbol sizes, set apart from text so a symbol never borrows a text size.
enum IconSize {
    static let tiny: CGFloat = 9
    static let small: CGFloat = 11.5
    static let control: CGFloat = 13
    static let toolbar: CGFloat = 14
    static let turn: CGFloat = 15
    static let avatar: CGFloat = 17
    static let state: CGFloat = 20
}

enum Radius {
    static let badge: CGFloat = 6
    static let chip: CGFloat = 6
    static let button: CGFloat = 7
    static let option: CGFloat = 8
    static let card: CGFloat = 10
    static let bubble: CGFloat = 14
}

extension Color {
    /// A colour with a light and a dark value, resolved by the window's appearance.
    init(light: UInt32, dark: UInt32) {
        self.init(nsColor: NSColor(name: nil) { appearance in
            let hex = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua ? dark : light
            return NSColor(srgbRed: CGFloat(hex >> 16 & 0xff) / 255, green: CGFloat(hex >> 8 & 0xff) / 255,
                           blue: CGFloat(hex & 0xff) / 255, alpha: 1)
        })
    }
}

/// The canvas's colours, light and dark, as docs/design/desktop.md lists them.
enum Palette {
    static let page = Color(light: 0xffffff, dark: 0x1e1e1e)
    static let sidebar = Color(light: 0xf2f2f6, dark: 0x26262a)
    static let sidebarSelected = Color(light: 0xdcdce3, dark: 0x3a3a40)
    static let search = Color(light: 0xe6e6ec, dark: 0x323237)
    static let hairline = Color(light: 0xe3e3e8, dark: 0x38383d)
    static let inner = Color(light: 0xececf1, dark: 0x2e2e33)
    static let rowSeparator = Color(light: 0xf0f0f3, dark: 0x2a2a2f)
    static let buttonBorder = Color(light: 0xd6d6dc, dark: 0x48484e)
    static let text = Color(light: 0x1d1d1f, dark: 0xf2f2f7)
    static let text2 = Color(light: 0x55555b, dark: 0xb4b4ba)
    static let text3 = Color(light: 0x6c6c72, dark: 0x8e8e95)
    static let stepText = Color(light: 0x3a3a3f, dark: 0xd0d0d6)
    static let accent = Color(light: 0x0a64d6, dark: 0x3d8bf0)
    static let link = Color(light: 0x0a5cc7, dark: 0x5aa0ff)
    static let green = Color(light: 0x1f9d55, dark: 0x34c46e)
    static let orange = Color(light: 0xe8820c, dark: 0xf0943a)
    static let grey = Color(light: 0x9a9aa0, dark: 0x7c7c82)
    static let questionBorder = Color(light: 0xf3c99a, dark: 0x6b4a22)
    static let questionFill = Color(light: 0xfff8f0, dark: 0x2b231a)
    static let needsYouRow = Color(light: 0xfffaf4, dark: 0x2a2219)
    static let bubbleFill = Color(light: 0xe5effc, dark: 0x1c2d45)
    static let bubbleBorder = Color(light: 0xbcd3f3, dark: 0x2f4a70)
    static let tagFill = Color(light: 0xf2f2f6, dark: 0x2c2c31)
    static let answeredFill = Color(light: 0xf0faf3, dark: 0x1b2a20)
    static let answeredBorder = Color(light: 0xcdebd7, dark: 0x2b4a35)
    static let plainCard = Color(light: 0xfafafc, dark: 0x232327)
    static let chipOn = Color(light: 0x1d1d1f, dark: 0xf2f2f7)
    static let chipOnText = Color(light: 0xffffff, dark: 0x1d1d1f)

    static let raised = page
    static let sunken = Color.primary.opacity(0.045)
    static let separator = hairline
    static let attention = orange
    static let settled = green
    static let failure = Color(light: 0xd70015, dark: 0xff6961)
}

/// The five badge colours of the canvas.
enum BadgeTone {
    case attention, settled, live, waiting, decision

    var fill: Color {
        switch self {
        case .attention: Color(light: 0xfff1e0, dark: 0x42301b)
        case .settled: Color(light: 0xe6f6ec, dark: 0x1e3528)
        case .live: Color(light: 0xe8effc, dark: 0x1a2b3f)
        case .waiting: Color(light: 0xefeff3, dark: 0x343435)
        case .decision: Color(light: 0xf1ebfb, dark: 0x292037)
        }
    }

    var text: Color {
        switch self {
        case .attention: Color(light: 0x9a3b00, dark: 0xffb067)
        case .settled: Color(light: 0x11692f, dark: 0x6fdc95)
        case .live: Color(light: 0x0a4fb0, dark: 0x8cb8ff)
        case .waiting: Color(light: 0x55555b, dark: 0xc2c2c8)
        case .decision: Color(light: 0x5b2aa8, dark: 0xc4a6ff)
        }
    }
}

extension BadgeTone {
    init(_ tone: Tone) {
        switch tone {
        case .attention: self = .attention
        case .settled: self = .settled
        case .live: self = .live
        case .waiting: self = .waiting
        case .decision: self = .decision
        }
    }
}

/// A small coloured label: a state, a verdict, a count that needs you.
struct Badge: View {
    let text: String
    let tone: BadgeTone

    init(_ text: String, _ tone: BadgeTone) {
        self.text = text
        self.tone = tone
    }

    init(_ badge: GoalBadge) {
        self.init(badge.text, BadgeTone(badge.tone))
    }

    var body: some View {
        Text(text)
            .font(TypeScale.tag)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(tone.fill, in: RoundedRectangle(cornerRadius: Radius.badge))
            .foregroundStyle(tone.text)
    }
}

/// Where a sub-goal stands, as the canvas's 20-point icon.
struct StateIcon: View {
    let mark: StateMark
    var size: CGFloat = IconSize.state

    var body: some View {
        Canvas { ctx, box in
            let s = box.width / 20
            let ring = Path(ellipseIn: CGRect(x: 2 * s, y: 2 * s, width: 16 * s, height: 16 * s))
            switch mark {
            case .done:
                ctx.fill(Path(ellipseIn: CGRect(x: 1 * s, y: 1 * s, width: 18 * s, height: 18 * s)), with: .color(Palette.green))
                var check = Path()
                check.move(to: CGPoint(x: 6 * s, y: 10.5 * s))
                check.addLine(to: CGPoint(x: 8.6 * s, y: 13 * s))
                check.addLine(to: CGPoint(x: 14 * s, y: 7.5 * s))
                ctx.stroke(check, with: .color(.white), lineWidth: 2 * s)
            case .needsYou:
                ctx.stroke(ring, with: .color(Palette.orange), lineWidth: 2 * s)
                ctx.fill(Path(ellipseIn: CGRect(x: 7 * s, y: 7 * s, width: 6 * s, height: 6 * s)), with: .color(Palette.orange))
            case .working:
                ctx.stroke(ring, with: .color(Palette.accent), lineWidth: 2 * s)
                var quarter = Path()
                quarter.move(to: CGPoint(x: 10 * s, y: 10 * s))
                quarter.addLine(to: CGPoint(x: 10 * s, y: 2 * s))
                quarter.addArc(center: CGPoint(x: 10 * s, y: 10 * s), radius: 8 * s, startAngle: .degrees(-90), endAngle: .degrees(0), clockwise: false)
                quarter.closeSubpath()
                ctx.fill(quarter, with: .color(Palette.accent))
            case .waiting:
                ctx.stroke(ring, with: .color(Palette.grey), style: StrokeStyle(lineWidth: 2 * s, dash: [3 * s, 3 * s]))
            case .superseded:
                ctx.stroke(ring, with: .color(Palette.grey), lineWidth: 2 * s)
                var bar = Path()
                bar.move(to: CGPoint(x: 5 * s, y: 10 * s))
                bar.addLine(to: CGPoint(x: 15 * s, y: 10 * s))
                ctx.stroke(bar, with: .color(Palette.grey), lineWidth: 2 * s)
            }
        }
        .frame(width: size, height: size)
        .accessibilityLabel(mark.label)
    }
}

/// Done of total as the canvas's thin green bar.
struct ProgressBar: View {
    let done: Int
    let total: Int

    var body: some View {
        ZStack(alignment: .leading) {
            RoundedRectangle(cornerRadius: 2).fill(Palette.inner)
            RoundedRectangle(cornerRadius: 2).fill(Palette.green)
                .frame(width: total > 0 ? 120 * CGFloat(done) / CGFloat(total) : 0)
        }
        .frame(width: 120, height: 4)
        .accessibilityLabel("\(done) of \(total) done")
    }
}

/// A chip that narrows a list to one kind.
struct FilterChip: View {
    let label: String
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(TypeScale.caption)
                .lineLimit(1)
                .fixedSize()
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .foregroundStyle(selected ? Palette.chipOnText : Palette.stepText)
                .background(selected ? Palette.chipOn : Palette.page, in: Capsule())
                .overlay(Capsule().strokeBorder(selected ? Palette.chipOn : Palette.hairline))
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
    }
}

/// Where in a goal's tree a turn was said.
struct SubGoalTag: View {
    let text: String

    var body: some View {
        Text(text)
            .font(TypeScale.tagMedium)
            .lineLimit(1)
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .foregroundStyle(Palette.stepText)
            .background(Palette.tagFill, in: RoundedRectangle(cornerRadius: Radius.badge))
            .overlay(RoundedRectangle(cornerRadius: Radius.badge).strokeBorder(Palette.hairline))
    }
}

/// A card's small label: Done when, Finishes at, Course.
struct CardLabel: View {
    let text: String
    var color: Color = Palette.text3
    init(_ text: String, color: Color = Palette.text3) {
        self.text = text
        self.color = color
    }

    var body: some View {
        Text(text).font(TypeScale.meta).foregroundStyle(color)
    }
}

/// The canvas's buttons: a hairline outline, or filled with the accent.
struct CanvasButtonStyle: ButtonStyle {
    var primary = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(primary ? TypeScale.calloutMedium : TypeScale.callout)
            .lineLimit(1)
            .padding(.horizontal, 12)
            .padding(.vertical, 5)
            .foregroundStyle(primary ? Color.white : Palette.text)
            .background(primary ? Palette.accent : Palette.page, in: RoundedRectangle(cornerRadius: Radius.button))
            .overlay(RoundedRectangle(cornerRadius: Radius.button).strokeBorder(primary ? Palette.accent : Palette.buttonBorder))
            .opacity(configuration.isPressed ? 0.75 : 1)
            .contentShape(RoundedRectangle(cornerRadius: Radius.button))
    }
}

extension ButtonStyle where Self == CanvasButtonStyle {
    static var canvas: CanvasButtonStyle { CanvasButtonStyle() }
    static var canvasPrimary: CanvasButtonStyle { CanvasButtonStyle(primary: true) }
}

extension View {
    /// The conversation's column: the window's whole width, with the page's
    /// gutters and room above the first turn.
    func readingColumn() -> some View {
        frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Space.gutter)
            .padding(.vertical, Space.section)
    }

    /// Text read at length: everything said in a conversation.
    func readable() -> some View {
        font(TypeScale.body).lineSpacing(TypeScale.bodyLeading)
    }

    /// A box that means something: a hairline card, or one filled in a role's tint.
    func card(_ tint: Color? = nil, radius: CGFloat = Radius.card, padding: CGFloat = Space.s4) -> some View {
        self.padding(padding)
            .background(tint.map { $0.opacity(0.08) } ?? Palette.page, in: RoundedRectangle(cornerRadius: radius))
            .overlay(RoundedRectangle(cornerRadius: radius).strokeBorder(tint.map { $0.opacity(0.3) } ?? Palette.hairline))
    }

    /// The canvas's card: hairline border, radius 10, padding 14 / 16.
    func canvasCard(fill: Color = Palette.page, padding: EdgeInsets = EdgeInsets(top: Space.cardV, leading: Space.cardH, bottom: Space.cardV, trailing: Space.cardH)) -> some View {
        self.padding(padding)
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .background(fill, in: RoundedRectangle(cornerRadius: Radius.card))
            .overlay(RoundedRectangle(cornerRadius: Radius.card).strokeBorder(Palette.hairline))
    }

    /// The question card's frame: orange border and fill with a 3-point
    /// leading bar.
    func questionFrame(padding: EdgeInsets = EdgeInsets(top: 14, leading: 13, bottom: 14, trailing: 16)) -> some View {
        HStack(spacing: 0) {
            Rectangle().fill(Palette.orange).frame(width: 3)
            self.padding(padding).frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(Palette.questionFill)
        .clipShape(RoundedRectangle(cornerRadius: Radius.card))
        .overlay(RoundedRectangle(cornerRadius: Radius.card).strokeBorder(Palette.questionBorder))
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
            .foregroundStyle(Palette.text3)
    }
}

/// Space between two groups of toolbar actions, where the system draws groups.
/// ToolbarSpacer exists from the macOS 26 SDK (Swift 6.2); built with an older
/// SDK, the actions are grouped without the space between.
struct GroupGap: ToolbarContent {
    #if compiler(>=6.2)
    var body: some ToolbarContent {
        if #available(macOS 26.0, *) {
            ToolbarSpacer(.fixed, placement: .primaryAction)
        }
    }
    #else
    var body: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) { EmptyView() }
    }
    #endif
}
