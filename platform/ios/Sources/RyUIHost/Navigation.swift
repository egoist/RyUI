#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A screen of a Stack, a page of Tabs or a sheet's content: its view is
/// sized by UIKit, and it reports its environment (size, safe area,
/// keyboard) to the core, which lays it out.
class RyUIScreenController: UIViewController {
    let nodeID: UInt32
    let content = RyUIContainerView()
    var lastEnv: RyUIEnvironment?
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
        RyUIRuntime.shared.adoptControllers(in: self)
    }

    func report(force: Bool = false) {
        let rt = RyUIRuntime.shared
        guard view.bounds.width > 0, rt.nodes[nodeID] != nil else { return }
        let e = rt.environment(of: view)
        if !force && e == lastEnv { return }
        lastEnv = e
        rt.send(RyUIEvents.environment(nodeID, e))
    }
}

final class RyUISheetController: RyUIScreenController, UIAdaptivePresentationControllerDelegate {
    var detents: Int64 = 0
    var grabber = false

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        guard let n = RyUIRuntime.shared.nodes[nodeID], !n.dying else { return }
        RyUIRuntime.shared.emit(nodeID, .dismissed) { RyUIEvents.dismissed(nodeID) }
    }
}

/// A Stack: screens are pushed by Insert and popped by Destroy; pops the
/// user makes (back button, swipe) are reported as Popped.
final class RyUIStackController: UINavigationController, UINavigationControllerDelegate, UIGestureRecognizerDelegate {
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
        return (topViewController as? RyUIScreenController)?.backGesture ?? true
    }

    func navigationController(_ nav: UINavigationController, didShow vc: UIViewController, animated: Bool) {
        RyUIRuntime.shared.stackDidShow(self)
    }
}

final class RyUITabsController: UITabBarController, UITabBarControllerDelegate {
    let nodeID: UInt32

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(nibName: nil, bundle: nil)
        delegate = self
    }
    required init?(coder: NSCoder) { fatalError() }

    func tabBarController(_ t: UITabBarController, didSelect vc: UIViewController) {
        let i = Int64(selectedIndex)
        RyUIRuntime.shared.emit(nodeID, .selected) { RyUIEvents.selected(nodeID, i) }
    }
}

/// Shows a container's menu on a long press.
final class RyUIContextMenu: NSObject, UIContextMenuInteractionDelegate {
    weak var node: RyUINode?
    init(node: RyUINode) { self.node = node }

    func contextMenuInteraction(_ i: UIContextMenuInteraction, configurationForMenuAtLocation p: CGPoint) -> UIContextMenuConfiguration? {
        guard let node else { return nil }
        let menu = RyUIRuntime.shared.menu(for: node)
        return UIContextMenuConfiguration(identifier: nil, previewProvider: nil) { _ in menu }
    }
}

/// An action of a menu, an alert or a navigation bar.
struct RyUIAction {
    var title: String
    var role: Int32
    var icon = ""
    /// The icon's symbol, if it has one.
    var image: UIImage? { icon.isEmpty ? nil : UIImage(systemName: icon) }
}

func decodeActions(_ b: UnsafeRawBufferPointer) -> [RyUIAction] {
    var r = RyUIReader(b)
    guard b.count >= 4 else { return [] }
    let n = Int(r.u32())
    var out: [RyUIAction] = []
    for _ in 0..<n {
        let role = Int32(r.u8())
        let title = r.string()
        out.append(RyUIAction(title: title, role: role, icon: r.string()))
    }
    return out
}

@MainActor extension RyUIRuntime {
    var topController: UIViewController? {
        var vc: UIViewController? = controller
        while let p = vc?.presentedViewController, !p.isBeingDismissed { vc = p }
        return vc
    }

