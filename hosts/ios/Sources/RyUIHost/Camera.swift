#if canImport(UIKit)
import UIKit

/// ryui/camera's module: "take" shows the system's camera (a
/// UIImagePickerController) and answers with the photo's file, a JPEG in
/// the caches ("" if cancelled), or fails "no camera".
@MainActor final class RyUICamera: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
    static let shared = RyUICamera()
    private var reply: ((Result<Data, Error>) -> Void)?

    func methods() -> [String: RyUIModuleMethod] {
        ["capture": { _, reply in
            #if RYUI_CAMERAVIEW
            guard let v = RyUICameraView.current else { reply(.failure(RyUIModuleError("no camera view"))); return }
            v.capture(reply)
            #else
            reply(.failure(RyUIModuleError("no camera view")))
            #endif
        },
         "record": { args, reply in
            #if RYUI_CAMERAVIEW && RYUI_RECORD
            guard let v = RyUICameraView.current else { reply(.failure(RyUIModuleError("no camera view"))); return }
            let a = [UInt8](args)
            guard a.count >= 5 else { reply(.failure(RyUIModuleError("bad arguments"))); return }
            let ms = UInt32(a[1]) | UInt32(a[2]) << 8 | UInt32(a[3]) << 16 | UInt32(a[4]) << 24
            v.record(mute: a[0] == 1, max: Double(ms) / 1000, reply)
            #else
            reply(.failure(RyUIModuleError("no camera view")))
            #endif
        },
         "stopRecording": { _, reply in
            #if RYUI_CAMERAVIEW && RYUI_RECORD
            RyUICameraView.current?.stopRecording()
            #endif
            reply(.success(Data()))
        },
         "take": { [unowned self] _, reply in
            guard UIImagePickerController.isSourceTypeAvailable(.camera) else {
                reply(.failure(RyUIModuleError("no camera"))); return
            }
            guard let top = RyUIRuntime.shared.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
            self.reply?(.success(Data())) // one at a time
            self.reply = reply
            let p = UIImagePickerController()
            p.sourceType = .camera
            p.delegate = self
            top.present(p, animated: true)
        }]
    }

    nonisolated func imagePickerController(_ p: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
        let image = info[.originalImage] as? UIImage
        MainActor.assumeIsolated {
            p.dismiss(animated: true)
            let reply = self.reply
            self.reply = nil
            guard let image else { reply?(.success(Data())); return }
            DispatchQueue.global(qos: .userInitiated).async {
                let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-camera", isDirectory: true)
                try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                let f = dir.appendingPathComponent(UUID().uuidString + ".jpg")
                let ok = (try? image.jpegData(compressionQuality: 0.9)?.write(to: f)) != nil
                DispatchQueue.main.async { reply?(ok ? .success(Data(f.path.utf8)) : .failure(RyUIModuleError("couldn't save the photo"))) }
            }
        }
    }

    nonisolated func imagePickerControllerDidCancel(_ p: UIImagePickerController) {
        MainActor.assumeIsolated {
            p.dismiss(animated: true)
            reply?(.success(Data()))
            reply = nil
        }
    }
}
#endif
