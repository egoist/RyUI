#if canImport(UIKit) && (RYUI_NOTIFY || RYUI_PUSH || RYUI_NOTIFICATION_SERVICE) // ryui/notify, ryui/push (see the CLI's iosFeatures)
import UIKit
import UserNotifications
import UniformTypeIdentifiers
#if RYUI_CONVERSATIONS
import Intents
#endif

/// A host-side queue of what Go waits for ("next..." methods): items are
/// kept until asked, waiting calls until an item comes; stop fails the
/// waiting ones with "stopped".
@MainActor final class RyUIWaitQueue {
    private var items: [Data] = []
    private var waiting: [(Result<Data, Error>) -> Void] = []

    func next(_ reply: @escaping (Result<Data, Error>) -> Void) {
        if items.isEmpty { waiting.append(reply) } else { reply(.success(items.removeFirst())) }
    }

    func deliver(_ item: Data) {
        if waiting.isEmpty { items.append(item) } else { waiting.removeFirst()(.success(item)) }
    }

    func stop() {
        let ws = waiting
        waiting = []
        for w in ws { w(.failure(RyUIModuleError("stopped"))) }
    }
}

/// ryui/notify's module, and the notification center's delegate (ryui/
/// push's notifications too):
/// - "request" (u8 allowed); "schedule" (a notifyspec.Spec: seconds 0
///   shows it now); "cancel" (the id).
/// - "categories" (JSON), kept in UserDefaults and set again at launch.
/// - "badge" (u32); "showing" (JSON list); "remove" (JSON ids),
///   "removeThread" (the thread), "removeAll".
/// - "present" (u8 on), "nextPresent" (a notifyspec.Present), "presented"
///   (u8 presentation, the token): with the presenter on, a notification
///   that comes in the foreground waits for Go's answer, a second at most.
/// - "nextAction" (a notifyspec.Response), "actionDone" (the token): a
///   background action keeps the app running until Go is done with it
///   (25 seconds at most); a tap or an action that opens the app is done
///   at once.
/// - "stop" (the method's name); "channels", "channelGroups": Android's.
@MainActor final class RyUINotifyCenter: NSObject, UNUserNotificationCenterDelegate {
    static let shared = RyUINotifyCenter()
    private let categoriesKey = "ryui.notify.categories"
    private var installed = false
    private var presenting = false
    private let presents = RyUIWaitQueue(), actions = RyUIWaitQueue()
    private var pendingPresent: [String: (UNNotificationPresentationOptions) -> Void] = [:]
    private var pendingActions: [String: () -> Void] = [:]
    /// The seconds a background action has: a margin under what iOS gives.
    static let actionSeconds = 25

    /// Becomes the center's delegate, and sets the app's categories: at
    /// launch, so a tap or an action that launched the app is seen.
    func install() {
        guard !installed else { return }
        installed = true
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        if let d = UserDefaults.standard.data(forKey: categoriesKey) { center.setNotificationCategories(Self.categories(d)) }
    }

