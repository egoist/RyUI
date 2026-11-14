#if canImport(UIKit)
import UIKit
#if ASUKA_WEBAUTH
import AuthenticationServices
#endif
#if canImport(AsukaOps)
import AsukaOps
#endif
#if canImport(CAsuka)
@_implementationOnly import CAsuka
#endif

/// A native view the core made, or a list cell the host made.
/// The app's assets/: in the app's bundle, or, for an app embedded in
/// another (asuka build --xcframework), in the framework that holds its code.
let asukaAssets: URL? = {
    for b in [Bundle(for: AsukaRuntime.self), Bundle.main] {
        if let u = b.resourceURL?.appendingPathComponent("assets"), FileManager.default.fileExists(atPath: u.path) { return u }
    }
    return Bundle.main.resourceURL?.appendingPathComponent("assets")
}()

@MainActor final class AsukaNode {
    let id: UInt32
    let kind: AsukaKind?
    var view: UIView // replaced only by a TextField made multiline
    var children: [AsukaNode] = []
    weak var parent: AsukaNode?
    var listens = Set<UInt16>()

    var textStyle: Int32 = 0
    var fontSize: Float = 0
    var fontWeight: Int32 = 0
    var fontFamily = ""
    var color: UInt64 = 0
    var borderColor: UInt64 = 0
    /// Style.CornerRadius as set; the layer's is at most half the view's
    /// shorter side, as CSS and Android clamp it (999: a capsule).
    var cornerRadius: CGFloat = 0
    var list: AsukaList?
    weak var cell: AsukaListCell?

    // Navigation and presentation.
    var vc: UIViewController?
    var dying = false // being popped or dismissed: its views leave with it
    var selected: Int?
    var actions: [AsukaAction] = []
    var swipeActions: [AsukaAction] = [] // a list cell's
    var leadingSwipeActions: [AsukaAction] = [] // a list cell's
    var shadowColor: UIColor?
    var clips = false // Style.Clip
    /// Its children's view, clipped, while it clips and casts a shadow.
    var clipView: UIView?
    var transition: AsukaTransition?
    var match: String? // Style.Match's key
    var zoomFrom: String? // a screen's ZoomFrom
    var opacity: CGFloat = 1 // its own, as Style.Opacity sets it
    var baseTransform: CGAffineTransform = .identity // Style.Transform's
    var gradient: CAGradientLayer?
    var material: UIVisualEffectView?
    var search: AsukaSearch? // a screen's search field
    var gradientColors: [UIColor?] = []
    var scrolls: AsukaScrollReporter? // a Scroll's or List's, when the core listens for Scrolled
    var menuTrigger: Int32 = 0
    var menuOverlay: UIButton?
    var contextMenu: UIContextMenuInteraction?
    var contextMenuDelegate: AsukaContextMenu?
    var alertTitle: String?
    var alertMessage: String?
    var alertStyle: Int32 = 0
    var alertFields: [AsukaAlertField] = [] // an alert's text fields
    var alertFieldDelegates: [AnyObject] = [] // its fields' delegates, while it shows
    var imageSource: String?
    var imageLoaded = false
    var imagePlaceholder = ""
    var imageHash: UIImage? // PlaceholderImage's
    var native: AsukaNativeView?
    /// DropAccept's patterns, and DragItems' items, and their interactions.
    var dropAccept: [String] = []
    var dragItems: [AsukaDragSpec] = []
    var dropInteraction: UIDropInteraction?
    var dragInteraction: UIDragInteraction?
    /// Style.TextAlign as set: what it means depends on the direction.
    var textAlign: Int32 = 0
    /// The LayoutDirection prop: inherit is the app's.
    var direction = AsukaLayoutDirection.inherit
    /// Its Keys prop's keys, as key commands (KeyCommands.swift).
    var keyCommands: [UIKeyCommand] = []

    /// Whether an ancestor is being popped or dismissed.
    var underDying: Bool {
        var p = parent
        while let n = p {
            if n.dying { return true }
            p = n.parent
        }
        return false
    }

    init(id: UInt32, kind: AsukaKind?, view: UIView) {
        self.id = id
        self.kind = kind
        self.view = view
    }

