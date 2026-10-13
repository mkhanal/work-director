// Prints the window number of a process's largest on-screen window, so a UI
// check can photograph that window alone (screencapture -l) without bringing
// it to the front or capturing anything else on a screen a person is using.
//   swift window.swift <pid>
import CoreGraphics
import Foundation

guard CommandLine.arguments.count == 2, let pid = Int32(CommandLine.arguments[1]) else {
    FileHandle.standardError.write("usage: window.swift <pid>\n".data(using: .utf8)!)
    exit(2)
}
let all = CGWindowListCopyWindowInfo([.optionAll], kCGNullWindowID) as? [[String: Any]] ?? []
let mine = all.filter { ($0[kCGWindowOwnerPID as String] as? Int32) == pid && ($0[kCGWindowLayer as String] as? Int) == 0 }
let area: ([String: Any]) -> Double = { w in
    let b = w[kCGWindowBounds as String] as? [String: Double] ?? [:]
    return (b["Width"] ?? 0) * (b["Height"] ?? 0)
}
guard let best = mine.max(by: { area($0) < area($1) }), let n = best[kCGWindowNumber as String] as? Int else { exit(1) }
print(n)
