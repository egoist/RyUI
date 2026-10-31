#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

func ryuiColor(_ v: UInt64) -> UIColor? {
    switch v >> 32 {
    case 1:
        // Its opacity (Color.Opacity) is in bits 24-31, as transparency.
        let a = 255 - CGFloat((v >> 24) & 0xff)
        let semantic = RyUISemanticColor(rawValue: Int32(truncatingIfNeeded: v & 0xffff))
        if a == 255 { return semanticColor(semantic) }
        // The tint color resolves against a view's tint, which a color
        // made from it can't: the app's accent, or UIKit's default tint.
        guard let c = semantic == .accent ? (ryuiAccent ?? .systemBlue) : semanticColor(semantic) else { return nil }
        // Each appearance's color, its alpha times a.
        return UIColor { t in
            let r = c.resolvedColor(with: t)
            return r.withAlphaComponent(r.cgColor.alpha * a / 255)
        }
    case 2:
        let c = UInt32(truncatingIfNeeded: v)
        return UIColor(red: CGFloat((c >> 24) & 0xff) / 255, green: CGFloat((c >> 16) & 0xff) / 255,
                       blue: CGFloat((c >> 8) & 0xff) / 255, alpha: CGFloat(c & 0xff) / 255)
    default:
        return nil
    }
}

/// The app's accent (the Accent command's), for colors made from it.
var ryuiAccent: UIColor?

func semanticColor(_ c: RyUISemanticColor?) -> UIColor? {
    switch c {
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
    case .secondaryGroupedBackground: return .secondarySystemGroupedBackground
    default: return nil
    }
}

