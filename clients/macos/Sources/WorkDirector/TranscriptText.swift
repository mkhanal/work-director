import AppKit
import SwiftUI
import WorkDirectorKit

/// A transcript as one native text view: selected across every item in one
/// drag, Select All and Copy as anywhere on the Mac, its steps folded and
/// unfolded by their chevrons. Grows to its text's height inside the page's
/// scroll view. Reading the conversation again edits only from the first item
/// that changed, so a selection before it, or one being dragged, holds. A long
/// transcript is set and laid out off the main thread, so showing one never
/// holds up the window.
struct TranscriptText: View {
    let transcript: Transcript
    /// Keeps the laid-out text for this key, so coming back to a long
    /// conversation shows it at once.
    var key: String? = nil
    var toggle: (String) -> Void = { _ in }
    /// Counts layouts finished off the main thread, so the page measures the
    /// text again when one lands.
    @State private var laidOut = 0

    var body: some View {
        TranscriptRepresentable(transcript: transcript, key: key, toggle: toggle, laidOut: laidOut) { laidOut += 1 }
    }
}

private struct TranscriptRepresentable: NSViewRepresentable {
    let transcript: Transcript
    let key: String?
    let toggle: (String) -> Void
    let laidOut: Int
    let landed: () -> Void

    func makeNSView(context: Context) -> TranscriptTextView {
        let view = key.flatMap { TranscriptTextView.kept[$0] }.flatMap { $0.superview == nil ? $0 : nil } ?? TranscriptTextView()
        if let key { TranscriptTextView.keep(view, as: key) }
        return view
    }

    func updateNSView(_ view: TranscriptTextView, context: Context) {
        view.toggle = toggle
        view.landed = landed
        view.show(transcript)
    }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView view: TranscriptTextView, context: Context) -> CGSize? {
        let width = proposal.width ?? view.bounds.width
        guard width.isFinite, width > 0 else { return nil }
        return CGSize(width: width, height: view.height(for: width))
    }
}

/// A text stack laid out off the main thread, handed to the main thread whole.
private struct Laid: @unchecked Sendable {
    /// Owns the stack: a layout manager and a container only point back at it.
    let storage: NSTextStorage
    let container: NSTextContainer
    let height: CGFloat
}

final class TranscriptTextView: NSTextView {
    private(set) var shown = Transcript(items: [])
    /// What is being set off the main thread, and for which width.
    private var setting: (transcript: Transcript, width: CGFloat)?
    private var generation = 0
    private var waiting: Transcript?
    private var dragging = false
    /// The width the text is laid out for, and its height there.
    private var laid: (width: CGFloat, height: CGFloat)? {
        didSet { if let laid { lastHeight = laid.height } }
    }
    private var lastHeight: CGFloat?
    private var appearanceName: NSAppearance.Name?
    /// The text this view shows; it owns the layout manager and container.
    private var owner: NSTextStorage
    var toggle: (String) -> Void = { _ in }
    var landed: () -> Void = {}

    /// Past this many characters a change is set and laid out off the main thread.
    static let long = 20_000
    /// How much of the end of a long transcript shows while the rest is set.
    static let glance = 6_000
    /// Up to this many characters a text is measured at any width asked.
    static let short = 10_000

    /// Laid-out conversations by key, the most recent last.
    @MainActor static var kept: [String: TranscriptTextView] = [:]
    @MainActor private static var keptOrder: [String] = []

    @MainActor static func keep(_ view: TranscriptTextView, as key: String) {
        kept[key] = view
        keptOrder.removeAll { $0 == key }
        keptOrder.append(key)
        while keptOrder.count > 12 { kept[keptOrder.removeFirst()] = nil }
    }

    init() {
        let (storage, container) = Self.stack(NSAttributedString(), width: 600)
        owner = storage
        // No width until the page gives one: the first it asks about is the
        // width the text is laid out for.
        super.init(frame: .zero, textContainer: container)
        isEditable = false
        isSelectable = true
        isRichText = true
        drawsBackground = false
        textContainerInset = .zero
        isVerticallyResizable = false
        isHorizontallyResizable = false
        // Links carry their own colour: a fold's chevron and name are links
        // too, so a fold can be pressed by keyboard and VoiceOver.
        linkTextAttributes = [.cursor: NSCursor.pointingHand]
        setAccessibilityLabel("Conversation")
    }

