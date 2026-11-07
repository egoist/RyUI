#if canImport(UIKit) && (RYUI_PREVIEW || RYUI_PRINT || RYUI_COMPOSE)
import UIKit
import UniformTypeIdentifiers

/// The files ryui/preview, ryui/print and ryui/compose take: a path, an
/// asset's path (in the app's assets), or an http(s) URL, downloaded to
/// the caches (ryui-files/) first, under its name, with the extension of
/// the type the server gives if it has none.
enum RyUILocalFile {
    /// The local file for source, on the main thread.
    @MainActor static func resolve(_ source: String, type: String = "", _ done: @escaping (Result<URL, Error>) -> Void) {
        if source.hasPrefix("http://") || source.hasPrefix("https://"), let url = URL(string: source) {
            download(url, type, done)
            return
        }
        var path = source
        if path.hasPrefix("file://") { path = URL(string: path)?.path ?? path }
        if !path.hasPrefix("/"), let assets = Bundle.main.resourceURL?.appendingPathComponent("assets") {
            path = assets.appendingPathComponent(path).path
        }
        guard FileManager.default.fileExists(atPath: path) else { return done(.failure(RyUIModuleError("no file at \(source)"))) }
        done(.success(URL(fileURLWithPath: path)))
    }

    /// The local files for sources, in their order; the first failure fails them all.
    @MainActor static func resolve(_ sources: [(source: String, type: String)], _ done: @escaping (Result<[URL], Error>) -> Void) {
        var urls = [URL?](repeating: nil, count: sources.count)
        var left = sources.count, failed = false
        if left == 0 { return done(.success([])) }
        for (i, s) in sources.enumerated() {
            resolve(s.source, type: s.type) { r in
                guard !failed else { return }
                switch r {
                case .success(let u):
                    urls[i] = u
                    left -= 1
                    if left == 0 { done(.success(urls.compactMap { $0 })) }
                case .failure(let e):
                    failed = true
                    done(.failure(e))
                }
            }
        }
    }

    private static func download(_ url: URL, _ type: String, _ done: @escaping (Result<URL, Error>) -> Void) {
        URLSession.shared.downloadTask(with: url) { tmp, response, error in
            let r = Result<URL, Error> {
                guard let tmp else { throw error ?? RyUIModuleError("couldn't download \(url)") }
                if let h = response as? HTTPURLResponse, !(200..<300).contains(h.statusCode) {
                    throw RyUIModuleError("couldn't download \(url): HTTP \(h.statusCode)")
                }
                var name = response?.suggestedFilename ?? url.lastPathComponent
                if name.isEmpty || name == "/" { name = "file" }
                let mime = type.isEmpty ? (response?.mimeType ?? "") : type
                if (name as NSString).pathExtension.isEmpty, let ext = UTType(mimeType: mime)?.preferredFilenameExtension {
                    name += "." + ext
                }
                let fm = FileManager.default
                let dir = fm.urls(for: .cachesDirectory, in: .userDomainMask)[0]
                    .appendingPathComponent("ryui-files/" + UUID().uuidString, isDirectory: true)
                try fm.createDirectory(at: dir, withIntermediateDirectories: true)
                let dst = dir.appendingPathComponent(name.replacingOccurrences(of: "/", with: "_"))
                try fm.moveItem(at: tmp, to: dst)
                return dst
            }
            DispatchQueue.main.async { done(r) }
        }.resume()
    }

    /// The app's name, as the home screen has it.
    static var appName: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String
            ?? Bundle.main.object(forInfoDictionaryKey: "CFBundleName") as? String ?? "App"
    }

    /// Where a popover from the view with node id from points: the view,
    /// else the top screen's middle.
    @MainActor static func anchor(_ rt: RyUIRuntime, _ from: UInt32, _ top: UIViewController) -> (UIView, CGRect, Bool) {
        if from != 0, let v = rt.nodes[from]?.view, v.window != nil { return (v, v.bounds, true) }
        let b = top.view.bounds
        return (top.view, CGRect(x: b.midX, y: b.midY, width: 0, height: 0), false)
    }
}
#endif
