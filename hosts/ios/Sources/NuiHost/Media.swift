#if canImport(UIKit)
import UIKit
import CryptoKit
#if canImport(NuiOps)
import NuiOps
#endif

// Images

/// Loads images off the main thread: fetched (http, https; kept on disk,
/// NuiDiskCache) or read (the app's assets), decoded and prepared for
/// display there, then cached in memory.
final class NuiImageLoader: @unchecked Sendable {
    static let shared = NuiImageLoader()
    private let cache = NSCache<NSString, UIImage>()
    private let queue = DispatchQueue(label: "nui.images", qos: .userInitiated, attributes: .concurrent)
    private var waiting: [String: [(Result<UIImage, Error>) -> Void]] = [:] // main thread only

    init() { cache.totalCostLimit = 64 << 20 }

    func cached(_ source: String) -> UIImage? { cache.object(forKey: source as NSString) }

    /// Calls done on the main thread.
    @MainActor func load(_ source: String, done: @escaping (Result<UIImage, Error>) -> Void) {
        if let img = cached(source) { done(.success(img)); return }
        if waiting[source] != nil { waiting[source]!.append(done); return }
        waiting[source] = [done]
        let finish: @Sendable (Result<UIImage, Error>) -> Void = { r in
            DispatchQueue.main.async {
                if case .success(let img) = r {
                    let px = img.size.width * img.size.height * img.scale * img.scale * 4
                    self.cache.setObject(img, forKey: source as NSString, cost: Int(px))
                }
                let fs = self.waiting.removeValue(forKey: source) ?? []
                for f in fs { f(r) }
            }
        }
        if source.hasPrefix("http://") || source.hasPrefix("https://"), let url = URL(string: source) {
            let disk = NuiDiskCache.images
            queue.async {
                if let d = disk.read(source), case .success(let img) = Self.decode(d) {
                    finish(.success(img))
                    return
                }
                URLSession.shared.dataTask(with: url) { data, resp, err in
                    if let err { finish(.failure(err)); return }
                    if let h = resp as? HTTPURLResponse {
                        if !(200..<300).contains(h.statusCode) { finish(.failure(NuiImageError("HTTP \(h.statusCode)"))); return }
                        let r = Self.decode(data)
                        if case .success = r, let data, NuiDiskCache.storable(url, h) { disk.write(source, data) }
                        finish(r)
                        return
                    }
                    finish(Self.decode(data))
                }.resume()
            }
            return
        }
        queue.async {
            // A file (a picked photo), or one of the app's assets.
            let url = source.hasPrefix("/") ? URL(fileURLWithPath: source)
                : source.hasPrefix("file://") ? URL(string: source)
                : Bundle.main.resourceURL?.appendingPathComponent("assets").appendingPathComponent(source)
            finish(Self.decode(url.flatMap { try? Data(contentsOf: $0) }))
        }
    }

    private static func decode(_ data: Data?) -> Result<UIImage, Error> {
        // One point per pixel, as on the web; decoded now, off the main thread.
        guard let data, let img = UIImage(data: data, scale: 1) else { return .failure(NuiImageError("can't decode the image")) }
        return .success(img.preparingForDisplay() ?? img)
    }
}

/// Files kept across launches in the app's caches (which the system may
/// clear), the least recently used going first past a limit.
final class NuiDiskCache: @unchecked Sendable {
    /// Images fetched over http(s), up to 256 MB.
    static let images = NuiDiskCache("nui-images", limit: 256 << 20)

    let dir: URL
    let limit: Int
    private let lock = NSLock()
    private var trimming = false

    init(_ name: String, limit: Int) {
        dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent(name, isDirectory: true)
        self.limit = limit
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    private func file(_ key: String) -> URL {
        dir.appendingPathComponent(SHA256.hash(data: Data(key.utf8)).map { String(format: "%02x", $0) }.joined())
    }

    func read(_ key: String) -> Data? {
        let f = file(key)
        guard let d = try? Data(contentsOf: f) else { return nil }
        try? FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: f.path) // used now
        return d
    }

