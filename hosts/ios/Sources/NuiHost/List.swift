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

    init(id: UInt32, runtime: NuiRuntime) {
        self.id = id
        var config = UICollectionLayoutListConfiguration(appearance: .plain)
        config.showsSeparators = true
        collection = UICollectionView(frame: .zero, collectionViewLayout: UICollectionViewCompositionalLayout.list(using: config))
        collection.keyboardDismissMode = .interactive
        // Rows handle their own presses; a cell selection would stick.
        collection.allowsSelection = false
        collection.selfSizingInvalidation = .enabled
        super.init()
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
