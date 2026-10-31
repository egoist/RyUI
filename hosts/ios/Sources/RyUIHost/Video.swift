#if canImport(UIKit) && RYUI_VIDEO // an app that has a Video (see the CLI's iosFeatures)
import UIKit
import AVKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A Video: an AVPlayerViewController's view (its controls, full screen,
/// picture in picture, AirPlay), the controller a child of the nearest
/// one while the view is in a window.
@MainActor final class RyUIVideoView: UIView {
    let ryuiID: UInt32
    weak var runtime: RyUIRuntime?
    let vc = AVPlayerViewController()
    let player = AVPlayer()
    var autoPlay = false
    var loop = false
    private var observations: [NSKeyValueObservation] = []
    private var reported = CGSize.zero
    private var endObserver: NSObjectProtocol?

    init(_ id: UInt32, _ rt: RyUIRuntime) {
        ryuiID = id
        runtime = rt
        super.init(frame: .zero)
        vc.player = player
        vc.view.frame = bounds
        vc.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        vc.view.backgroundColor = .clear
        addSubview(vc.view)
        clipsToBounds = true
    }

    required init?(coder: NSCoder) { fatalError() }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        if window != nil {
            var r: UIResponder? = superview
            while let x = r, !(x is UIViewController) { r = x.next }
            if let parent = r as? UIViewController, vc.parent !== parent {
                parent.addChild(vc)
                vc.didMove(toParent: parent)
            }
        } else if vc.parent != nil {
            vc.willMove(toParent: nil)
            vc.removeFromParent()
            player.pause() // gone from the screen (popped, dismissed)
        }
    }

    /// Plays source: an http(s) URL, a file, or one of the app's assets.
    func load(source: String) {
        observations = []
        if let o = endObserver { NotificationCenter.default.removeObserver(o) }
        endObserver = nil
        reported = .zero
        guard !source.isEmpty else { player.replaceCurrentItem(with: nil); return }
        let url: URL? = source.hasPrefix("http://") || source.hasPrefix("https://") || source.hasPrefix("file://") ? URL(string: source)
            : source.hasPrefix("/") ? URL(fileURLWithPath: source)
            : Bundle.main.resourceURL?.appendingPathComponent("assets").appendingPathComponent(source)
        guard let url else { return }
        let item = AVPlayerItem(url: url)
        let id = ryuiID
        observations.append(item.observe(\.presentationSize, options: [.new]) { [weak self] item, _ in
            let s = item.presentationSize
            MainActor.assumeIsolated {
                guard let self, s.width > 0, s != self.reported else { return }
                self.reported = s
                self.runtime?.emit(id, .loaded) { RyUIEvents.loaded(id, x: Double(s.width), y: Double(s.height)) }
            }
        })
        observations.append(item.observe(\.status, options: [.new]) { [weak self] item, _ in
            guard item.status == .failed else { return }
            let m = item.error?.localizedDescription ?? "can't play the video"
            MainActor.assumeIsolated { self?.runtime?.emit(id, .failed) { RyUIEvents.failed(id, m) } }
        })
        endObserver = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                self.runtime?.emit(id, .ended) { RyUIEvents.ended(id) }
                if self.loop {
                    self.player.seek(to: .zero)
                    self.player.play()
                }
            }
        }
        player.replaceCurrentItem(with: item)
        if autoPlay { play() }
    }

    func seek(_ seconds: Double) {
        player.seek(to: CMTime(seconds: seconds, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
    }

    func play() {
        if !player.isMuted {
            // Sound as a video player's: heard with the silent switch on.
            try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        }
        player.play()
    }
}
#endif
