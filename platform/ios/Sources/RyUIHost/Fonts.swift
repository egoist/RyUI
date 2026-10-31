#if canImport(UIKit)
import UIKit
import CoreText
#if canImport(RyUIOps)
import RyUIOps
#endif

/// The app's fonts: the files in its assets/fonts, registered the first
/// time a font is asked for, and (under ryui dev) fonts by URL. Any
/// thread: rich text lays out off the main one.
enum RyUIFonts {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var files: [String: String]? // a file's name (no extension) → its PostScript name
    nonisolated(unsafe) private static var urls: [String: String] = [:] // a URL → the PostScript name of the font there
    nonisolated(unsafe) private static var loading = Set<String>()
    /// Runs on the main thread when a font by URL has loaded (its URL).
    @MainActor static var loaded: ((String) -> Void)?

    /// A font of family at size: an asset file's (Inter-Bold), a loaded
    /// URL's, or a family's (an asset family, Inter, or the system's,
    /// Georgia) in weight. Nil: none such (or not loaded yet).
    static func font(_ family: String, size: CGFloat, weight: UIFont.Weight?) -> UIFont? {
        if family.contains("://") {
            if let ps = lock.withLock({ urls[family] }) { return UIFont(name: ps, size: size) }
            load(family)
            return nil
        }
        if let ps = assetFiles()[family] { return UIFont(name: ps, size: size) }
        // The generic families, as Android names them: the system font's designs.
        let designs: [String: UIFontDescriptor.SystemDesign] = ["serif": .serif, "monospace": .monospaced, "rounded": .rounded, "sans-serif": .default]
        if let design = designs[family],
           let d = UIFont.systemFont(ofSize: size, weight: weight ?? .regular).fontDescriptor.withDesign(design) {
            return UIFont(descriptor: d, size: size)
        }
        guard UIFont.familyNames.contains(family) else { return nil }
        var d = UIFontDescriptor(fontAttributes: [.family: family])
        if let weight { d = d.addingAttributes([.traits: [UIFontDescriptor.TraitKey.weight: weight]]) }
        return UIFont(descriptor: d, size: size)
    }

    private static func assetFiles() -> [String: String] {
        lock.lock()
        defer { lock.unlock() }
        if let files { return files }
        var out: [String: String] = [:]
        if let dir = Bundle.main.resourceURL?.appendingPathComponent("assets/fonts"),
           let list = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil) {
            for url in list where ["ttf", "otf"].contains(url.pathExtension.lowercased()) {
                if let d = try? Data(contentsOf: url), let ps = register(d) {
                    out[url.deletingPathExtension().lastPathComponent] = ps
                }
            }
        }
        files = out
        return out
    }

    /// Registers a font file's data for this process; its PostScript name.
    private static func register(_ data: Data) -> String? {
        guard let p = CGDataProvider(data: data as CFData), let f = CGFont(p), let ps = f.postScriptName as String? else { return nil }
        var err: Unmanaged<CFError>?
        if !CTFontManagerRegisterGraphicsFont(f, &err), let e = err?.takeRetainedValue(),
           CFErrorGetCode(e) != CTFontManagerError.alreadyRegistered.rawValue {
            return nil
        }
        return ps
    }

    private static func load(_ url: String) {
        guard let u = URL(string: url), lock.withLock({ loading.insert(url).inserted }) else { return }
        URLSession.shared.dataTask(with: u) { data, resp, _ in
            let ok = (resp as? HTTPURLResponse).map { $0.statusCode == 200 } ?? false
            // A failed load stays loading: asked again, it isn't fetched again.
            guard ok, let data, let ps = register(data) else { return }
            lock.withLock { urls[url] = ps }
            DispatchQueue.main.async { MainActor.assumeIsolated { loaded?(url) } }
        }.resume()
    }
}
#endif
