#if canImport(UIKit)
import UIKit
#if canImport(NuiOps)
import NuiOps
#endif

/// A screen of a Stack, a page of Tabs or a sheet's content: its view is
/// sized by UIKit, and it reports its environment (size, safe area,
/// keyboard) to the core, which lays it out.
class NuiScreenController: UIViewController {
    let nodeID: UInt32
    let content = NuiContainerView()
    var lastEnv: NuiEnvironment?
    var backGesture = true

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(nibName: nil, bundle: nil)
    }
    required init?(coder: NSCoder) { fatalError() }

    override func loadView() { view = content }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        report()
    }

    override func viewSafeAreaInsetsDidChange() {
        super.viewSafeAreaInsetsDidChange()
        report()
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        NuiRuntime.shared.adoptControllers(in: self)
    }

    func report(force: Bool = false) {
        let rt = NuiRuntime.shared
        guard view.bounds.width > 0, rt.nodes[nodeID] != nil else { return }
        let e = rt.environment(of: view)
        if !force && e == lastEnv { return }
        lastEnv = e
        rt.send(NuiEvents.environment(nodeID, e))
    }
}

final class NuiSheetController: NuiScreenController, UIAdaptivePresentationControllerDelegate {
    var detents: Int64 = 0
    var grabber = false

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        guard let n = NuiRuntime.shared.nodes[nodeID], !n.dying else { return }
        NuiRuntime.shared.emit(nodeID, .dismissed) { NuiEvents.dismissed(nodeID) }
    }
}

/// A Stack: screens are pushed by Insert and popped by Destroy; pops the
/// user makes (back button, swipe) are reported as Popped.
final class NuiStackController: UINavigationController, UINavigationControllerDelegate, UIGestureRecognizerDelegate {
    let nodeID: UInt32

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(nibName: nil, bundle: nil)
    }
    required init?(coder: NSCoder) { fatalError() }

    override func viewDidLoad() {
        super.viewDidLoad()
        delegate = self
        interactivePopGestureRecognizer?.delegate = self
    }

    func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
        guard viewControllers.count > 1 else { return false }
        return (topViewController as? NuiScreenController)?.backGesture ?? true
    }

    func navigationController(_ nav: UINavigationController, didShow vc: UIViewController, animated: Bool) {
        NuiRuntime.shared.stackDidShow(self)
    }
}

final class NuiTabsController: UITabBarController, UITabBarControllerDelegate {
    let nodeID: UInt32

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(nibName: nil, bundle: nil)
        delegate = self
    }
    required init?(coder: NSCoder) { fatalError() }

    func tabBarController(_ t: UITabBarController, didSelect vc: UIViewController) {
        let i = Int64(selectedIndex)
        NuiRuntime.shared.emit(nodeID, .selected) { NuiEvents.selected(nodeID, i) }
    }
}

/// Shows a container's menu on a long press.
final class NuiContextMenu: NSObject, UIContextMenuInteractionDelegate {
    weak var node: NuiNode?
    init(node: NuiNode) { self.node = node }

    func contextMenuInteraction(_ i: UIContextMenuInteraction, configurationForMenuAtLocation p: CGPoint) -> UIContextMenuConfiguration? {
        guard let node else { return nil }
        let menu = NuiRuntime.shared.menu(for: node)
        return UIContextMenuConfiguration(identifier: nil, previewProvider: nil) { _ in menu }
    }
}

/// An action of a menu, an alert or a navigation bar.
struct NuiAction {
    var title: String
    var role: Int32
}

func decodeActions(_ b: UnsafeRawBufferPointer) -> [NuiAction] {
    var r = NuiReader(b)
    guard b.count >= 4 else { return [] }
    let n = Int(r.u32())
    var out: [NuiAction] = []
    for _ in 0..<n {
        let role = Int32(r.u8())
        out.append(NuiAction(title: r.string(), role: role))
    }
    return out
}

@MainActor extension NuiRuntime {
    var topController: UIViewController? {
        var vc: UIViewController? = controller
        while let p = vc?.presentedViewController, !p.isBeingDismissed { vc = p }
        return vc
    }

    /// The environment of a view: its size, safe area and the keyboard's
    /// overlap with it.
    func environment(of view: UIView) -> NuiEnvironment {
        var e = NuiEnvironment()
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
        e.locale = Locale.current.identifier
        return e
    }

    /// Reports every screen's environment again (the keyboard moved).
    func reportScreens() {
        for n in nodes.values {
            (n.vc as? NuiScreenController)?.report()
        }
    }

