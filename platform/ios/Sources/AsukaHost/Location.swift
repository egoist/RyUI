#if canImport(UIKit) && ASUKA_LOCATION // an app that imports asuka/sdk/location (see the CLI's iosFeatures)
import UIKit
import CoreLocation

/// asuka/sdk/location's module: the position now ("current"), and each next
/// one while watching ("next", "stop"). Replies are JSON: lat, lon, acc
/// (meters), alt, t (Unix ms); "denied" fails when the user hasn't
/// allowed it.
@MainActor final class AsukaLocation: NSObject, CLLocationManagerDelegate {
    static let shared = AsukaLocation()
    private let manager = CLLocationManager()
    private var authorizing: [() -> Void] = []
    private var current: [(Result<Data, Error>) -> Void] = []
    private var next: [(Result<Data, Error>) -> Void] = []
    private var unseen: CLLocation? // the newest, not yet given to a next
    private var updating = false

    override init() {
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyBest
    }

    private var allowed: Bool { [.authorizedWhenInUse, .authorizedAlways].contains(manager.authorizationStatus) }

    /// The authorization changed (the user answered): its followers see it.
    private func changed() { AsukaLocationAccess.shared.changed() }

    /// Asks for permission the first time; then whether it's allowed.
    private func authorize(_ then: @escaping (Bool) -> Void) {
        if manager.authorizationStatus == .notDetermined {
            authorizing.append { [unowned self] in then(self.allowed) }
            manager.requestWhenInUseAuthorization()
        } else {
            then(allowed)
        }
    }

