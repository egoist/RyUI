#if canImport(UIKit)
import UIKit
import UIKit.UIGestureRecognizerSubclass
import CommonCrypto
#if canImport(AsukaOps)
import AsukaOps
#endif

// Images

/// Loads images off the main thread: fetched (http, https; kept on disk,
/// AsukaDiskCache) or read (the app's assets), decoded and prepared for
/// display there, then cached in memory.
final class AsukaImageLoader: @unchecked Sendable {
    static let shared = AsukaImageLoader()
    private let cache = NSCache<NSString, UIImage>()
    private let queue = DispatchQueue(label: "asuka.images", qos: .userInitiated, attributes: .concurrent)
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
            let disk = AsukaDiskCache.images
            queue.async {
                if let d = disk.read(source), case .success(let img) = Self.decode(d) {
                    finish(.success(img))
                    return
                }
                URLSession.shared.dataTask(with: url) { data, resp, err in
                    if let err { finish(.failure(err)); return }
                    if let h = resp as? HTTPURLResponse {
                        if !(200..<300).contains(h.statusCode) { finish(.failure(AsukaImageError("HTTP \(h.statusCode)"))); return }
                        let r = Self.decode(data)
                        if case .success = r, let data, AsukaDiskCache.storable(url, h) { disk.write(source, data) }
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
                : asukaAssets?.appendingPathComponent(source)
            finish(Self.decode(url.flatMap { try? Data(contentsOf: $0) }))
        }
    }

    private static func decode(_ data: Data?) -> Result<UIImage, Error> {
        // One point per pixel, as on the web; decoded now, off the main thread.
        guard let data, let img = UIImage(data: data, scale: 1) else { return .failure(AsukaImageError("can't decode the image")) }
        return .success(img.preparingForDisplay() ?? img)
    }
}

/// Files kept across launches in the app's caches (which the system may
/// clear), the least recently used going first past a limit.
final class AsukaDiskCache: @unchecked Sendable {
    /// Images fetched over http(s), up to 256 MB.
    static let images = AsukaDiskCache("asuka-images", limit: 256 << 20)

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
    /// from this machine (asuka dev's assets, which change).
    static func storable(_ url: URL, _ resp: HTTPURLResponse) -> Bool {
        if let cc = resp.value(forHTTPHeaderField: "Cache-Control")?.lowercased(), cc.contains("no-store") { return false }
        let h = url.host ?? ""
        return !(h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "10.0.2.2")
    }
}

struct AsukaImageError: LocalizedError {
    let message: String
    init(_ m: String) { message = m }
    var errorDescription: String? { message }
}

// Native views

/// A value of a native view's prop or event.
public enum AsukaNativeValue {
    case bool(Bool), int(Int64), float(Double), string(String)
    /// A typed view's prop or event (//asuka:view): its values, encoded.
    case bytes(Data)

    public var bool: Bool { if case .bool(let v) = self { return v }; return false }
    public var int: Int64 {
        switch self { case .int(let v): return v; case .float(let v): return Int64(v); default: return 0 }
    }
    public var float: Double {
        switch self { case .float(let v): return v; case .int(let v): return Double(v); default: return 0 }
    }
    public var string: String { if case .string(let v) = self { return v }; return "" }
    public var bytes: Data { if case .bytes(let v) = self { return v }; return Data() }
}

/// A view an app registers for ui.Native.
@MainActor public protocol AsukaNativeView: AnyObject {
    var view: UIView { get }
    func set(_ name: String, _ value: AsukaNativeValue)
}

/// What a native view is given: a way to send its events.
@MainActor public final class AsukaNativeContext {
    public let id: UInt32
    init(id: UInt32) { self.id = id }

