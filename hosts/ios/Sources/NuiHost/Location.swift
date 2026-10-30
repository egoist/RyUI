#if canImport(UIKit) && NUI_LOCATION // an app that imports nui/location (see the CLI's iosFeatures)
import UIKit
import CoreLocation

/// nui/location's module: the position now ("current"), and each next
/// one while watching ("next", "stop"). Replies are JSON: lat, lon, acc
/// (meters), alt, t (Unix ms); "denied" fails when the user hasn't
/// allowed it.
@MainActor final class NuiLocation: NSObject, CLLocationManagerDelegate {
    static let shared = NuiLocation()
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

    /// Asks for permission the first time; then whether it's allowed.
    private func authorize(_ then: @escaping (Bool) -> Void) {
        if manager.authorizationStatus == .notDetermined {
            authorizing.append { [unowned self] in then(self.allowed) }
            manager.requestWhenInUseAuthorization()
        } else {
            then(allowed)
        }
    }

    func methods() -> [String: NuiModuleMethod] {
        [
            "current": { [unowned self] _, reply in
                authorize { ok in
                    guard ok else { reply(.failure(NuiModuleError("denied"))); return }
                    self.current.append(reply)
                    self.manager.requestLocation()
                }
            },
            "next": { [unowned self] _, reply in
                authorize { ok in
                    guard ok else { reply(.failure(NuiModuleError("denied"))); return }
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
                for w in ws { w(.failure(NuiModuleError("stopped"))) }
                reply(.success(Data()))
            },
        ]
    }

    private static func encode(_ l: CLLocation) -> Data {
        let d: [String: Any] = ["lat": l.coordinate.latitude, "lon": l.coordinate.longitude, "acc": l.horizontalAccuracy,
                                "alt": l.verticalAccuracy >= 0 ? l.altitude : 0, "t": Int64(l.timestamp.timeIntervalSince1970 * 1000)]
        return (try? JSONSerialization.data(withJSONObject: d)) ?? Data("{}".utf8)
    }

    nonisolated func locationManagerDidChangeAuthorization(_ m: CLLocationManager) {
        MainActor.assumeIsolated {
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
            let e: Error = denied ? NuiModuleError("denied") : error
            let cs = current
            current = []
            for c in cs { c(.failure(e)) }
        }
    }
}
#endif