    override init(frame frameRect: NSRect, textContainer container: NSTextContainer?) {
        owner = container?.layoutManager?.textStorage ?? NSTextStorage()
        super.init(frame: frameRect, textContainer: container)
    }

    required init?(coder: NSCoder) { nil }

    /// A text stack of its own; the text keeps the width it is given, so
    /// only this view decides when it is laid out again.
    nonisolated private static func stack(_ text: NSAttributedString, width: CGFloat) -> (NSTextStorage, NSTextContainer) {
        let storage = NSTextStorage(attributedString: text)
        let layout = NSLayoutManager()
        storage.addLayoutManager(layout)
        let container = NSTextContainer(size: NSSize(width: width, height: .greatestFiniteMagnitude))
        container.lineFragmentPadding = 0
        container.widthTracksTextView = false
        layout.addTextContainer(container)
        return (storage, container)
    }

    nonisolated private static func measure(_ container: NSTextContainer) -> CGFloat {
        guard let layout = container.layoutManager else { return 0 }
        layout.ensureLayout(for: container)
        var height = layout.usedRect(for: container).height
        if layout.extraLineFragmentTextContainer != nil { height -= layout.extraLineFragmentRect.height }
        return ceil(max(height, 0))
    }

    // MARK: text

    func show(_ next: Transcript) {
        if dragging {
            waiting = next
            return
        }
        if let setting, setting.transcript == next { return }
        guard let from = Transcript.firstChange(from: shown, to: next) else {
            if setting != nil { generation += 1; setting = nil }
            return
        }
        let start = from < shown.items.count ? shown.starts[from] : shown.length
        if next.length - start > Self.long {
            // Nothing on screen yet: its end shows at once, the rest when set.
            if shown.items.isEmpty { glance(next) }
            setLater(next, width: setting?.width ?? laid?.width ?? frame.width)
            return
        }
        generation += 1
        setting = nil
        edit(from: start, to: next, items: next.items[from...])
    }

    /// The text from a character on replaced by items, the selection kept.
    private func edit(from start: Int, to next: Transcript, items: ArraySlice<TranscriptItem>) {
        guard let storage = textStorage else { return }
        let tail = TranscriptSetter.text(items)
        let selection = selectedRanges.map(\.rangeValue)
        storage.beginEditing()
        storage.replaceCharacters(in: NSRange(location: start, length: storage.length - start), with: tail)
        storage.endEditing()
        restore(selection)
        shown = next
        laid = nil
    }

    /// The end of a long transcript, as much as fills a window or two.
    private func glance(_ next: Transcript) {
        var at = next.items.count, length = 0
        while at > 0, length < Self.glance {
            at -= 1
            length += next.items[at].length
        }
        edit(from: 0, to: Transcript(items: Array(next.items[at...])), items: next.items[at...])
    }

    /// Sets and lays out a transcript off the main thread, then shows it
    /// whole; meanwhile what was there stays. Without a width yet, it waits
    /// for the page to give the view one.
    private func setLater(_ next: Transcript, width: CGFloat) {
        generation += 1
        let mine = generation
        setting = (next, width)
        guard width > 0 else { return }
        Task.detached(priority: .userInitiated) {
            let (storage, container) = Self.stack(TranscriptSetter.text(next.items[...]), width: width)
            let laid = Laid(storage: storage, container: container, height: Self.measure(container))
            await MainActor.run { self.install(laid, next, width: width, generation: mine) }
        }
    }

    private func install(_ made: Laid, _ next: Transcript, width: CGFloat, generation mine: Int) {
        guard mine == generation else { return }
        let selection = selectedRanges.map(\.rangeValue)
        // The whole stack moves in: replaceTextContainer would keep the old
        // text and lay it out again here.
        textContainer?.textView = nil
        made.container.textView = self
        owner = made.storage
        needsDisplay = true
        restore(selection)
        shown = next
        setting = nil
        laid = (width, made.height)
        landed()
    }

    private func restore(_ selection: [NSRange]) {
        let length = textStorage?.length ?? 0
        let kept = selection.map { r in
            let lo = min(r.location, length)
            return NSValue(range: NSRange(location: lo, length: min(r.length, length - lo)))
        }
        if !kept.isEmpty { setSelectedRanges(kept, affinity: .downstream, stillSelecting: false) }
    }

