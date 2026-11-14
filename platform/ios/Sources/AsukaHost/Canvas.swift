#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A Canvas: its display list, replayed with Core Graphics in draw(_:),
/// in the view's appearance (semantic colors follow dark mode).
final class AsukaCanvasView: UIView {
    var commands: [UInt8] = [] {
        didSet { setNeedsDisplay() }
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
        isOpaque = false
        backgroundColor = nil
        contentMode = .redraw
        isAccessibilityElement = false
    }
    required init?(coder: NSCoder) { fatalError() }

    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        if previous?.userInterfaceStyle != traitCollection.userInterfaceStyle { setNeedsDisplay() }
    }

    override func draw(_ rect: CGRect) {
        guard let ctx = UIGraphicsGetCurrentContext(), !commands.isEmpty else { return }
        commands.withUnsafeBytes { buf in
            var r = AsukaReader(buf)
            // FillRule's even-odd, saved and restored with the state.
            var evenOdd = false
            var rules: [Bool] = []
            func f() -> CGFloat { CGFloat(r.f32()) }
            func stops() -> CGGradient? {
                let n = Int(r.u32())
                var colors: [CGColor] = [], locations: [CGFloat] = []
                for _ in 0..<n {
                    colors.append(color())
                    locations.append(f())
                }
                return CGGradient(colorsSpace: CGColorSpace(name: CGColorSpace.sRGB), colors: colors as CFArray, locations: locations)
            }
            func clipPath(_ p: CGPath) {
                ctx.addPath(p)
                ctx.clip(using: evenOdd ? .evenOdd : .winding)
            }
            func strokeStyle() {
                let w = f()
                let cap = r.u8(), join = r.u8()
                let dashes = Int(r.u32())
                let phase = f()
                var lengths: [CGFloat] = []
                for _ in 0..<dashes { lengths.append(f()) }
                ctx.setLineWidth(w)
                ctx.setLineCap(cap == 1 ? .round : cap == 2 ? .square : .butt)
                ctx.setLineJoin(join == 1 ? .round : join == 2 ? .bevel : .miter)
                if !lengths.isEmpty { ctx.setLineDash(phase: phase, lengths: lengths) }
            }
            func color() -> CGColor { (asukaColor(r.u64()) ?? .clear).resolvedColor(with: traitCollection).cgColor }
            func path() -> CGPath {
                let n = min(Int(r.u32()), r.p.distance(to: r.end))
                let b = UnsafeRawBufferPointer(start: r.p, count: n)
                r.p += n
                return AsukaShapeView.path(b)
            }
            while r.p < r.end {
                switch AsukaCanvasOp(rawValue: Int32(r.u8())) {
                case .save: ctx.saveGState(); rules.append(evenOdd)
                case .restore: ctx.restoreGState(); if let e = rules.popLast() { evenOdd = e }
                case .translate: ctx.translateBy(x: f(), y: f())
                case .scale: ctx.scaleBy(x: f(), y: f())
                case .rotate: ctx.rotate(by: f() * .pi / 180)
                case .alpha: ctx.setAlpha(f())
                case .clip:
                    clipPath(path())
                case .fill:
                    let c = color()
                    ctx.addPath(path())
                    ctx.setFillColor(c)
                    ctx.fillPath(using: evenOdd ? .evenOdd : .winding)
                case .stroke:
                    let c = color()
                    let w = f()
                    let cap = r.u8(), join = r.u8()
                    let dashes = Int(r.u32())
                    let phase = f()
                    var lengths: [CGFloat] = []
                    for _ in 0..<dashes { lengths.append(f()) }
                    ctx.saveGState()
                    ctx.setStrokeColor(c)
                    ctx.setLineWidth(w)
                    ctx.setLineCap(cap == 1 ? .round : cap == 2 ? .square : .butt)
                    ctx.setLineJoin(join == 1 ? .round : join == 2 ? .bevel : .miter)
                    if !lengths.isEmpty { ctx.setLineDash(phase: phase, lengths: lengths) }
                    ctx.addPath(path())
                    ctx.strokePath()
                    ctx.restoreGState()
                case .gradient:
                    let n = Int(r.u32())
                    var colors: [CGColor] = [], locations: [CGFloat] = []
                    for _ in 0..<n {
                        colors.append(color())
                        locations.append(f())
                    }
                    let s = CGPoint(x: f(), y: f()), e = CGPoint(x: f(), y: f())
                    let p = path()
                    guard let g = CGGradient(colorsSpace: CGColorSpace(name: CGColorSpace.sRGB), colors: colors as CFArray, locations: locations) else { continue }
                    ctx.saveGState()
                    clipPath(p)
                    ctx.drawLinearGradient(g, start: s, end: e, options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
                    ctx.restoreGState()
                case .transform:
                    let a = f(), b = f(), c = f(), d = f(), tx = f(), ty = f()
                    ctx.concatenate(CGAffineTransform(a: a, b: b, c: c, d: d, tx: tx, ty: ty))
                case .fillRule:
                    evenOdd = r.u8() == 1
                case .radialGradient:
                    let g = stops()
                    let c = CGPoint(x: f(), y: f()), rad = f(), focus = CGPoint(x: f(), y: f())
                    let p = path()
                    guard let g else { continue }
                    ctx.saveGState()
                    clipPath(p)
                    ctx.drawRadialGradient(g, startCenter: focus, startRadius: 0, endCenter: c, endRadius: rad, options: [.drawsAfterEndLocation])
                    ctx.restoreGState()
                case .strokeGradient:
                    ctx.saveGState()
                    strokeStyle()
                    let radial = r.u8() == 1
                    let g = stops()
                    var a = CGPoint(x: f(), y: f()), b = CGPoint.zero, rad: CGFloat = 0
                    if radial { rad = f(); b = CGPoint(x: f(), y: f()) } else { b = CGPoint(x: f(), y: f()) }
                    ctx.addPath(path())
                    ctx.replacePathWithStrokedPath()
                    ctx.clip()
                    if let g {
                        if radial {
                            swap(&a, &b) // from the focus out to the center's circle
                            ctx.drawRadialGradient(g, startCenter: a, startRadius: 0, endCenter: b, endRadius: rad, options: [.drawsAfterEndLocation])
                        } else {
                            ctx.drawLinearGradient(g, start: a, end: b, options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
                        }
                    }
                    ctx.restoreGState()
                case .text:
                    let c = UIColor(cgColor: color())
                    let size = f()
                    let weight = uiWeight(Int32(r.u8())) ?? .regular
                    let align = r.u8()
                    var x = f()
                    let y = f()
                    let font = UIFont.systemFont(ofSize: size, weight: weight)
                    let s = NSAttributedString(string: r.string(), attributes: [.font: font, .foregroundColor: c])
                    let w = s.size().width
                    if align == UInt8(AsukaTextAlign.center.rawValue) { x -= w / 2 }
                    if align == UInt8(AsukaTextAlign.end.rawValue) { x -= w }
                    s.draw(at: CGPoint(x: x, y: y - font.ascender))
                default:
                    return
                }
            }
        }
    }
}
#endif
