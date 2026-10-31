#if canImport(UIKit) && RYUI_REVIEW // an app that calls review.Request (see the CLI's iosFeatures)
import UIKit
import StoreKit

/// ryui/review's module: "request" asks the App Store for its rating
/// prompt, which it shows or not (at most three times a year).
@MainActor enum RyUIReview {
    static func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        [
            "request": { [weak rt] _, reply in
                guard let scene = rt?.controller?.view.window?.windowScene else { reply(.failure(RyUIModuleError("no screen"))); return }
                AppStore.requestReview(in: scene)
                reply(.success(Data()))
            },
        ]
    }
}
#endif
