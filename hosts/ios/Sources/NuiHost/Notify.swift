#if canImport(UIKit) && NUI_NOTIFY // an app that imports nui/notify (see the CLI's iosFeatures)
import UIKit
import UserNotifications

/// nui/notify's module: "request" (u8 allowed), "schedule" (JSON: id,
/// title, body, url, seconds), "cancel" (the id). Shown in the
/// foreground too; a tap opens the app with its URL (ui.OnOpenURL).
@MainActor final class NuiNotify: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NuiNotify()

    /// Becomes the center's delegate: at launch, so a tap that launched
    /// the app is seen.
    func install() {
        UNUserNotificationCenter.current().delegate = self
    }

    func methods() -> [String: NuiModuleMethod] {
        let center = UNUserNotificationCenter.current()
        return [
            "request": { _, reply in
                center.requestAuthorization(options: [.alert, .sound, .badge]) { granted, error in
                    if let error { reply(.failure(error)) } else { reply(.success(Data([granted ? 1 : 0]))) }
                }
            },
            "schedule": { args, reply in
                guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] else {
                    reply(.failure(NuiModuleError("bad notification"))); return
                }
                let c = UNMutableNotificationContent()
                c.title = o["title"] as? String ?? ""
                c.body = o["body"] as? String ?? ""
                c.sound = .default
                if let url = o["url"] as? String, !url.isEmpty { c.userInfo = ["url": url] }
                let id = (o["id"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? UUID().uuidString
                let after = max(1, o["seconds"] as? Double ?? 1)
                let req = UNNotificationRequest(identifier: id, content: c,
                                                trigger: UNTimeIntervalNotificationTrigger(timeInterval: after, repeats: false))
                center.add(req) { error in reply(error.map { .failure($0) } ?? .success(Data())) }
            },
            "cancel": { args, reply in
                let id = String(decoding: args, as: UTF8.self)
                center.removePendingNotificationRequests(withIdentifiers: [id])
                center.removeDeliveredNotifications(withIdentifiers: [id])
                reply(.success(Data()))
            },
        ]
    }

    nonisolated func userNotificationCenter(_ c: UNUserNotificationCenter, willPresent n: UNNotification,
                                            withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .list, .sound])
    }

    nonisolated func userNotificationCenter(_ c: UNUserNotificationCenter, didReceive r: UNNotificationResponse,
                                            withCompletionHandler done: @escaping () -> Void) {
        let url = r.notification.request.content.userInfo["url"] as? String
        DispatchQueue.main.async {
            if let url, let u = URL(string: url) { NuiRuntime.shared.openURL(u) }
            done()
        }
    }
}
#endif
