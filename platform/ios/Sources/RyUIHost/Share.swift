#if canImport(UIKit)
import UIKit
import UniformTypeIdentifiers
#if canImport(RyUIOps)
import RyUIOps
#endif

/// Sharing (expo-sharing's): the "share" module's "files", the share
/// sheet for files (ui.ShareFiles); and what the share extension hands
/// the app (ui.OnShare).
enum RyUIShare {
    /// The Shared event's payload for what the share extension put in
    /// its URL (platform/ios/Share): the items as JSON, whose files are
    /// in the App Group's container (Info.plist's RyUIAppGroup), moved
    /// to the app's caches, under ryui-shared/.
    static func received(_ query: String) -> [UInt8] {
        var items: [[String: Any]] = []
        if let d = query.data(using: .utf8), let a = (try? JSONSerialization.jsonObject(with: d)) as? [[String: Any]] {
            items = a
        } else if !query.isEmpty {
            items = [["k": 0, "t": query]]
        }
        let fm = FileManager.default
        let container = (Bundle.main.object(forInfoDictionaryKey: "RyUIAppGroup") as? String)
            .flatMap { fm.containerURL(forSecurityApplicationGroupIdentifier: $0) }
        let caches = fm.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-shared", isDirectory: true)
        var w = RyUIWriter(), n: UInt32 = 0
        var body = RyUIWriter()
        for it in items {
            let kind = (it["k"] as? NSNumber)?.intValue ?? 0
            var path = ""
            if kind >= Int(RyUIShareKind.image.rawValue) {
                guard let container, let rel = it["p"] as? String, !rel.isEmpty, !rel.contains("..") else { continue }
                let src = container.appendingPathComponent(rel)
                let dir = caches.appendingPathComponent(UUID().uuidString, isDirectory: true)
                let dst = dir.appendingPathComponent(src.lastPathComponent)
                do {
                    try fm.createDirectory(at: dir, withIntermediateDirectories: true)
                    try fm.moveItem(at: src, to: dst)
                } catch {
                    continue
                }
                try? fm.removeItem(at: src.deletingLastPathComponent())
                path = dst.path
            }
            body.u8(UInt8(clamping: kind))
            body.string(it["t"] as? String ?? "")
            body.string(path)
            body.string(it["n"] as? String ?? "")
            body.string(it["m"] as? String ?? "text/plain")
            body.u64(UInt64(bitPattern: (it["s"] as? NSNumber)?.int64Value ?? -1))
            n += 1
        }
        w.u32(n)
        return w.bytes + body.bytes
    }

    /// args: u32 view the popover points at (0: none), string MIME,
    /// string UTI, string title, u32 count, then the paths; answers once
    /// the sheet is gone.
    @MainActor static func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        ["files": { [weak rt] args, reply in
            guard args.count >= 20 else { return reply(.failure(RyUIModuleError("bad arguments"))) }
            let (from, mime, uti, title, paths) = args.withUnsafeBytes { b -> (UInt32, String, String, String, [String]) in
                var r = RyUIReader(b)
                let from = r.u32(), mime = r.string(), uti = r.string(), title = r.string()
                let n = Int(r.u32())
                var paths: [String] = []
                for _ in 0..<min(n, 10_000) where r.end - r.p >= 4 { paths.append(r.string()) }
                return (from, mime, uti, title, paths)
            }
            guard let rt, let top = rt.topController else { return reply(.failure(RyUIModuleError("no screen"))) }
            // The type: the UTI, else the MIME type's.
            let type = UTType(uti).flatMap { $0.isDynamic ? nil : $0 } ?? (mime.isEmpty ? nil : UTType(mimeType: mime))
            var items: [Any] = []
            for p in paths {
                guard FileManager.default.fileExists(atPath: p) else { return reply(.failure(RyUIModuleError("no file at \(p)"))) }
                items.append(FileItem(typed(URL(fileURLWithPath: p), type), type: type, subject: title))
            }
            let vc = UIActivityViewController(activityItems: items, applicationActivities: nil)
            vc.completionWithItemsHandler = { _, _, _, _ in reply(.success(Data())) }
            if let pop = vc.popoverPresentationController { // iPad
                if from != 0, let v = rt.nodes[from]?.view, v.window != nil {
                    pop.sourceView = v
                    pop.sourceRect = v.bounds
                } else {
                    pop.sourceView = top.view
                    pop.sourceRect = CGRect(x: top.view.bounds.midX, y: top.view.bounds.midY, width: 0, height: 0)
                    pop.permittedArrowDirections = []
                }
            }
            top.present(vc, animated: true)
        }]
    }

    /// The file under a name its type goes with: as it is if its
    /// extension is the type's (or there's no type), else a link to it in
    /// the temporary folder with the type's extension added (as
    /// expo-sharing: "image.pdf" shared as a JPEG is "image.pdf.jpeg"),
    /// since apps on the sheet go by the name.
    private static func typed(_ url: URL, _ type: UTType?) -> URL {
        guard let type, let ext = type.preferredFilenameExtension,
              !(UTType(filenameExtension: url.pathExtension)?.conforms(to: type) ?? false) else { return url }
        let fm = FileManager.default
        let dir = fm.temporaryDirectory.appendingPathComponent("ryui-share/" + UUID().uuidString, isDirectory: true)
        let dst = dir.appendingPathComponent(url.lastPathComponent + "." + ext)
        do {
            try fm.createDirectory(at: dir, withIntermediateDirectories: true)
            if (try? fm.linkItem(at: url, to: dst)) == nil { try fm.copyItem(at: url, to: dst) }
            return dst
        } catch {
            return url
        }
    }

    /// A file on the sheet, with its type (for the activities that ask)
    /// and the title as a mail's subject.
    final class FileItem: NSObject, UIActivityItemSource {
        let url: URL, type: UTType?, subject: String
        init(_ url: URL, type: UTType?, subject: String) {
            self.url = url
            self.type = type
            self.subject = subject
        }
        func activityViewControllerPlaceholderItem(_ c: UIActivityViewController) -> Any { url }
        func activityViewController(_ c: UIActivityViewController, itemForActivityType t: UIActivity.ActivityType?) -> Any? { url }
        func activityViewController(_ c: UIActivityViewController, dataTypeIdentifierForActivityType t: UIActivity.ActivityType?) -> String {
            type?.identifier ?? UTType(filenameExtension: url.pathExtension)?.identifier ?? UTType.data.identifier
        }
        func activityViewController(_ c: UIActivityViewController, subjectForActivityType t: UIActivity.ActivityType?) -> String { subject }
    }
}
#endif
