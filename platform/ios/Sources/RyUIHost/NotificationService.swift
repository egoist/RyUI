#if canImport(UIKit) && RYUI_NOTIFICATION_SERVICE // an app that calls push.Rewrite (see the CLI's iosFeatures)
import UIKit
import UserNotifications
#if canImport(CRyUI)
@_implementationOnly import CRyUI
#endif

/// The Notification Service Extension of an app with a push rewrite
/// (push.Rewrite): its principal class, which runs the app's Go code in
/// the extension (RYUI_NOTIFICATION_SERVICE: main, with a root that shows
/// nothing). Each push with mutable-content is handed to the rewrite; what
/// it answers becomes the notification (RyUINotifyContent: the image
/// attached, a message as a communication notification). If the rewrite
/// fails, or the time iOS gives runs out, the push shows as it came.
@objc(RyUINotificationService)
public final class RyUINotificationService: UNNotificationServiceExtension {
    private var token = ""

    public override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        DispatchQueue.main.async {
            MainActor.assumeIsolated { self.token = RyUIRewrites.shared.start(request, contentHandler) }
        }
    }

    public override func serviceExtensionTimeWillExpire() {
        DispatchQueue.main.async {
            MainActor.assumeIsolated { RyUIRewrites.shared.finish(self.token, nil) }
        }
    }
}

/// The rewrites waiting for Go, and push's methods for them:
/// "nextRewrite" answers with one ({token, message: push.Message's JSON}),
/// "rewritten" ({token, ok, spec}) answers it.
@MainActor final class RyUIRewrites {
    static let shared = RyUIRewrites()
    private let queue = RyUIWaitQueue()
    private var pending: [String: (request: UNNotificationRequest, handler: (UNNotificationContent) -> Void)] = [:]

    func methods() -> [String: RyUIModuleMethod] {
        [
            "nextRewrite": { [unowned self] _, reply in self.queue.next(reply) },
            "rewritten": { [unowned self] args, reply in
                reply(.success(Data()))
                guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any],
                      let token = o["token"] as? String, let p = self.pending[token] else { return }
                guard o["ok"] as? Bool == true, let spec = o["spec"] as? [String: Any],
                      let base = p.request.content.mutableCopy() as? UNMutableNotificationContent else {
                    self.finish(token, nil)
                    return
                }
                RyUINotifyContent.make(spec, into: base) { content in self.finish(token, content) }
            },
        ]
    }

    /// Hands request to the rewrite, starting the Go code if it hasn't;
    /// returns its token.
    func start(_ request: UNNotificationRequest, _ handler: @escaping (UNNotificationContent) -> Void) -> String {
        "RYUI_NOTIFICATION_SERVICE".withCString { k in "1".withCString { v in ryui_setenv(k, v) } }
        RyUIRuntime.shared.startHeadless()
        let token = UUID().uuidString
        pending[token] = (request, handler)
        let content = request.content
        let message = RyUINotifyContent.pushMessage(content.userInfo, id: request.identifier, title: content.title, body: content.body)
        var o: [String: Any] = ["token": token]
        o["message"] = try? JSONSerialization.jsonObject(with: message)
        queue.deliver((try? JSONSerialization.data(withJSONObject: o)) ?? Data())
        return token
    }

    /// Shows the rewrite's content, or the push as it came (nil): once.
    func finish(_ token: String, _ content: UNNotificationContent?) {
        guard let p = pending.removeValue(forKey: token) else { return }
        p.handler(content ?? p.request.content)
    }
}
#endif