    /// The text's height at a width. Once placed, the text is laid out only
    /// for the width it is given; asked about another width, as the page does
    /// when it sizes itself, its height is scaled from the width it has.
    func height(for width: CGFloat) -> CGFloat {
        if let laid, laid.width == width { return laid.height }
        guard let container = textContainer else { return 0 }
        if width != container.containerSize.width {
            // A short text is measured at any width asked; a longer one only
            // until it has a height to scale.
            if shown.length > Self.long || (shown.length > Self.short && (frame.width > 0 || lastHeight != nil)) { return estimate(width) }
            container.containerSize = NSSize(width: width, height: .greatestFiniteMagnitude)
        }
        let height = Self.measure(container)
        // Measuring another width leaves the text laid out for its own.
        if frame.width > 0, frame.width != width {
            container.containerSize = NSSize(width: frame.width, height: .greatestFiniteMagnitude)
            return height
        }
        laid = (width, height)
        return height
    }

    override func setFrameSize(_ size: NSSize) {
        super.setFrameSize(size)
        guard size.width > 0, let container = textContainer else { return }
        if let setting, setting.width != size.width { setLater(setting.transcript, width: size.width) }
        guard size.width != container.containerSize.width else { return }
        if shown.length > Self.long {
            // While the window is dragged, a long text keeps its width; it
            // is laid out for the new one when the drag ends.
            if setting == nil, !inLiveResize { setLater(shown, width: size.width) }
        } else {
            container.containerSize = NSSize(width: size.width, height: .greatestFiniteMagnitude)
            laid = nil
            DispatchQueue.main.async { [weak self] in self?.landed() }
        }
    }

    override func viewDidEndLiveResize() {
        super.viewDidEndLiveResize()
        setFrameSize(frame.size)
    }

    private func estimate(_ width: CGFloat) -> CGFloat {
        guard let laid, laid.width > 0 else { return lastHeight ?? CGFloat(max(shown.items.count, setting?.transcript.items.count ?? 0)) * 24 }
        return ceil(laid.height * max(1, laid.width / width))
    }

    override var intrinsicContentSize: NSSize {
        NSSize(width: NSView.noIntrinsicMetric, height: laid?.height ?? NSView.noIntrinsicMetric)
    }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        let name = effectiveAppearance.bestMatch(from: [.aqua, .darkAqua])
        defer { appearanceName = name }
        guard let was = appearanceName, was != name else { return }
        // Marks are drawn in the colours of the appearance they were set in.
        let all = shown
        shown = Transcript(items: [])
        show(all)
    }

    // MARK: folding

    override func mouseDown(with event: NSEvent) {
        let point = convert(event.locationInWindow, from: nil)
        if event.clickCount == 1, let id = toggleID(at: point), let window,
           let next = window.nextEvent(matching: [.leftMouseUp, .leftMouseDragged], until: .distantFuture, inMode: .eventTracking, dequeue: false),
           next.type == .leftMouseUp {
            // A click on a step's chevron, mark or name folds it; a drag
            // from there selects, like anywhere else.
            toggle(id)
            return
        }
        dragging = true
        super.mouseDown(with: event)
        dragging = false
        if let waiting {
            self.waiting = nil
            show(waiting)
        }
    }

    override func clicked(onLink link: Any, at charIndex: Int) {
        if let url = link as? URL, url.scheme == TranscriptSetter.foldScheme {
            toggle(String(url.absoluteString.dropFirst(TranscriptSetter.foldScheme.count + 1)))
        } else {
            super.clicked(onLink: link, at: charIndex)
        }
    }

    private func toggleID(at point: NSPoint) -> String? {
        guard let layout = layoutManager, let container = textContainer, let storage = textStorage, storage.length > 0 else { return nil }
        var fraction: CGFloat = 0
        let glyph = layout.glyphIndex(for: point, in: container, fractionOfDistanceThroughGlyph: &fraction)
        let rect = layout.boundingRect(forGlyphRange: NSRange(location: glyph, length: 1), in: container)
        guard rect.insetBy(dx: -2, dy: -2).contains(point) else { return nil }
        let index = layout.characterIndexForGlyph(at: glyph)
        guard index < storage.length else { return nil }
        return storage.attribute(.wdToggle, at: index, effectiveRange: nil) as? String
    }

    // MARK: copying

    private var selection: Range<Int>? {
        let ranges = selectedRanges.map(\.rangeValue).filter { $0.length > 0 }
        guard let lo = ranges.map(\.location).min(), let hi = ranges.map({ $0.location + $0.length }).max() else { return nil }
        return lo..<hi
    }

    override func writeSelection(to pboard: NSPasteboard, types: [NSPasteboard.PasteboardType]) -> Bool {
        guard let selection else { return false }
        pboard.clearContents()
        return pboard.setString(shown.copied(selection), forType: .string)
    }

    override func copy(_ sender: Any?) {
        _ = writeSelection(to: .general, types: [.string])
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        let point = convert(event.locationInWindow, from: nil)
        let index = characterIndexForInsertion(at: point)
        let item = shown.item(at: min(index, max(shown.length - 1, 0)))
        let menu = NSMenu()
        if let selection {
            menu.addItem(Action("Copy") { [shown] in copyText(shown.copied(selection)) })
            menu.addItem(Action("Copy as Markdown") { [shown] in copyText(shown.markdown(selection)) })
        } else if let item {
            menu.addItem(Action("Copy") { copyText(Transcript(items: [item]).copied(0..<item.length)) })
            menu.addItem(Action("Copy as Markdown") { copyText(item.source) })
        }
        if let call = item?.tool {
            if !call.input.isEmpty { menu.addItem(Action("Copy Input") { copyText(call.input) }) }
            if let result = call.result, !result.isEmpty { menu.addItem(Action("Copy Result") { copyText(result) }) }
        }
        menu.addItem(.separator())
        menu.addItem(withTitle: "Select All", action: #selector(selectAll(_:)), keyEquivalent: "")
        return menu
    }
}

