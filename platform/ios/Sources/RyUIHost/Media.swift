#if canImport(UIKit)
import UIKit
import UIKit.UIGestureRecognizerSubclass
import CommonCrypto
#if canImport(RyUIOps)
import RyUIOps
#endif

// Images

/// Loads images off the main thread: fetched (http, https; kept on disk,
/// RyUIDiskCache) or read (the app's assets), decoded and prepared for
/// display there, then cached in memory.
final class RyUIImageLoader: @unchecked Sendable {
    static let shared = RyUIImageLoader()
    private let cache = NSCache<NSString, UIImage>()
    private let queue = DispatchQueue(label: "ryui.images", qos: .userInitiated, attributes: .concurrent)
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
            let disk = RyUIDiskCache.images
            queue.async {
                if let d = disk.read(source), case .success(let img) = Self.decode(d) {
                    finish(.success(img))
                    return
                }
                URLSession.shared.dataTask(with: url) { data, resp, err in
                    if let err { finish(.failure(err)); return }
                    if let h = resp as? HTTPURLResponse {
                        if !(200..<300).contains(h.statusCode) { finish(.failure(RyUIImageError("HTTP \(h.statusCode)"))); return }
                        let r = Self.decode(data)
                        if case .success = r, let data, RyUIDiskCache.storable(url, h) { disk.write(source, data) }
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
        guard let data, let img = UIImage(data: data, scale: 1) else { return .failure(RyUIImageError("can't decode the image")) }
        return .success(img.preparingForDisplay() ?? img)
    }
}

/// Files kept across launches in the app's caches (which the system may
/// clear), the least recently used going first past a limit.
final class RyUIDiskCache: @unchecked Sendable {
    /// Images fetched over http(s), up to 256 MB.
    static let images = RyUIDiskCache("ryui-images", limit: 256 << 20)

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
        var h = [UInt8](repeating: 0, count: Int(CC_SHA256_DIGEST_LENGTH))
        let k = Array(key.utf8)
        CC_SHA256(k, CC_LONG(k.count), &h)
        return dir.appendingPathComponent(h.map { String(format: "%02x", $0) }.joined())
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
    /// from this machine (ryui dev's assets, which change).
    static func storable(_ url: URL, _ resp: HTTPURLResponse) -> Bool {
        if let cc = resp.value(forHTTPHeaderField: "Cache-Control")?.lowercased(), cc.contains("no-store") { return false }
        let h = url.host ?? ""
        return !(h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "10.0.2.2")
    }
}

struct RyUIImageError: LocalizedError {
    let message: String
    init(_ m: String) { message = m }
    var errorDescription: String? { message }
}

// Native views

/// A value of a native view's prop or event.
public enum RyUINativeValue {
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
@MainActor public protocol RyUINativeView: AnyObject {
    var view: UIView { get }
    func set(_ name: String, _ value: RyUINativeValue)
}

/// What a native view is given: a way to send its events.
@MainActor public final class RyUINativeContext {
    public let id: UInt32
    init(id: UInt32) { self.id = id }

    /// Sends an event to the view's ui.NativeProps.On handler.
    public func emit(_ name: String, _ values: [String: RyUINativeValue] = [:]) {
        var w = RyUIWriter()
        w.string(name)
        w.u32(UInt32(values.count))
        for (k, v) in values { writeNative(&w, k, v) }
        let id = self.id
        let b = w.bytes
        RyUIRuntime.shared.emit(id, .native) { RyUIEvents.native(id, b) }
    }
}

func writeNative(_ w: inout RyUIWriter, _ k: String, _ v: RyUINativeValue) {
    w.string(k)
    switch v {
    case .bool(let b): w.u8(1); w.u8(b ? 1 : 0)
    case .int(let i): w.u8(2); w.u64(UInt64(bitPattern: i))
    case .float(let f): w.u8(3); w.f64(f)
    case .string(let s): w.u8(4); w.string(s)
    }
}

/// Apps register native views from a class named RyUIAppNatives that
/// conforms to this (the CLI compiles the app's ios/*.swift with the host).
@objc public protocol RyUINativeModule {
    @MainActor static func register()
}

/// Holds a native view, which fills it; it measures as the native view.
final class RyUINativeHostView: UIView {
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
/// A pan whose translation counts from where the finger came down, as
/// Android's does: UIKit's starts at zero only once the finger has moved far
/// enough to be a pan, about 10 points later, and an app drawing from
/// location minus translation would start in the wrong place.
final class RyUIPanRecognizer: UIPanGestureRecognizer {
    private var down = CGPoint.zero
    /// What UIKit's translation leaves out, from the moment it began.
    var slop = CGPoint.zero

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        if numberOfTouches == 0, let t = touches.first { down = t.location(in: nil) }
        super.touchesBegan(touches, with: event)
    }

    func began() {
        let at = location(in: nil), t = translation(in: nil)
        slop = CGPoint(x: at.x - down.x - t.x, y: at.y - down.y - t.y)
    }
}

final class RyUIGestureDelegate: NSObject, UIGestureRecognizerDelegate {
    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool {
        g.view === other.view
    }
}

@MainActor extension RyUIRuntime {
    func registerNatives() {
        #if RYUI_CAMERAVIEW
        registerNative("ryui.camera") { RyUICameraView($0) }
        #endif
        #if RYUI_MAPS
        registerNative("ryui.map") { RyUIMapView($0) }
        #endif
        #if RYUI_APPLEAUTH
        registerNative("ryui.appleSignIn") { RyUIAppleSignInButton($0) }
        #endif
        (NSClassFromString("RyUIAppNatives") as? RyUINativeModule.Type)?.register()
    }

    /// Registers a native view kind for ui.Native.
    public func registerNative(_ type: String, _ make: @escaping (RyUINativeContext) -> RyUINativeView) {
        nativeFactories[type] = make
    }

    func setMediaProp(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) -> Bool {
        switch (n.kind, prop) {
        case (.datePicker, RyUIProp.DatePicker.value):
            (n.view as? RyUIDatePickerView)?.picker.date = Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.datePicker, RyUIProp.DatePicker.mode):
            guard let d = (n.view as? RyUIDatePickerView)?.picker else { return true }
            switch RyUIDateMode(rawValue: v.enumValue) {
            case .time: d.datePickerMode = .time
            case .dateTime: d.datePickerMode = .dateAndTime
            default: d.datePickerMode = .date
            }
        case (.datePicker, RyUIProp.DatePicker.min):
            (n.view as? RyUIDatePickerView)?.picker.minimumDate = v.int == 0 ? nil : Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.datePicker, RyUIProp.DatePicker.max):
            (n.view as? RyUIDatePickerView)?.picker.maximumDate = v.int == 0 ? nil : Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.segmented, RyUIProp.Segmented.segments):
            guard case .bytes(let b) = v, let s = n.view as? UISegmentedControl else { return true }
            var r = RyUIReader(b)
            let selected = s.selectedSegmentIndex
            s.removeAllSegments()
            if b.count >= 4 {
                for i in 0..<Int(r.u32()) { s.insertSegment(withTitle: r.string(), at: i, animated: false) }
            }
            s.selectedSegmentIndex = selected < s.numberOfSegments ? selected : UISegmentedControl.noSegment
        case (.segmented, RyUIProp.Segmented.selected):
            (n.view as? UISegmentedControl)?.selectedSegmentIndex = Int(v.int)
        case (.progress, RyUIProp.Progress.value):
            guard let p = n.view as? UIProgressView else { return true }
            p.setProgress(Float(v.float), animated: p.window != nil)
        case (.progress, RyUIProp.Progress.indeterminate):
            break // UIKit's bar has no such state: its track shows
        case (.symbol, RyUIProp.Symbol.name):
            guard let s = n.view as? RyUISymbolView else { return true }
            s.node = n
            s.name = v.string
        #if RYUI_VIDEO
        case (.video, RyUIProp.Video.source):
            (n.view as? RyUIVideoView)?.load(source: v.string)
        case (.video, RyUIProp.Video.mode):
            (n.view as? RyUIVideoView)?.vc.videoGravity = RyUIImageMode(rawValue: v.enumValue) == .fill ? .resizeAspectFill : .resizeAspect
        case (.video, RyUIProp.Video.hideControls):
            (n.view as? RyUIVideoView)?.vc.showsPlaybackControls = !v.bool
        case (.video, RyUIProp.Video.autoPlay):
            (n.view as? RyUIVideoView)?.autoPlay = v.bool
        case (.video, RyUIProp.Video.loop):
            (n.view as? RyUIVideoView)?.loop = v.bool
        case (.video, RyUIProp.Video.muted):
            (n.view as? RyUIVideoView)?.player.isMuted = v.bool
        #endif
        #if RYUI_WEBVIEW
        case (.webView, RyUIProp.WebView.url):
            (n.view as? RyUIWebView)?.load(source: v.string)
        case (.webView, RyUIProp.WebView.html):
            if !v.string.isEmpty { (n.view as? RyUIWebView)?.loadHTMLString(v.string, baseURL: nil) }
        case (.webView, RyUIProp.WebView.preload):
            (n.view as? RyUIWebView)?.setPreload(v.string)
        case (.webView, RyUIProp.WebView.userAgent):
            (n.view as? RyUIWebView)?.customUserAgent = v.string.isEmpty ? nil : v.string
        case (.webView, RyUIProp.WebView.block):
            if case .bytes(let b) = v { (n.view as? RyUIWebView)?.setBlock(b) }
        case (.webView, RyUIProp.WebView.requests):
            break // iOS can't see a page's requests
        #endif
        case (.image, RyUIProp.Image.source):
            loadImage(n, v.string)
        case (.image, RyUIProp.Image.placeholder):
            n.imagePlaceholder = v.string
            if !n.imageLoaded { (n.view as? UIImageView)?.image = nil }
            showPlaceholder(n)
        case (.image, RyUIProp.Image.mode):
            let iv = n.view as! UIImageView
            switch RyUIImageMode(rawValue: v.enumValue) {
            case .fill: iv.contentMode = .scaleAspectFill; iv.clipsToBounds = true
            case .stretch: iv.contentMode = .scaleToFill
            case .center: iv.contentMode = .center
            default: iv.contentMode = .scaleAspectFit
            }
        case (.native, RyUIProp.Native.type):
            guard let make = nativeFactories[v.string] else {
                print("ryui: no native view \"\(v.string)\" is registered")
                return true
            }
            let nv = make(RyUINativeContext(id: n.id))
            n.native = nv
            (n.view as? RyUINativeHostView)?.content = nv.view
        case (.richText, RyUIProp.RichText.content):
            guard case .bytes(let b) = v else { return true }
            (n.view as? RyUIRichTextView)?.setContent(b)
        case (.richText, RyUIProp.RichText.inlines):
            guard case .bytes(let b) = v else { return true }
            (n.view as? RyUIRichTextView)?.setInlines(b)
        case (.native, RyUIProp.Native.prop):
            guard case .bytes(let b) = v, b.count >= 5 else { return true }
            var r = RyUIReader(b)
            let name = r.string()
            let value: RyUINativeValue
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

    func loadImage(_ n: RyUINode, _ source: String) {
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
                self.emit(id, .loaded) { RyUIEvents.loaded(id, x: w, y: h) }
            case .failure(let e):
                let m = e.localizedDescription
                self.emit(id, .failed) { RyUIEvents.failed(id, m) }
            }
        }
        if let img = RyUIImageLoader.shared.cached(source) {
            show(.success(img))
        } else {
            iv.image = nil
            showPlaceholder(n)
            RyUIImageLoader.shared.load(source, done: show)
        }
    }