    func methods() -> [String: RyUIModuleMethod] {
        let center = UNUserNotificationCenter.current()
        return [
            "request": { _, reply in
                center.requestAuthorization(options: [.alert, .sound, .badge]) { granted, error in
                    if let error { reply(.failure(error)) } else { reply(.success(Data([granted ? 1 : 0]))) }
                }
            },
            "schedule": { args, reply in
                guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] else {
                    reply(.failure(RyUIModuleError("bad notification"))); return
                }
                let id = (o["id"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? UUID().uuidString
                let after = o["seconds"] as? Double ?? 0
                let trigger = after > 0 ? UNTimeIntervalNotificationTrigger(timeInterval: max(1, after), repeats: false) : nil
                RyUINotifyContent.make(o, into: nil) { content in
                    center.add(UNNotificationRequest(identifier: id, content: content, trigger: trigger)) { error in
                        guard error != nil, o["sender"] != nil else { reply(error.map { .failure($0) } ?? .success(Data())); return }
                        // Not taken as a communication notification (the
                        // signature lacks the entitlement): as a plain one.
                        DispatchQueue.main.async {
                            RyUINotifyContent.make(o, into: nil, message: false) { plain in
                                center.add(UNNotificationRequest(identifier: id, content: plain, trigger: trigger)) { error in
                                    reply(error.map { .failure($0) } ?? .success(Data()))
                                }
                            }
                        }
                    }
                }
            },
            "cancel": { args, reply in
                let id = String(decoding: args, as: UTF8.self)
                center.removePendingNotificationRequests(withIdentifiers: [id])
                center.removeDeliveredNotifications(withIdentifiers: [id])
                reply(.success(Data()))
            },
            "categories": { [unowned self] args, reply in
                UserDefaults.standard.set(args, forKey: self.categoriesKey)
                center.setNotificationCategories(Self.categories(args))
                reply(.success(Data()))
            },
            "badge": { args, reply in
                let n = args.count >= 4 ? Int(args.withUnsafeBytes { $0.loadUnaligned(as: UInt32.self).littleEndian }) : 0
                center.setBadgeCount(n) { error in reply(error.map { .failure($0) } ?? .success(Data())) }
            },
            "showing": { _, reply in
                center.getDeliveredNotifications { ns in
                    let list = ns.sorted { $0.date > $1.date }.map(RyUINotifyContent.shown)
                    reply(.success((try? JSONSerialization.data(withJSONObject: list)) ?? Data("[]".utf8)))
                }
            },
            "remove": { args, reply in
                center.removeDeliveredNotifications(withIdentifiers: (try? JSONSerialization.jsonObject(with: args)) as? [String] ?? [])
                reply(.success(Data()))
            },
            "removeThread": { args, reply in
                let thread = String(decoding: args, as: UTF8.self)
                center.getDeliveredNotifications { ns in
                    center.removeDeliveredNotifications(withIdentifiers: ns.filter { $0.request.content.threadIdentifier == thread }.map { $0.request.identifier })
                    reply(.success(Data()))
                }
            },
            "removeAll": { _, reply in
                center.removeAllDeliveredNotifications()
                reply(.success(Data()))
            },
            "present": { [unowned self] args, reply in
                self.presenting = args.first == 1
                if !self.presenting { for (_, done) in self.pendingPresent { done(Self.options(0)) }; self.pendingPresent = [:] }
                reply(.success(Data()))
            },
            "nextPresent": { [unowned self] _, reply in self.presents.next(reply) },
            "presented": { [unowned self] args, reply in
                if let how = args.first, let done = self.pendingPresent.removeValue(forKey: String(decoding: args.dropFirst(), as: UTF8.self)) {
                    done(Self.options(how))
                }
                reply(.success(Data()))
            },
            "nextAction": { [unowned self] _, reply in self.actions.next(reply) },
            "actionDone": { [unowned self] args, reply in
                self.pendingActions.removeValue(forKey: String(decoding: args, as: UTF8.self))?()
                reply(.success(Data()))
            },
            "stop": { [unowned self] args, reply in
                switch String(decoding: args, as: UTF8.self) {
                case "nextPresent": self.presents.stop()
                case "nextAction": self.actions.stop()
                default: break
                }
                reply(.success(Data()))
            },
            // Android's: iOS has no channels.
            "channels": { _, reply in reply(.success(Data())) },
            "channelGroups": { _, reply in reply(.success(Data())) },
        ]
    }

    /// A presentation (notify.Presentation) as the center's options.
    static func options(_ how: UInt8) -> UNNotificationPresentationOptions {
        switch how {
        case 1: return [.list, .badge]
        case 2: return []
        default: return [.banner, .list, .sound, .badge]
        }
    }

    /// The categories' JSON (notify.SetCategories) as the center's.
    nonisolated static func categories(_ d: Data) -> Set<UNNotificationCategory> {
        guard let cs = (try? JSONSerialization.jsonObject(with: d)) as? [[String: Any]] else { return [] }
        var out = Set<UNNotificationCategory>()
        for c in cs {
            guard let id = c["id"] as? String else { continue }
            var actions: [UNNotificationAction] = []
            for a in c["actions"] as? [[String: Any]] ?? [] {
                guard let aid = a["id"] as? String else { continue }
                let title = a["title"] as? String ?? ""
                var opts: UNNotificationActionOptions = []
                if a["destructive"] as? Bool == true { opts.insert(.destructive) }
                if a["unlock"] as? Bool == true { opts.insert(.authenticationRequired) }
                if a["open"] as? Bool == true { opts.insert(.foreground) }
                let icon = (a["icon"] as? String).flatMap { $0.isEmpty ? nil : UNNotificationActionIcon(systemImageName: $0) }
                if let r = a["reply"] as? [String: Any] {
                    actions.append(UNTextInputNotificationAction(identifier: aid, title: title, options: opts, icon: icon,
                                                                 textInputButtonTitle: r["button"] as? String ?? "",
                                                                 textInputPlaceholder: r["placeholder"] as? String ?? ""))
                } else {
                    actions.append(UNNotificationAction(identifier: aid, title: title, options: opts, icon: icon))
                }
            }
            out.insert(UNNotificationCategory(identifier: id, actions: actions, intentIdentifiers: [], options: []))
        }
        return out
    }

    /// Whether the action actionID of category opens the app.
    private func opens(_ category: String, _ actionID: String) -> Bool {
        if actionID == UNNotificationDefaultActionIdentifier { return true }
        guard let d = UserDefaults.standard.data(forKey: categoriesKey),
              let cs = (try? JSONSerialization.jsonObject(with: d)) as? [[String: Any]],
              let c = cs.first(where: { $0["id"] as? String == category }),
              let a = (c["actions"] as? [[String: Any]])?.first(where: { $0["id"] as? String == actionID }) else { return false }
        return a["open"] as? Bool == true
    }

    nonisolated func userNotificationCenter(_ c: UNUserNotificationCenter, willPresent n: UNNotification,
                                            withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        DispatchQueue.main.async {
            MainActor.assumeIsolated { RyUINotifyCenter.shared.willPresent(n, done) }
        }
    }

    private func willPresent(_ n: UNNotification, _ done: @escaping (UNNotificationPresentationOptions) -> Void) {
        #if RYUI_PUSH
        if n.request.trigger is UNPushNotificationTrigger {
            let content = n.request.content
            RyUIPush.shared.deliver("nextReceived", RyUINotifyContent.pushMessage(content.userInfo, id: n.request.identifier, title: content.title, body: content.body))
        }
        #endif
        guard presenting else { done(Self.options(0)); return }
        let token = UUID().uuidString
        pendingPresent[token] = done
        let o: [String: Any] = ["token": token, "notification": RyUINotifyContent.shown(n)]
        presents.deliver((try? JSONSerialization.data(withJSONObject: o)) ?? Data())
        // A second at most: then it shows.
        DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
            MainActor.assumeIsolated {
                RyUINotifyCenter.shared.pendingPresent.removeValue(forKey: token)?(Self.options(0))
            }
        }
    }

