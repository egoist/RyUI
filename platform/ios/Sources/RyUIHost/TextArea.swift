#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A multiline TextField: a UITextView with a placeholder, bordered as a
/// rounded-rect field is. It grows with its text up to maxLines lines,
/// then scrolls.
final class RyUITextView: UITextView {
    var ryuiID: UInt32 = 0
    var maxLines = 0
    let placeholderLabel = UILabel()
    /// TextFieldProps.Plain: no border, background nor inset, the text
    /// alone.
    var plain = false {
        didSet {
            layer.borderWidth = plain ? 0 : 1 / UIScreen.main.scale
            backgroundColor = plain ? .clear : .systemBackground
            textContainerInset = plain ? .zero : UIEdgeInsets(top: 7, left: 3, bottom: 7, right: 3)
            textContainer.lineFragmentPadding = plain ? 0 : 5
            setNeedsLayout()
        }
    }

    var placeholder: String? {
        get { placeholderLabel.text }
        set { placeholderLabel.text = newValue; setNeedsLayout() }
    }

    /// The app's label, or, as a UITextField's, the placeholder.
    var label: String?
    override var accessibilityLabel: String? {
        get { label ?? placeholderLabel.text }
        set { label = newValue }
    }

    override var font: UIFont? {
        didSet { placeholderLabel.font = font }
    }

    override var text: String! {
        didSet { placeholderLabel.isHidden = !(text ?? "").isEmpty }
    }

    init() {
        super.init(frame: .zero, textContainer: nil)
        font = .preferredFont(forTextStyle: .body)
        adjustsFontForContentSizeCategory = true
        backgroundColor = .systemBackground
        layer.cornerRadius = 5
        layer.borderWidth = 1 / UIScreen.main.scale
        textContainerInset = UIEdgeInsets(top: 7, left: 3, bottom: 7, right: 3)
        isScrollEnabled = false
        placeholderLabel.textColor = .placeholderText
        placeholderLabel.font = font
        placeholderLabel.adjustsFontForContentSizeCategory = true
        placeholderLabel.numberOfLines = 0
        placeholderLabel.isAccessibilityElement = false
        addSubview(placeholderLabel)
        updateBorder()
        if #available(iOS 17, *) {
            registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (v: RyUITextView, _: UITraitCollection) in v.updateBorder() }
        }
    }
    required init?(coder: NSCoder) { fatalError() }

    private func updateBorder() {
        layer.borderColor = UIColor.systemGray4.resolvedColor(with: traitCollection).cgColor
    }

    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        if #unavailable(iOS 17) { updateBorder() }
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let pad = textContainer.lineFragmentPadding
        let x = textContainerInset.left + pad
        let w = bounds.width - x - textContainerInset.right - pad
        placeholderLabel.frame = CGRect(x: x, y: textContainerInset.top, width: w, height: placeholderLabel.sizeThatFits(CGSize(width: w, height: .greatestFiniteMagnitude)).height)
    }

    /// The height it wants at width: its text's (at least a line), up to
    /// maxLines lines; past them it scrolls.
    func height(at width: CGFloat) -> CGFloat {
        let insets = textContainerInset.top + textContainerInset.bottom
        let line = (font ?? .preferredFont(forTextStyle: .body)).lineHeight
        var h = sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude)).height
        h = max(h, ceil(line + insets))
        if maxLines > 0 {
            let cap = ceil(line * CGFloat(maxLines) + insets)
            isScrollEnabled = h > cap
            h = min(h, cap)
        } else {
            isScrollEnabled = false
        }
        return h
    }
}

@MainActor extension RyUIRuntime {
    /// Makes a TextField multiline: its UITextField becomes a RyUITextView
    /// (it's sent before the field is inserted; one in a view is replaced).
    func makeMultiline(_ n: RyUINode) {
        guard let old = n.view as? RyUITextField else { return }
        let t = RyUITextView()
        t.ryuiID = n.id
        t.tag = old.tag
        t.delegate = self
        t.text = old.text
        t.placeholder = old.placeholder
        t.keyboardType = old.keyboardType
        t.autocapitalizationType = old.autocapitalizationType
        t.autocorrectionType = old.autocorrectionType
        t.textContentType = old.textContentType
        t.plain = old.plain
        if let p = old.superview {
            p.insertSubview(t, aboveSubview: old)
            t.frame = old.frame
            old.removeFromSuperview()
        }
        n.view = t
        n.applyFont()
        n.applyColor()
    }
}

extension RyUIRuntime: UITextViewDelegate {
    public func textViewDidChange(_ textView: UITextView) {
        guard let t = textView as? RyUITextView else { return }
        t.placeholderLabel.isHidden = !t.text.isEmpty
        let id = t.ryuiID, s = t.text ?? ""
        emit(id, .textChanged) { RyUIEvents.textChanged(id, s) }
    }

    public func textViewDidBeginEditing(_ textView: UITextView) {
        guard let t = textView as? RyUITextView else { return }
        let id = t.ryuiID
        emit(id, .focus) { RyUIEvents.focus(id) }
    }

    public func textViewDidEndEditing(_ textView: UITextView) {
        guard let t = textView as? RyUITextView else { return }
        let id = t.ryuiID
        emit(id, .blur) { RyUIEvents.blur(id) }
    }
}
#endif