    /// Where children's views go.
    var container: UIView { cell?.contentView ?? clipView ?? view }

    func insert(_ child: AsukaNode, at index: Int) {
        let i = min(index, children.count)
        children.insert(child, at: i)
        child.parent = self
        let c = container
        if i + 1 < children.count, children[i + 1].view.superview === c {
            c.insertSubview(child.view, belowSubview: children[i + 1].view)
        } else {
            c.addSubview(child.view)
        }
        if let o = menuOverlay { o.superview?.bringSubviewToFront(o) }
    }

    func remove(_ child: AsukaNode) {
        if let i = children.firstIndex(where: { $0 === child }) {
            children.remove(at: i)
        }
        child.parent = nil
        child.view.removeFromSuperview()
    }
}

/// A container: Asuka positions its subviews itself.
final class AsukaContainerView: UIView {
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

    /// Leaving the window, or hidden: no longer pressed or hovered.
    override func didMoveToWindow() {
        super.didMoveToWindow()
        if window == nil { AsukaRuntime.shared.viewLeft(self) }
    }

    override var isHidden: Bool {
        didSet { if isHidden && !oldValue { AsukaRuntime.shared.viewLeft(self) } }
    }

    /// The app's label, or a pressable (adjustable, actionable) view's
    /// content, as VoiceOver reads a row: its texts and labeled images, in
    /// order.
    override var accessibilityLabel: String? {
        get {
            if let l = super.accessibilityLabel { return l }
            guard pressable || adjustable || accessibilityCustomActions?.isEmpty == false else { return nil }
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
        AsukaRuntime.shared.emit(id, .press) { AsukaEvents.press(id) }
        return true
    }

    /// OnAdjust: VoiceOver's swipe up and down step it.
    var adjustable = false {
        didSet {
            if adjustable { accessibilityTraits.insert(.adjustable); isAccessibilityElement = true } else { accessibilityTraits.remove(.adjustable) }
        }
    }

    override func accessibilityIncrement() { adjust(1) }
    override func accessibilityDecrement() { adjust(-1) }

    private func adjust(_ by: Int64) {
        guard adjustable, tag > 0 else { return }
        let id = UInt32(tag)
        AsukaRuntime.shared.emit(id, .adjust) { AsukaEvents.adjust(id, by) }
    }
}

/// A Text: a label that draws its text inside its padding (ContentInset).
final class AsukaLabel: UILabel {
    var contentInset: UIEdgeInsets = .zero {
        didSet { if contentInset != oldValue { setNeedsDisplay() } }
    }

    override func drawText(in rect: CGRect) {
        super.drawText(in: rect.inset(by: contentInset))
    }
}

final class AsukaTextField: UITextField {
    var asukaID: UInt32 = 0
    /// ContentInset: its text, placeholder and editing are inside it.
    var contentInset_: UIEdgeInsets = .zero {
        didSet { if contentInset_ != oldValue { setNeedsLayout() } }
    }

    override func textRect(forBounds bounds: CGRect) -> CGRect { super.textRect(forBounds: bounds.inset(by: contentInset_)) }
    override func editingRect(forBounds bounds: CGRect) -> CGRect { super.editingRect(forBounds: bounds.inset(by: contentInset_)) }
    override func placeholderRect(forBounds bounds: CGRect) -> CGRect { super.placeholderRect(forBounds: bounds.inset(by: contentInset_)) }
    /// TextFieldProps.Plain: no border, the text alone.
    var plain = false {
        didSet { borderStyle = plain ? .none : .roundedRect }
    }
    /// TextFieldProps.PasteTypes: content of these types pastes into the
    /// app's OnPaste, not the text.
    var pasteTypes: [String] = []

    override func canPerformAction(_ action: Selector, withSender sender: Any?) -> Bool {
        if action == #selector(paste(_:)), AsukaPaste.available(pasteTypes) { return true }
        return super.canPerformAction(action, withSender: sender)
    }

