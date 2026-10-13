// Scrolls whatever is under a screen point, as a wheel would: the UI check's
// way to reach the end of a page that keyboard focus can't scroll.
//   swift scroll.swift <x> <y> <lines>
import CoreGraphics
import Foundation

let args = CommandLine.arguments
guard args.count == 4, let x = Double(args[1]), let y = Double(args[2]), let lines = Int32(args[3]) else {
    FileHandle.standardError.write("usage: scroll.swift <x> <y> <lines>\n".data(using: .utf8)!)
    exit(2)
}
let point = CGPoint(x: x, y: y)
CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: point, mouseButton: .left)?.post(tap: .cghidEventTap)
usleep(100_000)
for _ in 0..<10 {
    CGEvent(scrollWheelEvent2Source: nil, units: .line, wheelCount: 1, wheel1: -lines / 10, wheel2: 0, wheel3: 0)?.post(tap: .cghidEventTap)
    usleep(30_000)
}
// Off the page again, so no hover tooltip lands in the photograph.
usleep(200_000)
CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: CGPoint(x: 1, y: 1), mouseButton: .left)?.post(tap: .cghidEventTap)
