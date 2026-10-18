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
    var open: (String) -> Void = { _ in }
    /// Runs a `wd-act` link: its verb and the id it names.
    var act: (String, String) -> Void = { _, _ in }
    /// The controls the text makes room for, by id (`Mark.slot`).
    var slots: [String: AnyView] = [:]
    /// The controls the text lays over words, by id (`Run.over`).
    var overs: [String: AnyView] = [:]
    /// Takes a dragged id dropped on an item, by the item's id; nil when the
    /// text takes no drops.
    var drop: ((String, String) -> Bool)? = nil
    var toggle: (String) -> Void = { _ in }
    /// Counts layouts finished off the main thread, so the page measures the
    /// text again when one lands.
    @State private var laidOut = 0

    init(transcript: Transcript, key: String? = nil, open: @escaping (String) -> Void = { _ in }, act: @escaping (String, String) -> Void = { _, _ in },
         slots: [String: AnyView] = [:], overs: [String: AnyView] = [:], drop: ((String, String) -> Bool)? = nil,
         toggle: @escaping (String) -> Void = { _ in }) {
        self.drop = drop
        self.transcript = transcript
        self.key = key
        self.open = open
        self.act = act
        self.slots = slots
        self.overs = overs
        self.toggle = toggle
    }

    var body: some View {
        TranscriptRepresentable(transcript: transcript, key: key, open: open, act: act, slots: slots, overs: overs, drop: drop, toggle: toggle, laidOut: laidOut) { laidOut += 1 }
    }
}

private struct TranscriptRepresentable: NSViewRepresentable {
    let transcript: Transcript
    let key: String?
    let open: (String) -> Void
    let act: (String, String) -> Void
    let slots: [String: AnyView]
    let overs: [String: AnyView]
    let drop: ((String, String) -> Bool)?
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
        view.open = open
        view.act = act
        view.takes(drop)
        view.landed = landed
        view.controls(slots, over: overs)
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
    /// Counts changes to the text, so a search of it knows it is stale.
    private(set) var version = 0
    /// What is being set off the main thread, and for which width.
    private var setting: (transcript: Transcript, width: CGFloat)?
    private var generation = 0
    private var waiting: Transcript?
    private var dragging = false
    /// The width the text is laid out for, and its height there.
    private var laid: (width: CGFloat, height: CGFloat)? {
        didSet {
            if let laid { lastHeight = laid.height } else { measured = [:] }
        }
    }
    /// Heights the text was measured at for widths other than its own,
    /// until it changes.
    private var measured: [CGFloat: CGFloat] = [:]
    private var lastHeight: CGFloat?
    private var appearanceName: NSAppearance.Name?
    /// The text this view shows; it owns the layout manager and container.
    private var owner: NSTextStorage
    var toggle: (String) -> Void = { _ in }
    /// Opens an agent's conversation, by its work's id.
    var open: (String) -> Void = { _ in }
    var act: (String, String) -> Void = { _, _ in }
    var landed: () -> Void = {}
    /// The controls set in the text, by id, each in a view of its own over
    /// the text.
    private var hosted: [String: NSHostingController<AnyView>] = [:]
    /// The sizes the text holds room for, by control.
    private var room: [String: CGSize] = [:]
    /// Whether the controls are to be placed again before the next draw.
    private var placing = false {
        didSet { if placing { needsDisplay = true } }
    }

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
        NotificationCenter.default.addObserver(self, selector: #selector(reveal(_:)), name: .revealItem, object: nil)
    }

    /// Scrolls an item this text shows to the top of the page.
    @objc private func reveal(_ note: Notification) {
        guard let id = note.object as? String, let i = shown.items.firstIndex(where: { $0.id == id }),
              let layout = layoutManager, let container = textContainer else { return }
        let glyphs = layout.glyphRange(forCharacterRange: NSRange(location: shown.starts[i], length: max(1, shown.items[i].length - 1)), actualCharacterRange: nil)
        let at = layout.boundingRect(forGlyphRange: glyphs, in: container)
        let tall = enclosingScrollView?.contentView.bounds.height ?? visibleRect.height
        scrollToVisible(NSRect(x: 0, y: max(0, at.minY - Space.s3), width: 1, height: max(tall - Space.s3, at.height)))
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
        let layout = PillLayoutManager()
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
        measureControls(items)
        let tail = TranscriptSetter.text(items, room: room)
        let selection = selectedRanges.map(\.rangeValue)
        storage.beginEditing()
        storage.replaceCharacters(in: NSRange(location: start, length: storage.length - start), with: tail)
        storage.endEditing()
        restore(selection)
        shown = next
        laid = nil
        placing = true
        needsDisplay = true
        changed()
    }

