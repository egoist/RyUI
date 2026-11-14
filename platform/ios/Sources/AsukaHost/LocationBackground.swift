#if canImport(UIKit) && ASUKA_LOCATION_BACKGROUND // an app that calls asuka/sdk/location's background part (see the CLI's iosFeatures)
import UIKit
import CoreLocation

/// asuka/sdk/location's background part (locationspec's "locationBackground"):
/// - "start" (JSON Updates), "stop", "updating": updates that go on with the
///   app in the background, kept across launches (UserDefaults) and started
///   again at launch. iOS 17's CLLocationUpdate.liveUpdates (with a
///   CLBackgroundActivitySession) for the best accuracies; else a
///   CLLocationManager with allowsBackgroundLocationUpdates, or its
///   significant-change service, which relaunches the app.
/// - "monitor" (JSON Region), "unmonitor" (an ID; empty: all), "regions":
///   iOS 17's CLMonitor ("asuka"), CLCircularRegion before; 20 at most,
///   Always needed. Both relaunch the app for a transition.
/// - "next", "done" (an event's ID): the events (JSON locationspec.Event),
///   kept in a file until Go reports each done, so one that came as the app
///   ended is handed over at the next launch. An event starts the core
///   (without a screen if there's none) and holds a background task until
///   the events are done.
/// - "capabilities".
@MainActor final class AsukaLocationBackground: NSObject, CLLocationManagerDelegate {
    static let shared = AsukaLocationBackground()
    static let limit = 20
    private let manager = CLLocationManager()
    private let updatesKey = "asuka.location.updates", regionsKey = "asuka.location.regions", insideKey = "asuka.location.inside"
    private var events: [[String: Any]] = []
    private var handed = Set<String>()
    private var waiting: [(Result<Data, Error>) -> Void] = []
    private var task: UIBackgroundTaskIdentifier = .invalid
    private var lastKept: CLLocation?
    private var lastSeen: CLLocation?
    private var live: Task<Void, Never>?
    private var session: AnyObject? // CLBackgroundActivitySession
    private var batchTimer: Timer?
    private var initialCheck = Set<String>() // regions just added: is the device inside? (before iOS 17)
    private var monitorRef: AnyObject? // CLMonitor
    private var monitorWaiting: [(AnyObject) -> Void]?
    private var registered = false

    private var file: URL {
        let d = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d.appendingPathComponent("asuka-location-events.json")
    }

