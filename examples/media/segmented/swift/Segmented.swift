// The segmented control on iOS (UISegmentedControl) and macOS
// (NSSegmentedControl).
import Asuka
#if canImport(UIKit)
import UIKit

@MainActor final class SwiftSegmented: Segmented {
    let control = UISegmentedControl()
    let events: SegmentedEvents
    var view: AsukaPlatformView { control }
    private var selected = 0

    init(_ events: SegmentedEvents) {
        self.events = events
        control.addAction(UIAction { [weak self] _ in
            guard let self else { return }
            selected = control.selectedSegmentIndex
            self.events.changed(index: selected)
        }, for: .valueChanged)
    }

    func setItems(_ value: [String]) {
        control.removeAllSegments()
        for (i, t) in value.enumerated() { control.insertSegment(withTitle: t, at: i, animated: false) }
        select()
    }

    func setSelected(_ value: Int) {
        selected = value
        select()
    }

    // Props come in no set order: the items may follow the selection.
    private func select() {
        if selected < control.numberOfSegments { control.selectedSegmentIndex = selected }
    }
}
#else
import AppKit

@MainActor final class SwiftSegmented: NSObject, Segmented {
    let control = NSSegmentedControl()
    let events: SegmentedEvents
    var view: AsukaPlatformView { control }
    private var selected = 0

    init(_ events: SegmentedEvents) {
        self.events = events
        super.init()
        control.trackingMode = .selectOne
        control.segmentDistribution = .fillEqually // as wide as its view
        control.target = self
        control.action = #selector(changed)
    }

    @objc private func changed() {
        selected = control.selectedSegment
        events.changed(index: selected)
    }

    func setItems(_ value: [String]) {
        control.segmentCount = value.count
        for (i, t) in value.enumerated() { control.setLabel(t, forSegment: i) }
        select()
    }

    func setSelected(_ value: Int) {
        selected = value
        select()
    }

    // Props come in no set order: the items may follow the selection.
    private func select() {
        if selected < control.segmentCount { control.selectedSegment = selected }
    }
}
#endif

@MainActor func makeSegmented(_ events: SegmentedEvents) -> any Segmented { SwiftSegmented(events) }