    override func paste(_ sender: Any?) {
        if AsukaPaste.take(asukaID, pasteTypes) { return }
        super.paste(sender)
    }
}

/// The iOS host: applies the core's ops to UIKit views and reports
/// events back. Everything runs on the main thread.
@MainActor public final class AsukaRuntime: NSObject, AsukaOpHandler {
    var nativeFactories: [String: (AsukaNativeContext) -> AsukaNativeView] = [:]
    let gestureDelegate = AsukaGestureDelegate()
    /// Between AnimateBegin and AnimateEnd: the duration and curve.
    var animation: (Double, Int32)?
    /// The animation's delay, and a spring's damping ratio and initial velocity.
    var animationDelay: Double = 0
    var spring: (damping: CGFloat, velocity: CGFloat) = (0.85, 0)
    /// The animation's repeat: times more (-1 forever), and autoreverses.
    var repeating: (Int32, Bool)?
    /// Views with a repeating animation, which a plain change stops.
    var repeatingViews = Set<ObjectIdentifier>()
    /// Native modules' methods, by module and method name (ui.Call).
    var modules: [String: [String: AsukaModuleMethod]] = [:]
    var builtinModules = false
    var pendingURLs: [URL] = []
    /// The app's accent color (asuka.json's), nil for the system's: the tint color.
    var accent: UIColor?
    /// The app's appearance (the ColorScheme command's): the windows' override.
    var colorScheme = UIUserInterfaceStyle.unspecified
    /// The app's direction (the Locale command's).
    lazy var rtl = systemRTL
    #if ASUKA_WEBAUTH
    /// The webAuth module's sign-ins under way: each session, what presents
    /// it, and its start's reply.
    var authSessions: [UInt32: (ASWebAuthenticationSession, AsukaAuthAnchor, (Result<Data, Error>) -> Void)] = [:]
    #endif
    /// Views coming in with a transition in the batch being applied.
    var entering: [(AsukaNode, (Double, Int32))] = []
    /// Views going out with a transition, still shown.
    var exiting = Set<ObjectIdentifier>()
    /// Style.Match: the views with keys, where they were as an animated
    /// batch began destroying, and those going and coming in it.
    var matchViews: [String: AsukaNode] = [:]
    var matchFrames: [String: CGRect]?
    var matchOut: [String: (view: UIView, frame: CGRect)] = [:]
    var matchIn: [AsukaNode] = []
    var matchAnimation: (Double, Int32)?
    var framesApplied = false
    weak var matchWindow: UIWindow?
    public static let shared = AsukaRuntime()

    var nodes: [UInt32: AsukaNode] = [:]
    /// The nodes with Keys.
    var keyed = Set<UInt32>()
    /// Buttons with a glass variant (roundGlass).
    var glassButtons = Set<ObjectIdentifier>()
    weak var controller: AsukaViewController?
    /// The share sheet this runtime is in (app.ShareSheet, in the share
    /// extension), if it is.
    weak var sheet: AsukaShareSheetController?
    var rootID: UInt32 = 0
    var started = false
    var depth = 0
    var pendingLists: [AsukaList] = []
    var pendingEvents: [[UInt8]] = []
    var pendingDirection: (() -> Void)? // the direction of the views there are, once Go has returned
    var nextCellID: UInt32 = 0
    // Text fields' selections: as last reported (or set), those waiting
    // to be, and whether the core is setting one.
    var lastSelection: [UInt32: (Int, Int)] = [:]
    var selectionQueued = Set<UInt32>()
    var settingSelection = false
    var scale: CGFloat = UIScreen.main.scale

    // MARK: Calling into Go

    /// Every call into Go goes through here, so that work UIKit might do
    /// re-entrantly (list snapshots binding cells) happens once Go has
    /// returned.
    func enter(_ f: () -> Void) {
        depth += 1
        f()
        depth -= 1
        while depth == 0 && (pendingDirection != nil || !pendingLists.isEmpty || !pendingEvents.isEmpty) {
            if let d = pendingDirection {
                pendingDirection = nil
                d()
            }
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
            bytes.withUnsafeBufferPointer { asuka_event($0.baseAddress, $0.count) }
        }
    }

    func emit(_ id: UInt32, _ event: AsukaEventKind, _ make: () -> [UInt8]) {
        guard let n = nodes[id], n.listens.contains(event.rawValue) else { return }
        send(make())
    }

