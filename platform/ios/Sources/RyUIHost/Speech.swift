#if canImport(UIKit) && RYUI_SPEECH // an app that calls ryui/speech (see the CLI's iosFeatures)
import UIKit
import AVFoundation

/// ryui/speech's module, with AVSpeechSynthesizer: "speak" (JSON: id,
/// text, language, voice, pitch, rate, volume) answers once the utterance
/// is spoken, or fails with "stopped"; "stop", "pause", "resume";
/// "speaking" (u8); "voices" (JSON). While it speaks, the audio session
/// plays (with the silent switch on) and ducks other apps, unless the app
/// set it up itself (ryui/audio's); after, it goes back.
@MainActor final class RyUISpeech: NSObject, AVSpeechSynthesizerDelegate {
    static let shared = RyUISpeech()
    private let synth = AVSpeechSynthesizer()
    private var replies: [ObjectIdentifier: (Result<Data, Error>) -> Void] = [:]
    private var session: (AVAudioSession.Category, AVAudioSession.CategoryOptions)? // what it was, if this set it

    override init() {
        super.init()
        synth.delegate = self
    }

    func methods() -> [String: RyUIModuleMethod] {
        [
            "speak": { [unowned self] args, reply in
                guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any], let text = o["text"] as? String else {
                    reply(.failure(RyUIModuleError("bad args"))); return
                }
                let u = AVSpeechUtterance(string: text)
                if let id = o["voice"] as? String, !id.isEmpty, let v = AVSpeechSynthesisVoice(identifier: id) {
                    u.voice = v
                } else if let l = o["language"] as? String, !l.isEmpty {
                    u.voice = AVSpeechSynthesisVoice(language: l)
                }
                u.pitchMultiplier = Float(min(max(o["pitch"] as? Double ?? 1, 0.5), 2))
                let rate = Float(o["rate"] as? Double ?? 1) * AVSpeechUtteranceDefaultSpeechRate
                u.rate = min(max(rate, AVSpeechUtteranceMinimumSpeechRate), AVSpeechUtteranceMaximumSpeechRate)
                u.volume = Float(min(max(o["volume"] as? Double ?? 1, 0), 1))
                self.claimSession()
                self.replies[ObjectIdentifier(u)] = reply
                self.synth.speak(u)
            },
            "stop": { [unowned self] _, reply in
                // Each queued one ends "stopped" here: after a pause, the
                // synthesizer reports a stopped utterance as finished. Its
                // delegate's calls for them find nothing after.
                let rs = self.replies
                self.replies = [:]
                self.synth.stopSpeaking(at: .immediate)
                for r in rs.values { r(.failure(RyUIModuleError("stopped"))) }
                self.releaseSession()
                reply(.success(Data()))
            },
            "pause": { [unowned self] _, reply in
                self.synth.pauseSpeaking(at: .word)
                reply(.success(Data()))
            },
            "resume": { [unowned self] _, reply in
                self.synth.continueSpeaking()
                reply(.success(Data()))
            },
            "speaking": { [unowned self] _, reply in
                reply(.success(Data([self.synth.isSpeaking || self.synth.isPaused ? 1 : 0])))
            },
            "voices": { _, reply in
                let vs = AVSpeechSynthesisVoice.speechVoices().map { v -> [String: Any] in
                    var q = 0
                    switch v.quality {
                    case .enhanced: q = 1
                    case .premium: q = 2
                    default: q = 0
                    }
                    return ["id": v.identifier, "name": v.name, "language": v.language, "quality": q, "network": false]
                }
                reply(.success((try? JSONSerialization.data(withJSONObject: vs)) ?? Data("[]".utf8)))
            },
        ]
    }

    /// Plays through the silent switch, ducking others, while it speaks,
    /// unless the app's session already plays (ryui/audio's).
    private func claimSession() {
        let s = AVAudioSession.sharedInstance()
        guard session == nil, s.category != .playback, s.category != .playAndRecord else { return }
        session = (s.category, s.categoryOptions)
        try? s.setCategory(.playback, mode: .spokenAudio, options: [.duckOthers, .interruptSpokenAudioAndMixWithOthers])
        try? s.setActive(true)
    }

    private func releaseSession() {
        guard let (category, options) = session, replies.isEmpty else { return }
        session = nil
        let s = AVAudioSession.sharedInstance()
        try? s.setActive(false, options: .notifyOthersOnDeactivation)
        try? s.setCategory(category, options: options)
    }

    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didFinish u: AVSpeechUtterance) {
        let id = ObjectIdentifier(u)
        DispatchQueue.main.async { RyUISpeech.shared.finish(id) { .success(Data()) } }
    }

    nonisolated func speechSynthesizer(_ s: AVSpeechSynthesizer, didCancel u: AVSpeechUtterance) {
        let id = ObjectIdentifier(u)
        DispatchQueue.main.async { RyUISpeech.shared.finish(id) { .failure(RyUIModuleError("stopped")) } }
    }

    private func finish(_ id: ObjectIdentifier, _ result: () -> Result<Data, Error>) {
        replies.removeValue(forKey: id)?(result())
        releaseSession()
    }
}
#endif
