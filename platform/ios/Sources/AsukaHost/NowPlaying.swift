#if canImport(UIKit) && ASUKA_NOWPLAYING // an app that imports asuka/nowplaying (see the CLI's iosFeatures)
import UIKit
import AVFoundation
import MediaPlayer

/// asuka/nowplaying's module: MPNowPlayingInfoCenter, which the lock
/// screen, Control Center, the Dynamic Island and CarPlay show, and
/// MPRemoteCommandCenter, whose commands they, AirPods and headsets send.
/// "set" (JSON: internal/nowplayingspec's Info) sets the info and enables
/// its commands; "clear" takes it away; "next" answers with the commands
/// since (u8 command, f64 value each) once there are some; "stop" fails
/// the waiting "next" with "stopped". iOS shows the app whose audio
/// session plays: the session is put in the playback category (the app
/// has the audio background mode, so it plays on with the screen locked).
@MainActor final class AsukaNowPlaying {
    static let shared = AsukaNowPlaying()

    /// The commands, by internal/nowplayingspec's numbers.
    private enum Command: UInt8 {
        case play = 1, pause, toggle, stop, next, previous, seek, skipForward, skipBackward
    }

    private struct Info: Decodable {
        var title: String?
        var artist: String?
        var album: String?
        var artwork: String?
        var duration: Double?
        var position: Double
        var rate: Double
        var state: String
        var commands: UInt32
        var skip: Double?

        func has(_ c: Command) -> Bool { commands & (1 << UInt32(c.rawValue)) != 0 }
    }

    private var info: Info?
    private var targets = false
    private var artSource = ""
    private var artwork: MPMediaItemArtwork?
    private var following = false
    private var waiting: ((Result<Data, Error>) -> Void)?
    private var queue = Data()

    func methods() -> [String: AsukaModuleMethod] {
        [
            "set": { [unowned self] args, reply in
                do {
                    set(try JSONDecoder().decode(Info.self, from: args))
                    reply(.success(Data()))
                } catch {
                    reply(.failure(error))
                }
            },
            "clear": { [unowned self] _, reply in
                set(nil)
                reply(.success(Data()))
            },
            "next": { [unowned self] _, reply in
                waiting?(.failure(AsukaModuleError("stopped"))) // one poll at a time
                following = true
                waiting = reply
                flush()
            },
            "stop": { [unowned self] _, reply in
                let w = waiting
                following = false
                waiting = nil
                queue = Data()
                w?(.failure(AsukaModuleError("stopped")))
                reply(.success(Data()))
            },
        ]
    }

    private func command(_ c: Command, _ value: Double) -> MPRemoteCommandHandlerStatus {
        guard let info else { return .noActionableNowPlayingItem }
        guard following, info.has(c) else { return .commandFailed }
        queue.append(c.rawValue)
        withUnsafeBytes(of: value.bitPattern.littleEndian) { queue.append(contentsOf: $0) }
        flush()
        return .success
    }

    private func flush() {
        guard let w = waiting, !queue.isEmpty else { return }
        let b = queue
        waiting = nil
        queue = Data()
        w(.success(b))
    }

    private func remoteCommands() -> [(MPRemoteCommand, Command)] {
        let c = MPRemoteCommandCenter.shared()
        return [
            (c.playCommand, .play), (c.pauseCommand, .pause), (c.togglePlayPauseCommand, .toggle),
            (c.stopCommand, .stop), (c.nextTrackCommand, .next), (c.previousTrackCommand, .previous),
            (c.changePlaybackPositionCommand, .seek), (c.skipForwardCommand, .skipForward),
            (c.skipBackwardCommand, .skipBackward),
        ]
    }

    private func set(_ new: Info?) {
        info = new
        if !targets, new != nil {
            targets = true
            // Heard with the silent switch on and the screen locked; iOS
            // shows the Now Playing of the app whose session plays.
            try? AVAudioSession.sharedInstance().setCategory(.playback)
            UIApplication.shared.beginReceivingRemoteControlEvents()
            for (cmd, c) in remoteCommands() {
                cmd.addTarget { [weak self] event in
                    var v = 0.0
                    if let e = event as? MPChangePlaybackPositionCommandEvent { v = e.positionTime }
                    if let e = event as? MPSkipIntervalCommandEvent { v = e.interval }
                    // MediaPlayer calls it on the main thread; should it not,
                    // the command goes there.
                    guard Thread.isMainThread else {
                        DispatchQueue.main.async { MainActor.assumeIsolated { _ = self?.command(c, v) } }
                        return .success
                    }
                    return MainActor.assumeIsolated { self?.command(c, v) ?? .commandFailed }
                }
            }
        }
        for (cmd, c) in remoteCommands() {
            cmd.isEnabled = new?.has(c) ?? false
        }
        let center = MPNowPlayingInfoCenter.default()
        guard let info = new else {
            artSource = ""
            artwork = nil
            center.nowPlayingInfo = nil
            return
        }
        if let s = info.skip, s > 0 {
            let c = MPRemoteCommandCenter.shared()
            c.skipForwardCommand.preferredIntervals = [NSNumber(value: s)]
            c.skipBackwardCommand.preferredIntervals = [NSNumber(value: s)]
        }
        loadArtwork(info.artwork ?? "")
        publish(info)
    }

    private func publish(_ info: Info) {
        var d: [String: Any] = [
            MPNowPlayingInfoPropertyMediaType: MPNowPlayingInfoMediaType.audio.rawValue,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: info.position,
            MPNowPlayingInfoPropertyPlaybackRate: info.state == "playing" ? info.rate : 0.0,
            MPNowPlayingInfoPropertyDefaultPlaybackRate: 1.0,
        ]
        if let t = info.title, !t.isEmpty { d[MPMediaItemPropertyTitle] = t }
        if let a = info.artist, !a.isEmpty { d[MPMediaItemPropertyArtist] = a }
        if let a = info.album, !a.isEmpty { d[MPMediaItemPropertyAlbumTitle] = a }
        if let du = info.duration, du > 0 { d[MPMediaItemPropertyPlaybackDuration] = du }
        if let artwork { d[MPMediaItemPropertyArtwork] = artwork }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = d
    }

    /// Loads the artwork (an asset, a file, an http(s) URL) off the main
    /// thread, and shows it once it's in.
    private func loadArtwork(_ source: String) {
        guard source != artSource else { return }
        artSource = source
        artwork = nil
        guard !source.isEmpty else { return }
        AsukaImageLoader.shared.load(source) { [weak self] r in
            guard let self, self.artSource == source, case .success(let img) = r else { return }
            self.artwork = MPMediaItemArtwork(boundsSize: img.size) { _ in img }
            if let info = self.info { self.publish(info) }
        }
    }
}
#endif
