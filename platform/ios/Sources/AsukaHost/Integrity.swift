#if canImport(UIKit) && ASUKA_INTEGRITY // an app that calls asuka/integrity (see the CLI's iosFeatures)
import UIKit
import DeviceCheck
#if canImport(AsukaOps)
import AsukaOps
#endif

/// asuka/integrity's module, App Attest: "supported" (u8), "generateKey"
/// (the key's ID), "attestKey" and "assert" (string key ID, then the
/// 32-byte client data hash) answer the attestation or assertion.
/// Play Integrity's "prepare" and "token" are Android's.
@MainActor enum AsukaIntegrity {
    static func methods() -> [String: AsukaModuleMethod] {
        let s = DCAppAttestService.shared
        func keyed(_ args: Data) -> (String, Data)? {
            guard args.count >= 36 else { return nil }
            let id = args.withUnsafeBytes { b -> String in var r = AsukaReader(b); return r.string() }
            return (id, Data(args.suffix(32)))
        }
        return [
            "supported": { _, reply in reply(.success(Data([s.isSupported ? 1 : 0]))) },
            "generateKey": { _, reply in
                guard s.isSupported else { reply(.failure(AsukaModuleError("App Attest isn't on this device"))); return }
                s.generateKey { id, error in reply(id.map { .success(Data($0.utf8)) } ?? .failure(error ?? AsukaModuleError("no key"))) }
            },
            "attestKey": { args, reply in
                guard let (id, hash) = keyed(args) else { reply(.failure(AsukaModuleError("bad args"))); return }
                s.attestKey(id, clientDataHash: hash) { d, error in reply(d.map { .success($0) } ?? .failure(error ?? AsukaModuleError("no attestation"))) }
            },
            "assert": { args, reply in
                guard let (id, hash) = keyed(args) else { reply(.failure(AsukaModuleError("bad args"))); return }
                s.generateAssertion(id, clientDataHash: hash) { d, error in reply(d.map { .success($0) } ?? .failure(error ?? AsukaModuleError("no assertion"))) }
            },
            "prepare": { _, reply in reply(.failure(AsukaModuleError("Play Integrity is Android's: use App Attest on iOS"))) },
            "token": { _, reply in reply(.failure(AsukaModuleError("Play Integrity is Android's: use App Attest on iOS"))) },
        ]
    }
}
#endif
