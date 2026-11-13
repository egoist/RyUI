// The app's native views for ui.Native on the Mac. The Asuka CLI compiles
// this file into a library in the app's bundle, which the Mac host loads.
import AppKit

@objc(AsukaAppNatives) final class AppNatives: NSObject, AsukaNativeModule {
    @MainActor static func register() {
        AsukaRuntime.shared.registerNative("Segmented") { ctx in Segmented(ctx) }
    }
}

/// An NSSegmentedControl: props "items" (comma-separated) and "selected";
/// event "changed" with "index".
@MainActor final class Segmented: NSObject, AsukaNativeView {
    let control = NSSegmentedControl()
    var view: NSView { control }
    private let ctx: AsukaNativeContext
    private var selected = 0

    init(_ ctx: AsukaNativeContext) {
        self.ctx = ctx
        super.init()
        control.trackingMode = .selectOne
        control.segmentDistribution = .fillEqually // as wide as its view
        control.target = self
        control.action = #selector(changed)
    }

    @objc private func changed() {
        selected = control.selectedSegment
        ctx.emit("changed", ["index": .int(Int64(selected))])
    }

    func set(_ name: String, _ value: AsukaNativeValue) {
        switch name {
        case "items":
            let items = value.string.split(separator: ",")
            control.segmentCount = items.count
            for (i, t) in items.enumerated() {
                control.setLabel(String(t), forSegment: i)
            }
        case "selected":
            selected = Int(value.int)
        default:
            return
        }
        // Props come in no set order: the items may follow the selection.
        if selected < control.segmentCount {
            control.selectedSegment = selected
        }
    }
}
