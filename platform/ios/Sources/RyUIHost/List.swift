#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif
#if canImport(CRyUI)
@_implementationOnly import CRyUI
#endif

/// A list cell, or a section's header: its content view holds a row the
/// core lays out; the row's height comes from the core.
final class RyUIListCell: UICollectionViewListCell {
    var ryuiID: UInt32 = 0
    var rowHeight: CGFloat = 44
    weak var list: RyUIList?
    /// The core says its row is selected (shown while the list is selecting).
    var ryuiSelected = false
    /// A header's, while it's pinned at the top: iOS 26's scroll edge
    /// effect goes behind it (UIScrollEdgeElementContainerInteraction).
    var edgeInteraction: UIInteraction?

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

/// The collection view of a List: it tells the list as it lays out, so a
/// list anchored at its bottom can stay there.
final class RyUIListView: UICollectionView {
    weak var owner: RyUIList?

    override func layoutSubviews() {
        owner?.willLayout()
        super.layoutSubviews()
        owner?.didLayout()
    }
}

/// Lets the list's layout configuration reach the list made after it.
final class RyUIWeakList {
    weak var list: RyUIList?
}

/// A List: a UICollectionView with a list layout, fed by a diffable data
/// source from the core's item IDs (in sections, when the core sends
/// them, each with a header the core binds too). Cells bind their rows
/// through ryui_list_bind when they're about to show.
@MainActor final class RyUIList: NSObject, UICollectionViewDelegate {
    let id: UInt32
    weak var runtime: RyUIRuntime?
    let collection: RyUIListView
    var source: UICollectionViewDiffableDataSource<Int64, Int64>!
    var pending: (ids: [Int64], sections: [(start: Int, id: Int64)])?
    var applied = false
    var binding = false
    private var applying = false
    var columns = 1
    var gap: CGFloat = 0
    var appearance = RyUIListAppearance.plain
    var inset: CGFloat = 0
    /// The first item's index of each section shown (one section: [0]).
    private var starts = [0]
    private var hasSections = false
    private var swipes: UICollectionLayoutListConfiguration.SwipeActionsConfigurationProvider!
    private var leadingSwipes: UICollectionLayoutListConfiguration.SwipeActionsConfigurationProvider!

    // Anchored at the bottom: whether it's at its end (and stays there),
    // the room added above rows that don't fill it, and a scroll to the
    // end under way, which layout leaves alone.
    private var bottom = false
    private var stuck = true
    private var extraTop: CGFloat = 0
    private var scrollingToEnd = false
    // Selecting, and the room its control takes (leading, trailing).
    private(set) var editing = false
    private var rowInset = CGPoint.zero

    init(id: UInt32, runtime: RyUIRuntime) {
        self.id = id
        self.runtime = runtime
        let box = RyUIWeakList()
        collection = RyUIListView(frame: .zero, collectionViewLayout: UICollectionViewFlowLayout())
        super.init()
        // A row's swipe actions are its cell's (the core sends them as it binds it).
        swipes = { [weak runtime] path in
            guard let runtime, let list = box.list else { return nil }
            return list.swipeConfiguration(runtime, path, leading: false)
        }
        leadingSwipes = { [weak runtime] path in
            guard let runtime, let list = box.list else { return nil }
            return list.swipeConfiguration(runtime, path, leading: true)
        }
        box.list = self
        collection.owner = self
        collection.delegate = self
        collection.keyboardDismissMode = .interactive
        // Rows handle their own presses; a cell selection would stick.
        // Selecting (edit mode), a tap selects its row.
        collection.allowsSelection = false
        collection.allowsSelectionDuringEditing = true
        collection.allowsMultipleSelectionDuringEditing = true
        collection.selfSizingInvalidation = .enabled
        relayout()
        let reg = UICollectionView.CellRegistration<RyUIListCell, Int64> { _, _, _ in }
        source = UICollectionViewDiffableDataSource(collectionView: collection) { [weak self] cv, path, item in
            let cell = cv.dequeueConfiguredReusableCell(using: reg, for: path, item: item)
            guard let self, let runtime = self.runtime else { return cell }
            cell.list = self
            if self.appearance == .bare { cell.backgroundConfiguration = .clear() }
            cell.accessories = [.multiselect(displayed: .whenEditing)]
            cell.contentView.isUserInteractionEnabled = !self.editing
            if cell.ryuiID == 0 {
                cell.ryuiID = runtime.makeCell(cell)
            }
            self.bind(runtime, self.index(of: path), cell)
            return cell
        }
        let headers = UICollectionView.SupplementaryRegistration<RyUIListCell>(elementKind: UICollectionView.elementKindSectionHeader) { [weak self] cell, _, path in
            guard let self, let runtime = self.runtime else { return }
            cell.list = self
            cell.backgroundConfiguration = switch self.appearance {
            case .bare: .clear()
            case .insetGrouped: .listGroupedHeaderFooter()
            default: .listPlainHeaderFooter()
            }
            if cell.ryuiID == 0 {
                cell.ryuiID = runtime.makeCell(cell)
            }
            self.bind(runtime, -1 - path.section, cell)
        }
        source.supplementaryViewProvider = { cv, kind, path in
            cv.dequeueConfiguredReusableSupplementary(using: headers, for: path)
        }
    }

