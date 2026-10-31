#if canImport(UIKit) && RYUI_CAMERAVIEW // an app that calls camera.View (see the CLI's iosFeatures)
import UIKit
import AVFoundation

/// ryui/camera's View: an AVCaptureSession's preview, filling the view,
/// a photo output for camera.Capture and, with scan on, a metadata output
/// that reads codes (ryui/scan's Codes). The session runs while the view is
/// in a window.
@MainActor final class RyUICameraView: NSObject, RyUINativeView, AVCapturePhotoCaptureDelegate, AVCaptureMetadataOutputObjectsDelegate {
    static weak var current: RyUICameraView?
    let ctx: RyUINativeContext
    let preview = RyUICameraPreview()
    var view: UIView { preview }
    private let session = AVCaptureSession()
    private let output = AVCapturePhotoOutput()
    private let metadata = AVCaptureMetadataOutput()
    private var facing: AVCaptureDevice.Position = .back
    private var scan = false
    private var kinds = RyUICodeKinds("")
    private var seen = RyUICodeSeen()
    private var configuring = false
    private var video = false
    #if RYUI_RECORD
    private let movie = AVCaptureMovieFileOutput()
    private var recording: ((Result<Data, Error>) -> Void)?
    #endif
    private var capturing: ((Result<Data, Error>) -> Void)?
    private let queue = DispatchQueue(label: "ryui.camera")

    init(_ ctx: RyUINativeContext) {
        self.ctx = ctx
        super.init()
        preview.preview.session = session
        preview.preview.videoGravity = .resizeAspectFill
        preview.attached = { [weak self] on in
            guard let self else { return }
            if on { RyUICameraView.current = self } else if RyUICameraView.current === self { RyUICameraView.current = nil }
            self.run(on)
        }
    }

    func set(_ name: String, _ value: RyUINativeValue) {
        switch name {
        case "facing": facing = value.string == "front" ? .front : .back
        case "scan": scan = value.bool
        case "codes": kinds = RyUICodeKinds(value.string)
        case "video": video = value.bool
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
                self.session.sessionPreset = self.video ? .high : .photo
                for i in self.session.inputs where (i as? AVCaptureDeviceInput)?.device.hasMediaType(.video) ?? true {
                    self.session.removeInput(i)
                }
                if self.session.canAddInput(input) { self.session.addInput(input) }
                #if RYUI_RECORD
                if self.video {
                    if !self.session.outputs.contains(self.movie), self.session.canAddOutput(self.movie) { self.session.addOutput(self.movie) }
                } else if self.session.outputs.contains(self.movie) {
                    self.session.removeOutput(self.movie)
                }
                #endif
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
        guard session.isRunning else { reply(.failure(RyUIModuleError("the camera isn't showing"))); return }
        capturing?(.failure(RyUIModuleError("another photo was taken")))
        capturing = reply
        output.capturePhoto(with: AVCapturePhotoSettings(), delegate: self)
    }

    #if RYUI_RECORD
    /// Records to a file until stopRecording (or max seconds, if not 0),
    /// with the microphone unless mute (or not allowed).
    func record(mute: Bool, max: Double, _ reply: @escaping (Result<Data, Error>) -> Void) {
        guard session.isRunning else { reply(.failure(RyUIModuleError("the camera isn't showing"))); return }
        guard video, session.outputs.contains(movie) else { reply(.failure(RyUIModuleError("the camera view isn't in video mode"))); return }
        guard !movie.isRecording else { reply(.failure(RyUIModuleError("already recording"))); return }
        let start = { [weak self] (sound: Bool) in
            guard let self else { return }
            self.session.beginConfiguration()
            let mic = self.session.inputs.first { ($0 as? AVCaptureDeviceInput)?.device.hasMediaType(.audio) ?? false }
            if sound, mic == nil, let d = AVCaptureDevice.default(for: .audio), let i = try? AVCaptureDeviceInput(device: d), self.session.canAddInput(i) {
                self.session.addInput(i)
            } else if !sound, let mic {
                self.session.removeInput(mic)
            }
            self.session.commitConfiguration()
            self.movie.maxRecordedDuration = max > 0 ? CMTime(seconds: max, preferredTimescale: 600) : .invalid
            let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-camera", isDirectory: true)
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            self.recording = reply
            self.movie.startRecording(to: dir.appendingPathComponent(UUID().uuidString + ".mov"), recordingDelegate: self)
        }
        if mute { start(false); return }
        AVCaptureDevice.requestAccess(for: .audio) { ok in DispatchQueue.main.async { start(ok) } }
    }

    func stopRecording() { if movie.isRecording { movie.stopRecording() } }
    #endif

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
            guard let data, error == nil else { reply?(.failure(error ?? RyUIModuleError("no photo"))); return }
            let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-camera", isDirectory: true)
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let f = dir.appendingPathComponent(UUID().uuidString + ".jpg")
            if (try? data.write(to: f)) != nil { reply?(.success(Data(f.path.utf8))) } else { reply?(.failure(RyUIModuleError("couldn't save the photo"))) }
        }
    }
}

/// The preview's view: its layer an AVCaptureVideoPreviewLayer.
final class RyUICameraPreview: UIView {
    override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
    var preview: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    var attached: ((Bool) -> Void)?

    override func didMoveToWindow() {
        super.didMoveToWindow()
        attached?(window != nil)
    }
}
#if RYUI_RECORD
extension RyUICameraView: AVCaptureFileOutputRecordingDelegate {
    nonisolated func fileOutput(_ o: AVCaptureFileOutput, didFinishRecordingTo url: URL, from c: [AVCaptureConnection], error: Error?) {
        MainActor.assumeIsolated {
            let reply = recording
            recording = nil
            // Stopped by its maximum duration (or the disk filling) the
            // file is still whole: AVErrorRecordingSuccessfullyFinishedKey.
            let ok = error == nil || ((error as NSError?)?.userInfo[AVErrorRecordingSuccessfullyFinishedKey] as? Bool ?? false)
            reply?(ok ? .success(Data(url.path.utf8)) : .failure(error!))
        }
    }
}
#endif
#endif
