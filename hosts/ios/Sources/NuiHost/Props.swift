#if canImport(UIKit)
import UIKit
#if canImport(NuiOps)
import NuiOps
#endif

func nuiColor(_ v: UInt64) -> UIColor? {
    switch v >> 32 {
    case 1:
        switch NuiSemanticColor(rawValue: Int32(truncatingIfNeeded: v & 0xffff_ffff)) {
        case .label: return .label
        case .secondaryLabel: return .secondaryLabel
        case .tertiaryLabel: return .tertiaryLabel
        case .background: return .systemBackground
        case .secondaryBackground: return .secondarySystemBackground
        case .groupedBackground: return .systemGroupedBackground
        case .separator: return .separator
        case .accent: return .tintColor
        case .red: return .systemRed
        case .orange: return .systemOrange
        case .yellow: return .systemYellow
        case .green: return .systemGreen
        case .blue: return .systemBlue
        case .purple: return .systemPurple
        case .pink: return .systemPink
        case .gray: return .systemGray
        case .clear: return .clear
        default: return nil
        }
    case 2:
        let c = UInt32(truncatingIfNeeded: v)
        return UIColor(red: CGFloat((c >> 24) & 0xff) / 255, green: CGFloat((c >> 16) & 0xff) / 255,
                       blue: CGFloat((c >> 8) & 0xff) / 255, alpha: CGFloat(c & 0xff) / 255)
    default:
        return nil
    }
}

func uiTextStyle(_ v: Int32) -> UIFont.TextStyle {
    switch NuiTextStyle(rawValue: v) {
    case .largeTitle: return .largeTitle
    case .title: return .title1
    case .title2: return .title2
    case .title3: return .title3
    case .headline: return .headline
    case .subheadline: return .subheadline
    case .callout: return .callout
    case .footnote: return .footnote
    case .caption: return .caption1
    case .caption2: return .caption2
    default: return .body
    }
}

func uiWeight(_ v: Int32) -> UIFont.Weight? {
    switch NuiFontWeight(rawValue: v) {
    case .ultralight: return .ultraLight
    case .thin: return .thin
    case .light: return .light
    case .regular: return .regular
    case .medium: return .medium
    case .semibold: return .semibold
    case .bold: return .bold
    case .heavy: return .heavy
    case .black: return .black
    default: return nil
    }
}

@MainActor extension NuiNode {
    /// The font for the node's text props: a Dynamic Type text style,
    /// or a system font scaled like one.
    var font: UIFont {
        let style = uiTextStyle(textStyle)
        let metrics = UIFontMetrics(forTextStyle: style)
        if fontSize > 0 {
            return metrics.scaledFont(for: .systemFont(ofSize: CGFloat(fontSize), weight: uiWeight(fontWeight) ?? .regular))
        }
        if let w = uiWeight(fontWeight) {
            let base = UIFont.preferredFont(forTextStyle: style, compatibleWith: UITraitCollection(preferredContentSizeCategory: .large))
            return metrics.scaledFont(for: .systemFont(ofSize: base.pointSize, weight: w))
        }
        return .preferredFont(forTextStyle: style)
    }

    func applyFont() {
        if let r = view as? NuiRichTextView {
            r.base.textStyle = textStyle
            r.base.size = fontSize
            r.base.weight = fontWeight
            return
        }
        let f = font
        switch view {
        case let l as UILabel:
            l.font = f
            l.adjustsFontForContentSizeCategory = true
        case let t as UITextField:
            t.font = f
            t.adjustsFontForContentSizeCategory = true
        case let t as NuiTextView:
            t.font = f
            t.adjustsFontForContentSizeCategory = true
        case let s as NuiSymbolView:
            s.node = self
            s.refresh()
        case let b as UIButton:
            var c = b.configuration ?? .plain()
            c.preferredSymbolConfigurationForImage = UIImage.SymbolConfiguration(font: f, scale: .medium)
            c.titleTextAttributesTransformer = UIConfigurationTextAttributesTransformer { a in
                var a = a
                a.font = f
                return a
            }
            b.configuration = c
        default:
            break
        }
    }

    func applyColor() {
        let c = nuiColor(color)
        switch view {
        case let l as UILabel: l.textColor = c ?? .label
        case let t as UITextField: t.textColor = c ?? .label
        case let t as NuiTextView: t.textColor = c ?? .label
        case let s as UISwitch: s.onTintColor = c
        case let a as UIActivityIndicatorView: a.color = c ?? .secondaryLabel
        case let p as UIProgressView: p.progressTintColor = c
        case let s as UISegmentedControl: s.selectedSegmentTintColor = c
        case let r as NuiRichTextView:
            r.base.color = color
        case let s as NuiSymbolView: s.tintColor = c ?? .label
        case let i as UIImageView:
            i.tintColor = c
            i.image = i.image?.withRenderingMode(c != nil ? .alwaysTemplate : .automatic)
        default: view.tintColor = c
        }
    }
}

