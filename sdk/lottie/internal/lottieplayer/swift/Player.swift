// asuka/sdk/lottie's player on iOS and macOS: lottie-ios' LottieAnimationView,
// driven as lottieplayer's props say (lottieplayer.go). A timer at the
// display's rate reports the progress while it plays (Report) and runs
// seeks that animate (their duration and curve: ui.Animate's).
import Asuka
import Foundation
import Lottie
import QuartzCore
#if canImport(UIKit)
import UIKit
#else
import AppKit
#endif

@MainActor final class LottiePlayer: Player, AsukaDestroying {
    let animationView: LottieAnimationView
    let events: PlayerEvents
    var view: AsukaPlatformView { animationView }

    private var source = ""
    private var data = Data()
    private var animationID = ""
    private var loop = false
    private var report = false
    private var playing = false
    private var speed: Double = 1
    private var segment = FrameRange()
    private var marker = ""
    private var seekSerial = 0
    private var pendingSeek: SeekTo?
    private var overrides: [KeypathOverride] = []
    private var applied: [AnimationKeypath] = []
    private var loadToken = 0
    private var done = false
    private var ticker: Timer?
    private var tween: Tween?
    private var lastReported = -1.0

    struct Tween {
        var from, to: Double
        var start: CFTimeInterval
        var seconds: Double
        var curve: Int
        var damping: Double
    }

    init(_ events: PlayerEvents) {
        self.events = events
        // Reduce Motion is the Go side's to follow (asuka/sdk/lottie).
        animationView = LottieAnimationView(animation: nil, configuration: LottieConfiguration(reducedMotionOption: .standardMotion))
        animationView.backgroundBehavior = .pauseAndRestore
        #if canImport(UIKit)
        animationView.contentMode = .scaleAspectFit
        animationView.clipsToBounds = true
        #else
        animationView.contentMode = .scaleAspectFit
        #endif
    }

    func destroy() {
        loadToken += 1
        ticker?.invalidate()
        ticker = nil
        animationView.stop()
    }

    // MARK: Loading

    func setSource(_ value: String) {
        source = value
        if !value.isEmpty { load() }
    }

    func setData(_ value: Data) {
        data = value
        if !value.isEmpty, source.isEmpty { load() }
    }

    func setAnimation(_ value: String) { animationID = value }

    private func load() {
        loadToken += 1
        let token = loadToken
        let src = source
        let bytes = data
        let finish: (Result<Data, Error>) -> Void = { r in
            DispatchQueue.main.async { [weak self] in self?.read(token, r) }
        }
        if !src.isEmpty, src.hasPrefix("http://") || src.hasPrefix("https://"), let url = URL(string: src) {
            URLSession.shared.dataTask(with: url) { d, resp, err in
                if let err { finish(.failure(err)); return }
                if let h = resp as? HTTPURLResponse, !(200..<300).contains(h.statusCode) {
                    finish(.failure(LottieError.message("\(src): HTTP \(h.statusCode)")))
                    return
                }
                finish(.success(d ?? Data()))
            }.resume()
            return
        }
        DispatchQueue.global(qos: .userInitiated).async {
            if src.isEmpty { finish(.success(bytes)); return }
            guard let url = LottiePlayer.fileURL(src), let d = try? Data(contentsOf: url) else {
                finish(.failure(LottieError.message("no animation at \(src)")))
                return
            }
            finish(.success(d))
        }
    }

    /// Where a source that isn't a URL is: an absolute path, a file:
    /// URL, or one of the app's assets (in its bundle; beside it, or in
    /// the working directory, for an app run from its folder on the Mac).
    nonisolated static func fileURL(_ src: String) -> URL? {
        if src.hasPrefix("file://") { return URL(string: src) }
        if src.hasPrefix("/") { return URL(fileURLWithPath: src) }
        var dirs: [URL] = []
        if let env = ProcessInfo.processInfo.environment["ASUKA_ASSETS"] { dirs.append(URL(fileURLWithPath: env)) }
        if let r = Bundle.main.resourceURL { dirs.append(r.appendingPathComponent("assets")) }
        dirs.append(Bundle.main.bundleURL.deletingLastPathComponent().appendingPathComponent("assets"))
        dirs.append(URL(fileURLWithPath: FileManager.default.currentDirectoryPath).appendingPathComponent("assets"))
        for d in dirs {
            let u = d.appendingPathComponent(src)
            if FileManager.default.fileExists(atPath: u.path) { return u }
        }
        return nil
    }

