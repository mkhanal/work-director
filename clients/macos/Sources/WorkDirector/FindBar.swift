import AppKit
import SwiftUI
import WorkDirectorKit

/// What the Find menu asks of the page in front.
enum FindCommand {
    case show, next, previous, useSelection
}

extension Notification.Name {
    /// A Find menu command for the page in front; the command is the object.
    static let findInPage = Notification.Name("wd.findInPage")
    /// A transcript's text changed; the text view is the object.
    static let transcriptChanged = Notification.Name("wd.transcriptChanged")
}

/// Find in page: every transcript a page shows, searched top to bottom as
/// one. Matches are marked in the layout, never in the text, so selecting,
/// copying and a refresh's edits are untouched by them.
@MainActor
@Observable
final class PageFind {
    private(set) var shown = false
    /// What is searched for, as typed.
    var query = ""
    private(set) var count = 0
    private(set) var current: Int?
    /// Bumped each time the field is to take the keyboard.
    private(set) var focusing = 0

    /// The window whose page is searched.
    @ObservationIgnored weak var window: NSWindow?
    /// The texts searched, top to bottom; the window's transcripts unless told otherwise.
    @ObservationIgnored var texts: () -> [TranscriptTextView] = { [] }
    @ObservationIgnored private var matches: [Match] = []
    /// The texts carrying marks, to clear them from.
    @ObservationIgnored private var marked: [TranscriptTextView] = []
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var refreshQueued = false

    struct Match {
        let view: TranscriptTextView
        let range: NSRange
    }

    init() {
        texts = { [weak self] in Self.transcripts(in: self?.window) }
    }

    var label: String { Find.label(current, of: count, query: query) }

    /// The current match, for a check to read.
    var currentMatch: Match? { current.map { matches[$0] } }

    func run(_ command: FindCommand) {
        switch command {
        case .show:
            show()
        case .next:
            go(by: 1)
        case .previous:
            go(by: -1)
        case .useSelection:
            guard let view = window?.firstResponder as? TranscriptTextView, let storage = view.textStorage,
                  let range = view.selectedRanges.first?.rangeValue, range.length > 0 else { return }
            query = storage.attributedSubstring(from: range).string
            show()
        }
    }

    func show() {
        shown = true
        focusing += 1
    }

    /// The next match, or the previous one, scrolled into view; the bar
    /// opens first if it was closed.
    func go(by step: Int) {
        if !shown { show() }
        guard let next = Find.step(current, by: step, count: count) else { return }
        let was = current
        current = next
        mark(was, current: false)
        mark(next, current: true)
        reveal(next)
    }

    /// Closes the bar: the marks go, and the current match is left selected
    /// so Copy takes it.
    func close() {
        if let current {
            let match = matches[current]
            match.view.setSelectedRange(match.range)
            window?.makeFirstResponder(match.view)
        }
        generation += 1
        clear()
        matches = []
        count = 0
        current = nil
        shown = false
    }