/// A menu item that runs a closure.
private final class Action: NSMenuItem {
    private let run: () -> Void

    init(_ title: String, _ run: @escaping () -> Void) {
        self.run = run
        super.init(title: title, action: #selector(fire), keyEquivalent: "")
        target = self
    }

    required init(coder: NSCoder) { fatalError("not decoded") }

    @objc private func fire() { run() }
}

extension NSAttributedString.Key {
    /// The id of the item a click on this text folds or unfolds.
    static let wdToggle = NSAttributedString.Key("wdToggle")
}

/// Sets a transcript's pieces in the design's type and colours.
enum TranscriptSetter {
    static func text(_ items: ArraySlice<TranscriptItem>) -> NSAttributedString {
        var setter = Setter()
        let out = NSMutableAttributedString()
        for item in items {
            for para in item.paras { setter.add(para, to: out) }
        }
        return out
    }

    /// The scheme of a fold's link.
    static let foldScheme = "wd-fold"

    static func font(_ face: Face, bold: Bool = false, italic: Bool = false) -> NSFont {
        var font: NSFont
        switch face {
        case .body: font = .systemFont(ofSize: 14)
        case .bodyMedium: font = .systemFont(ofSize: 14, weight: .medium)
        case .heading1: font = .systemFont(ofSize: 17, weight: .semibold)
        case .title: font = .systemFont(ofSize: 16, weight: .semibold)
        case .callout: font = .systemFont(ofSize: 13)
        case .calloutMedium: font = .systemFont(ofSize: 13, weight: .medium)
        case .meta: font = .systemFont(ofSize: 12, weight: .medium)
        case .caption: font = .systemFont(ofSize: 12)
        case .code: font = .monospacedSystemFont(ofSize: 12.5, weight: .regular)
        case .codeSmall: font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        case .micro: font = .systemFont(ofSize: 11, weight: .semibold)
        }
        if bold { font = NSFontManager.shared.convert(font, toHaveTrait: .boldFontMask) }
        if italic { font = NSFontManager.shared.convert(font, toHaveTrait: .italicFontMask) }
        return font
    }

    static func color(_ ink: Ink) -> NSColor {
        switch ink {
        case .text: NSColor(Palette.text)
        case .text2: NSColor(Palette.text2)
        case .text3: NSColor(Palette.text3)
        case .link: NSColor(Palette.link)
        case .failure: NSColor(Palette.failure)
        }
    }

