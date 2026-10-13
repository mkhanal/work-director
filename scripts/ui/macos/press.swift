// Presses the first control of a role whose title or description contains
// some text, through the accessibility API. System Events cannot read the
// names SwiftUI gives its controls (they come back as missing value), so the
// UI checks press through this instead.
//   swift press.swift <pid> <role> <text>
import ApplicationServices
import Foundation

let args = CommandLine.arguments
guard args.count == 4, let pid = pid_t(args[1]) else {
    FileHandle.standardError.write("usage: press.swift <pid> <role> <text>\n".data(using: .utf8)!)
    exit(2)
}
let (role, text) = (args[2], args[3])

func attr(_ e: AXUIElement, _ name: String) -> CFTypeRef? {
    var v: CFTypeRef?
    return AXUIElementCopyAttributeValue(e, name as CFString, &v) == .success ? v : nil
}

func string(_ v: CFTypeRef?) -> String {
    if let s = v as? String { return s }
    if let a = v as? NSAttributedString { return a.string }
    return ""
}

func find(_ e: AXUIElement, depth: Int) -> AXUIElement? {
    if string(attr(e, kAXRoleAttribute)) == role,
       (string(attr(e, kAXTitleAttribute)) + string(attr(e, kAXDescriptionAttribute))).contains(text) {
        return e
    }
    guard depth < 80, let kids = attr(e, kAXChildrenAttribute) as? [AXUIElement] else { return nil }
    for k in kids { if let hit = find(k, depth: depth + 1) { return hit } }
    return nil
}

guard let windows = attr(AXUIElementCreateApplication(pid), kAXWindowsAttribute) as? [AXUIElement],
      let hit = windows.lazy.compactMap({ find($0, depth: 0) }).first else {
    print("missing")
    exit(1)
}
print(AXUIElementPerformAction(hit, kAXPressAction as CFString) == .success ? "clicked" : "refused")
