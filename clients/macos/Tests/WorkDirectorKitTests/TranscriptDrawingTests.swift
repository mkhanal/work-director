import AppKit
import Testing
@testable import WorkDirector
@testable import WorkDirectorKit

/// The transcript's text drawn for real, off screen: what AppKit does with the
/// boxes and tables the setter makes.
@MainActor
struct TranscriptDrawingTests {
    /// A report as a goal's conversation shows it: its markdown, a table among
    /// it, inside the report's card.
    func report(_ id: String, filler: Int = 0) -> TranscriptItem {
        let text = """
        Checked every query path.

        | Path | Before | After |
        |------|:------:|------:|
        | read | open | scoped |
        | write | open | scoped |

        \(String(repeating: "All of it holds. ", count: filler))
        """
        let card = Box.card(id)
        let paras = [Para([Run("Report", .calloutMedium)], indent: 1, gap: .row, box: card)]
            + Builder.markdown(text, id: id, indent: 0).map { p in
                var p = p
                p.outer = card
                p.indent = 1
                return p
            }
        return TranscriptItem(id: id, paras: paras, source: text)
    }

    /// Draws everything the view holds, as the window would.
    func draw(_ view: TranscriptTextView) {
        let height = view.height(for: view.frame.width)
        view.setFrameSize(NSSize(width: view.frame.width, height: max(height, 1)))
        guard let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) else {
            Issue.record("no bitmap for the text")
            return
        }
        view.cacheDisplay(in: view.bounds, to: rep)
    }

    @Test func aTableInsideACardDraws() {
        let view = TranscriptTextView()
        view.setFrameSize(NSSize(width: 640, height: 10))
        view.show(Transcript(items: [report("e1")]))
        draw(view)
        // Edited in place from the first item that changed.
        view.show(Transcript(items: [report("e1"), report("e2")]))
        draw(view)
    }

    @Test func aLongTranscriptWithATableInsideACardDrawsOnceSetOffTheMainThread() async {
        let view = TranscriptTextView()
        view.setFrameSize(NSSize(width: 640, height: 10))
        let long = Transcript(items: (0..<4).map { report("e\($0)", filler: 400) })
        #expect(long.length > TranscriptTextView.long)
        await withCheckedContinuation { (done: CheckedContinuation<Void, Never>) in
            // Landed once the whole text is set and moved in.
            view.landed = { [weak view] in
                guard let view, view.shown == long else { return }
                view.landed = {}
                done.resume()
            }
            view.show(long)
        }
        draw(view)
    }
}
