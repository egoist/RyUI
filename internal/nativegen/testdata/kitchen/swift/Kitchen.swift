// The kitchen package's Swift: what its Go declares, implemented, on iOS
// and macOS (host/darwin's test calls each; so does the GUI tests' plugin
// app).
import Asuka
import Foundation
#if canImport(UIKit)
import UIKit
#else
import AppKit
#endif

final class SwiftKitchen: Kitchen {
    func name() -> String { "home" }
    func items(filter: String) throws -> [Item] {
        [Item(name: filter, weight: 1.5, unit: .gram, tags: ["a", "b"], notes: ["k": "v"],
              best: Date(timeIntervalSince1970: 1_700_000_000.25), next: Item(name: "inner"),
              data: Data([1, 2, 3]), took: 2.5, id: UInt64.max, count: -3, ratio: 0.5, fresh: true)]
    }
    func add(item: Item) throws { if item.name.isEmpty { throw KitchenError.busy } }
    func cook(recipe: String, progress: @escaping (Double) -> Void) async throws -> Item {
        if recipe == "slow" {
            try await Task.sleep(nanoseconds: 10_000_000_000)
        }
        progress(0.5)
        progress(1)
        return Item(name: recipe + " done")
    }
    func ask(question: @escaping (String) -> Bool) throws -> Bool { question("ok?") }
    func open(name: String) throws -> any Oven {
        if name == "none" { throw NSError(domain: "Oven", code: 7, userInfo: [NSLocalizedDescriptionKey: "no oven"]) }
        return try AsukaNative.onMain { SwiftOven() }
    }
    func show() throws {}
    func level() -> Level { .high }
    func counts() -> [String: Int64] { ["a": 1, "b": -2] }
    func when(at: Date, every: TimeInterval) throws -> Date? { every == 0 ? nil : at.addingTimeInterval(every) }
}

@MainActor final class SwiftOven: Oven {
    func close() throws {}
    func heat(to: Float, unit: Unit) throws {
        if unit == .ounce { throw AsukaNativeError.unsupported("no ounces") }
    }
}

func makeKitchen() throws -> any Kitchen { SwiftKitchen() }
@MainActor func makeOven() throws -> any Oven { SwiftOven() }

#if canImport(UIKit)
@MainActor final class SwiftDial: Dial, AsukaDestroying {
    let slider = UISlider()
    let events: DialEvents
    init(_ events: DialEvents) { self.events = events }
    var view: AsukaPlatformView { slider }
    func setValue(_ value: Double) {
        slider.value = Float(value)
        events.changed(value: value * 2)
    }
    func setLabel(_ value: String) { slider.accessibilityLabel = value }
    func setItems(_ value: [Item]) {
        if let f = value.last { events.picked(item: f, index: value.count - 1) }
    }
    func destroy() {}
}
#else
@MainActor final class SwiftDial: Dial, AsukaSizing, AsukaDestroying {
    let slider = NSSlider()
    let events: DialEvents
    init(_ events: DialEvents) { self.events = events }
    var view: AsukaPlatformView { slider }
    func setValue(_ value: Double) {
        slider.doubleValue = value
        events.changed(value: value * 2)
    }
    func setLabel(_ value: String) { slider.toolTip = value }
    func setItems(_ value: [Item]) {
        if let f = value.last { events.picked(item: f, index: value.count - 1) }
    }
    func sizeThatFits(_ size: CGSize) -> CGSize { CGSize(width: 160, height: 24) }
    func destroy() {}
}
#endif

@MainActor func makeDial(_ events: DialEvents) -> any Dial { SwiftDial(events) }
