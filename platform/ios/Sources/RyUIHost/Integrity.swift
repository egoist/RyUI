#if canImport(UIKit) && RYUI_INTEGRITY // an app that calls ryui/integrity (see the CLI's iosFeatures)
import UIKit
import DeviceCheck
#if canImport(RyUIOps)
import RyUIOps
#endif

/// ryui/integrity's module, App Attest: "supported" (u8), "generateKey"
/// (the key's ID), "attestKey" and "assert" (string key ID, then the
/// 32-byte client data hash) answer the attestation or assertion.
/// Play Integrity's "prepare" and "token" are Android's.
@MainActor enum RyUIIntegrity {
    static func methods() -> [String: RyUIModuleMethod] {
        let s = DCAppAttestService.shared
        func keyed(_ args: Data) -> (String, Data)? {
            guard args.count >= 36 else { return nil }
            let id = args.withUnsafeBytes { b -> String in var r = RyUIReader(b); return r.string() }
            return (id, Data(args.suffix(32)))
        }
        return [
            "supported": { _, reply in reply(.success(Data([s.isSupported ? 1 : 0]))) },
            "generateKey": { _, reply in
                guard s.isSupported else { reply(.failure(RyUIModuleError("App Attest isn't on this device"))); return }
                s.generateKey { id, error in reply(id.map { .success(Data($0.utf8)) } ?? .failure(error ?? RyUIModuleError("no key"))) }
            },
            "attestKey": { args, reply in
                guard let (id, hash) = keyed(args) else { reply(.failure(RyUIModuleError("bad args"))); return }
                s.attestKey(id, clientDataHash: hash) { d, error in reply(d.map { .success($0) } ?? .failure(error ?? RyUIModuleError("no attestation"))) }
            },
            "assert": { args, reply in
                guard let (id, hash) = keyed(args) else { reply(.failure(RyUIModuleError("bad args"))); return }
                s.generateAssertion(id, clientDataHash: hash) { d, error in reply(d.map { .success($0) } ?? .failure(error ?? RyUIModuleError("no assertion"))) }
            },
            "prepare": { _, reply in reply(.failure(RyUIModuleError("Play Integrity is Android's: use App Attest on iOS"))) },
            "token": { _, reply in reply(.failure(RyUIModuleError("Play Integrity is Android's: use App Attest on iOS"))) },
        ]
    }
}
#endif
