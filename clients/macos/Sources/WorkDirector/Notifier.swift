import Foundation
import UserNotifications
import WorkDirectorKit

/// Tells the person, outside the window, what happened and to which goal; a
/// click opens that goal.
@MainActor
final class Notifier: NSObject, UNUserNotificationCenterDelegate {
    static let shared = Notifier()

    /// A notification centre exists only for a bundled app; run bare from
    /// `swift run`, there is no one to tell.
    private var center: UNUserNotificationCenter? {
        Bundle.main.bundleIdentifier == nil ? nil : UNUserNotificationCenter.current()
    }

    func start() {
        guard let center else { return }
        center.delegate = self
        center.requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }

    func announce(_ notice: Notice) {
        guard let center, UserDefaults.standard.object(forKey: "notify") as? Bool ?? true else { return }
        let content = UNMutableNotificationContent()
        content.title = notice.title
        content.body = notice.body
        content.sound = .default
        content.userInfo = ["work": notice.work]
        center.add(UNNotificationRequest(identifier: notice.keys.first ?? notice.work, content: content, trigger: nil))
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        guard let id = response.notification.request.content.userInfo["work"] as? String else { return }
        await MainActor.run {
            NotificationCenter.default.post(name: .openWork, object: id)
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }
}