    private func bind(_ runtime: RyUIRuntime, _ index: Int, _ cell: RyUIListCell) {
        binding = true
        runtime.enter { ryui_list_bind(self.id, Int64(index), cell.ryuiID) }
        binding = false
    }

    /// Item path's index among all the list's items.
    private func index(of path: IndexPath) -> Int {
        (path.section < starts.count ? starts[path.section] : 0) + path.item
    }

    /// The path of item i, among the sections shown.
    private func path(of i: Int) -> IndexPath? {
        guard i >= 0, i < collection.numberOfItemsTotal else { return nil }
        var s = 0
        while s + 1 < starts.count && starts[s + 1] <= i { s += 1 }
        return IndexPath(item: i - starts[s], section: s)
    }

    private func swipeConfiguration(_ runtime: RyUIRuntime, _ path: IndexPath, leading: Bool) -> UISwipeActionsConfiguration? {
        guard !editing, let cell = collection.cellForItem(at: path) as? RyUIListCell,
              let n = runtime.nodes[cell.ryuiID] else { return nil }
        let acts = leading ? n.leadingSwipeActions : n.swipeActions
        guard !acts.isEmpty else { return nil }
        let id = cell.ryuiID
        let actions = acts.enumerated().map { i, act in
            let a = UIContextualAction(style: act.role == RyUIAlertButtonRole.destructive.rawValue ? .destructive : .normal,
                                       title: act.title) { [weak runtime] _, _, done in
                runtime?.emit(id, .action) { RyUIEvents.action(id, leading ? Int64(-1 - i) : Int64(i)) }
                done(true)
            }
            a.image = act.image
            // A leading action that isn't destructive is tinted, as Mail's are.
            if leading && act.role != RyUIAlertButtonRole.destructive.rawValue { a.backgroundColor = .tintColor }
            return a
        }
        return UISwipeActionsConfiguration(actions: actions)
    }

    func update(_ ids: [Int64], sections: [(start: Int, id: Int64)]) { pending = (ids, sections) }

    // MARK: Layout

    /// The list layout's configuration, in an appearance.
    private func config(_ appearance: UICollectionLayoutListConfiguration.Appearance) -> UICollectionLayoutListConfiguration {
        var config = UICollectionLayoutListConfiguration(appearance: appearance)
        config.showsSeparators = self.appearance != .bare
        if self.appearance == .bare { config.backgroundColor = .clear }
        config.headerMode = hasSections ? .supplementary : .none
        config.trailingSwipeActionsConfigurationProvider = swipes
        config.leadingSwipeActionsConfigurationProvider = leadingSwipes
        return config
    }

