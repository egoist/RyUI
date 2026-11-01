#if canImport(UIKit) && RYUI_PHOTOLIBRARY // an app that calls ryui/photos (see the CLI's iosFeatures)
import CryptoKit
import Photos
import PhotosUI
import UIKit

/// ryui/photos' module, on PhotoKit: "current" and "request" answer what's
/// allowed, "manage" shows the limited library picker, "list" answers JSON
/// (id, width, height, created in ms, screenshot), "thumbnail" a JPEG's path
/// and "export" JSON (path, name). Off the main thread where they wait.
enum RyUIPhotos {
    static func access(_ s: PHAuthorizationStatus) -> String {
        switch s {
        case .authorized: return "allowed"
        case .limited: return "limited"
        case .denied, .restricted: return "denied"
        default: return "notAsked"
        }
    }

    static func cacheDir(_ name: String) throws -> URL {
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent(name, isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    static func asset(_ id: String) throws -> PHAsset {
        guard let a = PHAsset.fetchAssets(withLocalIdentifiers: [id], options: nil).firstObject else { throw RyUIModuleError("no such photo") }
        return a
    }

    static func hash(_ s: String) -> String {
        SHA256.hash(data: Data(s.utf8)).prefix(16).map { String(format: "%02x", $0) }.joined()
    }

    @MainActor static func methods() -> [String: RyUIModuleMethod] {
        [
            "current": { _, reply in
                reply(.success(Data(access(PHPhotoLibrary.authorizationStatus(for: .readWrite)).utf8)))
            },
            "request": { _, reply in
                PHPhotoLibrary.requestAuthorization(for: .readWrite) { s in
                    reply(.success(Data(access(s).utf8)))
                }
            },
            "manage": { _, reply in
                guard let vc = RyUIRuntime.shared.topController else {
                    reply(.failure(RyUIModuleError("no window")))
                    return
                }
                PHPhotoLibrary.shared().presentLimitedLibraryPicker(from: vc) { _ in reply(.success(Data())) }
            },
            "list": { args, reply in
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                let screenshots = o["screenshots"] as? Bool ?? false
                let offset = o["offset"] as? Int ?? 0, limit = o["limit"] as? Int ?? 100
                DispatchQueue.global(qos: .userInitiated).async {
                    let opts = PHFetchOptions()
                    opts.sortDescriptors = [NSSortDescriptor(key: "creationDate", ascending: false)]
                    if screenshots {
                        opts.predicate = NSPredicate(format: "(mediaSubtype & %d) != 0", PHAssetMediaSubtype.photoScreenshot.rawValue)
                    }
                    let found = PHAsset.fetchAssets(with: .image, options: opts)
                    var out: [[String: Any]] = []
                    if offset < found.count {
                        for i in offset..<min(offset + limit, found.count) {
                            let a = found.object(at: i)
                            out.append([
                                "id": a.localIdentifier, "width": a.pixelWidth, "height": a.pixelHeight,
                                "created": Int64((a.creationDate?.timeIntervalSince1970 ?? 0) * 1000),
                                "screenshot": a.mediaSubtypes.contains(.photoScreenshot),
                            ])
                        }
                    }
                    reply(Result { try JSONSerialization.data(withJSONObject: out) })
                }
            },
            "thumbnail": { args, reply in
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                let size = o["size"] as? Int ?? 300
                DispatchQueue.global(qos: .userInitiated).async {
                    do {
                        let a = try asset(o["id"] as? String ?? "")
                        let stamp = a.modificationDate?.timeIntervalSince1970 ?? 0
                        let url = try cacheDir("ryui-photo-thumbs").appendingPathComponent("\(hash(a.localIdentifier + "\(stamp)"))-\(size).jpg")
                        if FileManager.default.fileExists(atPath: url.path) {
                            reply(.success(Data(url.path.utf8)))
                            return
                        }
                        let po = PHImageRequestOptions()
                        po.deliveryMode = .highQualityFormat
                        po.resizeMode = .fast
                        po.isNetworkAccessAllowed = true
                        po.isSynchronous = true
                        var image: UIImage?
                        PHImageManager.default().requestImage(for: a, targetSize: CGSize(width: size, height: size), contentMode: .aspectFill, options: po) { img, _ in image = img }
                        guard let data = image?.jpegData(compressionQuality: 0.8) else { throw RyUIModuleError("no thumbnail") }
                        try data.write(to: url, options: .atomic)
                        reply(.success(Data(url.path.utf8)))
                    } catch {
                        reply(.failure(error))
                    }
                }
            },
            "export": { args, reply in
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                DispatchQueue.global(qos: .userInitiated).async {
                    do {
                        let a = try asset(o["id"] as? String ?? "")
                        let rs = PHAssetResource.assetResources(for: a)
                        // The photo as edited, if it was.
                        guard let r = rs.first(where: { $0.type == .fullSizePhoto }) ?? rs.first(where: { $0.type == .photo }) ?? rs.first else {
                            throw RyUIModuleError("no such photo")
                        }
                        let original = rs.first(where: { $0.type == .photo })?.originalFilename ?? r.originalFilename
                        let ext = (r.originalFilename as NSString).pathExtension
                        let url = try cacheDir("ryui-photo-exports").appendingPathComponent("\(UUID().uuidString).\(ext.isEmpty ? "jpg" : ext)")
                        let wo = PHAssetResourceRequestOptions()
                        wo.isNetworkAccessAllowed = true
                        PHAssetResourceManager.default().writeData(for: r, toFile: url, options: wo) { error in
                            if let error {
                                reply(.failure(error))
                                return
                            }
                            reply(Result { try JSONSerialization.data(withJSONObject: ["path": url.path, "name": original]) })
                        }
                    } catch {
                        reply(.failure(error))
                    }
                }
            },
        ]
    }
}
#endif
