#if canImport(UIKit)
import UIKit
import CoreText
#if canImport(NuiOps)
import NuiOps
#endif

/// A span's look (see ops RichText.Content); zero fields are the view's.
struct NuiRichStyle {
    var textStyle: Int32 = 0
    var size: Float = 0
    var weight: Int32 = 0
    var flags: UInt8 = 0
    var color: UInt64 = 0
    var background: UInt64 = 0
    var link = ""
    var italic: Bool { flags & 1 != 0 }
    var mono: Bool { flags & 2 != 0 }
    var underline: Bool { flags & 4 != 0 }
    var strike: Bool { flags & 8 != 0 }
}

/// A paragraph: its runs, and a key for its layouts (its runs and the
/// styles they use, as bytes).
struct NuiRichParagraph {
    var runs: [(style: Int, text: String)] = []
    var key = Data()
}

/// The view's own text props: the spans' defaults.
struct NuiRichBase: Hashable {
    var textStyle: Int32 = 0
    var size: Float = 0
    var weight: Int32 = 0
    var color: UInt64 = 0
    var align: Int32 = 0
    var lines = 0
}

/// A font from text props: a Dynamic Type text style, or a system font
/// scaled like one; italic and monospaced as asked.
func nuiFont(_ textStyle: Int32, _ size: Float, _ weight: Int32, italic: Bool = false, mono: Bool = false) -> UIFont {
    let style = uiTextStyle(textStyle)
    let metrics = UIFontMetrics(forTextStyle: style)
    let w = uiWeight(weight)
    var f: UIFont
    let base = size > 0 ? CGFloat(size)
        : UIFont.preferredFont(forTextStyle: style, compatibleWith: UITraitCollection(preferredContentSizeCategory: .large)).pointSize
    let t = UITraitCollection.current
    if mono {
        f = metrics.scaledFont(for: .monospacedSystemFont(ofSize: base * 0.94, weight: w ?? .regular), compatibleWith: t)
    } else if size > 0 || w != nil {
        f = metrics.scaledFont(for: .systemFont(ofSize: base, weight: w ?? .regular), compatibleWith: t)
    } else {
        f = .preferredFont(forTextStyle: style, compatibleWith: t)
    }
    if italic, let d = f.fontDescriptor.withSymbolicTraits(f.fontDescriptor.symbolicTraits.union(.traitItalic)) {
        f = UIFont(descriptor: d, size: f.pointSize)
    }
    return f
}

private extension NSAttributedString.Key {
    static let nuiBackground = NSAttributedString.Key("nuiBackground")
    static let nuiLink = NSAttributedString.Key("nuiLink")
    static let nuiUnderline = NSAttributedString.Key("nuiUnderline")
    static let nuiStrike = NSAttributedString.Key("nuiStrike")
    static let ctForeground = NSAttributedString.Key(kCTForegroundColorAttributeName as String)
}

/// A paragraph typeset at a width: its lines, top to bottom.
final class NuiParagraphLayout {
    struct Line {
        var line: CTLine
        var baseline: CGFloat // from the paragraph's top
        var ascent: CGFloat, descent: CGFloat
        var width: CGFloat
        var range: CFRange
    }
    var lines: [Line] = []
    var height: CGFloat = 0
    var width: CGFloat = 0 // the widest line's
    let string: NSAttributedString

    init(_ s: NSAttributedString, width: CGFloat, emptyFont: UIFont, maxLines: Int, truncate: Bool) {
        string = s
        if s.length == 0 {
            // An empty paragraph is an empty line.
            height = ceil(emptyFont.lineHeight)
            return
        }
        let ts = CTTypesetterCreateWithAttributedString(s)
        var start = 0
        var y: CGFloat = 0
        while start < s.length && (maxLines == 0 || lines.count < maxLines) {
            var count = CTTypesetterSuggestLineBreak(ts, start, Double(width))
            if count == 0 { count = 1 }
            var line = CTTypesetterCreateLine(ts, CFRange(location: start, length: count))
            let last = maxLines > 0 && lines.count == maxLines - 1 && start + count < s.length
            if last && truncate {
                // The last line allowed, with more after it: the rest, truncated.
                let rest = CTTypesetterCreateLine(ts, CFRange(location: start, length: s.length - start))
                let token = CTLineCreateWithAttributedString(NSAttributedString(string: "…", attributes: s.attributes(at: start + count - 1, effectiveRange: nil)))
                line = CTLineCreateTruncatedLine(rest, Double(width), .end, token) ?? line
            }
            var a: CGFloat = 0, d: CGFloat = 0, l: CGFloat = 0
            let w = CGFloat(CTLineGetTypographicBounds(line, &a, &d, &l)) - CGFloat(CTLineGetTrailingWhitespaceWidth(line))
            y += a
            lines.append(Line(line: line, baseline: y, ascent: a, descent: d, width: w, range: CFRange(location: start, length: count)))
            y += d + max(l, 0)
            self.width = max(self.width, w)
            start += count
        }
        height = ceil(y)
    }
}

