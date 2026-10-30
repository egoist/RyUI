#if canImport(UIKit)
import UIKit

/// A position in a selection area's text.
final class NuiTextPosition: UITextPosition {
    let offset: Int
    init(_ o: Int) { offset = o }
}

/// A range of a selection area's text.
final class NuiTextRange: UITextRange {
    let a: Int, b: Int
    init(_ a: Int, _ b: Int) { self.a = min(a, b); self.b = max(a, b) }
    override var start: UITextPosition { NuiTextPosition(a) }
    override var end: UITextPosition { NuiTextPosition(b) }
    override var isEmpty: Bool { a == b }
}

final class NuiSelectionRect: UITextSelectionRect {
    let r: CGRect
    let first: Bool, last: Bool
    init(_ r: CGRect, first: Bool, last: Bool) { self.r = r; self.first = first; self.last = last }
    override var rect: CGRect { r }
    override var writingDirection: NSWritingDirection { .natural }
    override var containsStart: Bool { first }
    override var containsEnd: Bool { last }
    override var isVertical: Bool { false }
}

/// A Selectable: its RichText descendants, in document order, are one
/// text (joined by newlines), selected with UIKit's own text interaction
/// (long press, loupe, handles, the edit menu). The interaction is an
/// overlay's, a text input hidden from accessibility, so that the area
/// stays a container whose texts (and their links) VoiceOver reads.
final class NuiSelectionArea: UIView {
    let input = NuiSelectionInput()
    weak var node: NuiNode?

    override init(frame: CGRect) {
        super.init(frame: frame)
        input.area = self
        input.frame = bounds
        input.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        addSubview(input)
    }
    required init?(coder: NSCoder) { fatalError() }

    override func didAddSubview(_ v: UIView) {
        super.didAddSubview(v)
        if v !== input { bringSubviewToFront(input) }
    }

    override func hitTest(_ point: CGPoint, with event: UIEvent?) -> UIView? {
        // UIKit's text interaction starts only for touches on the text
        // input itself: touches on the texts are the input's. Not those of
        // a text in a scroll view within (a code block), which must scroll.
        let v = super.hitTest(point, with: event)
        guard let r = v as? NuiRichTextView else { return v }
        var p = r.superview
        while let x = p, x !== self {
            if x is UIScrollView { return v }
            p = x.superview
        }
        return input
    }

    /// The text changed (streaming): keep the selection within it.
    func textChanged() { input.textChanged() }
}

/// The text input over a selection area.
final class NuiSelectionInput: UIView, UITextInput, UIGestureRecognizerDelegate {
    weak var area: NuiSelectionArea?
    private var selection = NuiTextRange(0, 0)
    weak var inputDelegate: UITextInputDelegate?
    lazy var tokenizer: UITextInputTokenizer = UITextInputStringTokenizer(textInput: self)
    private var interaction: UITextInteraction!

    override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = .clear
        accessibilityElementsHidden = true
        isAccessibilityElement = false
        interaction = UITextInteraction(for: .nonEditable)
        interaction.textInput = self
        addInteraction(interaction)
        let tap = UITapGestureRecognizer(target: self, action: #selector(tapped(_:)))
        tap.delegate = self
        addGestureRecognizer(tap)
    }
    required init?(coder: NSCoder) { fatalError() }

    override var canBecomeFirstResponder: Bool { true }

    // Never hit by itself: the area hands it the touches on texts.
    override func point(inside point: CGPoint, with event: UIEvent?) -> Bool { false }
    var textInputView: UIView { self }

