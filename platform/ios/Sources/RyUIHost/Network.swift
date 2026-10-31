#if canImport(UIKit) && RYUI_NETWORK // an app that calls ryui/network (see the CLI's iosFeatures)
import Foundation
import Network

/// ryui/network's module: "current", and, while watching, "next" (answered
/// once the state isn't the one given) and "stop", from NWPathMonitor.
/// Replies are u8 connected, u8 type (none, wifi, cellular, ethernet,
/// other), u8 metered (isExpensive), u8 constrained (Low Data Mode).
@MainActor final class RyUINetwork {
    static let shared = RyUINetwork()
    private let monitor = NWPathMonitor()
    private var path: NWPath?
    private var next: [(Data, (Result<Data, Error>) -> Void)] = []
    private var current: [(Result<Data, Error>) -> Void] = []

    private init() {
        monitor.pathUpdateHandler = { [weak self] p in
            DispatchQueue.main.async { self?.update(p) }
        }
        monitor.start(queue: DispatchQueue(label: "ryui.network"))
    }

    private func update(_ p: NWPath) {
        path = p
        let s = Self.encode(p)
        let cs = current
        current = []
        for c in cs { c(.success(s)) }
        let changed = next.filter { $0.0 != s }
        next.removeAll { $0.0 != s }
        for (_, reply) in changed { reply(.success(s)) }
    }

    static func encode(_ p: NWPath) -> Data {
        guard p.status == .satisfied else { return Data([0, 0, 0, 0]) }
        let type: UInt8 = p.usesInterfaceType(.wifi) ? 1 : p.usesInterfaceType(.cellular) ? 2 : p.usesInterfaceType(.wiredEthernet) ? 3 : 4
        return Data([1, type, p.isExpensive ? 1 : 0, p.isConstrained ? 1 : 0])
    }

    func methods() -> [String: RyUIModuleMethod] {
        [
            // The first path comes soon after the monitor starts.
            "current": { [unowned self] _, reply in
                if let p = self.path { reply(.success(Self.encode(p))) } else { self.current.append(reply) }
            },
            "next": { [unowned self] args, reply in
                guard let p = self.path else { self.current.append(reply); return }
                let s = Self.encode(p)
                if args.isEmpty || args != s { reply(.success(s)) } else { self.next.append((args, reply)) }
            },
            "stop": { [unowned self] _, reply in
                let ws = self.next
                self.next = []
                for (_, w) in ws { w(.failure(RyUIModuleError("stopped"))) }
                reply(.success(Data()))
            },
        ]
    }
}
#endif
