#if canImport(UIKit)
import UIKit
import UIKit.UIGestureRecognizerSubclass
import UniformTypeIdentifiers
#if canImport(RyUIOps)
import RyUIOps
#endif

// Interaction states and drag and drop (DESIGN.md, "Interaction states,
// drag and drop"): a View's press (Press and Pressed, one recognizer),
// its hover, the drags it starts (DragItems) and the drops it takes
// (DropAccept).

/// A View's press, for Press and Pressed both, so the two agree: Pressed
/// true as the first touch goes down on the view (150 ms later inside a
/// scroll view, as UIScrollView delays its content's touches), false as
/// it lifts or moves more than 20 points off the view (true again back
/// on, while down); Press for a release while pressed. A touch on a
/// descendant that presses itself (a pressable or long-pressable view, a
/// control) presses only it. A scroll, another gesture, a long press or
/// a drag taking the touch, the app going to the background or the view
/// leaving the window cancel it: Pressed false, no Press.
///
/// Discrete, as UITapGestureRecognizer is: it stays possible while the
/// finger is down (so a scroll's pan or a long press can still take the
/// touch, failing it) and recognizes as it lifts, which is the Press.
final class RyUIPressRecognizer: UIGestureRecognizer {
    /// Reports Pressed's changes.
    var onPressed: ((Bool) -> Void)?
    /// The pressed state as reported.
    private(set) var pressed = false
    private weak var touch: UITouch?
    private var inside = false
    /// Whether Pressed may show: at once, or after a scroll view's delay.
    private var shown = false
    private var delay: DispatchWorkItem?
    /// A quick tap's false, a moment after its true.
    private var unpress: DispatchWorkItem?
    private var quick = false

    /// The recognizers tracking a touch, which the app going to the
    /// background or a drag lifting cancels.
    private static let tracking = NSHashTable<RyUIPressRecognizer>.weakObjects()

    static let margin: CGFloat = 20
    static let scrollDelay = 0.15
    static let quickDuration = 0.1

    override init(target: Any?, action: Selector?) {
        super.init(target: target, action: action)
        RyUIInteraction.observeApp()
    }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        if touch != nil {
            for t in touches { ignore(t, for: event) } // only the first touch counts
            return
        }
        guard let v = view, let t = touches.first else { state = .failed; return }
        for x in touches where x !== t { ignore(x, for: event) }
        if pressesDescendant(t, v) { state = .failed; return }
        endUnpress()
        touch = t
        inside = true
        Self.tracking.add(self)
        if inScroll(v) {
            let w = DispatchWorkItem { [weak self] in
                guard let self, self.touch != nil else { return }
                self.delay = nil
                self.shown = true
                self.set(self.inside)
            }
            delay = w
            DispatchQueue.main.asyncAfter(deadline: .now() + Self.scrollDelay, execute: w)
        } else {
            shown = true
            set(true)
        }
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent) {
        guard let t = touch, touches.contains(t), let v = view else { return }
        if v.window == nil { state = .failed; return }
        inside = v.bounds.insetBy(dx: -Self.margin, dy: -Self.margin).contains(t.location(in: v))
        if shown { set(inside) }
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent) {
        guard let t = touch, touches.contains(t), let v = view else { return }
        inside = v.bounds.insetBy(dx: -Self.margin, dy: -Self.margin).contains(t.location(in: v))
        guard inside, v.window != nil else { state = .failed; return }
        if !shown {
            // Released before the scroll view's delay: pressed now, and
            // not a moment later, so the look shows.
            delay?.cancel()
            delay = nil
            set(true)
            quick = true
        }
        state = .ended // recognized: the Press, then reset
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent) {
        guard let t = touch, touches.contains(t) else { return }
        state = .failed
    }

    override func reset() {
        super.reset()
        delay?.cancel()
        delay = nil
        touch = nil
        shown = false
        inside = false
        Self.tracking.remove(self)
        if quick {
            quick = false
            let w = DispatchWorkItem { [weak self] in
                self?.unpress = nil
                self?.set(false)
            }
            unpress = w
            DispatchQueue.main.asyncAfter(deadline: .now() + Self.quickDuration, execute: w)
        } else {
            set(false)
        }
    }

    /// Cancels the press under way, if there is one: Pressed false, no
    /// Press.
    func cancel() {
        endUnpress()
        guard touch != nil else { return }
        isEnabled = false // the documented way to cancel a recognizer
        isEnabled = true
        set(false)
    }

    /// Cancels every press under way.
    static func cancelAll() {
        for r in tracking.allObjects { r.cancel() }
    }

    /// A quick tap's false, now, as another press begins or it's cancelled.
    private func endUnpress() {
        guard let w = unpress else { return }
        w.cancel()
        unpress = nil
        set(false)
    }

    private func set(_ on: Bool) {
        guard pressed != on else { return }
        pressed = on
        onPressed?(on)
    }

    /// Whether the touch landed on a descendant of v that presses itself:
    /// a control, a text view, a view with its own press, long press or
    /// tap menu (whose button is a control). v's own menu button doesn't
    /// count.
    private func pressesDescendant(_ t: UITouch, _ v: UIView) -> Bool {
        let own = RyUIRuntime.shared.nodes[UInt32(max(v.tag, 0))]?.menuOverlay
        var x = t.view
        while let cur = x, cur !== v {
            if let c = cur as? UIControl, c.isEnabled, c !== own { return true }
            if let tv = cur as? UITextView, tv.isEditable { return true }
            if cur.gestureRecognizers?.contains(where: { ($0 is RyUIPressRecognizer || $0 is RyUILongPressRecognizer) && $0.isEnabled }) == true {
                return true
            }
            x = cur.superview
        }
        return false
    }

    /// Whether v is in a scroll view that delays its content's touches.
    private func inScroll(_ v: UIView) -> Bool {
        var s = v.superview
        while let x = s {
            if let sv = x as? UIScrollView, sv.isScrollEnabled, sv.delaysContentTouches { return true }
            s = x.superview
        }
        return false
    }
}