/// RichText's view: paragraphs typeset with CoreText and drawn by it,
/// so what's measured is what's drawn. Layouts are cached by paragraph
/// content, the view's text props, the width and the traits.
final class NuiRichTextView: UIView {
    var styles: [NuiRichStyle] = []
    var paragraphs: [NuiRichParagraph] = []
    var base = NuiRichBase() { didSet { if base != oldValue { invalidate() } } }
    var onLink: ((String) -> Void)?
    private var laid: [NuiParagraphLayout] = []
    private var laidWidth: CGFloat = -1
    private var plain = ""

    override init(frame: CGRect) {
        super.init(frame: frame)
        isOpaque = false
        backgroundColor = .clear
        contentMode = .redraw
        isAccessibilityElement = true
        accessibilityTraits = .staticText
        if #available(iOS 17, *) {
            registerForTraitChanges([UITraitUserInterfaceStyle.self, UITraitPreferredContentSizeCategory.self]) { (v: NuiRichTextView, _: UITraitCollection) in
                v.invalidate()
            }
        }
        addGestureRecognizer(UITapGestureRecognizer(target: self, action: #selector(tapped(_:))))
    }
    required init?(coder: NSCoder) { fatalError() }

    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        if #unavailable(iOS 17) { invalidate() }
    }

    // Content

    func setContent(_ b: UnsafeRawBufferPointer) {
        var r = NuiReader(b)
        guard b.count >= 8 else { styles = []; paragraphs = []; invalidate(); return }
        let n = Int(r.u32())
        var styleBytes: [Data] = []
        styles = (0..<n).map { _ in
            let start = r.p
            var s = NuiRichStyle()
            s.textStyle = Int32(r.u8())
            s.size = r.f32()
            s.weight = Int32(r.u8())
            s.flags = r.u8()
            s.color = r.u64()
            s.background = r.u64()
            s.link = r.string()
            styleBytes.append(Data(bytes: start, count: r.p - start))
            return s
        }
        let pn = Int(r.u32())
        var text = ""
        paragraphs = (0..<pn).map { i in
            var p = NuiRichParagraph()
            let rn = Int(r.u32())
            for _ in 0..<rn {
                let st = Int(r.u32())
                let t = r.string()
                p.runs.append((st, t))
                p.key.append(styleBytes[st])
                p.key.append(contentsOf: Array(t.utf8))
                p.key.append(0)
                text += t
            }
            if i < pn - 1 { text += "\n" }
            return p
        }
        plain = text
        accessibilityLabel = text
        accessibilityTraits = styles.contains { !$0.link.isEmpty } ? [.staticText, .link] : .staticText
        invalidate()
    }

    func invalidate() {
        laidWidth = -1
        setNeedsDisplay()
    }

    private func attributed(_ p: NuiRichParagraph) -> NSAttributedString {
        let out = NSMutableAttributedString()
        let t = traits
        for run in p.runs {
            let s = styles[run.style]
            let font = nuiFont(s.textStyle != 0 ? s.textStyle : base.textStyle, s.size > 0 ? s.size : base.size,
                               s.weight != 0 ? s.weight : base.weight, italic: s.italic, mono: s.mono)
            var color = nuiColor(s.color != 0 ? s.color : base.color) ?? .label
            if s.color == 0 && !s.link.isEmpty { color = tintColor }
            var a: [NSAttributedString.Key: Any] = [.font: font, .ctForeground: color.resolvedColor(with: t).cgColor]
            if s.background != 0, let bg = nuiColor(s.background) { a[.nuiBackground] = bg.resolvedColor(with: t).cgColor }
            if !s.link.isEmpty { a[.nuiLink] = s.link }
            if s.underline { a[.nuiUnderline] = true }
            if s.strike { a[.nuiStrike] = true }
            out.append(NSAttributedString(string: run.text, attributes: a))
        }
        return out
    }