@MainActor extension NuiRuntime {
    func setCommon(_ n: NuiNode, _ prop: UInt16, _ v: NuiValue) {
        let view = n.view
        switch prop {
        case NuiProp.background:
            let c = nuiColor(v.color)
            animated(view) { view.backgroundColor = c }
        case NuiProp.cornerRadius:
            view.layer.cornerCurve = .continuous
            let r = CGFloat(v.float)
            animated(view) { view.layer.cornerRadius = r }
            n.updateLayers()
        case NuiProp.borderWidth: view.layer.borderWidth = CGFloat(v.float)
        case NuiProp.borderColor:
            n.borderColor = v.color
            view.layer.borderColor = nuiColor(v.color)?.resolvedColor(with: view.traitCollection).cgColor
        case NuiProp.opacity:
            let a = CGFloat(v.float)
            n.opacity = a
            animated(view) { view.alpha = a }
        case NuiProp.hidden: view.isHidden = v.bool
        case NuiProp.clip: view.clipsToBounds = v.bool
        case NuiProp.transform:
            guard case .bytes(let b) = v, b.count >= 20 else { return }
            var r = NuiReader(b)
            let tx = CGFloat(r.f32()), ty = CGFloat(r.f32()), sx = CGFloat(r.f32()), sy = CGFloat(r.f32())
            let rot = CGFloat(r.f32()) * .pi / 180
            let t = CGAffineTransform(translationX: tx, y: ty).rotated(by: rot).scaledBy(x: sx, y: sy)
            n.baseTransform = t
            animated(view) { view.transform = t }
        case NuiProp.shadow:
            guard case .bytes(let b) = v, b.count >= 20 else { return }
            var r = NuiReader(b)
            let c = nuiColor(r.u64())
            let radius = CGFloat(r.f32()), x = CGFloat(r.f32()), y = CGFloat(r.f32())
            n.shadowColor = c
            view.layer.shadowColor = c?.resolvedColor(with: view.traitCollection).cgColor
            view.layer.shadowOpacity = c == nil ? 0 : 1
            view.layer.shadowRadius = radius / 2 // a blur radius, as CSS and SwiftUI mean it
            view.layer.shadowOffset = CGSize(width: x, height: y)
            n.updateLayers()
        case NuiProp.material:
            n.setMaterial(NuiMaterial(rawValue: v.enumValue) ?? .none)
        case NuiProp.transition:
            guard case .bytes(let b) = v else { return }
            n.transition = NuiTransition(b)
        case NuiProp.gradient:
            guard case .bytes(let b) = v else { return }
            n.setGradient(b)
        case NuiProp.a11yLabel:
            view.isAccessibilityElement = true
            view.accessibilityLabel = v.string
        case NuiProp.a11yHint:
            view.isAccessibilityElement = true
            view.accessibilityHint = v.string
        case NuiProp.a11yRole:
            view.isAccessibilityElement = true
            switch NuiRole(rawValue: v.enumValue) {
            case .button: view.accessibilityTraits.insert(.button)
            case .header: view.accessibilityTraits.insert(.header)
            case .image: view.accessibilityTraits.insert(.image)
            case .link: view.accessibilityTraits.insert(.link)
            case .text: view.accessibilityTraits.insert(.staticText)
            case .search: view.accessibilityTraits.insert(.searchField)
            default: break
            }
        case NuiProp.color:
            n.color = v.color
            n.applyColor()
        case NuiProp.textStyle:
            n.textStyle = v.enumValue
            n.applyFont()
        case NuiProp.fontSize:
            n.fontSize = Float(v.float)
            n.applyFont()
        case NuiProp.fontWeight:
            n.fontWeight = v.enumValue
            n.applyFont()
        case NuiProp.textAlign:
            let a: NSTextAlignment
            switch NuiTextAlign(rawValue: v.enumValue) {
            case .center: a = .center
            case .end: a = view.effectiveUserInterfaceLayoutDirection == .rightToLeft ? .left : .right
            case .justify: a = .justified
            default: a = .natural
            }
            (view as? UILabel)?.textAlignment = a
            (view as? UITextField)?.textAlignment = a
            (view as? UITextView)?.textAlignment = a
            (view as? NuiRichTextView)?.base.align = v.enumValue
        case NuiProp.lineLimit:
            (view as? UILabel)?.numberOfLines = Int(v.int)
            (view as? NuiRichTextView)?.base.lines = Int(v.int)
            (view as? NuiTextView)?.maxLines = Int(v.int)
        case NuiProp.disabled:
            (view as? UIControl)?.isEnabled = !v.bool
            (view as? NuiTextView)?.isEditable = !v.bool
        case NuiProp.menu:
            if case .bytes(let b) = v { n.actions = decodeActions(b) }
            installMenu(n)
        case NuiProp.swipeActions:
            guard case .bytes(let b) = v else { return }
            n.swipeActions = decodeActions(b)
        case NuiProp.menuTrigger:
            n.menuTrigger = v.enumValue
            installMenu(n)
        default:
            break
        }
    }

