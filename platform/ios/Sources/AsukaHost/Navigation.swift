#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A screen of a Stack, a page of Tabs or a sheet's content: its view is
/// sized by UIKit, and it reports its environment (size, safe area,
/// keyboard) to the core, which lays it out.
class AsukaScreenController: UIViewController {
    let nodeID: UInt32
    let content = AsukaContainerView()
    var lastEnv: AsukaEnvironment?
    var backGesture = true
    /// ui.OnBack: the core decides whether going back pops it.
    var interceptsBack = false
    /// ScreenOptions.Toolbar and Primary: its bottom toolbar's items.
    var toolbar: [AsukaAction] = []
    var primary: AsukaAction?

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(nibName: nil, bundle: nil)
    }
    required init?(coder: NSCoder) { fatalError() }

    override func loadView() { view = content }

    // asuka/sdk/screen's status bar, when this screen is presented full screen.
    override var preferredStatusBarStyle: UIStatusBarStyle { AsukaScreen.statusBarStyle }
    override var prefersStatusBarHidden: Bool { AsukaScreen.statusBarHidden }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        report()
        findContentScrollView()
    }

    private weak var scrollsContent: UIScrollView?

    /// Tells UIKit which scroll view the screen's bars follow, for its large
    /// title and scroll edge. It only looks down each view's first subview on
    /// its own, so a screen with something behind its list would never collapse
    /// its title. It's the first that takes up most of the screen: a row of
    /// photos that scrolls isn't what the bar should follow.
    private func findContentScrollView() {
        if let s = scrollsContent, s.window != nil, !s.isHidden { return }
        var queue: [UIView] = [content]
        var i = 0
        while i < queue.count {
            let v = queue[i]
            i += 1
            if let s = v as? UIScrollView, !s.isHidden, s.bounds.height >= content.bounds.height / 2, s.bounds.height > 0 {
                scrollsContent = s
                setContentScrollView(s, for: [.top, .bottom]) // the bottom: its toolbar
                return
            }
            if !(v is UIScrollView) { queue.append(contentsOf: v.subviews) }
        }
    }

    override func viewSafeAreaInsetsDidChange() {
        super.viewSafeAreaInsetsDidChange()
        report()
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        AsukaRuntime.shared.adoptControllers(in: self)
    }

    func report(force: Bool = false) {
        let rt = AsukaRuntime.shared
        guard view.bounds.width > 0, rt.nodes[nodeID] != nil else { return }
        let e = rt.environment(of: view)
        if !force && e == lastEnv { return }
        lastEnv = e
        rt.send(AsukaEvents.environment(nodeID, e))
    }
}

final class AsukaSheetController: AsukaScreenController, UIAdaptivePresentationControllerDelegate {
    var detents: Int64 = 0
    var grabber = false
    /// SheetProps.FullScreen: presented .fullScreen (SwiftUI's
    /// fullScreenCover), over everything, its view the screen's: its
    /// environment is the screen's size and safe area. Nothing but the
    /// core dismisses it.
    var fullScreen = false

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        guard let n = AsukaRuntime.shared.nodes[nodeID], !n.dying else { return }
        AsukaRuntime.shared.emit(nodeID, .dismissed) { AsukaEvents.dismissed(nodeID) }
    }

    /// A swipe down or a tap outside while it intercepts (isModalInPresentation): ui.OnBack decides.
    func presentationControllerDidAttemptToDismiss(_ presentationController: UIPresentationController) {
        guard let n = AsukaRuntime.shared.nodes[nodeID], !n.dying else { return }
        let id = nodeID
        AsukaRuntime.shared.emit(id, .back) { AsukaEvents.back(id) }
    }
}

/// A Popover's content: a popover on every size class (an iPhone's too,
/// as SwiftUI's popover with its compact adaptation turned off), as big as
/// the core laid its content out, pointing at its anchor.
final class AsukaPopoverController: AsukaScreenController, UIPopoverPresentationControllerDelegate {
    var anchor: UInt32 = 0
    var size = CGSize.zero {
        didSet { preferredContentSize = size }
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .clear // the popover's own background (Liquid Glass on iOS 26)
    }

    func adaptivePresentationStyle(for controller: UIPresentationController, traitCollection: UITraitCollection) -> UIModalPresentationStyle { .none }

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        guard let n = AsukaRuntime.shared.nodes[nodeID], !n.dying else { return }
        let id = nodeID
        AsukaRuntime.shared.emit(id, .dismissed) { AsukaEvents.dismissed(id) }
    }

    /// A tap outside while it intercepts (isModalInPresentation): ui.OnBack decides.
    func presentationControllerDidAttemptToDismiss(_ presentationController: UIPresentationController) {
        guard let n = AsukaRuntime.shared.nodes[nodeID], !n.dying else { return }
        let id = nodeID
        AsukaRuntime.shared.emit(id, .back) { AsukaEvents.back(id) }
    }
}

