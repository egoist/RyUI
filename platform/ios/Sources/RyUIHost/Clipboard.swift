#if canImport(UIKit)
import UIKit
import UniformTypeIdentifiers

/// ryui/device's clipboard module: "has" (u8 text, image, URL: no paste
/// prompt), "image" (the clipboard's image as a file in the caches, in its
/// own format; empty: none), "copyImage" (a file's path), and, while
/// watching, "next" (answered at the next change: the app's own, or, as
/// it comes back, another app's) and "stop".
@MainActor final class RyUIClipboard: NSObject {
    static let shared = RyUIClipboard()
    private var waiting: [(Result<Data, Error>) -> Void] = []
    private var observing = false
    private var seen = UIPasteboard.general.changeCount

    func methods() -> [String: RyUIModuleMethod] {
        let pb = UIPasteboard.general
        return [
            "has": { _, reply in
                reply(.success(Data([pb.hasStrings ? 1 : 0, pb.hasImages ? 1 : 0, pb.hasURLs ? 1 : 0])))
            },
            "image": { _, reply in
                // The image as it was copied (its bytes, not a re-encoding),
                // in the first image type the clipboard has.
                let types = pb.types.compactMap { UTType($0) }.filter { $0.conforms(to: .image) }
                var data: Data?
                var ext = "png"
                for t in types {
                    if let d = pb.data(forPasteboardType: t.identifier) {
                        data = d
                        ext = t.preferredFilenameExtension ?? "png"
                        break
                    }
                }
                if data == nil, let img = pb.image { data = img.pngData() }
                guard let data else { reply(.success(Data())); return }
                let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-clipboard", isDirectory: true)
                try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                let f = dir.appendingPathComponent(UUID().uuidString + "." + ext)
                do {
                    try data.write(to: f)
                    reply(.success(Data(f.path.utf8)))
                } catch {
                    reply(.failure(error))
                }
            },
            "copyImage": { args, reply in
                let url = URL(fileURLWithPath: String(decoding: args, as: UTF8.self))
                guard let data = try? Data(contentsOf: url) else { reply(.failure(RyUIModuleError("no file at \(url.path)"))); return }
                guard let t = UTType(filenameExtension: url.pathExtension), t.conforms(to: .image) else {
                    // An extension that says nothing: as an image UIKit reads.
                    guard let img = UIImage(data: data) else { reply(.failure(RyUIModuleError("not an image"))); return }
                    pb.image = img
                    reply(.success(Data()))
                    return
                }
                pb.setData(data, forPasteboardType: t.identifier)
                reply(.success(Data()))
            },
            "next": { [unowned self] _, reply in
                self.observe()
                self.waiting.append(reply)
            },
            "stop": { [unowned self] _, reply in
                let ws = self.waiting
                self.waiting = []
                for w in ws { w(.failure(RyUIModuleError("stopped"))) }
                reply(.success(Data()))
            },
        ]
    }

    private func observe() {
        guard !observing else { return }
        observing = true
        seen = UIPasteboard.general.changeCount
        let nc = NotificationCenter.default
        nc.addObserver(self, selector: #selector(check), name: UIPasteboard.changedNotification, object: nil)
        nc.addObserver(self, selector: #selector(check), name: UIApplication.didBecomeActiveNotification, object: nil)
    }

    /// The change count moved (iOS counts every change, any app's): a change.
    @objc private func check() {
        let n = UIPasteboard.general.changeCount
        guard n != seen else { return }
        seen = n
        let ws = waiting
        waiting = []
        for w in ws { w(.success(Data())) }
    }
}
#endif