    /// Sends an event to the view's ui.NativeProps.On handler.
    public func emit(_ name: String, _ values: [String: AsukaNativeValue] = [:]) {
        var w = AsukaWriter()
        w.string(name)
        w.u32(UInt32(values.count))
        for (k, v) in values { writeNative(&w, k, v) }
        let id = self.id
        let b = w.bytes
        AsukaRuntime.shared.emit(id, .native) { AsukaEvents.native(id, b) }
    }
}

func writeNative(_ w: inout AsukaWriter, _ k: String, _ v: AsukaNativeValue) {
    w.string(k)
    switch v {
    case .bool(let b): w.u8(1); w.u8(b ? 1 : 0)
    case .int(let i): w.u8(2); w.u64(UInt64(bitPattern: i))
    case .float(let f): w.u8(3); w.f64(f)
    case .string(let s): w.u8(4); w.string(s)
    case .bytes(let d): w.u8(5); w.u32(UInt32(d.count)); w.bytes.append(contentsOf: d)
    }
}

/// Holds a native view, which fills it; it measures as the native view.
final class AsukaNativeHostView: UIView {
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
final class AsukaPanRecognizer: UIPanGestureRecognizer {
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

final class AsukaGestureDelegate: NSObject, UIGestureRecognizerDelegate {
    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool {
        g.view === other.view
    }
}

@MainActor extension AsukaRuntime {
    func registerNatives() {
        #if ASUKA_CAMERAVIEW
        registerNative("asuka.camera") { AsukaCameraView($0) }
        #endif
        #if ASUKA_MAPS
        registerNative("asuka.map") { AsukaMapView($0) }
        #endif
        #if ASUKA_APPLEAUTH
        registerNative("asuka.appleSignIn") { AsukaAppleSignInButton($0) }
        #endif
        #if ASUKA_PAY
        registerNative("asuka.payButton") { AsukaPayButton($0) }
        #endif
        // Packages' typed views are found as they're shown
        // (AsukaNative.makeView).
    }

    /// Registers one of the host's native view kinds for ui.Native.
    func registerNative(_ type: String, _ make: @escaping (AsukaNativeContext) -> AsukaNativeView) {
        nativeFactories[type] = make
    }

