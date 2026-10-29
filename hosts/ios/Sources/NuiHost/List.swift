#if canImport(UIKit)
import UIKit
#if canImport(CNui)
import CNui
#endif

/// A list cell: its content view holds a row the core lays out; the
/// row's height comes from the core.
final class NuiListCell: UICollectionViewListCell {
    var nuiID: UInt32 = 0
    var rowHeight: CGFloat = 44
    weak var list: NuiList?

    func setRowHeight(_ h: CGFloat) {
        guard h != rowHeight else { return }
        rowHeight = h
        invalidateIntrinsicContentSize()
        list?.heightsChanged()
    }

    override func preferredLayoutAttributesFitting(_ attrs: UICollectionViewLayoutAttributes) -> UICollectionViewLayoutAttributes {
        let a = super.preferredLayoutAttributesFitting(attrs)
        a.size.height = rowHeight
        return a
    }
}

/// Lets the list's layout configuration reach the list made after it.
final class NuiWeakList {
    weak var list: NuiList?
}

/// A List: a UICollectionView with a list layout, fed by a diffable data
/// source from the core's item IDs. Cells bind their rows through
/// nui_list_bind when they're about to show.
@MainActor final class NuiList: NSObject {
    let id: UInt32
    let collection: UICollectionView
    var source: UICollectionViewDiffableDataSource<Int, Int64>!
    var pending: [Int64]?
    var applied = false
    var binding = false
    var columns = 1
    var gap: CGFloat = 0
    private var listLayout: UICollectionViewLayout!

    init(id: UInt32, runtime: NuiRuntime) {
        self.id = id
        var config = UICollectionLayoutListConfiguration(appearance: .plain)
        config.showsSeparators = true
        // A row's swipe actions are its cell's (the core sends them as it binds it).
        let box = NuiWeakList()
        config.trailingSwipeActionsConfigurationProvider = { [weak runtime] path in
            guard let runtime, let list = box.list,
                  let cell = list.collection.cellForItem(at: path) as? NuiListCell,
                  let n = runtime.nodes[cell.nuiID], !n.swipeActions.isEmpty else { return nil }
            let id = cell.nuiID
            let actions = n.swipeActions.enumerated().map { i, act in
                let a = UIContextualAction(style: act.role == NuiAlertButtonRole.destructive.rawValue ? .destructive : .normal,
                                           title: act.title) { [weak runtime] _, _, done in
                    runtime?.emit(id, .action) { NuiEvents.action(id, Int64(i)) }
                    done(true)
                }
                a.image = act.image
                return a
            }
            return UISwipeActionsConfiguration(actions: actions)
        }
        let layout = UICollectionViewCompositionalLayout.list(using: config)
        collection = UICollectionView(frame: .zero, collectionViewLayout: layout)
        collection.keyboardDismissMode = .interactive
        // Rows handle their own presses; a cell selection would stick.
        collection.allowsSelection = false
        collection.selfSizingInvalidation = .enabled
        super.init()
        listLayout = layout
        box.list = self
        let reg = UICollectionView.CellRegistration<NuiListCell, Int64> { _, _, _ in }
        source = UICollectionViewDiffableDataSource(collectionView: collection) { [weak self] cv, path, item in
            let cell = cv.dequeueConfiguredReusableCell(using: reg, for: path, item: item)
            guard let self else { return cell }
            cell.list = self
            if cell.nuiID == 0 {
                cell.nuiID = runtime.makeCell(cell)
            }
            self.binding = true
            runtime.enter { nui_list_bind(self.id, Int64(path.item), cell.nuiID) }
            self.binding = false
            return cell
        }
    }

    func update(_ ids: [Int64]) { pending = ids }

    /// A grid of columns, gap apart (as LazyVGrid's): each cell an equal
    /// share of the width, a row as tall as its tallest; or, columns 1,
    /// the list again.
    func setGrid(columns: Int, gap: CGFloat) {
        let columns = max(columns, 1)
        guard columns != self.columns || gap != self.gap else { return }
        self.columns = columns
        self.gap = gap
        if columns == 1 {
            collection.setCollectionViewLayout(listLayout, animated: false)
            return
        }
        // Each cell exactly as wide as the core lays it out.
        let layout = UICollectionViewCompositionalLayout { _, env in
            let w = (env.container.effectiveContentSize.width - gap * CGFloat(columns - 1)) / CGFloat(columns)
            let item = NSCollectionLayoutItem(layoutSize: .init(widthDimension: .absolute(max(w, 1)), heightDimension: .estimated(100)))
            let group = NSCollectionLayoutGroup.horizontal(
                layoutSize: .init(widthDimension: .fractionalWidth(1), heightDimension: .estimated(100)),
                repeatingSubitem: item, count: columns)
            group.interItemSpacing = .fixed(gap)
            let section = NSCollectionLayoutSection(group: group)
            section.interGroupSpacing = gap
            return section
        }
        collection.setCollectionViewLayout(layout, animated: false)
    }

    /// Scrolls item i into view, at the top.
    func scroll(to i: Int, animated: Bool) {
        applyPending()
        guard i >= 0, i < collection.numberOfItems(inSection: 0) else { return }
        collection.scrollToItem(at: IndexPath(item: i, section: 0), at: .top, animated: animated)
    }

    func applyPending() {
        guard let ids = pending else { return }
        pending = nil
        var snap = NSDiffableDataSourceSnapshot<Int, Int64>()
        snap.appendSections([0])
        snap.appendItems(ids)
        source.apply(snap, animatingDifferences: applied)
        applied = true
    }

    func heightsChanged() {
        guard !binding else { return }
        collection.collectionViewLayout.invalidateLayout()
    }
}
#endif

#if canImport(UIKit)
/// Reports a scroll view's offset (from its content's start) as Scrolled.
/// A list's collection view takes it as its delegate too.
@MainActor final class NuiScrollReporter: NSObject, UICollectionViewDelegate {
    let id: UInt32
    weak var runtime: NuiRuntime?
    private var last = CGPoint(x: CGFloat.nan, y: .nan)

    init(_ id: UInt32, _ rt: NuiRuntime) {
        self.id = id
        runtime = rt
    }

    func scrollViewDidScroll(_ s: UIScrollView) {
        // Not a view on its way out (its insets change as it leaves the
        // window), whose ID may be another view's by the time it's read.
        guard s.window != nil, runtime?.nodes[id]?.view === s else { return }
        let i = s.adjustedContentInset
        let p = CGPoint(x: s.contentOffset.x + i.left, y: s.contentOffset.y + i.top)
        guard p != last else { return }
        last = p
        let id = self.id
        runtime?.emit(id, .scrolled) { NuiEvents.scrolled(id, x: Double(p.x), y: Double(p.y)) }
    }
}
#endif
