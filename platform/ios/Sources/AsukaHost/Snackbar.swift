#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A Snackbar node. UIKit has none, so it's drawn as iOS draws its own
/// brief messages (Mail's Undo Send, Photos' Copied): a capsule in the
/// system material (Liquid Glass on iOS 26), centered above the tab bar or
/// toolbar and the keyboard, on the window's topmost presented content. It
/// springs in and out (a crossfade with Reduce Motion), a swipe down
/// dismisses it, VoiceOver announces it and reaches its button (its
/// escape gesture dismisses it), and it follows Dynamic Type, right to
/// left and dark mode.
final class AsukaSnackbarView: UIView {
    let nodeID: UInt32
    var message = "" {
        didSet {
            label.text = message
            accessibilityLabel = message
        }
    }
    var action = "" {
        didSet { setActionTitle() }
    }
    var duration: Int32 = AsukaSnackbarDuration.short.rawValue

    private let background: UIVisualEffectView
    private let stack = UIStackView()
    private let label = UILabel()
    private let button = UIButton(type: .system)
    private var bottom: NSLayoutConstraint?
    private var barBottom: NSLayoutConstraint?
    private var timer: Timer?
    private var remaining: TimeInterval = 0
    private var started: Date?
    /// Reported (its action, a timeout, a swipe) or destroyed: it's going.
    private var done = false
    private var leaving = false

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        if #available(iOS 26.0, *) {
            let glass = UIGlassEffect(style: .regular)
            glass.isInteractive = true
            background = UIVisualEffectView(effect: glass)
            background.cornerConfiguration = .capsule()
        } else {
            background = UIVisualEffectView(effect: UIBlurEffect(style: .systemThickMaterial))
            background.layer.cornerCurve = .continuous
            background.clipsToBounds = true
        }
        super.init(frame: .zero)
        translatesAutoresizingMaskIntoConstraints = false
        if #unavailable(iOS 26.0) {
            // Glass casts its own; the material needs one to lift off the content.
            layer.shadowColor = UIColor.black.cgColor
            layer.shadowOpacity = 0.15
            layer.shadowRadius = 16
            layer.shadowOffset = CGSize(width: 0, height: 4)
        }
        background.translatesAutoresizingMaskIntoConstraints = false
        addSubview(background)

        label.font = .preferredFont(forTextStyle: .subheadline)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = .label
        label.numberOfLines = 0
        label.isAccessibilityElement = false // the view is, and reads it
        label.setContentHuggingPriority(.defaultLow, for: .horizontal)
        label.setContentCompressionResistancePriority(.defaultHigh, for: .horizontal)

        var c = UIButton.Configuration.plain()
        c.contentInsets = NSDirectionalEdgeInsets(top: 6, leading: 10, bottom: 6, trailing: 10)
        c.titleTextAttributesTransformer = UIConfigurationTextAttributesTransformer { a in
            var a = a
            a.font = UIFontMetrics(forTextStyle: .subheadline).scaledFont(for: .systemFont(ofSize: UIFont.preferredFont(forTextStyle: .subheadline).pointSize, weight: .semibold))
            return a
        }
        button.configuration = c
        button.setContentHuggingPriority(.required, for: .horizontal)
        button.setContentCompressionResistancePriority(.required, for: .horizontal)
        button.isHidden = true
        button.addAction(UIAction { [weak self] _ in self?.acted() }, for: .primaryActionTriggered)

        stack.axis = .horizontal
        stack.alignment = .center
        stack.spacing = 8
        stack.translatesAutoresizingMaskIntoConstraints = false
        stack.addArrangedSubview(label)
        stack.addArrangedSubview(button)
        background.contentView.addSubview(stack)
        NSLayoutConstraint.activate([
            background.topAnchor.constraint(equalTo: topAnchor),
            background.bottomAnchor.constraint(equalTo: bottomAnchor),
            background.leadingAnchor.constraint(equalTo: leadingAnchor),
            background.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: background.contentView.topAnchor, constant: 10),
            stack.bottomAnchor.constraint(equalTo: background.contentView.bottomAnchor, constant: -10),
            stack.leadingAnchor.constraint(equalTo: background.contentView.leadingAnchor, constant: 20),
            stack.trailingAnchor.constraint(equalTo: background.contentView.trailingAnchor, constant: -10),
            heightAnchor.constraint(greaterThanOrEqualToConstant: 48),
        ])
        adaptToTextSize()

        let pan = UIPanGestureRecognizer(target: self, action: #selector(panned(_:)))
        addGestureRecognizer(pan)

        // VoiceOver: the message, then the button.
        isAccessibilityElement = false
        accessibilityContainerType = .semanticGroup
        let text = UIAccessibilityElement(accessibilityContainer: self)
        textElement = text
        text.accessibilityTraits = .staticText
        accessibilityElements = [text, button]
    }

    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        if previous?.preferredContentSizeCategory != traitCollection.preferredContentSizeCategory { adaptToTextSize() }
    }

    /// A spring that settles in about duration seconds (UIKit's, from iOS 16).
    private func spring(_ duration: TimeInterval, damping: CGFloat, velocity: CGFloat = 0,
                        _ animations: @escaping () -> Void, completion: ((Bool) -> Void)? = nil) {
        UIView.animate(withDuration: duration, delay: 0, usingSpringWithDamping: damping, initialSpringVelocity: velocity,
                       options: [.allowUserInteraction, .beginFromCurrentState], animations: animations, completion: completion)
    }

    private var textElement: UIAccessibilityElement?

    required init?(coder: NSCoder) { fatalError() }

    override func layoutSubviews() {
        super.layoutSubviews()
        if #unavailable(iOS 26.0) {
            // A capsule while it's one line, rounded as tall as it grows.
            background.layer.cornerRadius = min(bounds.height / 2, 26)
            layer.shadowPath = UIBezierPath(roundedRect: bounds, cornerRadius: background.layer.cornerRadius).cgPath
        }
        textElement?.accessibilityFrameInContainerSpace = label.convert(label.bounds, to: self)
        placeAboveBars()
        fitLabel()
    }

    /// At accessibility text sizes the button goes under the message.
    private func adaptToTextSize() {
        let big = traitCollection.preferredContentSizeCategory.isAccessibilityCategory
        stack.axis = big ? .vertical : .horizontal
        stack.alignment = big ? .leading : .center
        fitLabel()
    }

    private func setActionTitle() {
        button.isHidden = action.isEmpty
        button.configuration?.title = action
        button.accessibilityLabel = action
        fitLabel()
    }

    override func accessibilityPerformEscape() -> Bool {
        report(.dismissed)
        return true
    }

    // MARK: Showing

    /// Shows it at the bottom of the window's topmost presented content.
    func present(on vc: UIViewController) {
        guard superview == nil, let host = vc.view else { return }
        textElement?.accessibilityLabel = message
        host.addSubview(self)
        let side = widthAnchor.constraint(lessThanOrEqualTo: host.safeAreaLayoutGuide.widthAnchor, constant: -32)
        let readable = widthAnchor.constraint(lessThanOrEqualToConstant: 560)
        let keyboard = bottomAnchor.constraint(lessThanOrEqualTo: host.keyboardLayoutGuide.topAnchor, constant: -12)
        let bars = bottomAnchor.constraint(lessThanOrEqualTo: host.safeAreaLayoutGuide.bottomAnchor, constant: -12)
        let low = bottomAnchor.constraint(equalTo: host.bottomAnchor)
        low.priority = .defaultLow // as low as the bars and the keyboard let it
        barBottom = bars
        bottom = low
        NSLayoutConstraint.activate([
            centerXAnchor.constraint(equalTo: host.safeAreaLayoutGuide.centerXAnchor),
            side, readable, keyboard, bars, low,
        ])
        placeAboveBars()
        fitLabel()
        host.layoutIfNeeded()

        // In: springing up from below, or fading with Reduce Motion.
        let reduce = UIAccessibility.isReduceMotionEnabled
        alpha = 0
        if !reduce { transform = CGAffineTransform(translationX: 0, y: bounds.height + 24).scaledBy(x: 0.9, y: 0.9) }
        spring(reduce ? 0.25 : 0.5, damping: reduce ? 1 : 0.75) {
            self.alpha = 1
            self.transform = .identity
        }
        UIAccessibility.post(notification: .announcement, argument: announcement)
        start()
    }

    /// What VoiceOver says as it shows: the message, and that it has a button.
    private var announcement: NSAttributedString {
        let s = NSAttributedString(string: action.isEmpty ? message : "\(message), \(action)",
                                   attributes: [.accessibilitySpeechQueueAnnouncement: true])
        return s
    }

    /// The message wraps only past the widest the capsule can be (its
    /// window's width less its margins, at most 560 points), less its
    /// padding and its button: a multiline label needs the width to wrap
    /// at, or it wraps at whatever width it was given.
    private func fitLabel() {
        guard let host = superview else { return }
        let safe = host.bounds.inset(by: host.safeAreaInsets).width
        var w = min(safe - 32, 560) - 30 - (stack.axis == .horizontal ? stack.spacing : 0)
        if !button.isHidden && stack.axis == .horizontal { w -= button.intrinsicContentSize.width }
        w = max(w, 60)
        if abs(label.preferredMaxLayoutWidth - w) > 0.5 { label.preferredMaxLayoutWidth = w }
    }

    /// The tab bar or toolbar of the content it's on, if one shows: it
    /// stays above it. The safe area is the bar's already where it's the
    /// content's own (a tab's screen); this is for what sits over the bars.
    private func placeAboveBars() {
        guard let host = superview, let c = barBottom else { return }
        var top = host.bounds.maxY - host.safeAreaInsets.bottom
        for bar in bars(in: host) where bar.window != nil && !bar.isHidden && bar.alpha > 0.01 {
            let f = bar.convert(bar.bounds, to: host)
            if f.maxY >= host.bounds.maxY - host.safeAreaInsets.bottom - 1 { top = min(top, f.minY) }
        }
        let constant = -12 - max(0, host.bounds.maxY - host.safeAreaInsets.bottom - top)
        if abs(c.constant - constant) > 0.5 { c.constant = constant }
    }

    /// The bottom bars shown in host: tab bars and toolbars, down the
    /// visible controllers.
    private func bars(in host: UIView) -> [UIView] {
        var out: [UIView] = []
        func visit(_ vc: UIViewController) {
            if let t = vc as? UITabBarController {
                out.append(t.tabBar)
                if let s = t.selectedViewController { visit(s) }
                return
            }
            if let n = vc as? UINavigationController {
                if !n.isToolbarHidden { out.append(n.toolbar) }
                if let top = n.topViewController { visit(top) }
                return
            }
            for c in vc.children { visit(c) }
        }
        var r: UIResponder? = host
        while let x = r, !(x is UIViewController) { r = x.next }
        if let vc = r as? UIViewController { visit(vc) }
        return out
    }

    // MARK: Timing

    private func start() {
        guard duration != AsukaSnackbarDuration.indefinite.rawValue else { return }
        var t: TimeInterval = duration == AsukaSnackbarDuration.long.rawValue ? 10 : 4
        // iOS has no recommended timeout: as Material does without one,
        // with VoiceOver or Switch Control on, one with a button stays
        // until it's dismissed, and one without stays long.
        if UIAccessibility.isVoiceOverRunning || UIAccessibility.isSwitchControlRunning {
            if !action.isEmpty { return }
            t = max(t, 10)
        }
        remaining = t
        resume()
    }

    private func resume() {
        guard remaining > 0, timer == nil, !done else { return }
        started = Date()
        timer = Timer.scheduledTimer(withTimeInterval: remaining, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.report(.dismissed) }
        }
    }

    private func pause() {
        guard let t = timer, let s = started else { return }
        t.invalidate()
        timer = nil
        remaining = max(0.5, remaining - Date().timeIntervalSince(s))
    }

    // MARK: Leaving

    private func acted() {
        guard !done else { return }
        report(.action)
    }

    private func report(_ e: AsukaEventKind) {
        guard !done else { return }
        done = true
        timer?.invalidate()
        timer = nil
        let id = nodeID
        if e == .action {
            AsukaRuntime.shared.emit(id, .action) { AsukaEvents.action(id, 0) }
        } else {
            AsukaRuntime.shared.emit(id, .dismissed) { AsukaEvents.dismissed(id) }
        }
        leave(velocity: 0)
    }

    /// The core destroyed it: out, animated.
    func dismiss() {
        done = true
        timer?.invalidate()
        timer = nil
        leave(velocity: 0)
    }

    private func leave(velocity: CGFloat) {
        guard !leaving else { return }
        leaving = true
        isUserInteractionEnabled = false
        accessibilityElementsHidden = true
        guard superview != nil else { return }
        let reduce = UIAccessibility.isReduceMotionEnabled
        let travel = bounds.height + 24 + max(0, transform.ty)
        spring(reduce ? 0.2 : 0.35, damping: 1, velocity: velocity / max(travel, 1), {
            self.alpha = 0
            if !reduce { self.transform = CGAffineTransform(translationX: 0, y: travel).scaledBy(x: 0.95, y: 0.95) }
        }, completion: { _ in
            self.removeFromSuperview()
        })
    }

    // MARK: Swiping

    override func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
        guard let pan = g as? UIPanGestureRecognizer, pan.view === self else { return super.gestureRecognizerShouldBegin(g) }
        let v = pan.velocity(in: self)
        return abs(v.y) > abs(v.x) // down (or a little up), not across
    }

    @objc private func panned(_ g: UIPanGestureRecognizer) {
        guard !done else { return }
        let y = g.translation(in: superview).y
        switch g.state {
        case .began:
            pause()
        case .changed:
            // Down follows the finger; up resists.
            transform = CGAffineTransform(translationX: 0, y: y > 0 ? y : -sqrt(-y) * 2)
        case .ended, .cancelled:
            let v = g.velocity(in: superview).y
            if g.state == .ended && (y > bounds.height / 2 || v > 600) {
                done = true
                timer?.invalidate()
                timer = nil
                let id = nodeID
                AsukaRuntime.shared.emit(id, .dismissed) { AsukaEvents.dismissed(id) }
                leave(velocity: v)
            } else {
                spring(0.4, damping: 0.8) { self.transform = .identity }
                resume()
            }
        default:
            break
        }
    }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        super.touchesBegan(touches, with: event)
        pause() // as Android's: it waits while it's touched
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        super.touchesEnded(touches, with: event)
        resume()
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) {
        super.touchesCancelled(touches, with: event)
        resume()
    }
}

@MainActor extension AsukaRuntime {
    /// The controller a snackbar goes on: the topmost presented one that
    /// shows content (not an alert, which it would sit inside).
    var snackbarController: UIViewController? {
        var vc: UIViewController? = controller
        while let p = vc?.presentedViewController, !p.isBeingDismissed, !(p is UIAlertController) { vc = p }
        return vc
    }

    func presentSnackbar(_ n: AsukaNode) {
        guard let s = n.view as? AsukaSnackbarView, let vc = snackbarController else { return }
        s.present(on: vc)
    }
}
#endif
