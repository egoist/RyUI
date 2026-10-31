#if canImport(UIKit) && RYUI_PUSH // an app that calls ryui/push (see the CLI's iosFeatures)
import UIKit
import UserNotifications

/// ryui/push's module: "register" asks for notifications, registers with
/// APNs and answers JSON (token, service, allowed); "nextReceived" and
/// "nextOpened" answer with the next message (JSON: id, title, body,
/// data), kept until asked; "stop" (the method's name) fails the waiting
/// ones with "stopped". It's the notification center's delegate, for
/// ryui/notify's notifications too, which it treats as RyUINotify does.
@MainActor final class RyUIPush: NSObject, UNUserNotificationCenterDelegate {
    static let shared = RyUIPush()
    private var registering: [(Result<Data, Error>) -> Void] = []
    private var allowed = false
    private var queues: [String: [Data]] = ["nextReceived": [], "nextOpened": []]
    private var waiting: [String: [(Result<Data, Error>) -> Void]] = ["nextReceived": [], "nextOpened": []]

    /// Becomes the center's delegate: at launch, so a tap that launched
    /// the app is seen.
    func install() {
        UNUserNotificationCenter.current().delegate = self
    }

    func methods() -> [String: RyUIModuleMethod] {
        [
            "register": { [unowned self] _, reply in
                UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { granted, _ in
                    DispatchQueue.main.async {
                        self.allowed = granted
                        self.registering.append(reply)
                        UIApplication.shared.registerForRemoteNotifications()
                    }
                }
            },
            "nextReceived": { [unowned self] _, reply in self.next("nextReceived", reply) },
            "nextOpened": { [unowned self] _, reply in self.next("nextOpened", reply) },
            "stop": { [unowned self] args, reply in
                let m = String(decoding: args, as: UTF8.self)
                let ws = self.waiting[m] ?? []
                self.waiting[m] = []
                for w in ws { w(.failure(RyUIModuleError("stopped"))) }
                reply(.success(Data()))
            },
        ]
    }

    private func next(_ kind: String, _ reply: @escaping (Result<Data, Error>) -> Void) {
        if var q = queues[kind], !q.isEmpty {
            let m = q.removeFirst()
            queues[kind] = q
            reply(.success(m))
        } else {
            waiting[kind, default: []].append(reply)
        }
    }

    private func deliver(_ kind: String, _ m: Data) {
        if var ws = waiting[kind], !ws.isEmpty {
            let w = ws.removeFirst()
            waiting[kind] = ws
            w(.success(m))
        } else {
            var q = queues[kind] ?? []
            q.append(m)
            if q.count > 64 { q.removeFirst() }
            queues[kind] = q
        }
    }

    // From the app delegate.

    func registered(_ token: Data) {
        let hex = token.map { String(format: "%02x", $0) }.joined()
        let d = (try? JSONSerialization.data(withJSONObject: ["token": hex, "service": "apns", "allowed": allowed] as [String: Any])) ?? Data()
        let rs = registering
        registering = []
        for r in rs { r(.success(d)) }
    }

    func failed(_ error: Error) {
        let rs = registering
        registering = []
        for r in rs { r(.failure(error)) }
    }

    /// A data message (content-available), in the foreground or woken for it.
    func received(_ userInfo: [AnyHashable: Any]) {
        deliver("nextReceived", Self.message(userInfo, id: ""))
    }

    nonisolated static func message(_ userInfo: [AnyHashable: Any], id: String, title: String = "", body: String = "") -> Data {
        var data: [String: String] = [:]
        for (k, v) in userInfo {
            guard let k = k as? String, k != "aps" else { continue }
            data[k] = v as? String ?? (v as? NSNumber)?.stringValue ?? ((try? JSONSerialization.data(withJSONObject: v)).map { String(decoding: $0, as: UTF8.self) } ?? "")
        }
        var t = title, b = body
        if t.isEmpty && b.isEmpty, let alert = (userInfo["aps"] as? [String: Any])?["alert"] {
            if let s = alert as? String { b = s } else if let a = alert as? [String: Any] {
                t = a["title"] as? String ?? ""
                b = a["body"] as? String ?? ""
            }
        }
        let o: [String: Any] = ["id": id, "title": t, "body": b, "data": data]
        return (try? JSONSerialization.data(withJSONObject: o)) ?? Data("{}".utf8)
    }

    nonisolated func userNotificationCenter(_ c: UNUserNotificationCenter, willPresent n: UNNotification,
                                            withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        if n.request.trigger is UNPushNotificationTrigger {
            let content = n.request.content
            let m = Self.message(content.userInfo, id: n.request.identifier, title: content.title, body: content.body)
            DispatchQueue.main.async { RyUIPush.shared.deliver("nextReceived", m) }
        }
        done([.banner, .list, .sound])
    }

    nonisolated func userNotificationCenter(_ c: UNUserNotificationCenter, didReceive r: UNNotificationResponse,
                                            withCompletionHandler done: @escaping () -> Void) {
        let n = r.notification
        let content = n.request.content
        let remote = n.request.trigger is UNPushNotificationTrigger
        let url = content.userInfo["url"] as? String
        let m = remote ? Self.message(content.userInfo, id: n.request.identifier, title: content.title, body: content.body) : nil
        DispatchQueue.main.async {
            if let m { RyUIPush.shared.deliver("nextOpened", m) }
            if let url, let u = URL(string: url) { RyUIRuntime.shared.openURL(u) }
            done()
        }
    }
}
#endif
