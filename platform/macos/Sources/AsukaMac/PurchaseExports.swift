#if ASUKA_PURCHASE // an app that imports asuka/sdk/purchase (the CLI's featurePermissions)
import Foundation

// The Mac host's way into asuka/sdk/purchase's StoreKit (Purchase.swift,
// the iOS host's): host/darwin's "purchase" module calls these through
// purego, on the main thread, and each call's answer comes back through
// the reply it booted with, on the main thread too.

/// reply(call, ok, bytes, count): ok 1, the answer; 0, an error's message.
public typealias AsukaPurchaseReply = @convention(c) (UInt64, Int32, UnsafePointer<UInt8>?, Int) -> Void

private var purchaseReply: AsukaPurchaseReply?

/// Takes the reply, and starts listening to StoreKit's transactions: as
/// the app starts, so none that comes before the Go code observes is lost.
@_cdecl("asuka_purchase_boot")
public func asuka_purchase_boot(_ reply: AsukaPurchaseReply) {
    purchaseReply = reply
    MainActor.assumeIsolated {
        if #available(macOS 12.0, *) { AsukaStore.shared.start() }
    }
}

/// Runs a method of the module (internal/purchasespec's), answering call.
@_cdecl("asuka_purchase_call")
public func asuka_purchase_call(_ call: UInt64, _ method: UnsafePointer<CChar>, _ args: UnsafePointer<UInt8>?, _ count: Int) {
    let name = String(cString: method)
    let data = args.map { Data(bytes: $0, count: count) } ?? Data()
    MainActor.assumeIsolated {
        guard #available(macOS 12.0, *) else {
            purchaseAnswer(call, .failure(AsukaPurchaseFailure("not available: the App Store's StoreKit needs macOS 12")))
            return
        }
        AsukaStore.shared.call(name, data) { purchaseAnswer(call, $0) }
    }
}

private struct AsukaPurchaseFailure: LocalizedError {
    let message: String
    init(_ m: String) { message = m }
    var errorDescription: String? { message }
}

private func purchaseAnswer(_ call: UInt64, _ r: Result<Data, Error>) {
    guard let reply = purchaseReply else { return }
    switch r {
    case .success(let d):
        d.withUnsafeBytes { reply(call, 1, $0.bindMemory(to: UInt8.self).baseAddress, d.count) }
    case .failure(let e):
        let m = Array(e.localizedDescription.utf8)
        m.withUnsafeBufferPointer { reply(call, 0, $0.baseAddress, m.count) }
    }
}
#endif