/// A View's long press (LongPress), told apart from UIKit's own long
/// presses: a draggable view's lifts it instead.
final class RyUILongPressRecognizer: UILongPressGestureRecognizer {}

/// Lets hover recognize with every other recognizer, so nested views
/// that listen for Hover stay hovered together.
final class RyUIHoverDelegate: NSObject, UIGestureRecognizerDelegate {
    static let shared = RyUIHoverDelegate()
    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
}

/// An item of a view's DragItems.
struct RyUIDragSpec {
    var kind: Int32
    var text = "", path = "", name = "", mime = ""
    var value = false

    /// Whether it's only an app's value: nothing for other apps.
    var valueOnly: Bool { text.isEmpty && path.isEmpty }
}

/// A drag from the app, as its session's local context: the view it's
/// dragged from and its items' MIME types, as they were when it began.
final class RyUIDragContext: NSObject {
    let source: UInt32
    let mimes: [String]
    init(source: UInt32, mimes: [String]) {
        self.source = source
        self.mimes = mimes
    }
}

enum RyUIInteraction {
    /// Mirrors ops.MatchMIME: an exact type, a family (image/*) or
    /// anything (*/*, *), without regard to case or parameters.
    static func matchMIME(_ patterns: [String], _ mime: String) -> Bool {
        let m = baseMIME(mime).lowercased()
        for pat in patterns {
            let p = baseMIME(pat).lowercased()
            if p == "*" || p == "*/*" { return true }
            if p.hasSuffix("/*") {
                let prefix = String(p.dropLast())
                if m.utf8.count > prefix.utf8.count && m.hasPrefix(prefix) { return true }
            } else if p == m {
                return true
            }
        }
        return false
    }