    // Layout

    private struct CacheKey: Hashable {
        var paragraph: Data
        var base: NuiRichBase
        var width: CGFloat
        var lines: Int
        var size: UIContentSizeCategory
        var dark: Bool
    }
    private static var cache: [CacheKey: NuiParagraphLayout] = [:]

    /// The paragraphs' layouts at width (cached). Fonts resolve with the
    /// view's traits (Dynamic Type), wherever it's called from.
    func layouts(_ width: CGFloat) -> [NuiParagraphLayout] {
        var out: [NuiParagraphLayout] = []
        traits.performAsCurrent { out = layoutsNow(width) }
        return out
    }

    /// The view's traits with the app's text size: the core measures as
    /// soon as the size changes, before UIKit hands the view its new
    /// traits, and what it measures must be what's drawn.
    var traits: UITraitCollection {
        UITraitCollection(traitsFrom: [traitCollection, UITraitCollection(preferredContentSizeCategory: UIApplication.shared.preferredContentSizeCategory)])
    }

    private func layoutsNow(_ width: CGFloat) -> [NuiParagraphLayout] {
        var out: [NuiParagraphLayout] = []
        var left = base.lines
        let t = traits
        let emptyFont = nuiFont(base.textStyle, base.size, base.weight)
        for p in paragraphs {
            if base.lines > 0 && left <= 0 { break }
            let key = CacheKey(paragraph: p.key, base: base, width: width, lines: left, size: t.preferredContentSizeCategory, dark: t.userInterfaceStyle == .dark)
            let l: NuiParagraphLayout
            if let hit = Self.cache[key] {
                l = hit
            } else {
                l = NuiParagraphLayout(attributed(p), width: width, emptyFont: emptyFont, maxLines: max(left, 0), truncate: true)
                if Self.cache.count > 2048 { Self.cache.removeAll(keepingCapacity: true) }
                Self.cache[key] = l
            }
            out.append(l)
            left -= max(l.lines.count, 1)
        }
        return out
    }