    /// The controller whose view holds node's view: the nearest screen or
    /// sheet, else the root.
    func hostController(of node: NuiNode) -> UIViewController? {
        var p = node.parent
        while let n = p {
            if let vc = n.vc as? NuiScreenController { return vc }
            p = n.parent
        }
        return controller
    }

    /// Makes the stacks and tabs under vc's view its children (they may
    /// have been attached before their screen existed).
    func adoptControllers(in vc: UIViewController) {
        for n in nodes.values {
            guard let child = n.vc, child is NuiStackController || child is NuiTabsController,
                  child.parent !== vc, child.view.isDescendant(of: vc.view) else { continue }
            if let owner = hostController(of: n), owner !== vc, child.parent === owner { continue }
            child.willMove(toParent: nil)
            child.removeFromParent()
            vc.addChild(child)
            child.didMove(toParent: vc)
        }
    }

    func makeController(_ id: UInt32, _ kind: NuiKind) -> (UIView, UIViewController?)? {
        switch kind {
        case .stack:
            let s = NuiStackController(nodeID: id)
            return (s.view, s)
        case .screen:
            let s = NuiScreenController(nodeID: id)
            return (s.content, s)
        case .tabs:
            let t = NuiTabsController(nodeID: id)
            return (t.view, t)
        case .sheet:
            let s = NuiSheetController(nodeID: id)
            return (s.content, s)
        case .alert:
            return (UIView(), nil)
        default:
            return nil
        }
    }