    func setMediaProp(_ n: AsukaNode, _ prop: UInt16, _ v: AsukaValue) -> Bool {
        switch (n.kind, prop) {
        case (.datePicker, AsukaProp.DatePicker.value):
            (n.view as? AsukaDatePickerView)?.picker.date = Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.datePicker, AsukaProp.DatePicker.mode):
            guard let d = (n.view as? AsukaDatePickerView)?.picker else { return true }
            switch AsukaDateMode(rawValue: v.enumValue) {
            case .time: d.datePickerMode = .time
            case .dateTime: d.datePickerMode = .dateAndTime
            default: d.datePickerMode = .date
            }
        case (.datePicker, AsukaProp.DatePicker.inline):
            (n.view as? AsukaDatePickerView)?.picker.preferredDatePickerStyle = v.bool ? .inline : .compact
            n.view.invalidateIntrinsicContentSize()
        case (.datePicker, AsukaProp.DatePicker.min):
            (n.view as? AsukaDatePickerView)?.picker.minimumDate = v.int == 0 ? nil : Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.datePicker, AsukaProp.DatePicker.max):
            (n.view as? AsukaDatePickerView)?.picker.maximumDate = v.int == 0 ? nil : Date(timeIntervalSince1970: Double(v.int) / 1000)
        case (.segmented, AsukaProp.Segmented.segments):
            guard case .bytes(let b) = v, let s = n.view as? UISegmentedControl else { return true }
            var r = AsukaReader(b)
            let selected = s.selectedSegmentIndex
            s.removeAllSegments()
            if b.count >= 4 {
                for i in 0..<Int(r.u32()) { s.insertSegment(withTitle: r.string(), at: i, animated: false) }
            }
            s.selectedSegmentIndex = selected < s.numberOfSegments ? selected : UISegmentedControl.noSegment
        case (.segmented, AsukaProp.Segmented.selected):
            (n.view as? UISegmentedControl)?.selectedSegmentIndex = Int(v.int)
        case (.pageControl, AsukaProp.PageControl.count):
            (n.view as? UIPageControl)?.numberOfPages = Int(v.int)
        case (.pageControl, AsukaProp.PageControl.page):
            (n.view as? UIPageControl)?.currentPage = Int(v.int)
        case (.progress, AsukaProp.Progress.value):
            guard let p = n.view as? UIProgressView else { return true }
            p.setProgress(Float(v.float), animated: p.window != nil)
        case (.progress, AsukaProp.Progress.indeterminate):
            break // UIKit's bar has no such state: its track shows
        case (.progressRing, AsukaProp.ProgressRing.value):
            (n.view as? AsukaProgressRing)?.set(CGFloat(v.float), animation)
        case (.progressRing, AsukaProp.ProgressRing.indeterminate):
            (n.view as? AsukaProgressRing)?.indeterminate = v.bool
        case (.symbol, AsukaProp.Symbol.name):
            guard let s = n.view as? AsukaSymbolView else { return true }
            s.node = n
            s.name = v.string
        #if ASUKA_VIDEO
        case (.video, AsukaProp.Video.source):
            (n.view as? AsukaVideoView)?.load(source: v.string)
        case (.video, AsukaProp.Video.mode):
            (n.view as? AsukaVideoView)?.vc.videoGravity = AsukaImageMode(rawValue: v.enumValue) == .fill ? .resizeAspectFill : .resizeAspect
        case (.video, AsukaProp.Video.hideControls):
            (n.view as? AsukaVideoView)?.vc.showsPlaybackControls = !v.bool
        case (.video, AsukaProp.Video.autoPlay):
            (n.view as? AsukaVideoView)?.autoPlay = v.bool
        case (.video, AsukaProp.Video.loop):
            (n.view as? AsukaVideoView)?.loop = v.bool
        case (.video, AsukaProp.Video.muted):
            (n.view as? AsukaVideoView)?.player.isMuted = v.bool
        #endif
        #if ASUKA_WEBVIEW
        case (.webView, AsukaProp.WebView.url):
            (n.view as? AsukaWebView)?.load(source: v.string)
        case (.webView, AsukaProp.WebView.html):
            if !v.string.isEmpty { (n.view as? AsukaWebView)?.loadHTMLString(v.string, baseURL: nil) }
        case (.webView, AsukaProp.WebView.preload):
            (n.view as? AsukaWebView)?.setPreload(v.string)
        case (.webView, AsukaProp.WebView.userAgent):
            (n.view as? AsukaWebView)?.customUserAgent = v.string.isEmpty ? nil : v.string
        case (.webView, AsukaProp.WebView.block):
            if case .bytes(let b) = v { (n.view as? AsukaWebView)?.setBlock(b) }
        case (.webView, AsukaProp.WebView.requests):
            break // iOS can't see a page's requests
        #endif
        case (.image, AsukaProp.Image.source):
            loadImage(n, v.string)
        case (.image, AsukaProp.Image.placeholder):
            n.imagePlaceholder = v.string
            if !n.imageLoaded { (n.view as? UIImageView)?.image = nil }
            showPlaceholder(n)
        case (.image, AsukaProp.Image.mode):
            let iv = n.view as! UIImageView
            switch AsukaImageMode(rawValue: v.enumValue) {
            case .fill: iv.contentMode = .scaleAspectFill; iv.clipsToBounds = true
            case .stretch: iv.contentMode = .scaleToFill
            case .center: iv.contentMode = .center
            default: iv.contentMode = .scaleAspectFit
            }
        case (.native, AsukaProp.Native.type):
            // The host's own kinds, else a package's typed view (its
            // module's class # its name).
            let ctx = AsukaNativeContext(id: n.id)
            guard let nv = nativeFactories[v.string]?(ctx) ?? AsukaNative.makeView(v.string, ctx) else {
                print("asuka: no native view \"\(v.string)\": build the app with its packages' native code")
                return true
            }
            n.native = nv
            (n.view as? AsukaNativeHostView)?.content = nv.view
        case (.richText, AsukaProp.RichText.content):
            guard case .bytes(let b) = v else { return true }
            (n.view as? AsukaRichTextView)?.setContent(b)
        case (.richText, AsukaProp.RichText.inlines):
            guard case .bytes(let b) = v else { return true }
            (n.view as? AsukaRichTextView)?.setInlines(b)
        case (.native, AsukaProp.Native.prop):
            guard case .bytes(let b) = v, b.count >= 5 else { return true }
            var r = AsukaReader(b)
            let name = r.string()
            let value: AsukaNativeValue
            switch r.u8() {
            case 1: value = .bool(r.u8() != 0)
            case 2: value = .int(Int64(bitPattern: r.u64()))
            case 3: value = .float(r.f64())
            case 5: value = .bytes(Data(r.bytes()))
            default: value = .string(r.string())
            }
            n.native?.set(name, value)
        default:
            return false
        }
        return true
    }