    /// Lays the list out again for its look: a list (plain, bare, or
    /// inset grouped, its rows inset from each side by exactly the core's
    /// inset, which lays them out at the list's width less it, not by the
    /// collection view's layout margins), or a grid of columns, gap
    /// apart (as LazyVGrid's: each cell an equal share of the width, a
    /// row as tall as its tallest). A plain or bare list's headers stick
    /// to its top, as UITableView's plain style's do.
    private func relayout() {
        let pin = appearance != .insetGrouped
        let layout: UICollectionViewLayout
        if columns > 1 {
            let columns = columns, gap = gap, headers = hasSections
            layout = UICollectionViewCompositionalLayout { _, env in
                let w = (env.container.effectiveContentSize.width - gap * CGFloat(columns - 1)) / CGFloat(columns)
                let item = NSCollectionLayoutItem(layoutSize: .init(widthDimension: .absolute(max(w, 1)), heightDimension: .estimated(100)))
                let group = NSCollectionLayoutGroup.horizontal(
                    layoutSize: .init(widthDimension: .fractionalWidth(1), heightDimension: .estimated(100)),
                    repeatingSubitem: item, count: columns)
                group.interItemSpacing = .fixed(gap)
                let section = NSCollectionLayoutSection(group: group)
                section.interGroupSpacing = gap
                if headers {
                    let h = NSCollectionLayoutBoundarySupplementaryItem(
                        layoutSize: .init(widthDimension: .fractionalWidth(1), heightDimension: .estimated(44)),
                        elementKind: UICollectionView.elementKindSectionHeader, alignment: .top)
                    h.pinToVisibleBounds = pin
                    h.zIndex = 2
                    section.boundarySupplementaryItems = [h]
                    section.contentInsets.bottom = gap
                }
                return section
            }
        } else {
            let config = config(appearance == .insetGrouped ? .insetGrouped : .plain)
            let grouped = appearance == .insetGrouped, inset = inset
            layout = UICollectionViewCompositionalLayout { _, env in
                let section = NSCollectionLayoutSection.list(using: config, layoutEnvironment: env)
                if grouped {
                    section.contentInsetsReference = .none
                    section.contentInsets.leading = inset
                    section.contentInsets.trailing = inset
                }
                for h in section.boundarySupplementaryItems {
                    h.pinToVisibleBounds = pin
                    h.zIndex = 2
                }
                return section
            }
        }
        collection.backgroundColor = appearance == .bare ? .clear : nil
        collection.setCollectionViewLayout(layout, animated: false)
    }

    func setGrid(columns: Int, gap: CGFloat) {
        let columns = max(columns, 1)
        guard columns != self.columns || gap != self.gap else { return }
        self.columns = columns
        self.gap = gap
        relayout()
    }

    func setAppearance(_ appearance: RyUIListAppearance, inset: CGFloat) {
        guard appearance != self.appearance || inset != self.inset else { return }
        self.appearance = appearance
        self.inset = inset
        relayout()
    }

    /// Anchored at the bottom: it opens at its end and stays there while
    /// the user leaves it there.
    func setAnchor(_ a: RyUIScrollAnchor) {
        bottom = a == .bottom
        stuck = true
        collection.setNeedsLayout()
    }

    // MARK: Scrolling

    private var maxOffsetY: CGFloat {
        let i = collection.adjustedContentInset
        return max(-i.top, collection.contentSize.height - collection.bounds.height + i.bottom)
    }

    private var atEnd: Bool { collection.contentOffset.y >= maxOffsetY - 1 }
    private var atStart: Bool { collection.contentOffset.y <= -collection.adjustedContentInset.top + 1 }

    /// Before the collection view lays out: rows that don't fill a list
    /// anchored at its bottom sit at its bottom, and one at its end goes
    /// to its end (its new end: rows added, rows grown, the list shorter).
    func willLayout() {
        guard bottom, collection.bounds.height > 0 else { return }
        let content = collection.collectionViewLayout.collectionViewContentSize.height
        let i = collection.adjustedContentInset
        let free = max(0, collection.bounds.height - (i.top - extraTop) - i.bottom - content)
        if abs(free - extraTop) > 0.5 {
            collection.contentInset.top += free - extraTop
            extraTop = free
        }
        guard stuck, !scrollingToEnd, !collection.isTracking, !collection.isDecelerating else { return }
        let a = collection.adjustedContentInset
        let end = max(-a.top, content - collection.bounds.height + a.bottom)
        if abs(collection.contentOffset.y - end) > 0.5 {
            collection.contentOffset.y = end
        }
    }

    /// After: rows sized as they showed may have moved the end.
    func didLayout() {
        pinEdgeElements()
        guard bottom, stuck, !scrollingToEnd, !collection.isTracking, !collection.isDecelerating else { return }
        if abs(collection.contentOffset.y - maxOffsetY) > 0.5 { collection.setNeedsLayout() }
    }