/// A Stack: screens are pushed by Insert and popped by Destroy; pops the
/// user makes (back button, swipe) are reported as Popped.
final class AsukaStackController: UINavigationController, UINavigationControllerDelegate, UIGestureRecognizerDelegate {
    let nodeID: UInt32
    /// Replace: the screens destroyed next, and the one inserted next, change at once.
    var instant = false
    /// What the stack is to become when the transition under way ends: UIKit
    /// drops a push or pop made during one, as a pop then a push would be.
    private var queued: [UIViewController]?

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(nibName: nil, bundle: nil)
    }
    required init?(coder: NSCoder) { fatalError() }

    /// The screens, with what's queued.
    var target: [UIViewController] { queued ?? viewControllers }

    /// Whether a change now must wait for the transition under way.
    var mustQueue: Bool { queued != nil || transitionCoordinator != nil }

    func enqueue(_ screens: [UIViewController]) { queued = screens }

    override func viewDidLoad() {
        super.viewDidLoad()
        delegate = self
        interactivePopGestureRecognizer?.delegate = self
        if #available(iOS 26.0, *) { interactiveContentPopGestureRecognizer?.delegate = self }
        navigationBar.prefersLargeTitles = true // each screen's largeTitleDisplayMode decides
    }

    func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
        guard viewControllers.count > 1 else { return false }
        guard let top = topViewController as? AsukaScreenController else { return true }
        return top.backGesture && !top.interceptsBack
    }

    /// Shows the bottom toolbar while the screen on top has items for it,
    /// sliding with a push or a pop; an interactive pop that's cancelled
    /// puts it back as it was.
    func navigationController(_ nav: UINavigationController, willShow vc: UIViewController, animated: Bool) {
        setToolbarHidden(vc.toolbarItems?.isEmpty ?? true, animated: animated)
        hideTabBar(for: vc, animated: animated)
        transitionCoordinator?.notifyWhenInteractionChanges { [weak self] ctx in
            guard ctx.isCancelled, let self, let from = ctx.viewController(forKey: .from) else { return }
            self.setToolbarHidden(from.toolbarItems?.isEmpty ?? true, animated: true)
            self.hideTabBar(for: from, animated: true)
        }
    }

    /// The top screen's toolbar items changed, or it was pushed: the
    /// toolbar shows while it has some.
    func showToolbar() {
        guard transitionCoordinator == nil, let top = topViewController else { return }
        let hidden = top.toolbarItems?.isEmpty ?? true
        if hidden != isToolbarHidden { setToolbarHidden(hidden, animated: view.window != nil) }
        hideTabBar(for: top, animated: view.window != nil)
    }

    /// Whether this stack hid its tabs' tab bar.
    private var hidTabBar = false

    /// A pushed screen with a toolbar covers the tab bar, as a message in
    /// Mail does (hidesBottomBarWhenPushed, which UIKit only honors for a
    /// tab's own navigation controller): the toolbar takes the bottom edge,
    /// where iOS 26's floating tab bar would lie over it. A primary action
    /// alone sits beside the tab bar. Before iOS 18 the toolbar stays above
    /// the tab bar, as UIKit stacks them.
    func hideTabBar(for vc: UIViewController, animated: Bool) {
        guard #available(iOS 18.0, *), let tabs = tabBarController else { return }
        let hide = vc !== viewControllers.first && !((vc as? AsukaScreenController)?.toolbar.isEmpty ?? true)
        guard hide != hidTabBar else { return }
        hidTabBar = hide
        tabs.setTabBarHidden(hide, animated: animated)
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        // Going away (a split's detail replaced): the tab bar comes back.
        if #available(iOS 18.0, *), hidTabBar, isMovingFromParent || isBeingDismissed {
            hidTabBar = false
            tabBarController?.setTabBarHidden(false, animated: animated)
        }
    }

    func navigationController(_ nav: UINavigationController, didShow vc: UIViewController, animated: Bool) {
        AsukaRuntime.shared.stackDidShow(self)
        if let q = queued {
            queued = nil
            if q != viewControllers { setViewControllers(q, animated: view.window != nil) }
        }
    }
}

final class AsukaTabsController: UITabBarController, UITabBarControllerDelegate {
    let nodeID: UInt32

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(nibName: nil, bundle: nil)
        delegate = self
    }
    required init?(coder: NSCoder) { fatalError() }

    func tabBarController(_ t: UITabBarController, didSelect vc: UIViewController) {
        let i = Int64(selectedIndex)
        AsukaRuntime.shared.emit(nodeID, .selected) { AsukaEvents.selected(nodeID, i) }
    }
}

/// An alert. Its title and message are attributed text, whose natural
/// alignment is the process's and not the view's: where the app goes another
/// way, they're aligned to its side.
final class AsukaAlertController: UIAlertController {
    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        let rt = AsukaRuntime.shared
        guard rt.rtl != rt.systemRTL else { return }
        let side: NSTextAlignment = rt.rtl ? .right : .left
        let other: NSTextAlignment = rt.rtl ? .left : .right
        func walk(_ v: UIView) {
            if let l = v as? UILabel, let t = l.text, t == title || t == message,
               l.textAlignment == .natural || l.textAlignment == other {
                l.textAlignment = side
            }
            for s in v.subviews { walk(s) }
        }
        walk(view)
    }
}

/// An alert's text field's delegate, in front of UIKit's own (the
/// alert's), which gets everything else: return goes on only if
/// shouldReturn says so.
final class AsukaAlertFieldDelegate: NSObject, UITextFieldDelegate {
    weak var inner: UITextFieldDelegate?
    let shouldReturn: () -> Bool

