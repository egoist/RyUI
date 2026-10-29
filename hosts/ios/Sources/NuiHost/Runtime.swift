#if canImport(UIKit)
import UIKit
#if canImport(NuiOps)
import NuiOps
#endif
#if canImport(CNui)
import CNui
#endif

/// A native view the core made, or a list cell the host made.
@MainActor final class NuiNode {
    let id: UInt32
    let kind: NuiKind?
    var view: UIView // replaced only by a TextField made multiline
    var children: [NuiNode] = []
    weak var parent: NuiNode?
    var listens = Set<UInt16>()

    var textStyle: Int32 = 0
    var fontSize: Float = 0
    var fontWeight: Int32 = 0
    var fontFamily = ""
    var color: UInt64 = 0
    var borderColor: UInt64 = 0
    var list: NuiList?
    weak var cell: NuiListCell?

    // Navigation and presentation.
    var vc: UIViewController?
    var dying = false // being popped or dismissed: its views leave with it
    var selected: Int?
    var actions: [NuiAction] = []
    var swipeActions: [NuiAction] = [] // a list cell's
    var shadowColor: UIColor?
    var transition: NuiTransition?
    var opacity: CGFloat = 1 // its own, as Style.Opacity sets it
    var baseTransform: CGAffineTransform = .identity // Style.Transform's
    var gradient: CAGradientLayer?
    var material: UIVisualEffectView?
    var search: NuiSearch? // a screen's search field
    var gradientColors: [UIColor?] = []
    var scrolls: NuiScrollReporter? // a Scroll's or List's, when the core listens for Scrolled
    var menuTrigger: Int32 = 0
    var menuOverlay: UIButton?
    var contextMenu: UIContextMenuInteraction?
    var contextMenuDelegate: NuiContextMenu?
    var alertTitle: String?
    var alertMessage: String?
    var alertStyle: Int32 = 0
    var imageSource: String?
    var imageLoaded = false
    var imagePlaceholder = ""
    var native: NuiNativeView?

    /// Whether an ancestor is being popped or dismissed.
    var underDying: Bool {
        var p = parent
        while let n = p {
            if n.dying { return true }
            p = n.parent
        }
        return false
    }

    init(id: UInt32, kind: NuiKind?, view: UIView) {
        self.id = id
        self.kind = kind
        self.view = view
    }

    /// Where children's views go.
    var container: UIView { cell?.contentView ?? view }

    func insert(_ child: NuiNode, at index: Int) {
        let i = min(index, children.count)
        children.insert(child, at: i)
        child.parent = self
        let c = container
        if i + 1 < children.count, children[i + 1].view.superview === c {
            c.insertSubview(child.view, belowSubview: children[i + 1].view)
        } else {
            c.addSubview(child.view)
        }
        if let o = menuOverlay { c.bringSubviewToFront(o) }
    }

    func remove(_ child: NuiNode) {
        if let i = children.firstIndex(where: { $0 === child }) {
            children.remove(at: i)
        }
        child.parent = nil
        child.view.removeFromSuperview()
    }
}

/// A container: nui positions its subviews itself.
final class NuiContainerView: UIView {
    /// It presses (OnPress): one element for VoiceOver, a button.
    var pressable = false {
        didSet {
            isAccessibilityElement = pressable || super.accessibilityLabel != nil
            if pressable { accessibilityTraits.insert(.button) }
        }
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
        isAccessibilityElement = false
    }
    required init?(coder: NSCoder) { fatalError() }

    /// The app's label, or a pressable view's content, as VoiceOver reads
    /// a row: its texts and labeled images, in order.
    override var accessibilityLabel: String? {
        get {
            if let l = super.accessibilityLabel { return l }
            guard pressable else { return nil }
            var parts: [String] = []
            func walk(_ v: UIView) {
                for s in v.subviews where !s.isHidden && s.alpha > 0 {
                    if s.isAccessibilityElement, let l = s.accessibilityLabel, !l.isEmpty {
                        parts.append(l)
                    } else if let l = s as? UILabel, let t = l.text, !t.isEmpty {
                        parts.append(t)
                    } else {
                        walk(s)
                    }
                }
            }
            walk(self)
            return parts.isEmpty ? nil : parts.joined(separator: ", ")
        }
        set { super.accessibilityLabel = newValue }
    }

    /// VoiceOver's double tap: the press, not a tap at the center (which a
    /// subview could take).
    override func accessibilityActivate() -> Bool {
        guard pressable, tag > 0 else { return false }
        let id = UInt32(tag)
        NuiRuntime.shared.emit(id, .press) { NuiEvents.press(id) }
        return true
    }
}