    /// The header pinned at the top is an element over the content: iOS
    /// 26's scroll edge effect goes behind it, not over it (it would blur
    /// it away). Only that one: the effect's region (which takes touches)
    /// reaches as far down as its elements do.
    private func pinEdgeElements() {
        guard #available(iOS 26.0, *), hasSections else { return }
        let top = collection.contentOffset.y + collection.adjustedContentInset.top
        let pin = appearance != .insetGrouped
        for case let c as RyUIListCell in collection.visibleSupplementaryViews(ofKind: UICollectionView.elementKindSectionHeader) {
            let pinned = pin && abs(c.frame.minY - top) < 0.5
            if pinned, c.edgeInteraction == nil {
                let i = UIScrollEdgeElementContainerInteraction()
                i.scrollView = collection
                i.edge = .top
                c.addInteraction(i)
                c.edgeInteraction = i
            } else if !pinned, let i = c.edgeInteraction {
                c.removeInteraction(i)
                c.edgeInteraction = nil
            }
        }
    }

    func scrollViewDidScroll(_ s: UIScrollView) {
        // Only the user's scrolling moves a list off its end, or back.
        if bottom && (s.isTracking || s.isDecelerating) && !scrollingToEnd { stuck = atEnd }
        runtime?.nodes[id]?.scrolls?.scrollViewDidScroll(s)
    }

    func scrollViewDidEndDecelerating(_ s: UIScrollView) {
        if bottom { stuck = atEnd }
    }

    func scrollViewDidEndDragging(_ s: UIScrollView, willDecelerate: Bool) {
        if bottom && !willDecelerate { stuck = atEnd }
    }

    func scrollViewDidEndScrollingAnimation(_ s: UIScrollView) {
        scrollingToEnd = false
        if bottom { stuck = atEnd }
    }

    func scrollViewDidScrollToTop(_ s: UIScrollView) {
        if bottom { stuck = atEnd }
    }

    /// Scrolls item i into view, at the top.
    func scroll(to i: Int, animated: Bool) {
        applyPending()
        guard let p = path(of: i) else { return }
        if bottom { stuck = i == collection.numberOfItemsTotal - 1 }
        collection.scrollToItem(at: p, at: i == collection.numberOfItemsTotal - 1 && bottom ? .bottom : .top, animated: animated)
    }

    /// Scrolls to an offset the core gives (from the content's start):
    /// to the end, a list anchored at its bottom stays there again.
    func scrolled(toY y: CGFloat) {
        if bottom { stuck = y >= maxOffsetY - 1 }
    }

    // MARK: Updates

    /// Applies what the core last sent. A row being bound can change the list
    /// it's in (its language changing, say), and a data source mustn't be applied
    /// to from inside itself or a cell's making: that waits until it's done.
    ///
    /// Rows added or removed above the ones on screen keep those where
    /// they are, unless the list is at the edge it's anchored to: at its
    /// start, new rows there show (a list read from the top); at its end,
    /// anchored at the bottom, it scrolls to show new rows there.
    func applyPending() {
        guard !applying else { return }
        guard !binding else {
            DispatchQueue.main.async { [weak self] in self?.applyPending() }
            return
        }
        applying = true
        defer { applying = false }
        while let (ids, sections) = pending {
            pending = nil
            var snap = NSDiffableDataSourceSnapshot<Int64, Int64>()
            var next = [0]
            let headers = !sections.isEmpty
            if headers {
                next = []
                for (k, s) in sections.enumerated() {
                    let end = k + 1 < sections.count ? sections[k + 1].start : ids.count
                    snap.appendSections([s.id])
                    snap.appendItems(Array(ids[min(s.start, ids.count)..<min(max(end, s.start), ids.count)]), toSection: s.id)
                    next.append(s.start)
                }
            } else {
                snap.appendSections([0])
                snap.appendItems(ids)
            }
            if headers != hasSections {
                hasSections = headers
                relayout()
            }
            let old = source.snapshot().itemIdentifiers
            let wasApplied = applied
            let atEdge = bottom ? stuck : atStart
            let keep = wasApplied && !atEdge ? visibleAnchor() : nil
            // What's above the row kept in place changed: no animation,
            // and the row back where it was.
            var moved = false
            if let k = keep, let o = old.firstIndex(of: k.id), let n = ids.firstIndex(of: k.id) {
                moved = o != n || old[..<o] != ids[..<n]
            }
            let appendedAtEnd = wasApplied && bottom && stuck && ids.count > old.count && ids.last != old.last
            starts = next
            source.apply(snap, animatingDifferences: wasApplied && !moved)
            applied = true
            if moved, let k = keep, let p = source.indexPath(for: k.id) {
                collection.layoutIfNeeded()
                if let a = collection.layoutAttributesForItem(at: p) {
                    collection.contentOffset.y = a.frame.minY - k.dy
                }
            }
            if appendedAtEnd && collection.window != nil {
                // New rows at the end of a chat the user is at the end of.
                collection.layoutIfNeeded()
                let end = CGPoint(x: collection.contentOffset.x, y: maxOffsetY)
                if abs(end.y - collection.contentOffset.y) > 0.5 {
                    scrollingToEnd = true
                    collection.setContentOffset(end, animated: true)
                }
            }
        }
    }

