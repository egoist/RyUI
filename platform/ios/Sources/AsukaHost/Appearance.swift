#if canImport(UIKit)
import UIKit

/// asuka/sdk/appearance's module: "current" (u8 has accent, red, green, blue,
/// increase contrast, reduce transparency), and, while watching, "next"
/// and "stop". iOS has no accent of the system's (an app's tint is its
/// own): none. Increase Contrast and Reduce Transparency are
/// UIAccessibility's.
@MainActor final class AsukaAppearance: NSObject {
    static let shared = AsukaAppearance()
    private lazy var watch = AsukaWatch(state: { Self.state() }, start: { [unowned self] in self.observe(true) }, end: { [unowned self] in self.observe(false) })

    private func observe(_ on: Bool) {
        let nc = NotificationCenter.default
        for n in [UIAccessibility.darkerSystemColorsStatusDidChangeNotification, UIAccessibility.reduceTransparencyStatusDidChangeNotification] {
            if on { nc.addObserver(self, selector: #selector(changed), name: n, object: nil) } else { nc.removeObserver(self, name: n, object: nil) }
        }
    }

    @objc func changed() { watch.changed() }

    private static func state() -> Data {
        Data([0, 0, 0, 0, UIAccessibility.isDarkerSystemColorsEnabled ? 1 : 0, UIAccessibility.isReduceTransparencyEnabled ? 1 : 0])
    }

    func methods() -> [String: AsukaModuleMethod] { watch.methods() }
}
#endif