final class NuiTextField: UITextField {
    var nuiID: UInt32 = 0
}

/// The iOS host: applies the core's ops to UIKit views and reports
/// events back. Everything runs on the main thread.
@MainActor public final class NuiRuntime: NSObject, NuiOpHandler {
    var nativeFactories: [String: (NuiNativeContext) -> NuiNativeView] = [:]
    let gestureDelegate = NuiGestureDelegate()
    /// Between AnimateBegin and AnimateEnd: the duration and curve.
    var animation: (Double, Int32)?
    /// The animation's repeat: times more (-1 forever), and autoreverses.
    var repeating: (Int32, Bool)?
    /// Views with a repeating animation, which a plain change stops.
    var repeatingViews = Set<ObjectIdentifier>()
    /// Native modules' methods, by module and method name (ui.Call).
    var modules: [String: [String: NuiModuleMethod]] = [:]
    var builtinModules = false
    var pendingURLs: [URL] = []
    /// The app's accent color (nui.json's), nil for the system's: the tint color.
    var accent: UIColor?
    /// Views coming in with a transition in the batch being applied.
    var entering: [(NuiNode, (Double, Int32))] = []
    /// Views going out with a transition, still shown.
    var exiting = Set<ObjectIdentifier>()
    public static let shared = NuiRuntime()

    var nodes: [UInt32: NuiNode] = [:]
    weak var controller: NuiViewController?
    var rootID: UInt32 = 0
    var started = false
    var depth = 0
    var pendingLists: [NuiList] = []
    var pendingEvents: [[UInt8]] = []
    var nextCellID: UInt32 = 0
    var scale: CGFloat = UIScreen.main.scale

    // MARK: Calling into Go

    /// Every call into Go goes through here, so that work UIKit might do
    /// re-entrantly (list snapshots binding cells) happens once Go has
    /// returned.
    func enter(_ f: () -> Void) {
        depth += 1
        f()
        depth -= 1
        while depth == 0 && (!pendingLists.isEmpty || !pendingEvents.isEmpty) {
            let lists = pendingLists
            pendingLists = []
            for l in lists { l.applyPending() }
            if !pendingEvents.isEmpty {
                let e = pendingEvents.removeFirst()
                send(e)
            }
        }
    }

    /// Sends an event to the core. Inside a call into Go (UIKit laying
    /// out or calling a delegate while a batch is applied), it waits until
    /// that call returns.
    func send(_ bytes: [UInt8]) {
        if depth > 0 {
            pendingEvents.append(bytes)
            return
        }
        enter {
            bytes.withUnsafeBufferPointer { nui_event($0.baseAddress, $0.count) }
        }
    }

    func emit(_ id: UInt32, _ event: NuiEventKind, _ make: () -> [UInt8]) {
        guard let n = nodes[id], n.listens.contains(event.rawValue) else { return }
        send(make())
    }

    func start(_ controller: NuiViewController) {
        self.controller = controller
        guard !started else { return }
        started = true
        #if DEBUG
        NuiAutomation.start()
        #endif
        registerNatives()
        var cb = nui_host_callbacks(
            apply: { buf, len in
                MainActor.assumeIsolated {
                    NuiRuntime.shared.apply(UnsafeRawBufferPointer(start: buf, count: len))
                }
            },
            measure: { id, w, h, aw, ah, ow, oh in
                MainActor.assumeIsolated {
                    let s = NuiRuntime.shared.measure(id, w, h, aw, ah)
                    ow?.pointee = Float(s.width)
                    oh?.pointee = Float(s.height)
                }
            },
            post: {
                DispatchQueue.main.async {
                    MainActor.assumeIsolated { NuiRuntime.shared.enter { nui_run_posted() } }
                }
            }
        )
        enter { nui_start(&cb) }
        let urls = pendingURLs
        pendingURLs = []
        for u in urls { openURL(u) }
    }

    /// The app was opened with url (its scheme, a universal link): the
    /// core's ui.OnOpenURL handlers get it, once it has started.
    public func openURL(_ url: URL) {
        guard started else { pendingURLs.append(url); return }
        send(NuiEvents.incomingURL(rootID, url.absoluteString))
    }

    public func lifecycle(_ state: NuiLifecycle) {
        guard started else { return }
        send(NuiEvents.lifecycle(rootID, Int64(state.rawValue)))
    }

