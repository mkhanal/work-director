// Presses a menu item by its path from the menu bar, through the
// accessibility API: the way a UI check gives the app a menu command or its
// shortcut without posting a key event.
//   swift menu.swift <pid> <menu> [submenu…] <item>
import ApplicationServices
import Foundation

let args = CommandLine.arguments
guard args.count >= 4, let pid = pid_t(args[1]) else {
    FileHandle.standardError.write("usage: menu.swift <pid> <menu> [submenu…] <item>\n".data(using: .utf8)!)
    exit(2)
}

func attr(_ e: AXUIElement, _ name: String) -> CFTypeRef? {
    var v: CFTypeRef?
    return AXUIElementCopyAttributeValue(e, name as CFString, &v) == .success ? v : nil
}

/// The child titled `title`, looking through the menu a bar item or item holds.
func child(_ e: AXUIElement, _ title: String) -> AXUIElement? {
    for k in attr(e, kAXChildrenAttribute) as? [AXUIElement] ?? [] {
        if attr(k, kAXTitleAttribute) as? String == title { return k }
        if attr(k, kAXRoleAttribute) as? String == kAXMenuRole, let hit = child(k, title) { return hit }
    }
    return nil
}

guard let bar = attr(AXUIElementCreateApplication(pid), kAXMenuBarAttribute) else {
    print("no menu bar")
    exit(1)
}
var at = bar as! AXUIElement
for title in args.dropFirst(2) {
    guard let next = child(at, title) else {
        print("missing \(title)")
        exit(1)
    }
    at = next
}
print(AXUIElementPerformAction(at, kAXPressAction as CFString) == .success ? "pressed" : "refused")