    /// Searches again once the page has changed, keeping the current match
    /// where it was; what the person reads stays where it is.
    func refresh() {
        guard shown, !query.isEmpty, !refreshQueued else { return }
        refreshQueued = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            refreshQueued = false
            Task { await self.search(scroll: false) }
        }
    }

    /// Searches every text for the query off the main thread, marks what is
    /// found and keeps current the match at or after the one that was, or
    /// at or after the top of what shows.
    func search(scroll: Bool) async {
        generation += 1
        let mine = generation
        let parts = shown ? texts() : []
        let query = query
        let versions = parts.map(\.version)
        let strings = parts.map { $0.textStorage?.string ?? "" }
        let found = await Task.detached(priority: .userInitiated) { strings.map { Find.ranges(of: query, in: $0) } }.value
        // A text that changed while it was searched is searched again; its
        // change has asked for that already.
        guard mine == generation, parts.map(\.version) == versions else { return }
        let was = currentMatch.flatMap { m in parts.firstIndex { $0 === m.view }.map { Find.Spot(part: $0, at: m.range.location) } } ?? top(of: parts)
        clear()
        matches = zip(parts, found).flatMap { view, ranges in ranges.map { Match(view: view, range: $0) } }
        count = matches.count
        let spots = matches.map { m in Find.Spot(part: parts.firstIndex { $0 === m.view }!, at: m.range.location) }
        current = Find.keep(was, in: spots)
        marked = parts
        for i in matches.indices { mark(i, current: i == current) }
        if scroll, let current { reveal(current) }
    }

    /// Where the page shows its top: the first text in view, at its first
    /// character in view.
    private func top(of parts: [TranscriptTextView]) -> Find.Spot {
        for (i, view) in parts.enumerated() where !view.visibleRect.isEmpty {
            return Find.Spot(part: i, at: view.characterIndexForInsertion(at: view.visibleRect.origin))
        }
        return Find.Spot(part: 0, at: 0)
    }

    private func mark(_ i: Int?, current: Bool) {
        guard let i, let layout = matches[i].view.layoutManager else { return }
        let range = matches[i].range
        layout.removeTemporaryAttribute(.foregroundColor, forCharacterRange: range)
        layout.addTemporaryAttribute(.backgroundColor, value: current ? NSColor.findHighlightColor : Self.found, forCharacterRange: range)
        // The current match is black on its yellow in either appearance.
        if current { layout.addTemporaryAttribute(.foregroundColor, value: NSColor.black, forCharacterRange: range) }
    }

    /// Every match but the current one.
    private static let found = NSColor.systemYellow.withAlphaComponent(0.35)

    private func clear() {
        for view in marked {
            guard let layout = view.layoutManager, let storage = view.textStorage else { continue }
            let all = NSRange(location: 0, length: storage.length)
            layout.removeTemporaryAttribute(.backgroundColor, forCharacterRange: all)
            layout.removeTemporaryAttribute(.foregroundColor, forCharacterRange: all)
        }
        marked = []
    }

    /// Scrolls the page so a match shows with some of what surrounds it.
    private func reveal(_ i: Int) {
        let match = matches[i]
        guard let layout = match.view.layoutManager, let container = match.view.textContainer else { return }
        let glyphs = layout.glyphRange(forCharacterRange: match.range, actualCharacterRange: nil)
        let origin = match.view.textContainerOrigin
        let rect = layout.boundingRect(forGlyphRange: glyphs, in: container).offsetBy(dx: origin.x, dy: origin.y)
        match.view.scrollToVisible(rect.insetBy(dx: 0, dy: -Space.section * 2))
    }

    /// The transcripts a window shows, top to bottom.
    static func transcripts(in window: NSWindow?) -> [TranscriptTextView] {
        var found: [(view: TranscriptTextView, top: CGFloat, left: CGFloat)] = []
        func walk(_ view: NSView) {
            if let text = view as? TranscriptTextView, !text.isHiddenOrHasHiddenAncestor {
                let frame = text.convert(text.bounds, to: nil)
                found.append((text, frame.maxY, frame.minX))
            }
            view.subviews.forEach(walk)
        }
        window?.contentView.map(walk)
        return found.sorted { $0.top != $1.top ? $0.top > $1.top : $0.left < $1.left }.map(\.view)
    }
}

/// The find bar at the top of a page: a search field, how many it found,
/// the previous and next match, and Done.
struct FindBar: View {
    let find: PageFind

