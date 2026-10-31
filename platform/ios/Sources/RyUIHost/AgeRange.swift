#if canImport(UIKit) && RYUI_AGERANGE // an app that calls ryui/agerange (see the CLI's iosFeatures)
import UIKit
import DeclaredAgeRange

/// ryui/agerange's module: "request" (up to three u32 age gates) asks for
/// the Declared Age Range (iOS 26) and answers u8 status (declined,
/// shared), u8 source (unknown, self, guardian, confirmed), u32 lower,
/// u32 upper (0: open); it fails with "not available" before iOS 26 and
/// where the system can't answer.
@MainActor enum RyUIAgeRange {
    static func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        [
            "request": { [weak rt] args, reply in
                guard #available(iOS 26.0, *) else { reply(.failure(RyUIModuleError("not available"))); return }
                guard let vc = rt?.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
                var gates: [Int] = []
                var i = 0
                while i + 4 <= args.count && gates.count < 3 {
                    gates.append(Int(args.subdata(in: args.startIndex + i ..< args.startIndex + i + 4).withUnsafeBytes { $0.loadUnaligned(as: UInt32.self).littleEndian }))
                    i += 4
                }
                if gates.isEmpty { gates = [18] }
                Task { @MainActor in
                    do {
                        let r = try await AgeRangeService.shared.requestAgeRange(ageGates: gates[0], gates.count > 1 ? gates[1] : nil, gates.count > 2 ? gates[2] : nil, in: vc)
                        var out = Data()
                        func u32(_ v: Int) { withUnsafeBytes(of: UInt32(max(v, 0)).littleEndian) { out.append(contentsOf: $0) } }
                        switch r {
                        case .declinedSharing:
                            out.append(contentsOf: [0, 0]); u32(0); u32(0)
                        case .sharing(let range):
                            var source: UInt8 = 0
                            switch range.ageRangeDeclaration {
                            case .selfDeclared?: source = 1
                            case .guardianDeclared?: source = 2
                            case nil: source = 0
                            default: source = 3 // checked or confirmed
                            }
                            out.append(contentsOf: [1, source]); u32(range.lowerBound ?? 0); u32(range.upperBound ?? 0)
                        @unknown default:
                            out.append(contentsOf: [0, 0]); u32(0); u32(0)
                        }
                        reply(.success(out))
                    } catch let e as AgeRangeService.Error where e == .notAvailable {
                        reply(.failure(RyUIModuleError("not available")))
                    } catch {
                        reply(.failure(error))
                    }
                }
            },
        ]
    }
}
#endif
