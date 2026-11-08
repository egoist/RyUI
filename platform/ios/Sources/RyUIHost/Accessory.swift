#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A TextField's Accessory: its inputAccessoryView, a bar UIKit puts on top
/// of the keyboard and moves with it, through its interactive dismissal
/// too. The core lays its content out as wide as the root and as tall as
/// it is: UIKit takes its height from intrinsicContentSize.
final class RyUIAccessoryView: UIView {
    var height: CGFloat = 0 {
        didSet {
            guard height != oldValue else { return }
            frame.size.height = height
            invalidateIntrinsicContentSize()
            superview?.setNeedsLayout()
        }
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
        autoresizingMask = .flexibleWidth
    }
    required init?(coder: NSCoder) { fatalError() }

    override var intrinsicContentSize: CGSize { CGSize(width: UIView.noIntrinsicMetric, height: height) }
}

@MainActor extension RyUIRuntime {
    /// Gives a field its accessory (0: none), at once if it's being edited.
    func setAccessory(_ n: RyUINode, _ id: UInt32) {
        let bar = nodes[id]?.view as? RyUIAccessoryView
        if let f = n.view as? UITextField {
            f.inputAccessoryView = bar
        } else if let t = n.view as? UITextView {
            t.inputAccessoryView = bar
        }
        if n.view.isFirstResponder { n.view.reloadInputViews() }
    }
}
#endif
