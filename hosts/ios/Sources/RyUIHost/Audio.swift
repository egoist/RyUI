#if canImport(UIKit) && RYUI_AUDIO // an app that imports ryui/audio (see the CLI's iosFeatures)
import UIKit
import AVFoundation

/// ryui/audio's module: players by ID (each call's args begin with it, a
/// u32-length string): load (the source), play, playOnce, pause, seek
/// (f64 seconds), volume (f64), loop (u8), release.
@MainActor final class RyUIAudio {
    static let shared = RyUIAudio()
    private var players: [String: AVPlayer] = [:]
    private var loops = Set<String>()
    private var once = Set<String>()
    private var ends: [String: NSObjectProtocol] = [:]

    func methods() -> [String: RyUIModuleMethod] {
        func split(_ d: Data) -> (String, Data) {
            guard d.count >= 4 else { return ("", Data()) }
            let n = Int(d.prefix(4).withUnsafeBytes { $0.loadUnaligned(as: UInt32.self).littleEndian })
            return (String(decoding: d.dropFirst(4).prefix(n), as: UTF8.self), Data(d.dropFirst(4 + n)))
        }
        func f64(_ d: Data) -> Double { d.count >= 8 ? d.prefix(8).withUnsafeBytes { Double(bitPattern: $0.loadUnaligned(as: UInt64.self).littleEndian) } : 0 }
        func method(_ f: @escaping (String, Data) -> Void) -> RyUIModuleMethod {
            { args, reply in
                let (id, rest) = split(args)
                f(id, rest)
                reply(.success(Data()))
            }
        }
        return [
            "load": method { [unowned self] id, rest in load(id, String(decoding: rest, as: UTF8.self)) },
            "play": method { [unowned self] id, _ in play(id) },
            "playOnce": method { [unowned self] id, _ in once.insert(id); play(id) },
            "pause": method { [unowned self] id, _ in players[id]?.pause() },
            "seek": method { [unowned self] id, rest in
                players[id]?.seek(to: CMTime(seconds: f64(rest), preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
            },
            "volume": method { [unowned self] id, rest in players[id]?.volume = Float(f64(rest)) },
            "loop": method { [unowned self] id, rest in if rest.first == 1 { loops.insert(id) } else { loops.remove(id) } },
            "release": method { [unowned self] id, _ in release(id) },
        ]
    }

    private func load(_ id: String, _ source: String) {
        let url: URL? = source.hasPrefix("http://") || source.hasPrefix("https://") || source.hasPrefix("file://") ? URL(string: source)
            : source.hasPrefix("/") ? URL(fileURLWithPath: source)
            : Bundle.main.resourceURL?.appendingPathComponent("assets").appendingPathComponent(source)
        guard let url else { return }
        let item = AVPlayerItem(url: url)
        let p = AVPlayer(playerItem: item)
        players[id] = p
        ends[id] = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                if self.loops.contains(id) {
                    p.seek(to: .zero)
                    p.play()
                } else if self.once.contains(id) {
                    self.release(id)
                }
            }
        }
    }

    private func play(_ id: String) {
        // Heard with the silent switch on, as a music or game app's sound.
        try? AVAudioSession.sharedInstance().setCategory(.playback)
        try? AVAudioSession.sharedInstance().setActive(true)
        players[id]?.play()
    }

    private func release(_ id: String) {
        players.removeValue(forKey: id)?.pause()
        if let o = ends.removeValue(forKey: id) { NotificationCenter.default.removeObserver(o) }
        loops.remove(id)
        once.remove(id)
    }
}
#endif
