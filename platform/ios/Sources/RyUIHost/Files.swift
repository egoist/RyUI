#if canImport(UIKit) && RYUI_FILES // an app that calls device.PickFiles (see the CLI's iosFeatures)
import UIKit
import UniformTypeIdentifiers

/// device.PickFiles' module: "pick" shows the document picker. args: u8
/// multiple, then MIME types a line each (none: any file); the reply is
/// a JSON array of the copies: path, name, type, size (none: cancelled).
@MainActor enum RyUIFiles {
    static func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        [
            "pick": { [weak rt] args, reply in
                guard let top = rt?.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
                let multiple = args.first == 1
                let mimes = String(decoding: args.dropFirst(), as: UTF8.self).split(separator: "\n").map(String.init)
                var types = mimes.compactMap(utType)
                if types.isEmpty { types = [.item] }
                let p = UIDocumentPickerViewController(forOpeningContentTypes: types, asCopy: true)
                p.allowsMultipleSelection = multiple
                let d = RyUIFilePicker(reply)
                p.delegate = d
                objc_setAssociatedObject(p, &RyUIFilePicker.key, d, .OBJC_ASSOCIATION_RETAIN)
                top.present(p, animated: true)
            },
        ]
    }

    /// A MIME type's UTType; "image/*" and the like are the family's.
    static func utType(_ mime: String) -> UTType? {
        switch mime {
        case "*/*", "": return .item
        case "image/*": return .image
        case "video/*": return .movie
        case "audio/*": return .audio
        case "text/*": return .text
        case "application/*": return .data
        default: return UTType(mimeType: mime)
        }
    }
}

/// Moves the picked copies where the app keeps them, each in a directory
/// of its own (so its name stays its own), and answers.
final class RyUIFilePicker: NSObject, UIDocumentPickerDelegate {
    nonisolated(unsafe) static var key = 0
    let reply: (Result<Data, Error>) -> Void
    init(_ r: @escaping (Result<Data, Error>) -> Void) { reply = r }

    func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        let fm = FileManager.default
        let dir = fm.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-files", isDirectory: true)
        var out: [[String: Any]] = []
        for u in urls {
            let into = dir.appendingPathComponent(UUID().uuidString, isDirectory: true)
            let to = into.appendingPathComponent(u.lastPathComponent)
            do {
                try fm.createDirectory(at: into, withIntermediateDirectories: true)
                try fm.moveItem(at: u, to: to)
            } catch { continue }
            let size = (try? fm.attributesOfItem(atPath: to.path)[.size] as? NSNumber)?.int64Value ?? 0
            let type = UTType(filenameExtension: to.pathExtension)?.preferredMIMEType ?? ""
            out.append(["path": to.path, "name": u.lastPathComponent, "type": type, "size": size])
        }
        reply(.success((try? JSONSerialization.data(withJSONObject: out)) ?? Data("[]".utf8)))
    }

    func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
        reply(.success(Data()))
    }
}
#endif
