#if canImport(UIKit) && ASUKA_REVIEW // an app that calls review.Request (see the CLI's iosFeatures)
import UIKit
import StoreKit

/// asuka/sdk/review's module: "request" asks the App Store for its rating
/// prompt, which it shows or not (at most three times a year).
@MainActor enum AsukaReview {
    static func methods(_ rt: AsukaRuntime) -> [String: AsukaModuleMethod] {
        [
            "request": { [weak rt] _, reply in
                guard let scene = rt?.controller?.view.window?.windowScene else { reply(.failure(AsukaModuleError("no screen"))); return }
                AppStore.requestReview(in: scene)
                reply(.success(Data()))
            },
        ]
    }
}
#endif
