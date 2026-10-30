#if canImport(UIKit) && NUI_LIBRARY // an app that calls nui/library (see the CLI's iosFeatures)
import Foundation
import Photos

/// nui/library's module: "save" (args: JSON path, collection 0 downloads,
/// 1 movies, 2 music, 3 pictures, name, folder, mime, move). Movies and
/// pictures go to Photos (after asking to add to it), answering with the
/// asset's identifier; the rest to the app's Documents (the Files app
/// shows them), under folder, answering with the file's path.
enum NuiLibrary {
    @MainActor static func methods() -> [String: NuiModuleMethod] {
        ["save": { args, reply in
            guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any], let path = o["path"] as? String else {
                reply(.failure(NuiModuleError("bad arguments"))); return
            }
            let src = URL(fileURLWithPath: path)
            let collection = o["collection"] as? Int ?? 0
            let name = (o["name"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? src.lastPathComponent
            let folder = o["folder"] as? String ?? ""
            let move = o["move"] as? Bool ?? false
            guard FileManager.default.fileExists(atPath: path) else { reply(.failure(NuiModuleError("no file at \(path)"))); return }
            if collection == 1 || collection == 3 {
                PHPhotoLibrary.requestAuthorization(for: .addOnly) { status in
                    guard status == .authorized || status == .limited else {
                        DispatchQueue.main.async { reply(.failure(NuiModuleError("not allowed"))) }
                        return
                    }
                    var id: String?
                    PHPhotoLibrary.shared().performChanges({
                        let r = PHAssetCreationRequest.forAsset()
                        let opts = PHAssetResourceCreationOptions()
                        opts.originalFilename = name
                        opts.shouldMoveFile = move
                        r.addResource(with: collection == 1 ? .video : .photo, fileURL: src, options: opts)
                        id = r.placeholderForCreatedAsset?.localIdentifier
                    }) { ok, err in
                        DispatchQueue.main.async {
                            reply(ok ? .success(Data((id ?? "").utf8)) : .failure(err ?? NuiModuleError("couldn't save")))
                        }
                    }
                }
                return
            }
            DispatchQueue.global(qos: .utility).async {
                let r: Result<Data, Error> = Result {
                    let fm = FileManager.default
                    var dir = fm.urls(for: .documentDirectory, in: .userDomainMask)[0]
                    if !folder.isEmpty { dir.appendPathComponent(folder, isDirectory: true) }
                    try fm.createDirectory(at: dir, withIntermediateDirectories: true)
                    let base = (name as NSString).deletingPathExtension, ext = (name as NSString).pathExtension
                    var dst = dir.appendingPathComponent(name)
                    var i = 1
                    while fm.fileExists(atPath: dst.path) {
                        dst = dir.appendingPathComponent("\(base) (\(i))" + (ext.isEmpty ? "" : ".\(ext)"))
                        i += 1
                    }
                    if move { try fm.moveItem(at: src, to: dst) } else { try fm.copyItem(at: src, to: dst) }
                    return Data(dst.path.utf8)
                }
                DispatchQueue.main.async { reply(r) }
            }
        }]
    }
}
#endif