    /// Handles inserting into a Stack or Tabs; false: an ordinary insert.
    func insertController(_ p: NuiNode, _ c: NuiNode, _ index: Int) -> Bool {
        if let nav = p.vc as? NuiStackController, let screen = c.vc {
            let i = min(index, p.children.count)
            p.children.insert(c, at: i)
            c.parent = p
            if i == nav.viewControllers.count {
                nav.pushViewController(screen, animated: !nav.viewControllers.isEmpty && nav.view.window != nil)
            } else {
                var vcs = nav.viewControllers
                vcs.insert(screen, at: min(i, vcs.count))
                nav.setViewControllers(vcs, animated: false)
            }
            return true
        }
        if let tabs = p.vc as? NuiTabsController {
            let i = min(index, p.children.count)
            p.children.insert(c, at: i)
            c.parent = p
            tabs.setViewControllers(p.children.compactMap { $0.vc }, animated: false)
            if let sel = p.selected { tabs.selectedIndex = sel }
            return true
        }
        if let child = c.vc, child is NuiStackController || child is NuiTabsController {
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
    func destroyController(_ n: NuiNode) -> Bool {
        if n.underDying {
            return true // its views leave with the animating screen
        }
        guard let vc = n.vc else { return false }
        n.dying = true
        if let nav = n.parent?.vc as? NuiStackController {
            n.parent?.children.removeAll { $0 === n }
            if nav.topViewController === vc {
                nav.popViewController(animated: nav.view.window != nil)
            } else if nav.viewControllers.contains(vc) {
                nav.setViewControllers(nav.viewControllers.filter { $0 !== vc }, animated: false)
            }
            return true
        }
        if let tabs = n.parent?.vc as? NuiTabsController {
            n.parent?.children.removeAll { $0 === n }
            tabs.setViewControllers(n.parent?.children.compactMap { $0.vc }, animated: false)
            return true
        }
        if vc is NuiSheetController || vc is UIAlertController {
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
    func stackDidShow(_ nav: NuiStackController) {
        guard let node = nodes[nav.nodeID] else { return }
        let shown = Set(nav.viewControllers.map { ObjectIdentifier($0) })
        for c in node.children.reversed() where !c.dying {
            guard let vc = c.vc, !shown.contains(ObjectIdentifier(vc)) else { continue }
            c.dying = true
            node.children.removeAll { $0 === c }
            let id = c.id
            emit(nav.nodeID, .popped) { NuiEvents.popped(nav.nodeID, Int64(id)) }
        }
    }

    func present(_ n: NuiNode) {
        switch n.kind {
        case .sheet:
            guard let s = n.vc as? NuiSheetController, let top = topController else { return }
            if let sheet = s.sheetPresentationController {
                var ds: [UISheetPresentationController.Detent] = []
                if s.detents & 1 != 0 { ds.append(.medium()) }
                if s.detents & 2 != 0 || ds.isEmpty { ds.append(.large()) }
                sheet.detents = ds
                sheet.prefersGrabberVisible = s.grabber
            }
            s.presentationController?.delegate = s
            top.present(s, animated: true)
        case .alert:
            guard let top = topController else { return }
            let a = UIAlertController(title: n.alertTitle, message: n.alertMessage,
                                      preferredStyle: n.alertStyle == NuiAlertStyle.actionSheet.rawValue ? .actionSheet : .alert)
            let id = n.id
            for (i, act) in n.actions.enumerated() {
                let style: UIAlertAction.Style = act.role == NuiAlertButtonRole.cancel.rawValue ? .cancel
                    : act.role == NuiAlertButtonRole.destructive.rawValue ? .destructive : .default
                a.addAction(UIAlertAction(title: act.title, style: style) { [weak self] _ in
                    self?.emit(id, .action) { NuiEvents.action(id, Int64(i)) }
                })
            }
            if let pop = a.popoverPresentationController, let v = top.view {
                pop.sourceView = v
                pop.sourceRect = CGRect(x: v.bounds.midX, y: v.bounds.midY, width: 0, height: 0)
                pop.permittedArrowDirections = []
            }
            n.vc = a
            top.present(a, animated: true)
        default:
            break
        }
    }

    func menu(for n: NuiNode) -> UIMenu {
        let id = n.id
        let items: [UIMenuElement] = n.actions.enumerated().map { i, act in
            UIAction(title: act.title, attributes: act.role == NuiAlertButtonRole.destructive.rawValue ? .destructive : []) { [weak self] _ in
                self?.emit(id, .action) { NuiEvents.action(id, Int64(i)) }
            }
        }
        return UIMenu(children: items)
    }

    /// Installs a node's menu: on a button, as its primary action; on a
    /// view, a tap (an invisible button over it) or a long press (a
    /// context menu).
    func installMenu(_ n: NuiNode) {
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
        if n.menuTrigger == NuiMenuTrigger.longPress.rawValue {
            let d = NuiContextMenu(node: n)
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

    func setNavProp(_ n: NuiNode, _ prop: UInt16, _ v: NuiValue) {
        switch (n.kind, prop) {
        case (.screen, NuiProp.Screen.title):
            n.vc?.navigationItem.title = v.string
            n.vc?.title = v.string
        case (.screen, NuiProp.Screen.icon):
            n.vc?.tabBarItem.image = UIImage(systemName: v.string)
        case (.screen, NuiProp.Screen.backGesture):
            (n.vc as? NuiScreenController)?.backGesture = v.bool
        case (.screen, NuiProp.Screen.actions):
            guard case .bytes(let b) = v, let vc = n.vc else { return }
            let id = n.id
            vc.navigationItem.rightBarButtonItems = decodeActions(b).enumerated().map { i, act in
                let item = UIBarButtonItem(title: act.title, primaryAction: UIAction(title: act.title) { [weak self] _ in
                    self?.emit(id, .action) { NuiEvents.action(id, Int64(i)) }
                })
                if act.role == NuiAlertButtonRole.destructive.rawValue { item.tintColor = .systemRed }
                return item
            }
        case (.tabs, NuiProp.Tabs.selected):
            n.selected = Int(v.int)
            (n.vc as? NuiTabsController)?.selectedIndex = Int(v.int)
        case (.sheet, NuiProp.Sheet.detents):
            (n.vc as? NuiSheetController)?.detents = v.int
        case (.sheet, NuiProp.Sheet.grabber):
            (n.vc as? NuiSheetController)?.grabber = v.bool
        case (.alert, NuiProp.Alert.title):
            n.alertTitle = v.string
        case (.alert, NuiProp.Alert.message):
            n.alertMessage = v.string
        case (.alert, NuiProp.Alert.buttons):
            if case .bytes(let b) = v { n.actions = decodeActions(b) }
        case (.alert, NuiProp.Alert.style):
            n.alertStyle = v.enumValue
        case (.scroll, NuiProp.Scroll.refreshable), (.list, NuiProp.List.refreshable):
            guard let s = n.view as? UIScrollView else { return }
            if v.bool {
                let rc = UIRefreshControl()
                let id = n.id
                rc.addAction(UIAction { [weak self] _ in self?.emit(id, .refresh) { NuiEvents.refresh(id) } }, for: .valueChanged)
                s.refreshControl = rc
            } else {
                s.refreshControl = nil
            }
        case (.scroll, NuiProp.Scroll.refreshing), (.list, NuiProp.List.refreshing):
            guard let rc = (n.view as? UIScrollView)?.refreshControl else { return }
            if v.bool { rc.beginRefreshing() } else { rc.endRefreshing() }
        default:
            break
        }
    }

    func haptic(_ kind: Int32) {
        switch NuiHaptic(rawValue: kind) {
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
#endif