    func start(_ controller: AsukaViewController) {
        self.controller = controller
        guard !started else {
            // Started without a screen (asuka/sdk/periodic's task): the root goes in it now.
            if let n = nodes[rootID], n.view.superview == nil { controller.mount(n.view) }
            return
        }
        boot()
    }

    /// Starts the core without a screen, if it hasn't started: the system
    /// launched the app in the background for a task (asuka/sdk/periodic). The
    /// root view is made, and goes on screen if a scene comes.
    func startHeadless() {
        guard !started else { return }
        boot()
    }

    private func boot() {
        started = true
        AsukaCrash.start() // before the Go code's main: its crashes are kept
        NotificationCenter.default.addObserver(self, selector: #selector(voiceOverChanged), name: UIAccessibility.voiceOverStatusDidChangeNotification, object: nil)
        #if ASUKA_NOTIFY || ASUKA_PUSH || ASUKA_NOTIFICATION_SERVICE
        AsukaNotifyCenter.shared.install() // asuka/sdk/push's too
        #endif
        #if DEBUG
        AsukaAutomation.start()
        #endif
        registerNatives()
        // Typed native modules' way back to Go.
        AsukaNative.done = { call, b in b.withUnsafeBufferPointer { asuka_native_done(call, $0.baseAddress, $0.count) } }
        AsukaNative.invoke = { id, b in
            var out: UnsafeMutablePointer<UInt8>? = nil
            let n = b.withUnsafeBufferPointer { asuka_native_invoke(id, $0.baseAddress, $0.count, &out) }
            guard let out else { return nil }
            defer { free(out) }
            return Array(UnsafeBufferPointer(start: out, count: n))
        }
        if let c = controller { AsukaSplash.shared.show(over: c) }
        var cb = asuka_host_callbacks(
            apply: { buf, len in
                MainActor.assumeIsolated {
                    AsukaRuntime.shared.apply(UnsafeRawBufferPointer(start: buf, count: len))
                }
            },
            measure: { id, w, h, aw, ah, ow, oh in
                MainActor.assumeIsolated {
                    let s = AsukaRuntime.shared.measure(id, w, h, aw, ah)
                    ow?.pointee = Float(s.width)
                    oh?.pointee = Float(s.height)
                }
            },
            post: {
                DispatchQueue.main.async {
                    MainActor.assumeIsolated { AsukaRuntime.shared.enter { asuka_run_posted() } }
                }
            },
            native_new: { cls, len, iface, out, cap, big in
                let name = String(decoding: UnsafeBufferPointer(start: cls, count: len), as: UTF8.self)
                return AsukaNative.write(AsukaNative.new(name, iface), out, cap, big)
            },
            native_call: { h, method, p, n, out, cap, big in
                AsukaNative.write(AsukaNative.call(h, method, Array(UnsafeBufferPointer(start: p, count: n))), out, cap, big)
            },
            native_start: { h, method, p, n, call in
                AsukaNative.start(h, method, Array(UnsafeBufferPointer(start: p, count: n)), call)
            },
            native_cancel: { AsukaNative.cancel($0) },
            native_release: { AsukaNative.release($0) },
            native_free: { free($0) }
        )
        enter { asuka_start(&cb) }
        AsukaSplash.shared.started()
        let urls = pendingURLs
        pendingURLs = []
        openURLs(urls)
    }

    /// The app was opened with urls together (scene(_:openURLContexts:)):
    /// its files go to ui.OnOpenFiles at once, as file:// URLs one per
    /// line (Go's internal/fileurl), the rest as openURL's.
    public func openURLs(_ urls: [URL]) {
        guard started else { pendingURLs.append(contentsOf: urls); return }
        let files = urls.filter { $0.isFileURL }
        for u in files {
            // A document opened in place (another app's, iCloud Drive's):
            // the app may read and write it until it quits.
            _ = u.startAccessingSecurityScopedResource()
        }
        if !files.isEmpty {
            send(AsukaEvents.incomingURL(rootID, files.map { $0.absoluteString }.joined(separator: "\n")))
        }
        for u in urls where !u.isFileURL { openURL(u) }
    }

    /// The app was opened with url (its scheme, a universal link): the
    /// core's ui.OnOpenURL handlers get it, once it has started.
    public func openURL(_ url: URL) {
        guard started else { pendingURLs.append(url); return }
        if url.isFileURL { openURLs([url]); return }
        // From the share extension (asuka.json's shareTarget): what another
        // app shared, for ui.OnShare.
        if url.scheme?.lowercased() == "asukashare." + (Bundle.main.bundleIdentifier ?? "").lowercased() {
            let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedQuery?.removingPercentEncoding ?? ""
            let items = AsukaShare.received(q)
            if !items.isEmpty { send(AsukaEvents.shared(rootID, items)) }
            return
        }
        send(AsukaEvents.incomingURL(rootID, url.absoluteString))
    }

    public func lifecycle(_ state: AsukaLifecycle) {
        guard started else { return }
        send(AsukaEvents.lifecycle(rootID, Int64(state.rawValue)))
    }

    // MARK: Applying ops

    func apply(_ buf: UnsafeRawBufferPointer) {
        asukaDecode(buf, self)
        runEnters()
        runMatches()
    }

    public func create(_ id: UInt32, kind raw: UInt16) {
        guard let kind = AsukaKind(rawValue: raw) else {
            fatalError("asuka: the iOS host can't show kind \(raw) yet")
        }
        if let (v, vc) = makeController(id, kind) {
            v.tag = Int(id)
            // The core sizes stacks and tabs; screens are sized by their
            // container controller. Never by autoresizing.
            if kind == .stack || kind == .tabs || kind == .split { v.autoresizingMask = [] }
            let node = AsukaNode(id: id, kind: kind, view: v)
            vc?.navigationItem.largeTitleDisplayMode = .never // Screen.LargeTitle asks for it
            node.vc = vc
            nodes[id] = node
            adoptDirection(node)
            return
        }
        let view: UIView
        var list: AsukaList?
        switch kind {
        case .view:
            view = AsukaContainerView()
        case .accessory:
            view = AsukaAccessoryView(frame: .zero)
        case .canvas:
            view = AsukaCanvasView(frame: .zero)
        case .shader:
            view = makeShader()
        case .text:
            let l = AsukaLabel()
            l.numberOfLines = 0
            l.font = .preferredFont(forTextStyle: .body)
            l.adjustsFontForContentSizeCategory = true
            l.textColor = .label
            view = l
        case .button:
            let b = UIButton(configuration: .plain())
            b.addAction(UIAction { [weak self] _ in self?.emit(id, .press) { AsukaEvents.press(id) } }, for: .primaryActionTriggered)
            view = b
        case .textField:
            let t = AsukaTextField()
            t.asukaID = id
            t.borderStyle = .roundedRect
            t.font = .preferredFont(forTextStyle: .body)
            t.adjustsFontForContentSizeCategory = true
            t.delegate = self
            t.addAction(UIAction { [weak self, weak t] _ in
                guard let t else { return }
                let s = t.text ?? ""
                self?.emit(id, .textChanged) { AsukaEvents.textChanged(id, s) }
            }, for: .editingChanged)
            view = t
        case .toggle:
            let s = UISwitch()
            s.addAction(UIAction { [weak self, weak s] _ in
                guard let s else { return }
                let on = s.isOn
                self?.emit(id, .toggled) { AsukaEvents.toggled(id, on) }
            }, for: .valueChanged)
            view = s
        case .slider:
            let s = UISlider()
            s.addAction(UIAction { [weak self, weak s] _ in
                guard let s else { return }
                let v = Double(s.value)
                self?.emit(id, .valueChanged) { AsukaEvents.valueChanged(id, v) }
            }, for: .valueChanged)
            view = s
        case .shape:
            view = AsukaShapeView()
        case .scroll:
            let s = AsukaScrollView()
            s.keyboardDismissMode = .interactive
            s.alwaysBounceVertical = true
            s.contentInsetAdjustmentBehavior = .never
            view = s
        #if ASUKA_WEBVIEW
        case .webView:
            view = AsukaWebView(id, self)
        #endif
        #if ASUKA_VIDEO
        case .video:
            view = AsukaVideoView(id, self)
        #endif
        case .list:
            let l = AsukaList(id: id, runtime: self)
            list = l
            view = l.collection
        case .activity:
            view = UIActivityIndicatorView(style: .medium)
        case .symbol:
            view = AsukaSymbolView(frame: .zero)
        case .wheel:
            view = makeWheel(id)
        case .picker:
            view = makePicker(id)
        case .stepper:
            view = makeStepper(id)
        case .colorPicker:
            view = makeColorPicker(id)
        case .segmented:
            let s = UISegmentedControl()
            s.addAction(UIAction { [weak self, weak s] _ in
                guard let s else { return }
                let i = Int64(s.selectedSegmentIndex)
                self?.emit(id, .selected) { AsukaEvents.selected(id, i) }
            }, for: .valueChanged)
            view = s
        case .pageControl:
            // The system's dots: tapping its sides steps, a scrub moves along, VoiceOver adjusts.
            let p = UIPageControl()
            // Its default dots are white, for photos; on the app's own background, the label's.
            p.currentPageIndicatorTintColor = .label
            p.pageIndicatorTintColor = .tertiaryLabel
            p.addAction(UIAction { [weak self, weak p] _ in
                guard let p else { return }
                let i = Int64(p.currentPage)
                self?.emit(id, .selected) { AsukaEvents.selected(id, i) }
            }, for: .valueChanged)
            view = p
        case .progress:
            view = UIProgressView(progressViewStyle: .default)
        case .progressRing:
            view = AsukaProgressRing(frame: .zero)
        case .datePicker:
            let h = AsukaDatePickerView()
            let d = h.picker
            d.addAction(UIAction { [weak self, weak d] _ in
                guard let d else { return }
                let ms = (d.date.timeIntervalSince1970 * 1000).rounded()
                self?.emit(id, .valueChanged) { AsukaEvents.valueChanged(id, ms) }
            }, for: .valueChanged)
            view = h
        case .image:
            view = AsukaImageView(frame: .zero)
        case .native:
            view = AsukaNativeHostView()
        case .selectable:
            view = AsukaSelectionArea()
        case .richText:
            let r = AsukaRichTextView()
            r.onLink = { [weak self] url in self?.emit(id, .link) { AsukaEvents.link(id, url) } }
            view = r
        default:
            fatalError("asuka: the iOS host can't show \(kind.name) yet")
        }
        view.tag = Int(id)
        view.translatesAutoresizingMaskIntoConstraints = true
        view.autoresizingMask = []
        let node = AsukaNode(id: id, kind: kind, view: view)
        node.list = list
        (view as? AsukaScrollView)?.node = node
        (view as? AsukaSelectionArea)?.node = node
        nodes[id] = node
        adoptDirection(node)
    }

    public func destroy(_ id: UInt32) {
        guard let n = nodes.removeValue(forKey: id) else { return }
        keyed.remove(id)
        if let s = n.view as? AsukaSnackbarView {
            s.dismiss()
            return
        }
        if destroyController(n) { return }
        for c in n.children { c.parent = nil }
        if matchOut(n) { return }
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
        matchIn(c)
    }

    public func remove(_ parent: UInt32, child: UInt32) {
        guard let p = nodes[parent], let c = nodes[child] else { return }
        p.remove(c)
    }

    public func setFrame(_ id: UInt32, x: Float, y: Float, width: Float, height: Float) {
        guard let n = nodes[id] else { return }
        if n.parent?.vc is AsukaSplitController { return } // its column's, which UIKit sizes
        framesApplied = true
        if let cell = n.cell {
            cell.setRowHeight(CGFloat(height))
            return
        }
        let f = CGRect(x: CGFloat(x), y: CGFloat(y), width: CGFloat(width), height: CGFloat(height))
        let v = n.view
        let scroll = n.parent?.view as? UIScrollView
        if let r = v.superview as? AsukaRichTextView {
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
            (scroll as? AsukaScrollView)?.keepEnd()
            (v as? AsukaScrollView)?.keepEnd()
        }
        // A row of a Scroll's content moved: what it takes out is redone.
        (n.parent?.parent?.view as? AsukaScrollView)?.setNeedsLayout()
        n.updateLayers()
    }

    /// Makes c the windows' tint color, which UIKit's controls and the
    /// accent color take; rich texts typeset their links again.
    func setAccent(_ c: UIColor?) {
        accent = c
        asukaAccent = c
        for case let s as UIWindowScene in UIApplication.shared.connectedScenes {
            for w in s.windows { w.tintColor = c }
        }
        controller?.view.window?.tintColor = c // a share sheet's
        AsukaLayoutCache.shared.clear()
        for n in nodes.values { (n.view as? AsukaRichTextView)?.invalidate() }
    }

    /// Shows the windows light or dark, or as the system is (.system):
    /// the trait change restyles the views and reports the environment.
    func setColorScheme(_ s: AsukaColorScheme) {
        colorScheme = s == .dark ? .dark : s == .light ? .light : .unspecified
        for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
            for w in scene.windows { w.overrideUserInterfaceStyle = colorScheme }
        }
        controller?.view.window?.overrideUserInterfaceStyle = colorScheme
    }