    static func gap(_ gap: Gap) -> CGFloat {
        switch gap {
        case .none: 0
        case .line: Space.s1
        case .row: Space.s2
        case .block: Space.s3
        case .item: Space.s4
        case .turn: Space.s5
        }
    }

    /// Extra leading that brings a face to its line height in the design.
    static func leading(_ face: Face) -> CGFloat {
        switch face {
        case .body, .bodyMedium: TypeScale.bodyLeading
        case .code, .codeSmall: TypeScale.codeLeading
        default: 2
        }
    }

    /// How far a list or a run of calls nests each level.
    static let nestStep: CGFloat = 18

    /// The setter for one run of paragraphs: boxes and tables shared by
    /// their paragraphs, so consecutive ones are drawn as one.
    struct Setter {
        var boxes: [Box: NSTextBlock] = [:]
        var tables: [String: NSTextTable] = [:]

        mutating func add(_ para: Para, to out: NSMutableAttributedString) {
            let x = CGFloat(para.indent) * Space.turnIndent + CGFloat(para.nest) * TranscriptSetter.nestStep
            let face = para.runs.first(where: { $0.mark == nil })?.face ?? .body
            let style = NSMutableParagraphStyle()
            // The space above a box goes outside it, not inside its first line.
            let opens = para.box.map { box in
                if case .cell(let c) = box { return tables[c.table] == nil }
                return boxes[box] == nil
            } ?? false
            if !opens { style.paragraphSpacingBefore = TranscriptSetter.gap(para.gap) }
            style.lineSpacing = TranscriptSetter.leading(face)
            if para.oneLine { style.lineBreakMode = .byTruncatingTail }
            if let box = para.box {
                style.textBlocks = [block(box, x: x, gap: TranscriptSetter.gap(para.gap))]
                if case .cell(let c) = box {
                    style.alignment = switch c.align {
                    case .leading: .natural
                    case .center: .center
                    case .trailing: .right
                    }
                }
            } else if para.hang {
                style.firstLineHeadIndent = x
                style.headIndent = x + 22
                style.tabStops = [NSTextTab(textAlignment: .natural, location: x + 22)]
            } else {
                style.firstLineHeadIndent = x
                style.headIndent = x
            }
            let start = out.length
            for run in para.runs { out.append(TranscriptSetter.text(run)) }
            out.append(NSAttributedString(string: "\n", attributes: [.font: TranscriptSetter.font(face)]))
            out.addAttribute(.paragraphStyle, value: style, range: NSRange(location: start, length: out.length - start))
        }

        private mutating func block(_ box: Box, x: CGFloat, gap: CGFloat) -> NSTextBlock {
            if let known = boxes[box] { return known }
            let made: NSTextBlock
            switch box {
            case .cell(let c):
                let table = tables[c.table] ?? {
                    let t = NSTextTable()
                    t.numberOfColumns = c.columns
                    t.collapsesBorders = true
                    t.hidesEmptyCells = false
                    t.layoutAlgorithm = .automaticLayoutAlgorithm
                    t.setWidth(x, type: .absoluteValueType, for: .margin, edge: .minX)
                    t.setWidth(gap, type: .absoluteValueType, for: .margin, edge: .minY)
                    t.setWidth(Space.s1, type: .absoluteValueType, for: .margin, edge: .maxY)
                    return t
                }()
                tables[c.table] = table
                let cell = NSTextTableBlock(table: table, startingRow: c.row, rowSpan: 1, startingColumn: c.column, columnSpan: 1)
                cell.setWidth(Space.s1 + 2, type: .absoluteValueType, for: .padding, edge: .minY)
                cell.setWidth(Space.s1 + 2, type: .absoluteValueType, for: .padding, edge: .maxY)
                cell.setWidth(10, type: .absoluteValueType, for: .padding, edge: .minX)
                cell.setWidth(10, type: .absoluteValueType, for: .padding, edge: .maxX)
                cell.setWidth(1, type: .absoluteValueType, for: .border)
                cell.setBorderColor(NSColor(Palette.hairline))
                if c.header { cell.backgroundColor = NSColor(Palette.plainCard) }
                made = cell
            case .told:
                made = CardBlock(fill: NSColor(Palette.page), stroke: NSColor(Palette.hairline), padding: Space.s3, x: x + Space.turnIndent,
                                 mark: Marks.symbol("arrow.turn.down.right", size: IconSize.turn, weight: .regular, color: .secondaryLabelColor, box: Space.avatar))
            case .brief:
                made = CardBlock(fill: NSColor(Palette.page), stroke: NSColor(Palette.hairline), padding: Space.s3, x: x + Space.turnIndent,
                                 mark: Marks.symbol("doc.text.fill", size: IconSize.turn, weight: .regular, color: .secondaryLabelColor, box: Space.avatar))
            case .director:
                made = CardBlock(fill: NSColor(Palette.settled.opacity(0.08)), stroke: NSColor(Palette.settled.opacity(0.3)), padding: Space.s3, x: x)
            case .asked:
                made = CardBlock(fill: NSColor(Palette.plainCard), stroke: NSColor(Palette.hairline), padding: Space.cardH, x: x)
            case .code:
                made = CardBlock(fill: NSColor(Palette.sunken), stroke: nil, padding: Space.s3, x: x)
            case .quote:
                made = CardBlock(fill: nil, stroke: nil, padding: Space.s1, x: x, bar: NSColor(Palette.buttonBorder))
            case .rule:
                made = CardBlock(fill: nil, stroke: nil, padding: 0, x: x, rule: NSColor(Palette.hairline))
            }
            if !(made is NSTextTableBlock) { made.setWidth(gap, type: .absoluteValueType, for: .margin, edge: .minY) }
            boxes[box] = made
            return made
        }
    }