    // MARK: Applying ops

    func apply(_ buf: UnsafeRawBufferPointer) {
        nuiDecode(buf, self)
        runEnters()
    }

    public func create(_ id: UInt32, kind raw: UInt16) {
        guard let kind = NuiKind(rawValue: raw) else {
            fatalError("nui: the iOS host can't show kind \(raw) yet")
        }
        if let (v, vc) = makeController(id, kind) {
            v.tag = Int(id)
            // The core sizes stacks and tabs; screens are sized by their
            // container controller. Never by autoresizing.
            if kind == .stack || kind == .tabs { v.autoresizingMask = [] }
            let node = NuiNode(id: id, kind: kind, view: v)
            node.vc = vc
            nodes[id] = node
            return
        }
        let view: UIView
        var list: NuiList?
        switch kind {
        case .view:
            view = NuiContainerView()
        case .text:
            let l = UILabel()
            l.numberOfLines = 0
            l.font = .preferredFont(forTextStyle: .body)
            l.adjustsFontForContentSizeCategory = true
            l.textColor = .label
            view = l
        case .button:
            let b = UIButton(configuration: .plain())
            b.addAction(UIAction { [weak self] _ in self?.emit(id, .press) { NuiEvents.press(id) } }, for: .primaryActionTriggered)
            view = b
        case .textField:
            let t = NuiTextField()
            t.nuiID = id
            t.borderStyle = .roundedRect
            t.font = .preferredFont(forTextStyle: .body)
            t.adjustsFontForContentSizeCategory = true
            t.delegate = self
            t.addAction(UIAction { [weak self, weak t] _ in
                guard let t else { return }
                let s = t.text ?? ""
                self?.emit(id, .textChanged) { NuiEvents.textChanged(id, s) }
            }, for: .editingChanged)
            view = t
        case .toggle:
            let s = UISwitch()
            s.addAction(UIAction { [weak self, weak s] _ in
                guard let s else { return }
                let on = s.isOn
                self?.emit(id, .toggled) { NuiEvents.toggled(id, on) }
            }, for: .valueChanged)
            view = s
        case .slider:
            let s = UISlider()
            s.addAction(UIAction { [weak self, weak s] _ in
                guard let s else { return }
                let v = Double(s.value)
                self?.emit(id, .valueChanged) { NuiEvents.valueChanged(id, v) }
            }, for: .valueChanged)
            view = s
        case .scroll:
            let s = UIScrollView()
            s.keyboardDismissMode = .interactive
            s.alwaysBounceVertical = true
            s.contentInsetAdjustmentBehavior = .never
            view = s
        case .webView:
            view = NuiWebView(id, self)
        case .video:
            view = NuiVideoView(id, self)
        case .list:
            let l = NuiList(id: id, runtime: self)
            list = l
            view = l.collection
        case .activity:
            view = UIActivityIndicatorView(style: .medium)
        case .symbol:
            view = NuiSymbolView(frame: .zero)
        case .segmented:
            let s = UISegmentedControl()
            s.addAction(UIAction { [weak self, weak s] _ in
                guard let s else { return }
                let i = Int64(s.selectedSegmentIndex)
                self?.emit(id, .selected) { NuiEvents.selected(id, i) }
            }, for: .valueChanged)
            view = s
        case .progress:
            view = UIProgressView(progressViewStyle: .default)
        case .datePicker:
            let h = NuiDatePickerView()
            let d = h.picker
            d.addAction(UIAction { [weak self, weak d] _ in
                guard let d else { return }
                let ms = (d.date.timeIntervalSince1970 * 1000).rounded()
                self?.emit(id, .valueChanged) { NuiEvents.valueChanged(id, ms) }
            }, for: .valueChanged)
            view = h
        case .image:
            let iv = UIImageView()
            iv.contentMode = .scaleAspectFit
            view = iv
        case .native:
            view = NuiNativeHostView()
        case .selectable:
            view = NuiSelectionArea()
        case .richText:
            let r = NuiRichTextView()
            r.onLink = { [weak self] url in self?.emit(id, .link) { NuiEvents.link(id, url) } }
            view = r
        default:
            fatalError("nui: the iOS host can't show \(kind.name) yet")
        }
        view.tag = Int(id)
        view.translatesAutoresizingMaskIntoConstraints = true
        view.autoresizingMask = []
        let node = NuiNode(id: id, kind: kind, view: view)
        node.list = list
        nodes[id] = node
    }