    private static func baseMIME(_ m: String) -> String {
        var s = Substring(m)
        if let i = s.firstIndex(of: ";") { s = s[..<i] }
        return s.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Decodes a DragItems prop: u32 count, per item u8 kind, strings
    /// text, path, name, MIME, u8 flags; a short list ends where it's cut.
    static func decodeDragItems(_ b: UnsafeRawBufferPointer) -> [RyUIDragSpec] {
        var r = RyUIReader(b)
        func left() -> Int { r.end - r.p }
        func string() -> String? {
            guard left() >= 4 else { return nil }
            var peek = r
            let n = Int(peek.u32())
            guard left() - 4 >= n else { return nil }
            return r.string()
        }
        guard left() >= 4 else { return [] }
        let n = r.u32()
        var out: [RyUIDragSpec] = []
        for _ in 0..<n {
            guard left() >= 1 else { break }
            var it = RyUIDragSpec(kind: Int32(r.u8()))
            guard let t = string(), let p = string(), let nm = string(), let m = string(), left() >= 1 else { break }
            it.text = t; it.path = p; it.name = nm; it.mime = m
            it.value = r.u8() & 1 != 0
            out.append(it)
        }
        return out
    }

    /// Decodes a DropAccept prop: u32 count, then the strings.
    static func decodeStrings(_ b: UnsafeRawBufferPointer) -> [String] {
        var r = RyUIReader(b)
        guard r.end - r.p >= 4 else { return [] }
        let n = r.u32()
        var out: [String] = []
        for _ in 0..<n {
            guard r.end - r.p >= 4 else { break }
            var peek = r
            let len = Int(peek.u32())
            guard r.end - r.p - 4 >= len else { break }
            out.append(r.string())
        }
        return out
    }

    // MARK: The app

    @MainActor private static var observing = false

    /// Cancels presses and hovers as the app goes to the background.
    @MainActor static func observeApp() {
        guard !observing else { return }
        observing = true
        NotificationCenter.default.addObserver(forName: UIApplication.willResignActiveNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated {
                RyUIPressRecognizer.cancelAll()
                for n in RyUIRuntime.shared.nodes.values {
                    for case let g as UIHoverGestureRecognizer in n.view.gestureRecognizers ?? [] where g.state == .began || g.state == .changed {
                        g.isEnabled = false
                        g.isEnabled = true
                    }
                }
            }
        }
    }

    // MARK: Another app's items

    /// What an item from another app is: its kind, MIME type and the type
    /// to load it as (nil: a text or a link). A file's (data that isn't a
    /// URL or a plain text, unless it's a styled text with a plain one:
    /// Notes' RTF) first, then a file URL, a link, a text; unknown: a file
    /// of application/octet-stream.
    static func describe(_ p: NSItemProvider) -> (kind: RyUIShareKind, mime: String, type: UTType?) {
        let types = p.registeredTypeIdentifiers.compactMap { UTType($0) }
        let hasText = types.contains { $0.conforms(to: .plainText) }
        if let f = types.first(where: { $0.conforms(to: .data) && !$0.conforms(to: .url) && !$0.conforms(to: .plainText) }),
           !(hasText && f.conforms(to: .text)) {
            return (fileKind(f), f.preferredMIMEType ?? "application/octet-stream", f)
        }
        if types.contains(where: { $0.conforms(to: .fileURL) }) {
            let t = p.suggestedName.flatMap { UTType(filenameExtension: ($0 as NSString).pathExtension) } ?? .data
            return (fileKind(t), t.preferredMIMEType ?? "application/octet-stream", .fileURL)
        }
        if types.contains(where: { $0.conforms(to: .url) }) { return (.link, "text/uri-list", nil) }
        if hasText { return (.text, "text/plain", nil) }
        return (.file, "application/octet-stream", types.first ?? .data)
    }

    static func fileKind(_ t: UTType) -> RyUIShareKind {
        if t.conforms(to: .image) { return .image }
        if t.conforms(to: .movie) || t.conforms(to: .video) { return .video }
        if t.conforms(to: .audio) { return .audio }
        return .file
    }

    /// An item loaded from another app's drop, as the Drop event carries it.
    struct Loaded {
        var kind: RyUIShareKind
        var text = "", path = "", name = "", mime = ""
        var size: Int64 = -1
    }

    /// Loads another app's item: a text, a link, or a file (and image
    /// data) copied into a new folder in the app's caches, under
    /// ryui-dropped/. done runs on any queue, nil for what can't be had.
    static func load(_ p: NSItemProvider, _ done: @escaping (Loaded?) -> Void) {
        let d = describe(p)
        switch d.kind {
        case .text where d.type == nil:
            _ = p.loadObject(ofClass: NSString.self) { s, _ in
                if let s = s as? String { return done(Loaded(kind: .text, text: s, mime: "text/plain")) }
                p.loadItem(forTypeIdentifier: UTType.plainText.identifier, options: nil) { v, _ in
                    switch v {
                    case let s as String: done(Loaded(kind: .text, text: s, mime: "text/plain"))
                    case let s as NSAttributedString: done(Loaded(kind: .text, text: s.string, mime: "text/plain"))
                    case let b as Data: done(String(data: b, encoding: .utf8).map { Loaded(kind: .text, text: $0, mime: "text/plain") })
                    default: done(nil)
                    }
                }
            }
        case .link where d.type == nil:
            _ = p.loadObject(ofClass: NSURL.self) { u, _ in
                guard let u = u as? URL else { return done(nil) }
                if u.isFileURL {
                    return copy(u, type: UTType(filenameExtension: u.pathExtension) ?? .data, name: nil, done)
                }
                done(Loaded(kind: .link, text: u.absoluteString, mime: "text/uri-list"))
            }
        default:
            guard let type = d.type else { return done(nil) }
            if type == .fileURL {
                _ = p.loadObject(ofClass: NSURL.self) { u, _ in
                    guard let u = u as? URL, u.isFileURL else { return done(nil) }
                    copy(u, type: UTType(filenameExtension: u.pathExtension) ?? .data, name: p.suggestedName, done)
                }
                return
            }
            p.loadFileRepresentation(forTypeIdentifier: type.identifier) { url, _ in
                if let url { return copy(url, type: type, name: p.suggestedName, done) }
                // No file to be had (an image made in memory): its data.
                p.loadDataRepresentation(forTypeIdentifier: type.identifier) { data, _ in
                    guard let data else { return done(nil) }
                    write(data, type: type, name: p.suggestedName, done)
                }
            }
        }
    }

    /// A new folder in the caches for one dropped file.
    private static func slot() -> URL? {
        let fm = FileManager.default
        let d = fm.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("ryui-dropped", isDirectory: true)
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
        return (try? fm.createDirectory(at: d, withIntermediateDirectories: true)) != nil ? d : nil
    }

    /// The file's name: its own, or the provider's with the type's
    /// extension.
    private static func fileName(_ url: URL?, _ type: UTType, _ suggested: String?) -> String {
        if let n = url?.lastPathComponent, !n.isEmpty, !n.hasPrefix(".") { return n }
        var n = suggested.flatMap { $0.isEmpty ? nil : $0 } ?? "file"
        if let ext = type.preferredFilenameExtension, (n as NSString).pathExtension.isEmpty { n += "." + ext }
        return n
    }

    private static func item(_ dst: URL, _ type: UTType) -> Loaded {
        let t = UTType(filenameExtension: dst.pathExtension).flatMap { $0.conforms(to: type) ? $0 : nil } ?? type
        let size = (try? FileManager.default.attributesOfItem(atPath: dst.path)[.size] as? NSNumber)?.int64Value ?? -1
        return Loaded(kind: fileKind(t), path: dst.path, name: dst.lastPathComponent, mime: t.preferredMIMEType ?? "application/octet-stream", size: size)
    }

    /// Copies a file into the caches. Within the provider's completion:
    /// its file is gone after.
    private static func copy(_ src: URL, type: UTType, name: String?, _ done: (Loaded?) -> Void) {
        guard let d = slot() else { return done(nil) }
        let dst = d.appendingPathComponent(fileName(src, type, name))
        let scoped = src.startAccessingSecurityScopedResource()
        defer { if scoped { src.stopAccessingSecurityScopedResource() } }
        do {
            try FileManager.default.copyItem(at: src, to: dst)
            done(item(dst, type))
        } catch {
            try? FileManager.default.removeItem(at: d)
            done(nil)
        }
    }

    private static func write(_ data: Data, type: UTType, name: String?, _ done: (Loaded?) -> Void) {
        guard let d = slot() else { return done(nil) }
        let dst = d.appendingPathComponent(fileName(nil, type, name))
        do {
            try data.write(to: dst)
            done(item(dst, type))
        } catch {
            try? FileManager.default.removeItem(at: d)
            done(nil)
        }
    }

    /// A Drop event's payload: f32 x, y, u32 source, u8 flags (1: copied),
    /// then the items as a Shared event's.
    static func dropPayload(x: CGFloat, y: CGFloat, source: UInt32, copied: Bool, items: [Loaded]) -> [UInt8] {
        var w = RyUIWriter()
        w.f32(Float(x))
        w.f32(Float(y))
        w.u32(source)
        w.u8(copied ? 1 : 0)
        w.u32(UInt32(items.count))
        for it in items {
            w.u8(UInt8(clamping: it.kind.rawValue))
            w.string(it.text)
            w.string(it.path)
            w.string(it.name)
            w.string(it.mime)
            w.u64(UInt64(bitPattern: it.size))
        }
        return w.bytes
    }
}

// MARK: The runtime's side

@MainActor extension RyUIRuntime {
    /// A View's press recognizer, for Press and Pressed: one per view.
    func installPress(_ n: RyUINode) {
        if n.view.gestureRecognizers?.contains(where: { $0 is RyUIPressRecognizer }) == true { return }
        let id = n.id
        let g = RyUIPressRecognizer(target: self, action: #selector(pressRecognized(_:)))
        g.onPressed = { [weak self] on in
            self?.emit(id, .pressed) { RyUIEvents.pressed(id, on) }
        }
        n.view.addGestureRecognizer(g)
    }

    @objc func pressRecognized(_ g: RyUIPressRecognizer) {
        guard let id = nodeID(of: g.view) else { return }
        emit(id, .press) { RyUIEvents.press(id) }
    }

    func installLongPress(_ n: RyUINode) {
        let g = RyUILongPressRecognizer(target: self, action: #selector(longPressed(_:)))
        g.isEnabled = n.dragInteraction == nil // a draggable view's long press lifts it
        n.view.addGestureRecognizer(g)
    }

    func installHover(_ n: RyUINode) {
        if n.view.gestureRecognizers?.contains(where: { $0 is UIHoverGestureRecognizer }) == true { return }
        let g = UIHoverGestureRecognizer(target: self, action: #selector(hovered(_:)))
        g.delegate = RyUIHoverDelegate.shared
        n.view.addGestureRecognizer(g)
        RyUIInteraction.observeApp()
    }

    @objc func hovered(_ g: UIHoverGestureRecognizer) {
        guard let id = nodeID(of: g.view) else { return }
        switch g.state {
        case .began: emit(id, .hover) { RyUIEvents.hover(id, true) }
        case .ended, .cancelled, .failed: emit(id, .hover) { RyUIEvents.hover(id, false) }
        default: break
        }
    }

    /// A View left the window or was hidden: its press is cancelled and
    /// it isn't hovered.
    func viewLeft(_ v: UIView) {
        for case let g as RyUIPressRecognizer in v.gestureRecognizers ?? [] { g.cancel() }
        guard let id = nodeID(of: v), let n = nodes[id], n.view === v, n.listens.contains(RyUIEventKind.hover.rawValue) else { return }
        emit(id, .hover) { RyUIEvents.hover(id, false) }
    }

    func setDropAccept(_ n: RyUINode, _ v: RyUIValue) {
        guard case .bytes(let b) = v else { return }
        n.dropAccept = RyUIInteraction.decodeStrings(b)
        if n.dropAccept.isEmpty {
            if let i = n.dropInteraction { n.view.removeInteraction(i) }
            n.dropInteraction = nil
        } else if n.dropInteraction == nil {
            let i = UIDropInteraction(delegate: RyUIDropDelegate.shared)
            n.view.addInteraction(i)
            n.dropInteraction = i
        }
    }

    func setDragItems(_ n: RyUINode, _ v: RyUIValue) {
        guard case .bytes(let b) = v else { return }
        n.dragItems = RyUIInteraction.decodeDragItems(b)
        if n.dragItems.isEmpty {
            if let i = n.dragInteraction { n.view.removeInteraction(i) }
            n.dragInteraction = nil
        } else if n.dragInteraction == nil {
            let i = UIDragInteraction(delegate: RyUIDragDelegate.shared)
            i.isEnabled = true // on iPhone too
            n.view.addInteraction(i)
            n.dragInteraction = i
        }
        for case let g as RyUILongPressRecognizer in n.view.gestureRecognizers ?? [] {
            g.isEnabled = n.dragInteraction == nil
        }
    }
}

/// Every View's drags (DragItems): the items for other apps and, in the
/// session's local context, its view's ID for the app's own drops.
@MainActor final class RyUIDragDelegate: NSObject, UIDragInteractionDelegate {
    static let shared = RyUIDragDelegate()
    var rt: RyUIRuntime { RyUIRuntime.shared }

    private func node(_ i: UIDragInteraction) -> RyUINode? {
        guard let id = rt.nodeID(of: i.view), let n = rt.nodes[id], n.view === i.view else { return nil }
        return n
    }

    func dragInteraction(_ i: UIDragInteraction, itemsForBeginning session: UIDragSession) -> [UIDragItem] {
        guard let n = node(i), !n.dragItems.isEmpty else { return [] }
        let id = n.id
        session.localContext = RyUIDragContext(source: id, mimes: n.dragItems.map(\.mime))
        return n.dragItems.map { spec in
            let item = UIDragItem(itemProvider: Self.provider(spec))
            item.localObject = NSNumber(value: id)
            return item
        }
    }

    /// What other apps get of an item: its file, its link or its text;
    /// nothing for a value alone.
    static func provider(_ s: RyUIDragSpec) -> NSItemProvider {
        if !s.path.isEmpty {
            let url = URL(fileURLWithPath: s.path)
            if let p = NSItemProvider(contentsOf: url) {
                p.suggestedName = s.name.isEmpty ? url.lastPathComponent : s.name
                return p
            }
        }
        if s.kind == RyUIShareKind.link.rawValue, let u = URL(string: s.text), u.scheme != nil {
            return NSItemProvider(object: u as NSURL)
        }
        if !s.text.isEmpty { return NSItemProvider(object: s.text as NSString) }
        return NSItemProvider()
    }

    func dragInteraction(_ i: UIDragInteraction, sessionIsRestrictedToDraggingApplication session: UIDragSession) -> Bool {
        guard let n = node(i) else { return true }
        return n.dragItems.allSatisfy(\.valueOnly)
    }

    func dragInteraction(_ i: UIDragInteraction, previewForLifting item: UIDragItem, session: UIDragSession) -> UITargetedDragPreview? {
        guard let v = i.view, v.window != nil, v.layer.cornerRadius > 0 else { return nil }
        let p = UIDragPreviewParameters()
        p.visiblePath = UIBezierPath(roundedRect: v.bounds, cornerRadius: v.layer.cornerRadius)
        return UITargetedDragPreview(view: v, parameters: p)
    }

    func dragInteraction(_ i: UIDragInteraction, willAnimateLiftWith animator: UIDragAnimating, session: UIDragSession) {
        RyUIPressRecognizer.cancelAll() // the lift takes the touch
    }

    func dragInteraction(_ i: UIDragInteraction, sessionWillBegin session: UIDragSession) {
        RyUIPressRecognizer.cancelAll()
        guard let id = (session.localContext as? RyUIDragContext)?.source else { return }
        rt.emit(id, .dragging) { RyUIEvents.dragging(id, Int64(RyUIDragPhase.began.rawValue)) }
    }

    func dragInteraction(_ i: UIDragInteraction, session: UIDragSession, didEndWith operation: UIDropOperation) {
        guard let id = (session.localContext as? RyUIDragContext)?.source else { return }
        let phase: RyUIDragPhase = operation == .cancel || operation == .forbidden ? .cancelled : .dropped
        rt.emit(id, .dragging) { RyUIEvents.dragging(id, Int64(phase.rawValue)) }
    }
}

/// The items of a drop being loaded, from any queue, in order.
private final class Loads: @unchecked Sendable {
    private let lock = NSLock()
    private var all: [RyUIInteraction.Loaded?]
    init(_ n: Int) { all = Array(repeating: nil, count: n) }
    func set(_ i: Int, _ it: RyUIInteraction.Loaded?) {
        lock.lock()
        all[i] = it
        lock.unlock()
    }
    var items: [RyUIInteraction.Loaded] {
        lock.lock()
        defer { lock.unlock() }
        return all.compactMap { $0 }
    }
}

/// Every View's drops (DropAccept): a drag is taken if one of its items'
/// MIME types matches; UIKit gives it to the innermost view that takes it.
@MainActor final class RyUIDropDelegate: NSObject, UIDropInteractionDelegate {
    static let shared = RyUIDropDelegate()
    var rt: RyUIRuntime { RyUIRuntime.shared }
    /// The interactions a drag is over.
    private var over = Set<ObjectIdentifier>()

    private func node(_ i: UIDropInteraction) -> RyUINode? {
        guard let id = rt.nodeID(of: i.view), let n = rt.nodes[id], n.view === i.view else { return nil }
        return n
    }

    /// A drag from the app: its context.
    private func local(_ s: UIDropSession) -> RyUIDragContext? {
        s.localDragSession?.localContext as? RyUIDragContext
    }

    /// The MIME types of a drag's items.
    private func mimes(_ s: UIDropSession) -> [String] {
        if let c = local(s) { return c.mimes }
        return s.items.map { RyUIInteraction.describe($0.itemProvider).mime }
    }

    func dropInteraction(_ i: UIDropInteraction, canHandle session: UIDropSession) -> Bool {
        guard let n = node(i), !n.dropAccept.isEmpty else { return false }
        return mimes(session).contains { RyUIInteraction.matchMIME(n.dropAccept, $0) }
    }

    private func report(_ i: UIDropInteraction, _ s: UIDropSession, _ state: RyUIGestureState) {
        guard let v = i.view, let id = rt.nodeID(of: v) else { return }
        var g = RyUIGesture()
        g.state = state.rawValue
        let p = s.location(in: v)
        g.x = Float(p.x); g.y = Float(p.y)
        rt.emit(id, .dragOver) { RyUIEvents.dragOver(id, g) }
    }

    func dropInteraction(_ i: UIDropInteraction, sessionDidEnter session: UIDropSession) {
        over.insert(ObjectIdentifier(i))
        report(i, session, .began)
    }

    func dropInteraction(_ i: UIDropInteraction, sessionDidUpdate session: UIDropSession) -> UIDropProposal {
        if over.insert(ObjectIdentifier(i)).inserted {
            report(i, session, .began)
        } else {
            report(i, session, .changed)
        }
        return UIDropProposal(operation: local(session) != nil ? .move : .copy)
    }

    private func leave(_ i: UIDropInteraction, _ s: UIDropSession) {
        guard over.remove(ObjectIdentifier(i)) != nil else { return }
        report(i, s, .cancelled)
    }

    func dropInteraction(_ i: UIDropInteraction, sessionDidExit session: UIDropSession) { leave(i, session) }
    func dropInteraction(_ i: UIDropInteraction, sessionDidEnd session: UIDropSession) { leave(i, session) }

    func dropInteraction(_ i: UIDropInteraction, performDrop session: UIDropSession) {
        guard let n = node(i) else { return }
        let id = n.id, v = n.view
        let p = session.location(in: v)
        if let c = local(session) {
            // The core has the items, as they were when the drag began.
            let b = RyUIInteraction.dropPayload(x: p.x, y: p.y, source: c.source, copied: false, items: [])
            rt.emit(id, .drop) { RyUIEvents.drop(id, b) }
            leave(i, session)
            return
        }
        leave(i, session)
        // Another app's: only the items the view takes, loaded (files
        // copied into the caches), in order.
        let accept = n.dropAccept
        let providers = session.items.map(\.itemProvider).filter {
            RyUIInteraction.matchMIME(accept, RyUIInteraction.describe($0).mime)
        }
        guard !providers.isEmpty else { return }
        let group = DispatchGroup()
        let loaded = Loads(providers.count)
        for (k, pr) in providers.enumerated() {
            group.enter()
            RyUIInteraction.load(pr) { it in
                loaded.set(k, it)
                group.leave()
            }
        }
        group.notify(queue: .main) {
            MainActor.assumeIsolated {
                let items = loaded.items
                guard !items.isEmpty else { return }
                let b = RyUIInteraction.dropPayload(x: p.x, y: p.y, source: 0, copied: true, items: items)
                RyUIRuntime.shared.emit(id, .drop) { RyUIEvents.drop(id, b) }
            }
        }
    }
}
#endif