    init(_ inner: UITextFieldDelegate?, shouldReturn: @escaping () -> Bool) {
        self.inner = inner
        self.shouldReturn = shouldReturn
    }

    func textFieldShouldReturn(_ tf: UITextField) -> Bool {
        guard shouldReturn() else { return false }
        return inner?.textFieldShouldReturn?(tf) ?? true
    }

    override func responds(to sel: Selector!) -> Bool {
        super.responds(to: sel) || (inner?.responds(to: sel) ?? false)
    }

    override func forwardingTarget(for sel: Selector!) -> Any? {
        if let inner, inner.responds(to: sel) { return inner }
        return super.forwardingTarget(for: sel)
    }
}

/// Shows a container's menu on a long press.
final class AsukaContextMenu: NSObject, UIContextMenuInteractionDelegate {
    weak var node: AsukaNode?
    init(node: AsukaNode) { self.node = node }

    func contextMenuInteraction(_ i: UIContextMenuInteraction, configurationForMenuAtLocation p: CGPoint) -> UIContextMenuConfiguration? {
        guard let node else { return nil }
        let menu = AsukaRuntime.shared.menu(for: node)
        return UIContextMenuConfiguration(identifier: nil, previewProvider: nil) { _ in menu }
    }
}

/// An action of a menu, an alert or a navigation bar (AsukaOps' reader:
/// its number, flags and children).
typealias AsukaAction = AsukaActionItem

extension AsukaActionItem {
    /// The icon's symbol, if it has one.
    var image: UIImage? { icon.isEmpty ? nil : UIImage(systemName: icon) }
}

func decodeActions(_ b: UnsafeRawBufferPointer) -> [AsukaAction] { AsukaActions.decode(b) }

@MainActor extension AsukaRuntime {
    var topController: UIViewController? {
        var vc: UIViewController? = controller
        while let p = vc?.presentedViewController, !p.isBeingDismissed { vc = p }
        return vc
    }

    /// The environment of a view: its size, safe area and the keyboard's
    /// overlap with it.
    func environment(of view: UIView) -> AsukaEnvironment {
        var e = AsukaEnvironment()
        let b = view.bounds
        e.width = Float(b.width)
        e.height = Float(b.height)
        let s = view.safeAreaInsets
        e.safeTop = Float(s.top)
        e.safeRight = Float(s.right)
        e.safeBottom = Float(s.bottom)
        e.safeLeft = Float(s.left)
        if let end = controller?.keyboardEnd, let window = view.window {
            let f = view.convert(end, from: window.screen.coordinateSpace)
            e.keyboard = Float(max(0, b.maxY - f.minY))
        }
        let t = view.traitCollection
        e.scale = Float(t.displayScale > 0 ? t.displayScale : UIScreen.main.scale)
        e.fontScale = Float(UIFontMetrics.default.scaledValue(for: 17) / 17)
        e.dark = t.userInterfaceStyle == .dark
        e.reduceMotion = UIAccessibility.isReduceMotionEnabled
        e.screenReader = UIAccessibility.isVoiceOverRunning
        e.widthClass = t.horizontalSizeClass.asuka
        e.heightClass = t.verticalSizeClass.asuka
        e.setLanguages()
        return e
    }

    /// Reports every screen's environment again (the keyboard moved).
    func reportScreens() {
        for n in nodes.values {
            (n.vc as? AsukaScreenController)?.report()
        }
    }

    /// The controller whose view holds node's view: the nearest screen or
    /// sheet, else the root.
    func hostController(of node: AsukaNode) -> UIViewController? {
        var p = node.parent
        while let n = p {
            if let vc = n.vc as? AsukaScreenController { return vc }
            p = n.parent
        }
        return controller
    }

    /// Makes the stacks and tabs under vc's view its children (they may
    /// have been attached before their screen existed).
    func adoptControllers(in vc: UIViewController) {
        for n in nodes.values {
            guard let child = n.vc, child is AsukaStackController || child is AsukaTabsController || child is AsukaSplitController,
                  child.parent !== vc, child.view.isDescendant(of: vc.view) else { continue }
            if let owner = hostController(of: n), owner !== vc, child.parent === owner { continue }
            child.willMove(toParent: nil)
            child.removeFromParent()
            vc.addChild(child)
            child.didMove(toParent: vc)
        }
    }

    func makeController(_ id: UInt32, _ kind: AsukaKind) -> (UIView, UIViewController?)? {
        switch kind {
        case .stack:
            let s = AsukaStackController(nodeID: id)
            return (s.view, s)
        case .screen:
            let s = AsukaScreenController(nodeID: id)
            return (s.content, s)
        case .tabs:
            let t = AsukaTabsController(nodeID: id)
            return (t.view, t)
        case .sheet:
            let s = AsukaSheetController(nodeID: id)
            return (s.content, s)
        case .popover:
            let p = AsukaPopoverController(nodeID: id)
            return (p.content, p)
        case .split:
            let s = AsukaSplitController(nodeID: id)
            return (s.view, s)
        case .alert:
            return (UIView(), nil)
        case .snackbar:
            return (AsukaSnackbarView(nodeID: id), nil)
        default:
            return nil
        }
    }