    func write(_ key: String, _ data: Data) {
        try? data.write(to: file(key), options: .atomic)
        lock.lock()
        let start = !trimming
        trimming = true
        lock.unlock()
        // Once a launch, after it has written: what's past the limit goes.
        if start { DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 5) { self.trim() } }
    }

    func trim() {
        let keys: [URLResourceKey] = [.fileSizeKey, .contentModificationDateKey]
        guard let files = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: keys) else { return }
        var list = files.compactMap { f -> (URL, Int, Date)? in
            guard let v = try? f.resourceValues(forKeys: Set(keys)) else { return nil }
            return (f, v.fileSize ?? 0, v.contentModificationDate ?? .distantPast)
        }
        var total = list.reduce(0) { $0 + $1.1 }
        list.sort { $0.2 < $1.2 }
        for (f, size, _) in list where total > limit {
            try? FileManager.default.removeItem(at: f)
            total -= size
        }
    }

    /// Whether a response may be kept: not one that says not to, nor one
    /// from this machine (nui dev's assets, which change).
    static func storable(_ url: URL, _ resp: HTTPURLResponse) -> Bool {
        if let cc = resp.value(forHTTPHeaderField: "Cache-Control")?.lowercased(), cc.contains("no-store") { return false }
        let h = url.host ?? ""
        return !(h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "10.0.2.2")
    }
}

struct NuiImageError: LocalizedError {
    let message: String
    init(_ m: String) { message = m }
    var errorDescription: String? { message }
}

// Native views

/// A value of a native view's prop or event.
public enum NuiNativeValue {
    case bool(Bool), int(Int64), float(Double), string(String)

    public var bool: Bool { if case .bool(let v) = self { return v }; return false }
    public var int: Int64 {
        switch self { case .int(let v): return v; case .float(let v): return Int64(v); default: return 0 }
    }
    public var float: Double {
        switch self { case .float(let v): return v; case .int(let v): return Double(v); default: return 0 }
    }
    public var string: String { if case .string(let v) = self { return v }; return "" }
}

/// A view an app registers for ui.Native.
@MainActor public protocol NuiNativeView: AnyObject {
    var view: UIView { get }
    func set(_ name: String, _ value: NuiNativeValue)
}

/// What a native view is given: a way to send its events.
@MainActor public final class NuiNativeContext {
    public let id: UInt32
    init(id: UInt32) { self.id = id }

    /// Sends an event to the view's ui.NativeProps.On handler.
    public func emit(_ name: String, _ values: [String: NuiNativeValue] = [:]) {
        var w = NuiWriter()
        w.string(name)
        w.u32(UInt32(values.count))
        for (k, v) in values { writeNative(&w, k, v) }
        let id = self.id
        let b = w.bytes
        NuiRuntime.shared.emit(id, .native) { NuiEvents.native(id, b) }
    }
}

func writeNative(_ w: inout NuiWriter, _ k: String, _ v: NuiNativeValue) {
    w.string(k)
    switch v {
    case .bool(let b): w.u8(1); w.u8(b ? 1 : 0)
    case .int(let i): w.u8(2); w.u64(UInt64(bitPattern: i))
    case .float(let f): w.u8(3); w.f64(f)
    case .string(let s): w.u8(4); w.string(s)
    }
}

/// Apps register native views from a class named NuiAppNatives that
/// conforms to this (the CLI compiles the app's ios/*.swift with the host).
@objc public protocol NuiNativeModule {
    @MainActor static func register()
}

/// Holds a native view, which fills it; it measures as the native view.
final class NuiNativeHostView: UIView {
    var content: UIView? {
        didSet {
            oldValue?.removeFromSuperview()
            guard let c = content else { return }
            c.frame = bounds
            c.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            addSubview(c)
        }
    }
    override func sizeThatFits(_ size: CGSize) -> CGSize { content?.sizeThatFits(size) ?? .zero }
}

// Gestures