    /// A tap on a link, which the texts don't see (their touches are the area's).
    @objc private func tapped(_ g: UITapGestureRecognizer) {
        let p = g.location(in: self)
        for s in segments {
            let q = convert(p, to: s)
            if s.bounds.contains(q), let link = s.link(at: q) {
                s.onLink?(link)
                return
            }
        }
    }

    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }

    // The texts

    /// The area's rich texts, in document order: from the core's tree,
    /// which has those a Scroll took out of the views' (out of sight).
    var segments: [NuiRichTextView] {
        var out: [NuiRichTextView] = []
        func walk(_ n: NuiNode) {
            for c in n.children {
                if let r = c.view as? NuiRichTextView { out.append(r) } else { walk(c) }
            }
        }
        func walk(_ v: UIView) {
            for s in v.subviews where s !== self {
                if let r = s as? NuiRichTextView { out.append(r) } else { walk(s) }
            }
        }
        if let n = area?.node { walk(n) } else if let area { walk(area) }
        return out
    }

    /// Each segment with its offset in the text.
    private func layout() -> [(seg: NuiRichTextView, start: Int)] {
        var out: [(NuiRichTextView, Int)] = []
        var at = 0
        for s in segments {
            out.append((s, at))
            at += s.textLength + 1
        }
        return out
    }

    private var length: Int {
        let ls = layout()
        guard let last = ls.last else { return 0 }
        return last.start + last.seg.textLength
    }

    private func clamp(_ o: Int) -> Int { max(0, min(o, length)) }

    /// The segment holding offset o, and o within it.
    private func segment(at o: Int) -> (NuiRichTextView, Int)? {
        var found: (NuiRichTextView, Int)?
        for (s, start) in layout() where o >= start {
            found = (s, min(o - start, s.textLength))
        }
        return found
    }

    private var string: String { segments.map(\.text).joined(separator: "\n") }

    // UITextInput: the text

    func text(in range: UITextRange) -> String? {
        guard let r = range as? NuiTextRange else { return nil }
        let s = string as NSString
        let a = max(0, min(r.a, s.length)), b = max(a, min(r.b, s.length))
        return s.substring(with: NSRange(location: a, length: b - a))
    }

    func replace(_ range: UITextRange, withText text: String) {}

    var selectedTextRange: UITextRange? {
        get { selection }
        set {
            guard let r = newValue as? NuiTextRange else { return }
            selection = NuiTextRange(clamp(r.a), clamp(r.b))
        }
    }

    var markedTextRange: UITextRange? { nil }
    var markedTextStyle: [NSAttributedString.Key: Any]? {
        get { nil }
        set {}
    }
    func setMarkedText(_ markedText: String?, selectedRange: NSRange) {}
    func unmarkText() {}

    var beginningOfDocument: UITextPosition { NuiTextPosition(0) }
    var endOfDocument: UITextPosition { NuiTextPosition(length) }

    func textRange(from: UITextPosition, to: UITextPosition) -> UITextRange? {
        guard let a = from as? NuiTextPosition, let b = to as? NuiTextPosition else { return nil }
        return NuiTextRange(a.offset, b.offset)
    }

    func position(from position: UITextPosition, offset: Int) -> UITextPosition? {
        guard let p = position as? NuiTextPosition else { return nil }
        let o = p.offset + offset
        return o < 0 || o > length ? nil : NuiTextPosition(o)
    }

    func position(from position: UITextPosition, in direction: UITextLayoutDirection, offset: Int) -> UITextPosition? {
        guard let p = position as? NuiTextPosition else { return nil }
        switch direction {
        case .left: return self.position(from: p, offset: -offset)
        case .right: return self.position(from: p, offset: offset)
        case .up, .down:
            var r = caretRect(for: p)
            for _ in 0..<max(offset, 1) {
                r.origin.y += direction == .up ? -r.height : r.height * 1.5
            }
            return closestPosition(to: CGPoint(x: r.midX, y: r.midY))
        @unknown default: return nil
        }
    }

    func compare(_ position: UITextPosition, to other: UITextPosition) -> ComparisonResult {
        guard let a = position as? NuiTextPosition, let b = other as? NuiTextPosition else { return .orderedSame }
        return a.offset < b.offset ? .orderedAscending : a.offset > b.offset ? .orderedDescending : .orderedSame
    }

    func offset(from: UITextPosition, to: UITextPosition) -> Int {
        guard let a = from as? NuiTextPosition, let b = to as? NuiTextPosition else { return 0 }
        return b.offset - a.offset
    }

    func position(within range: UITextRange, farthestIn direction: UITextLayoutDirection) -> UITextPosition? {
        guard let r = range as? NuiTextRange else { return nil }
        return direction == .left || direction == .up ? r.start : r.end
    }

    func characterRange(byExtending position: UITextPosition, in direction: UITextLayoutDirection) -> UITextRange? {
        guard let p = position as? NuiTextPosition else { return nil }
        return direction == .left || direction == .up ? NuiTextRange(clamp(p.offset - 1), p.offset) : NuiTextRange(p.offset, clamp(p.offset + 1))
    }

    func baseWritingDirection(for position: UITextPosition, in direction: UITextStorageDirection) -> NSWritingDirection { .natural }
    func setBaseWritingDirection(_ writingDirection: NSWritingDirection, for range: UITextRange) {}

    // UITextInput: geometry, in the area's coordinates

    func firstRect(for range: UITextRange) -> CGRect {
        selectionRects(for: range).first?.rect ?? .zero
    }

    func caretRect(for position: UITextPosition) -> CGRect {
        guard let p = position as? NuiTextPosition, let (s, o) = segment(at: p.offset) else { return .zero }
        return s.convert(s.caretRect(at: o), to: self)
    }

    func selectionRects(for range: UITextRange) -> [UITextSelectionRect] {
        guard let r = range as? NuiTextRange, r.a < r.b else { return [] }
        var rects: [CGRect] = []
        for (s, start) in layout() {
            let a = max(r.a - start, 0), b = min(r.b - start, s.textLength)
            guard a < b else { continue }
            rects += s.rects(from: a, to: b).map { s.convert($0, to: self) }
        }
        return rects.enumerated().map { i, rc in NuiSelectionRect(rc, first: i == 0, last: i == rects.count - 1) }
    }

    func closestPosition(to point: CGPoint) -> UITextPosition? {
        // The segment nearest the point, by its frame.
        var best: (NuiRichTextView, Int)?
        var dist = CGFloat.infinity
        for (s, start) in layout() {
            let f = s.convert(s.bounds, to: self)
            let dy = point.y < f.minY ? f.minY - point.y : point.y > f.maxY ? point.y - f.maxY : 0
            let dx = point.x < f.minX ? f.minX - point.x : point.x > f.maxX ? point.x - f.maxX : 0
            let d = dy * 4 + dx // rows first
            if d < dist { dist = d; best = (s, start) }
        }
        guard let (s, start) = best else { return NuiTextPosition(0) }
        return NuiTextPosition(start + s.closestOffset(to: convert(point, to: s)))
    }

    func closestPosition(to point: CGPoint, within range: UITextRange) -> UITextPosition? {
        guard let r = range as? NuiTextRange, let p = closestPosition(to: point) as? NuiTextPosition else { return nil }
        return NuiTextPosition(min(max(p.offset, r.a), r.b))
    }

    func characterRange(at point: CGPoint) -> UITextRange? {
        guard let p = closestPosition(to: point) as? NuiTextPosition else { return nil }
        return NuiTextRange(p.offset, clamp(p.offset + 1))
    }

    // UIKeyInput: nothing to type

    var hasText: Bool { length > 0 }
    func insertText(_ text: String) {}
    func deleteBackward() {}

    // The edit menu

    override func canPerformAction(_ action: Selector, withSender sender: Any?) -> Bool {
        switch action {
        case #selector(copy(_:)): return !selection.isEmpty
        case #selector(selectAll(_:)): return hasText && (selection.a != 0 || selection.b != length)
        default: return false
        }
    }

    override func copy(_ sender: Any?) {
        // Not the inline components' placeholders.
        UIPasteboard.general.string = text(in: selection)?.replacingOccurrences(of: "\u{FFFC}", with: "")
    }

    override func selectAll(_ sender: Any?) {
        inputDelegate?.selectionWillChange(self)
        selection = NuiTextRange(0, length)
        inputDelegate?.selectionDidChange(self)
    }

    /// The text changed (streaming): keep the selection within it.
    func textChanged() {
        let n = length
        if selection.b > n {
            inputDelegate?.selectionWillChange(self)
            selection = NuiTextRange(min(selection.a, n), n)
            inputDelegate?.selectionDidChange(self)
        }
    }
}
#endif
