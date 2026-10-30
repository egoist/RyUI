#if canImport(UIKit) && NUI_CAMERAVIEW // an app that calls camera.View (see the CLI's iosFeatures)
import UIKit
import AVFoundation

/// nui/camera's View: an AVCaptureSession's preview, filling the view,
/// a photo output for camera.Capture and, with scan on, a metadata output
/// that reads codes (nui/scan's Codes). The session runs while the view is
/// in a window.
@MainActor final class NuiCameraView: NSObject, NuiNativeView, AVCapturePhotoCaptureDelegate, AVCaptureMetadataOutputObjectsDelegate {
    static weak var current: NuiCameraView?
    let ctx: NuiNativeContext
    let preview = NuiCameraPreview()
    var view: UIView { preview }
    private let session = AVCaptureSession()
    private let output = AVCapturePhotoOutput()
    private let metadata = AVCaptureMetadataOutput()
    private var facing: AVCaptureDevice.Position = .back
    private var scan = false
    private var kinds = NuiCodeKinds("")
    private var seen = NuiCodeSeen()
    private var configuring = false
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
        switch name {
        case "facing": facing = value.string == "front" ? .front : .back
        case "scan": scan = value.bool
        case "codes": kinds = NuiCodeKinds(value.string)
        default: return
        }
        // Once for the props set together.
        guard !configuring else { return }
        configuring = true
        DispatchQueue.main.async { [weak self] in
            self?.configuring = false
            self?.configure()
        }
    }

    static let metadataTypes: [(String, [AVMetadataObject.ObjectType])] = [
        ("qr", [.qr]), ("aztec", [.aztec]), ("datamatrix", [.dataMatrix]), ("pdf417", [.pdf417]),
        ("ean13", [.ean13]), ("ean8", [.ean8]), ("upca", [.ean13]), ("upce", [.upce]),
        ("code128", [.code128]), ("code39", [.code39]), ("code93", [.code93]), ("codabar", [.codabar]),
        ("itf", [.itf14, .interleaved2of5]),
    ]

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
                if !self.session.outputs.contains(self.output), self.session.canAddOutput(self.output) { self.session.addOutput(self.output) }
                if self.scan {
                    if !self.session.outputs.contains(self.metadata), self.session.canAddOutput(self.metadata) {
                        self.session.addOutput(self.metadata)
                        self.metadata.setMetadataObjectsDelegate(self, queue: .main)
                    }
                    // What it can read is known once it's in the session.
                    let available = self.metadata.availableMetadataObjectTypes
                    var types: [AVMetadataObject.ObjectType] = []
                    for (k, ts) in Self.metadataTypes where self.kinds.wants(k) {
                        for t in ts where available.contains(t) && !types.contains(t) { types.append(t) }
                    }
                    self.metadata.metadataObjectTypes = types
                } else if self.session.outputs.contains(self.metadata) {
                    self.session.removeOutput(self.metadata)
                }
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

    nonisolated func metadataOutput(_ o: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from c: AVCaptureConnection) {
        let found = objects.compactMap { o -> (AVMetadataObject.ObjectType, String)? in
            guard let m = o as? AVMetadataMachineReadableCodeObject, let v = m.stringValue else { return nil }
            return (m.type, v)
        }
        MainActor.assumeIsolated { // its queue is the main one
            let now = CACurrentMediaTime()
            for (t, v) in found {
                guard let k = Self.metadataTypes.first(where: { $0.0 != "upca" && $0.1.contains(t) })?.0 else { continue }
                let (kind, value) = kinds.named(k, v)
                guard kinds.wants(kind), seen.isNew(kind, value, now: now) else { continue }
                ctx.emit("code", ["kind": .string(kind), "value": .string(value)])
            }
        }
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