    func loadImage(_ n: AsukaNode, _ source: String) {
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
                self.emit(id, .loaded) { AsukaEvents.loaded(id, x: w, y: h) }
            case .failure(let e):
                let m = e.localizedDescription
                self.emit(id, .failed) { AsukaEvents.failed(id, m) }
            }
        }
        if let img = AsukaImageLoader.shared.cached(source) {
            show(.success(img))
        } else {
            iv.image = nil
            showPlaceholder(n)
            AsukaImageLoader.shared.load(source, done: show)
        }
    }

    /// Shows an image's placeholder until (or unless) the image loads.
    func showPlaceholder(_ n: AsukaNode) {
        guard !n.imagePlaceholder.isEmpty, !n.imageLoaded, let iv = n.view as? UIImageView else { return }
        let id = n.id, p = n.imagePlaceholder
        AsukaImageLoader.shared.load(p) { [weak self] r in
            guard let self, let n = self.nodes[id], !n.imageLoaded, n.imagePlaceholder == p, case .success(let img) = r else { return }
            iv.image = n.color != 0 ? img.withRenderingMode(.alwaysTemplate) : img
        }
    }

    func listenGesture(_ n: AsukaNode, _ e: AsukaEventKind) {
        let v = n.view
        switch e {
        case .pan:
            let g = AsukaPanRecognizer(target: self, action: #selector(panned(_:)))
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
        case .began: return AsukaGestureState.began.rawValue
        case .changed: return AsukaGestureState.changed.rawValue
        case .ended: return AsukaGestureState.ended.rawValue
        case .cancelled, .failed: return AsukaGestureState.cancelled.rawValue
        default: return nil
        }
    }

    @objc func panned(_ g: UIPanGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = AsukaGesture()
        e.state = s
        var slop = CGPoint.zero
        if let r = g as? AsukaPanRecognizer {
            if g.state == .began { r.began() }
            slop = r.slop
        }
        let p = g.location(in: v), t = g.translation(in: nil), vel = g.velocity(in: nil)
        e.x = Float(p.x); e.y = Float(p.y)
        e.translationX = Float(t.x + slop.x); e.translationY = Float(t.y + slop.y)
        e.velocityX = Float(vel.x); e.velocityY = Float(vel.y)
        emit(id, .pan) { AsukaEvents.pan(id, e) }
    }

    @objc func pinched(_ g: UIPinchGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = AsukaGesture()
        e.state = s
        let p = g.location(in: v)
        e.x = Float(p.x); e.y = Float(p.y)
        e.scale = Float(g.scale)
        e.velocityX = Float(g.velocity)
        emit(id, .pinch) { AsukaEvents.pinch(id, e) }
    }

    @objc func rotated(_ g: UIRotationGestureRecognizer) {
        guard let v = g.view, let id = nodeID(of: v), let s = gestureState(g) else { return }
        var e = AsukaGesture()
        e.state = s
        let p = g.location(in: v)
        e.x = Float(p.x); e.y = Float(p.y)
        e.rotation = Float(g.rotation)
        e.velocityX = Float(g.velocity)
        emit(id, .rotate) { AsukaEvents.rotate(id, e) }
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
        emit(id, .swipe) { AsukaEvents.swipe(id, d) }
    }

    // Animation: between AnimateBegin and AnimateEnd, frames and visual
    // props change in UIView animations.

    func beginAnimation(_ args: UnsafeRawBufferPointer) {
        guard args.count >= 5 else { return }
        var r = AsukaReader(args)
        animation = (Double(r.f32()), Int32(r.u8()))
        repeating = nil
        if args.count >= 10 {
            let n = Int32(bitPattern: r.u32()), auto = r.u8() != 0
            if n != 0 || auto { repeating = (n, auto) }
        }
        animationDelay = 0
        spring = (0.85, 0)
        if args.count >= 22 {
            animationDelay = Double(r.f32())
            spring = (CGFloat(r.f32()), CGFloat(r.f32()))
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
