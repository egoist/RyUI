#if canImport(UIKit) && canImport(ActivityKit) && ASUKA_LIVEACTIVITY // asuka/sdk/liveactivity (see the CLI's iosFeatures)
import UIKit
import ActivityKit

/// asuka/sdk/liveactivity's module: ActivityKit's Live Activities, drawn by the
/// widget extension's template (Widget/LiveActivity.swift), which the
/// build adds to an app that calls the package.
/// - "enabled" (u8): iOS 16.2, and the user allows them.
/// - "start" (JSON: id, content, push, relevance): requests one, with a
///   push token if push.
/// - "update" (JSON: id, content, alert?), "end" (JSON: id, content,
///   dismiss "now" or "at" with atMs, or the default).
/// - "running" (JSON: [{id, content}]): the active ones.
/// - "next" (JSON: {type, id, token}): what happened, kept until asked:
///   "started" (by a push), "ended", "pushToken", "pushToStartToken";
///   "stop" fails the waiting one with "stopped".
@MainActor final class AsukaLiveActivities {
    static let shared = AsukaLiveActivities()
    private var events: [Data] = []
    private var waiting: [(Result<Data, Error>) -> Void] = []
    /// The activities followed (by ActivityKit's ID), and those the app
    /// started (by theirs): another that comes is a server's.
    private var followed = Set<String>()
    private var startedHere = Set<String>()
    private var installed = false

    @available(iOS 16.2, *)
    private struct Start: Decodable {
        var id: String
        var content: AsukaActivityAttributes.ContentState
        var push: Bool?
        var relevance: Double?
    }

    @available(iOS 16.2, *)
    private struct Change: Decodable {
        struct Alert: Decodable { var title: String?; var body: String? }
        var id: String
        var content: AsukaActivityAttributes.ContentState
        var alert: Alert?
        var dismiss: String?
        var atMs: Double?
    }

