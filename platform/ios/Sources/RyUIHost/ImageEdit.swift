#if canImport(UIKit) && RYUI_IMAGEEDIT // an app that calls ryui/imageedit (see the CLI's iosFeatures)
import UIKit
import ImageIO
import AVFoundation
import UniformTypeIdentifiers

/// ryui/imageedit's module: "edit", "size" and "videoFrame", each with
/// JSON (source, actions, format, quality, at in ms), answer JSON (path,
/// width, height) or "no image". ImageIO decodes (HEIC, JPEG, PNG, WebP...)
/// upright, at the size the first resize needs; Core Graphics does the
/// actions; ImageIO writes JPEG or PNG. Off the main thread.
enum RyUIImageEdit {
    struct Request {
        var source: String
        var actions: [[String: Any]]
        var png: Bool
        var quality: Double
        var at: Double // seconds

        init?(_ args: Data) {
            guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any], let s = o["source"] as? String else { return nil }
            source = s
            actions = o["actions"] as? [[String: Any]] ?? []
            png = o["format"] as? String == "png"
            quality = o["quality"] as? Double ?? 0.9
            at = (o["at"] as? Double ?? 0) / 1000
        }
    }

    static func methods() -> [String: RyUIModuleMethod] {
        func run(_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void, _ f: @escaping (Request) throws -> [String: Any]) {
            guard let r = Request(args) else { reply(.failure(RyUIModuleError("bad args"))); return }
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    let out = try f(r)
                    reply(.success(try JSONSerialization.data(withJSONObject: out)))
                } catch {
                    reply(.failure(error))
                }
            }
        }
        return [
            "size": { args, reply in
                run(args, reply) { r in
                    let src = try imageSource(r.source)
                    let (w, h) = uprightSize(src)
                    return ["path": "", "width": w, "height": h]
                }
            },
            "edit": { args, reply in
                run(args, reply) { r in
                    let src = try imageSource(r.source)
                    var img = try decode(src, for: r.actions)
                    img = try apply(r.actions, to: img)
                    return try write(img, r)
                }
            },
            "videoFrame": { args, reply in
                run(args, reply) { r in
                    var img = try frame(r.source, at: r.at)
                    img = try apply(r.actions, to: img)
                    return try write(img, r)
                }
            },
        ]
    }

    // Sources: as ui.Image's.

    static func url(_ source: String) -> URL? {
        if source.hasPrefix("http://") || source.hasPrefix("https://") || source.hasPrefix("file://") { return URL(string: source) }
        if source.hasPrefix("/") { return URL(fileURLWithPath: source) }
        return Bundle.main.resourceURL?.appendingPathComponent("assets").appendingPathComponent(source)
    }

    static func imageSource(_ source: String) throws -> CGImageSource {
        guard let u = url(source) else { throw RyUIModuleError("no image") }
        if u.isFileURL {
            guard let s = CGImageSourceCreateWithURL(u as CFURL, nil), CGImageSourceGetCount(s) > 0 else { throw RyUIModuleError("no image") }
            return s
        }
        // A remote image: downloaded first (this is a worker thread).
        var data: Data?, failure: Error?
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: u) { d, resp, e in
            if let h = resp as? HTTPURLResponse, !(200..<300).contains(h.statusCode) { failure = RyUIModuleError("HTTP \(h.statusCode)") }
            data = d
            failure = failure ?? e
            done.signal()
        }.resume()
        done.wait()
        if let failure { throw failure }
        guard let data, let s = CGImageSourceCreateWithData(data as CFData, nil), CGImageSourceGetCount(s) > 0 else { throw RyUIModuleError("no image") }
        return s
    }

    /// The size upright: EXIF orientations 5 to 8 turn it a quarter.
    static func uprightSize(_ s: CGImageSource) -> (Int, Int) {
        let p = CGImageSourceCopyPropertiesAtIndex(s, 0, nil) as? [CFString: Any] ?? [:]
        let w = p[kCGImagePropertyPixelWidth] as? Int ?? 0, h = p[kCGImagePropertyPixelHeight] as? Int ?? 0
        let o = p[kCGImagePropertyOrientation] as? Int ?? 1
        return o >= 5 ? (h, w) : (w, h)
    }

    /// Decodes the image upright, no larger than the first action needs
    /// (a resize, first: decoded near its size, which saves memory).
    static func decode(_ s: CGImageSource, for actions: [[String: Any]]) throws -> CGImage {
        let (w, h) = uprightSize(s)
        guard w > 0, h > 0 else { throw RyUIModuleError("no image") }
        var maxSide = max(w, h)
        if let a = actions.first, a["op"] as? String == "resize" {
            let (tw, th) = target(a, w, h)
            maxSide = min(maxSide, max(tw, th))
        }
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxSide,
            kCGImageSourceShouldCacheImmediately: true,
        ]
        guard let img = CGImageSourceCreateThumbnailAtIndex(s, 0, opts as CFDictionary) else { throw RyUIModuleError("no image") }
        return img
    }

    /// A resize's size: a 0 keeps the aspect ratio from the other.
    static func target(_ a: [String: Any], _ w: Int, _ h: Int) -> (Int, Int) {
        var tw = a["width"] as? Int ?? 0, th = a["height"] as? Int ?? 0
        if tw <= 0 && th <= 0 { return (w, h) }
        if tw <= 0 { tw = max(1, Int((Double(w) * Double(th) / Double(h)).rounded())) }
        if th <= 0 { th = max(1, Int((Double(h) * Double(tw) / Double(w)).rounded())) }
        return (tw, th)
    }

    static func context(_ w: Int, _ h: Int, opaque: Bool = false, space: CGColorSpace? = nil) throws -> CGContext {
        let info = opaque ? CGImageAlphaInfo.noneSkipLast.rawValue : CGImageAlphaInfo.premultipliedLast.rawValue
        guard w > 0, h > 0, w * h <= 400_000_000,
              let c = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                                space: space ?? CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: info)
                ?? CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                             space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: info) else {
            throw RyUIModuleError("can't make a \(w)×\(h) image")
        }
        c.interpolationQuality = .high
        return c
    }

    static func apply(_ actions: [[String: Any]], to image: CGImage) throws -> CGImage {
        var img = image
        // Fills and pixelations in a row share one drawing, in the image's
        // own color space: a screenshot in Display P3 stays in it.
        var canvas: CGContext?
        func finish() {
            if let c = canvas, let done = c.makeImage() { img = done }
            canvas = nil
        }
        defer { finish() }
        for a in actions {
            let op = a["op"] as? String
            if op != "fill" && op != "pixelate" { finish() }
            let w = img.width, h = img.height
            switch op {
            case "fill", "pixelate":
                if canvas == nil {
                    let c = try context(w, h, space: img.colorSpace)
                    c.draw(img, in: CGRect(x: 0, y: 0, width: w, height: h))
                    canvas = c
                }
                let r = CGRect(x: a["x"] as? Int ?? 0, y: a["y"] as? Int ?? 0, width: a["width"] as? Int ?? 0, height: a["height"] as? Int ?? 0)
                    .intersection(CGRect(x: 0, y: 0, width: w, height: h))
                if r.isNull || r.width < 1 || r.height < 1 { continue }
                if op == "fill" {
                    let rgb = a["rgb"] as? Int ?? 0
                    canvas!.setShouldAntialias(false)
                    canvas!.setFillColor(CGColor(srgbRed: CGFloat(rgb >> 16 & 255) / 255, green: CGFloat(rgb >> 8 & 255) / 255, blue: CGFloat(rgb & 255) / 255, alpha: 1))
                    canvas!.fill(CGRect(x: r.minX, y: CGFloat(h) - r.maxY, width: r.width, height: r.height)) // y is up
                } else {
                    pixelate(canvas!, r, block: max(a["block"] as? Int ?? 1, 1))
                }
            case "resize":
                let (tw, th) = target(a, w, h)
                if tw == w && th == h { continue }
                let c = try context(tw, th)
                c.draw(img, in: CGRect(x: 0, y: 0, width: tw, height: th))
                img = c.makeImage()!
            case "crop":
                let r = CGRect(x: a["x"] as? Int ?? 0, y: a["y"] as? Int ?? 0, width: a["width"] as? Int ?? 0, height: a["height"] as? Int ?? 0)
                    .intersection(CGRect(x: 0, y: 0, width: w, height: h))
                guard !r.isNull, r.width >= 1, r.height >= 1, let cropped = img.cropping(to: r) else {
                    throw RyUIModuleError("the crop is outside the \(w)×\(h) image")
                }
                img = cropped
            case "rotate":
                let deg = (a["degrees"] as? Double ?? 0).truncatingRemainder(dividingBy: 360)
                if deg == 0 { continue }
                let t = -deg * .pi / 180 // clockwise on screen; Core Graphics' y is up
                let quarter = deg.truncatingRemainder(dividingBy: 90) == 0
                let cosv = abs(cos(t)), sinv = abs(sin(t))
                var nw = Double(w) * cosv + Double(h) * sinv, nh = Double(w) * sinv + Double(h) * cosv
                if quarter { (nw, nh) = (nw.rounded(), nh.rounded()) } else { (nw, nh) = (nw.rounded(.up), nh.rounded(.up)) }
                let c = try context(Int(nw), Int(nh))
                c.translateBy(x: nw / 2, y: nh / 2)
                c.rotate(by: t)
                c.draw(img, in: CGRect(x: -Double(w) / 2, y: -Double(h) / 2, width: Double(w), height: Double(h)))
                img = c.makeImage()!
            case "flipH", "flipV":
                let c = try context(w, h)
                if a["op"] as? String == "flipH" {
                    c.translateBy(x: CGFloat(w), y: 0)
                    c.scaleBy(x: -1, y: 1)
                } else {
                    c.translateBy(x: 0, y: CGFloat(h))
                    c.scaleBy(x: 1, y: -1)
                }
                c.draw(img, in: CGRect(x: 0, y: 0, width: w, height: h))
                img = c.makeImage()!
            default:
                throw RyUIModuleError("unknown action \(a["op"] ?? "")")
            }
        }
        finish()
        return img
    }

    /// Squares of block pixels, aligned to the image, each the average of
    /// all it covers; only those within r change. A bitmap context's
    /// memory is top row first.
    static func pixelate(_ c: CGContext, _ r: CGRect, block: Int) {
        guard let data = c.data?.assumingMemoryBound(to: UInt8.self) else { return }
        let w = c.width, h = c.height, row = c.bytesPerRow
        let rx0 = Int(r.minX), ry0 = Int(r.minY), rx1 = Int(r.maxX), ry1 = Int(r.maxY)
        var by = ry0 / block * block
        while by < ry1 {
            let by1 = min(by + block, h)
            var bx = rx0 / block * block
            while bx < rx1 {
                let bx1 = min(bx + block, w)
                var sum = (0, 0, 0, 0)
                for y in by..<by1 {
                    let p = data + y * row
                    for x in bx..<bx1 {
                        sum.0 += Int(p[x * 4]); sum.1 += Int(p[x * 4 + 1]); sum.2 += Int(p[x * 4 + 2]); sum.3 += Int(p[x * 4 + 3])
                    }
                }
                let n = (by1 - by) * (bx1 - bx)
                let v = (UInt8(sum.0 / n), UInt8(sum.1 / n), UInt8(sum.2 / n), UInt8(sum.3 / n))
                for y in max(by, ry0)..<min(by1, ry1) {
                    let p = data + y * row
                    for x in max(bx, rx0)..<min(bx1, rx1) {
                        p[x * 4] = v.0; p[x * 4 + 1] = v.1; p[x * 4 + 2] = v.2; p[x * 4 + 3] = v.3
                    }
                }
                bx += block
            }
            by += block
        }
    }

    /// Writes the image to the caches: PNG, or JPEG on white (it has no
    /// transparency).
    static func write(_ image: CGImage, _ r: Request) throws -> [String: Any] {
        var img = image
        if !r.png, image.alphaInfo != .none, image.alphaInfo != .noneSkipLast, image.alphaInfo != .noneSkipFirst {
            let c = try context(image.width, image.height, opaque: true)
            c.setFillColor(UIColor.white.cgColor)
            c.fill(CGRect(x: 0, y: 0, width: image.width, height: image.height))
            c.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
            img = c.makeImage()!
        }
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-images", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(UUID().uuidString + (r.png ? ".png" : ".jpg"))
        let type = (r.png ? UTType.png : UTType.jpeg).identifier as CFString
        guard let d = CGImageDestinationCreateWithURL(url as CFURL, type, 1, nil) else { throw RyUIModuleError("can't write \(url.path)") }
        CGImageDestinationAddImage(d, img, r.png ? nil : [kCGImageDestinationLossyCompressionQuality: r.quality] as CFDictionary)
        guard CGImageDestinationFinalize(d) else { throw RyUIModuleError("can't write \(url.path)") }
        return ["path": url.path, "width": img.width, "height": img.height]
    }

    /// The video's frame at seconds (exactly), upright.
    static func frame(_ source: String, at seconds: Double) throws -> CGImage {
        guard let u = url(source) else { throw RyUIModuleError("no image") }
        let g = AVAssetImageGenerator(asset: AVURLAsset(url: u))
        g.appliesPreferredTrackTransform = true
        g.requestedTimeToleranceBefore = .zero
        g.requestedTimeToleranceAfter = .zero
        var out: CGImage?, failure: Error?
        let done = DispatchSemaphore(value: 0)
        g.generateCGImageAsynchronously(for: CMTime(seconds: max(seconds, 0), preferredTimescale: 600)) { img, _, e in
            out = img
            failure = e
            done.signal()
        }
        done.wait()
        guard let out else {
            if let code = (failure as? AVError)?.code, [.fileFormatNotRecognized, .decodeFailed, .noImageAtTime].contains(code) {
                throw RyUIModuleError("no image")
            }
            throw failure ?? RyUIModuleError("no image")
        }
        return out
    }
}
#endif
