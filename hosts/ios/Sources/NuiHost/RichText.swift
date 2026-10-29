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
    var inline: Bool { flags & 16 != 0 }
}

/// A paragraph: its runs, and a key for its layouts (its runs and the
/// styles they use, as bytes).
struct NuiRichParagraph {
    var runs: [(style: Int, text: String)] = []
    var key = Data()
    /// The inline components of its runs, in order: indexes into the view's.
    var inlines: [Int] = []
}

/// The view's own text props: the spans' defaults.
struct NuiRichBase: Hashable {
    var textStyle: Int32 = 0
    var size: Float = 0
    var weight: Int32 = 0
    var family = ""
    var color: UInt64 = 0
    var align: Int32 = 0
    var lines = 0
}

/// A font from text props: a Dynamic Type text style, or a system font
/// scaled like one; italic and monospaced as asked.
func nuiFont(_ textStyle: Int32, _ size: Float, _ weight: Int32, family: String = "", italic: Bool = false, mono: Bool = false) -> UIFont {
    let style = uiTextStyle(textStyle)
    let metrics = UIFontMetrics(forTextStyle: style)
    let w = uiWeight(weight)
    var f: UIFont
    let base = size > 0 ? CGFloat(size)
        : UIFont.preferredFont(forTextStyle: style, compatibleWith: UITraitCollection(preferredContentSizeCategory: .large)).pointSize
    let t = UITraitCollection.current
    if mono {
        f = metrics.scaledFont(for: .monospacedSystemFont(ofSize: base * 0.94, weight: w ?? .regular), compatibleWith: t)
    } else if !family.isEmpty, let c = NuiFonts.font(family, size: base, weight: w) {
        f = metrics.scaledFont(for: c, compatibleWith: t)
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
    static let nuiInline = NSAttributedString.Key("nuiInline")
}

/// The metrics of an inline component's placeholder: its size, on the baseline.
private final class NuiInlineMetrics {
    let size: CGSize
    init(_ s: CGSize) { size = s }
}

private func inlineDelegate(_ size: CGSize) -> CTRunDelegate? {
    var cb = CTRunDelegateCallbacks(version: kCTRunDelegateVersion1, dealloc: { p in
        Unmanaged<NuiInlineMetrics>.fromOpaque(p).release()
    }, getAscent: { p in
        Unmanaged<NuiInlineMetrics>.fromOpaque(p).takeUnretainedValue().size.height
    }, getDescent: { _ in 0 }, getWidth: { p in
        Unmanaged<NuiInlineMetrics>.fromOpaque(p).takeUnretainedValue().size.width
    })
    return CTRunDelegateCreate(&cb, Unmanaged.passRetained(NuiInlineMetrics(size)).toOpaque())
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

/// Paragraph layouts by their inputs, shared by all rich texts. Layouts
/// are typeset ahead on a background queue when a text's content changes
/// (at the width it was laid out at), so that the main thread, measuring,
/// takes them done or waits for the rest of the work, not all of it.
final class NuiLayoutCache: @unchecked Sendable {
    static let shared = NuiLayoutCache()
    struct Key: Hashable {
        var paragraph: Data
        var base: NuiRichBase
        var width: CGFloat
        var lines: Int
        var size: UIContentSizeCategory
        var dark: Bool
    }
    private let lock = NSLock()
    private var done: [Key: NuiParagraphLayout] = [:]
    private var pending: [Key: DispatchGroup] = [:]
    private let queue = DispatchQueue(label: "nui.richtext", qos: .userInitiated, attributes: .concurrent)

    /// The layout for key: done, done by the work in flight (waited for),
    /// or made now.
    func layout(_ key: Key, make: () -> NuiParagraphLayout) -> NuiParagraphLayout {
        lock.lock()
        if let l = done[key] { lock.unlock(); return l }
        if let g = pending[key] {
            lock.unlock()
            g.wait()
            lock.lock()
            if let l = done[key] { lock.unlock(); return l }
        }
        lock.unlock()
        let l = make()
        store(key, l)
        return l
    }

    /// Whether the layout for key is done or in flight.
    func has(_ key: Key) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return done[key] != nil || pending[key] != nil
    }

    /// Starts making the layout for key, off the main thread, unless it's
    /// done or in flight.
    func prepare(_ key: Key, make: @escaping @Sendable () -> NuiParagraphLayout) {
        lock.lock()
        if done[key] != nil || pending[key] != nil { lock.unlock(); return }
        let g = DispatchGroup()
        g.enter()
        pending[key] = g
        lock.unlock()
        queue.async {
            let l = make()
            self.store(key, l)
            g.leave()
        }
    }

    /// Forgets the layouts (the accent color changed: links are colored with it).
    func clear() {
        lock.lock()
        done.removeAll()
        lock.unlock()
    }

    private func store(_ key: Key, _ l: NuiParagraphLayout) {
        lock.lock()
        if done.count > 2048 { done.removeAll(keepingCapacity: true) }
        done[key] = l
        pending[key] = nil
        lock.unlock()
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
    /// The inline components (children): node id and size, in run order.
    private(set) var inlines: [(id: UInt32, size: CGSize)] = []
    /// The links, by their range of the plain text (UTF-16).
    private var links: [(range: NSRange, url: String, text: String)] = []

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
        var inlineCount = 0
        var found: [(NSRange, String, String)] = []
        paragraphs = (0..<pn).map { i in
            var p = NuiRichParagraph()
            let rn = Int(r.u32())
            for _ in 0..<rn {
                let st = Int(r.u32())
                let t = r.string()
                if styles[st].inline {
                    p.inlines.append(inlineCount)
                    inlineCount += 1
                }
                p.runs.append((st, t))
                p.key.append(styleBytes[st])
                p.key.append(contentsOf: Array(t.utf8))
                p.key.append(0)
                if !styles[st].link.isEmpty {
                    let at = (text as NSString).length
                    found.append((NSRange(location: at, length: (t as NSString).length), styles[st].link, t))
                }
                text += t
            }
            if i < pn - 1 { text += "\n" }
            return p
        }
        plain = text
        // Adjacent runs of one link are one link.
        links = []
        for l in found {
            if let last = links.last, last.url == l.1, NSMaxRange(last.range) == l.0.location {
                links[links.count - 1] = (NSRange(location: last.range.location, length: last.range.length + l.0.length), last.url, last.text + l.2)
            } else {
                links.append((l.0, l.1, l.2))
            }
        }
        accessibilityLabel = text.replacingOccurrences(of: "\u{FFFC}", with: "") // the inline components are views of their own
        a11y = nil
        var v = superview
        while let x = v, !(x is NuiSelectionArea) { v = x.superview }
        (v as? NuiSelectionArea)?.textChanged()
        accessibilityTraits = styles.contains { !$0.link.isEmpty } ? [.staticText, .link] : .staticText
        invalidate()
        prepare()
    }

    func invalidate() {
        laidWidth = -1
        setNeedsDisplay()
        if !subviews.isEmpty { setNeedsLayout() }
    }

    /// Sets the inline components' ids and sizes (the Inlines prop).
    func setInlines(_ b: UnsafeRawBufferPointer) {
        var r = NuiReader(b)
        var out: [(UInt32, CGSize)] = []
        if b.count >= 4 {
            for _ in 0..<Int(r.u32()) {
                let id = r.u32()
                let w = CGFloat(r.f32()), h = CGFloat(r.f32())
                out.append((id, CGSize(width: w, height: h)))
            }
        }
        inlines = out
        invalidate()
    }

    /// A paragraph's layout key: its content, and its components' sizes.
    private func layoutKey(_ p: NuiRichParagraph) -> Data {
        guard !p.inlines.isEmpty else { return p.key }
        var k = p.key
        for i in p.inlines {
            let s = i < inlines.count ? inlines[i].size : .zero
            withUnsafeBytes(of: Float(s.width)) { k.append(contentsOf: $0) }
            withUnsafeBytes(of: Float(s.height)) { k.append(contentsOf: $0) }
        }
        return k
    }

    private func attributed(_ p: NuiRichParagraph) -> NSAttributedString {
        let out = NSMutableAttributedString()
        let t = traits
        var next = 0
        for run in p.runs {
            let s = styles[run.style]
            if s.inline {
                // A placeholder the component's size, drawn as nothing.
                let i = next < p.inlines.count ? p.inlines[next] : -1
                next += 1
                let size = i >= 0 && i < inlines.count ? inlines[i].size : .zero
                var a: [NSAttributedString.Key: Any] = [.ctForeground: UIColor.clear.cgColor, .nuiInline: i]
                if let d = inlineDelegate(size) { a[NSAttributedString.Key(kCTRunDelegateAttributeName as String)] = d }
                out.append(NSAttributedString(string: "\u{FFFC}", attributes: a))
                continue
            }
            let font = nuiFont(s.textStyle != 0 ? s.textStyle : base.textStyle, s.size > 0 ? s.size : base.size,
                               s.weight != 0 ? s.weight : base.weight, family: base.family, italic: s.italic, mono: s.mono)
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

    /// The width it was last laid out at: where new content is typeset ahead.
    private var lastWidth: CGFloat = -1

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
        let emptyFont = nuiFont(base.textStyle, base.size, base.weight, family: base.family)
        if width < 1e6 { lastWidth = width }
        for p in paragraphs {
            if base.lines > 0 && left <= 0 { break }
            let key = NuiLayoutCache.Key(paragraph: layoutKey(p), base: base, width: width, lines: left, size: t.preferredContentSizeCategory, dark: t.userInterfaceStyle == .dark)
            let lines = max(left, 0)
            let l = NuiLayoutCache.shared.layout(key) {
                NuiParagraphLayout(attributed(p), width: width, emptyFont: emptyFont, maxLines: lines, truncate: true)
            }
            out.append(l)
            left -= max(l.lines.count, 1)
        }
        return out
    }

    /// Typesets the paragraphs at the last width, off the main thread, for
    /// the measuring to come. The attributed strings are made here (they
    /// read the view's tint and traits); the typesetting, the work, there.
    private func prepare() {
        guard lastWidth > 0, base.lines == 0 else { return }
        let width = lastWidth
        let t = traits
        t.performAsCurrent {
            let emptyFont = nuiFont(base.textStyle, base.size, base.weight, family: base.family)
            for p in paragraphs {
                let key = NuiLayoutCache.Key(paragraph: layoutKey(p), base: base, width: width, lines: 0, size: t.preferredContentSizeCategory, dark: t.userInterfaceStyle == .dark)
                if !p.inlines.isEmpty || NuiLayoutCache.shared.has(key) { continue }
                let s = attributed(p)
                NuiLayoutCache.shared.prepare(key) {
                    NuiParagraphLayout(s, width: width, emptyFont: emptyFont, maxLines: 0, truncate: true)
                }
            }
        }
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

    // Geometry, for selection: offsets are into the plain text (the
    // paragraphs joined by newlines), in UTF-16 units, as UIKit's are.

    var text: String { plain }
    var textLength: Int { (plain as NSString).length }

    struct LineGeometry {
        var line: NuiParagraphLayout.Line
        var top: CGFloat // the paragraph's top
        var x: CGFloat // the line's pen
        var start: Int // the paragraph's offset in the plain text
        var rect: CGRect { CGRect(x: x, y: top + line.baseline - line.ascent, width: line.width, height: line.ascent + line.descent) }
    }

    func lineGeometry() -> [LineGeometry] {
        var out: [LineGeometry] = []
        var top: CGFloat = 0
        var start = 0
        for p in current() {
            for l in p.lines {
                out.append(LineGeometry(line: l, top: top, x: penX(l), start: start))
            }
            start += p.string.length + 1
            top += p.height
        }
        return out
    }

    /// The line holding offset (the last one ending at it).
    private func line(at offset: Int, _ ls: [LineGeometry]) -> LineGeometry? {
        var best: LineGeometry?
        for g in ls {
            let a = g.start + g.line.range.location
            if offset >= a && offset <= a + g.line.range.length { best = g; if offset < a + g.line.range.length { break } }
            if a > offset { break }
        }
        return best ?? ls.last
    }

    private func x(of offset: Int, in g: LineGeometry) -> CGFloat {
        g.x + CTLineGetOffsetForStringIndex(g.line.line, max(0, offset - g.start), nil)
    }

    func caretRect(at offset: Int) -> CGRect {
        let ls = lineGeometry()
        guard let g = line(at: offset, ls) else {
            let f = nuiFont(base.textStyle, base.size, base.weight, family: base.family)
            return CGRect(x: 0, y: 0, width: 2, height: f.lineHeight)
        }
        let r = g.rect
        return CGRect(x: x(of: offset, in: g), y: r.minY, width: 2, height: r.height)
    }

    /// The rectangles covering [from, to), a line each.
    func rects(from: Int, to: Int) -> [CGRect] {
        var out: [CGRect] = []
        for g in lineGeometry() {
            let a = g.start + g.line.range.location, b = a + g.line.range.length
            let s = max(from, a), e = min(to, b)
            guard s < e else { continue }
            let x0 = x(of: s, in: g), x1 = min(x(of: e, in: g), g.x + g.line.width)
            let r = g.rect
            out.append(CGRect(x: min(x0, x1), y: r.minY, width: max(abs(x1 - x0), 4), height: r.height))
        }
        return out
    }

    /// The offset closest to p.
    func closestOffset(to p: CGPoint) -> Int {
        let ls = lineGeometry()
        guard !ls.isEmpty else { return 0 }
        var best = ls[0]
        var dist = CGFloat.infinity
        for g in ls {
            let r = g.rect
            let d = p.y < r.minY ? r.minY - p.y : p.y > r.maxY ? p.y - r.maxY : 0
            if d < dist { dist = d; best = g }
        }
        let i = CTLineGetStringIndexForPosition(best.line.line, CGPoint(x: p.x - best.x, y: 0))
        let a = best.line.range.location
        let local = i == kCFNotFound ? a : min(max(i, a), a + best.line.range.length)
        return best.start + local
    }

    // Accessibility: the text, then each link an element of its own that
    // VoiceOver can find and activate.

    private var a11y: [UIAccessibilityElement]?

    override var isAccessibilityElement: Bool {
        get { links.isEmpty && subviews.isEmpty }
        set {}
    }

    override var accessibilityElements: [Any]? {
        get {
            guard !links.isEmpty || !subviews.isEmpty else { return nil }
            if let a11y { return a11y + subviews }
            let whole = UIAccessibilityElement(accessibilityContainer: self)
            whole.accessibilityLabel = plain.replacingOccurrences(of: "\u{FFFC}", with: "")
            whole.accessibilityTraits = .staticText
            whole.accessibilityFrameInContainerSpace = bounds
            var out = [whole]
            for l in links {
                let e = NuiLinkElement(accessibilityContainer: self)
                e.accessibilityLabel = l.text
                e.accessibilityTraits = .link
                e.url = l.url
                e.view = self
                let rs = rects(from: l.range.location, to: NSMaxRange(l.range))
                e.accessibilityFrameInContainerSpace = rs.dropFirst().reduce(rs.first ?? .zero) { $0.union($1) }
                out.append(e)
            }
            a11y = out
            return out + subviews
        }
        set {}
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        a11y = nil // frames move with the layout
        placeInlines()
    }

    /// Puts each inline component at its placeholder (the core sizes it);
    /// one in a line cut off is hidden.
    func placeInlines() {
        guard !inlines.isEmpty else { return }
        var at: [Int: CGPoint] = [:]
        var top: CGFloat = 0
        for p in current() {
            for l in p.lines {
                for run in CTLineGetGlyphRuns(l.line) as! [CTRun] {
                    let attrs = CTRunGetAttributes(run) as NSDictionary
                    guard let i = attrs[NSAttributedString.Key.nuiInline] as? Int, i >= 0, i < inlines.count else { continue }
                    let x = penX(l) + CTLineGetOffsetForStringIndex(l.line, CTRunGetStringRange(run).location, nil)
                    at[i] = CGPoint(x: x, y: top + l.baseline - inlines[i].size.height)
                }
            }
            top += p.height
        }
        let rt = NuiRuntime.shared
        for (i, c) in inlines.enumerated() {
            guard let v = rt.nodes[c.id]?.view, v.superview === self else { continue }
            if let o = at[i] {
                v.isHidden = false
                v.frame.origin = o
            } else {
                v.isHidden = true
            }
        }
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
/// A link of a rich text, for VoiceOver: activating it follows it.
final class NuiLinkElement: UIAccessibilityElement {
    var url = ""
    weak var view: NuiRichTextView?
    override func accessibilityActivate() -> Bool {
        guard let v = view, let f = v.onLink else { return false }
        f(url)
        return true
    }
}
#endif