    var body: some View {
        VStack(spacing: 0) {
            if find.shown {
                HStack(spacing: Space.s2) {
                    FindField(find: find)
                        .frame(maxWidth: 320)
                    Text(find.label)
                        .font(TypeScale.caption)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(find.label)
                    Spacer(minLength: 0)
                    ControlGroup {
                        Button { find.go(by: -1) } label: { Image(systemName: "chevron.left") }
                            .help("Previous (⇧⌘G)")
                            .accessibilityLabel("Previous")
                        Button { find.go(by: 1) } label: { Image(systemName: "chevron.right") }
                            .help("Next (⌘G)")
                            .accessibilityLabel("Next")
                    }
                    .disabled(find.count == 0)
                    .fixedSize()
                    Button("Done") { find.close() }
                }
                .padding(.horizontal, Space.gutter)
                .padding(.vertical, Space.s2)
                .background(.bar)
                Rectangle().fill(Palette.hairline).frame(height: 1)
            }
        }
        .background(WindowReader { find.window = $0 })
        .task(id: find.shown ? find.query : nil) {
            guard find.shown else { return }
            await find.search(scroll: true)
        }
        .onReceive(NotificationCenter.default.publisher(for: .findInPage)) { note in
            find.run(note.object as! FindCommand)
        }
        .onReceive(NotificationCenter.default.publisher(for: .transcriptChanged)) { note in
            if (note.object as! TranscriptTextView).window === find.window { find.refresh() }
        }
    }
}

/// Tells the window a view is in, once it is in one.
private struct WindowReader: NSViewRepresentable {
    let found: (NSWindow?) -> Void

    func makeNSView(context: Context) -> Reader {
        let view = Reader()
        view.found = found
        return view
    }

    func updateNSView(_ view: Reader, context: Context) {}

    final class Reader: NSView {
        var found: (NSWindow?) -> Void = { _ in }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            found(window)
        }
    }
}

/// The search field: typing searches, Return goes to the next match and
/// Shift-Return to the previous one, Esc closes the bar.
private struct FindField: NSViewRepresentable {
    let find: PageFind

    func makeCoordinator() -> Coordinator { Coordinator(find: find) }

    func makeNSView(context: Context) -> NSSearchField {
        let field = NSSearchField()
        field.placeholderString = "Find"
        field.setAccessibilityLabel("Find")
        field.sendsSearchStringImmediately = true
        field.delegate = context.coordinator
        field.stringValue = find.query
        return field
    }

    func updateNSView(_ field: NSSearchField, context: Context) {
        if field.stringValue != find.query { field.stringValue = find.query }
        guard context.coordinator.focused != find.focusing else { return }
        context.coordinator.focused = find.focusing
        DispatchQueue.main.async {
            field.window?.makeFirstResponder(field)
            field.currentEditor()?.selectAll(nil)
        }
    }

    final class Coordinator: NSObject, NSSearchFieldDelegate {
        let find: PageFind
        var focused = 0

        init(find: PageFind) { self.find = find }

        func controlTextDidChange(_ note: Notification) {
            guard let field = note.object as? NSSearchField else { return }
            MainActor.assumeIsolated { find.query = field.stringValue }
        }

        func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            MainActor.assumeIsolated {
                switch selector {
                case #selector(NSResponder.insertNewline(_:)):
                    find.go(by: NSApp.currentEvent?.modifierFlags.contains(.shift) == true ? -1 : 1)
                    return true
                case #selector(NSResponder.cancelOperation(_:)):
                    find.close()
                    return true
                default:
                    return false
                }
            }
        }
    }
}

/// The Find items of the Edit menu.
struct FindCommands: Commands {
    var body: some Commands {
        CommandGroup(after: .textEditing) {
            Menu("Find") {
                Button("Find…") { post(.show) }
                    .keyboardShortcut("f", modifiers: .command)
                Button("Find Next") { post(.next) }
                    .keyboardShortcut("g", modifiers: .command)
                Button("Find Previous") { post(.previous) }
                    .keyboardShortcut("g", modifiers: [.command, .shift])
                Button("Use Selection for Find") { post(.useSelection) }
                    .keyboardShortcut("e", modifiers: .command)
            }
        }
    }

    private func post(_ command: FindCommand) {
        NotificationCenter.default.post(name: .findInPage, object: command)
    }
}
