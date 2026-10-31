#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// Hosts the core's root view and reports its environment: size, safe
/// area, keyboard, scale, appearance and text size.
public final class RyUIViewController: UIViewController {
    var keyboard: CGFloat = 0
    var keyboardEnd: CGRect?
    var lastEnv: RyUIEnvironment?
    weak var root: UIView?

    public override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        let nc = NotificationCenter.default
        nc.addObserver(self, selector: #selector(keyboardWillChange(_:)), name: UIResponder.keyboardWillChangeFrameNotification, object: nil)
        nc.addObserver(self, selector: #selector(keyboardWillChange(_:)), name: UIResponder.keyboardWillHideNotification, object: nil)
        nc.addObserver(self, selector: #selector(contentSizeChanged), name: UIContentSizeCategory.didChangeNotification, object: nil)
        nc.addObserver(self, selector: #selector(contentSizeChanged), name: UIAccessibility.reduceMotionStatusDidChangeNotification, object: nil)
        if #available(iOS 17.0, *) {
            registerForTraitChanges([UITraitUserInterfaceStyle.self, UITraitDisplayScale.self]) { (vc: RyUIViewController, _: UITraitCollection) in
                vc.sendEnvironment()
                RyUIRuntime.shared.resolveLayerColors()
            }
            // Split View, Slide Over, Stage Manager: the size classes
            // usually change with the size, but not always.
            registerForTraitChanges([UITraitHorizontalSizeClass.self, UITraitVerticalSizeClass.self]) { (vc: RyUIViewController, _: UITraitCollection) in
                vc.sendEnvironment()
                RyUIRuntime.shared.reportScreens()
            }
        }
        let rt = RyUIRuntime.shared
        rt.scale = traitCollection.displayScale > 0 ? traitCollection.displayScale : UIScreen.main.scale
        sendEnvironment()
        rt.start(self)
    }

    // ryui/screen's lock and status bar.
    public override var supportedInterfaceOrientations: UIInterfaceOrientationMask {
        RyUIScreen.lock ?? super.supportedInterfaceOrientations
    }
    public override var preferredStatusBarStyle: UIStatusBarStyle { RyUIScreen.statusBarStyle }
    public override var prefersStatusBarHidden: Bool { RyUIScreen.statusBarHidden }

    func mount(_ v: UIView) {
        root?.removeFromSuperview()
        root = v
        view.addSubview(v)
    }

    public override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        RyUIRuntime.shared.adoptControllers(in: self)
        if let c = RyUIRuntime.shared.accent { view.window?.tintColor = c }
        if RyUIRuntime.shared.colorScheme != .unspecified { view.window?.overrideUserInterfaceStyle = RyUIRuntime.shared.colorScheme }
    }

    public override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        updateKeyboard()
        sendEnvironment()
    }

    public override func viewSafeAreaInsetsDidChange() {
        super.viewSafeAreaInsetsDidChange()
        sendEnvironment()
    }

    // Labels pick up the new Dynamic Type size in their own trait
    // update, after this notification: report it a turn later so the
    // core measures with the new fonts.
    /// Text size or Reduce Motion changed: the root and the screens report it.
    @objc func contentSizeChanged() {
        DispatchQueue.main.async {
            self.sendEnvironment()
            RyUIRuntime.shared.reportScreens()
        }
    }

    func updateKeyboard() {
        guard let end = keyboardEnd, let window = view.window else {
            keyboard = 0
            return
        }
        let frame = view.convert(end, from: window.screen.coordinateSpace)
        keyboard = max(0, view.bounds.maxY - frame.minY)
    }

    @objc func keyboardWillChange(_ note: Notification) {
        let info = note.userInfo ?? [:]
        if note.name == UIResponder.keyboardWillHideNotification {
            keyboardEnd = nil
        } else {
            keyboardEnd = (info[UIResponder.keyboardFrameEndUserInfoKey] as? NSValue)?.cgRectValue
        }
        updateKeyboard()
        let duration = (info[UIResponder.keyboardAnimationDurationUserInfoKey] as? Double) ?? 0.25
        let curve = (info[UIResponder.keyboardAnimationCurveUserInfoKey] as? UInt) ?? 7
        // The core lays out and applies frames inside this block, so the
        // views move with the keyboard.
        UIView.animate(withDuration: duration, delay: 0, options: UIView.AnimationOptions(rawValue: curve << 16)) {
            self.sendEnvironment()
            RyUIRuntime.shared.reportScreens()
        }
    }

    func sendEnvironment() {
        var e = RyUIEnvironment()
        let b = view.bounds
        e.width = Float(b.width)
        e.height = Float(b.height)
        let s = view.safeAreaInsets
        e.safeTop = Float(s.top)
        e.safeRight = Float(s.right)
        e.safeBottom = Float(s.bottom)
        e.safeLeft = Float(s.left)
        e.keyboard = Float(keyboard)
        let scale = traitCollection.displayScale
        e.scale = Float(scale > 0 ? scale : UIScreen.main.scale)
        e.fontScale = Float(UIFontMetrics.default.scaledValue(for: 17) / 17)
        e.dark = traitCollection.userInterfaceStyle == .dark
        e.reduceMotion = UIAccessibility.isReduceMotionEnabled
        e.widthClass = traitCollection.horizontalSizeClass.ryui
        e.heightClass = traitCollection.verticalSizeClass.ryui
        e.locale = Locale.current.identifier
        guard e != lastEnv, e.width > 0 else { return }
        lastEnv = e
        let rt = RyUIRuntime.shared
        rt.scale = CGFloat(e.scale)
        rt.send(RyUIEvents.environment(rt.rootID, e))
    }
}

extension UIUserInterfaceSizeClass {
    /// As ops.SizeClass: compact, or regular as expanded.
    var ryui: UInt8 {
        switch self {
        case .compact: return 1
        case .regular: return 3
        default: return 0
        }
    }
}
#endif