    /// The environment of a view: its size, safe area and the keyboard's
    /// overlap with it.
    func environment(of view: UIView) -> RyUIEnvironment {
        var e = RyUIEnvironment()
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
        e.widthClass = t.horizontalSizeClass.ryui
        e.heightClass = t.verticalSizeClass.ryui
        e.locale = Locale.current.identifier
        return e
    }

    /// Reports every screen's environment again (the keyboard moved).
    func reportScreens() {
        for n in nodes.values {
            (n.vc as? RyUIScreenController)?.report()
        }
    }

    /// The controller whose view holds node's view: the nearest screen or
    /// sheet, else the root.
    func hostController(of node: RyUINode) -> UIViewController? {
        var p = node.parent
        while let n = p {
            if let vc = n.vc as? RyUIScreenController { return vc }
            p = n.parent
        }
        return controller
    }

    /// Makes the stacks and tabs under vc's view its children (they may
    /// have been attached before their screen existed).
    func adoptControllers(in vc: UIViewController) {
        for n in nodes.values {
            guard let child = n.vc, child is RyUIStackController || child is RyUITabsController,
                  child.parent !== vc, child.view.isDescendant(of: vc.view) else { continue }
            if let owner = hostController(of: n), owner !== vc, child.parent === owner { continue }
            child.willMove(toParent: nil)
            child.removeFromParent()
            vc.addChild(child)
            child.didMove(toParent: vc)
        }
    }

    func makeController(_ id: UInt32, _ kind: RyUIKind) -> (UIView, UIViewController?)? {
        switch kind {
        case .stack:
            let s = RyUIStackController(nodeID: id)
            return (s.view, s)
        case .screen:
            let s = RyUIScreenController(nodeID: id)
            return (s.content, s)
        case .tabs:
            let t = RyUITabsController(nodeID: id)
            return (t.view, t)
        case .sheet:
            let s = RyUISheetController(nodeID: id)
            return (s.content, s)
        case .alert:
            return (UIView(), nil)
        default:
            return nil
        }
    }

