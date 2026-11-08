#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A SplitView: UIKit's split view with a sidebar, its secondary column
/// the detail Stack. On an iPad it's a sidebar beside the content; on an
/// iPhone it collapses into a stack, starting at the sidebar's list.
final class RyUISplitController: UISplitViewController, UISplitViewControllerDelegate {
    let nodeID: UInt32
    let sidebar: RyUISidebarController

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        sidebar = RyUISidebarController(nodeID: nodeID)
        super.init(style: .doubleColumn)
        delegate = self
        preferredDisplayMode = .oneBesideSecondary
        preferredSplitBehavior = .tile
        setViewController(sidebar, for: .primary)
    }
    required init?(coder: NSCoder) { fatalError() }

    /// An iPhone starts at the list, as NavigationSplitView does.
    func splitViewController(_ svc: UISplitViewController, topColumnForCollapsingToProposedTopColumn proposed: UISplitViewController.Column) -> UISplitViewController.Column {
        sidebar.choseOnce ? proposed : .primary
    }

    func splitViewControllerDidCollapse(_ svc: UISplitViewController) { sidebar.restyle() }
    func splitViewControllerDidExpand(_ svc: UISplitViewController) { sidebar.restyle() }
}

/// A SplitView's sidebar: its sections and destinations in a list, the
/// sidebar's look beside the content, an inset grouped list with
/// disclosure indicators when collapsed (an iPhone's first screen).
final class RyUISidebarController: UICollectionViewController {
    struct Item { let title: String; let icon: String }
    struct Section { let title: String; let items: [Item] }

    let nodeID: UInt32
    var sections: [Section] = [] { didSet { collectionView.reloadData(); showSelection() } }
    var selected = -1 { didSet { showSelection() } }
    var choseOnce = false

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        super.init(collectionViewLayout: UICollectionViewFlowLayout())
    }
    required init?(coder: NSCoder) { fatalError() }

    private var collapsed: Bool { splitViewController?.isCollapsed ?? (traitCollection.horizontalSizeClass == .compact) }

    override func viewDidLoad() {
        super.viewDidLoad()
        navigationItem.largeTitleDisplayMode = .always
        navigationController?.navigationBar.prefersLargeTitles = true
        collectionView.register(UICollectionViewListCell.self, forCellWithReuseIdentifier: "item")
        collectionView.register(UICollectionViewListCell.self, forSupplementaryViewOfKind: UICollectionView.elementKindSectionHeader, withReuseIdentifier: "header")
        restyle()
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        navigationController?.navigationBar.prefersLargeTitles = true
        // Back at the list on an iPhone: nothing stays highlighted.
        if collapsed, let ip = collectionView.indexPathsForSelectedItems?.first {
            collectionView.deselectItem(at: ip, animated: animated)
        }
    }

    func restyle() {
        guard isViewLoaded else { return }
        let compact = collapsed
        collectionView.collectionViewLayout = UICollectionViewCompositionalLayout { [weak self] i, env in
            var c = UICollectionLayoutListConfiguration(appearance: compact ? .insetGrouped : .sidebar)
            let titled = !(self?.sections[safe: i]?.title.isEmpty ?? true)
            c.headerMode = titled ? .supplementary : .none
            return NSCollectionLayoutSection.list(using: c, layoutEnvironment: env)
        }
        collectionView.reloadData()
        showSelection()
    }

    private func flat(_ ip: IndexPath) -> Int { sections.prefix(ip.section).reduce(0) { $0 + $1.items.count } + ip.item }

    private func indexPath(_ i: Int) -> IndexPath? {
        var i = i
        for (s, sec) in sections.enumerated() {
            if i < sec.items.count { return IndexPath(item: i, section: s) }
            i -= sec.items.count
        }
        return nil
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        showSelection()
    }

    /// Beside the content, the chosen destination is highlighted (once the
    /// list has its cells: selecting before it's laid out is lost).
    private func showSelection() {
        guard isViewLoaded else { return }
        DispatchQueue.main.async { [weak self] in
            guard let self, !self.collapsed, let ip = self.indexPath(self.selected), self.collectionView.numberOfSections > ip.section else { return }
            self.collectionView.selectItem(at: ip, animated: false, scrollPosition: [])
        }
    }

    override func numberOfSections(in cv: UICollectionView) -> Int { sections.count }
    override func collectionView(_ cv: UICollectionView, numberOfItemsInSection s: Int) -> Int { sections[s].items.count }

    override func collectionView(_ cv: UICollectionView, cellForItemAt ip: IndexPath) -> UICollectionViewCell {
        let cell = cv.dequeueReusableCell(withReuseIdentifier: "item", for: ip) as! UICollectionViewListCell
        let item = sections[ip.section].items[ip.item]
        var c = cell.defaultContentConfiguration()
        c.text = item.title
        c.image = item.icon.isEmpty ? nil : UIImage(systemName: item.icon)
        cell.contentConfiguration = c
        cell.accessories = collapsed ? [.disclosureIndicator()] : []
        return cell
    }

    override func collectionView(_ cv: UICollectionView, viewForSupplementaryElementOfKind kind: String, at ip: IndexPath) -> UICollectionReusableView {
        let h = cv.dequeueReusableSupplementaryView(ofKind: kind, withReuseIdentifier: "header", for: ip) as! UICollectionViewListCell
        var c = UIListContentConfiguration.sidebarHeader()
        if collapsed { c = .groupedHeader() }
        c.text = sections[ip.section].title
        h.contentConfiguration = c
        return h
    }

    override func collectionView(_ cv: UICollectionView, didSelectItemAt ip: IndexPath) {
        let i = flat(ip)
        choseOnce = true
        selected = i
        let id = nodeID
        RyUIRuntime.shared.emit(id, .selected) { RyUIEvents.selected(id, Int64(i)) }
        splitViewController?.show(.secondary)
    }
}

extension Array {
    subscript(safe i: Int) -> Element? { indices.contains(i) ? self[i] : nil }
}

@MainActor extension RyUIRuntime {
    func setSplitProp(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) {
        guard let s = n.vc as? RyUISplitController else { return }
        switch prop {
        case RyUIProp.Split.items:
            guard case .bytes(let b) = v, b.count >= 4 else { return }
            var r = RyUIReader(b)
            var sections: [RyUISidebarController.Section] = []
            for _ in 0..<Int(r.u32()) {
                let title = r.string()
                var items: [RyUISidebarController.Item] = []
                for _ in 0..<Int(r.u32()) { items.append(.init(title: r.string(), icon: r.string())) }
                sections.append(.init(title: title, items: items))
            }
            s.sidebar.sections = sections
        case RyUIProp.Split.selected:
            s.sidebar.selected = Int(v.int)
        case RyUIProp.Split.title:
            s.sidebar.title = v.string
            s.sidebar.navigationItem.title = v.string
        default:
            break
        }
    }
}
#endif