    nonisolated func userNotificationCenter(_ c: UNUserNotificationCenter, didReceive r: UNNotificationResponse,
                                            withCompletionHandler done: @escaping () -> Void) {
        DispatchQueue.main.async {
            MainActor.assumeIsolated { RyUINotifyCenter.shared.didReceive(r, done) }
        }
    }

    private func didReceive(_ r: UNNotificationResponse, _ done: @escaping () -> Void) {
        let n = r.notification
        let content = n.request.content
        // Launched for it (a background action): the Go code starts without a screen.
        RyUIRuntime.shared.startHeadless()
        let action = r.actionIdentifier == UNNotificationDefaultActionIdentifier ? "" : r.actionIdentifier
        let token = UUID().uuidString
        let o: [String: Any] = ["token": token, "action": action, "reply": (r as? UNTextInputNotificationResponse)?.userText ?? "",
                                "seconds": Self.actionSeconds, "notification": RyUINotifyContent.shown(n)]
        actions.deliver((try? JSONSerialization.data(withJSONObject: o)) ?? Data())
        #if RYUI_PUSH
        if n.request.trigger is UNPushNotificationTrigger, r.actionIdentifier != UNNotificationDismissActionIdentifier {
            RyUIPush.shared.deliver("nextOpened", RyUINotifyContent.pushMessage(content.userInfo, id: n.request.identifier, title: content.title, body: content.body))
        }
        #endif
        if r.actionIdentifier == UNNotificationDefaultActionIdentifier, let url = content.userInfo["url"] as? String, let u = URL(string: url) {
            RyUIRuntime.shared.openURL(u)
        }
        // The app is in front for a tap or an action that opens it: done.
        // A background action keeps it running until Go is done.
        if opens(content.categoryIdentifier, r.actionIdentifier) { done(); return }
        pendingActions[token] = done
        DispatchQueue.main.asyncAfter(deadline: .now() + .seconds(Self.actionSeconds)) {
            MainActor.assumeIsolated { RyUINotifyCenter.shared.pendingActions.removeValue(forKey: token)?() }
        }
    }
}