    /// Handles inserting into a Stack or Tabs; false: an ordinary insert.
    func insertController(_ p: RyUINode, _ c: RyUINode, _ index: Int) -> Bool {
        if let nav = p.vc as? RyUIStackController, let screen = c.vc {
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
                        if let m = below.flatMap({ RyUIRuntime.matched(key, in: $0) }) { return m.view }
                        guard let m = self?.matchViews[key], m.view.window != nil, let s = screen?.view, !m.view.isDescendant(of: s) else { return nil }
                        return m.view
                    }
                }
                nav.pushViewController(screen, animated: !nav.viewControllers.isEmpty && nav.view.window != nil)
            } else {
                var vcs = nav.viewControllers
                vcs.insert(screen, at: min(i, vcs.count))
                nav.setViewControllers(vcs, animated: false)
            }
            return true
        }
        if let tabs = p.vc as? RyUITabsController {
            let i = min(index, p.children.count)
            p.children.insert(c, at: i)
            c.parent = p
            tabs.setViewControllers(p.children.compactMap { $0.vc }, animated: false)
            if let sel = p.selected { tabs.selectedIndex = sel }
            return true
        }
        if let child = c.vc, child is RyUIStackController || child is RyUITabsController {
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
    func destroyController(_ n: RyUINode) -> Bool {
        if n.underDying {
            return true // its views leave with the animating screen
        }
        guard let vc = n.vc else { return false }
        n.dying = true
        if let nav = n.parent?.vc as? RyUIStackController {
            n.parent?.children.removeAll { $0 === n }
            if nav.topViewController === vc {
                nav.popViewController(animated: nav.view.window != nil)
            } else if nav.viewControllers.contains(vc) {
                nav.setViewControllers(nav.viewControllers.filter { $0 !== vc }, animated: false)
            }
            return true
        }
        if let tabs = n.parent?.vc as? RyUITabsController {
            n.parent?.children.removeAll { $0 === n }
            tabs.setViewControllers(n.parent?.children.compactMap { $0.vc }, animated: false)
            return true
        }
        if vc is RyUISheetController || vc is UIAlertController {
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
    func stackDidShow(_ nav: RyUIStackController) {
        guard let node = nodes[nav.nodeID] else { return }
        let shown = Set(nav.viewControllers.map { ObjectIdentifier($0) })
        for c in node.children.reversed() where !c.dying {
            guard let vc = c.vc, !shown.contains(ObjectIdentifier(vc)) else { continue }
            c.dying = true
            node.children.removeAll { $0 === c }
            let id = c.id
            emit(nav.nodeID, .popped) { RyUIEvents.popped(nav.nodeID, Int64(id)) }
        }
    }

    func present(_ n: RyUINode) {
        switch n.kind {
        case .sheet:
            guard let s = n.vc as? RyUISheetController, let top = topController else { return }
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
                                      preferredStyle: n.alertStyle == RyUIAlertStyle.actionSheet.rawValue ? .actionSheet : .alert)
            let id = n.id
            for (i, act) in n.actions.enumerated() {
                let style: UIAlertAction.Style = act.role == RyUIAlertButtonRole.cancel.rawValue ? .cancel
                    : act.role == RyUIAlertButtonRole.destructive.rawValue ? .destructive : .default
                a.addAction(UIAlertAction(title: act.title, style: style) { [weak self] _ in
                    self?.emit(id, .action) { RyUIEvents.action(id, Int64(i)) }
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

    func menu(for n: RyUINode) -> UIMenu {
        let id = n.id
        let items: [UIMenuElement] = n.actions.enumerated().map { i, act in
            UIAction(title: act.title, image: act.image, attributes: act.role == RyUIAlertButtonRole.destructive.rawValue ? .destructive : []) { [weak self] _ in
                self?.emit(id, .action) { RyUIEvents.action(id, Int64(i)) }
            }
        }
        return UIMenu(children: items)
    }

    /// Installs a node's menu: on a button, as its primary action; on a
    /// view, a tap (an invisible button over it) or a long press (a
    /// context menu).
    func installMenu(_ n: RyUINode) {
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
        if n.menuTrigger == RyUIMenuTrigger.longPress.rawValue {
            let d = RyUIContextMenu(node: n)
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

    func setNavProp(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) {
        switch (n.kind, prop) {
        case (.screen, RyUIProp.Screen.title):
            n.vc?.navigationItem.title = v.string
            n.vc?.title = v.string
        case (.screen, RyUIProp.Screen.icon):
            n.vc?.tabBarItem.image = UIImage(systemName: v.string)
        case (.screen, RyUIProp.Screen.search):
            guard let vc = n.vc else { return }
            let s = n.search ?? RyUISearch(id: n.id, runtime: self)
            n.search = s
            s.controller.searchBar.placeholder = v.string
            vc.navigationItem.searchController = s.controller
            vc.navigationItem.hidesSearchBarWhenScrolling = false
            vc.definesPresentationContext = true
        case (.screen, RyUIProp.Screen.searchText):
            n.search?.setText(v.string)
        case (.screen, RyUIProp.Screen.zoomFrom):
            n.zoomFrom = v.string.isEmpty ? nil : v.string
        case (.screen, RyUIProp.Screen.backGesture):
            (n.vc as? RyUIScreenController)?.backGesture = v.bool
        case (.screen, RyUIProp.Screen.actions):
            guard case .bytes(let b) = v, let vc = n.vc else { return }
            let id = n.id
            vc.navigationItem.rightBarButtonItems = decodeActions(b).enumerated().map { i, act in
                // An icon stands for the title, which VoiceOver reads.
                let item = UIBarButtonItem(title: act.image == nil ? act.title : nil, image: act.image, primaryAction: UIAction(title: act.title) { [weak self] _ in
                    self?.emit(id, .action) { RyUIEvents.action(id, Int64(i)) }
                })
                item.accessibilityLabel = act.title
                if act.role == RyUIAlertButtonRole.destructive.rawValue { item.tintColor = .systemRed }
                return item
            }
        case (.tabs, RyUIProp.Tabs.selected):
            n.selected = Int(v.int)
            (n.vc as? RyUITabsController)?.selectedIndex = Int(v.int)
        case (.sheet, RyUIProp.Sheet.detents):
            (n.vc as? RyUISheetController)?.detents = v.int
        case (.sheet, RyUIProp.Sheet.grabber):
            (n.vc as? RyUISheetController)?.grabber = v.bool
        case (.alert, RyUIProp.Alert.title):
            n.alertTitle = v.string
        case (.alert, RyUIProp.Alert.message):
            n.alertMessage = v.string
        case (.alert, RyUIProp.Alert.buttons):
            if case .bytes(let b) = v { n.actions = decodeActions(b) }
        case (.alert, RyUIProp.Alert.style):
            n.alertStyle = v.enumValue
        case (.scroll, RyUIProp.Scroll.refreshable), (.list, RyUIProp.List.refreshable):
            guard let s = n.view as? UIScrollView else { return }
            if v.bool {
                let rc = UIRefreshControl()
                let id = n.id
                rc.addAction(UIAction { [weak self] _ in self?.emit(id, .refresh) { RyUIEvents.refresh(id) } }, for: .valueChanged)
                s.refreshControl = rc
            } else {
                s.refreshControl = nil
            }
        case (.scroll, RyUIProp.Scroll.refreshing), (.list, RyUIProp.List.refreshing):
            guard let rc = (n.view as? UIScrollView)?.refreshControl else { return }
            if v.bool { rc.beginRefreshing() } else { rc.endRefreshing() }
        case (.list, RyUIProp.List.columns):
            n.list?.setGrid(columns: Int(v.int), gap: n.list?.gap ?? 0)
        case (.list, RyUIProp.List.gap):
            n.list?.setGrid(columns: n.list?.columns ?? 1, gap: CGFloat(v.float))
        case (.list, RyUIProp.List.appearance):
            n.list?.setAppearance(RyUIListAppearance(rawValue: v.enumValue) ?? .plain, inset: n.list?.inset ?? 0)
        case (.list, RyUIProp.List.inset):
            n.list?.setAppearance(n.list?.appearance ?? .plain, inset: CGFloat(v.float))
        default:
            break
        }
    }

    func haptic(_ kind: Int32) {
        switch RyUIHaptic(rawValue: kind) {
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
@MainActor final class RyUISearch: NSObject, UISearchResultsUpdating, UISearchBarDelegate {
    let controller = UISearchController(searchResultsController: nil)
    let id: UInt32
    weak var runtime: RyUIRuntime?
    private var setting = false

    init(id: UInt32, runtime: RyUIRuntime) {
        self.id = id
        self.runtime = runtime
        super.init()
        controller.obscuresBackgroundDuringPresentation = false
        controller.searchResultsUpdater = self
        controller.searchBar.delegate = self
    }

    func setText(_ s: String) {
        setting = true
        controller.searchBar.text = s
        setting = false
    }

    func updateSearchResults(for c: UISearchController) {
        guard !setting else { return }
        let id = self.id, s = c.searchBar.text ?? ""
        runtime?.emit(id, .textChanged) { RyUIEvents.textChanged(id, s) }
    }

    func searchBarSearchButtonClicked(_ bar: UISearchBar) {
        let id = self.id
        runtime?.emit(id, .submit) { RyUIEvents.submit(id) }
    }
}
@MainActor extension RyUIRuntime {
    /// The view with Style.Match key in n's subtree, if it's on screen.
    static func matched(_ key: String, in n: RyUINode) -> RyUINode? {
        if n.match == key, n.view.window != nil { return n }
        for c in n.children { if let m = matched(key, in: c) { return m } }
        return nil
    }
}
#endif
