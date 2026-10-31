#if canImport(UIKit)
import UIKit

/// ryui/background's module: "start" and "end" (args: JSON id...) hold a
/// background task while a task lives, which gives the app about 30
/// seconds more once it's out of sight; iOS shows no notification, so
/// "update" does nothing, and "next" (actions tapped) waits until the
/// last task ends, then fails "ended".
@MainActor final class RyUIBackground {
    static let shared = RyUIBackground()
    private var tasks: [Int: UIBackgroundTaskIdentifier] = [:]
    private var next: [(Result<Data, Error>) -> Void] = []

    private static func id(_ args: Data) -> Int {
        ((try? JSONSerialization.jsonObject(with: args)) as? [String: Any])?["id"] as? Int ?? 0
    }

    private func end(_ id: Int) {
        guard let t = tasks.removeValue(forKey: id) else { return }
        UIApplication.shared.endBackgroundTask(t)
        if tasks.isEmpty {
            let ws = next
            next = []
            for w in ws { w(.failure(RyUIModuleError("ended"))) }
        }
    }

    func methods() -> [String: RyUIModuleMethod] {
        [
            "start": { [unowned self] args, reply in
                let id = Self.id(args)
                // Out of time: the system ends it (the work may stop).
                tasks[id] = UIApplication.shared.beginBackgroundTask(withName: "ryui.background") { [weak self] in
                    MainActor.assumeIsolated { self?.end(id) }
                }
                reply(.success(Data()))
            },
            "update": { _, reply in reply(.success(Data())) },
            "end": { [unowned self] args, reply in
                end(Self.id(args))
                reply(.success(Data()))
            },
            "next": { [unowned self] _, reply in
                if tasks.isEmpty { reply(.failure(RyUIModuleError("ended"))) } else { next.append(reply) }
            },
        ]
    }
}
#endif
