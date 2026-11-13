// The greeter package's Swift: its module and its view, on iOS and macOS.
// GreeterKit is its own Swift package (a resource and C).
import Asuka
import Foundation
import GreeterKit
#if canImport(UIKit)
import UIKit
#else
import AppKit
#endif

final class SwiftGreeter: Greeter {
    func hello(name: String) throws -> String {
        #if os(iOS)
        let platform = "iOS"
        #else
        let platform = "macOS"
        #endif
        return "hello \(name) from \(platform), \(GreeterKit.signature())"
    }
}

func makeGreeter() throws -> any Greeter { SwiftGreeter() }

/// A label showing its name; it sends shown with what it shows.
@MainActor final class SwiftBadge: Badge {
    #if canImport(UIKit)
    let label = UILabel()
    var text: String { label.text ?? "" }
    func show(_ s: String) { label.text = s }
    let platform = "iOS"
    #else
    let label = NSTextField(labelWithString: "")
    var text: String { label.stringValue }
    func show(_ s: String) { label.stringValue = s }
    let platform = "macOS"
    #endif
    let events: BadgeEvents
    var view: AsukaPlatformView { label }

    init(_ events: BadgeEvents) { self.events = events }

    func setName(_ value: String) {
        show("\(platform) badge for \(value)")
        events.shown(text: text)
    }
}

@MainActor func makeBadge(_ events: BadgeEvents) -> any Badge { SwiftBadge(events) }
