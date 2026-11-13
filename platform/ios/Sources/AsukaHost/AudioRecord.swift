#if canImport(UIKit) && ASUKA_AUDIORECORD // an app that calls asuka/audio/record (see the CLI's iosFeatures)
import UIKit
import AVFoundation
#if canImport(AsukaOps)
import AsukaOps
#endif

/// asuka/audio/record's module: "start" (u8 quality: voice, music; u32 max
/// milliseconds, 0: none; u8 flags, 1 background (which iOS doesn't show), 2
/// stream; u32-length title; u32-length path, empty for the app's caches)
/// records until "stop" and answers with the file's path; "pause",
/// "resume"; "level" answers u8, 0 to 255; "status" answers u8 level, u8
/// recording, u32 milliseconds kept. An interruption (a call) pauses it,
/// and it goes on after if the system says it may.

@MainActor final class AsukaAudioRecord: NSObject, AVAudioRecorderDelegate {
    static let shared = AsukaAudioRecord()
    private var recorder: AVAudioRecorder?
    private var done: ((Result<Data, Error>) -> Void)?
    private var category: AVAudioSession.Category?
    private var interruption: NSObjectProtocol?
    private var userPaused = false

    func methods() -> [String: AsukaModuleMethod] {
        [
            "start": { [unowned self] args, reply in
                guard self.recorder == nil else { reply(.failure(AsukaModuleError("already recording"))); return }
                var quality: UInt8 = 0, max: UInt32 = 0, flags: UInt8 = 0, path = ""
                args.withUnsafeBytes { b in
                    var r = AsukaReader(b)
                    quality = r.u8()
                    max = r.u32()
                    flags = r.u8()
                    _ = r.string() // the notification's title: iOS has none
                    path = r.string()
                }
                let stream = flags & 2 != 0
                AVAudioSession.sharedInstance().requestRecordPermission { ok in
                    DispatchQueue.main.async {
                        guard ok else { reply(.failure(AsukaModuleError("not allowed"))); return }
                        self.start(quality, max, stream, path, reply)
                    }
                }
            },
            "stop": { [unowned self] _, reply in
                self.recorder?.stop() // the delegate answers start
                reply(.success(Data()))
            },
            "pause": { [unowned self] _, reply in self.userPaused = true; self.recorder?.pause(); reply(.success(Data())) },
            "resume": { [unowned self] _, reply in
                self.userPaused = false
                try? AVAudioSession.sharedInstance().setActive(true)
                self.recorder?.record()
                reply(.success(Data()))
            },
            "level": { [unowned self] _, reply in reply(.success(Data([self.level()]))) },
            "status": { [unowned self] _, reply in
                var out = Data([self.level(), self.recorder?.isRecording == true ? 1 : 0])
                let ms = UInt32(max((self.recorder?.currentTime ?? 0) * 1000, 0))
                withUnsafeBytes(of: ms.littleEndian) { out.append(contentsOf: $0) }
                reply(.success(out))
            },
        ]
    }

    private func level() -> UInt8 {
        guard let r = recorder, r.isRecording else { return 0 }
        r.updateMeters()
        let db = r.averagePower(forChannel: 0) // -160...0
        let level = db <= -60 ? 0 : pow(10, db / 20)
        return UInt8(min(max(level, 0), 1) * 255)
    }

    private func observeInterruptions() {
        interruption = NotificationCenter.default.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { [weak self] n in
            guard let info = n.userInfo, let raw = info[AVAudioSessionInterruptionTypeKey] as? UInt,
                  AVAudioSession.InterruptionType(rawValue: raw) == .ended else { return }
            let options = AVAudioSession.InterruptionOptions(rawValue: info[AVAudioSessionInterruptionOptionKey] as? UInt ?? 0)
            MainActor.assumeIsolated {
                guard let self, !self.userPaused, options.contains(.shouldResume) else { return }
                try? AVAudioSession.sharedInstance().setActive(true)
                self.recorder?.record()
            }
        }
    }

    private func start(_ quality: UInt8, _ maxMillis: UInt32, _ stream: Bool, _ path: String, _ reply: @escaping (Result<Data, Error>) -> Void) {
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
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("asuka-audio", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        // An .aac is ADTS, which AudioFile writes frame by frame.
        let url = path.isEmpty ? dir.appendingPathComponent(UUID().uuidString + (stream ? ".aac" : ".m4a")) : URL(fileURLWithPath: path)
        do {
            let r = try AVAudioRecorder(url: url, settings: settings)
            r.delegate = self
            r.isMeteringEnabled = true
            let ok = maxMillis > 0 ? r.record(forDuration: TimeInterval(maxMillis) / 1000) : r.record()
            guard ok else { reply(.failure(AsukaModuleError("couldn't record"))); restore(); return }
            recorder = r
            done = reply
            userPaused = false
            observeInterruptions()
        } catch {
            reply(.failure(error))
            restore()
        }
    }

    private func restore() {
        if let o = interruption { NotificationCenter.default.removeObserver(o) }
        interruption = nil
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
            d?(ok ? .success(Data(path.utf8)) : .failure(AsukaModuleError("the recording failed")))
        }
    }

    nonisolated func audioRecorderEncodeErrorDidOccur(_ r: AVAudioRecorder, error: Error?) {
        DispatchQueue.main.async {
            let d = self.done
            self.done = nil
            self.recorder = nil
            self.restore()
            d?(.failure(error ?? AsukaModuleError("the recording failed")))
        }
    }
}
#endif