func uiTextStyle(_ v: Int32) -> UIFont.TextStyle {
    switch RyUITextStyle(rawValue: v) {
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
    switch RyUIFontWeight(rawValue: v) {
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

@MainActor extension RyUINode {
    /// The font for the node's text props: a Dynamic Type text style,
    /// or a system font (or its own family's) scaled like one.
    var font: UIFont {
        let style = uiTextStyle(textStyle)
        let metrics = UIFontMetrics(forTextStyle: style)
        if !fontFamily.isEmpty {
            let base = fontSize > 0 ? CGFloat(fontSize)
                : UIFont.preferredFont(forTextStyle: style, compatibleWith: UITraitCollection(preferredContentSizeCategory: .large)).pointSize
            if let f = RyUIFonts.font(fontFamily, size: base, weight: uiWeight(fontWeight)) {
                return metrics.scaledFont(for: f)
            }
        }
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
        if let r = view as? RyUIRichTextView {
            r.base.textStyle = textStyle
            r.base.size = fontSize
            r.base.weight = fontWeight
            r.base.family = fontFamily
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
        case let t as RyUITextView:
            t.font = f
            t.adjustsFontForContentSizeCategory = true
        case let s as RyUISymbolView:
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
        let c = ryuiColor(color)
        switch view {
        case let l as UILabel: l.textColor = c ?? .label
        case let t as UITextField: t.textColor = c ?? .label
        case let t as RyUITextView: t.textColor = c ?? .label
        case let s as UISwitch: s.onTintColor = c
        case let a as UIActivityIndicatorView: a.color = c ?? .secondaryLabel
        case let p as UIProgressView: p.progressTintColor = c
        case let s as UISegmentedControl: s.selectedSegmentTintColor = c
        case let r as RyUIRichTextView:
            r.base.color = color
        case let s as RyUISymbolView: s.tintColor = c ?? .label
        case let i as UIImageView:
            i.tintColor = c
            i.image = i.image?.withRenderingMode(c != nil ? .alwaysTemplate : .automatic)
        default: view.tintColor = c
        }
    }
}

@MainActor extension RyUIRuntime {
    func setCommon(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) {
        let view = n.view
        switch prop {
        case RyUIProp.background:
            let c = ryuiColor(v.color)
            animated(view) { view.backgroundColor = c }
        case RyUIProp.cornerRadius:
            view.layer.cornerCurve = .continuous
            n.cornerRadius = CGFloat(v.float)
            let r = n.clampedRadius
            animated(view) { view.layer.cornerRadius = r }
            n.updateLayers()
        case RyUIProp.borderWidth: view.layer.borderWidth = CGFloat(v.float)
        case RyUIProp.borderColor:
            n.borderColor = v.color
            view.layer.borderColor = ryuiColor(v.color)?.resolvedColor(with: view.traitCollection).cgColor
        case RyUIProp.opacity:
            let a = CGFloat(v.float)
            n.opacity = a
            animated(view) { view.alpha = a }
        case RyUIProp.hidden: view.isHidden = v.bool
        case RyUIProp.a11yActions:
            guard case .bytes(let b) = v, b.count >= 4 else { return }
            var r = RyUIReader(b)
            let count = Int(r.u32())
            let id = n.id
            view.accessibilityCustomActions = (0..<count).map { i in
                UIAccessibilityCustomAction(name: r.string()) { [weak self] _ in
                    self?.emit(id, .a11yAction) { RyUIEvents.a11yAction(id, Int64(i)) }
                    return true
                }
            }
            if !view.isAccessibilityElement, view is RyUIContainerView { view.isAccessibilityElement = true }
        case RyUIProp.a11yAdjustable:
            (view as? RyUIContainerView)?.adjustable = v.bool
        case RyUIProp.a11yHidden:
            view.accessibilityElementsHidden = v.bool
            if v.bool { view.isAccessibilityElement = false }
        case RyUIProp.clip: view.clipsToBounds = v.bool
        case RyUIProp.transform:
            guard case .bytes(let b) = v, b.count >= 20 else { return }
            var r = RyUIReader(b)
            let tx = CGFloat(r.f32()), ty = CGFloat(r.f32()), sx = CGFloat(r.f32()), sy = CGFloat(r.f32())
            let rot = CGFloat(r.f32()) * .pi / 180
            let t = CGAffineTransform(translationX: tx, y: ty).rotated(by: rot).scaledBy(x: sx, y: sy)
            n.baseTransform = t
            animated(view) { view.transform = t }
        case RyUIProp.shadow:
            guard case .bytes(let b) = v, b.count >= 20 else { return }
            var r = RyUIReader(b)
            let c = ryuiColor(r.u64())
            let radius = CGFloat(r.f32()), x = CGFloat(r.f32()), y = CGFloat(r.f32())
            n.shadowColor = c
            view.layer.shadowColor = c?.resolvedColor(with: view.traitCollection).cgColor
            view.layer.shadowOpacity = c == nil ? 0 : 1
            view.layer.shadowRadius = radius / 2 // a blur radius, as CSS and SwiftUI mean it
            view.layer.shadowOffset = CGSize(width: x, height: y)
            n.updateLayers()
        case RyUIProp.material:
            n.setMaterial(RyUIMaterial(rawValue: v.enumValue) ?? .none)
        case RyUIProp.transition:
            guard case .bytes(let b) = v else { return }
            n.transition = RyUITransition(b)
        case RyUIProp.match:
            if let k = n.match, matchViews[k] === n { matchViews[k] = nil }
            n.match = v.string.isEmpty ? nil : v.string
            if let k = n.match { matchViews[k] = n }
        case RyUIProp.gradient:
            guard case .bytes(let b) = v else { return }
            n.setGradient(b)
        case RyUIProp.a11yLabel:
            view.isAccessibilityElement = true
            view.accessibilityLabel = v.string
        case RyUIProp.a11yHint:
            view.isAccessibilityElement = true
            view.accessibilityHint = v.string
        case RyUIProp.a11yRole:
            if let r = view as? RyUIRichTextView { // its own elements: the text, its links
                r.heading = RyUIRole(rawValue: v.enumValue) == .header
                return
            }
            view.isAccessibilityElement = true
            switch RyUIRole(rawValue: v.enumValue) {
            case .button: view.accessibilityTraits.insert(.button)
            case .header: view.accessibilityTraits.insert(.header)
            case .image: view.accessibilityTraits.insert(.image)
            case .link: view.accessibilityTraits.insert(.link)
            case .text: view.accessibilityTraits.insert(.staticText)
            case .search: view.accessibilityTraits.insert(.searchField)
            default: break
            }
        case RyUIProp.color:
            n.color = v.color
            n.applyColor()
        case RyUIProp.textStyle:
            n.textStyle = v.enumValue
            n.applyFont()
        case RyUIProp.fontSize:
            n.fontSize = Float(v.float)
            n.applyFont()
        case RyUIProp.fontWeight:
            n.fontWeight = v.enumValue
            n.applyFont()
        case RyUIProp.fontFamily:
            n.fontFamily = v.string
            if n.fontFamily.contains("://"), RyUIFonts.loaded == nil {
                RyUIFonts.loaded = { [weak self] url in
                    guard let self else { return }
                    RyUILayoutCache.shared.clear()
                    for m in self.nodes.values where m.fontFamily == url {
                        m.applyFont()
                        (m.view as? RyUIRichTextView)?.invalidate()
                    }
                    self.send(RyUIEvents.fontLoaded(self.rootID, url))
                }
            }
            n.applyFont()
        case RyUIProp.textAlign:
            let a: NSTextAlignment
            switch RyUITextAlign(rawValue: v.enumValue) {
            case .center: a = .center
            case .end: a = view.effectiveUserInterfaceLayoutDirection == .rightToLeft ? .left : .right
            case .justify: a = .justified
            default: a = .natural
            }
            (view as? UILabel)?.textAlignment = a
            (view as? UITextField)?.textAlignment = a
            (view as? UITextView)?.textAlignment = a
            (view as? RyUIRichTextView)?.base.align = v.enumValue
        case RyUIProp.lineLimit:
            (view as? UILabel)?.numberOfLines = Int(v.int)
            (view as? RyUIRichTextView)?.base.lines = Int(v.int)
            (view as? RyUITextView)?.maxLines = Int(v.int)
        case RyUIProp.disabled:
            (view as? UIControl)?.isEnabled = !v.bool
            (view as? RyUITextView)?.isEditable = !v.bool
        case RyUIProp.menu:
            if case .bytes(let b) = v { n.actions = decodeActions(b) }
            installMenu(n)
        case RyUIProp.swipeActions:
            guard case .bytes(let b) = v else { return }
            n.swipeActions = decodeActions(b)
        case RyUIProp.menuTrigger:
            n.menuTrigger = v.enumValue
            installMenu(n)
        default:
            break
        }
    }

    /// A glass button with an icon alone is a circle: as much room
    /// around the icon on every side.
    func roundGlass(_ b: UIButton) {
        guard glassButtons.contains(ObjectIdentifier(b)), var c = b.configuration else { return }
        let iconOnly = (c.title ?? "").isEmpty && c.image != nil
        let want = iconOnly ? NSDirectionalEdgeInsets(top: 11, leading: 11, bottom: 11, trailing: 11) : nil
        if let want, c.contentInsets != want {
            c.contentInsets = want
            b.configuration = c
        }
    }

    func setOwn(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) {
        switch (n.kind, prop) {
        case (.text, RyUIProp.Text.text):
            (n.view as? UILabel)?.text = v.string
        case (.button, RyUIProp.Button.title):
            guard let b = n.view as? UIButton else { return }
            var c = b.configuration ?? .plain()
            c.title = v.string
            b.configuration = c
            roundGlass(b)
        case (.button, RyUIProp.Button.variant):
            guard let b = n.view as? UIButton else { return }
            let old = b.configuration
            var c: UIButton.Configuration
            let variant = RyUIButtonVariant(rawValue: v.enumValue)
            switch variant {
            case .filled: c = .filled()
            case .tinted: c = .tinted()
            case .gray: c = .gray()
            case .bordered: c = .bordered()
            case .glass, .glassProminent:
                if #available(iOS 26.0, *) {
                    c = variant == .glass ? .glass() : .prominentGlass()
                } else {
                    c = variant == .glass ? .gray() : .filled()
                    c.cornerStyle = .capsule
                }
            default: c = .plain()
            }
            if variant == .glass || variant == .glassProminent {
                glassButtons.insert(ObjectIdentifier(b))
            } else {
                glassButtons.remove(ObjectIdentifier(b))
            }
            c.title = old?.title
            c.titleTextAttributesTransformer = old?.titleTextAttributesTransformer
            c.image = old?.image
            c.imagePadding = old?.imagePadding ?? 0
            c.preferredSymbolConfigurationForImage = old?.preferredSymbolConfigurationForImage
            b.configuration = c
            roundGlass(b)
        case (.button, RyUIProp.Button.icon):
            guard let b = n.view as? UIButton else { return }
            var c = b.configuration ?? .plain()
            c.image = v.string.isEmpty ? nil : UIImage(systemName: v.string)
            c.imagePadding = 6
            b.configuration = c
            roundGlass(b)
        case (.textField, RyUIProp.TextField.value):
            (n.view as? UITextField)?.text = v.string
            (n.view as? RyUITextView)?.text = v.string
        case (.textField, RyUIProp.TextField.placeholder):
            (n.view as? UITextField)?.placeholder = v.string
            (n.view as? RyUITextView)?.placeholder = v.string
        case (.textField, RyUIProp.TextField.multiline):
            if v.bool { makeMultiline(n) }
        case (.textField, RyUIProp.TextField.plain):
            (n.view as? RyUITextField)?.plain = v.bool
            (n.view as? RyUITextView)?.plain = v.bool
        case (.textField, RyUIProp.TextField.keyboard):
            let t: UIKeyboardType
            switch RyUIKeyboard(rawValue: v.enumValue) {
            case .number: t = .numberPad
            case .decimal: t = .decimalPad
            case .email: t = .emailAddress
            case .url: t = .URL
            case .phone: t = .phonePad
            default: t = .default
            }
            (n.view as? UITextField)?.keyboardType = t
            (n.view as? RyUITextView)?.keyboardType = t
        case (.textField, RyUIProp.TextField.returnKey):
            let k: UIReturnKeyType
            switch RyUIReturnKey(rawValue: v.enumValue) {
            case .done: k = .done
            case .go: k = .go
            case .next: k = .next
            case .search: k = .search
            case .send: k = .send
            default: k = .default
            }
            (n.view as? UITextField)?.returnKeyType = k
            (n.view as? RyUITextView)?.returnKeyType = k
        case (.textField, RyUIProp.TextField.secure):
            (n.view as? UITextField)?.isSecureTextEntry = v.bool
            (n.view as? RyUITextView)?.isSecureTextEntry = v.bool
        case (.textField, RyUIProp.TextField.autocapitalize):
            let a: UITextAutocapitalizationType
            switch RyUIAutocapitalize(rawValue: v.enumValue) {
            case .some(.none): a = .none
            case .words: a = .words
            case .characters: a = .allCharacters
            default: a = .sentences
            }
            (n.view as? UITextField)?.autocapitalizationType = a
            (n.view as? RyUITextView)?.autocapitalizationType = a
        case (.textField, RyUIProp.TextField.autocorrect):
            let a: UITextAutocorrectionType
            switch RyUIAutocorrect(rawValue: v.enumValue) {
            case .yes: a = .yes
            case .no: a = .no
            default: a = .default
            }
            (n.view as? UITextField)?.autocorrectionType = a
            (n.view as? RyUITextView)?.autocorrectionType = a
        case (.textField, RyUIProp.TextField.contentType):
            (n.view as? UITextField)?.textContentType = UITextContentType(rawValue: v.string)
            (n.view as? RyUITextView)?.textContentType = UITextContentType(rawValue: v.string)
        case (.toggle, RyUIProp.Toggle.on):
            (n.view as? UISwitch)?.setOn(v.bool, animated: n.view.window != nil)
        case (.slider, RyUIProp.Slider.value):
            (n.view as? UISlider)?.value = Float(v.float)
        case (.slider, RyUIProp.Slider.min):
            (n.view as? UISlider)?.minimumValue = Float(v.float)
        case (.slider, RyUIProp.Slider.max):
            (n.view as? UISlider)?.maximumValue = Float(v.float)
        case (.scroll, RyUIProp.Scroll.horizontal):
            guard let s = n.view as? UIScrollView else { return }
            s.alwaysBounceVertical = !v.bool
            s.alwaysBounceHorizontal = v.bool
        case (.activity, RyUIProp.Activity.animating):
            guard let a = n.view as? UIActivityIndicatorView else { return }
            if v.bool { a.startAnimating() } else { a.stopAnimating() }
        default:
            if !setShapeProp(n, prop, v), !setWheelProp(n, prop, v), !setMediaProp(n, prop, v) { setNavProp(n, prop, v) }
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
        case let t as RyUITextView:
            // As wide as a field would be; as tall as its text, to its limit.
            let width = w.isNaN ? (aw.isFinite ? CGFloat(max(aw, 0)) : 200) : CGFloat(w)
            s = CGSize(width: width, height: t.height(at: width))
        case is UITextField:
            // A text field scrolls its text: it can be narrow.
            s = n.view.sizeThatFits(CGSize(width: big, height: big))
            s.width = max(s.width, 120)
            if aw == -.infinity { s.width = 80 }
            if aw.isFinite { s.width = min(s.width, CGFloat(max(aw, 0))) }
        case let r as RyUIRichTextView:
            s = r.measure(w, aw)
        case is RyUIWheelView:
            // As wide as it's let be; as tall as UIKit makes a wheel.
            s = CGSize(width: w.isNaN ? (aw.isFinite ? CGFloat(max(aw, 0)) : 320) : CGFloat(w), height: 216)
        case let p as UIProgressView:
            // As wide as it's let be; as tall as UIKit makes it.
            s = CGSize(width: w.isNaN ? (aw.isFinite ? CGFloat(max(aw, 0)) : 100) : CGFloat(w), height: p.intrinsicContentSize.height)
        case let sym as RyUISymbolView:
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
