#if canImport(UIKit)
import UIKit

/// A module's watching of a state (internal/follow's side): "next" (args:
/// the state last given) is answered once the state isn't that one, or
/// with the events queued since (they follow the state); events are
/// queued from the first "next" until "stop", which answers the waiting
/// ones "stopped". start and end begin and end observing.
@MainActor final class AsukaWatch {
    private let state: () -> Data
    private let start: () -> Void
    private let end: () -> Void
    private var waiting: [(Data, (Result<Data, Error>) -> Void)] = []
    private var events = Data()
    private(set) var watching = false

    init(state: @escaping () -> Data, start: @escaping () -> Void = {}, end: @escaping () -> Void = {}) {
        self.state = state
        self.start = start
        self.end = end
    }

    func next(_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) {
        if !watching {
            watching = true
            start()
        }
        waiting.append((args, reply))
        changed()
    }

    /// Queues an event while watched.
    func event(_ e: UInt8) {
        guard watching else { return }
        events.append(e)
        changed()
    }

    /// Answers the waiting ones the state (and events) are new to.
    func changed() {
        guard !waiting.isEmpty else { return }
        let s = state()
        var answer: [(Result<Data, Error>) -> Void] = []
        var keep: [(Data, (Result<Data, Error>) -> Void)] = []
        for w in waiting {
            if !events.isEmpty || w.0 != s { answer.append(w.1) } else { keep.append(w) }
        }
        guard !answer.isEmpty else { return }
        waiting = keep
        let out = s + events
        events = Data()
        for reply in answer { reply(.success(out)) }
    }

    func stop(_ reply: (Result<Data, Error>) -> Void) {
        if watching {
            watching = false
            end()
        }
        events = Data()
        let ws = waiting
        waiting = []
        for (_, w) in ws { w(.failure(AsukaModuleError("stopped"))) }
        reply(.success(Data()))
    }

    func methods() -> [String: AsukaModuleMethod] {
        [
            "current": { [unowned self] _, reply in reply(.success(self.state())) },
            "next": { [unowned self] args, reply in self.next(args, reply) },
            "stop": { [unowned self] _, reply in self.stop(reply) },
        ]
    }
}

/// A number as JSON, the same each time: whole, or to three places.
func asukaJSONNumber(_ v: Double) -> String {
    guard v.isFinite else { return "0" }
    let r = (v * 1000).rounded() / 1000
    if r == r.rounded() { return String(Int64(r)) }
    var s = String(format: "%.3f", r)
    while s.hasSuffix("0") { s.removeLast() }
    return s
}

/// asuka/sdk/screen's displays: "current" (a JSON list as screen.Display's,
/// the device's own screen first, in points as it's turned now), and,
/// while watching, "next" and "stop". A screen the iPhone mirrors or
/// extends to (AirPlay, a cable) follows it, placed after it in a row.
/// A display's work area is its bounds: the status bar and the home
/// indicator are the app's safe area (ui.UseEnvironment).
@MainActor final class AsukaDisplays: NSObject {
    static let shared = AsukaDisplays()
    private lazy var watch = AsukaWatch(state: { [unowned self] in self.state() }, start: { [unowned self] in self.observe(true) }, end: { [unowned self] in self.observe(false) })

    private func observe(_ on: Bool) {
        let nc = NotificationCenter.default
        let names = [UIScreen.didConnectNotification, UIScreen.didDisconnectNotification, UIScreen.modeDidChangeNotification]
        for n in names {
            if on { nc.addObserver(self, selector: #selector(changed), name: n, object: nil) } else { nc.removeObserver(self, name: n, object: nil) }
        }
    }

    /// Something about the screens changed (one connected, a mode, the
    /// app's size as the device turns).
    @objc func changed() { watch.changed() }

    private func state() -> Data {
        let main = UIScreen.main
        let screens = [main] + UIScreen.screens.filter { $0 !== main }
        var x = 0.0
        var out = "["
        for (i, s) in screens.enumerated() {
            let b = s.bounds
            let rect = "{\"x\":\(asukaJSONNumber(x)),\"y\":0,\"width\":\(asukaJSONNumber(b.width)),\"height\":\(asukaJSONNumber(b.height))}"
            x += b.width
            if i > 0 { out += "," }
            let id = s === main ? "main" : "external-\(i)"
            let name = s === main ? "Built-in Display" : "External Display"
            out += "{\"id\":\"\(id)\",\"name\":\"\(name)\",\"bounds\":\(rect),\"workArea\":\(rect),\"scale\":\(asukaJSONNumber(s.scale)),\"refreshRate\":\(asukaJSONNumber(Double(s.maximumFramesPerSecond))),\"primary\":\(s === main)}"
        }
        out += "]"
        return Data(out.utf8)
    }

    func methods() -> [String: AsukaModuleMethod] { watch.methods() }
}

/// asuka/sdk/dock: a phone has no Dock or taskbar: nothing to show, nothing asked.
enum AsukaDock {
    static func methods() -> [String: AsukaModuleMethod] {
        [
            "progress": { _, reply in reply(.success(Data())) },
            "attention": { _, reply in reply(.success(Data([0, 0, 0, 0]))) },
            "cancelAttention": { _, reply in reply(.success(Data())) },
        ]
    }
}
#endif