    public func listen(_ id: UInt32, event: UInt16) {
        guard let n = nodes[id] else { return }
        n.listens.insert(event)
        if event == AsukaEventKind.scrolled.rawValue || event == AsukaEventKind.paged.rawValue, let s = n.view as? UIScrollView, n.scrolls == nil {
            n.scrolls = AsukaScrollReporter(id, self)
            if n.list == nil { s.delegate = n.scrolls } // a list's own delegate forwards it
        }
        guard n.kind == .view, let e = AsukaEventKind(rawValue: event) else { return }
        switch e {
        case .pan, .pinch, .swipe, .rotate:
            listenGesture(n, e)
        case .press:
            installPress(n) // Press and Pressed, one recognizer (Interaction.swift)
            if let c = n.view as? AsukaContainerView { c.pressable = true } else { n.view.accessibilityTraits.insert(.button) }
        case .pressed:
            installPress(n) // no button for VoiceOver: a hold isn't one
        case .hover:
            installHover(n)
        case .longPress:
            installLongPress(n)
        default:
            break
        }
    }

    public func unlisten(_ id: UInt32, event: UInt16) {
        nodes[id]?.listens.remove(event)
    }

    @objc func longPressed(_ g: UILongPressGestureRecognizer) {
        // A draggable view's long press lifts it instead.
        guard g.state == .began, let id = nodeID(of: g.view), nodes[id]?.dragInteraction == nil else { return }
        emit(id, .longPress) { AsukaEvents.longPress(id) }
    }

