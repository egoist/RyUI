// The app's native views for ui.Native. The nui CLI compiles this file
// with the iOS host.
import UIKit

@objc(NuiAppNatives) final class AppNatives: NSObject, NuiNativeModule {
    @MainActor static func register() {
        NuiRuntime.shared.registerNative("Segmented") { ctx in Segmented(ctx) }
    }
}

/// A UISegmentedControl: props "items" (comma-separated) and "selected";
/// event "changed" with "index".
@MainActor final class Segmented: NuiNativeView {
    let control = UISegmentedControl()
    var view: UIView { control }

    init(_ ctx: NuiNativeContext) {
        control.addAction(UIAction { [weak control] _ in
            guard let control else { return }
            ctx.emit("changed", ["index": .int(Int64(control.selectedSegmentIndex))])
        }, for: .valueChanged)
    }

    func set(_ name: String, _ value: NuiNativeValue) {
        switch name {
        case "items":
            let sel = control.selectedSegmentIndex
            control.removeAllSegments()
            for (i, t) in value.string.split(separator: ",").enumerated() {
                control.insertSegment(withTitle: String(t), at: i, animated: false)
            }
            control.selectedSegmentIndex = sel
        case "selected":
            control.selectedSegmentIndex = Int(value.int)
        default:
            break
        }
    }
}