    /// The first row on screen, and its top's distance below the offset.
    private func visibleAnchor() -> (id: Int64, dy: CGFloat)? {
        let top = collection.contentOffset.y + collection.adjustedContentInset.top
        var best: (IndexPath, CGRect)?
        for p in collection.indexPathsForVisibleItems {
            guard let a = collection.layoutAttributesForItem(at: p), a.frame.maxY > top else { continue }
            if best == nil || a.frame.minY < best!.1.minY { best = (p, a.frame) }
        }
        guard let (p, f) = best, let id = source.itemIdentifier(for: p) else { return nil }
        return (id, f.minY - collection.contentOffset.y)
    }

    /// The layout mirrors its cells for the collection view's direction.
    func directionChanged() {
        collection.collectionViewLayout.invalidateLayout()
        measureRowInset()
    }

    func heightsChanged() {
        guard !binding else { return }
        collection.collectionViewLayout.invalidateLayout()
    }

    // MARK: Selection

    /// Selecting rows (UIKit's edit mode): each row shows its selection
    /// control, a tap selects it, its content doesn't take touches, and
    /// rows don't swipe. The rows the core says are selected show it.
    func setEditing(_ on: Bool) {
        coreEditing = on
        applyEditing(on)
    }

    /// What the core last said of Selecting.
    private var coreEditing = false

    private func applyEditing(_ on: Bool) {
        guard on != editing else { return }
        editing = on
        // Selection is the list's while it's selecting; otherwise rows
        // handle their own presses, and a cell selection would stick.
        collection.allowsSelection = on
        collection.allowsMultipleSelection = on
        collection.isEditing = on
        for case let c as RyUIListCell in collection.visibleCells {
            c.contentView.isUserInteractionEnabled = !on
        }
        if on {
            for case let c as RyUIListCell in collection.visibleCells where c.ryuiSelected {
                if let p = collection.indexPath(for: c) { collection.selectItem(at: p, animated: false, scrollPosition: []) }
            }
        } else {
            // Not selecting, no row looks selected.
            for p in collection.indexPathsForSelectedItems ?? [] { collection.deselectItem(at: p, animated: false) }
        }
        measureRowInset()
    }

    /// The core says cell's row is (or isn't) selected.
    func setSelected(_ cell: RyUIListCell, _ v: Bool) {
        cell.ryuiSelected = v
        guard editing, !binding, let p = collection.indexPath(for: cell) else { return }
        showSelection(cell, p)
    }

    private func showSelection(_ cell: RyUIListCell, _ p: IndexPath) {
        let shown = collection.indexPathsForSelectedItems?.contains(p) ?? false
        let want = editing && cell.ryuiSelected
        if want && !shown { collection.selectItem(at: p, animated: false, scrollPosition: []) }
        if !want && shown { collection.deselectItem(at: p, animated: false) }
    }

    func collectionView(_ cv: UICollectionView, willDisplay cell: UICollectionViewCell, forItemAt p: IndexPath) {
        guard let c = cell as? RyUIListCell else { return }
        c.contentView.isUserInteractionEnabled = !editing
        showSelection(c, p)
        if editing && rowInset == .zero { measureRowInset() }
    }

