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
            updateInsets()
            textContainer.lineFragmentPadding = plain ? 0 : 5
            setNeedsLayout()
        }
    }

    /// ContentInset: its padding, inside its own inset.
    var contentInset_: UIEdgeInsets = .zero {
        didSet { if contentInset_ != oldValue { updateInsets(); setNeedsLayout() } }
    }

    private func updateInsets() {
        let own = plain ? UIEdgeInsets.zero : UIEdgeInsets(top: 7, left: 3, bottom: 7, right: 3)
        textContainerInset = UIEdgeInsets(top: own.top + contentInset_.top, left: own.left + contentInset_.left,
                                          bottom: own.bottom + contentInset_.bottom, right: own.right + contentInset_.right)
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

    /// TextFieldProps.PasteTypes: content of these types pastes into the
    /// app's OnPaste, not the text.
    var pasteTypes: [String] = []
    /// TextFieldProps.Rich: its text is styled (RichField.swift); the
    /// core is setting it; the styles at the cursor as last reported.
    var rich = false
    var settingRich = false
    var lastFormats = RyUIFormat(rawValue: 0xff)
    var lastLink = ""

    override func canPerformAction(_ action: Selector, withSender sender: Any?) -> Bool {
        if action == #selector(paste(_:)), RyUIPaste.available(pasteTypes) { return true }
        return super.canPerformAction(action, withSender: sender)
    }

    override func paste(_ sender: Any?) {
        if RyUIPaste.take(ryuiID, pasteTypes) { return }
        super.paste(sender)
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

    /// A text area that grows with its text doesn't scroll: what holds it does.
    /// Keeps the caret in sight in that, above the keyboard, as it moves.
    func revealCaret() {
        guard isFirstResponder, let at = selectedTextRange?.end else { return }
        var holder = superview
        while let v = holder, !((v as? UIScrollView)?.isScrollEnabled ?? false) { holder = v.superview }
        guard let scroll = holder as? UIScrollView else { return }
        let caret = caretRect(for: at)
        guard !caret.isNull, !caret.isInfinite, caret.origin.y.isFinite else { return }
        scroll.scrollRectToVisible(convert(caret, to: scroll).insetBy(dx: 0, dy: -14), animated: false)
    }

    private func updateBorder() {
        layer.borderColor = UIColor.systemGray4.resolvedColor(with: traitCollection).cgColor
    }

    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        if #unavailable(iOS 17) { updateBorder() }
    }

    private var laidOutHeight: CGFloat = 0

    override func layoutSubviews() {
        // Grown by a new line: now there's somewhere for the caret to go.
        if bounds.height != laidOutHeight {
            laidOutHeight = bounds.height
            DispatchQueue.main.async { [weak self] in self?.revealCaret() }
        }
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
        // A placeholder can run longer than a line: it's what's showing.
        if (text ?? "").isEmpty, !(placeholderLabel.text ?? "").isEmpty {
            let pad = textContainer.lineFragmentPadding
            let w = width - textContainerInset.left - textContainerInset.right - 2 * pad
            h = max(h, ceil(placeholderLabel.sizeThatFits(CGSize(width: w, height: .greatestFiniteMagnitude)).height + insets))
        }
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
        t.inputAccessoryView = old.inputAccessoryView
        t.plain = old.plain
        if let p = old.superview {
            p.insertSubview(t, aboveSubview: old)
            t.frame = old.frame
            old.removeFromSuperview()
        }
        n.view = t
        n.applyFont()
        n.applyColor()
        setAttribute(t, from: .unspecified, to: old.semanticContentAttribute, forced: [:], redo: false)
        n.applyTextAlign()
    }
}

extension RyUIRuntime: UITextViewDelegate {
    public func textViewDidChange(_ textView: UITextView) {
        guard let t = textView as? RyUITextView else { return }
        t.placeholderLabel.isHidden = !t.text.isEmpty
        let id = t.ryuiID, s = t.text ?? ""
        emit(id, .textChanged) { RyUIEvents.textChanged(id, s) }
        richChanged(t)
        t.revealCaret()
    }

    public func textViewDidChangeSelection(_ textView: UITextView) {
        guard let t = textView as? RyUITextView else { return }
        t.revealCaret()
        reportSelection(t.ryuiID)
        reportFormats(t)
    }

    public func textViewDidBeginEditing(_ textView: UITextView) {
        guard let t = textView as? RyUITextView else { return }
        let id = t.ryuiID
        emit(id, .focus) { RyUIEvents.focus(id) }
        // Once the keyboard's up and whatever holds this has made room for it.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { [weak t] in t?.revealCaret() }
    }

    public func textViewDidEndEditing(_ textView: UITextView) {
        guard let t = textView as? RyUITextView else { return }
        let id = t.ryuiID
        emit(id, .blur) { RyUIEvents.blur(id) }
    }
}
#endif
