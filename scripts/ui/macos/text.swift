// Reads and selects in an app's text areas through the accessibility API,
// so a UI check can prove what a person's selection would hold without
// posting a single mouse or key event.
//   swift text.swift <pid> count                 how many text areas, and each one's length
//   swift text.swift <pid> value <n>             the nth text area's text
//   swift text.swift <pid> select <n> <at> <len> selects in the nth, prints what is selected
//   swift text.swift <pid> link <text>           presses the first link holding the text
//   swift text.swift <pid> show <n>             scrolls the nth text area into view
//   swift text.swift <pid> scroll <fraction>     scrolls the window's first scroll area (0 top, 1 bottom)
import ApplicationServices
import Foundation

let args = CommandLine.arguments
guard args.count >= 3, let pid = pid_t(args[1]) else {
    FileHandle.standardError.write("usage: text.swift <pid> count|value|select|scroll …\n".data(using: .utf8)!)
    exit(2)
}

func attr(_ e: AXUIElement, _ name: String) -> CFTypeRef? {
    var v: CFTypeRef?
    return AXUIElementCopyAttributeValue(e, name as CFString, &v) == .success ? v : nil
}

func all(_ e: AXUIElement, role: String, depth: Int = 0, into out: inout [AXUIElement]) {
    if attr(e, kAXRoleAttribute) as? String == role { out.append(e) }
    guard depth < 80, let kids = attr(e, kAXChildrenAttribute) as? [AXUIElement] else { return }
    for k in kids { all(k, role: role, depth: depth + 1, into: &out) }
}

func found(_ role: String) -> [AXUIElement] {
    var out: [AXUIElement] = []
    for w in attr(AXUIElementCreateApplication(pid), kAXWindowsAttribute) as? [AXUIElement] ?? [] { all(w, role: role, into: &out) }
    return out
}

let areas = found(kAXTextAreaRole)
switch args[2] {
case "count":
    print(areas.count)
    for (i, a) in areas.enumerated() { print(i, (attr(a, kAXValueAttribute) as? String)?.utf16.count ?? -1) }
case "value":
    guard args.count == 4, let n = Int(args[3]), areas.indices.contains(n) else { exit(1) }
    print(attr(areas[n], kAXValueAttribute) as? String ?? "")
case "select":
    guard args.count == 6, let n = Int(args[3]), let at = Int(args[4]), let len = Int(args[5]), areas.indices.contains(n) else { exit(1) }
    var range = CFRange(location: at, length: len)
    let value = AXValueCreate(.cfRange, &range)!
    let set = AXUIElementSetAttributeValue(areas[n], kAXSelectedTextRangeAttribute as CFString, value)
    guard set == .success else { print("refused \(set.rawValue)"); exit(1) }
    print(attr(areas[n], kAXSelectedTextAttribute) as? String ?? "")
case "show":
    guard args.count == 4, let n = Int(args[3]), areas.indices.contains(n) else { exit(1) }
    print(AXUIElementPerformAction(areas[n], "AXScrollToVisible" as CFString) == .success ? "shown" : "refused")
case "link":
    guard args.count == 4 else { exit(1) }
    let links = found("AXLink")
    let words = { (e: AXUIElement) in [kAXTitleAttribute, kAXValueAttribute, kAXDescriptionAttribute].map { attr(e, $0) as? String ?? "" }.joined(separator: " ") }
    guard let hit = links.first(where: { words($0).contains(args[3]) }) else {
        print("missing among \(links.count): \(links.prefix(8).map(words))")
        exit(1)
    }
    print(AXUIElementPerformAction(hit, kAXPressAction as CFString) == .success ? "pressed" : "refused")
case "scroll":
    let areas = found(kAXScrollAreaRole)
    guard args.count == 4, let f = Double(args[3]),
          let bar = areas.lazy.compactMap({ attr($0, kAXVerticalScrollBarAttribute) }).first else { print("missing \(areas.count)"); exit(1) }
    let set = AXUIElementSetAttributeValue(bar as! AXUIElement, kAXValueAttribute as CFString, f as CFNumber)
    print(set == .success ? "scrolled" : "refused \(set.rawValue)")
default:
    exit(2)
}
