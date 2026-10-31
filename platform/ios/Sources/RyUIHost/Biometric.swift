#if canImport(UIKit) && RYUI_BIOMETRIC // an app that calls ryui/biometric (see the CLI's iosFeatures)
import UIKit
import LocalAuthentication
#if canImport(RyUIOps)
import RyUIOps
#endif

/// ryui/biometric's module: "check" answers u8 kind (none, fingerprint,
/// face, iris, optic), u8 enrolled, u8 passcode; "authenticate" (u8
/// flags: 1 no passcode; string reason; string cancel) shows the prompt,
/// and fails with "canceled", "not available" or "failed".
@MainActor enum RyUIBiometric {
    static func methods() -> [String: RyUIModuleMethod] {
        [
            "check": { _, reply in
                let c = LAContext()
                var e: NSError?
                let enrolled = c.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &e)
                var kind: UInt8 = 0
                switch c.biometryType {
                case .touchID: kind = 1
                case .faceID: kind = 2
                default:
                    if #available(iOS 17.0, *), c.biometryType == .opticID { kind = 4 }
                }
                let passcode = LAContext().canEvaluatePolicy(.deviceOwnerAuthentication, error: nil)
                reply(.success(Data([kind, enrolled ? 1 : 0, passcode ? 1 : 0])))
            },
            "authenticate": { args, reply in
                guard args.count >= 9 else { reply(.failure(RyUIModuleError("bad args"))); return }
                let (flags, reason, cancel) = args.withUnsafeBytes { b -> (UInt8, String, String) in
                    var r = RyUIReader(b)
                    return (r.u8(), r.string(), r.string())
                }
                let c = LAContext()
                if !cancel.isEmpty { c.localizedCancelTitle = cancel }
                let biometryOnly = flags & 1 != 0
                if biometryOnly { c.localizedFallbackTitle = "" }
                let policy: LAPolicy = biometryOnly ? .deviceOwnerAuthenticationWithBiometrics : .deviceOwnerAuthentication
                var e: NSError?
                guard c.canEvaluatePolicy(policy, error: &e) else { reply(.failure(RyUIModuleError("not available"))); return }
                c.evaluatePolicy(policy, localizedReason: reason.isEmpty ? " " : reason) { ok, error in
                    if ok { reply(.success(Data())); return }
                    switch (error as? LAError)?.code {
                    case .userCancel?, .appCancel?, .systemCancel?, .userFallback?: reply(.failure(RyUIModuleError("canceled")))
                    case .biometryNotAvailable?, .biometryNotEnrolled?, .biometryLockout?, .passcodeNotSet?: reply(.failure(RyUIModuleError("not available")))
                    case .authenticationFailed?: reply(.failure(RyUIModuleError("failed")))
                    default: reply(.failure(error ?? RyUIModuleError("failed")))
                    }
                }
            },
        ]
    }
}
#endif
