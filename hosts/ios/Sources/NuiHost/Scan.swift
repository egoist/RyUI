#if canImport(UIKit)
import Foundation

/// The kinds of code nui/scan asks for ("" or none: every kind), parsed.
struct NuiCodeKinds {
    let wanted: Set<String>
    init(_ s: String) { wanted = Set(s.split(separator: ",").map(String.init)) }
    func wants(_ kind: String) -> Bool { wanted.isEmpty || wanted.contains(kind) }

    /// The code as nui/scan names it. Apple reads a UPC-A as an EAN-13
    /// with a leading 0: it's a UPC-A again if the app asked for those.
    func named(_ kind: String, _ value: String) -> (String, String) {
        if kind == "ean13", value.count == 13, value.hasPrefix("0"), wants("upca") {
            return ("upca", String(value.dropFirst()))
        }
        return (kind, value)
    }
}

/// Repeats of a code in view, dropped: a code is reported as it comes
/// into view, and again once it's been out of it for a second.
struct NuiCodeSeen {
    private var last: [String: TimeInterval] = [:]
    mutating func isNew(_ kind: String, _ value: String, now: TimeInterval) -> Bool {
        let key = kind + "\u{0}" + value
        let new = (last[key].map { now - $0 > 1 }) ?? true
        last[key] = now
        if last.count > 32 { last = last.filter { now - $0.value <= 1 } }
        return new
    }
}

/// The reply nui/scan's decode reads: a u32 count, then each code's kind
/// and value, each a u32 length and its bytes.
func nuiEncodeCodes(_ codes: [(String, String)]) -> Data {
    var d = Data()
    func u32(_ n: Int) { var v = UInt32(n).littleEndian; withUnsafeBytes(of: &v) { d.append(contentsOf: $0) } }
    u32(codes.count)
    for (k, v) in codes {
        for s in [k, v] { let b = Data(s.utf8); u32(b.count); d.append(b) }
    }
    return d
}
#endif

#if canImport(UIKit) && NUI_SCAN // an app that calls scan.File (see the CLI's iosFeatures)
import Vision

/// nui/scan's module: "file" reads the codes in an image file, with Vision.
/// args: the kinds, comma-separated, a newline, the path.
enum NuiScan {
    static let symbologies: [(String, [VNBarcodeSymbology])] = [
        ("qr", [.qr]), ("aztec", [.aztec]), ("datamatrix", [.dataMatrix]), ("pdf417", [.pdf417]),
        ("ean13", [.ean13]), ("ean8", [.ean8]), ("upca", [.ean13]), ("upce", [.upce]),
        ("code128", [.code128]), ("code39", [.code39]), ("code93", [.code93]), ("codabar", [.codabar]),
        ("itf", [.itf14, .i2of5]),
    ]

    static func kind(_ s: VNBarcodeSymbology) -> String? {
        symbologies.first { $0.0 != "upca" && $0.1.contains(s) }?.0
    }

    @MainActor static func methods() -> [String: NuiModuleMethod] {
        ["file": { args, reply in
            let s = String(decoding: args, as: UTF8.self)
            guard let nl = s.firstIndex(of: "\n") else { reply(.failure(NuiModuleError("bad arguments"))); return }
            let kinds = NuiCodeKinds(String(s[..<nl]))
            let url = URL(fileURLWithPath: String(s[s.index(after: nl)...]))
            var wanted: [VNBarcodeSymbology] = []
            for (k, syms) in symbologies where kinds.wants(k) {
                for x in syms where !wanted.contains(x) { wanted.append(x) }
            }
            DispatchQueue.global(qos: .userInitiated).async {
                let req = VNDetectBarcodesRequest()
                req.symbologies = wanted
                #if targetEnvironment(simulator)
                // The simulator can't run the learned detectors (revisions 2
                // and later find nothing there): the first, on the CPU.
                req.revision = VNDetectBarcodesRequestRevision1
                req.usesCPUOnly = true
                req.symbologies = wanted.filter { (try? req.supportedSymbologies())?.contains($0) ?? false }
                #endif
                let result: Result<Data, Error>
                do {
                    try VNImageRequestHandler(url: url).perform([req])
                    var codes: [(String, String)] = []
                    for o in req.results ?? [] {
                        guard let v = o.payloadStringValue, let k = kind(o.symbology) else { continue }
                        let c = kinds.named(k, v)
                        if kinds.wants(c.0), !codes.contains(where: { $0 == c }) { codes.append(c) }
                    }
                    result = .success(nuiEncodeCodes(codes))
                } catch {
                    result = .failure(NuiModuleError("can't read the image: \(error.localizedDescription)"))
                }
                DispatchQueue.main.async { reply(result) }
            }
        }]
    }
}
#endif