    func nodeID(of view: UIView?) -> UInt32? {
        guard let view else { return nil }
        return view.tag > 0 ? UInt32(view.tag) : nil
    }

    public func command(_ id: UInt32, command: UInt16, args: UnsafeRawBufferPointer) {
        guard let n = nodes[id], let c = AsukaCommand(rawValue: command) else { return }
        switch c {
        case .insertText:
            insertText(n, String(decoding: args, as: UTF8.self))
        case .format:
            if args.count >= 2 { format(n, args[0], args[1]) }
        case .setLink:
            setLink(n, String(decoding: args, as: UTF8.self))
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
        case .frames:
            setFrames(args.first == 1)
        #if ASUKA_VIDEO
        case .play:
            (n.view as? AsukaVideoView)?.play()
        case .pause:
            (n.view as? AsukaVideoView)?.player.pause()
        case .seek:
            guard args.count >= 8, let v = n.view as? AsukaVideoView else { return }
            var r = AsukaReader(args)
            v.seek(r.f64())
        #else
        case .play, .pause, .seek: break
        #endif
        #if ASUKA_WEBVIEW
        case .eval:
            (n.view as? AsukaWebView)?.evaluateJavaScript(String(decoding: args, as: UTF8.self))
        case .webNavigate:
            if let to = args.first { (n.view as? AsukaWebView)?.navigate(to) }
        #else
        case .eval, .webNavigate: break
        #endif
        case .announce:
            UIAccessibility.post(notification: .announcement, argument: String(decoding: args, as: UTF8.self))
        case .replace:
            (n.vc as? AsukaStackController)?.instant = true
        case .backExit:
            break // iOS has no back to leave the app with
        case .a11yFocus:
            // After this batch's frames, so VoiceOver finds the view where it shows.
            DispatchQueue.main.async { [weak v = n.view] in
                guard let v, v.window != nil else { return }
                UIAccessibility.post(notification: .screenChanged, argument: v)
            }
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
            var r = AsukaReader(args)
            let i = Int(r.u32())
            n.list?.scroll(to: i, animated: r.u8() != 0)
        case .scrollToOffset:
            guard args.count >= 9, let s = n.view as? UIScrollView else { return }
            var r = AsukaReader(args)
            let x = CGFloat(r.f32()), y = CGFloat(r.f32())
            n.list?.applyPending()
            s.layoutIfNeeded()
            // From the content's start, clamped to what it can scroll.
            let i = s.adjustedContentInset
            let maxX = max(-i.left, s.contentSize.width - s.bounds.width + i.right)
            let maxY = max(-i.top, s.contentSize.height - s.bounds.height + i.bottom)
            let to = CGPoint(x: min(max(x - i.left, -i.left), maxX), y: min(max(y - i.top, -i.top), maxY))
            s.setContentOffset(to, animated: r.u8() != 0)
            n.list?.scrolled(toY: to.y)
        case .openURL:
            if let url = URL(string: String(decoding: args, as: UTF8.self)) { UIApplication.shared.open(url) }
        case .accent:
            guard args.count >= 8 else { return }
            var r = AsukaReader(args)
            setAccent(asukaColor(r.u64()))
        case .menuBar, .windowOptions, .windowTitle, .windowAction:
            break // desktop hosts' (a phone has no menu bar or windows)
        case .locale:
            setLocale(args)
        case .saveLocale:
            saveLocale(String(decoding: args, as: UTF8.self))
        case .shareClose:
            sheet?.close(args.first ?? 0)
        case .colorScheme:
            guard let v = args.first else { return }
            setColorScheme(AsukaColorScheme(rawValue: Int32(v)) ?? .system)
        case .listUpdate:
            var r = AsukaReader(args)
            let count = Int(r.u32())
            var ids = [Int64]()
            ids.reserveCapacity(count)
            for _ in 0..<count { ids.append(Int64(bitPattern: r.u64())) }
            var sections: [(start: Int, id: Int64)] = []
            if !r.atEnd {
                let ns = Int(r.u32())
                sections.reserveCapacity(ns)
                for _ in 0..<ns {
                    let start = Int(r.u32())
                    sections.append((start, Int64(bitPattern: r.u64())))
                }
            }
            n.list?.update(ids, sections: sections)
            if let l = n.list, !pendingLists.contains(where: { $0 === l }) { pendingLists.append(l) }
        }
    }