    public func destroy(_ id: UInt32) {
        guard let n = nodes.removeValue(forKey: id) else { return }
        if destroyController(n) { return }
        for c in n.children { c.parent = nil }
        // Going out with a transition, or inside a view that is: shown
        // until it's gone.
        if leaving(n.view) || exit(n) {
            if let p = n.parent, let i = p.children.firstIndex(where: { $0 === n }) { p.children.remove(at: i) }
            n.parent = nil
            return
        }
        n.parent?.remove(n)
        n.view.removeFromSuperview()
    }

    public func insert(_ parent: UInt32, child: UInt32, index: Int) {
        guard let p = nodes[parent], let c = nodes[child] else { return }
        if insertController(p, c, index) { return }
        p.insert(c, at: index)
        enter(c)
    }

    public func remove(_ parent: UInt32, child: UInt32) {
        guard let p = nodes[parent], let c = nodes[child] else { return }
        p.remove(c)
    }

    public func setFrame(_ id: UInt32, x: Float, y: Float, width: Float, height: Float) {
        guard let n = nodes[id] else { return }
        if let cell = n.cell {
            cell.setRowHeight(CGFloat(height))
            return
        }
        let f = CGRect(x: CGFloat(x), y: CGFloat(y), width: CGFloat(width), height: CGFloat(height))
        let v = n.view
        let scroll = n.parent?.view as? UIScrollView
        if let r = v.superview as? NuiRichTextView {
            // An inline component: its size is the core's, its place the text's.
            v.frame.size = f.size
            r.setNeedsLayout()
            return
        }
        animated(v) {
            // Through bounds and center: a transformed view's frame isn't its
            // layout's (and a scroll view's bounds origin is its offset).
            v.bounds = CGRect(origin: v.bounds.origin, size: f.size)
            v.center = CGPoint(x: f.midX, y: f.midY)
            scroll?.contentSize = f.size
        }
        n.updateLayers()
    }

    /// Makes c the windows' tint color, which UIKit's controls and the
    /// accent color take; rich texts typeset their links again.
    func setAccent(_ c: UIColor?) {
        accent = c
        for case let s as UIWindowScene in UIApplication.shared.connectedScenes {
            for w in s.windows { w.tintColor = c }
        }
        NuiLayoutCache.shared.clear()
        for n in nodes.values { (n.view as? NuiRichTextView)?.invalidate() }
    }

