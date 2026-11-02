#if canImport(UIKit) && RYUI_TRANSCRIBE // an app that calls ryui/transcribe (see the CLI's iosFeatures)
import UIKit
import Speech

/// ryui/transcribe's module: "check" (the language) answers JSON onDevice,
/// language; "file" (JSON path, language) answers JSON text, segments,
/// language. On the device only: it never falls back to Apple's servers.
@MainActor final class RyUITranscribe {
    static let shared = RyUITranscribe()
    private var task: SFSpeechRecognitionTask?

    private func recognizer(_ language: String) -> SFSpeechRecognizer? {
        language.isEmpty ? SFSpeechRecognizer() : SFSpeechRecognizer(locale: Locale(identifier: language))
    }

    func methods() -> [String: RyUIModuleMethod] {
        [
            "check": { [unowned self] args, reply in
                let r = self.recognizer(String(decoding: args, as: UTF8.self))
                reply(Result { try JSONSerialization.data(withJSONObject: [
                    "onDevice": r?.supportsOnDeviceRecognition ?? false,
                    "language": r?.locale.identifier ?? "",
                ] as [String: Any]) })
            },
            "file": { [unowned self] args, reply in
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: String] ?? [:]
                let path = o["path"] ?? "", language = o["language"] ?? ""
                SFSpeechRecognizer.requestAuthorization { status in
                    DispatchQueue.main.async {
                        guard status == .authorized else { reply(.failure(RyUIModuleError("not allowed"))); return }
                        self.run(path, language, reply)
                    }
                }
            },
        ]
    }

    private func run(_ path: String, _ language: String, _ reply: @escaping (Result<Data, Error>) -> Void) {
        guard task == nil else { reply(.failure(RyUIModuleError("already transcribing"))); return }
        guard let rec = recognizer(language), rec.supportsOnDeviceRecognition else {
            reply(.failure(RyUIModuleError("not available"))); return
        }
        let request = SFSpeechURLRecognitionRequest(url: URL(fileURLWithPath: path))
        request.requiresOnDeviceRecognition = true
        request.addsPunctuation = true
        request.shouldReportPartialResults = false
        let locale = rec.locale.identifier
        let collect = Collect()
        task = rec.recognitionTask(with: request) { [weak self] result, error in
            if let result { collect.add(result.bestTranscription) }
            guard error != nil || result?.isFinal == true else { return }
            let answer = collect.answer(locale: locale)
            let err = error as NSError?
            DispatchQueue.main.async {
                self?.task = nil
                // No speech in it (kAFAssistantErrorDomain 1110, or 203) is an empty text.
                let silence = err?.domain == "kAFAssistantErrorDomain" && (err?.code == 1110 || err?.code == 203)
                if let err, !silence, !collect.hasText {
                    // The recognizer can't start here (kLSRErrorDomain): the simulator
                    // says it can, then has no models; a phone may not have them yet.
                    reply(.failure(err.domain == "kLSRErrorDomain" ? RyUIModuleError("not available") : err))
                } else {
                    reply(Result { try JSONSerialization.data(withJSONObject: answer) })
                }
            }
        }
    }

    /// What's been recognized. A long file can come in several results, each
    /// on from the last, or a result can replace the one before.
    private final class Collect: @unchecked Sendable {
        private var done: [SFTranscription] = []
        private let lock = NSLock()

        var hasText: Bool { lock.withLock { done.contains { !$0.formattedString.isEmpty } } }

        func add(_ t: SFTranscription) {
            lock.withLock {
                if let last = done.last, let lastSeg = last.segments.last, let first = t.segments.first,
                   first.timestamp >= lastSeg.timestamp + lastSeg.duration - 0.01 {
                    done.append(t)
                } else {
                    if !done.isEmpty { done.removeLast() }
                    done.append(t)
                }
            }
        }

        func answer(locale: String) -> [String: Any] {
            lock.withLock {
                [
                    "text": done.map(\.formattedString).filter { !$0.isEmpty }.joined(separator: " "),
                    "language": locale,
                    "segments": done.flatMap(\.segments).map {
                        ["text": $0.substring, "start": $0.timestamp, "duration": $0.duration] as [String: Any]
                    },
                ]
            }
        }
    }
}
#endif
