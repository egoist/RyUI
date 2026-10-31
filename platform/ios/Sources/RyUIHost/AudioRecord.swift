#if canImport(UIKit) && RYUI_AUDIORECORD // an app that calls ryui/audio/record (see the CLI's iosFeatures)
import UIKit
import AVFoundation
#if canImport(RyUIOps)
import RyUIOps
#endif

/// ryui/audio/record's module: "start" (u8 quality: voice, music; u32 max
/// milliseconds, 0: none) records until "stop" and answers with the
/// file's path; "pause", "resume"; "level" answers u8, 0 to 255.
@MainActor final class RyUIAudioRecord: NSObject, AVAudioRecorderDelegate {
    static let shared = RyUIAudioRecord()
    private var recorder: AVAudioRecorder?
    private var done: ((Result<Data, Error>) -> Void)?
    private var category: AVAudioSession.Category?

    func methods() -> [String: RyUIModuleMethod] {
        [
            "start": { [unowned self] args, reply in
                guard self.recorder == nil else { reply(.failure(RyUIModuleError("already recording"))); return }
                let quality = args.first ?? 0
                let max = args.count >= 5 ? args.withUnsafeBytes { b -> UInt32 in var r = RyUIReader(b); _ = r.u8(); return r.u32() } : 0
                AVAudioSession.sharedInstance().requestRecordPermission { ok in
                    DispatchQueue.main.async {
                        guard ok else { reply(.failure(RyUIModuleError("not allowed"))); return }
                        self.start(quality, max, reply)
                    }
                }
            },
            "stop": { [unowned self] _, reply in
                self.recorder?.stop() // the delegate answers start
                reply(.success(Data()))
            },
            "pause": { [unowned self] _, reply in self.recorder?.pause(); reply(.success(Data())) },
            "resume": { [unowned self] _, reply in self.recorder?.record(); reply(.success(Data())) },
            "level": { [unowned self] _, reply in
                guard let r = self.recorder else { reply(.success(Data([0]))); return }
                r.updateMeters()
                let db = r.averagePower(forChannel: 0) // -160...0
                let level = db <= -60 ? 0 : pow(10, db / 20)
                reply(.success(Data([UInt8(min(max(level, 0), 1) * 255)])))
            },
        ]
    }

    private func start(_ quality: UInt8, _ maxMillis: UInt32, _ reply: @escaping (Result<Data, Error>) -> Void) {
        let session = AVAudioSession.sharedInstance()
        category = session.category
        do {
            try session.setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker, .allowBluetoothHFP])
            try session.setActive(true)
        } catch {
            reply(.failure(error)); return
        }
        let music = quality == 1
        let settings: [String: Any] = [
            AVFormatIDKey: kAudioFormatMPEG4AAC,
            AVSampleRateKey: music ? 44100 : 22050,
            AVNumberOfChannelsKey: music ? 2 : 1,
            AVEncoderBitRateKey: music ? 128_000 : 64_000,
            AVEncoderAudioQualityKey: AVAudioQuality.high.rawValue,
        ]
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-audio", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(UUID().uuidString + ".m4a")
        do {
            let r = try AVAudioRecorder(url: url, settings: settings)
            r.delegate = self
            r.isMeteringEnabled = true
            let ok = maxMillis > 0 ? r.record(forDuration: TimeInterval(maxMillis) / 1000) : r.record()
            guard ok else { reply(.failure(RyUIModuleError("couldn't record"))); restore(); return }
            recorder = r
            done = reply
        } catch {
            reply(.failure(error))
            restore()
        }
    }

    private func restore() {
        let s = AVAudioSession.sharedInstance()
        try? s.setCategory(category ?? .playback)
        category = nil
    }

    nonisolated func audioRecorderDidFinishRecording(_ r: AVAudioRecorder, successfully ok: Bool) {
        let path = r.url.path
        DispatchQueue.main.async {
            let d = self.done
            self.done = nil
            self.recorder = nil
            self.restore()
            d?(ok ? .success(Data(path.utf8)) : .failure(RyUIModuleError("the recording failed")))
        }
    }

    nonisolated func audioRecorderEncodeErrorDidOccur(_ r: AVAudioRecorder, error: Error?) {
        DispatchQueue.main.async {
            let d = self.done
            self.done = nil
            self.recorder = nil
            self.restore()
            d?(.failure(error ?? RyUIModuleError("the recording failed")))
        }
    }
}
#endif