    static func text(_ run: Run) -> NSAttributedString {
        let font = font(run.face, bold: run.bold, italic: run.italic)
        var attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: color(run.ink)]
        if run.face == .micro { attributes[.kern] = 0.4 }
        if run.strike { attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
        if run.chip { attributes[.backgroundColor] = NSColor(Palette.sunken) }
        if let link = run.link, let url = URL(string: link) { attributes[.link] = url }
        if let id = run.toggle {
            attributes[.wdToggle] = id
            attributes[.link] = URL(string: foldScheme + ":" + id)
            attributes[.cursor] = NSCursor.pointingHand
        }
        guard let mark = run.mark else { return NSAttributedString(string: run.text, attributes: attributes) }
        let attachment = NSTextAttachment()
        let image = Marks.image(mark, ink: run.ink)
        attachment.image = image
        attachment.bounds = CGRect(x: 0, y: (font.capHeight - image.size.height) / 2, width: image.size.width, height: image.size.height)
        attributes[.attachment] = attachment
        return NSAttributedString(string: run.text, attributes: attributes)
    }
}

/// The symbols and speaker marks drawn in a transcript, in the colours of the
/// appearance they are drawn in.
enum Marks {
    /// A mark with the space after it drawn in, as a mark is never copied
    /// and so neither is a space beside it.
    static func image(_ mark: Mark, ink: Ink) -> NSImage {
        switch mark {
        case .avatar(let avatar):
            let size = Space.avatar
            return NSImage(size: NSSize(width: Space.turnIndent, height: size), flipped: true) { _ in
                avatarPath(avatar, size: size)
                return true
            }
        case .chevron(let open):
            return symbol(open ? "chevron.down" : "chevron.right", size: IconSize.tiny, weight: .bold, color: NSColor(Palette.text3), box: 10, gap: Space.s1)
        case .symbol(let name):
            return symbol(name, size: IconSize.control - 1, weight: .regular, color: TranscriptSetter.color(ink), box: 16, gap: Space.s1 + 2)
        }
    }

    static func symbol(_ name: String, size: CGFloat, weight: NSFont.Weight, color: NSColor, box: CGFloat, gap: CGFloat = 0) -> NSImage {
        guard let base = NSImage(systemSymbolName: name, accessibilityDescription: nil)?
            .withSymbolConfiguration(.init(pointSize: size, weight: weight)) else { return NSImage(size: NSSize(width: box + gap, height: box)) }
        let height = max(base.size.height, size)
        return NSImage(size: NSSize(width: box + gap, height: height), flipped: false) { rect in
            let at = NSRect(x: (box - base.size.width) / 2, y: (height - base.size.height) / 2, width: base.size.width, height: base.size.height)
            base.draw(in: at)
            color.set()
            at.fill(using: .sourceAtop)
            return true
        }
    }

