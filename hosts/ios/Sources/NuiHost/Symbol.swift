#if canImport(UIKit)
import UIKit
#if canImport(NuiOps)
import NuiOps
#endif

/// A Symbol: an SF Symbol at its node's font, made with the app's text
/// size (as rich text measures) so that what's measured is what's drawn.
final class NuiSymbolView: UIImageView {
    var name = "" { didSet { if name != oldValue { refresh() } } }
    weak var node: NuiNode?

    override init(frame: CGRect) {
        super.init(frame: frame)
        contentMode = .center
        tintColor = .label
        isAccessibilityElement = false // decoration, unless the app labels it
        if #available(iOS 17, *) {
            registerForTraitChanges([UITraitPreferredContentSizeCategory.self]) { (v: NuiSymbolView, _: UITraitCollection) in v.refresh() }
        }
    }
    required init?(coder: NSCoder) { fatalError() }

    func refresh() {
        guard !name.isEmpty else { image = nil; return }
        let traits = UITraitCollection(traitsFrom: [traitCollection, UITraitCollection(preferredContentSizeCategory: UIApplication.shared.preferredContentSizeCategory)])
        var font = UIFont.preferredFont(forTextStyle: .body)
        traits.performAsCurrent { font = node?.font ?? .preferredFont(forTextStyle: .body, compatibleWith: traits) }
        image = UIImage(systemName: name, withConfiguration: UIImage.SymbolConfiguration(font: font, scale: .medium))
    }
}
#endif