    private func read(_ token: Int, _ r: Result<Data, Error>) {
        guard token == loadToken else { return }
        let d: Data
        switch r {
        case .failure(let e): events.failed(message: e.localizedDescription); return
        case .success(let v): d = v
        }
        if d.starts(with: [0x50, 0x4B, 0x03, 0x04]) { // a dotLottie (a zip)
            DotLottieFile.loadedFrom(data: d, filename: "animation") { [weak self] result in
                guard let self, token == self.loadToken else { return }
                switch result {
                case .success(let file):
                    self.animationView.loadAnimation(self.animationID.isEmpty ? nil : self.animationID, from: file)
                    if self.animationView.animation == nil {
                        self.events.failed(message: "the dotLottie has no animation \"\(self.animationID)\"")
                        return
                    }
                    self.didLoad()
                case .failure(let e):
                    self.events.failed(message: "\(e)")
                }
            }
            return
        }
        do {
            animationView.animation = try LottieAnimation.from(data: d)
            didLoad()
        } catch {
            events.failed(message: "not a Lottie animation: \(error)")
        }
    }

    private func didLoad() {
        guard let a = animationView.animation else { return }
        animationView.animationSpeed = abs(speed)
        applyOverrides()
        var markers: [MarkerInfo] = []
        for n in a.markerNames {
            if let s = a.frameTime(forMarker: n) {
                markers.append(MarkerInfo(name: n, start: Double(s), duration: Double(a.durationFrameTime(forMarker: n) ?? 0)))
            }
        }
        events.loaded(info: AnimationInfo(width: Double(a.size.width), height: Double(a.size.height),
                                          start: Double(a.startFrame), end: Double(a.endFrame), frameRate: a.framerate, markers: markers))
        done = false
        animationView.currentFrame = span().from
        if let s = pendingSeek {
            pendingSeek = nil
            seekSerial = 0
            setSeek(s)
        }
        if playing { play() } else { reportNow() }
        tick()
    }

    // MARK: Playing

    /// The range it plays: a marker's, the segment, or all of it.
    private func span() -> (from: Double, to: Double) {
        guard let a = animationView.animation else { return (0, 0) }
        var from = Double(a.startFrame), to = Double(a.endFrame)
        if !marker.isEmpty, let s = a.frameTime(forMarker: marker) {
            return (Double(s), Double(s) + Double(a.durationFrameTime(forMarker: marker) ?? 0))
        }
        if segment.to > segment.from {
            from = max(segment.from, from)
            to = min(segment.to, to)
        }
        return (from, to)
    }

    private func play() {
        guard animationView.animation != nil, tween == nil else { return }
        let (from, to) = span()
        let back = speed < 0
        var start = Double(animationView.realtimeAnimationFrame)
        if done || !back && start >= to - 0.001 || back && start <= from + 0.001 || start < from || start > to {
            start = back ? to : from
        }
        done = false
        let end = back ? from : to
        let whole = back ? start == to : start == from
        // Looping from the middle: to the end once, then the whole range.
        let mode: LottieLoopMode = loop && whole ? .loop : .playOnce
        animationView.play(fromFrame: start, toFrame: end, loopMode: mode) { [weak self] finished in
            guard let self, finished else { return }
            if self.loop {
                self.animationView.play(fromFrame: back ? to : from, toFrame: end, loopMode: .loop)
                return
            }
            self.done = true
            self.reportNow()
            self.events.completed()
        }
        tick()
    }

    func setPlaying(_ value: Bool) {
        playing = value
        guard animationView.animation != nil else { return }
        if value {
            play()
        } else {
            animationView.pause()
            reportNow()
        }
        tick()
    }

    func setLoop(_ value: Bool) { loop = value }
    func setReport(_ value: Bool) { report = value; tick() }

    func setSpeed(_ value: Double) {
        let flipped = (value < 0) != (speed < 0)
        speed = value == 0 ? 1 : value
        animationView.animationSpeed = abs(speed)
        if flipped, playing, animationView.isAnimationPlaying { play() }
    }

    func setSegment(_ value: FrameRange) {
        segment = value
        rangeChanged()
    }

    func setMarker(_ value: String) {
        marker = value
        rangeChanged()
    }

    private func rangeChanged() {
        guard animationView.animation != nil else { return }
        let (from, to) = span()
        done = false
        tween = nil
        animationView.currentFrame = speed < 0 ? to : from
        if playing { play() } else { reportNow() }
    }

    func setSeek(_ value: SeekTo) {
        if value.serial == seekSerial { return }
        guard let a = animationView.animation else { pendingSeek = value; return }
        seekSerial = value.serial
        let (from, to) = span()
        var target = Double(a.startFrame) + value.progress * Double(a.endFrame - a.startFrame)
        if value.to == 1 { target = from }
        if value.to == 2 { target = to }
        done = false
        if value.duration > 0 {
            let at = Double(animationView.realtimeAnimationFrame)
            animationView.pause()
            tween = Tween(from: at, to: target, start: CACurrentMediaTime(), seconds: value.duration, curve: value.curve, damping: value.damping)
            tick()
            return
        }
        tween = nil
        animationView.currentFrame = target
        if playing { play() } else { reportNow() }
    }

    // MARK: Overrides

    func setOverrides(_ value: [KeypathOverride]) {
        overrides = value
        if animationView.animation != nil { applyOverrides() }
    }