/// Lets a view's pan and pinch recognize together.
final class NuiGestureDelegate: NSObject, UIGestureRecognizerDelegate {
    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool {
        g.view === other.view
    }
}

@MainActor extension NuiRuntime {
    func registerNatives() {
        (NSClassFromString("NuiAppNatives") as? NuiNativeModule.Type)?.register()
    }

    /// Registers a native view kind for ui.Native.
    public func registerNative(_ type: String, _ make: @escaping (NuiNativeContext) -> NuiNativeView) {
        nativeFactories[type] = make
    }

    func setMediaProp(_ n: NuiNode, _ prop: UInt16, _ v: NuiValue) -> Bool {
        switch (n.kind, prop) {
        case (.datePicker, NuiProp.DatePicker.value):
            (n.view as? NuiDatePickerView)?.picker.date = Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.datePicker, NuiProp.DatePicker.mode):
            guard let d = (n.view as? NuiDatePickerView)?.picker else { return true }
            switch NuiDateMode(rawValue: v.enumValue) {
            case .time: d.datePickerMode = .time
            case .dateTime: d.datePickerMode = .dateAndTime
            default: d.datePickerMode = .date
            }
        case (.datePicker, NuiProp.DatePicker.min):
            (n.view as? NuiDatePickerView)?.picker.minimumDate = v.int == 0 ? nil : Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.datePicker, NuiProp.DatePicker.max):
            (n.view as? NuiDatePickerView)?.picker.maximumDate = v.int == 0 ? nil : Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.segmented, NuiProp.Segmented.segments):
            guard case .bytes(let b) = v, let s = n.view as? UISegmentedControl else { return true }
            var r = NuiReader(b)
            let selected = s.selectedSegmentIndex
            s.removeAllSegments()
            if b.count >= 4 {
                for i in 0..<Int(r.u32()) { s.insertSegment(withTitle: r.string(), at: i, animated: false) }
            }
            s.selectedSegmentIndex = selected < s.numberOfSegments ? selected : UISegmentedControl.noSegment
        case (.segmented, NuiProp.Segmented.selected):
            (n.view as? UISegmentedControl)?.selectedSegmentIndex = Int(v.int)
        case (.progress, NuiProp.Progress.value):
            guard let p = n.view as? UIProgressView else { return true }
            p.setProgress(Float(v.float), animated: p.window != nil)
        case (.progress, NuiProp.Progress.indeterminate):
            break // UIKit's bar has no such state: its track shows
        case (.symbol, NuiProp.Symbol.name):
            guard let s = n.view as? NuiSymbolView else { return true }
            s.node = n
            s.name = v.string
        case (.video, NuiProp.Video.source):
            (n.view as? NuiVideoView)?.load(source: v.string)
        case (.video, NuiProp.Video.mode):
            (n.view as? NuiVideoView)?.vc.videoGravity = NuiImageMode(rawValue: v.enumValue) == .fill ? .resizeAspectFill : .resizeAspect
        case (.video, NuiProp.Video.hideControls):
            (n.view as? NuiVideoView)?.vc.showsPlaybackControls = !v.bool
        case (.video, NuiProp.Video.autoPlay):
            (n.view as? NuiVideoView)?.autoPlay = v.bool
        case (.video, NuiProp.Video.loop):
            (n.view as? NuiVideoView)?.loop = v.bool
        case (.video, NuiProp.Video.muted):
            (n.view as? NuiVideoView)?.player.isMuted = v.bool
        case (.webView, NuiProp.WebView.url):
            (n.view as? NuiWebView)?.load(source: v.string)
        case (.webView, NuiProp.WebView.html):
            if !v.string.isEmpty { (n.view as? NuiWebView)?.loadHTMLString(v.string, baseURL: nil) }
        case (.image, NuiProp.Image.source):
            loadImage(n, v.string)
        case (.image, NuiProp.Image.placeholder):
            n.imagePlaceholder = v.string
            if !n.imageLoaded { (n.view as? UIImageView)?.image = nil }
            showPlaceholder(n)
        case (.image, NuiProp.Image.mode):
            let iv = n.view as! UIImageView
            switch NuiImageMode(rawValue: v.enumValue) {
            case .fill: iv.contentMode = .scaleAspectFill; iv.clipsToBounds = true
            case .stretch: iv.contentMode = .scaleToFill
            case .center: iv.contentMode = .center
            default: iv.contentMode = .scaleAspectFit
            }
        case (.native, NuiProp.Native.type):
            guard let make = nativeFactories[v.string] else {
                print("nui: no native view \"\(v.string)\" is registered")
                return true
            }
            let nv = make(NuiNativeContext(id: n.id))
            n.native = nv
            (n.view as? NuiNativeHostView)?.content = nv.view
        case (.richText, NuiProp.RichText.content):
            guard case .bytes(let b) = v else { return true }
            (n.view as? NuiRichTextView)?.setContent(b)
        case (.richText, NuiProp.RichText.inlines):
            guard case .bytes(let b) = v else { return true }
            (n.view as? NuiRichTextView)?.setInlines(b)
        case (.native, NuiProp.Native.prop):
            guard case .bytes(let b) = v, b.count >= 5 else { return true }
            var r = NuiReader(b)
            let name = r.string()
            let value: NuiNativeValue
            switch r.u8() {
            case 1: value = .bool(r.u8() != 0)
            case 2: value = .int(Int64(bitPattern: r.u64()))
            case 3: value = .float(r.f64())
            default: value = .string(r.string())
            }
            n.native?.set(name, value)
        default:
            return false
        }
        return true
    }

    func loadImage(_ n: NuiNode, _ source: String) {
        n.imageSource = source
        n.imageLoaded = false
        let iv = n.view as! UIImageView
        let id = n.id
        if source.isEmpty { iv.image = nil; showPlaceholder(n); return }
        let show = { [weak self] (r: Result<UIImage, Error>) in
            guard let self, let n = self.nodes[id], n.imageSource == source else { return }
            switch r {
            case .success(let img):
                n.imageLoaded = true
                iv.image = n.color != 0 ? img.withRenderingMode(.alwaysTemplate) : img
                let w = Double(img.size.width * img.scale), h = Double(img.size.height * img.scale)
                self.emit(id, .loaded) { NuiEvents.loaded(id, x: w, y: h) }
            case .failure(let e):
                let m = e.localizedDescription
                self.emit(id, .failed) { NuiEvents.failed(id, m) }
            }
        }
        if let img = NuiImageLoader.shared.cached(source) {
            show(.success(img))
        } else {
            iv.image = nil
            showPlaceholder(n)
            NuiImageLoader.shared.load(source, done: show)
        }
    }

    /// Shows an image's placeholder until (or unless) the image loads.
    func showPlaceholder(_ n: NuiNode) {
        guard !n.imagePlaceholder.isEmpty, !n.imageLoaded, let iv = n.view as? UIImageView else { return }
        let id = n.id, p = n.imagePlaceholder
        NuiImageLoader.shared.load(p) { [weak self] r in
            guard let self, let n = self.nodes[id], !n.imageLoaded, n.imagePlaceholder == p, case .success(let img) = r else { return }
            iv.image = n.color != 0 ? img.withRenderingMode(.alwaysTemplate) : img
        }
    }

    func listenGesture(_ n: NuiNode, _ e: NuiEventKind) {
        let v = n.view
        switch e {
        case .pan:
            let g = UIPanGestureRecognizer(target: self, action: #selector(panned(_:)))
            g.delegate = gestureDelegate
            v.addGestureRecognizer(g)
        case .pinch:
            let g = UIPinchGestureRecognizer(target: self, action: #selector(pinched(_:)))
            g.delegate = gestureDelegate
            v.addGestureRecognizer(g)
        case .swipe:
            for d: UISwipeGestureRecognizer.Direction in [.right, .left, .up, .down] {
                let g = UISwipeGestureRecognizer(target: self, action: #selector(swiped(_:)))
                g.direction = d
                g.delegate = gestureDelegate
                v.addGestureRecognizer(g)
            }
        default:
            break
        }
    }

    private func gestureState(_ g: UIGestureRecognizer) -> Int32? {
        switch g.state {
        case .began: return NuiGestureState.began.rawValue
        case .changed: return NuiGestureState.changed.rawValue
        case .ended: return NuiGestureState.ended.rawValue
        case .cancelled, .failed: return NuiGestureState.cancelled.rawValue
        default: return nil
        }
    }

    @objc func panned(_ g: UIPanGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = NuiGesture()
        e.state = s
        let p = g.location(in: v), t = g.translation(in: nil), vel = g.velocity(in: nil)
        e.x = Float(p.x); e.y = Float(p.y)
        e.translationX = Float(t.x); e.translationY = Float(t.y)
        e.velocityX = Float(vel.x); e.velocityY = Float(vel.y)
        emit(id, .pan) { NuiEvents.pan(id, e) }
    }

    @objc func pinched(_ g: UIPinchGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = NuiGesture()
        e.state = s
        let p = g.location(in: v)
        e.x = Float(p.x); e.y = Float(p.y)
        e.scale = Float(g.scale)
        e.velocityX = Float(g.velocity)
        emit(id, .pinch) { NuiEvents.pinch(id, e) }
    }

    @objc func swiped(_ g: UISwipeGestureRecognizer) {
        guard g.state == .ended, let id = nodeID(of: g.view) else { return }
        let d: Int64
        switch g.direction {
        case .left: d = 2
        case .up: d = 4
        case .down: d = 8
        default: d = 1
        }
        emit(id, .swipe) { NuiEvents.swipe(id, d) }
    }

    // Animation: between AnimateBegin and AnimateEnd, frames and visual
    // props change in UIView animations.

    func beginAnimation(_ args: UnsafeRawBufferPointer) {
        guard args.count >= 5 else { return }
        var r = NuiReader(args)
        animation = (Double(r.f32()), Int32(r.u8()))
        repeating = nil
        if args.count >= 10 {
            let n = Int32(bitPattern: r.u32()), auto = r.u8() != 0
            if n != 0 || auto { repeating = (n, auto) }
        }
    }

    /// Runs change, animated if an animation is on and the view is shown.
    func animated(_ view: UIView, _ change: @escaping () -> Void) {
        guard let (duration, curve) = animation, view.window != nil, view.bounds.size != .zero else {
            // A change without an animation stops a repeating one.
            if repeatingViews.remove(ObjectIdentifier(view)) != nil { view.layer.removeAllAnimations() }
            change()
            return
        }
        var change = change
        var rep: UIView.AnimationOptions = []
        if let (n, auto) = repeating {
            repeatingViews.insert(ObjectIdentifier(view))
            view.layer.removeAllAnimations()
            if n < 0 {
                rep = auto ? [.repeat, .autoreverse] : [.repeat]
            } else {
                let inner = change
                change = {
                    UIView.modifyAnimations(withRepeatCount: CGFloat(n + 1), autoreverses: auto, animations: inner)
                }
            }
        }
        let ui = UIView.self
        switch NuiAnimationCurve(rawValue: curve) {
        case .spring:
            ui.animate(withDuration: duration, delay: 0, usingSpringWithDamping: 0.85, initialSpringVelocity: 0,
                       options: [.beginFromCurrentState, .allowUserInteraction, rep], animations: change)
        default:
            let c: UIView.AnimationOptions
            switch NuiAnimationCurve(rawValue: curve) {
            case .linear: c = .curveLinear
            case .easeIn: c = .curveEaseIn
            case .easeOut: c = .curveEaseOut
            default: c = .curveEaseInOut
            }
            ui.animate(withDuration: duration, delay: 0, options: [c, .beginFromCurrentState, .allowUserInteraction, rep], animations: change)
        }
    }
}
#endif