    private static func avatarPath(_ avatar: Avatar, size: CGFloat) {
        let s = size / 20
        switch avatar {
        case .done:
            NSColor(Palette.green).setFill()
            NSBezierPath(ovalIn: NSRect(x: s, y: s, width: 18 * s, height: 18 * s)).fill()
            let check = NSBezierPath()
            check.move(to: NSPoint(x: 6 * s, y: 10.5 * s))
            check.line(to: NSPoint(x: 8.6 * s, y: 13 * s))
            check.line(to: NSPoint(x: 14 * s, y: 7.5 * s))
            check.lineWidth = 2 * s
            NSColor.white.setStroke()
            check.stroke()
        case .working:
            let ring = NSBezierPath(ovalIn: NSRect(x: 2 * s, y: 2 * s, width: 16 * s, height: 16 * s))
            ring.lineWidth = 2 * s
            NSColor(Palette.accent).setStroke()
            ring.stroke()
            let quarter = NSBezierPath()
            quarter.move(to: NSPoint(x: 10 * s, y: 10 * s))
            quarter.line(to: NSPoint(x: 10 * s, y: 2 * s))
            quarter.appendArc(withCenter: NSPoint(x: 10 * s, y: 10 * s), radius: 8 * s, startAngle: -90, endAngle: 0, clockwise: false)
            quarter.close()
            NSColor(Palette.accent).setFill()
            quarter.fill()
        }
    }
}

/// A box drawn behind its paragraphs: a rounded card, a code block, a quote's
/// bar or a rule. A card for what the session was told carries its mark in
/// the column left of it.
final class CardBlock: NSTextBlock {
    private let fill: NSColor?
    private let stroke: NSColor?
    private let mark: NSImage?
    private let bar: NSColor?
    private let rule: NSColor?

    init(fill: NSColor?, stroke: NSColor?, padding: CGFloat, x: CGFloat, mark: NSImage? = nil, bar: NSColor? = nil, rule: NSColor? = nil) {
        self.fill = fill
        self.stroke = stroke
        self.mark = mark
        self.bar = bar
        self.rule = rule
        super.init()
        setWidth(x, type: .absoluteValueType, for: .margin, edge: .minX)
        setWidth(padding, type: .absoluteValueType, for: .padding)
        setContentWidth(100, type: .percentageValueType)
        if bar != nil { setWidth(Space.s3 + 3, type: .absoluteValueType, for: .padding, edge: .minX) }
        if rule != nil {
            setValue(Space.s2, type: .absoluteValueType, for: .height)
            setValue(Space.s2, type: .absoluteValueType, for: .maximumHeight)
        }
    }

    required init?(coder: NSCoder) { nil }

    override func drawBackground(withFrame frameRect: NSRect, in controlView: NSView, characterRange: NSRange, layoutManager: NSLayoutManager) {
        let box = NSRect(x: frameRect.minX + width(for: .margin, edge: .minX), y: frameRect.minY + width(for: .margin, edge: .minY),
                         width: frameRect.width - width(for: .margin, edge: .minX) - width(for: .margin, edge: .maxX),
                         height: frameRect.height - width(for: .margin, edge: .minY) - width(for: .margin, edge: .maxY))
        if let rule {
            rule.setFill()
            NSRect(x: box.minX, y: box.midY.rounded(), width: box.width, height: 1).fill()
            return
        }
        if let bar {
            bar.setFill()
            NSBezierPath(roundedRect: NSRect(x: box.minX, y: box.minY, width: 3, height: box.height), xRadius: 1.5, yRadius: 1.5).fill()
            return
        }
        let shape = NSBezierPath(roundedRect: box.insetBy(dx: 0.5, dy: 0.5), xRadius: Radius.card, yRadius: Radius.card)
        if let fill {
            fill.setFill()
            shape.fill()
        }
        if let stroke {
            stroke.setStroke()
            shape.lineWidth = 1
            shape.stroke()
        }
        if let mark {
            let at = NSRect(x: box.minX - Space.turnIndent, y: box.minY + Space.s3, width: mark.size.width, height: mark.size.height)
            mark.draw(in: at, from: .zero, operation: .sourceOver, fraction: 1, respectFlipped: true, hints: nil)
        }
    }
}