    func setOwn(_ n: NuiNode, _ prop: UInt16, _ v: NuiValue) {
        switch (n.kind, prop) {
        case (.text, NuiProp.Text.text):
            (n.view as? UILabel)?.text = v.string
        case (.button, NuiProp.Button.title):
            guard let b = n.view as? UIButton else { return }
            var c = b.configuration ?? .plain()
            c.title = v.string
            b.configuration = c
        case (.button, NuiProp.Button.variant):
            guard let b = n.view as? UIButton else { return }
            let old = b.configuration
            var c: UIButton.Configuration
            switch NuiButtonVariant(rawValue: v.enumValue) {
            case .filled: c = .filled()
            case .tinted: c = .tinted()
            case .gray: c = .gray()
            case .bordered: c = .bordered()
            default: c = .plain()
            }
            c.title = old?.title
            c.titleTextAttributesTransformer = old?.titleTextAttributesTransformer
            c.image = old?.image
            c.imagePadding = old?.imagePadding ?? 0
            c.preferredSymbolConfigurationForImage = old?.preferredSymbolConfigurationForImage
            b.configuration = c
        case (.button, NuiProp.Button.icon):
            guard let b = n.view as? UIButton else { return }
            var c = b.configuration ?? .plain()
            c.image = v.string.isEmpty ? nil : UIImage(systemName: v.string)
            c.imagePadding = 6
            b.configuration = c
        case (.textField, NuiProp.TextField.value):
            (n.view as? UITextField)?.text = v.string
            (n.view as? NuiTextView)?.text = v.string
        case (.textField, NuiProp.TextField.placeholder):
            (n.view as? UITextField)?.placeholder = v.string
            (n.view as? NuiTextView)?.placeholder = v.string
        case (.textField, NuiProp.TextField.multiline):
            if v.bool { makeMultiline(n) }
        case (.textField, NuiProp.TextField.keyboard):
            let t: UIKeyboardType
            switch NuiKeyboard(rawValue: v.enumValue) {
            case .number: t = .numberPad
            case .decimal: t = .decimalPad
            case .email: t = .emailAddress
            case .url: t = .URL
            case .phone: t = .phonePad
            default: t = .default
            }
            (n.view as? UITextField)?.keyboardType = t
            (n.view as? NuiTextView)?.keyboardType = t
        case (.textField, NuiProp.TextField.returnKey):
            let k: UIReturnKeyType
            switch NuiReturnKey(rawValue: v.enumValue) {
            case .done: k = .done
            case .go: k = .go
            case .next: k = .next
            case .search: k = .search
            case .send: k = .send
            default: k = .default
            }
            (n.view as? UITextField)?.returnKeyType = k
            (n.view as? NuiTextView)?.returnKeyType = k
        case (.textField, NuiProp.TextField.secure):
            (n.view as? UITextField)?.isSecureTextEntry = v.bool
            (n.view as? NuiTextView)?.isSecureTextEntry = v.bool
        case (.textField, NuiProp.TextField.autocapitalize):
            let a: UITextAutocapitalizationType
            switch NuiAutocapitalize(rawValue: v.enumValue) {
            case .some(.none): a = .none
            case .words: a = .words
            case .characters: a = .allCharacters
            default: a = .sentences
            }
            (n.view as? UITextField)?.autocapitalizationType = a
            (n.view as? NuiTextView)?.autocapitalizationType = a
        case (.textField, NuiProp.TextField.autocorrect):
            let a: UITextAutocorrectionType
            switch NuiAutocorrect(rawValue: v.enumValue) {
            case .yes: a = .yes
            case .no: a = .no
            default: a = .default
            }
            (n.view as? UITextField)?.autocorrectionType = a
            (n.view as? NuiTextView)?.autocorrectionType = a
        case (.textField, NuiProp.TextField.contentType):
            (n.view as? UITextField)?.textContentType = UITextContentType(rawValue: v.string)
            (n.view as? NuiTextView)?.textContentType = UITextContentType(rawValue: v.string)
        case (.toggle, NuiProp.Toggle.on):
            (n.view as? UISwitch)?.setOn(v.bool, animated: n.view.window != nil)
        case (.slider, NuiProp.Slider.value):
            (n.view as? UISlider)?.value = Float(v.float)
        case (.slider, NuiProp.Slider.min):
            (n.view as? UISlider)?.minimumValue = Float(v.float)
        case (.slider, NuiProp.Slider.max):
            (n.view as? UISlider)?.maximumValue = Float(v.float)
        case (.scroll, NuiProp.Scroll.horizontal):
            guard let s = n.view as? UIScrollView else { return }
            s.alwaysBounceVertical = !v.bool
            s.alwaysBounceHorizontal = v.bool
        case (.activity, NuiProp.Activity.animating):
            guard let a = n.view as? UIActivityIndicatorView else { return }
            if v.bool { a.startAnimating() } else { a.stopAnimating() }
        default:
            if !setMediaProp(n, prop, v) { setNavProp(n, prop, v) }
        }
    }