    func collectionView(_ cv: UICollectionView, didSelectItemAt p: IndexPath) {
        userSelected(p, true)
    }

    func collectionView(_ cv: UICollectionView, didDeselectItemAt p: IndexPath) {
        userSelected(p, false)
    }

    private func userSelected(_ p: IndexPath, _ v: Bool) {
        guard editing, let c = collection.cellForItem(at: p) as? RyUIListCell else { return }
        c.ryuiSelected = v
        let id = c.ryuiID
        runtime?.emit(id, .rowSelected) { RyUIEvents.rowSelected(id, v) }
    }

    /// A two-finger pan over rows selects them: while selecting, or to
    /// begin selecting when the app asks to hear of it.
    func collectionView(_ cv: UICollectionView, shouldBeginMultipleSelectionInteractionAt p: IndexPath) -> Bool {
        if editing { return true }
        guard let c = cv.cellForItem(at: p) as? RyUIListCell else { return false }
        return runtime?.nodes[c.ryuiID]?.listens.contains(RyUIEventKind.rowSelectBegin.rawValue) ?? false
    }

    func collectionView(_ cv: UICollectionView, didBeginMultipleSelectionInteractionAt p: IndexPath) {
        guard !editing, let c = cv.cellForItem(at: p) as? RyUIListCell else { return }
        // At once, so the pan selects as it goes; the core's Selecting
        // follows, or, if the app doesn't turn it on, it ends.
        applyEditing(true)
        let id = c.ryuiID
        runtime?.emit(id, .rowSelectBegin) { RyUIEvents.rowSelectBegin(id) }
        DispatchQueue.main.async { [weak self] in
            guard let self, !self.coreEditing else { return }
            self.applyEditing(false)
        }
    }

    /// Tells the core how much room the selection control takes from the
    /// rows (the core lays them out at the width they have).
    private func measureRowInset() {
        var next = CGPoint.zero
        if editing {
            collection.layoutIfNeeded()
            guard let c = collection.visibleCells.compactMap({ $0 as? RyUIListCell }).first else { return }
            c.layoutIfNeeded()
            let f = c.contentView.frame
            let left = f.minX, right = c.bounds.width - f.maxX
            next = collection.effectiveUserInterfaceLayoutDirection == .rightToLeft ? CGPoint(x: right, y: left) : CGPoint(x: left, y: right)
        }
        guard next != rowInset else { return }
        rowInset = next
        runtime?.emit(id, .rowInset) { RyUIEvents.rowInset(id, x: Double(next.x), y: Double(next.y)) }
    }
}

extension UICollectionView {
    /// Its items, in all its sections.
    var numberOfItemsTotal: Int {
        (0..<numberOfSections).reduce(0) { $0 + numberOfItems(inSection: $1) }
    }
}
#endif

#if canImport(UIKit)
/// Reports a scroll view's offset (from its content's start) as Scrolled.
/// A list forwards its collection view's scrolling to it.
@MainActor final class RyUIScrollReporter: NSObject, UIScrollViewDelegate {
    let id: UInt32
    weak var runtime: RyUIRuntime?
    private var last = CGPoint(x: CGFloat.nan, y: .nan)

    init(_ id: UInt32, _ rt: RyUIRuntime) {
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
        runtime?.emit(id, .scrolled) { RyUIEvents.scrolled(id, x: Double(p.x), y: Double(p.y)) }
    }

    /// A paging Scroll's swipe: the page it's settling on, from the content's
    /// physical start, as soon as UIKit knows it.
    func scrollViewWillEndDragging(_ s: UIScrollView, withVelocity velocity: CGPoint, targetContentOffset: UnsafeMutablePointer<CGPoint>) {
        guard s.isPagingEnabled, s.bounds.width > 0, s.bounds.height > 0 else { return }
        let i = s.adjustedContentInset
        let t = targetContentOffset.pointee
        let horizontal = (s as? RyUIScrollView)?.horizontal ?? (s.contentSize.width > s.bounds.width)
        let page = horizontal ? ((t.x + i.left) / s.bounds.width).rounded() : ((t.y + i.top) / s.bounds.height).rounded()
        let id = self.id
        runtime?.emit(id, .paged) { RyUIEvents.paged(id, Int64(page)) }
    }
}
#endif
