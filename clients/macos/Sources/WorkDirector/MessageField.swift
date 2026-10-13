import AppKit
import SwiftUI
import WorkDirectorKit

/// The text a person types a message into. Return is read from the key event
/// itself, so the chord the person chose sends and every other chord with
/// Return starts a new line; a SwiftUI field submits on Return whatever was held.
struct MessageField: NSViewRepresentable {
    @Binding var text: String
    let placeholder: String
    let sendKey: SendKey
    let enabled: Bool
    /// The height the text needs, from one line up to `maxLines`.
    @Binding var height: CGFloat
    /// Takes the files pasted or dropped on the field; nil where nothing can be attached.
    var attach: (([Attaching]) -> Void)? = nil
    let send: () -> Void

    static let font = NSFont.systemFont(ofSize: 14)
    static let maxLines: CGFloat = 10

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let view = PlaceholderTextView()
        view.font = Self.font
        view.isRichText = false
        view.allowsUndo = true
        view.drawsBackground = false
        view.textContainerInset = .zero
        view.textContainer?.lineFragmentPadding = 0
        view.textContainer?.widthTracksTextView = true
        view.isVerticallyResizable = true
        view.isHorizontallyResizable = false
        view.autoresizingMask = [.width]
        view.isAutomaticQuoteSubstitutionEnabled = false
        view.isAutomaticDashSubstitutionEnabled = false
        view.delegate = context.coordinator

        let scroll = NSScrollView()
        scroll.documentView = view
        scroll.drawsBackground = false
        scroll.borderType = .noBorder
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        context.coordinator.view = view
        DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        context.coordinator.field = self
        guard let view = context.coordinator.view else { return }
        if view.string != text {
            view.string = text
            view.setSelectedRange(NSRange(location: (text as NSString).length, length: 0))
        }
        view.isEditable = enabled
        view.placeholder = placeholder
        view.attach = attach
        view.needsDisplay = true
        context.coordinator.measure()
    }

    @MainActor
    final class Coordinator: NSObject, NSTextViewDelegate {
        var field: MessageField
        weak var view: PlaceholderTextView?

        init(_ field: MessageField) { self.field = field }

        func textDidChange(_ notification: Notification) {
            guard let view else { return }
            field.text = view.string
            measure()
        }

        func textView(_ textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            guard let event = NSApp.currentEvent, event.type == .keyDown,
                  event.keyCode == 36 || event.keyCode == 76 else { return false }
            switch field.sendKey.press(Self.chord(event.modifierFlags)) {
            case .send:
                field.send()
            case .newline:
                textView.insertNewlineIgnoringFieldEditor(nil)
            }
            return true
        }

        static func chord(_ flags: NSEvent.ModifierFlags) -> Chord {
            var chord: Chord = []
            if flags.contains(.shift) { chord.insert(.shift) }
            if flags.contains(.command) { chord.insert(.command) }
            if flags.contains(.option) { chord.insert(.option) }
            if flags.contains(.control) { chord.insert(.control) }
            return chord
        }

        func measure() {
            guard let view, let layout = view.layoutManager, let container = view.textContainer else { return }
            layout.ensureLayout(for: container)
            let line = layout.defaultLineHeight(for: MessageField.font)
            let used = max(layout.usedRect(for: container).height, line)
            let wanted = min(used, line * MessageField.maxLines).rounded(.up)
            if abs(wanted - field.height) > 0.5 {
                let binding = field.$height
                DispatchQueue.main.async { binding.wrappedValue = wanted }
            }
        }
    }
}

/// A text view that shows what to type while it is empty.
final class PlaceholderTextView: NSTextView {
    var placeholder = ""
    /// Where pasted and dropped files go; without it they paste as text.
    var attach: (([Attaching]) -> Void)?

    override func paste(_ sender: Any?) {
        if let attach, let files = Self.files(on: .general) {
            attach(files)
        } else {
            super.paste(sender)
        }
    }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        attach != nil && Self.fileURLs(on: sender.draggingPasteboard) != nil ? .copy : super.draggingEntered(sender)
    }

    override func draggingUpdated(_ sender: NSDraggingInfo) -> NSDragOperation {
        attach != nil && Self.fileURLs(on: sender.draggingPasteboard) != nil ? .copy : super.draggingUpdated(sender)
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        if let attach, let urls = Self.fileURLs(on: sender.draggingPasteboard) {
            attach(urls.compactMap { try? Attaching.file($0) })
            return true
        }
        return super.performDragOperation(sender)
    }

    static func fileURLs(on board: NSPasteboard) -> [URL]? {
        let urls = board.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        return urls.isEmpty ? nil : urls
    }

    /// The files a paste brings: copied files, else an image with no text,
    /// written as a PNG. Nil when it is text to paste.
    static func files(on board: NSPasteboard) -> [Attaching]? {
        if let urls = fileURLs(on: board) { return urls.compactMap { try? Attaching.file($0) } }
        guard board.string(forType: .string) == nil, let image = NSImage(pasteboard: board),
              let tiff = image.tiffRepresentation, let rep = NSBitmapImageRep(data: tiff),
              let png = rep.representation(using: .png, properties: [:]),
              let file = try? Attaching.png(png, in: Attaching.pasteDir) else { return nil }
        return [file]
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        guard string.isEmpty, !placeholder.isEmpty else { return }
        let attrs: [NSAttributedString.Key: Any] = [
            .font: font ?? MessageField.font,
            .foregroundColor: NSColor.placeholderTextColor,
        ]
        let rect = bounds.insetBy(dx: textContainerInset.width, dy: textContainerInset.height)
        (placeholder as NSString).draw(with: rect, options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine], attributes: attrs)
    }

    override func didChangeText() {
        super.didChangeText()
        needsDisplay = true
    }
}