    public func setRoot(_ id: UInt32) {
        guard let n = nodes[id] else { return }
        rootID = id
        controller?.mount(n.view)
        sheet?.deliver()
    }

    public func setProp(_ id: UInt32, prop: UInt16, value: AsukaValue) {
        guard let n = nodes[id] else { return }
        if prop < 64 {
            setCommon(n, prop, value)
        } else {
            setOwn(n, prop, value)
        }
    }

    // MARK: Cells

    func makeCell(_ cell: AsukaListCell) -> UInt32 {
        nextCellID += 1
        let id = 0x8000_0000 | nextCellID
        let n = AsukaNode(id: id, kind: nil, view: cell)
        n.cell = cell
        nodes[id] = n
        adoptDirection(n)
        return id
    }
}

extension AsukaRuntime: UITextFieldDelegate {
    public func textFieldShouldReturn(_ textField: UITextField) -> Bool {
        guard let t = textField as? AsukaTextField else { return true }
        let id = t.asukaID
        emit(id, .submit) { AsukaEvents.submit(id) }
        return false
    }

    public func textFieldDidBeginEditing(_ textField: UITextField) {
        guard let t = textField as? AsukaTextField else { return }
        let id = t.asukaID
        emit(id, .focus) { AsukaEvents.focus(id) }
    }

    public func textFieldDidChangeSelection(_ textField: UITextField) {
        guard let t = textField as? AsukaTextField else { return }
        reportSelection(t.asukaID)
    }

    public func textFieldDidEndEditing(_ textField: UITextField) {
        guard let t = textField as? AsukaTextField else { return }
        let id = t.asukaID
        emit(id, .blur) { AsukaEvents.blur(id) }
    }
}
#endif