    // MARK: Measuring

    func measure(_ id: UInt32, _ w: Float, _ h: Float, _ aw: Float, _ ah: Float) -> CGSize {
        guard let n = nodes[id] else { return .zero }
        let big = CGFloat.greatestFiniteMagnitude
        func fit(_ known: Float, _ avail: Float, minContent: CGFloat) -> CGFloat {
            if !known.isNaN { return CGFloat(known) }
            if avail == .infinity { return big }
            if avail == -.infinity { return minContent }
            return CGFloat(max(avail, 0))
        }
        var s: CGSize
        switch n.view {
        case let l as UILabel:
            // Min-content: the widest word.
            var widest: CGFloat = 0
            if aw == -.infinity, w.isNaN, let text = l.text {
                let attrs: [NSAttributedString.Key: Any] = [.font: l.font as Any]
                for word in text.split(whereSeparator: { $0.isWhitespace }) {
                    widest = max(widest, (String(word) as NSString).size(withAttributes: attrs).width)
                }
            }
            let fw = fit(w, aw, minContent: ceil(widest))
            s = l.sizeThatFits(CGSize(width: fw, height: big))
            if fw < big { s.width = min(s.width, fw) }
            if l.text?.isEmpty ?? true { s = .zero }
        case is UISlider:
            s = n.view.sizeThatFits(CGSize(width: big, height: big))
            s.width = w.isNaN ? (aw.isFinite ? CGFloat(aw) : 200) : CGFloat(w)
        case let t as NuiTextView:
            // As wide as a field would be; as tall as its text, to its limit.
            let width = w.isNaN ? (aw.isFinite ? CGFloat(max(aw, 0)) : 200) : CGFloat(w)
            s = CGSize(width: width, height: t.height(at: width))
        case is UITextField:
            // A text field scrolls its text: it can be narrow.
            s = n.view.sizeThatFits(CGSize(width: big, height: big))
            s.width = max(s.width, 120)
            if aw == -.infinity { s.width = 80 }
            if aw.isFinite { s.width = min(s.width, CGFloat(max(aw, 0))) }
        case let r as NuiRichTextView:
            s = r.measure(w, aw)
        case let p as UIProgressView:
            // As wide as it's let be; as tall as UIKit makes it.
            s = CGSize(width: w.isNaN ? (aw.isFinite ? CGFloat(max(aw, 0)) : 100) : CGFloat(w), height: p.intrinsicContentSize.height)
        case let sym as NuiSymbolView:
            sym.refresh()
            s = sym.image?.size ?? .zero
        case let b as UIButton:
            // A configured button applies a new Dynamic Type size on its
            // next configuration update: apply it before measuring.
            b.updateConfiguration()
            b.layoutIfNeeded()
            s = b.sizeThatFits(CGSize(width: fit(w, aw, minContent: big), height: fit(h, ah, minContent: big)))
        default:
            s = n.view.sizeThatFits(CGSize(width: fit(w, aw, minContent: big), height: fit(h, ah, minContent: big)))
        }
        if !w.isNaN { s.width = CGFloat(w) }
        if !h.isNaN { s.height = CGFloat(h) }
        let k = scale
        return CGSize(width: ceil(s.width * k) / k, height: ceil(s.height * k) / k)
    }
}
#endif
