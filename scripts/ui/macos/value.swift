// Sets the value of the first element of a role whose title or description
// holds some text, through the accessibility API, as typing would leave it.
//   swift value.swift <pid> <role> <text> <value>
import ApplicationServices
import Foundation

let args = CommandLine.arguments
guard args.count == 5, let pid = pid_t(args[1]) else {
    FileHandle.standardError.write("usage: value.swift <pid> <role> <text> <value>\n".data(using: .utf8)!)
    exit(2)
}
let (role, text, value) = (args[2], args[3], args[4])

func attr(_ e: AXUIElement, _ name: String) -> CFTypeRef? {
    var v: CFTypeRef?
    return AXUIElementCopyAttributeValue(e, name as CFString, &v) == .success ? v : nil
}

func find(_ e: AXUIElement, depth: Int) -> AXUIElement? {
    let words = [kAXTitleAttribute, kAXDescriptionAttribute, kAXPlaceholderValueAttribute].map { attr(e, $0) as? String ?? "" }.joined()
    if attr(e, kAXRoleAttribute) as? String == role, words.contains(text) { return e }
    guard depth < 80, let kids = attr(e, kAXChildrenAttribute) as? [AXUIElement] else { return nil }
    for k in kids { if let hit = find(k, depth: depth + 1) { return hit } }
    return nil
}

guard let windows = attr(AXUIElementCreateApplication(pid), kAXWindowsAttribute) as? [AXUIElement],
      let hit = windows.lazy.compactMap({ find($0, depth: 0) }).first else {
    print("missing")
    exit(1)
}
print(AXUIElementSetAttributeValue(hit, kAXValueAttribute as CFString, value as CFString) == .success ? "set" : "refused")
