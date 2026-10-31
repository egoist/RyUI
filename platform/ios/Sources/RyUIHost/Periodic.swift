#if canImport(UIKit) && RYUI_PERIODIC // an app that calls ryui/periodic (see the CLI's iosFeatures)
import UIKit
import BackgroundTasks
#if canImport(RyUIOps)
import RyUIOps
#endif

/// ryui/periodic's module, with BGTaskScheduler: "schedule" (string name,
/// u32 seconds, u8 flags: 1 network, 2 charging) and "cancel" (the name)
/// keep the app's tasks (in UserDefaults) and submit a refresh task (a
/// processing task for those that want power) for the earliest due; when
/// the system runs it, the core starts if it hasn't (without a screen),
/// and "next" hands it each due task (string name, u32 seconds), which it
/// reports with "done" (string name, u8 ok).
@MainActor final class RyUIPeriodic {
    static let shared = RyUIPeriodic()
    static let refreshID = "dev.ryui.refresh", processingID = "dev.ryui.processing"
    private let key = "ryui.periodic"
    private var due: [String] = []
    private var waiting: [(Result<Data, Error>) -> Void] = []
    private var running: [String: BGTask] = [:] // a task's name: the system's task running it
    private var budget: UInt32 = 25

    /// Registers the launch handlers: in application(_:didFinishLaunchingWithOptions:).
    func registerAtLaunch() {
        for id in [Self.refreshID, Self.processingID] {
            BGTaskScheduler.shared.register(forTaskWithIdentifier: id, using: .main) { task in
                MainActor.assumeIsolated { RyUIPeriodic.shared.run(task) }
            }
        }
    }

    private struct Task: Codable {
        var every: Double
        var network: Bool
        var charging: Bool
        var last: Double
    }

    private var tasks: [String: Task] {
        get { (UserDefaults.standard.data(forKey: key)).flatMap { try? JSONDecoder().decode([String: Task].self, from: $0) } ?? [:] }
        set { UserDefaults.standard.set(try? JSONEncoder().encode(newValue), forKey: key) }
    }

    func methods() -> [String: RyUIModuleMethod] {
        [
            "schedule": { [unowned self] args, reply in
                guard args.count >= 9 else { reply(.failure(RyUIModuleError("bad args"))); return }
                let (name, secs, flags) = args.withUnsafeBytes { b -> (String, UInt32, UInt8) in
                    var r = RyUIReader(b)
                    return (r.string(), r.u32(), r.u8())
                }
                var ts = self.tasks
                let last = ts[name]?.last ?? Date().timeIntervalSince1970
                ts[name] = Task(every: Double(secs), network: flags & 1 != 0, charging: flags & 2 != 0, last: last)
                self.tasks = ts
                self.submit()
                reply(.success(Data()))
            },
            "cancel": { [unowned self] args, reply in
                var ts = self.tasks
                ts[String(decoding: args, as: UTF8.self)] = nil
                self.tasks = ts
                self.submit()
                reply(.success(Data()))
            },
            "next": { [unowned self] _, reply in
                if self.due.isEmpty { self.waiting.append(reply) } else { reply(.success(self.encode(self.due.removeFirst()))) }
            },
            "done": { [unowned self] args, reply in
                let (name, ok) = args.withUnsafeBytes { b -> (String, UInt8) in var r = RyUIReader(b); return (r.string(), r.u8()) }
                self.finished(name, ok == 1)
                reply(.success(Data()))
            },
        ]
    }

    private func encode(_ name: String) -> Data {
        var w = RyUIWriter()
        w.string(name)
        w.u32(budget)
        return Data(w.bytes)
    }

    /// Submits the next refresh and processing tasks, for the earliest due of each.
    private func submit() {
        let ts = tasks
        func earliest(_ charging: Bool) -> (Date, Bool)? {
            let mine = ts.values.filter { $0.charging == charging }
            guard let t = mine.min(by: { $0.last + $0.every < $1.last + $1.every }) else { return nil }
            return (Date(timeIntervalSince1970: t.last + t.every), mine.contains { $0.network })
        }
        let s = BGTaskScheduler.shared
        if let (at, _) = earliest(false) {
            let r = BGAppRefreshTaskRequest(identifier: Self.refreshID)
            r.earliestBeginDate = at
            try? s.submit(r)
        } else {
            s.cancel(taskRequestWithIdentifier: Self.refreshID)
        }
        if let (at, network) = earliest(true) {
            let r = BGProcessingTaskRequest(identifier: Self.processingID)
            r.earliestBeginDate = at
            r.requiresExternalPower = true
            r.requiresNetworkConnectivity = network
            try? s.submit(r)
        } else {
            s.cancel(taskRequestWithIdentifier: Self.processingID)
        }
    }

    /// The system runs task: the due tasks of its kind go to the Go code.
    private func run(_ task: BGTask) {
        let processing = task.identifier == Self.processingID
        let now = Date().timeIntervalSince1970
        let names = tasks.filter { $0.value.charging == processing && $0.value.last + $0.value.every <= now + 60 }.map(\.key)
        guard !names.isEmpty else {
            submit()
            task.setTaskCompleted(success: true)
            return
        }
        budget = processing ? 240 : 25
        task.expirationHandler = { [weak self] in
            MainActor.assumeIsolated {
                guard let self else { return }
                for (n, t) in self.running where t === task { self.running[n] = nil }
                self.due.removeAll { names.contains($0) }
                self.submit()
                task.setTaskCompleted(success: false)
            }
        }
        for n in names { running[n] = task }
        RyUIRuntime.shared.startHeadless()
        for n in names {
            if waiting.isEmpty { due.append(n) } else { waiting.removeFirst()(.success(encode(n))) }
        }
    }

    private func finished(_ name: String, _ ok: Bool) {
        guard let task = running.removeValue(forKey: name) else { return }
        var ts = tasks
        ts[name]?.last = Date().timeIntervalSince1970
        tasks = ts
        // The system's task is done when all its runs are.
        if !running.values.contains(where: { $0 === task }) {
            submit()
            task.setTaskCompleted(success: ok)
        }
    }
}
#endif
