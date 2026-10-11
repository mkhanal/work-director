import Foundation
import UserNotifications
import WorkDirectorKit

/// Tells the person, outside the window, when work comes to wait on them; a
/// click opens that work.
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

    func announce(_ arrival: Arrival) {
        guard let center, UserDefaults.standard.object(forKey: "notify") as? Bool ?? true else { return }
        let content = UNMutableNotificationContent()
        content.title = switch arrival.band {
        case .needsYou: arrival.work.state == .blocked ? "Blocked" : "Needs you"
        case .readyToPush: "Ready to land"
        case .readyToClose: "Ready to close"
        default: arrival.band.label
        }
        content.body = arrival.work.title
        content.subtitle = arrival.work.project
        content.sound = .default
        content.userInfo = ["work": arrival.work.id]
        center.add(UNNotificationRequest(identifier: "\(arrival.work.id)-\(arrival.band.rawValue)", content: content, trigger: nil))
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
