#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A Rich TextField: its text view's attributed text, from and to the
/// core's styled text (RichText's Content encoding). Bold, italic,
/// underline, strikethrough, monospaced and links are what the text has
/// (the user's B/I/U menu, the Format and SetLink commands change them);
/// each span's own style (its text style, size, colors) rides along in an
/// attribute of its own, so what the user didn't change comes back as it
/// was sent.

extension NSAttributedString.Key {
    static let ryuiFieldStyle = NSAttributedString.Key("ryuiFieldStyle")
}

/// A span's style as the core sent it, kept on its text.
final class RyUIFieldStyle: NSObject {
    let style: RyUIRichStyle
    init(_ s: RyUIRichStyle) { style = s }
    override func isEqual(_ object: Any?) -> Bool {
        guard let o = object as? RyUIFieldStyle else { return false }
        return o.style == style
    }
    override var hash: Int { Int(style.color &+ style.background) &+ Int(style.textStyle) }
}

extension RyUIRichStyle: Equatable {
    static func == (a: RyUIRichStyle, b: RyUIRichStyle) -> Bool {
        a.textStyle == b.textStyle && a.size == b.size && a.weight == b.weight && a.flags == b.flags &&
            a.color == b.color && a.background == b.background && a.link == b.link
    }
}

/// TextFormat bits (ops.TextFormat): the Formats event's and the Format command's.
struct RyUIFormat: OptionSet {
    let rawValue: UInt8
    static let bold = RyUIFormat(rawValue: 1)
    static let italic = RyUIFormat(rawValue: 2)
    static let underline = RyUIFormat(rawValue: 4)
    static let strike = RyUIFormat(rawValue: 8)
    static let mono = RyUIFormat(rawValue: 16)
}

private let semibold: Int32 = 6, bold: Int32 = 7

@MainActor extension RyUIRuntime {
    /// The field's own font and color: what a span's zero fields are.
    private func richBase(_ n: RyUINode) -> (UIFont, UIColor) {
        (n.font, ryuiColor(n.color) ?? .label)
    }