    /// Handles inserting into a Stack or Tabs; false: an ordinary insert.
    func insertController(_ p: AsukaNode, _ c: AsukaNode, _ index: Int) -> Bool {
        if let split = p.vc as? AsukaSplitController, let stack = c.vc as? AsukaStackController {
            p.children.insert(c, at: min(index, p.children.count))
            c.parent = p
            split.setViewController(stack, for: .secondary)
            return true
        }
        if let nav = p.vc as? AsukaStackController, let screen = c.vc {
            let i = min(index, p.children.count)
            p.children.insert(c, at: i)
            c.parent = p
            if i == nav.viewControllers.count {
                if #available(iOS 18, *), let key = c.zoomFrom, i > 0 {
                    // Zooms out of the view with the key on the screen
                    // below, and into it as it's popped (asked each time).
                    // A list's cells aren't in the tree: then the view
                    // registered with the key, if it isn't this screen's.
                    let below = p.children[i - 1]
                    screen.preferredTransition = .zoom { [weak self, weak below, weak screen] _ in
                        if let m = below.flatMap({ AsukaRuntime.matched(key, in: $0) }) { return m.view }
                        guard let m = self?.matchViews[key], m.view.window != nil, let s = screen?.view, !m.view.isDescendant(of: s) else { return nil }
                        return m.view
                    }
                }
                if nav.mustQueue {
                    nav.enqueue(nav.target + [screen])
                } else {
                    nav.pushViewController(screen, animated: !nav.instant && !nav.viewControllers.isEmpty && nav.view.window != nil)
                    nav.showToolbar() // a first screen: no transition to show it with
                }
                nav.instant = false
            } else {
                var vcs = nav.viewControllers
                vcs.insert(screen, at: min(i, vcs.count))
                nav.setViewControllers(vcs, animated: false)
            }
            return true
        }
        if let tabs = p.vc as? AsukaTabsController {
            let i = min(index, p.children.count)
            p.children.insert(c, at: i)
            c.parent = p
            tabs.setViewControllers(p.children.compactMap { $0.vc }, animated: false)
            if let sel = p.selected { tabs.selectedIndex = sel }
            return true
        }
        if let child = c.vc, child is AsukaStackController || child is AsukaTabsController || child is AsukaSplitController {
            let owner = hostController(of: p) ?? controller
            owner?.addChild(child)
            p.insert(c, at: index)
            child.didMove(toParent: owner)
            return true
        }
        return false
    }

    /// Destroys a node that has a controller or sits under one being
    /// dismissed; false: an ordinary destroy.
    func destroyController(_ n: AsukaNode) -> Bool {
        if n.underDying {
            return true // its views leave with the animating screen
        }
        guard let vc = n.vc else { return false }
        n.dying = true
        if let nav = n.parent?.vc as? AsukaStackController {
            n.parent?.children.removeAll { $0 === n }
            if nav.mustQueue {
                nav.enqueue(nav.target.filter { $0 !== vc })
            } else if nav.instant {
                // A SplitView's destination changing: gone at once, the
                // stack's first screen too.
                nav.setViewControllers(nav.viewControllers.filter { $0 !== vc }, animated: false)
            } else if nav.topViewController === vc {
                nav.popViewController(animated: nav.view.window != nil)
            } else if nav.viewControllers.contains(vc) {
                nav.setViewControllers(nav.viewControllers.filter { $0 !== vc }, animated: false)
            }
            return true
        }
        if let tabs = n.parent?.vc as? AsukaTabsController {
            n.parent?.children.removeAll { $0 === n }
            tabs.setViewControllers(n.parent?.children.compactMap { $0.vc }, animated: false)
            return true
        }
        if vc is AsukaSheetController || vc is AsukaPopoverController || vc is UIAlertController {
            if vc.presentingViewController != nil && !vc.isBeingDismissed {
                vc.dismiss(animated: true)
            }
            return true
        }
        // A stack or tabs inside the tree.
        vc.willMove(toParent: nil)
        n.parent?.remove(n)
        vc.removeFromParent()
        return true
    }

    /// The user popped screens (back button, swipe): tell the core.
    func stackDidShow(_ nav: AsukaStackController) {
        guard let node = nodes[nav.nodeID] else { return }
        let shown = Set(nav.target.map { ObjectIdentifier($0) })
        for c in node.children.reversed() where !c.dying {
            guard let vc = c.vc, !shown.contains(ObjectIdentifier(vc)) else { continue }
            c.dying = true
            node.children.removeAll { $0 === c }
            let id = c.id
            emit(nav.nodeID, .popped) { AsukaEvents.popped(nav.nodeID, Int64(id)) }
        }
    }

    func present(_ n: AsukaNode, tries: Int = 40) {
        // A sheet or an alert in the app's first frame: UIKit won't present
        // from a controller that's not in a window yet, so it waits to be.
        if let top = topController, top.viewIfLoaded?.window == nil, tries > 0 {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) { [weak self, weak n] in
                if let n, self?.nodes[n.id] === n { self?.present(n, tries: tries - 1) }
            }
            return
        }
        switch n.kind {
        case .sheet:
            guard let s = n.vc as? AsukaSheetController, let top = topController else { return }
            // The style before the presentation controller is first asked
            // for: it's made for the style it has then.
            s.modalPresentationStyle = s.fullScreen ? .fullScreen : .automatic
            if !s.fullScreen, let sheet = s.sheetPresentationController {
                var ds: [UISheetPresentationController.Detent] = []
                if s.detents & 1 != 0 { ds.append(.medium()) }
                if s.detents & 2 != 0 || ds.isEmpty { ds.append(.large()) }
                sheet.detents = ds
                sheet.prefersGrabberVisible = s.grabber
            }
            s.presentationController?.delegate = s
            top.present(s, animated: true)
        case .popover:
            guard let p = n.vc as? AsukaPopoverController, let top = topController,
                  let anchor = nodes[p.anchor]?.view, anchor.window != nil else { return }
            p.modalPresentationStyle = .popover
            p.preferredContentSize = p.size
            if let pc = p.popoverPresentationController {
                pc.sourceView = anchor
                pc.sourceRect = anchor.bounds
                pc.delegate = p
            }
            top.present(p, animated: true)
        case .alert:
            guard let top = topController else { return }
            let a = AsukaAlertController(title: n.alertTitle, message: n.alertMessage,
                                        preferredStyle: n.alertStyle == AsukaAlertStyle.actionSheet.rawValue ? .actionSheet : .alert)
            let id = n.id
            for act in n.actions {
                let style: UIAlertAction.Style = act.role == AsukaAlertButtonRole.cancel.rawValue ? .cancel
                    : act.role == AsukaAlertButtonRole.destructive.rawValue ? .destructive : .default
                let index = act.index
                let action = UIAlertAction(title: act.title, style: style) { [weak self] _ in
                    self?.emit(id, .action) { AsukaEvents.action(id, index) }
                }
                action.isEnabled = !act.disabled
                a.addAction(action)
            }
            if a.preferredStyle == .alert && !n.alertFields.isEmpty {
                addFields(n, to: a)
            }
            if let pop = a.popoverPresentationController, let v = top.view {
                pop.sourceView = v
                pop.sourceRect = CGRect(x: v.bounds.midX, y: v.bounds.midY, width: 0, height: 0)
                pop.permittedArrowDirections = []
            }
            n.vc = a
            top.present(a, animated: true)
        case .snackbar:
            presentSnackbar(n)
        default:
            break
        }
    }

    /// Adds an alert's text fields, which report the user's typing with
    /// FieldChanged; the alert focuses the first as it shows. Return
    /// presses the preferred action, the default one (the first whose role
    /// is Default), which UIKit bolds; a field before the last moves on to
    /// the next.
    func addFields(_ n: AsukaNode, to a: UIAlertController) {
        let id = n.id
        let count = n.alertFields.count
        for (i, f) in n.alertFields.enumerated() {
            a.addTextField { [weak self] tf in
                guard let self else { return }
                tf.text = f.text
                tf.placeholder = f.placeholder
                tf.isSecureTextEntry = f.secure
                tf.keyboardType = keyboardType(f.keyboard)
                if !f.contentType.isEmpty { tf.textContentType = contentType(f.contentType) }
                // A password, an account's name, an address: as typed.
                if f.secure || f.keyboard == AsukaKeyboard.email.rawValue || f.keyboard == AsukaKeyboard.url.rawValue
                    || ["username", "password", "newPassword", "oneTimeCode", "email", "url"].contains(f.contentType) {
                    tf.autocapitalizationType = .none
                    tf.autocorrectionType = .no
                }
                tf.returnKeyType = i < count - 1 ? .next : .default
                let attr = self.appAttribute
                if attr != .unspecified {
                    tf.semanticContentAttribute = attr
                    tf.textAlignment = self.startAlignment(self.rtl)
                }
                tf.addAction(UIAction { [weak self, weak tf] _ in
                    let text = tf?.text ?? ""
                    self?.emit(id, .fieldChanged) { AsukaEvents.fieldChanged(id, AsukaAlertFields.changed(i, text)) }
                }, for: .editingChanged)
            }
        }
        var preferred: UIAlertAction?
        if let k = n.actions.firstIndex(where: { $0.role == AsukaAlertButtonRole.default.rawValue }), k < a.actions.count {
            preferred = a.actions[k]
            a.preferredAction = preferred
        }
        // Return: before the last field, on to the next; in the last, the
        // alert's own (the preferred action) only while it's enabled, as
        // UIKit dismisses the alert otherwise.
        n.alertFieldDelegates = (a.textFields ?? []).enumerated().map { i, tf in
            let d = AsukaAlertFieldDelegate(tf.delegate) { [weak a, weak preferred] in
                guard let fields = a?.textFields else { return false }
                if i + 1 < fields.count {
                    fields[i + 1].becomeFirstResponder()
                    return false
                }
                return preferred?.isEnabled == true
            }
            tf.delegate = d
            return d
        }
    }

    func menu(for n: AsukaNode) -> UIMenu { menu(of: n.actions, title: "", for: n.id) }

    /// A UIMenu of actions whose items send Action to the node with id:
    /// disabled and checked items, submenus, and sections shown inline
    /// under their titles (as SwiftUI's Section in a Menu).
    /// Action events carry base + an item's number (a screen's Toolbar's and
    /// Primary's: AsukaActions.toolbarBase, primaryBase).
    func menu(of actions: [AsukaAction], title: String, image: UIImage? = nil, inline: Bool = false, for id: UInt32, base: Int64 = 0) -> UIMenu {
        let items: [UIMenuElement] = actions.compactMap { act in
            if act.role == AsukaAlertButtonRole.cancel.rawValue { return nil } // the menu's dismissal
            if act.submenu {
                // A disabled submenu: its title, dimmed, opening nothing.
                if act.disabled && !act.inline { return UIAction(title: act.title, image: act.image, attributes: .disabled) { _ in } }
                return menu(of: act.items, title: act.title, image: act.image, inline: act.inline, for: id, base: base)
            }
            var attrs: UIMenuElement.Attributes = []
            if act.role == AsukaAlertButtonRole.destructive.rawValue { attrs.insert(.destructive) }
            if act.disabled { attrs.insert(.disabled) }
            let index = base + act.index
            return UIAction(title: act.title, image: act.image, attributes: attrs, state: act.checked ? .on : .off) { [weak self] _ in
                self?.emit(id, .action) { AsukaEvents.action(id, index) }
            }
        }
        return UIMenu(title: title, image: image, options: inline ? .displayInline : [], children: items)
    }

    /// Installs a node's menu: on a button, as its primary action; on a
    /// view, a tap (an invisible button over it) or a long press (a
    /// context menu).
    func installMenu(_ n: AsukaNode) {
        n.menuOverlay?.removeFromSuperview()
        n.menuOverlay = nil
        if let i = n.contextMenu { n.view.removeInteraction(i) }
        n.contextMenu = nil
        guard !n.actions.isEmpty else { return }
        if let b = n.view as? UIButton {
            b.menu = menu(for: n)
            b.showsMenuAsPrimaryAction = true
            return
        }
        if n.menuTrigger == AsukaMenuTrigger.longPress.rawValue {
            let d = AsukaContextMenu(node: n)
            n.contextMenuDelegate = d
            let i = UIContextMenuInteraction(delegate: d)
            n.view.addInteraction(i)
            n.contextMenu = i
            return
        }
        let b = UIButton(type: .system)
        b.frame = n.view.bounds
        b.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        b.menu = menu(for: n)
        b.showsMenuAsPrimaryAction = true
        n.view.addSubview(b)
        n.menuOverlay = b
    }

    /// ui.OnBack on a screen (its back button asks the core, its edge swipe
    /// is off) or a sheet (it can't be swiped away; trying asks the core).
    /// The root has no back on iOS.
    func setInterceptBack(_ n: AsukaNode, _ on: Bool) {
        let id = n.id
        switch n.kind {
        case .screen:
            guard let vc = n.vc as? AsukaScreenController else { return }
            vc.interceptsBack = on
            vc.navigationItem.backAction = on ? UIAction { [weak self] _ in
                self?.emit(id, .back) { AsukaEvents.back(id) }
            } : nil
        case .sheet, .popover:
            n.vc?.isModalInPresentation = on
        default:
            break
        }
    }

    func setNavProp(_ n: AsukaNode, _ prop: UInt16, _ v: AsukaValue) {
        switch (n.kind, prop) {
        case (.screen, AsukaProp.Screen.title):
            n.vc?.navigationItem.title = v.string
            n.vc?.title = v.string
            n.vc?.tabBarItem.accessibilityLabel = v.string // it keeps the first title otherwise
        case (.screen, AsukaProp.Screen.largeTitle):
            n.vc?.navigationItem.largeTitleDisplayMode = v.bool ? .always : .never
        case (.screen, AsukaProp.Screen.icon):
            n.vc?.tabBarItem.image = UIImage(systemName: v.string)
        case (.screen, AsukaProp.Screen.badge):
            // The system's badge, which VoiceOver reads with the tab.
            n.vc?.tabBarItem.badgeValue = v.string.isEmpty ? nil : v.string
        case (.screen, AsukaProp.Screen.search):
            guard let vc = n.vc else { return }
            let s = n.search ?? AsukaSearch(id: n.id, runtime: self)
            n.search = s
            s.controller.searchBar.placeholder = v.string
            vc.navigationItem.searchController = s.controller
            vc.navigationItem.hidesSearchBarWhenScrolling = false
            vc.definesPresentationContext = true
        case (.screen, AsukaProp.Screen.searchButton):
            guard let vc = n.vc, v.bool else { return }
            if #available(iOS 26.0, *) {
                vc.navigationItem.preferredSearchBarPlacement = .integratedButton
                vc.navigationItem.searchBarPlacementAllowsToolbarIntegration = false
                n.search?.titled = vc
            } else {
                vc.navigationItem.hidesSearchBarWhenScrolling = true
            }
        case (.screen, AsukaProp.Screen.searchText):
            n.search?.setText(v.string)
        case (.screen, AsukaProp.Screen.zoomFrom):
            n.zoomFrom = v.string.isEmpty ? nil : v.string
        case (.screen, AsukaProp.Screen.backGesture):
            (n.vc as? AsukaScreenController)?.backGesture = v.bool
        case (.screen, AsukaProp.Screen.actions):
            guard case .bytes(let b) = v, let vc = n.vc else { return }
            let id = n.id
            vc.navigationItem.rightBarButtonItems = decodeActions(b).map { act in
                // An icon stands for the title, which VoiceOver reads.
                let item: UIBarButtonItem
                if act.submenu {
                    // With children, it opens them as a menu (SwiftUI's Menu in a toolbar).
                    item = UIBarButtonItem(title: act.image == nil ? act.title : nil, image: act.image,
                                           menu: menu(of: act.items, title: "", for: id))
                } else {
                    let index = act.index
                    item = UIBarButtonItem(title: act.image == nil ? act.title : nil, image: act.image, primaryAction: UIAction(title: act.title) { [weak self] _ in
                        self?.emit(id, .action) { AsukaEvents.action(id, index) }
                    })
                }
                item.accessibilityLabel = act.title
                item.isEnabled = !act.disabled
                if act.role == AsukaAlertButtonRole.destructive.rawValue { item.tintColor = .systemRed }
                return item
            }
        case (.screen, AsukaProp.Screen.toolbar):
            guard case .bytes(let b) = v, let vc = n.vc as? AsukaScreenController else { return }
            vc.toolbar = decodeActions(b)
            setToolbar(vc)
        case (.screen, AsukaProp.Screen.primary):
            guard case .bytes(let b) = v, let vc = n.vc as? AsukaScreenController else { return }
            vc.primary = decodeActions(b).first
            setToolbar(vc)
        case (.screen, AsukaProp.Screen.primaryExtended):
            break // Android's extended FAB: iOS's toolbar item is as it is
        case (.tabs, AsukaProp.Tabs.selected):
            n.selected = Int(v.int)
            (n.vc as? AsukaTabsController)?.selectedIndex = Int(v.int)
        case (.popover, AsukaProp.Popover.anchor):
            (n.vc as? AsukaPopoverController)?.anchor = UInt32(truncatingIfNeeded: v.int)
        case (.popover, AsukaProp.Popover.width):
            guard let p = n.vc as? AsukaPopoverController else { return }
            p.size.width = CGFloat(v.float)
        case (.popover, AsukaProp.Popover.height):
            guard let p = n.vc as? AsukaPopoverController else { return }
            p.size.height = CGFloat(v.float)
        case (.sheet, AsukaProp.Sheet.detents):
            (n.vc as? AsukaSheetController)?.detents = v.int
        case (.sheet, AsukaProp.Sheet.grabber):
            (n.vc as? AsukaSheetController)?.grabber = v.bool
        case (.sheet, AsukaProp.Sheet.fullScreen):
            (n.vc as? AsukaSheetController)?.fullScreen = v.bool
        case (.alert, AsukaProp.Alert.title):
            n.alertTitle = v.string
        case (.alert, AsukaProp.Alert.message):
            n.alertMessage = v.string
        case (.alert, AsukaProp.Alert.buttons):
            guard case .bytes(let b) = v else { return }
            n.actions = decodeActions(b)
            // Showing, its buttons follow Disabled (a UIAlertAction's title
            // is its own once it's made).
            if let a = n.vc as? UIAlertController {
                for (act, button) in zip(n.actions, a.actions) { button.isEnabled = !act.disabled }
            }
        case (.alert, AsukaProp.Alert.style):
            n.alertStyle = v.enumValue
        case (.alert, AsukaProp.Alert.fields):
            guard case .bytes(let b) = v else { return }
            n.alertFields = AsukaAlertFields.decode(b)
        case (.sheet, AsukaProp.Sheet.window), (.alert, AsukaProp.Alert.window), (.snackbar, AsukaProp.Snackbar.window):
            break // the window it's declared in: a phone has one
        case (.snackbar, AsukaProp.Snackbar.message):
            (n.view as? AsukaSnackbarView)?.message = v.string
        case (.snackbar, AsukaProp.Snackbar.action):
            (n.view as? AsukaSnackbarView)?.action = v.string
        case (.snackbar, AsukaProp.Snackbar.duration):
            (n.view as? AsukaSnackbarView)?.duration = v.enumValue
        case (.scroll, AsukaProp.Scroll.refreshable), (.list, AsukaProp.List.refreshable):
            guard let s = n.view as? UIScrollView else { return }
            if v.bool {
                let rc = UIRefreshControl()
                let id = n.id
                rc.addAction(UIAction { [weak self] _ in self?.emit(id, .refresh) { AsukaEvents.refresh(id) } }, for: .valueChanged)
                s.refreshControl = rc
            } else {
                s.refreshControl = nil
            }
        case (.scroll, AsukaProp.Scroll.refreshing), (.list, AsukaProp.List.refreshing):
            guard let rc = (n.view as? UIScrollView)?.refreshControl else { return }
            if v.bool { rc.beginRefreshing() } else { rc.endRefreshing() }
        case (.list, AsukaProp.List.columns):
            n.list?.setGrid(columns: Int(v.int), gap: n.list?.gap ?? 0)
        case (.list, AsukaProp.List.gap):
            n.list?.setGrid(columns: n.list?.columns ?? 1, gap: CGFloat(v.float))
        case (.list, AsukaProp.List.appearance):
            n.list?.setAppearance(AsukaListAppearance(rawValue: v.enumValue) ?? .plain, inset: n.list?.inset ?? 0)
        case (.list, AsukaProp.List.inset):
            n.list?.setAppearance(n.list?.appearance ?? .plain, inset: CGFloat(v.float))
        case (.list, AsukaProp.List.anchor):
            n.list?.setAnchor(AsukaScrollAnchor(rawValue: v.enumValue) ?? .top)
        case (.list, AsukaProp.List.selecting):
            n.list?.setEditing(v.bool)
        default:
            break
        }
    }

    /// A screen's bottom toolbar (SwiftUI's .bottomBar): its Toolbar's
    /// buttons with flexible space between them, and its Primary action as
    /// the trailing item, prominent on iOS 26 (Mail's Compose; iOS has no
    /// floating button). The navigation controller shows the toolbar while
    /// the screen with items is on top (AsukaStackController).
    func setToolbar(_ vc: AsukaScreenController) {
        let id = vc.nodeID
        var items: [UIBarButtonItem] = []
        for act in vc.toolbar {
            if !items.isEmpty { items.append(.flexibleSpace()) }
            items.append(barButton(act, for: id, base: AsukaActions.toolbarBase))
        }
        if let p = vc.primary {
            items.append(.flexibleSpace())
            let item = barButton(p, for: id, base: AsukaActions.primaryBase)
            if #available(iOS 26.0, *) {
                item.style = .prominent
            } else {
                item.style = .done
            }
            items.append(item)
        }
        vc.setToolbarItems(items, animated: vc.view.window != nil)
        (vc.navigationController as? AsukaStackController)?.showToolbar()
    }

    /// A bar button of an action: its icon, else its title (VoiceOver reads
    /// the title either way); one with children opens them as a menu.
    /// Action events carry base + the item's number.
    func barButton(_ act: AsukaAction, for id: UInt32, base: Int64) -> UIBarButtonItem {
        let item: UIBarButtonItem
        let title = act.image == nil ? act.title : nil
        if act.submenu {
            item = UIBarButtonItem(title: title, image: act.image, menu: menu(of: act.items, title: "", for: id, base: base))
        } else {
            let index = base + act.index
            item = UIBarButtonItem(title: title, image: act.image, primaryAction: UIAction(title: act.title) { [weak self] _ in
                self?.emit(id, .action) { AsukaEvents.action(id, index) }
            })
        }
        item.accessibilityLabel = act.title
        item.isEnabled = !act.disabled
        if act.role == AsukaAlertButtonRole.destructive.rawValue { item.tintColor = .systemRed }
        return item
    }

    func haptic(_ kind: Int32) {
        switch AsukaHaptic(rawValue: kind) {
        case .selection: UISelectionFeedbackGenerator().selectionChanged()
        case .light: UIImpactFeedbackGenerator(style: .light).impactOccurred()
        case .medium: UIImpactFeedbackGenerator(style: .medium).impactOccurred()
        case .heavy: UIImpactFeedbackGenerator(style: .heavy).impactOccurred()
        case .success: UINotificationFeedbackGenerator().notificationOccurred(.success)
        case .warning: UINotificationFeedbackGenerator().notificationOccurred(.warning)
        case .error: UINotificationFeedbackGenerator().notificationOccurred(.error)
        default: break
        }
    }
}