    /// The size the text wants within the core's constraints.
    func measure(_ w: Float, _ aw: Float) -> CGSize {
        if aw == -.infinity && w.isNaN {
            // Min-content: the widest word.
            var widest: CGFloat = 0
            traits.performAsCurrent {
            for p in paragraphs {
                let s = attributed(p)
                let ns = s.string as NSString
                ns.enumerateSubstrings(in: NSRange(location: 0, length: ns.length), options: .byWords) { _, r, _, _ in
                    let line = CTLineCreateWithAttributedString(s.attributedSubstring(from: r))
                    widest = max(widest, CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil)))
                }
            }
            }
            let ls = layouts(ceil(widest))
            return CGSize(width: ceil(widest), height: ls.reduce(0) { $0 + $1.height })
        }
        let width: CGFloat = !w.isNaN ? CGFloat(w) : (aw.isFinite ? CGFloat(max(aw, 0)) : 1e7)
        let ls = layouts(width)
        let wide = ls.reduce(0) { max($0, $1.width) }
        return CGSize(width: !w.isNaN ? CGFloat(w) : ceil(wide), height: ls.reduce(0) { $0 + $1.height })
    }

    private func current() -> [NuiParagraphLayout] {
        if laidWidth != bounds.width {
            laid = layouts(bounds.width)
            laidWidth = bounds.width
        }
        return laid
    }

    private func penX(_ l: NuiParagraphLayout.Line) -> CGFloat {
        var flush: CGFloat = 0
        let rtl = effectiveUserInterfaceLayoutDirection == .rightToLeft
        switch NuiTextAlign(rawValue: base.align) {
        case .center: flush = 0.5
        case .end: flush = rtl ? 0 : 1
        default: flush = rtl ? 1 : 0
        }
        return (bounds.width - l.width) * flush
    }

    override func draw(_ rect: CGRect) {
        guard let ctx = UIGraphicsGetCurrentContext() else { return }
        // Glyphs upright in UIKit's flipped coordinates.
        ctx.textMatrix = CGAffineTransform(scaleX: 1, y: -1)
        var top: CGFloat = 0
        for p in current() {
            for l in p.lines {
                let x = penX(l)
                let baseline = top + l.baseline
                let runs = CTLineGetGlyphRuns(l.line) as! [CTRun]
                // Backgrounds first, each run's on this line, rounded.
                for run in runs {
                    let attrs = CTRunGetAttributes(run) as NSDictionary
                    guard let bg = attrs[NSAttributedString.Key.nuiBackground] else { continue }
                    // Not past the line's end: the space a line breaks at isn't highlighted.
                    var r = runRect(l, run, x: x, baseline: baseline)
                    r.size.width = max(0, min(r.maxX, x + l.width) - r.minX)
                    if r.width == 0 { continue }
                    r = r.insetBy(dx: -3, dy: -1)
                    ctx.setFillColor(bg as! CGColor)
                    ctx.addPath(UIBezierPath(roundedRect: r, cornerRadius: min(5, r.height / 4)).cgPath)
                    ctx.fillPath()
                }
                ctx.textPosition = CGPoint(x: x, y: baseline)
                CTLineDraw(l.line, ctx)
                // Underlines and strikethroughs: CoreText draws neither.
                for run in runs {
                    let attrs = CTRunGetAttributes(run) as NSDictionary
                    let under = attrs[NSAttributedString.Key.nuiUnderline] != nil
                    let strike = attrs[NSAttributedString.Key.nuiStrike] != nil
                    guard under || strike, let font = attrs[kCTFontAttributeName] else { continue }
                    let f = font as! CTFont
                    let r = runRect(l, run, x: x, baseline: baseline)
                    let thick = max(CTFontGetUnderlineThickness(f), 1)
                    let color = attrs[NSAttributedString.Key.ctForeground] as! CGColor
                    ctx.setFillColor(color)
                    if under {
                        ctx.fill(CGRect(x: r.minX, y: baseline - CTFontGetUnderlinePosition(f), width: r.width, height: thick))
                    }
                    if strike {
                        ctx.fill(CGRect(x: r.minX, y: baseline - CTFontGetXHeight(f) / 2, width: r.width, height: thick))
                    }
                }
            }
            top += p.height
        }
    }

    private func runRect(_ l: NuiParagraphLayout.Line, _ run: CTRun, x: CGFloat, baseline: CGFloat) -> CGRect {
        let range = CTRunGetStringRange(run)
        let x0 = CTLineGetOffsetForStringIndex(l.line, range.location, nil)
        let x1 = CTLineGetOffsetForStringIndex(l.line, range.location + range.length, nil)
        var a: CGFloat = 0, d: CGFloat = 0
        CTRunGetTypographicBounds(run, CFRange(location: 0, length: 0), &a, &d, nil)
        return CGRect(x: x + min(x0, x1), y: baseline - a, width: abs(x1 - x0), height: a + d)
    }

    // Links

    @objc private func tapped(_ g: UITapGestureRecognizer) {
        guard let onLink, let link = link(at: g.location(in: self)) else { return }
        onLink(link)
    }

    func link(at p: CGPoint) -> String? {
        var top: CGFloat = 0
        for para in current() {
            if p.y >= top && p.y < top + para.height {
                for l in para.lines where p.y < top + l.baseline + l.descent + 2 {
                    let x = p.x - penX(l)
                    guard x >= -4 && x <= l.width + 4 else { return nil }
                    let i = CTLineGetStringIndexForPosition(l.line, CGPoint(x: x, y: 0))
                    guard i != kCFNotFound, i < para.string.length else { return nil }
                    // The index is the caret's nearest; the glyph is the one before it when the tap is left of it.
                    let at = CTLineGetOffsetForStringIndex(l.line, i, nil) > x && i > l.range.location ? i - 1 : i
                    return para.string.attribute(.nuiLink, at: at, effectiveRange: nil) as? String
                }
                return nil
            }
            top += para.height
        }
        return nil
    }
}
#endif