    private func changed() {
        version += 1
        NotificationCenter.default.post(name: .transcriptChanged, object: self)
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
        measureControls(next.items[...])
        let room = room
        Task.detached(priority: .userInitiated) {
            let (storage, container) = Self.stack(TranscriptSetter.text(next.items[...], room: room), width: width)
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
        placing = true
        landed()
        changed()
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
    /// when it sizes itself, a short text is measured on a copy, once a width
    /// until it changes, and a long one's height is scaled from the width it has.
    func height(for width: CGFloat) -> CGFloat {
        if let laid, laid.width == width { return laid.height }
        if let known = measured[width] { return known }
        guard let container = textContainer, let storage = textStorage else { return 0 }
        if frame.width > 0, frame.width != width {
            if shown.length > Self.short { return estimate(width) }
            let (copy, scratch) = Self.stack(NSAttributedString(attributedString: storage), width: width)
            let height = withExtendedLifetime(copy) { Self.measure(scratch) }
            measured[width] = height
            return height
        }
        if width != container.containerSize.width {
            if shown.length > Self.long || (shown.length > Self.short && lastHeight != nil) { return estimate(width) }
            container.containerSize = NSSize(width: width, height: .greatestFiniteMagnitude)
        }
        let height = Self.measure(container)
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
            // A control as wide as the line is measured again for the new
            // width when it is next placed.
            placing = true
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
        } else if let url = link as? URL, url.scheme == openScheme {
            open(String(url.absoluteString.dropFirst(openScheme.count + 1)))
        } else if let url = link as? URL, url.scheme == actScheme {
            let said = url.absoluteString.dropFirst(actScheme.count + 1)
            guard let slash = said.firstIndex(of: "/") else { return }
            act(String(said[..<slash]), String(said[said.index(after: slash)...]))
        } else if let url = link as? URL, url.isFileURL {
            let image = ["png", "jpg", "jpeg", "gif", "heic", "tiff", "webp"].contains(url.pathExtension.lowercased())
            if image { QuickLook.shared.show(url) } else { NSWorkspace.shared.activateFileViewerSelecting([url]) }
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

    // MARK: controls

    /// Takes the controls for the text, each kept by id so its own state
    /// lives as long as the text shows it. One the text makes room for tells
    /// the text when it needs another size.
    func controls(_ slots: [String: AnyView], over: [String: AnyView]) {
        var all: [String: AnyView] = over
        for (id, view) in slots {
            all[id] = AnyView(view.fixedSize(horizontal: false, vertical: true)
                .onGeometryChange(for: CGSize.self, of: \.size) { [weak self] _ in
                    DispatchQueue.main.async { self?.controlResized(id) }
                })
        }
        for (id, view) in all {
            if let known = hosted[id] {
                known.rootView = view
            } else {
                let host = NSHostingController(rootView: view)
                host.sizingOptions = [.intrinsicContentSize]
                host.view.translatesAutoresizingMaskIntoConstraints = true
                hosted[id] = host
                addSubview(host.view)
            }
        }
        for id in hosted.keys where all[id] == nil {
            hosted[id]?.view.removeFromSuperview()
            hosted[id] = nil
        }
        placing = true
        needsDisplay = true
    }

    /// The room a control needs: its own size, or for one alone on its line,
    /// the line's width and the height it takes there.
    private func measure(_ id: String, whole: Para?) -> CGSize? {
        guard let host = hosted[id] else { return nil }
        guard let whole else {
            // Its ideal size: a control that would grow takes only what it needs.
            let size = host.view.fittingSize
            return CGSize(width: ceil(size.width), height: ceil(size.height))
        }
        let w = lineWidth(whole)
        return CGSize(width: w, height: ceil(host.sizeThatFits(in: CGSize(width: w, height: .greatestFiniteMagnitude)).height))
    }

    /// The width a control alone on its line has.
    private func lineWidth(_ para: Para) -> CGFloat {
        let width = frame.width > 0 ? frame.width : textContainer?.containerSize.width ?? 0
        return max(1, width - TranscriptSetter.inset(para))
    }

    private func measureControls(_ items: ArraySlice<TranscriptItem>) {
        for item in items {
            for para in item.paras {
                for run in para.runs {
                    let id: String
                    switch run.mark {
                    case .slot(let named), .block(let named): id = named
                    default: continue
                    }
                    // A control is measured once, and again only when it says
                    // its size changed or its line's width did.
                    if let known = room[id], !para.isWholeSlot || known.width == lineWidth(para) { continue }
                    guard let size = measure(id, whole: para.isWholeSlot ? para : nil) else { continue }
                    room[id] = size
                }
            }
        }
    }

    /// Lays each control over the room the text holds for it, or over the
    /// words it sits on.
    private func placeControls() {
        guard let layout = layoutManager, let container = textContainer, let storage = textStorage else { return }
        // A control alone on its line takes the height it needs at the width
        // the text now has; one that needs another sets the text again.
        var changed = false
        for para in shown.items.lazy.flatMap(\.paras) where para.isWholeSlot {
            guard case .block(let id) = para.runs[0].mark, let known = room[id], known.width != lineWidth(para),
                  let size = measure(id, whole: para) else { continue }
            room[id] = size
            changed = resize(id) || changed
        }
        if changed { DispatchQueue.main.async { [weak self] in self?.landed() } }
        layout.ensureLayout(for: container)
        let origin = textContainerOrigin
        var seen: Set<String> = []
        let all = NSRange(location: 0, length: storage.length)
        storage.enumerateAttribute(.wdSlot, in: all) { value, range, _ in
            guard let id = value as? String, let host = hosted[id], let size = room[id] else { return }
            let glyphs = layout.glyphRange(forCharacterRange: range, actualCharacterRange: nil)
            let line = layout.lineFragmentRect(forGlyphAt: glyphs.location, effectiveRange: nil)
            let at = layout.location(forGlyphAt: glyphs.location)
            let baseline = line.minY + at.y
            let font = storage.attribute(.font, at: range.location, effectiveRange: nil) as? NSFont ?? .systemFont(ofSize: 13)
            // Centred on the line's capitals, as the attachment's room is; one
            // alone on its line fills the line's top.
            let alone = storage.attribute(.wdWhole, at: range.location, effectiveRange: nil) != nil
            let y = alone ? layout.boundingRect(forGlyphRange: glyphs, in: container).minY : baseline - font.capHeight / 2 - size.height / 2
            host.view.frame = NSRect(x: line.minX + at.x + origin.x, y: y + origin.y, width: size.width, height: size.height)
            host.view.isHidden = false
            seen.insert(id)
        }
        storage.enumerateAttribute(.wdOver, in: all) { value, range, _ in
            guard let id = value as? String, let host = hosted[id] else { return }
            let glyphs = layout.glyphRange(forCharacterRange: range, actualCharacterRange: nil)
            var rect = NSRect.null
            layout.enumerateEnclosingRects(forGlyphRange: glyphs, withinSelectedGlyphRange: NSRange(location: NSNotFound, length: 0), in: container) { r, _ in
                rect = rect.union(r)
            }
            guard !rect.isNull else { return }
            host.view.frame = (seen.contains(id) ? rect.union(host.view.frame.offsetBy(dx: -origin.x, dy: -origin.y)) : rect).offsetBy(dx: origin.x, dy: origin.y)
            host.view.isHidden = false
            seen.insert(id)
        }
        for (id, host) in hosted where !seen.contains(id) { host.view.isHidden = true }
    }

    /// A control asks for another size: the text makes room for it again.
    private func controlResized(_ id: String) {
        guard let known = room[id] else { return }
        let whole = shown.items.lazy.flatMap(\.paras).first { p in p.isWholeSlot && p.runs[0].mark == .block(id) }
        guard let size = measure(id, whole: whole), abs(size.height - known.height) > 0.5 || abs(size.width - known.width) > 0.5 else { return }
        room[id] = size
        if resize(id) { landed() }
    }

    /// Gives a control's room in the text its new size, laying out again only
    /// the line it is on.
    private func resize(_ id: String) -> Bool {
        guard let storage = textStorage, let size = room[id] else { return false }
        var found = false
        storage.enumerateAttribute(.wdSlot, in: NSRange(location: 0, length: storage.length)) { value, range, stop in
            guard value as? String == id, let attachment = storage.attribute(.attachment, at: range.location, effectiveRange: nil) as? NSTextAttachment,
                  let font = storage.attribute(.font, at: range.location, effectiveRange: nil) as? NSFont else { return }
            attachment.image = NSImage(size: size)
            attachment.bounds = CGRect(x: 0, y: (font.capHeight - size.height) / 2, width: size.width, height: size.height)
            storage.beginEditing()
            storage.edited(.editedAttributes, range: range, changeInLength: 0)
            storage.endEditing()
            found = true
            stop.pointee = true
        }
        guard found else { return false }
        laid = nil
        placing = true
        return true
    }

    override func viewWillDraw() {
        if placing {
            placing = false
            placeControls()
        }
        super.viewWillDraw()
    }

    override func accessibilityChildren() -> [Any]? {
        (super.accessibilityChildren() ?? []) + hosted.values.map(\.view).filter { !$0.isHidden }
    }

    // MARK: dropping

    private var drop: ((String, String) -> Bool)?
    /// The item a drag would drop on, marked by a line above it.
    private var dropTarget: Int? {
        didSet { if dropTarget != oldValue { needsDisplay = true } }
    }

    func takes(_ drop: ((String, String) -> Bool)?) {
        let was = self.drop != nil
        self.drop = drop
        if drop != nil, !was { registerForDraggedTypes([.string]) }
        if drop == nil, was { unregisterDraggedTypes() }
    }

    private func target(_ sender: NSDraggingInfo) -> Int? {
        let point = convert(sender.draggingLocation, from: nil)
        let index = characterIndexForInsertion(at: point)
        guard let item = shown.item(at: min(index, max(shown.length - 1, 0))) else { return nil }
        return shown.items.firstIndex(of: item)
    }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        guard drop != nil else { return super.draggingEntered(sender) }
        dropTarget = target(sender)
        return .move
    }

    override func draggingUpdated(_ sender: NSDraggingInfo) -> NSDragOperation {
        guard drop != nil else { return super.draggingUpdated(sender) }
        dropTarget = target(sender)
        return .move
    }

    override func draggingExited(_ sender: NSDraggingInfo?) {
        guard drop != nil else { return super.draggingExited(sender) }
        dropTarget = nil
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        guard let drop else { return super.performDragOperation(sender) }
        defer { dropTarget = nil }
        guard let moved = sender.draggingPasteboard.string(forType: .string), let i = target(sender) else { return false }
        return drop(moved, shown.items[i].id)
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        guard let i = dropTarget, let layout = layoutManager, let container = textContainer else { return }
        let glyphs = layout.glyphRange(forCharacterRange: NSRange(location: shown.starts[i], length: 1), actualCharacterRange: nil)
        let top = layout.lineFragmentRect(forGlyphAt: glyphs.location, effectiveRange: nil).minY + textContainerOrigin.y
        NSColor(Palette.accent).setFill()
        NSRect(x: 0, y: max(0, top - Space.s2), width: bounds.width, height: 2).fill()
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
        for copy in item?.copies ?? [] {
            menu.addItem(Action(copy.title) { copyText(copy.text) })
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

extension Notification.Name {
    /// Asks the text showing an item, by its id, to scroll it into view.
    static let revealItem = Notification.Name("wd.revealItem")
}

extension NSAttributedString.Key {
    /// The id of the item a click on this text folds or unfolds.
    static let wdToggle = NSAttributedString.Key("wdToggle")
    /// The rounded tint drawn behind a badge's or tag's words.
    static let wdPill = NSAttributedString.Key("wdPill")
    /// The control set in this character's room, by id.
    static let wdSlot = NSAttributedString.Key("wdSlot")
    /// The control laid over these words, by id.
    static let wdOver = NSAttributedString.Key("wdOver")
    /// The control set here takes the whole line.
    static let wdWhole = NSAttributedString.Key("wdWhole")
}

/// Sets a transcript's pieces in the design's type and colours.
enum TranscriptSetter {
    static func text(_ items: ArraySlice<TranscriptItem>, room: [String: CGSize] = [:]) -> NSAttributedString {
        var setter = Setter(room: room)
        let out = NSMutableAttributedString()
        for item in items {
            for para in item.paras { setter.add(para, to: out) }
        }
        setter.finish()
        return out
    }

    /// How far in a paragraph's line starts from the text's edge, outside
    /// any box: what a control alone on it leaves of the width.
    static func inset(_ para: Para) -> CGFloat {
        CGFloat(para.indent) * Space.turnIndent + CGFloat(para.nest) * nestStep + CGFloat(para.lead)
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
        case .headline: font = .systemFont(ofSize: 14, weight: .semibold)
        case .calloutStrong: font = .systemFont(ofSize: 13, weight: .semibold)
        case .callout: font = .systemFont(ofSize: 13)
        case .calloutMedium: font = .systemFont(ofSize: 13, weight: .medium)
        case .meta: font = .systemFont(ofSize: 12, weight: .medium)
        case .metaStrong: font = .systemFont(ofSize: 12, weight: .semibold)
        case .tag: font = .systemFont(ofSize: 10.5, weight: .semibold)
        case .tagMedium: font = .systemFont(ofSize: 11, weight: .medium)
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
        case .stepText: NSColor(Palette.stepText)
        case .settled: NSColor(BadgeTone.settled.text)
        case .attention: NSColor(Palette.orange)
        case .grey: NSColor(Palette.grey)
        case .live: NSColor(BadgeTone.live.text)
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
        case .section: Space.section
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
        let room: [String: CGSize]
        /// The cells at the end of a row as wide as what they hold, by table,
        /// and the widest of them: every row's is set as wide.
        var ends: [String: (cells: [NSTextTableBlock], width: CGFloat)] = [:]

        func finish() {
            for (_, end) in ends {
                for cell in end.cells { cell.setContentWidth(end.width, type: .absoluteValueType) }
            }
        }
        var boxes: [Box: NSTextBlock] = [:]
        var tables: [String: NSTextTable] = [:]

        mutating func add(_ para: Para, to out: NSMutableAttributedString) {
            let column = CGFloat(para.indent) * Space.turnIndent
            let nest = CGFloat(para.nest) * TranscriptSetter.nestStep
            let lead = CGFloat(para.lead)
            let face = para.runs.first(where: { $0.mark == nil })?.face ?? .body
            let style = NSMutableParagraphStyle()
            let gap = TranscriptSetter.gap(para.gap)
            // The space above a box goes outside it, not inside its first line.
            var blocks: [NSTextBlock] = []
            var spent = false
            if let outer = para.outer {
                spent = opens(outer)
                blocks.append(block(outer, x: column, gap: spent ? gap : 0))
            }
            if let box = para.box {
                let opening = opens(box)
                blocks.append(block(box, x: para.outer == nil ? column + nest : nest, gap: opening && !spent ? gap : 0))
                spent = spent || opening
            }
            if !spent { style.paragraphSpacingBefore = gap }
            style.lineSpacing = TranscriptSetter.leading(face)
            if para.oneLine { style.lineBreakMode = .byTruncatingTail }
            style.alignment = switch para.align {
            case .leading: .natural
            case .center: .center
            case .trailing: .right
            }
            // Inside a box, the box holds the column and the nesting.
            let x = para.box != nil ? lead : para.outer != nil ? nest + lead : column + nest + lead
            if !blocks.isEmpty { style.textBlocks = blocks }
            if case .cell(let c) = para.box {
                style.alignment = switch c.align {
                case .leading: .natural
                case .center: .center
                case .trailing: .right
                }
            }
            if para.label {
                style.firstLineHeadIndent = x - lead
                style.headIndent = x
                style.tabStops = [NSTextTab(textAlignment: .natural, location: x)]
            } else if para.hang {
                style.firstLineHeadIndent = x
                style.headIndent = x + 22
                style.tabStops = [NSTextTab(textAlignment: .natural, location: x + 22)]
            } else {
                style.firstLineHeadIndent = x
                style.headIndent = x
            }
            let start = out.length
            for run in para.runs { out.append(TranscriptSetter.text(run, room: room)) }
            // A cell set at the end of its row is as wide as what it holds;
            // the cells before it share the rest.
            if case .cell(let c) = para.box, c.look == .row || c.look == .bare, c.align == .trailing, c.width == nil,
               let cell = blocks.last as? NSTextTableBlock {
                let words = out.attributedSubstring(from: NSRange(location: start, length: out.length - start))
                let width = ceil(words.size().width) + 1
                ends[c.table, default: ([], 0)].cells.append(cell)
                ends[c.table]!.width = max(ends[c.table]!.width, width)
            }
            out.append(NSAttributedString(string: "\n", attributes: [.font: TranscriptSetter.font(face)]))
            out.addAttribute(.paragraphStyle, value: style, range: NSRange(location: start, length: out.length - start))
        }

        private func opens(_ box: Box) -> Bool {
            if case .cell(let c) = box { return c.look == .card ? boxes[box] == nil : tables[c.table] == nil }
            return boxes[box] == nil
        }

        private mutating func block(_ box: Box, x: CGFloat, gap: CGFloat) -> NSTextBlock {
            if let known = boxes[box] { return known }
            let made: NSTextBlock
            switch box {
            case .cell(let c):
                let table = tables[c.table] ?? {
                    let t = NSTextTable()
                    t.numberOfColumns = c.columns
                    // Borders are never collapsed: collapsing them, AppKit
                    // takes every box around a cell for a cell of an outer
                    // table, and a table in a card would end the app when
                    // drawn. A grid's cells draw only their right and bottom
                    // edges instead, the first row and column their outer ones.
                    t.hidesEmptyCells = false
                    t.layoutAlgorithm = .automaticLayoutAlgorithm
                    t.setWidth(x, type: .absoluteValueType, for: .margin, edge: .minX)
                    // Space above as padding: a margin there narrows the
                    // table by as much on its right.
                    t.setWidth(gap, type: .absoluteValueType, for: .padding, edge: .minY)
                    switch c.look {
                    case .grid:
                        t.setWidth(Space.s1, type: .absoluteValueType, for: .margin, edge: .maxY)
                    case .card:
                        // Each column as wide as the next.
                        t.layoutAlgorithm = .fixedLayoutAlgorithm
                        t.setContentWidth(100, type: .percentageValueType)
                    case .lined:
                        t.setContentWidth(100, type: .percentageValueType)
                    case .row, .bare:
                        // Columns as their first row sets them: one sized to
                        // what it holds, the rest to what is left.
                        t.layoutAlgorithm = .fixedLayoutAlgorithm
                        t.setContentWidth(100, type: .percentageValueType)
                    }
                    return t
                }()
                tables[c.table] = table
                let cell: NSTextTableBlock = switch c.look {
                case .card: CardCell(table: table, column: c.column)
                case .row: RowCell(table: table, cell: c)
                default: NSTextTableBlock(table: table, startingRow: c.row, rowSpan: 1, startingColumn: c.column, columnSpan: 1)
                }
                cell.verticalAlignment = .topAlignment
                switch c.look {
                case .card:
                    // Room between the cards, shared so every card is as wide.
                    let gap = Space.s3, n = CGFloat(c.columns), i = CGFloat(c.column)
                    cell.setWidth(gap * i / n, type: .absoluteValueType, for: .margin, edge: .minX)
                    cell.setWidth(gap * (n - 1 - i) / n, type: .absoluteValueType, for: .margin, edge: .maxX)
                    cell.setWidth(Space.cardV, type: .absoluteValueType, for: .padding, edge: .minY)
                    cell.setWidth(Space.cardV, type: .absoluteValueType, for: .padding, edge: .maxY)
                    cell.setWidth(Space.cardH, type: .absoluteValueType, for: .padding, edge: .minX)
                    cell.setWidth(Space.cardH, type: .absoluteValueType, for: .padding, edge: .maxX)
                case .row:
                    cell.setWidth(14, type: .absoluteValueType, for: .padding, edge: .minY)
                    cell.setWidth(14, type: .absoluteValueType, for: .padding, edge: .maxY)
                    cell.setWidth(c.column == 0 ? 16 + CGFloat(c.inset) : Space.s2, type: .absoluteValueType, for: .padding, edge: .minX)
                    cell.setWidth(c.column == c.columns - 1 ? 16 : 0, type: .absoluteValueType, for: .padding, edge: .maxX)
                case .bare:
                    if let width = c.width { cell.setContentWidth(CGFloat(width), type: .absoluteValueType) }
                    if !c.last { cell.setWidth(Space.s3, type: .absoluteValueType, for: .padding, edge: .maxY) }
                    cell.verticalAlignment = .middleAlignment
                case .lined, .grid:
                    break
                }
                if c.look == .lined {
                    cell.setWidth(9, type: .absoluteValueType, for: .padding, edge: .minY)
                    cell.setWidth(9, type: .absoluteValueType, for: .padding, edge: .maxY)
                    if c.column < c.columns - 1 { cell.setWidth(12, type: .absoluteValueType, for: .padding, edge: .maxX) }
                    if let width = c.width { cell.setContentWidth(CGFloat(width), type: .absoluteValueType) }
                    cell.verticalAlignment = .baselineAlignment
                    if !c.last {
                        cell.setWidth(1, type: .absoluteValueType, for: .border, edge: .maxY)
                        cell.setBorderColor(NSColor(c.header ? Palette.hairline : Palette.rowSeparator), for: .maxY)
                    }
                } else if c.look == .grid {
                    cell.setWidth(Space.s1 + 2, type: .absoluteValueType, for: .padding, edge: .minY)
                    cell.setWidth(Space.s1 + 2, type: .absoluteValueType, for: .padding, edge: .maxY)
                    cell.setWidth(10, type: .absoluteValueType, for: .padding, edge: .minX)
                    cell.setWidth(10, type: .absoluteValueType, for: .padding, edge: .maxX)
                    cell.setWidth(1, type: .absoluteValueType, for: .border, edge: .maxX)
                    cell.setWidth(1, type: .absoluteValueType, for: .border, edge: .maxY)
                    if c.column == 0 { cell.setWidth(1, type: .absoluteValueType, for: .border, edge: .minX) }
                    if c.row == 0 { cell.setWidth(1, type: .absoluteValueType, for: .border, edge: .minY) }
                    cell.setBorderColor(NSColor(Palette.hairline))
                    if c.header { cell.backgroundColor = NSColor(Palette.plainCard) }
                }
                made = cell
            case .told:
                made = CardBlock(fill: NSColor(Palette.page), stroke: NSColor(Palette.hairline), padding: Space.s3, x: x + Space.turnIndent,
                                 mark: Marks.symbol("arrow.turn.down.right", size: IconSize.turn, weight: .regular, color: .secondaryLabelColor, box: Space.avatar))
            case .brief:
                made = CardBlock(fill: NSColor(Palette.page), stroke: NSColor(Palette.hairline), padding: Space.s3, x: x + Space.turnIndent,
                                 mark: Marks.symbol("doc.text.fill", size: IconSize.turn, weight: .regular, color: .secondaryLabelColor, box: Space.avatar))
            case .director, .settled:
                made = CardBlock(fill: NSColor(Palette.settled.opacity(0.08)), stroke: NSColor(Palette.settled.opacity(0.3)), padding: Space.s3, x: x)
            case .asked:
                made = CardBlock(fill: NSColor(Palette.plainCard), stroke: NSColor(Palette.hairline), padding: Space.cardH, x: x)
            case .code:
                made = CardBlock(fill: NSColor(Palette.sunken), stroke: nil, padding: Space.s3, x: x)
            case .quote:
                made = CardBlock(fill: nil, stroke: nil, padding: Space.s1, x: x, bar: NSColor(Palette.buttonBorder))
            case .rule:
                made = CardBlock(fill: nil, stroke: nil, padding: 0, x: x, rule: NSColor(Palette.hairline))
            case .bubble:
                made = BubbleBlock(fill: NSColor(Palette.bubbleFill), stroke: NSColor(Palette.bubbleBorder), padding: 10, x: max(x, 96), across: 14, radius: Radius.bubble)
            case .card:
                made = CardBlock(fill: NSColor(Palette.page), stroke: NSColor(Palette.hairline), padding: 12, x: x, across: 14)
            case .plain:
                made = CardBlock(fill: NSColor(Palette.plainCard), stroke: NSColor(Palette.hairline), padding: 12, x: x, across: 14)
            case .frame:
                made = CardBlock(fill: NSColor(Palette.page), stroke: NSColor(Palette.hairline), padding: 0, x: x)
            case .agent:
                made = CardBlock(fill: NSColor(Palette.page), stroke: NSColor(Palette.hairline), padding: Space.s3, x: x)
            case .answered:
                made = CardBlock(fill: NSColor(Palette.answeredFill), stroke: NSColor(Palette.answeredBorder), padding: 8, x: x, across: 10, radius: Radius.option)
            case .event:
                made = LineBlock(color: NSColor(Palette.hairline), x: x, both: true)
            case .day:
                made = LineBlock(color: NSColor(Palette.separator), x: x, both: false)
            }
            if !(made is NSTextTableBlock) { made.setWidth(gap, type: .absoluteValueType, for: .margin, edge: .minY) }
            boxes[box] = made
            return made
        }
    }

    static func text(_ run: Run, room: [String: CGSize] = [:]) -> NSAttributedString {
        let font = font(run.face, bold: run.bold, italic: run.italic)
        var attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: color(run.ink)]
        if run.face == .micro { attributes[.kern] = 0.4 }
        if run.strike { attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
        if run.chip { attributes[.backgroundColor] = NSColor(Palette.sunken) }
        if let link = run.link, let url = URL(string: link) { attributes[.link] = url }
        if let pill = run.pill {
            let look = PillLook(pill)
            attributes[.wdPill] = look
            attributes[.foregroundColor] = look.text
        }
        if let over = run.over { attributes[.wdOver] = over }
        if let help = run.help { attributes[.toolTip] = help }
        if let id = run.toggle {
            attributes[.wdToggle] = id
            attributes[.link] = URL(string: foldScheme + ":" + id)
            attributes[.cursor] = NSCursor.pointingHand
        }
        guard let mark = run.mark else { return NSAttributedString(string: run.text, attributes: attributes) }
        let attachment = NSTextAttachment()
        if case .slot(let id) = mark { return slot(id, whole: false) }
        if case .block(let id) = mark { return slot(id, whole: true) }
        func slot(_ id: String, whole: Bool) -> NSAttributedString {
            // Room for a control the view lays over it.
            let size = room[id] ?? .zero
            attachment.image = NSImage(size: size)
            attachment.bounds = CGRect(x: 0, y: (font.capHeight - size.height) / 2, width: size.width, height: size.height)
            attributes[.attachment] = attachment
            attributes[.wdSlot] = id
            if whole { attributes[.wdWhole] = true }
            return NSAttributedString(string: run.text, attributes: attributes)
        }
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
        case .state(let avatar):
            let size = IconSize.state
            return NSImage(size: NSSize(width: size + Space.s3, height: size), flipped: true) { _ in
                avatarPath(avatar, size: size)
                return true
            }
        case .menu:
            return symbol("arrowtriangle.down.fill", size: 6, weight: .regular, color: TranscriptSetter.color(ink), box: 8, gap: 0)
        case .slot, .block:
            return NSImage(size: .zero)
        case .chevron(let open):
            return symbol(open ? "chevron.down" : "chevron.right", size: IconSize.tiny, weight: .bold, color: NSColor(Palette.text3), box: 10, gap: Space.s1)
        case .symbol(let name):
            return symbol(name, size: IconSize.control - 1, weight: .regular, color: TranscriptSetter.color(ink), box: 16, gap: Space.s1 + 2)
        case .space(let width):
            return NSImage(size: NSSize(width: width, height: 1))
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
        case .needsYou:
            let ring = NSBezierPath(ovalIn: NSRect(x: 2 * s, y: 2 * s, width: 16 * s, height: 16 * s))
            ring.lineWidth = 2 * s
            NSColor(Palette.orange).setStroke()
            ring.stroke()
            NSColor(Palette.orange).setFill()
            NSBezierPath(ovalIn: NSRect(x: 7 * s, y: 7 * s, width: 6 * s, height: 6 * s)).fill()
        case .waiting:
            let ring = NSBezierPath(ovalIn: NSRect(x: 2 * s, y: 2 * s, width: 16 * s, height: 16 * s))
            ring.lineWidth = 2 * s
            ring.setLineDash([3 * s, 3 * s], count: 2, phase: 0)
            NSColor(Palette.grey).setStroke()
            ring.stroke()
        case .superseded:
            let ring = NSBezierPath(ovalIn: NSRect(x: 2 * s, y: 2 * s, width: 16 * s, height: 16 * s))
            ring.lineWidth = 2 * s
            NSColor(Palette.grey).setStroke()
            ring.stroke()
            let bar = NSBezierPath()
            bar.move(to: NSPoint(x: 5 * s, y: 10 * s))
            bar.line(to: NSPoint(x: 15 * s, y: 10 * s))
            bar.lineWidth = 2 * s
            bar.stroke()
        case .you:
            symbol("person.crop.circle.fill", size: IconSize.avatar, weight: .regular, color: NSColor(Palette.accent), box: size)
                .draw(in: NSRect(x: 0, y: 0, width: size, height: size))
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
class CardBlock: NSTextBlock {
    private let fill: NSColor?
    private let stroke: NSColor?
    private let mark: NSImage?
    private let bar: NSColor?
    private let rule: NSColor?

    private let radius: CGFloat

    init(fill: NSColor?, stroke: NSColor?, padding: CGFloat, x: CGFloat, across: CGFloat? = nil, radius: CGFloat = Radius.card,
         mark: NSImage? = nil, bar: NSColor? = nil, rule: NSColor? = nil) {
        self.radius = radius
        self.fill = fill
        self.stroke = stroke
        self.mark = mark
        self.bar = bar
        self.rule = rule
        super.init()
        setWidth(x, type: .absoluteValueType, for: .margin, edge: .minX)
        setWidth(padding, type: .absoluteValueType, for: .padding)
        if let across {
            setWidth(across, type: .absoluteValueType, for: .padding, edge: .minX)
            setWidth(across, type: .absoluteValueType, for: .padding, edge: .maxX)
        }
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
        let shape = NSBezierPath(roundedRect: box.insetBy(dx: 0.5, dy: 0.5), xRadius: radius, yRadius: radius)
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

/// A card in a row of cards: its own rounded hairline and fill over its
/// cell, as tall as the row.
final class CardCell: NSTextTableBlock {
    init(table: NSTextTable, column: Int) {
        super.init(table: table, startingRow: 0, rowSpan: 1, startingColumn: column, columnSpan: 1)
    }

    required init?(coder: NSCoder) { nil }

    override func drawBackground(withFrame frameRect: NSRect, in controlView: NSView, characterRange: NSRange, layoutManager: NSLayoutManager) {
        let box = NSRect(x: frameRect.minX + width(for: .margin, edge: .minX), y: frameRect.minY + width(for: .margin, edge: .minY),
                         width: frameRect.width - width(for: .margin, edge: .minX) - width(for: .margin, edge: .maxX),
                         height: frameRect.height - width(for: .margin, edge: .minY) - width(for: .margin, edge: .maxY))
        let shape = NSBezierPath(roundedRect: box.insetBy(dx: 0.5, dy: 0.5), xRadius: Radius.card, yRadius: Radius.card)
        NSColor(Palette.page).setFill()
        shape.fill()
        NSColor(Palette.hairline).setStroke()
        shape.lineWidth = 1
        shape.stroke()
    }
}

/// One cell of a list's rows: its tint, the hairline under it, and its
/// share of the rounded frame around the whole list.
final class RowCell: NSTextTableBlock {
    private let cell: Cell

    init(table: NSTextTable, cell: Cell) {
        self.cell = cell
        super.init(table: table, startingRow: cell.row, rowSpan: 1, startingColumn: cell.column, columnSpan: 1)
    }

    required init?(coder: NSCoder) { nil }

    override func drawBackground(withFrame frameRect: NSRect, in controlView: NSView, characterRange: NSRange, layoutManager: NSLayoutManager) {
        let box = frameRect
        let r = Radius.card
        let top = cell.row == 0, bottom = cell.last, left = cell.column == 0, right = cell.column == cell.columns - 1
        // The outline of the frame this cell is part of, its own corners rounded.
        let shape = NSBezierPath()
        let tl = top && left ? r : 0, tr = top && right ? r : 0, br = bottom && right ? r : 0, bl = bottom && left ? r : 0
        shape.move(to: NSPoint(x: box.minX + tl, y: box.minY))
        shape.line(to: NSPoint(x: box.maxX - tr, y: box.minY))
        if tr > 0 { shape.appendArc(withCenter: NSPoint(x: box.maxX - tr, y: box.minY + tr), radius: tr, startAngle: 270, endAngle: 0) }
        shape.line(to: NSPoint(x: box.maxX, y: box.maxY - br))
        if br > 0 { shape.appendArc(withCenter: NSPoint(x: box.maxX - br, y: box.maxY - br), radius: br, startAngle: 0, endAngle: 90) }
        shape.line(to: NSPoint(x: box.minX + bl, y: box.maxY))
        if bl > 0 { shape.appendArc(withCenter: NSPoint(x: box.minX + bl, y: box.maxY - bl), radius: bl, startAngle: 90, endAngle: 180) }
        shape.line(to: NSPoint(x: box.minX, y: box.minY + tl))
        if tl > 0 { shape.appendArc(withCenter: NSPoint(x: box.minX + tl, y: box.minY + tl), radius: tl, startAngle: 180, endAngle: 270) }
        shape.close()
        NSColor(cell.tint == .needsYou ? Palette.needsYouRow : Palette.page).setFill()
        shape.fill()
        NSColor(Palette.hairline).setStroke()
        let edge = NSBezierPath()
        edge.lineWidth = 1
        let inner = box.insetBy(dx: 0.5, dy: 0.5)
        if top { edge.move(to: NSPoint(x: inner.minX + tl, y: inner.minY)); edge.line(to: NSPoint(x: inner.maxX - tr, y: inner.minY)) }
        if right { edge.move(to: NSPoint(x: inner.maxX, y: inner.minY + tr)); edge.line(to: NSPoint(x: inner.maxX, y: inner.maxY - br)) }
        if bottom { edge.move(to: NSPoint(x: inner.maxX - br, y: inner.maxY)); edge.line(to: NSPoint(x: inner.minX + bl, y: inner.maxY)) }
        if left { edge.move(to: NSPoint(x: inner.minX, y: inner.maxY - bl)); edge.line(to: NSPoint(x: inner.minX, y: inner.minY + tl)) }
        if tl > 0 { edge.move(to: NSPoint(x: inner.minX, y: inner.minY + tl)); edge.appendArc(withCenter: NSPoint(x: inner.minX + tl, y: inner.minY + tl), radius: tl - 0.5, startAngle: 180, endAngle: 270) }
        if tr > 0 { edge.move(to: NSPoint(x: inner.maxX - tr, y: inner.minY)); edge.appendArc(withCenter: NSPoint(x: inner.maxX - tr, y: inner.minY + tr), radius: tr - 0.5, startAngle: 270, endAngle: 0) }
        if br > 0 { edge.move(to: NSPoint(x: inner.maxX, y: inner.maxY - br)); edge.appendArc(withCenter: NSPoint(x: inner.maxX - br, y: inner.maxY - br), radius: br - 0.5, startAngle: 0, endAngle: 90) }
        if bl > 0 { edge.move(to: NSPoint(x: inner.minX + bl, y: inner.maxY)); edge.appendArc(withCenter: NSPoint(x: inner.minX + bl, y: inner.maxY - bl), radius: bl - 0.5, startAngle: 90, endAngle: 180) }
        edge.stroke()
        if !bottom {
            NSColor(Palette.inner).setFill()
            NSRect(x: box.minX, y: box.maxY - 1, width: box.width, height: 1).fill()
        }
    }
}

/// What the person said: a card on the right, only as wide as its words.
final class BubbleBlock: CardBlock {
    override func rectForLayout(at startingPoint: NSPoint, in rect: NSRect, textContainer: NSTextContainer, characterRange: NSRange) -> NSRect {
        let r = super.rectForLayout(at: startingPoint, in: rect, textContainer: textContainer, characterRange: characterRange)
        guard let storage = textContainer.layoutManager?.textStorage,
              let range = storage.string.utf16.count >= NSMaxRange(characterRange) ? Optional(characterRange) : nil else { return r }
        let words = NSMutableAttributedString(attributedString: storage.attributedSubstring(from: range))
        words.removeAttribute(.paragraphStyle, range: NSRange(location: 0, length: words.length))
        let natural = ceil(words.boundingRect(with: NSSize(width: 100_000, height: 100_000), options: [.usesLineFragmentOrigin]).width) + 1
        let width = min(r.width, natural)
        return NSRect(x: r.maxX - width, y: r.minY, width: width, height: r.height)
    }
}

/// Something that happened: hairlines either side of its centred words, or
/// a day's name with a hairline after it.
final class LineBlock: NSTextBlock {
    private let color: NSColor
    private let both: Bool

    init(color: NSColor, x: CGFloat, both: Bool) {
        self.color = color
        self.both = both
        super.init()
        setWidth(x, type: .absoluteValueType, for: .margin, edge: .minX)
        setWidth(Space.s1, type: .absoluteValueType, for: .padding, edge: .minY)
        setWidth(Space.s1, type: .absoluteValueType, for: .padding, edge: .maxY)
        setContentWidth(100, type: .percentageValueType)
    }

    required init?(coder: NSCoder) { nil }

    override func drawBackground(withFrame frameRect: NSRect, in controlView: NSView, characterRange: NSRange, layoutManager: NSLayoutManager) {
        guard let container = layoutManager.textContainers.first, characterRange.length > 0 else { return }
        let glyphs = layoutManager.glyphRange(forCharacterRange: NSRange(location: characterRange.location, length: characterRange.length - 1), actualCharacterRange: nil)
        let words = layoutManager.boundingRect(forGlyphRange: glyphs, in: container)
        let left = frameRect.minX + width(for: .margin, edge: .minX)
        let right = frameRect.maxX - width(for: .margin, edge: .maxX)
        let y = words.midY.rounded()
        color.setFill()
        if both { NSRect(x: left, y: y, width: max(0, words.minX - 10 - left), height: 1).fill() }
        NSRect(x: words.maxX + (both ? 10 : Space.s3), y: y, width: max(0, right - words.maxX - (both ? 10 : Space.s3)), height: 1).fill()
    }
}

/// How a pill is drawn: its tint, its edge and its words' colour.
final class PillLook: NSObject {
    let fill: NSColor
    let stroke: NSColor?
    let text: NSColor

    init(_ pill: Pill) {
        switch pill {
        case .tone(let tone):
            fill = NSColor(BadgeTone(tone).fill)
            stroke = nil
            text = NSColor(BadgeTone(tone).text)
        case .failure:
            fill = NSColor(Palette.failure.opacity(0.14))
            stroke = nil
            text = NSColor(Palette.failure)
        case .tag:
            fill = NSColor(Palette.tagFill)
            stroke = NSColor(Palette.hairline)
            text = NSColor(Palette.stepText)
        }
    }
}

/// Lays out a transcript and draws its pills behind their words.
final class PillLayoutManager: NSLayoutManager {
    override func drawBackground(forGlyphRange glyphsToShow: NSRange, at origin: NSPoint) {
        super.drawBackground(forGlyphRange: glyphsToShow, at: origin)
        guard let storage = textStorage, let container = textContainers.first else { return }
        let chars = characterRange(forGlyphRange: glyphsToShow, actualGlyphRange: nil)
        // A text view draws on the main thread only.
        nonisolated(unsafe) let view = container.textView
        let selected = MainActor.assumeIsolated { (view?.selectedRanges ?? []).map(\.rangeValue) }
        storage.enumerateAttribute(.wdPill, in: chars) { value, range, _ in
            guard let look = value as? PillLook else { return }
            if selected.contains(where: { NSIntersectionRange($0, range).length > 0 }) { return }
            let glyphs = glyphRange(forCharacterRange: range, actualCharacterRange: nil)
            let font = storage.attribute(.font, at: range.location, effectiveRange: nil) as? NSFont ?? .systemFont(ofSize: 11)
            enumerateEnclosingRects(forGlyphRange: glyphs, withinSelectedGlyphRange: NSRange(location: NSNotFound, length: 0), in: container) { rect, _ in
                let line = self.lineFragmentRect(forGlyphAt: glyphs.location, effectiveRange: nil)
                let baseline = line.minY + self.location(forGlyphAt: glyphs.location).y
                let height = ceil(font.ascender - font.descender) + 4
                let box = NSRect(x: rect.minX - 6 + origin.x, y: baseline - font.ascender - 2 + origin.y, width: rect.width + 12, height: height)
                let shape = NSBezierPath(roundedRect: box, xRadius: Radius.badge, yRadius: Radius.badge)
                look.fill.setFill()
                shape.fill()
                if let stroke = look.stroke {
                    stroke.setStroke()
                    shape.lineWidth = 1
                    NSBezierPath(roundedRect: box.insetBy(dx: 0.5, dy: 0.5), xRadius: Radius.badge, yRadius: Radius.badge).stroke()
                }
            }
        }
    }
}

extension Para {

    /// Whether all it holds is one control, set across its whole line.
    var isWholeSlot: Bool {
        guard runs.count == 1, case .block = runs[0].mark else { return false }
        return true
    }
}