    /// Sets the overrides' value providers, taking the last ones' away:
    /// a color on the fills and strokes under the keypath, an opacity on
    /// what it names, a width on the strokes under it.
    private func applyOverrides() {
        for k in applied { animationView.removeValueProvider(for: k) }
        applied = []
        func set(_ p: AnyValueProvider, _ path: String) {
            let k = AnimationKeypath(keypath: path)
            animationView.setValueProvider(p, keypath: k)
            applied.append(k)
        }
        for o in overrides where !o.keypath.isEmpty {
            switch o.kind {
            case 0:
                let c = ColorValueProvider(LottieColor(r: o.r, g: o.g, b: o.b, a: o.a))
                set(c, "\(o.keypath).Color")
                set(c, "\(o.keypath).**.Color")
            case 1:
                let v = FloatValueProvider(CGFloat(o.value * 100))
                set(v, "\(o.keypath).Transform.Opacity")
                set(v, "\(o.keypath).Opacity")
            default:
                let v = FloatValueProvider(CGFloat(o.value))
                set(v, "\(o.keypath).Stroke Width")
                set(v, "\(o.keypath).**.Stroke Width")
            }
        }
    }

    // MARK: Frames

    /// Runs the timer while there's a seek to animate or a progress to
    /// report.
    private func tick() {
        let needed = animationView.animation != nil && (tween != nil || report && animationView.isAnimationPlaying)
        if needed, ticker == nil {
            let t = Timer(timeInterval: 1.0 / 60, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.frame() }
            }
            RunLoop.main.add(t, forMode: .common)
            ticker = t
        } else if !needed, let t = ticker {
            t.invalidate()
            ticker = nil
        }
    }

    private func frame() {
        if let tw = tween {
            let x = (CACurrentMediaTime() - tw.start) / tw.seconds
            if x >= 1 {
                tween = nil
                animationView.currentFrame = tw.to
                if playing { play() }
            } else {
                animationView.currentFrame = tw.from + (tw.to - tw.from) * ease(tw.curve, tw.damping, x, tw.seconds)
            }
        }
        reportNow()
        tick()
    }

    private func reportNow() {
        guard report, let a = animationView.animation, a.endFrame > a.startFrame else { return }
        let f = tween != nil || !animationView.isAnimationPlaying ? Double(animationView.currentFrame) : Double(animationView.realtimeAnimationFrame)
        let p = min(1, max(0, (f - Double(a.startFrame)) / Double(a.endFrame - a.startFrame)))
        if p != lastReported {
            lastReported = p
            events.progress(progress: p)
        }
    }

    func setMode(_ value: Int) {
        #if canImport(UIKit)
        switch value {
        case 1: animationView.contentMode = .scaleAspectFill
        case 2: animationView.contentMode = .scaleToFill
        case 3: animationView.contentMode = .center
        default: animationView.contentMode = .scaleAspectFit
        }
        #else
        switch value {
        case 1: animationView.contentMode = .scaleAspectFill
        case 2: animationView.contentMode = .scaleToFill
        case 3: animationView.contentMode = .center
        default: animationView.contentMode = .scaleAspectFit
        }
        #endif
    }
}

enum LottieError: Error, LocalizedError {
    case message(String)
    var errorDescription: String? { if case .message(let m) = self { return m }; return nil }
}

/// A seek's curve at x (0 to 1) of its time: ui.Curve's (EaseInOut,
/// Linear, EaseIn, EaseOut, Spring), as package lottie's Go computes it.
func ease(_ curve: Int, _ damping: Double, _ x: Double, _ seconds: Double) -> Double {
    func bezier(_ x1: Double, _ y1: Double, _ x2: Double, _ y2: Double) -> Double {
        var lo = 0.0, hi = 1.0, t = x
        for _ in 0..<30 {
            let u = 1 - t
            let v = 3 * u * u * t * x1 + 3 * u * t * t * x2 + t * t * t
            if abs(v - x) < 1e-6 { break }
            if v < x { lo = t } else { hi = t }
            t = (lo + hi) / 2
        }
        let u = 1 - t
        return 3 * u * u * t * y1 + 3 * u * t * t * y2 + t * t * t
    }
    switch curve {
    case 1: return x
    case 2: return bezier(0.42, 0, 1, 1)
    case 3: return bezier(0, 0, 0.58, 1)
    case 4:
        let z = max(0.01, min(1, damping))
        let w = 10 / max(seconds, 0.01)
        let t = x * seconds
        if z >= 1 { return 1 - exp(-w * t) * (1 + w * t) }
        let wd = w * (1 - z * z).squareRoot()
        return 1 - exp(-z * w * t) * (cos(wd * t) + z * w / wd * sin(wd * t))
    default: return bezier(0.42, 0, 0.58, 1)
    }
}

@MainActor func makePlayer(_ events: PlayerEvents) -> any Player { LottiePlayer(events) }