    private var updates: [String: Any]? {
        get { UserDefaults.standard.data(forKey: updatesKey).flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } }
        set {
            if let v = newValue, let d = try? JSONSerialization.data(withJSONObject: v) {
                UserDefaults.standard.set(d, forKey: updatesKey)
            } else {
                UserDefaults.standard.removeObject(forKey: updatesKey)
            }
        }
    }

    private var regions: [[String: Any]] {
        get { UserDefaults.standard.data(forKey: regionsKey).flatMap { try? JSONSerialization.jsonObject(with: $0) as? [[String: Any]] } ?? [] }
        set { UserDefaults.standard.set(try? JSONSerialization.data(withJSONObject: newValue), forKey: regionsKey) }
    }

    /// Whether the device was inside each region, as last reported.
    private var inside: [String: Bool] {
        get { UserDefaults.standard.dictionary(forKey: insideKey) as? [String: Bool] ?? [:] }
        set { UserDefaults.standard.set(newValue, forKey: insideKey) }
    }

    /// At launch (application(_:didFinishLaunchingWithOptions:)): the
    /// events kept, the delegate for those the system relaunched the app
    /// for, and what ran before started again.
    func registerAtLaunch() {
        guard !registered else { return }
        registered = true
        manager.delegate = self
        if let d = try? Data(contentsOf: file), let es = try? JSONSerialization.jsonObject(with: d) as? [[String: Any]] {
            events = es
        }
        if let u = updates { begin(u) }
        closeBatch(force: true) // what a launch before this one batched
        if #available(iOS 17.0, *), !regions.isEmpty { withMonitor { _ in } }
        if events.contains(where: { $0["open"] == nil }) { hold() }
    }

    private func save() {
        if let d = try? JSONSerialization.data(withJSONObject: events) { try? d.write(to: file, options: .atomic) }
    }

    private static func ms(_ d: Date) -> Int64 { Int64(d.timeIntervalSince1970 * 1000) }

    // MARK: Events

    private func add(_ e: [String: Any]) {
        var e = e
        e["id"] = UUID().uuidString
        events.append(e)
        save()
        if e["open"] == nil { ready() }
    }

    /// An event is ready: the core starts if it hasn't, and gets it.
    private func ready() {
        hold()
        AsukaRuntime.shared.startHeadless()
        hand()
    }

    private func hand() {
        while !waiting.isEmpty, let e = events.first(where: { $0["open"] == nil && !handed.contains($0["id"] as? String ?? "") }) {
            handed.insert(e["id"] as? String ?? "")
            var out = e
            out["seconds"] = budget()
            let data = (try? JSONSerialization.data(withJSONObject: out)) ?? Data("{}".utf8)
            waiting.removeFirst()(.success(data))
        }
    }

    /// How long the app may run in the background: 0 on screen, or while
    /// it runs for location updates.
    private func budget() -> Int {
        let app = UIApplication.shared
        if app.applicationState == .active { return 0 }
        let left = app.backgroundTimeRemaining
        return left > 600 ? 0 : max(Int(left), 1)
    }

    /// Holds a background task while events aren't done.
    private func hold() {
        guard task == .invalid, UIApplication.shared.applicationState != .active else { return }
        task = UIApplication.shared.beginBackgroundTask(withName: "asuka.location") {
            MainActor.assumeIsolated { AsukaLocationBackground.shared.release(force: true) }
        }
    }

    private func release(force: Bool = false) {
        guard task != .invalid, force || !events.contains(where: { $0["open"] == nil }) else { return }
        let t = task
        task = .invalid
        UIApplication.shared.endBackgroundTask(t)
    }

    // MARK: Updates

    private static func accuracy(_ a: Int) -> CLLocationAccuracy {
        [kCLLocationAccuracyBest, kCLLocationAccuracyBestForNavigation, kCLLocationAccuracyHundredMeters,
         kCLLocationAccuracyKilometer, kCLLocationAccuracyThreeKilometers][max(0, min(a, 4))]
    }

    private static func activity(_ a: Int) -> CLActivityType {
        [.other, .automotiveNavigation, .otherNavigation, .fitness, .airborne][max(0, min(a, 4))]
    }

    private var backgroundMode: Bool {
        (Bundle.main.object(forInfoDictionaryKey: "UIBackgroundModes") as? [String])?.contains("location") == true
    }

    /// Starts the updates u (JSON's object) as they're kept.
    private func begin(_ u: [String: Any]) {
        end()
        let significant = u["significant"] as? Bool ?? false
        let acc = u["accuracy"] as? Int ?? 0
        if significant {
            manager.startMonitoringSignificantLocationChanges()
            return
        }
        if #available(iOS 17.0, *) {
            session = CLBackgroundActivitySession()
            if acc <= 1 {
                let cfg: CLLocationUpdate.LiveConfiguration
                switch u["activity"] as? Int ?? 0 {
                case 1: cfg = .automotiveNavigation
                case 2: cfg = .otherNavigation
                case 3: cfg = .fitness
                case 4: cfg = .airborne
                default: cfg = acc == 1 ? .otherNavigation : .default
                }
                live = Task { @MainActor in
                    do {
                        for try await update in CLLocationUpdate.liveUpdates(cfg) {
                            if Task.isCancelled { return }
                            if let l = update.location { AsukaLocationBackground.shared.got([l]) }
                        }
                    } catch {}
                }
                return
            }
        }
        manager.desiredAccuracy = Self.accuracy(acc)
        let d = (u["distance"] as? NSNumber)?.doubleValue ?? 0
        manager.distanceFilter = d > 0 ? d : kCLDistanceFilterNone
        manager.activityType = Self.activity(u["activity"] as? Int ?? 0)
        manager.pausesLocationUpdatesAutomatically = u["autoPause"] as? Bool ?? false
        if backgroundMode { manager.allowsBackgroundLocationUpdates = true }
        manager.showsBackgroundLocationIndicator = u["indicator"] as? Bool ?? false
        manager.startUpdatingLocation()
    }

    private func end() {
        live?.cancel()
        live = nil
        if #available(iOS 17.0, *) { (session as? CLBackgroundActivitySession)?.invalidate() }
        session = nil
        manager.stopUpdatingLocation()
        manager.stopMonitoringSignificantLocationChanges()
        lastKept = nil
    }

    /// New positions: those past the distance filter go into an event (the
    /// batch filling, with Defer).
    private func got(_ ls: [CLLocation]) {
        if let l = ls.last { lastSeen = l }
        guard let u = updates else { return }
        let dist = (u["significant"] as? Bool ?? false) ? 0 : (u["distance"] as? NSNumber)?.doubleValue ?? 0
        var kept: [[String: Any]] = []
        for l in ls where l.horizontalAccuracy >= 0 {
            if let k = lastKept, dist > 0, l.distance(from: k) < dist { continue }
            lastKept = l
            kept.append(AsukaLocation.object(l))
        }
        guard !kept.isEmpty else { return }
        let wait = ((u["defer"] as? NSNumber)?.doubleValue ?? 0) / 1000
        guard wait > 0 else { add(["positions": kept]); return }
        let now = Date().timeIntervalSince1970
        if let i = events.lastIndex(where: { $0["open"] != nil }) {
            events[i]["positions"] = (events[i]["positions"] as? [[String: Any]] ?? []) + kept
            save()
        } else {
            add(["positions": kept, "open": true, "opened": now])
            batchTimer?.invalidate()
            batchTimer = Timer.scheduledTimer(withTimeInterval: wait, repeats: false) { _ in
                MainActor.assumeIsolated { AsukaLocationBackground.shared.closeBatch(force: true) }
            }
        }
        closeBatch(force: false)
    }

    /// Closes the batch filling once its Defer is over (the timer's, or
    /// a position's that came later: a suspended app's timers don't fire).
    private func closeBatch(force: Bool) {
        guard let i = events.lastIndex(where: { $0["open"] != nil }) else { return }
        let wait = ((updates?["defer"] as? NSNumber)?.doubleValue ?? 0) / 1000
        let opened = events[i]["opened"] as? Double ?? 0
        guard force || Date().timeIntervalSince1970 - opened >= wait else { return }
        events[i]["open"] = nil
        events[i]["opened"] = nil
        batchTimer?.invalidate()
        batchTimer = nil
        save()
        ready()
    }

    // MARK: Regions

    private func region(_ id: String) -> [String: Any]? { regions.first { $0["id"] as? String == id } }

    /// A region's transition, if it's one it reports: 1 enter, 2 exit.
    private func crossed(_ id: String, _ kind: Int, at: Date) {
        guard let r = region(id) else { return }
        var ins = inside
        let was = ins[id]
        ins[id] = kind == 1
        inside = ins
        // From unknown to outside is none: the region was just added with
        // the device out of it.
        if was == nil && kind == 2 || was == (kind == 1) { return }
        guard (r["events"] as? Int ?? 3) & kind != 0 else { return }
        var e: [String: Any] = ["region": id, "kind": kind, "t": Self.ms(at)]
        if let l = lastSeen, Date().timeIntervalSince(l.timestamp) < 60 { e["at"] = AsukaLocation.object(l) }
        add(e)
    }

    /// Calls f with iOS 17's monitor, made once (at launch, so that the
    /// events it relaunched the app for come).
    @available(iOS 17.0, *)
    private func withMonitor(_ f: @escaping (CLMonitor) -> Void) {
        if let m = monitorRef as? CLMonitor { f(m); return }
        if monitorWaiting != nil { monitorWaiting?.append { f($0 as! CLMonitor) }; return }
        monitorWaiting = [{ f($0 as! CLMonitor) }]
        Task { @MainActor in
            let m = await CLMonitor("asuka")
            let me = AsukaLocationBackground.shared
            me.monitorRef = m
            let ws = me.monitorWaiting ?? []
            me.monitorWaiting = nil
            for w in ws { w(m) }
            do {
                for try await e in await m.events {
                    switch e.state {
                    case .satisfied: me.crossed(e.identifier, 1, at: e.date)
                    case .unsatisfied: me.crossed(e.identifier, 2, at: e.date)
                    default: break
                    }
                }
            } catch {}
        }
    }

    private var always: Bool { manager.authorizationStatus == .authorizedAlways }

    private func monitor(_ r: [String: Any], _ reply: @escaping (Result<Data, Error>) -> Void) {
        guard let id = r["id"] as? String, let lat = (r["lat"] as? NSNumber)?.doubleValue, let lon = (r["lon"] as? NSNumber)?.doubleValue,
              let radius = (r["radius"] as? NSNumber)?.doubleValue else { reply(.failure(AsukaModuleError("bad region"))); return }
        guard always else { reply(.failure(AsukaModuleError("always"))); return }
        var rs = regions
        rs.removeAll { $0["id"] as? String == id }
        guard rs.count < Self.limit else {
            reply(.failure(AsukaModuleError("too many regions (\(Self.limit) at most on iOS)")))
            return
        }
        let maxRadius = manager.maximumRegionMonitoringDistance
        guard maxRadius <= 0 || radius <= maxRadius else {
            reply(.failure(AsukaModuleError("region \(id): a radius of \(Int(radius)) m is more than iOS's \(Int(maxRadius)) m")))
            return
        }
        rs.append(r)
        regions = rs
        var ins = inside
        ins[id] = nil
        inside = ins
        let center = CLLocationCoordinate2D(latitude: lat, longitude: lon)
        if #available(iOS 17.0, *) {
            withMonitor { m in
                Task { @MainActor in
                    await m.add(CLMonitor.CircularGeographicCondition(center: center, radius: radius), identifier: id, assuming: .unsatisfied)
                    reply(.success(Data()))
                }
            }
        } else {
            let c = CLCircularRegion(center: center, radius: radius, identifier: id)
            let ev = r["events"] as? Int ?? 3
            c.notifyOnEntry = ev & 1 != 0 || ev & 4 != 0
            c.notifyOnExit = true // to know it's out, for the next Enter
            initialCheck.insert(id)
            manager.startMonitoring(for: c)
            reply(.success(Data()))
        }
    }

    private func unmonitor(_ id: String, _ reply: @escaping (Result<Data, Error>) -> Void) {
        let gone = regions.compactMap { $0["id"] as? String }.filter { id.isEmpty || $0 == id }
        regions = regions.filter { !gone.contains($0["id"] as? String ?? "") }
        var ins = inside
        for g in gone { ins[g] = nil }
        inside = ins
        if #available(iOS 17.0, *) {
            withMonitor { m in
                Task { @MainActor in
                    // Every condition the monitor has, those of an ID no longer kept too.
                    let kept = Set(AsukaLocationBackground.shared.regions.compactMap { $0["id"] as? String })
                    for i in await m.identifiers where id.isEmpty || i == id || !kept.contains(i) { await m.remove(i) }
                    reply(.success(Data()))
                }
            }
        } else {
            for r in manager.monitoredRegions where id.isEmpty || r.identifier == id { manager.stopMonitoring(for: r) }
            reply(.success(Data()))
        }
    }

    // MARK: The module

    func methods() -> [String: AsukaModuleMethod] {
        registerAtLaunch()
        return [
            "capabilities": { [unowned self] _, reply in
                let c: [String: Any] = ["updates": true, "regions": true, "limit": Self.limit, "dwell": false, "relaunch": true,
                                        "maxRadius": self.manager.maximumRegionMonitoringDistance]
                reply(.success((try? JSONSerialization.data(withJSONObject: c)) ?? Data()))
            },
            "start": { [unowned self] args, reply in
                guard let u = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] else {
                    reply(.failure(AsukaModuleError("bad updates"))); return
                }
                if !(u["significant"] as? Bool ?? false) && !self.backgroundMode {
                    reply(.failure(AsukaModuleError("Info.plist's UIBackgroundModes has no location: asuka build adds it for an app that calls location.StartUpdates")))
                    return
                }
                AsukaLocationAccess.shared.request(3) { _ in
                    let s = AsukaLocationAccess.shared.status
                    guard s == .authorizedWhenInUse || s == .authorizedAlways else { reply(.failure(AsukaModuleError("denied"))); return }
                    self.updates = u
                    self.begin(u)
                    reply(.success(Data()))
                }
            },
            "stop": { [unowned self] _, reply in
                self.updates = nil
                self.end()
                self.closeBatch(force: true)
                reply(.success(Data()))
            },
            "updating": { [unowned self] _, reply in
                reply(.success(self.updates.flatMap { try? JSONSerialization.data(withJSONObject: $0) } ?? Data("null".utf8)))
            },
            "monitor": { [unowned self] args, reply in
                guard let r = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] else {
                    reply(.failure(AsukaModuleError("bad region"))); return
                }
                self.monitor(r, reply)
            },
            "unmonitor": { [unowned self] args, reply in self.unmonitor(String(decoding: args, as: UTF8.self), reply) },
            "regions": { [unowned self] _, reply in
                reply(.success((try? JSONSerialization.data(withJSONObject: self.regions)) ?? Data("[]".utf8)))
            },
            "next": { [unowned self] _, reply in
                self.waiting.append(reply)
                self.hand()
            },
            "done": { [unowned self] args, reply in
                let id = String(decoding: args, as: UTF8.self)
                self.events.removeAll { $0["id"] as? String == id }
                self.handed.remove(id)
                self.save()
                self.release()
                reply(.success(Data()))
            },
        ]
    }

    // MARK: The delegate

    nonisolated func locationManager(_ m: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        MainActor.assumeIsolated { got(locations) }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didStartMonitoringFor region: CLRegion) {
        MainActor.assumeIsolated {
            if initialCheck.contains(region.identifier) { m.requestState(for: region) }
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didDetermineState state: CLRegionState, for region: CLRegion) {
        MainActor.assumeIsolated {
            guard initialCheck.remove(region.identifier) != nil else { return }
            if state == .inside { crossed(region.identifier, 1, at: Date()) }
            if state == .outside { crossed(region.identifier, 2, at: Date()) }
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didEnterRegion region: CLRegion) {
        MainActor.assumeIsolated { crossed(region.identifier, 1, at: Date()) }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didExitRegion region: CLRegion) {
        MainActor.assumeIsolated { crossed(region.identifier, 2, at: Date()) }
    }

    nonisolated func locationManager(_ m: CLLocationManager, monitoringDidFailFor region: CLRegion?, withError error: Error) {
        MainActor.assumeIsolated {
            // iOS won't monitor it: it's no longer monitored (Monitored says so).
            guard let id = region?.identifier else { return }
            NSLog("asuka: location: region %@ isn't monitored: %@", id, error.localizedDescription)
            regions = regions.filter { $0["id"] as? String != id }
            initialCheck.remove(id)
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didFailWithError error: Error) {}
}
#endif