    /// The attributes of a span's style in the field.
    private func attributes(_ s: RyUIRichStyle, _ n: RyUINode) -> [NSAttributedString.Key: Any] {
        let (baseFont, baseColor) = richBase(n)
        var font = s.textStyle != 0 || s.size > 0 || s.mono
            ? ryuiFont(s.textStyle != 0 ? s.textStyle : n.textStyle, s.size > 0 ? s.size : n.fontSize, n.fontWeight, family: n.fontFamily, mono: s.mono)
            : baseFont
        var traits = font.fontDescriptor.symbolicTraits
        if s.weight >= semibold { traits.insert(.traitBold) }
        if s.italic { traits.insert(.traitItalic) }
        if traits != font.fontDescriptor.symbolicTraits, let d = font.fontDescriptor.withSymbolicTraits(traits) {
            font = UIFont(descriptor: d, size: font.pointSize)
        }
        var a: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: ryuiColor(s.color) ?? baseColor,
                                                .ryuiFieldStyle: RyUIFieldStyle(s)]
        if s.background != 0, let bg = ryuiColor(s.background) { a[.backgroundColor] = bg }
        if s.underline { a[.underlineStyle] = NSUnderlineStyle.single.rawValue }
        if s.strike { a[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
        if !s.link.isEmpty { a[.link] = URL(string: s.link) ?? s.link }
        return a
    }

    /// The style of text with these attributes: the core's as sent, with
    /// what the user changed.
    private func style(_ a: [NSAttributedString.Key: Any], _ n: RyUINode?) -> RyUIRichStyle {
        var s = (a[.ryuiFieldStyle] as? RyUIFieldStyle)?.style ?? RyUIRichStyle()
        let traits = (a[.font] as? UIFont)?.fontDescriptor.symbolicTraits ?? []
        // What the field's own font has (a headline's bold) isn't the
        // run's: the run keeps what it was sent with for it.
        let base = n.map { richBase($0).0.fontDescriptor.symbolicTraits } ?? []
        if !base.contains(.traitBold) {
            if traits.contains(.traitBold) {
                if s.weight < semibold { s.weight = bold }
            } else if s.weight >= semibold {
                s.weight = 0
            }
        }
        var flags: UInt8 = s.flags & (base.contains(.traitItalic) ? 1 : 0) | s.flags & (base.contains(.traitMonoSpace) ? 2 : 0)
        if !base.contains(.traitItalic), traits.contains(.traitItalic) { flags |= 1 }
        if !base.contains(.traitMonoSpace), traits.contains(.traitMonoSpace) { flags |= 2 }
        if let u = a[.underlineStyle] as? Int, u != 0 { flags |= 4 }
        if let u = a[.strikethroughStyle] as? Int, u != 0 { flags |= 8 }
        s.flags = flags
        switch a[.link] {
        case let u as URL: s.link = u.absoluteString
        case let u as String: s.link = u
        default: s.link = ""
        }
        return s
    }

    /// The Rich prop: the field's text, styled.
    func setRich(_ n: RyUINode, _ b: UnsafeRawBufferPointer) {
        guard let t = n.view as? RyUITextView else { return }
        t.rich = true
        t.allowsEditingTextAttributes = true
        var r = RyUIReader(b)
        var styles: [RyUIRichStyle] = []
        let out = NSMutableAttributedString()
        if b.count >= 8 {
            let count = Int(r.u32())
            for _ in 0..<count {
                var s = RyUIRichStyle()
                s.textStyle = Int32(r.u8())
                s.size = r.f32()
                s.weight = Int32(r.u8())
                s.flags = r.u8()
                s.color = r.u64()
                s.background = r.u64()
                s.link = r.string()
                styles.append(s)
            }
            let paragraphs = Int(r.u32())
            var last = RyUIRichStyle()
            for p in 0..<paragraphs {
                if p > 0 { out.append(NSAttributedString(string: "\n", attributes: attributes(last, n))) }
                let runs = Int(r.u32())
                for _ in 0..<runs {
                    let i = Int(r.u32())
                    let text = r.string()
                    guard i < styles.count, !styles[i].inline else { continue }
                    last = styles[i]
                    out.append(NSAttributedString(string: text, attributes: attributes(styles[i], n)))
                }
            }
        }
        let sel = t.selectedRange
        t.settingRich = true
        t.attributedText = out
        t.placeholderLabel.isHidden = out.length > 0
        let end = min(sel.location, out.length)
        t.selectedRange = NSRange(location: end, length: min(sel.length, out.length - end))
        if out.length == 0 { t.typingAttributes = attributes(RyUIRichStyle(), n) }
        t.settingRich = false
        reportFormats(t)
    }

    /// The field's styled text, as the core's encoding has it: styles in
    /// the order its runs first use them, a paragraph per line, runs of
    /// one style merged.
    func encodeRich(_ a: NSAttributedString, _ n: RyUINode?) -> [UInt8] {
        let s = a.string as NSString
        var styles: [RyUIRichStyle] = []
        var paragraphs: [[(Int, String)]] = [[]]
        var at = 0
        while at <= s.length {
            let line = s.range(of: "\n", options: [], range: NSRange(location: at, length: s.length - at))
            let end = line.location == NSNotFound ? s.length : line.location
            var runs: [(Int, String)] = []
            if end > at {
                a.enumerateAttributes(in: NSRange(location: at, length: end - at), options: []) { attrs, range, _ in
                    let st = style(attrs, n)
                    let text = s.substring(with: range)
                    let i: Int
                    if let k = styles.firstIndex(of: st) { i = k } else { styles.append(st); i = styles.count - 1 }
                    if let last = runs.last, last.0 == i {
                        runs[runs.count - 1].1 += text
                    } else {
                        runs.append((i, text))
                    }
                }
            }
            paragraphs[paragraphs.count - 1] = runs
            if line.location == NSNotFound { break }
            paragraphs.append([])
            at = end + 1
        }
        // Styles in the order runs first use them (a run's style may come
        // before it was merged into the table's order above: renumber).
        var order: [Int] = []
        for p in paragraphs { for r in p where !order.contains(r.0) { order.append(r.0) } }
        var w = RyUIWriter()
        w.u32(UInt32(order.count))
        for i in order {
            let st = styles[i]
            w.u8(UInt8(clamping: st.textStyle))
            w.f32(st.size)
            w.u8(UInt8(clamping: st.weight))
            w.u8(st.flags)
            w.u64(st.color)
            w.u64(st.background)
            w.string(st.link)
        }
        w.u32(UInt32(paragraphs.count))
        for p in paragraphs {
            w.u32(UInt32(p.count))
            for r in p {
                w.u32(UInt32(order.firstIndex(of: r.0)!))
                w.string(r.1)
            }
        }
        return w.bytes
    }

    /// The user changed the text or its styles.
    func richChanged(_ t: RyUITextView) {
        guard t.rich, !t.settingRich else { return }
        let id = t.ryuiID
        let b = encodeRich(t.attributedText, nodes[id])
        emit(id, .richChanged) { RyUIEvents.richChanged(id, b) }
        reportFormats(t)
    }

    /// The styles at the cursor (what's typed next) or that all the
    /// selection has, and its link, if they changed.
    func reportFormats(_ t: RyUITextView) {
        guard t.rich, nodes[t.ryuiID]?.listens.contains(RyUIEventKind.formats.rawValue) == true else { return }
        let (f, link) = formats(t)
        guard f != t.lastFormats || link != t.lastLink else { return }
        t.lastFormats = f
        t.lastLink = link
        var w = RyUIWriter()
        w.u8(f.rawValue)
        w.string(link)
        let id = t.ryuiID, b = w.bytes
        emit(id, .formats) { RyUIEvents.formats(id, b) }
    }

    private func formatsOf(_ s: RyUIRichStyle) -> RyUIFormat {
        var f: RyUIFormat = []
        if s.weight >= semibold { f.insert(.bold) }
        if s.italic { f.insert(.italic) }
        if s.underline { f.insert(.underline) }
        if s.strike { f.insert(.strike) }
        if s.mono { f.insert(.mono) }
        return f
    }

    private func formats(_ t: RyUITextView) -> (RyUIFormat, String) {
        let n = nodes[t.ryuiID]
        let r = t.selectedRange
        if r.length == 0 {
            let s = style(t.typingAttributes, n)
            return (formatsOf(s), s.link)
        }
        var all: RyUIFormat = [.bold, .italic, .underline, .strike, .mono]
        var link: String?
        t.attributedText.enumerateAttributes(in: r, options: []) { a, _, _ in
            let s = style(a, n)
            all = all.intersection(formatsOf(s))
            if link == nil { link = s.link } else if link != s.link { link = "" }
        }
        return (all, link ?? "")
    }

    /// The Format command: styles on, off, or toggled, for the selection,
    /// or for what's typed next at the cursor. Undoable.
    func format(_ n: RyUINode, _ bits: UInt8, _ how: UInt8) {
        guard let t = n.view as? RyUITextView, t.rich else { return }
        let f = RyUIFormat(rawValue: bits)
        let r = t.selectedRange
        let (has, _) = formats(t)
        let on = how == 2 ? !has.isSuperset(of: f) : how == 1
        if r.length == 0 {
            var s = style(t.typingAttributes, n)
            apply(f, on, to: &s)
            t.typingAttributes = attributes(s, n)
            reportFormats(t)
            return
        }
        edit(t) { storage in
            storage.enumerateAttributes(in: r, options: []) { a, range, _ in
                var s = style(a, n)
                apply(f, on, to: &s)
                storage.setAttributes(attributes(s, n), range: range)
            }
        }
    }

    private func apply(_ f: RyUIFormat, _ on: Bool, to s: inout RyUIRichStyle) {
        if f.contains(.bold) { s.weight = on ? max(s.weight, bold) : (s.weight >= semibold ? 0 : s.weight) }
        var flags = s.flags
        for (bit, flag) in [(RyUIFormat.italic, UInt8(1)), (.mono, 2), (.underline, 4), (.strike, 8)] where f.contains(bit) {
            if on { flags |= flag } else { flags &= ~flag }
        }
        s.flags = flags
    }

    /// The SetLink command: the selection links to url ("" none); at the
    /// cursor, the link it's in. Undoable.
    func setLink(_ n: RyUINode, _ url: String) {
        guard let t = n.view as? RyUITextView, t.rich else { return }
        var r = t.selectedRange
        if r.length == 0 {
            guard r.location <= t.attributedText.length, t.attributedText.length > 0 else { return }
            var effective = NSRange()
            let at = min(r.location, t.attributedText.length - 1)
            guard t.attributedText.attribute(.link, at: at, effectiveRange: &effective) != nil else { return }
            r = effective
        }
        edit(t) { storage in
            storage.enumerateAttributes(in: r, options: []) { a, range, _ in
                var s = style(a, n)
                s.link = url
                storage.setAttributes(attributes(s, n), range: range)
            }
        }
    }

    /// Changes the text's attributes undoably, then reports them.
    private func edit(_ t: RyUITextView, _ change: (NSTextStorage) -> Void) {
        let before = NSAttributedString(attributedString: t.attributedText)
        let sel = t.selectedRange
        t.undoManager?.registerUndo(withTarget: t) { [weak self] tv in
            tv.attributedText = before
            tv.selectedRange = sel
            self?.richChanged(tv)
        }
        t.textStorage.beginEditing()
        change(t.textStorage)
        t.textStorage.endEditing()
        t.selectedRange = sel
        richChanged(t)
    }
}
#endif