/// A screen's search field (ScreenOptions.Search): typing and the search
/// key go to the screen's node.
@MainActor final class AsukaSearch: NSObject, UISearchResultsUpdating, UISearchBarDelegate, UISearchControllerDelegate {
    let controller = UISearchController(searchResultsController: nil)
    let id: UInt32
    weak var runtime: AsukaRuntime?
    private var setting = false
    /// A search button's screen: its large title makes way for the field while
    /// it's open, as nothing else moves it.
    weak var titled: UIViewController?
    private var titleMode: UINavigationItem.LargeTitleDisplayMode?

    func willPresentSearchController(_ c: UISearchController) {
        guard let item = titled?.navigationItem else { return }
        titleMode = item.largeTitleDisplayMode
        item.largeTitleDisplayMode = .never
    }

    func willDismissSearchController(_ c: UISearchController) {
        if let m = titleMode { titled?.navigationItem.largeTitleDisplayMode = m }
        titleMode = nil
    }

    init(id: UInt32, runtime: AsukaRuntime) {
        self.id = id
        self.runtime = runtime
        super.init()
        controller.obscuresBackgroundDuringPresentation = false
        controller.searchResultsUpdater = self
        controller.searchBar.delegate = self
        controller.delegate = self
    }

    func setText(_ s: String) {
        setting = true
        controller.searchBar.text = s
        setting = false
    }

    func updateSearchResults(for c: UISearchController) {
        guard !setting else { return }
        let id = self.id, s = c.searchBar.text ?? ""
        runtime?.emit(id, .textChanged) { AsukaEvents.textChanged(id, s) }
    }

    func searchBarSearchButtonClicked(_ bar: UISearchBar) {
        let id = self.id
        runtime?.emit(id, .submit) { AsukaEvents.submit(id) }
    }
}
@MainActor extension AsukaRuntime {
    /// The view with Style.Match key in n's subtree, if it's on screen.
    static func matched(_ key: String, in n: AsukaNode) -> AsukaNode? {
        if n.match == key, n.view.window != nil { return n }
        for c in n.children { if let m = matched(key, in: c) { return m } }
        return nil
    }
}
#endif