    func methods() -> [String: AsukaModuleMethod] {
        [
            "name": { args, reply in
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Double] ?? [:]
                CLGeocoder().reverseGeocodeLocation(CLLocation(latitude: o["lat"] ?? 0, longitude: o["lon"] ?? 0)) { places, error in
                    if let p = places?.first {
                        let parts = [p.subLocality ?? p.name, p.locality ?? p.administrativeArea ?? p.country].compactMap { $0 }
                        var seen = Set<String>()
                        reply(.success(Data(parts.filter { seen.insert($0).inserted }.joined(separator: ", ").utf8)))
                    } else {
                        reply(.failure(error ?? AsukaModuleError("no name for this place")))
                    }
                }
            },
            "current": { [unowned self] _, reply in
                authorize { ok in
                    guard ok else { reply(.failure(AsukaModuleError("denied"))); return }
                    self.current.append(reply)
                    self.manager.requestLocation()
                }
            },
            "next": { [unowned self] _, reply in
                authorize { ok in
                    guard ok else { reply(.failure(AsukaModuleError("denied"))); return }
                    if let u = self.unseen {
                        self.unseen = nil
                        reply(.success(Self.encode(u)))
                    } else {
                        self.next.append(reply)
                    }
                    if !self.updating {
                        self.updating = true
                        self.manager.startUpdatingLocation()
                    }
                }
            },
            "stop": { [unowned self] _, reply in
                updating = false
                unseen = nil
                manager.stopUpdatingLocation()
                let ws = next
                next = []
                for w in ws { w(.failure(AsukaModuleError("stopped"))) }
                reply(.success(Data()))
            },
        ]
    }

    static func encode(_ l: CLLocation) -> Data {
        (try? JSONSerialization.data(withJSONObject: object(l))) ?? Data("{}".utf8)
    }

    /// A position as JSON's object: lat, lon, acc, alt (0: not known),
    /// spd and crs (negative: not known), t (Unix ms).
    static func object(_ l: CLLocation) -> [String: Any] {
        ["lat": l.coordinate.latitude, "lon": l.coordinate.longitude, "acc": l.horizontalAccuracy,
         "alt": l.verticalAccuracy >= 0 ? l.altitude : 0, "spd": l.speedAccuracy >= 0 ? l.speed : -1,
         "crs": l.courseAccuracy >= 0 ? l.course : -1, "t": Int64(l.timestamp.timeIntervalSince1970 * 1000)]
    }

    nonisolated func locationManagerDidChangeAuthorization(_ m: CLLocationManager) {
        MainActor.assumeIsolated {
            changed()
            guard m.authorizationStatus != .notDetermined else { return }
            let ws = authorizing
            authorizing = []
            for w in ws { w() }
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        MainActor.assumeIsolated {
            guard let l = locations.last else { return }
            let data = Self.encode(l)
            let cs = current
            current = []
            for c in cs { c(.success(data)) }
            guard updating else { return }
            if next.isEmpty {
                unseen = l
            } else {
                let ws = next
                next = []
                for w in ws { w(.success(data)) }
            }
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didFailWithError error: Error) {
        MainActor.assumeIsolated {
            // Not known yet: Core Location keeps trying.
            if (error as? CLError)?.code == .locationUnknown { return }
            let denied = (error as? CLError)?.code == .denied
            let e: Error = denied ? AsukaModuleError("denied") : error
            let cs = current
            current = []
            for c in cs { c(.failure(e)) }
        }
    }
}

/// asuka/sdk/location's authorization (locationspec's "locationAccess"):
/// "current", "next" (args: the last state; answered once it differs) and
/// "stop" follow it, three bytes: level (0 not asked, 1 denied, 2
/// restricted, 3 when in use, 4 always), precise, off; "request" (u8: 3
/// when in use, 4 always) asks the user and answers with the state they
/// left; "precise" (a purpose key of NSLocationTemporaryUsageDescriptionDictionary)
/// asks for full accuracy until the app leaves the foreground; "settings"
/// opens the app's page in Settings.
@MainActor final class AsukaLocationAccess: NSObject, CLLocationManagerDelegate {
    static let shared = AsukaLocationAccess()
    private let manager = CLLocationManager()
    private var waits: [(last: Data, reply: (Result<Data, Error>) -> Void)] = []
    private var answering: [() -> Void] = [] // requests waiting for the user's answer
    private var servicesOn = true
    private var resigned = false // the app went inactive: a question shows

    override init() {
        super.init()
        manager.delegate = self
        refreshServices()
        let nc = NotificationCenter.default
        nc.addObserver(forName: UIApplication.willResignActiveNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { AsukaLocationAccess.shared.resigned = true }
        }
        nc.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated {
                let a = AsukaLocationAccess.shared
                a.refreshServices()
                // A question was answered (or dismissed) without a change.
                if a.resigned { a.resigned = false; a.answered() }
            }
        }
    }

    /// Whether location services are on: asked off the main thread, as Core Location wants.
    private func refreshServices() {
        DispatchQueue.global().async {
            let on = CLLocationManager.locationServicesEnabled()
            DispatchQueue.main.async {
                MainActor.assumeIsolated {
                    let a = AsukaLocationAccess.shared
                    if a.servicesOn != on { a.servicesOn = on; a.changed() }
                }
            }
        }
    }

    static func level(_ s: CLAuthorizationStatus) -> UInt8 {
        switch s {
        case .notDetermined: return 0
        case .denied: return 1
        case .restricted: return 2
        case .authorizedWhenInUse: return 3
        case .authorizedAlways: return 4
        @unknown default: return 1
        }
    }

    var status: CLAuthorizationStatus { manager.authorizationStatus }

    var state: Data {
        Data([Self.level(manager.authorizationStatus), manager.accuracyAuthorization == .fullAccuracy ? 1 : 0, servicesOn ? 0 : 1])
    }

    /// The state may have changed: the followers whose last state it isn't get it.
    func changed() {
        let s = state
        var keep: [(last: Data, reply: (Result<Data, Error>) -> Void)] = []
        for w in waits {
            if w.last != s { w.reply(.success(s)) } else { keep.append(w) }
        }
        waits = keep
    }

    /// The user answered, or no question showed: the requests waiting get the state.
    private func answered() {
        let ws = answering
        answering = []
        for w in ws { w() }
    }

    /// Waits for the user's answer to a question just asked: the
    /// authorization's change, or the question going without one; a
    /// question that doesn't show (asked before) answers within a second.
    private func waitForAnswer(_ then: @escaping () -> Void) {
        answering.append(then)
        DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
            MainActor.assumeIsolated {
                let a = AsukaLocationAccess.shared
                if !a.resigned { a.answered() }
            }
        }
    }

    /// Asks for level (3, 4), then gives the state.
    func request(_ level: UInt8, _ done: @escaping (Data) -> Void) {
        switch manager.authorizationStatus {
        case .notDetermined:
            waitForAnswer { [unowned self] in
                if level == 4 && self.manager.authorizationStatus == .authorizedWhenInUse {
                    self.request(4, done)
                } else {
                    done(self.state)
                }
            }
            manager.requestWhenInUseAuthorization()
        case .authorizedWhenInUse where level == 4:
            waitForAnswer { [unowned self] in done(self.state) }
            manager.requestAlwaysAuthorization()
        default:
            done(state)
        }
    }

    func methods() -> [String: AsukaModuleMethod] {
        [
            "current": { [unowned self] _, reply in reply(.success(self.state)) },
            "next": { [unowned self] args, reply in
                let s = self.state
                if args.isEmpty || args != s { reply(.success(s)) } else { self.waits.append((args, reply)) }
            },
            "stop": { [unowned self] _, reply in
                let ws = self.waits
                self.waits = []
                for w in ws { w.reply(.failure(AsukaModuleError("stopped"))) }
                reply(.success(Data()))
            },
            "request": { [unowned self] args, reply in
                self.request(args.first ?? 3) { reply(.success($0)) }
            },
            "precise": { [unowned self] args, reply in
                guard [.authorizedWhenInUse, .authorizedAlways].contains(self.manager.authorizationStatus),
                      self.manager.accuracyAuthorization != .fullAccuracy else { reply(.success(self.state)); return }
                let key = String(decoding: args, as: UTF8.self)
                self.manager.requestTemporaryFullAccuracyAuthorization(withPurposeKey: key.isEmpty ? "default" : key) { _ in
                    DispatchQueue.main.async {
                        MainActor.assumeIsolated { reply(.success(AsukaLocationAccess.shared.state)) }
                    }
                }
            },
            "settings": { _, reply in
                if let u = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(u) }
                reply(.success(Data()))
            },
        ]
    }

    nonisolated func locationManagerDidChangeAuthorization(_ m: CLLocationManager) {
        MainActor.assumeIsolated {
            changed()
            if m.authorizationStatus != .notDetermined { answered() }
        }
    }
}
#endif
