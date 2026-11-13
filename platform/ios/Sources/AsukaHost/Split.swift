#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A SplitView: UIKit's split view with a sidebar, its secondary column
/// the detail Stack. On an iPad it's a sidebar beside the content; on an
/// iPhone it collapses into a stack, starting at the sidebar's list.
final class AsukaSplitController: UISplitViewController, UISplitViewControllerDelegate {
    let nodeID: UInt32
    let sidebar: AsukaSidebarController

    init(nodeID: UInt32) {
        self.nodeID = nodeID
        sidebar = AsukaSidebarController(nodeID: nodeID)
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
final class AsukaSidebarController: UICollectionViewController {
    typealias Section = AsukaSidebarSection

    let nodeID: UInt32
    private(set) var sections: [Section] = []
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

    /// Shows new sections: with as many sections and items as before (a
    /// badge counting, a title changing), the rows that changed update in
    /// place, keeping the list's scroll, selection and VoiceOver's focus;
    /// otherwise the list is made again.
    func setSections(_ new: [Section]) {
        let old = sections
        sections = new
        guard isViewLoaded else { return }
        guard AsukaSidebar.sameShape(old, new), collectionView.numberOfSections == new.count else {
            collectionView.reloadData()
            showSelection()
            return
        }
        var changed: [IndexPath] = []
        for (s, sec) in new.enumerated() {
            for (i, it) in sec.items.enumerated() where it != old[s].items[i] { changed.append(IndexPath(item: i, section: s)) }
            if sec.title != old[s].title {
                if sec.title.isEmpty != old[s].title.isEmpty {
                    // A header comes or goes: the layout changes.
                    restyle()
                    return
                }
                let ip = IndexPath(item: 0, section: s)
                if let h = collectionView.supplementaryView(forElementKind: UICollectionView.elementKindSectionHeader, at: ip) as? UICollectionViewListCell {
                    configureHeader(h, s)
                }
            }
        }
        if !changed.isEmpty { collectionView.reconfigureItems(at: changed) }
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
        // The badge is a label at the trailing end, in the secondary
        // color, as the system's sidebars show counts; VoiceOver reads it
        // after the title.
        var accessories: [UICellAccessory] = []
        if !item.badge.isEmpty {
            accessories.append(.label(text: item.badge))
        }
        if collapsed { accessories.append(.disclosureIndicator()) }
        cell.accessories = accessories
        return cell
    }

    override func collectionView(_ cv: UICollectionView, viewForSupplementaryElementOfKind kind: String, at ip: IndexPath) -> UICollectionReusableView {
        let h = cv.dequeueReusableSupplementaryView(ofKind: kind, withReuseIdentifier: "header", for: ip) as! UICollectionViewListCell
        configureHeader(h, ip.section)
        return h
    }

    private func configureHeader(_ h: UICollectionViewListCell, _ section: Int) {
        var c = UIListContentConfiguration.sidebarHeader()
        if collapsed { c = .groupedHeader() }
        c.text = sections[section].title
        h.contentConfiguration = c
    }

    override func collectionView(_ cv: UICollectionView, didSelectItemAt ip: IndexPath) {
        let i = flat(ip)
        choseOnce = true
        selected = i
        let id = nodeID
        AsukaRuntime.shared.emit(id, .selected) { AsukaEvents.selected(id, Int64(i)) }
        splitViewController?.show(.secondary)
    }
}

extension Array {
    subscript(safe i: Int) -> Element? { indices.contains(i) ? self[i] : nil }
}

@MainActor extension AsukaRuntime {
    func setSplitProp(_ n: AsukaNode, _ prop: UInt16, _ v: AsukaValue) {
        guard let s = n.vc as? AsukaSplitController else { return }
        switch prop {
        case AsukaProp.Split.items:
            guard case .bytes(let b) = v else { return }
            s.sidebar.setSections(AsukaSidebar.decode(b))
        case AsukaProp.Split.selected:
            s.sidebar.selected = Int(v.int)
        case AsukaProp.Split.title:
            s.sidebar.title = v.string
            s.sidebar.navigationItem.title = v.string
        default:
            break
        }
    }
}
#endif