    public func listen(_ id: UInt32, event: UInt16) {
        guard let n = nodes[id] else { return }
        n.listens.insert(event)
        if event == NuiEventKind.scrolled.rawValue, let s = n.view as? UIScrollView, n.scrolls == nil {
            n.scrolls = NuiScrollReporter(id, self)
            s.delegate = n.scrolls
        }
        guard n.kind == .view, let e = NuiEventKind(rawValue: event) else { return }
        switch e {
        case .pan, .pinch, .swipe:
            listenGesture(n, e)
        case .press:
            let tap = UITapGestureRecognizer(target: self, action: #selector(tapped(_:)))
            n.view.addGestureRecognizer(tap)
            if let c = n.view as? NuiContainerView { c.pressable = true } else { n.view.accessibilityTraits.insert(.button) }
        case .longPress:
            n.view.addGestureRecognizer(UILongPressGestureRecognizer(target: self, action: #selector(longPressed(_:))))
        default:
            break
        }
    }

    public func unlisten(_ id: UInt32, event: UInt16) {
        nodes[id]?.listens.remove(event)
    }

    @objc func tapped(_ g: UITapGestureRecognizer) {
        guard let id = nodeID(of: g.view) else { return }
        emit(id, .press) { NuiEvents.press(id) }
    }

    @objc func longPressed(_ g: UILongPressGestureRecognizer) {
        guard g.state == .began, let id = nodeID(of: g.view) else { return }
        emit(id, .longPress) { NuiEvents.longPress(id) }
    }

    func nodeID(of view: UIView?) -> UInt32? {
        guard let view else { return nil }
        return view.tag > 0 ? UInt32(view.tag) : nil
    }

    public func command(_ id: UInt32, command: UInt16, args: UnsafeRawBufferPointer) {
        guard let n = nodes[id], let c = NuiCommand(rawValue: command) else { return }
        switch c {
        case .focus:
            n.view.becomeFirstResponder()
        case .blur:
            n.view.resignFirstResponder()
        case .present:
            present(n)
        case .haptic:
            if args.count > 0 { haptic(Int32(args[0])) }
        case .animateBegin:
            beginAnimation(args)
        case .animateEnd:
            animation = nil
            repeating = nil
        case .call:
            call(id, args)
        case .copy:
            UIPasteboard.general.string = String(decoding: args, as: UTF8.self)
        case .play:
            (n.view as? NuiVideoView)?.play()
        case .pause:
            (n.view as? NuiVideoView)?.player.pause()
        case .seek:
            guard args.count >= 8, let v = n.view as? NuiVideoView else { return }
            var r = NuiReader(args)
            v.seek(r.f64())
        case .eval:
            (n.view as? NuiWebView)?.evaluateJavaScript(String(decoding: args, as: UTF8.self))
        case .announce:
            UIAccessibility.post(notification: .announcement, argument: String(decoding: args, as: UTF8.self))
        case .share:
            let s = String(decoding: args, as: UTF8.self)
            // A URL is shared as one (a link preview, Safari's actions).
            let item: Any = URL(string: s).flatMap { $0.scheme != nil && $0.host != nil ? $0 : nil } ?? s
            let vc = UIActivityViewController(activityItems: [item], applicationActivities: nil)
            guard let top = topController else { return }
            if let pop = vc.popoverPresentationController { // iPad
                pop.sourceView = top.view
                pop.sourceRect = CGRect(x: top.view.bounds.midX, y: top.view.bounds.midY, width: 0, height: 0)
                pop.permittedArrowDirections = []
            }
            top.present(vc, animated: true)
        case .scrollTo:
            guard args.count >= 5 else { return }
            var r = NuiReader(args)
            let i = Int(r.u32())
            n.list?.scroll(to: i, animated: r.u8() != 0)
        case .scrollToOffset:
            guard args.count >= 9, let s = n.view as? UIScrollView else { return }
            var r = NuiReader(args)
            let x = CGFloat(r.f32()), y = CGFloat(r.f32())
            n.list?.applyPending()
            s.layoutIfNeeded()
            // From the content's start, clamped to what it can scroll.
            let i = s.adjustedContentInset
            let maxX = max(-i.left, s.contentSize.width - s.bounds.width + i.right)
            let maxY = max(-i.top, s.contentSize.height - s.bounds.height + i.bottom)
            let to = CGPoint(x: min(max(x - i.left, -i.left), maxX), y: min(max(y - i.top, -i.top), maxY))
            s.setContentOffset(to, animated: r.u8() != 0)
        case .openURL:
            if let url = URL(string: String(decoding: args, as: UTF8.self)) { UIApplication.shared.open(url) }
        case .accent:
            guard args.count >= 8 else { return }
            var r = NuiReader(args)
            setAccent(nuiColor(r.u64()))
        case .listUpdate:
            var r = NuiReader(args)
            let count = Int(r.u32())
            var ids = [Int64]()
            ids.reserveCapacity(count)
            for _ in 0..<count { ids.append(Int64(bitPattern: r.u64())) }
            n.list?.update(ids)
            if let l = n.list, !pendingLists.contains(where: { $0 === l }) { pendingLists.append(l) }
        }
    }

    public func setRoot(_ id: UInt32) {
        guard let n = nodes[id] else { return }
        rootID = id
        controller?.mount(n.view)
    }

    public func setProp(_ id: UInt32, prop: UInt16, value: NuiValue) {
        guard let n = nodes[id] else { return }
        if prop < 64 {
            setCommon(n, prop, value)
        } else {
            setOwn(n, prop, value)
        }
    }

    // MARK: Cells

    func makeCell(_ cell: NuiListCell) -> UInt32 {
        nextCellID += 1
        let id = 0x8000_0000 | nextCellID
        let n = NuiNode(id: id, kind: nil, view: cell)
        n.cell = cell
        nodes[id] = n
        return id
    }
}

extension NuiRuntime: UITextFieldDelegate {
    public func textFieldShouldReturn(_ textField: UITextField) -> Bool {
        guard let t = textField as? NuiTextField else { return true }
        let id = t.nuiID
        emit(id, .submit) { NuiEvents.submit(id) }
        return false
    }

    public func textFieldDidBeginEditing(_ textField: UITextField) {
        guard let t = textField as? NuiTextField else { return }
        let id = t.nuiID
        emit(id, .focus) { NuiEvents.focus(id) }
    }

    public func textFieldDidEndEditing(_ textField: UITextField) {
        guard let t = textField as? NuiTextField else { return }
        let id = t.nuiID
        emit(id, .blur) { NuiEvents.blur(id) }
    }
}
#endif
