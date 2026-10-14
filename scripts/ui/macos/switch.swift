// Times how long a press takes to show something: presses the first control
// of a role whose title or description holds some text, through the
// accessibility API, then reads the window until an element's text holds the
// awaited words. Each read waits for the app's main thread, so the time is
// how long until the app shows the words and answers again; the longest
// single read is the longest the app stopped answering. The app may answer a
// read from a run loop nested inside a long stretch of work, so this can read
// short; run the app with WD_SWITCH_LOG=1 for the longest pass of its main run
// loop after each switch, which it measures itself.
//   swift switch.swift <pid> <role> [last:]<text> <awaited words> [timeout s]
import ApplicationServices
import Foundation

let args = CommandLine.arguments
guard args.count >= 5, let pid = pid_t(args[1]) else {
    FileHandle.standardError.write("usage: switch.swift <pid> <role> <text> <awaited words> [timeout s]\n".data(using: .utf8)!)
    exit(2)
}
let (role, text, awaited) = (args[2], args[3], args[4])
let timeout = args.count > 5 ? Double(args[5]) ?? 20 : 20
let app = AXUIElementCreateApplication(pid)
AXUIElementSetMessagingTimeout(app, Float(timeout))

func attr(_ e: AXUIElement, _ name: String) -> CFTypeRef? {
    var v: CFTypeRef?
    return AXUIElementCopyAttributeValue(e, name as CFString, &v) == .success ? v : nil
}

func words(_ e: AXUIElement) -> String {
    [kAXTitleAttribute, kAXValueAttribute, kAXDescriptionAttribute].map { attr(e, $0) as? String ?? "" }.joined(separator: " ")
}

func find(_ e: AXUIElement, depth: Int, _ match: (AXUIElement) -> Bool) -> AXUIElement? {
    if match(e) { return e }
    guard depth < 80, let kids = attr(e, kAXChildrenAttribute) as? [AXUIElement] else { return nil }
    for k in kids { if let hit = find(k, depth: depth + 1, match) { return hit } }
    return nil
}

func inWindows(_ match: (AXUIElement) -> Bool) -> AXUIElement? {
    for w in attr(app, kAXWindowsAttribute) as? [AXUIElement] ?? [] {
        if let hit = find(w, depth: 0, match) { return hit }
    }
    return nil
}

func every(_ e: AXUIElement, depth: Int, _ match: (AXUIElement) -> Bool, into out: inout [AXUIElement]) {
    if match(e) { out.append(e) }
    guard depth < 80, let kids = attr(e, kAXChildrenAttribute) as? [AXUIElement] else { return }
    for k in kids { every(k, depth: depth + 1, match, into: &out) }
}

/// The control to press: the first holding the text, or the last when the
/// text starts `last:`.
func control() -> AXUIElement? {
    let last = text.hasPrefix("last:")
    let needle = last ? String(text.dropFirst(5)) : text
    var hits: [AXUIElement] = []
    for w in attr(app, kAXWindowsAttribute) as? [AXUIElement] ?? [] {
        every(w, depth: 0, { attr($0, kAXRoleAttribute) as? String == role && words($0).contains(needle) }, into: &hits)
    }
    return last ? hits.last : hits.first
}

guard let control = control() else {
    let near = inWindows { words($0).contains(text) }
    print("missing \(role) \(text); \(near.map { attr($0, kAXRoleAttribute) as? String ?? "?" } ?? "nothing") holds it")
    exit(1)
}
let start = Date()
guard AXUIElementPerformAction(control, kAXPressAction as CFString) == .success else {
    print("refused")
    exit(1)
}
// A cheap read every few milliseconds measures how long the app's main
// thread kept it waiting: the longest is the worst hitch a person would feel.
var longest: TimeInterval = 0
var searched = Date.distantPast
while Date().timeIntervalSince(start) < timeout {
    let probe = Date()
    _ = attr(app, kAXFocusedWindowAttribute)
    longest = max(longest, Date().timeIntervalSince(probe))
    if Date().timeIntervalSince(searched) > 0.05 {
        searched = Date()
        if inWindows({ words($0).contains(awaited) }) != nil {
            print(String(format: "shown after %.0f ms; the app answered within %.0f ms throughout", Date().timeIntervalSince(start) * 1000, longest * 1000))
            exit(0)
        }
    }
    usleep(5000)
}
print("not shown within \(timeout) s")
exit(1)