/// Notifications' content from Go's spec (internal/notifyspec's Spec):
/// ryui/notify's, and a push's rewrite (push.Rewrite, in the Notification
/// Service Extension); and what's reported of one that shows.
enum RyUINotifyContent {
    /// Makes the content o says (into base: a push's, which keeps its aps),
    /// with its image attached and, for a message (unless message is
    /// false), as a communication notification. done gets it on the main
    /// thread.
    @MainActor static func make(_ o: [String: Any], into base: UNMutableNotificationContent?, message asMessage: Bool = true,
                                done: @escaping (UNNotificationContent) -> Void) {
        let c = base ?? UNMutableNotificationContent()
        c.title = o["title"] as? String ?? ""
        c.subtitle = o["subtitle"] as? String ?? ""
        c.body = o["body"] as? String ?? ""
        if o["silent"] as? Bool == true { c.sound = nil } else if base == nil { c.sound = .default }
        let data = o["data"] as? [String: String] ?? [:]
        let url = o["url"] as? String ?? ""
        if let base {
            // A push's: its data are the keys beside aps.
            var info: [AnyHashable: Any] = [:]
            if let aps = base.userInfo["aps"] { info["aps"] = aps }
            for (k, v) in data { info[k] = v }
            if !url.isEmpty { info["url"] = url }
            c.userInfo = info
        } else {
            var info: [AnyHashable: Any] = ["ryui.data": data]
            if !url.isEmpty { info["url"] = url }
            c.userInfo = info
        }
        c.categoryIdentifier = o["category"] as? String ?? ""
        c.threadIdentifier = o["thread"] as? String ?? ""
        if let b = o["badge"] as? Int, b > 0 { c.badge = NSNumber(value: b) }
        let sender = asMessage ? o["sender"] as? [String: Any] : nil
        let conversation = asMessage ? o["conversation"] as? [String: Any] : nil
        // The images, then the content: the attachment, the avatars.
        let group = DispatchGroup()
        var image: (URL, String)?
        if let src = o["image"] as? String, !src.isEmpty {
            group.enter()
            fetch(src) { f in image = f; group.leave() }
        }
        var senderImage: Data?, groupImage: Data?
        #if RYUI_CONVERSATIONS
        if let src = sender?["image"] as? String, !src.isEmpty {
            group.enter()
            fetch(src) { f in senderImage = f.flatMap { try? Data(contentsOf: $0.0) }; group.leave() }
        }
        if let src = conversation?["image"] as? String, !src.isEmpty {
            group.enter()
            fetch(src) { f in groupImage = f.flatMap { try? Data(contentsOf: $0.0) }; group.leave() }
        }
        #endif
        group.notify(queue: .main) {
            MainActor.assumeIsolated {
                if let (file, type) = image,
                   let a = try? UNNotificationAttachment(identifier: "image", url: file, options: [UNNotificationAttachmentOptionsTypeHintKey: type]) {
                    c.attachments = [a]
                }
                guard let sender, let conversation else { done(c); return }
                message(c, sender: sender, conversation: conversation, senderImage: senderImage, groupImage: groupImage, done: done)
            }
        }
    }

    /// Makes c a communication notification: the message's intent, donated
    /// (RYUI_CONVERSATIONS: the app calls From, and has the entitlement).
    @MainActor private static func message(_ c: UNMutableNotificationContent, sender: [String: Any], conversation: [String: Any],
                                           senderImage: Data?, groupImage: Data?, done: @escaping (UNNotificationContent) -> Void) {
        #if RYUI_CONVERSATIONS
        func person(_ p: [String: Any]?, me: Bool, image: Data?) -> INPerson {
            let id = p?["id"] as? String ?? ""
            let name = p?["name"] as? String ?? ""
            return INPerson(personHandle: INPersonHandle(value: id.isEmpty ? name : id, type: .unknown), nameComponents: nil,
                            displayName: name, image: image.map { INImage(imageData: $0) }, contactIdentifier: nil,
                            customIdentifier: id.isEmpty ? nil : id, isMe: me, suggestionType: .none)
        }
        let from = person(sender, me: false, image: senderImage)
        let group = conversation["group"] as? Bool == true
        let title = conversation["title"] as? String ?? ""
        let intent = INSendMessageIntent(recipients: group ? [person(conversation["me"] as? [String: Any], me: true, image: nil), from] : nil,
                                         outgoingMessageType: .outgoingMessageText, content: c.body,
                                         speakableGroupName: group && !title.isEmpty ? INSpeakableString(spokenPhrase: title) : nil,
                                         conversationIdentifier: conversation["id"] as? String, serviceName: nil, sender: from, attachments: nil)
        if group, let g = groupImage { intent.setImage(INImage(imageData: g), forParameterNamed: \.speakableGroupName) }
        let interaction = INInteraction(intent: intent, response: nil)
        interaction.direction = .incoming
        interaction.donate { _ in
            DispatchQueue.main.async {
                // Without the entitlement, it shows as it is.
                done((try? c.updating(from: intent)) ?? c)
            }
        }
        #else
        done(c)
        #endif
    }

