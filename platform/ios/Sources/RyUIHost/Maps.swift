#if canImport(UIKit) && RYUI_MAPS // an app that calls maps.View (see the CLI's iosFeatures)
import UIKit
import MapKit

/// maps.View: an MKMapView. Props: region and markers (JSON), type
/// (standard, satellite, hybrid, muted), showsUser, interactive. Events:
/// region (lat, lng, latDelta, lngDelta, once the user moved it), marker
/// (id), press (lat, lng).
@MainActor final class RyUIMapView: NSObject, RyUINativeView, MKMapViewDelegate {
    let ctx: RyUINativeContext
    let map = RyUIMap()
    var view: UIView { map }
    private var setting = false // moving the map for the region prop: not the user's move
    private var userMoving = false
    private var location: CLLocationManager?

    init(_ ctx: RyUINativeContext) {
        self.ctx = ctx
        super.init()
        map.delegate = self
        let tap = UITapGestureRecognizer(target: self, action: #selector(tapped(_:)))
        tap.delegate = map
        map.addGestureRecognizer(tap)
    }

    func set(_ name: String, _ value: RyUINativeValue) {
        switch name {
        case "region":
            guard let o = Self.json(value.string) as? [String: Any], let c = o["center"] as? [String: Any] else { return }
            let center = CLLocationCoordinate2D(latitude: c["lat"] as? Double ?? 0, longitude: c["lng"] as? Double ?? 0)
            let span = MKCoordinateSpan(latitudeDelta: max(o["latDelta"] as? Double ?? 0.05, 0.0005), longitudeDelta: max(o["lngDelta"] as? Double ?? 0.05, 0.0005))
            let r = MKCoordinateRegion(center: center, span: span)
            // The region the user moved to, given back: already there.
            let cur = map.region
            if abs(cur.center.latitude - r.center.latitude) < 1e-6 && abs(cur.center.longitude - r.center.longitude) < 1e-6
                && abs(cur.span.latitudeDelta - r.span.latitudeDelta) < 1e-6 { return }
            setting = true
            map.setRegion(r, animated: map.window != nil)
        case "markers":
            let list = Self.json(value.string) as? [[String: Any]] ?? []
            map.removeAnnotations(map.annotations.filter { $0 is RyUIMarker })
            map.addAnnotations(list.map(RyUIMarker.init))
        case "type":
            switch value.int {
            case 1: map.preferredConfiguration = MKImageryMapConfiguration()
            case 2: map.preferredConfiguration = MKHybridMapConfiguration()
            case 3: map.preferredConfiguration = MKStandardMapConfiguration(emphasisStyle: .muted)
            default: map.preferredConfiguration = MKStandardMapConfiguration()
            }
        case "showsUser":
            if value.bool && CLLocationManager().authorizationStatus == .notDetermined {
                // Asks for it, if the app has the purpose string (ryui/location's).
                if Bundle.main.object(forInfoDictionaryKey: "NSLocationWhenInUseUsageDescription") != nil {
                    location = CLLocationManager()
                    location?.requestWhenInUseAuthorization()
                }
            }
            map.showsUserLocation = value.bool
        case "interactive":
            map.isScrollEnabled = value.bool
            map.isZoomEnabled = value.bool
            map.isRotateEnabled = value.bool
            map.isPitchEnabled = value.bool
        default: break
        }
    }

    static func json(_ s: String) -> Any? { try? JSONSerialization.jsonObject(with: Data(s.utf8)) }

    @objc private func tapped(_ g: UITapGestureRecognizer) {
        let p = g.location(in: map)
        // A tap on a marker is the marker's.
        if map.annotations.contains(where: { a in map.view(for: a)?.frame.contains(p) ?? false }) { return }
        let c = map.convert(p, toCoordinateFrom: map)
        ctx.emit("press", ["lat": .float(c.latitude), "lng": .float(c.longitude)])
    }

    nonisolated func mapView(_ m: MKMapView, regionWillChangeAnimated animated: Bool) {
        MainActor.assumeIsolated {
            // A gesture moves it: the user's.
            userMoving = map.subviews.first?.gestureRecognizers?.contains { $0.state == .began || $0.state == .changed } ?? false
        }
    }

    nonisolated func mapView(_ m: MKMapView, regionDidChangeAnimated animated: Bool) {
        MainActor.assumeIsolated {
            defer { setting = false; userMoving = false }
            guard userMoving && !setting else { return }
            let r = map.region
            ctx.emit("region", ["lat": .float(r.center.latitude), "lng": .float(r.center.longitude),
                                "latDelta": .float(r.span.latitudeDelta), "lngDelta": .float(r.span.longitudeDelta)])
        }
    }

    nonisolated func mapView(_ m: MKMapView, viewFor annotation: MKAnnotation) -> MKAnnotationView? {
        MainActor.assumeIsolated {
            guard let a = annotation as? RyUIMarker else { return nil }
            let v = map.dequeueReusableAnnotationView(withIdentifier: "ryui") as? MKMarkerAnnotationView
                ?? MKMarkerAnnotationView(annotation: a, reuseIdentifier: "ryui")
            v.annotation = a
            v.markerTintColor = a.color
            v.canShowCallout = a.title != nil
            return v
        }
    }

    nonisolated func mapView(_ m: MKMapView, didSelect annotation: MKAnnotation) {
        MainActor.assumeIsolated {
            if let a = annotation as? RyUIMarker { ctx.emit("marker", ["id": .string(a.id)]) }
        }
    }
}

/// The map view: it has no size of its own (its style gives it one, or
/// Grow), and lets its own gestures and the tap recognize together.
final class RyUIMap: MKMapView, UIGestureRecognizerDelegate {
    override func sizeThatFits(_ size: CGSize) -> CGSize { .zero }
    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
}

final class RyUIMarker: NSObject, MKAnnotation {
    let id: String
    let coordinate: CLLocationCoordinate2D
    let title: String?
    let subtitle: String?
    let color: UIColor?

    init(_ o: [String: Any]) {
        id = o["id"] as? String ?? ""
        let at = o["at"] as? [String: Any] ?? [:]
        coordinate = CLLocationCoordinate2D(latitude: at["lat"] as? Double ?? 0, longitude: at["lng"] as? Double ?? 0)
        let t = o["title"] as? String ?? "", s = o["subtitle"] as? String ?? ""
        title = t.isEmpty ? nil : t
        subtitle = s.isEmpty ? nil : s
        color = (o["color"] as? String).flatMap(RyUIMarker.color)
    }

    /// "#rrggbbaa".
    static func color(_ s: String) -> UIColor? {
        guard s.count == 9, let v = UInt32(s.dropFirst(), radix: 16) else { return nil }
        return UIColor(red: CGFloat(v >> 24 & 0xff) / 255, green: CGFloat(v >> 16 & 0xff) / 255,
                       blue: CGFloat(v >> 8 & 0xff) / 255, alpha: CGFloat(v & 0xff) / 255)
    }
}
#endif