    func methods() -> [String: AsukaModuleMethod] {
        install()
        return [
            "enabled": { _, reply in
                guard #available(iOS 16.2, *) else { reply(.success(Data([0]))); return }
                reply(.success(Data([ActivityAuthorizationInfo().areActivitiesEnabled ? 1 : 0])))
            },
            "start": { [weak self] args, reply in
                guard #available(iOS 16.2, *) else {
                    reply(.failure(AsukaModuleError("not available: Live Activities need iOS 16.2"))); return
                }
                guard let s = try? JSONDecoder().decode(Start.self, from: args) else { reply(.failure(AsukaModuleError("bad args"))); return }
                guard ActivityAuthorizationInfo().areActivitiesEnabled else {
                    reply(.failure(AsukaModuleError("not available: Live Activities are off for the app (Settings)"))); return
                }
                do {
                    let content = ActivityContent(state: s.content, staleDate: nil, relevanceScore: s.relevance ?? 0)
                    let a = try Activity.request(attributes: AsukaActivityAttributes(id: s.id), content: content,
                                                 pushType: s.push == true ? .token : nil)
                    self?.startedHere.insert(s.id)
                    self?.follow(a)
                    reply(.success(Data()))
                } catch {
                    reply(.failure(error))
                }
            },
            "update": { [weak self] args, reply in
                guard #available(iOS 16.2, *) else { reply(.success(Data())); return }
                guard let c = try? JSONDecoder().decode(Change.self, from: args) else { reply(.failure(AsukaModuleError("bad args"))); return }
                guard let a = self?.activity(c.id) else { reply(.success(Data())); return }
                let content = ActivityContent(state: c.content, staleDate: nil, relevanceScore: a.content.relevanceScore)
                var alert: AlertConfiguration?
                if let al = c.alert {
                    alert = AlertConfiguration(title: LocalizedStringResource(stringLiteral: al.title ?? ""),
                                               body: LocalizedStringResource(stringLiteral: al.body ?? ""), sound: .default)
                }
                Task {
                    await a.update(content, alertConfiguration: alert)
                    reply(.success(Data()))
                }
            },
            "end": { [weak self] args, reply in
                guard #available(iOS 16.2, *) else { reply(.success(Data())); return }
                guard let c = try? JSONDecoder().decode(Change.self, from: args) else { reply(.failure(AsukaModuleError("bad args"))); return }
                guard let a = self?.activity(c.id) else { reply(.success(Data())); return }
                var policy = ActivityUIDismissalPolicy.default
                if c.dismiss == "now" {
                    policy = .immediate
                } else if c.dismiss == "at", let ms = c.atMs {
                    policy = .after(Date(timeIntervalSince1970: ms / 1000))
                }
                Task {
                    await a.end(ActivityContent(state: c.content, staleDate: nil), dismissalPolicy: policy)
                    reply(.success(Data()))
                }
            },
            "running": { _, reply in
                guard #available(iOS 16.2, *) else { reply(.success(Data("[]".utf8))); return }
                var out: [[String: Any]] = []
                for a in Activity<AsukaActivityAttributes>.activities where a.activityState == .active || a.activityState == .stale {
                    let state = (try? JSONEncoder().encode(a.content.state)).flatMap { try? JSONSerialization.jsonObject(with: $0) } ?? [:]
                    out.append(["id": a.attributes.id, "content": state])
                }
                reply(.success((try? JSONSerialization.data(withJSONObject: out)) ?? Data("[]".utf8)))
            },
            "next": { [weak self] _, reply in
                guard let self else { return }
                if self.events.isEmpty { self.waiting.append(reply) } else { reply(.success(self.events.removeFirst())) }
            },
            "stop": { [weak self] _, reply in
                let ws = self?.waiting ?? []
                self?.waiting = []
                for w in ws { w(.failure(AsukaModuleError("stopped"))) }
                reply(.success(Data()))
            },
        ]
    }

    /// The activity the app's ID names, if it runs.
    @available(iOS 16.2, *)
    private func activity(_ id: String) -> Activity<AsukaActivityAttributes>? {
        Activity<AsukaActivityAttributes>.activities.first { $0.attributes.id == id }
    }

    /// Something happened: for the next "next" (kept until asked, the last
    /// 100).
    private func deliver(_ type: String, id: String? = nil, token: String? = nil) {
        var o: [String: String] = ["type": type]
        o["id"] = id
        o["token"] = token
        guard let d = try? JSONSerialization.data(withJSONObject: o) else { return }
        if waiting.isEmpty {
            events.append(d)
            if events.count > 100 { events.removeFirst() }
        } else {
            waiting.removeFirst()(.success(d))
        }
    }

    /// Follows the activities there are (an app relaunched), those a push
    /// starts, and the app's push-to-start token.
    private func install() {
        guard !installed else { return }
        installed = true
        guard #available(iOS 16.2, *) else { return }
        for a in Activity<AsukaActivityAttributes>.activities { follow(a) }
        Task { @MainActor [weak self] in
            for await a in Activity<AsukaActivityAttributes>.activityUpdates {
                guard let self else { return }
                if !self.startedHere.contains(a.attributes.id) && !self.followed.contains(a.id) {
                    self.deliver("started", id: a.attributes.id)
                }
                self.follow(a)
            }
        }
        if #available(iOS 17.2, *) {
            Task { @MainActor [weak self] in
                for await t in Activity<AsukaActivityAttributes>.pushToStartTokenUpdates {
                    self?.deliver("pushToStartToken", token: t.map { String(format: "%02x", $0) }.joined())
                }
            }
        }
    }

    /// Follows an activity's state (its end) and push tokens, once.
    @available(iOS 16.2, *)
    private func follow(_ a: Activity<AsukaActivityAttributes>) {
        guard !followed.contains(a.id) else { return }
        followed.insert(a.id)
        let id = a.attributes.id
        if a.activityState == .ended || a.activityState == .dismissed {
            deliver("ended", id: id)
            return
        }
        Task { @MainActor [weak self] in
            for await s in a.activityStateUpdates where s == .ended || s == .dismissed {
                self?.deliver("ended", id: id)
                return
            }
        }
        Task { @MainActor [weak self] in
            for await t in a.pushTokenUpdates {
                self?.deliver("pushToken", id: id, token: t.map { String(format: "%02x", $0) }.joined())
            }
        }
    }
}
#endif