    /// Shows an image's placeholder until (or unless) the image loads.
    func showPlaceholder(_ n: RyUINode) {
        guard !n.imagePlaceholder.isEmpty, !n.imageLoaded, let iv = n.view as? UIImageView else { return }
        let id = n.id, p = n.imagePlaceholder
        RyUIImageLoader.shared.load(p) { [weak self] r in
            guard let self, let n = self.nodes[id], !n.imageLoaded, n.imagePlaceholder == p, case .success(let img) = r else { return }
            iv.image = n.color != 0 ? img.withRenderingMode(.alwaysTemplate) : img
        }
    }

    func listenGesture(_ n: RyUINode, _ e: RyUIEventKind) {
        let v = n.view
        switch e {
        case .pan:
            let g = RyUIPanRecognizer(target: self, action: #selector(panned(_:)))
            g.delegate = gestureDelegate
            v.addGestureRecognizer(g)
        case .pinch:
            let g = UIPinchGestureRecognizer(target: self, action: #selector(pinched(_:)))
            g.delegate = gestureDelegate
            v.addGestureRecognizer(g)
        case .rotate:
            let g = UIRotationGestureRecognizer(target: self, action: #selector(rotated(_:)))
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
        case .began: return RyUIGestureState.began.rawValue
        case .changed: return RyUIGestureState.changed.rawValue
        case .ended: return RyUIGestureState.ended.rawValue
        case .cancelled, .failed: return RyUIGestureState.cancelled.rawValue
        default: return nil
        }
    }

    @objc func panned(_ g: UIPanGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = RyUIGesture()
        e.state = s
        var slop = CGPoint.zero
        if let r = g as? RyUIPanRecognizer {
            if g.state == .began { r.began() }
            slop = r.slop
        }
        let p = g.location(in: v), t = g.translation(in: nil), vel = g.velocity(in: nil)
        e.x = Float(p.x); e.y = Float(p.y)
        e.translationX = Float(t.x + slop.x); e.translationY = Float(t.y + slop.y)
        e.velocityX = Float(vel.x); e.velocityY = Float(vel.y)
        emit(id, .pan) { RyUIEvents.pan(id, e) }
    }

    @objc func pinched(_ g: UIPinchGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = RyUIGesture()
        e.state = s
        let p = g.location(in: v)
        e.x = Float(p.x); e.y = Float(p.y)
        e.scale = Float(g.scale)
        e.velocityX = Float(g.velocity)
        emit(id, .pinch) { RyUIEvents.pinch(id, e) }
    }

    @objc func rotated(_ g: UIRotationGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = RyUIGesture()
        e.state = s
        let p = g.location(in: v)
        e.x = Float(p.x); e.y = Float(p.y)
        e.rotation = Float(g.rotation)
        e.velocityX = Float(g.velocity)
        emit(id, .rotate) { RyUIEvents.rotate(id, e) }
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
        emit(id, .swipe) { RyUIEvents.swipe(id, d) }
    }

    // Animation: between AnimateBegin and AnimateEnd, frames and visual
    // props change in UIView animations.

    func beginAnimation(_ args: UnsafeRawBufferPointer) {
        guard args.count >= 5 else { return }
        var r = RyUIReader(args)
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
        animate((duration, curve), options: [.beginFromCurrentState, .allowUserInteraction, rep], change)
    }
}
#endif
