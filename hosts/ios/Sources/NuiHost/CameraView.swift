#if canImport(UIKit) && NUI_CAMERAVIEW // an app that calls camera.View (see the CLI's iosFeatures)
import UIKit
import AVFoundation

/// nui/camera's View: an AVCaptureSession's preview, filling the view,
/// and a photo output for camera.Capture. The session runs while the view
/// is in a window.
@MainActor final class NuiCameraView: NSObject, NuiNativeView, AVCapturePhotoCaptureDelegate {
    static weak var current: NuiCameraView?
    let ctx: NuiNativeContext
    let preview = NuiCameraPreview()
    var view: UIView { preview }
    private let session = AVCaptureSession()
    private let output = AVCapturePhotoOutput()
    private var facing: AVCaptureDevice.Position = .back
    private var capturing: ((Result<Data, Error>) -> Void)?
    private let queue = DispatchQueue(label: "nui.camera")

    init(_ ctx: NuiNativeContext) {
        self.ctx = ctx
        super.init()
        preview.preview.session = session
        preview.preview.videoGravity = .resizeAspectFill
        preview.attached = { [weak self] on in
            guard let self else { return }
            if on { NuiCameraView.current = self } else if NuiCameraView.current === self { NuiCameraView.current = nil }
            self.run(on)
        }
    }

    func set(_ name: String, _ value: NuiNativeValue) {
        guard name == "facing" else { return }
        facing = value.string == "front" ? .front : .back
        configure()
    }

    private func configure() {
        AVCaptureDevice.requestAccess(for: .video) { ok in
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                guard ok else { self.ctx.emit("error", ["message": .string("not allowed")]); return }
                guard let device = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: self.facing),
                      let input = try? AVCaptureDeviceInput(device: device) else {
                    self.ctx.emit("error", ["message": .string("no camera")]); return
                }
                self.session.beginConfiguration()
                self.session.sessionPreset = .photo
                for i in self.session.inputs { self.session.removeInput(i) }
                if self.session.canAddInput(input) { self.session.addInput(input) }
                if self.session.outputs.isEmpty, self.session.canAddOutput(self.output) { self.session.addOutput(self.output) }
                self.session.commitConfiguration()
                self.run(self.preview.window != nil)
            }
        }
    }

    private func run(_ on: Bool) {
        let s = session
        queue.async { // startRunning blocks: off the main thread
            if on, !s.isRunning, !s.inputs.isEmpty { s.startRunning() } else if !on, s.isRunning { s.stopRunning() }
        }
    }

    func capture(_ reply: @escaping (Result<Data, Error>) -> Void) {
        guard session.isRunning else { reply(.failure(NuiModuleError("the camera isn't showing"))); return }
        capturing?(.failure(NuiModuleError("another photo was taken")))
        capturing = reply
        output.capturePhoto(with: AVCapturePhotoSettings(), delegate: self)
    }

    nonisolated func photoOutput(_ o: AVCapturePhotoOutput, didFinishProcessingPhoto photo: AVCapturePhoto, error: Error?) {
        let data = photo.fileDataRepresentation()
        MainActor.assumeIsolated {
            let reply = capturing
            capturing = nil
            guard let data, error == nil else { reply?(.failure(error ?? NuiModuleError("no photo"))); return }
            let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("nui-camera", isDirectory: true)
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let f = dir.appendingPathComponent(UUID().uuidString + ".jpg")
            if (try? data.write(to: f)) != nil { reply?(.success(Data(f.path.utf8))) } else { reply?(.failure(NuiModuleError("couldn't save the photo"))) }
        }
    }
}

/// The preview's view: its layer an AVCaptureVideoPreviewLayer.
final class NuiCameraPreview: UIView {
    override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
    var preview: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    var attached: ((Bool) -> Void)?

    override func didMoveToWindow() {
        super.didMoveToWindow()
        attached?(window != nil)
    }
}
#endif
