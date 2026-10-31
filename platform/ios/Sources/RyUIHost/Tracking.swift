#if canImport(UIKit) && RYUI_TRACKING // an app that calls ryui/tracking (see the CLI's iosFeatures)
import UIKit
import AppTrackingTransparency

/// ryui/tracking's module: "request" shows the App Tracking Transparency
/// prompt (the first time), "status" reads the answer. The reply: u8 as
/// tracking.Status: not asked, restricted, denied, allowed.
@MainActor enum RyUITracking {
    static func code(_ s: ATTrackingManager.AuthorizationStatus) -> UInt8 {
        switch s {
        case .restricted: return 1
        case .denied: return 2
        case .authorized: return 3
        default: return 0
        }
    }

    static func methods() -> [String: RyUIModuleMethod] {
        [
            "request": { _, reply in
                ATTrackingManager.requestTrackingAuthorization { s in reply(.success(Data([code(s)]))) }
            },
            "status": { _, reply in
                reply(.success(Data([code(ATTrackingManager.trackingAuthorizationStatus)])))
            },
        ]
    }
}
#endif
