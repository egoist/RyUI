#if canImport(UIKit)
import UIKit
#if canImport(NuiOps)
import NuiOps
#endif

/// Hosts the core's root view and reports its environment: size, safe
/// area, keyboard, scale, appearance and text size.
public final class NuiViewController: UIViewController {
    var keyboard: CGFloat = 0
    var keyboardEnd: CGRect?
    var lastEnv: NuiEnvironment?
    weak var root: UIView?

    public override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        let nc = NotificationCenter.default
        nc.addObserver(self, selector: #selector(keyboardWillChange(_:)), name: UIResponder.keyboardWillChangeFrameNotification, object: nil)
        nc.addObserver(self, selector: #selector(keyboardWillChange(_:)), name: UIResponder.keyboardWillHideNotification, object: nil)
        nc.addObserver(self, selector: #selector(contentSizeChanged), name: UIContentSizeCategory.didChangeNotification, object: nil)
        if #available(iOS 17.0, *) {
            registerForTraitChanges([UITraitUserInterfaceStyle.self, UITraitDisplayScale.self]) { (vc: NuiViewController, _: UITraitCollection) in
                vc.sendEnvironment()
            }
        }
        let rt = NuiRuntime.shared
        rt.scale = traitCollection.displayScale > 0 ? traitCollection.displayScale : UIScreen.main.scale
        sendEnvironment()
        rt.start(self)
    }

    func mount(_ v: UIView) {
        root?.removeFromSuperview()
        root = v
        view.addSubview(v)
    }

    public override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        NuiRuntime.shared.adoptControllers(in: self)
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
    @objc func contentSizeChanged() {
        DispatchQueue.main.async { self.sendEnvironment() }
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
            NuiRuntime.shared.reportScreens()
        }
    }

    func sendEnvironment() {
        var e = NuiEnvironment()
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
        e.locale = Locale.current.identifier
        guard e != lastEnv, e.width > 0 else { return }
        lastEnv = e
        let rt = NuiRuntime.shared
        rt.scale = CGFloat(e.scale)
        rt.send(NuiEvents.environment(rt.rootID, e))
    }
}
#endif
