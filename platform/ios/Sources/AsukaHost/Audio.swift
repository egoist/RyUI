#if canImport(UIKit) && ASUKA_AUDIO // an app that imports asuka/audio (see the CLI's iosFeatures)
import UIKit
import AVFoundation

/// asuka/audio's module: players by ID (each call's args begin with it, a
/// u32-length string): load (the source), play, playOnce, pause, seek
/// (f64 seconds), volume (f64), rate (f64), loop (u8), release; status
/// answers f64 position, f64 duration, u8 playing, u8 ended.
@MainActor final class AsukaAudio {
    static let shared = AsukaAudio()
    private var players: [String: AVPlayer] = [:]
    private var loops = Set<String>()
    private var once = Set<String>()
    private var ends: [String: NSObjectProtocol] = [:]
    private var rates: [String: Float] = [:]
    private var ended = Set<String>()

    func methods() -> [String: AsukaModuleMethod] {
        func split(_ d: Data) -> (String, Data) {
            guard d.count >= 4 else { return ("", Data()) }
            let n = Int(d.prefix(4).withUnsafeBytes { $0.loadUnaligned(as: UInt32.self).littleEndian })
            return (String(decoding: d.dropFirst(4).prefix(n), as: UTF8.self), Data(d.dropFirst(4 + n)))
        }
        func f64(_ d: Data) -> Double { d.count >= 8 ? d.prefix(8).withUnsafeBytes { Double(bitPattern: $0.loadUnaligned(as: UInt64.self).littleEndian) } : 0 }
        func method(_ f: @escaping (String, Data) -> Void) -> AsukaModuleMethod {
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
                ended.remove(id)
                players[id]?.seek(to: CMTime(seconds: f64(rest), preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
            },
            "volume": method { [unowned self] id, rest in players[id]?.volume = Float(f64(rest)) },
            "loop": method { [unowned self] id, rest in if rest.first == 1 { loops.insert(id) } else { loops.remove(id) } },
            "rate": method { [unowned self] id, rest in
                let r = Float(f64(rest))
                rates[id] = r
                if let p = players[id], p.rate > 0 { p.rate = r }
            },
            "release": method { [unowned self] id, _ in release(id) },
            "status": { [unowned self] args, reply in
                let (id, _) = split(args)
                var out = Data(capacity: 18)
                func put(_ v: Double) { withUnsafeBytes(of: v.bitPattern.littleEndian) { out.append(contentsOf: $0) } }
                let p = players[id]
                let position = p?.currentTime().seconds ?? 0
                let duration = p?.currentItem?.duration.seconds ?? 0
                put(position.isFinite ? position : 0)
                put(duration.isFinite ? duration : 0)
                out.append(p?.timeControlStatus == .playing ? 1 : 0)
                out.append(ended.contains(id) ? 1 : 0)
                reply(.success(out))
            },
        ]
    }

    private func load(_ id: String, _ source: String) {
        let url: URL? = source.hasPrefix("http://") || source.hasPrefix("https://") || source.hasPrefix("file://") ? URL(string: source)
            : source.hasPrefix("/") ? URL(fileURLWithPath: source)
            : asukaAssets?.appendingPathComponent(source)
        guard let url else { return }
        let item = AVPlayerItem(url: url)
        item.audioTimePitchAlgorithm = .timeDomain // keeps a voice's pitch at any rate
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
                } else {
                    self.ended.insert(id)
                }
            }
        }
    }

    private func play(_ id: String) {
        // Heard with the silent switch on, as a music or game app's sound.
        try? AVAudioSession.sharedInstance().setCategory(.playback)
        try? AVAudioSession.sharedInstance().setActive(true)
        guard let p = players[id] else { return }
        if ended.remove(id) != nil { p.seek(to: .zero, toleranceBefore: .zero, toleranceAfter: .zero) }
        p.playImmediately(atRate: rates[id] ?? 1)
    }

    private func release(_ id: String) {
        players.removeValue(forKey: id)?.pause()
        if let o = ends.removeValue(forKey: id) { NotificationCenter.default.removeObserver(o) }
        loops.remove(id)
        once.remove(id)
        rates.removeValue(forKey: id)
        ended.remove(id)
    }
}
#endif