    /// The local file an image source names (as ui.Image's: an http(s) URL,
    /// downloaded; a file's path; an asset's), copied to a temporary file
    /// the system can move into its store, with its type's identifier.
    static func fetch(_ src: String, done: @escaping ((URL, String)?) -> Void) {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("ryui-notify", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        func finish(_ from: URL, _ ext: String) {
            let type = UTType(filenameExtension: ext.lowercased())?.identifier ?? UTType.jpeg.identifier
            let to = dir.appendingPathComponent(UUID().uuidString).appendingPathExtension(ext.isEmpty ? "jpg" : ext)
            do {
                try FileManager.default.copyItem(at: from, to: to)
                DispatchQueue.main.async { done((to, type)) }
            } catch {
                DispatchQueue.main.async { done(nil) }
            }
        }
        if src.hasPrefix("http://") || src.hasPrefix("https://"), let u = URL(string: src) {
            var req = URLRequest(url: u)
            req.timeoutInterval = 15
            URLSession.shared.downloadTask(with: req) { file, resp, _ in
                guard let file, (resp as? HTTPURLResponse).map({ (200..<300).contains($0.statusCode) }) ?? true else {
                    DispatchQueue.main.async { done(nil) }; return
                }
                var ext = u.pathExtension
                if let mime = resp?.mimeType, let t = UTType(mimeType: mime), let e = t.preferredFilenameExtension { ext = e }
                finish(file, ext)
            }.resume()
            return
        }
        let file: URL
        if src.hasPrefix("file://"), let u = URL(string: src) {
            file = u
        } else if src.hasPrefix("/") {
            file = URL(fileURLWithPath: src)
        } else {
            file = appBundle.appendingPathComponent("assets").appendingPathComponent(src)
        }
        DispatchQueue.global(qos: .userInitiated).async { finish(file, file.pathExtension) }
    }

    /// The app's bundle, from the app or one of its extensions (PlugIns/x.appex).
    static var appBundle: URL {
        let b = Bundle.main.bundleURL
        return b.pathExtension == "appex" ? b.deletingLastPathComponent().deletingLastPathComponent() : b
    }

    /// What's reported of a notification (a notifyspec.Shown).
    static func shown(_ n: UNNotification) -> [String: Any] {
        let c = n.request.content
        let remote = n.request.trigger is UNPushNotificationTrigger
        var data: [String: String] = [:]
        if remote {
            data = pushData(c.userInfo)
        } else if let d = c.userInfo["ryui.data"] as? [String: String] {
            data = d
        }
        return ["id": n.request.identifier, "title": c.title, "subtitle": c.subtitle, "body": c.body,
                "url": c.userInfo["url"] as? String ?? "", "data": data, "category": c.categoryIdentifier,
                "thread": c.threadIdentifier, "time": Int64(n.date.timeIntervalSince1970 * 1000), "remote": remote]
    }

    /// A push's data: its payload's keys beside aps, as strings.
    static func pushData(_ userInfo: [AnyHashable: Any]) -> [String: String] {
        var data: [String: String] = [:]
        for (k, v) in userInfo {
            guard let k = k as? String, k != "aps" else { continue }
            data[k] = v as? String ?? (v as? NSNumber)?.stringValue ?? ((try? JSONSerialization.data(withJSONObject: v)).map { String(decoding: $0, as: UTF8.self) } ?? "")
        }
        return data
    }

    /// A push as push.Message's JSON: its id, title, body, data, and what
    /// its aps says (subtitle, category, thread-id, badge).
    static func pushMessage(_ userInfo: [AnyHashable: Any], id: String, title: String = "", body: String = "") -> Data {
        let aps = userInfo["aps"] as? [String: Any] ?? [:]
        var t = title, b = body, sub = ""
        if let alert = aps["alert"] {
            if let s = alert as? String {
                if b.isEmpty { b = s }
            } else if let a = alert as? [String: Any] {
                if t.isEmpty && b.isEmpty {
                    t = a["title"] as? String ?? ""
                    b = a["body"] as? String ?? ""
                }
                sub = a["subtitle"] as? String ?? ""
            }
        }
        let o: [String: Any] = ["id": id, "title": t, "body": b, "subtitle": sub, "data": pushData(userInfo),
                                "category": aps["category"] as? String ?? "", "thread": aps["thread-id"] as? String ?? "",
                                "badge": aps["badge"] as? Int ?? 0]
        return (try? JSONSerialization.data(withJSONObject: o)) ?? Data("{}".utf8)
    }
}
#endif
