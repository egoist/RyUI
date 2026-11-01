#if canImport(UIKit) && RYUI_VISION // an app that calls vision.Analyze (see the CLI's iosFeatures)
import UIKit
import ImageIO
import NaturalLanguage
import Vision

/// ryui/vision's module: "analyze" takes JSON (path, text, faces, codes,
/// languages) and answers JSON: the image's size upright, its lines of text
/// with their words, its faces and codes, each with a box in fractions of
/// the image from its top left. Off the main thread.
enum RyUIVision {
    @MainActor static func methods() -> [String: RyUIModuleMethod] {
        ["analyze": { args, reply in
            guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any], let path = o["path"] as? String else {
                reply(.failure(RyUIModuleError("bad args")))
                return
            }
            let text = o["text"] as? Bool ?? true, faces = o["faces"] as? Bool ?? true, codes = o["codes"] as? Bool ?? true
            let languages = o["languages"] as? [String] ?? []
            DispatchQueue.global(qos: .userInitiated).async {
                let result = Result<Data, Error> {
                    let img = try upright(path)
                    var out: [String: Any] = ["width": img.width, "height": img.height, "lines": [], "faces": [], "codes": []]
                    // One handler per kind: one that fails (a simulator
                    // lacking a model) doesn't lose the others.
                    if text { out["lines"] = try readText(img, languages) }
                    if faces { out["faces"] = try findFaces(img) }
                    if codes { out["codes"] = try findCodes(img) }
                    return try JSONSerialization.data(withJSONObject: out)
                }
                DispatchQueue.main.async { reply(result) }
            }
        }]
    }

    /// The image upright, whatever its orientation says: the boxes are in
    /// the same space as ryui/imageedit's pixels.
    static func upright(_ path: String) throws -> CGImage {
        let url = path.hasPrefix("file://") ? URL(string: path) : URL(fileURLWithPath: path)
        guard let url, let s = CGImageSourceCreateWithURL(url as CFURL, nil), CGImageSourceGetCount(s) > 0 else {
            throw RyUIModuleError("no image")
        }
        let p = CGImageSourceCopyPropertiesAtIndex(s, 0, nil) as? [CFString: Any] ?? [:]
        let w = p[kCGImagePropertyPixelWidth] as? Int ?? 0, h = p[kCGImagePropertyPixelHeight] as? Int ?? 0
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: max(w, h, 1),
            kCGImageSourceShouldCacheImmediately: true,
        ]
        guard let img = CGImageSourceCreateThumbnailAtIndex(s, 0, opts as CFDictionary) else { throw RyUIModuleError("no image") }
        return img
    }

    /// Vision's rectangle, y up, as fractions from the top left.
    static func box(_ r: CGRect) -> [String: Double] {
        ["x": Double(r.minX), "y": Double(1 - r.maxY), "w": Double(r.width), "h": Double(r.height)]
    }

    static func box(_ q: VNRectangleObservation) -> CGRect {
        let xs = [q.topLeft.x, q.topRight.x, q.bottomLeft.x, q.bottomRight.x]
        let ys = [q.topLeft.y, q.topRight.y, q.bottomLeft.y, q.bottomRight.y]
        let x = xs.min()!, y = ys.min()!
        return CGRect(x: x, y: y, width: xs.max()! - x, height: ys.max()! - y)
    }

    // The simulator can't run the learned detectors on its GPU.
    static func prepare(_ r: VNRequest) {
        #if targetEnvironment(simulator)
        r.usesCPUOnly = true
        #endif
    }

    // Text

    static func readText(_ img: CGImage, _ languages: [String]) throws -> [[String: Any]] {
        let req = VNRecognizeTextRequest()
        req.recognitionLevel = .accurate
        // Correction turns numbers and names into likelier words: here the
        // text as it's written matters.
        req.usesLanguageCorrection = false
        let supported = (try? req.supportedRecognitionLanguages()) ?? []
        var wanted = languages.isEmpty ? Locale.preferredLanguages + ["en-US"] : languages
        wanted = wanted.compactMap { l in
            if supported.contains(l) { return l }
            let base = l.split(separator: "-").first.map(String.init) ?? l
            if l.hasPrefix("zh") { return l.contains("Hant") || l.hasSuffix("TW") || l.hasSuffix("HK") ? "zh-Hant" : "zh-Hans" }
            return supported.first { $0 == base || $0.hasPrefix(base + "-") }
        }
        var langs: [String] = []
        for l in wanted where supported.contains(l) && !langs.contains(l) { langs.append(l) }
        if !langs.isEmpty { req.recognitionLanguages = langs }
        prepare(req)
        try VNImageRequestHandler(cgImage: img).perform([req])
        var lines: [[String: Any]] = []
        for ob in req.results ?? [] {
            guard let c = ob.topCandidates(1).first else { continue }
            let t = c.string
            let lineBox = (try? c.boundingBox(for: t.startIndex..<t.endIndex)).flatMap { $0 }.map(box) ?? ob.boundingBox
            lines.append(["text": t, "box": box(lineBox), "words": words(of: c, in: lineBox)])
        }
        return lines
    }

    /// The line's words, and its marks of punctuation each as a word of its
    /// own: what the platform's word breaking finds, boxed.
    static func words(of c: VNRecognizedText, in line: CGRect) -> [[String: Any]] {
        let t = c.string
        var ranges: [Range<String.Index>] = []
        let tok = NLTokenizer(unit: .word)
        tok.string = t
        var at = t.startIndex
        func marks(upTo end: String.Index) {
            var i = at
            while i < end {
                let n = t.index(after: i)
                if !t[i].isWhitespace { ranges.append(i..<n) }
                i = n
            }
        }
        tok.enumerateTokens(in: t.startIndex..<t.endIndex) { r, _ in
            marks(upTo: r.lowerBound)
            ranges.append(r)
            at = r.upperBound
            return true
        }
        marks(upTo: t.endIndex)

        var boxes = ranges.map { r -> CGRect? in
            (try? c.boundingBox(for: r)).flatMap { $0 }.map(box)
        }
        spread(&boxes, ranges.map { String(t[$0]) }, in: line)
        var out: [[String: Any]] = []
        for (r, b) in zip(ranges, boxes) {
            out.append([
                "text": String(t[r]), "box": box(b ?? line),
                "start": t.unicodeScalars.distance(from: t.startIndex, to: r.lowerBound),
                "end": t.unicodeScalars.distance(from: t.startIndex, to: r.upperBound),
            ])
        }
        return out
    }

    /// Vision's accurate level boxes what it read as one group: several
    /// words of a Chinese line get the same box, as can a word it has no
    /// box for. Those share the group's box by how wide their characters
    /// are, wide (CJK) as two narrow ones.
    static func spread(_ boxes: inout [CGRect?], _ texts: [String], in line: CGRect) {
        func weight(_ s: String) -> CGFloat {
            s.unicodeScalars.reduce(0) { $0 + ($1.value >= 0x2E80 ? 2 : 1) }
        }
        var i = 0
        while i < boxes.count {
            var j = i + 1
            let b = boxes[i]
            while j < boxes.count, same(boxes[j], b) { j += 1 }
            if j - i > 1 || b == nil {
                let group = b ?? line
                let total = max(texts[i..<j].reduce(0) { $0 + weight($1) }, 1)
                var x = group.minX
                for k in i..<j {
                    let w = group.width * weight(texts[k]) / total
                    boxes[k] = CGRect(x: x, y: group.minY, width: w, height: group.height)
                    x += w
                }
            }
            i = j
        }
    }

    static func same(_ a: CGRect?, _ b: CGRect?) -> Bool {
        guard let a, let b else { return a == nil && b == nil }
        let e: CGFloat = 0.0005
        return abs(a.minX - b.minX) < e && abs(a.minY - b.minY) < e && abs(a.width - b.width) < e && abs(a.height - b.height) < e
    }

    // Faces

    static func findFaces(_ img: CGImage) throws -> [[String: Double]] {
        let req = VNDetectFaceRectanglesRequest()
        prepare(req)
        do {
            try VNImageRequestHandler(cgImage: img).perform([req])
        } catch {
            #if targetEnvironment(simulator)
            return [] // not every simulator can run it
            #else
            throw error
            #endif
        }
        return (req.results ?? []).map { box($0.boundingBox) }
    }

    // Codes

    static func findCodes(_ img: CGImage) throws -> [[String: Any]] {
        let req = VNDetectBarcodesRequest()
        #if targetEnvironment(simulator)
        // As ryui/scan: only the first revision finds anything there.
        req.revision = VNDetectBarcodesRequestRevision1
        prepare(req)
        #endif
        try VNImageRequestHandler(cgImage: img).perform([req])
        var out: [[String: Any]] = []
        var seen: [CGRect] = []
        for o in req.results ?? [] {
            let k = RyUIScan.kind(o.symbology) ?? "other"
            let r = barsExtent(img, o.boundingBox)
            // The same code found twice (several scan lines over a barcode).
            if seen.contains(where: { $0.intersects(r) && $0.intersection(r).width * $0.intersection(r).height > 0.5 * min($0.width * $0.height, r.width * r.height) }) { continue }
            seen.append(r)
            out.append(["kind": k, "value": o.payloadStringValue ?? "", "box": box(r)])
        }
        return out
    }

    /// A barcode's box from Vision can be the line it read along, not the
    /// bars. The bars are what's above and below that line that looks like
    /// it: rows that match, from the line outward. Sideways barcodes and
    /// boxes with a height already are left as they are.
    static func barsExtent(_ img: CGImage, _ r: CGRect) -> CGRect {
        let W = img.width, H = img.height
        guard r.width > 0, r.height < r.width * 0.25, let c = CGContext(data: nil, width: W, height: H, bitsPerComponent: 8, bytesPerRow: W,
                                                                        space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGImageAlphaInfo.none.rawValue),
              let _ = Optional(c.draw(img, in: CGRect(x: 0, y: 0, width: W, height: H))), let data = c.data?.assumingMemoryBound(to: UInt8.self) else { return r }
        // Top row first in memory; Vision's y is up.
        let x0 = max(Int(r.minX * CGFloat(W)), 0), x1 = min(Int(r.maxX * CGFloat(W)), W)
        let mid = min(max(Int((1 - r.midY) * CGFloat(H)), 0), H - 1)
        guard x1 - x0 > 8 else { return r }
        func differs(_ row: Int) -> Double {
            var diff = 0
            let a = data + mid * W, b = data + row * W
            for x in x0..<x1 where abs(Int(a[x]) - Int(b[x])) > 64 { diff += 1 }
            return Double(diff) / Double(x1 - x0)
        }
        var top = mid, bottom = mid
        while top > 0, differs(top - 1) < 0.2 { top -= 1 }
        while bottom < H - 1, differs(bottom + 1) < 0.2 { bottom += 1 }
        return CGRect(x: r.minX, y: 1 - CGFloat(bottom + 1) / CGFloat(H), width: r.width, height: CGFloat(bottom + 1 - top) / CGFloat(H))
    }
}
#endif
