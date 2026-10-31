#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A Shape: a CAShapeLayer, its path and trim the core's; inside an
/// Animate, the trim and the path animate (UIView animations don't
/// reach a shape layer's).
final class RyUIShapeView: UIView {
    override class var layerClass: AnyClass { CAShapeLayer.self }
    var shape: CAShapeLayer { layer as! CAShapeLayer }
    var fill: UIColor? { didSet { colors() } }
    var stroke: UIColor? { didSet { colors() } }

    override init(frame: CGRect) {
        super.init(frame: frame)
        shape.fillColor = nil
        shape.strokeColor = nil
        isAccessibilityElement = false
    }

    required init?(coder: NSCoder) { fatalError() }

    private func colors() {
        traitCollection.performAsCurrent {
            shape.fillColor = fill?.cgColor
            shape.strokeColor = stroke?.cgColor
        }
    }

    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        colors() // a CGColor is one appearance's
    }

    /// Sets a shape layer property, animated if an animation is running.
    func set(_ key: String, _ value: Any?, _ animation: (Double, Int32)?) {
        if let (duration, curve) = animation, window != nil {
            let from = shape.presentation()?.value(forKeyPath: key) ?? shape.value(forKeyPath: key)
            let a: CABasicAnimation
            if RyUIAnimationCurve(rawValue: curve) == .spring {
                let s = CASpringAnimation(keyPath: key)
                s.damping = 18
                s.stiffness = 180
                s.duration = s.settlingDuration
                a = s
            } else {
                a = CABasicAnimation(keyPath: key)
                a.duration = duration
                a.timingFunction = CAMediaTimingFunction(name: {
                    switch RyUIAnimationCurve(rawValue: curve) {
                    case .linear: return .linear
                    case .easeIn: return .easeIn
                    case .easeOut: return .easeOut
                    default: return .easeInEaseOut
                    }
                }())
            }
            a.fromValue = from
            a.toValue = value
            shape.add(a, forKey: key)
        }
        shape.setValue(value, forKeyPath: key)
    }

    /// The path from its wire format: u8 PathOp, then its f32 arguments.
    static func path(_ b: UnsafeRawBufferPointer) -> CGPath {
        let p = CGMutablePath()
        var r = RyUIReader(b)
        func f() -> CGFloat { CGFloat(r.f32()) }
        while r.p < r.end {
            switch RyUIPathOp(rawValue: Int32(r.u8())) {
            case .move: p.move(to: CGPoint(x: f(), y: f()))
            case .line: p.addLine(to: CGPoint(x: f(), y: f()))
            case .quad:
                let c = CGPoint(x: f(), y: f())
                p.addQuadCurve(to: CGPoint(x: f(), y: f()), control: c)
            case .cubic:
                let c1 = CGPoint(x: f(), y: f()), c2 = CGPoint(x: f(), y: f())
                p.addCurve(to: CGPoint(x: f(), y: f()), control1: c1, control2: c2)
            case .close: p.closeSubpath()
            case .rect: p.addRect(CGRect(x: f(), y: f(), width: f(), height: f()))
            case .roundedRect:
                let rect = CGRect(x: f(), y: f(), width: f(), height: f())
                let rad = min(f(), rect.width / 2, rect.height / 2)
                p.addRoundedRect(in: rect, cornerWidth: rad, cornerHeight: rad)
            case .ellipse: p.addEllipse(in: CGRect(x: f(), y: f(), width: f(), height: f()))
            case .arc:
                let c = CGPoint(x: f(), y: f())
                let rad = f(), start = f(), end = f(), cw = f() != 0
                // Screen coordinates: y down, so the screen's clockwise is
                // Core Graphics' counterclockwise flag set false.
                p.addArc(center: c, radius: rad, startAngle: start, endAngle: end, clockwise: !cw)
            default: return p
            }
        }
        return p
    }
}

@MainActor extension RyUIRuntime {
    func setShapeProp(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) -> Bool {
        guard let s = n.view as? RyUIShapeView else { return false }
        switch prop {
        case RyUIProp.Shape.path:
            guard case .bytes(let b) = v else { return true }
            s.set("path", RyUIShapeView.path(b), animation)
        case RyUIProp.Shape.fill: s.fill = ryuiColor(v.color)
        case RyUIProp.Shape.stroke: s.stroke = ryuiColor(v.color)
        case RyUIProp.Shape.lineWidth: s.shape.lineWidth = CGFloat(v.float)
        case RyUIProp.Shape.lineCap:
            s.shape.lineCap = RyUILineCap(rawValue: v.enumValue) == .round ? .round : RyUILineCap(rawValue: v.enumValue) == .square ? .square : .butt
        case RyUIProp.Shape.lineJoin:
            s.shape.lineJoin = RyUILineJoin(rawValue: v.enumValue) == .round ? .round : RyUILineJoin(rawValue: v.enumValue) == .bevel ? .bevel : .miter
        case RyUIProp.Shape.trimStart: s.set("strokeStart", CGFloat(v.float), animation)
        case RyUIProp.Shape.trimEnd: s.set("strokeEnd", CGFloat(v.float), animation)
        case RyUIProp.Shape.dash:
            // f32 phase, then the lengths.
            guard case .bytes(let b) = v, b.count >= 8 else { s.shape.lineDashPattern = nil; return true }
            var r = RyUIReader(b)
            s.shape.lineDashPhase = CGFloat(r.f32())
            var lengths = [NSNumber]()
            while r.p + 4 <= r.end { lengths.append(NSNumber(value: r.f32())) }
            s.shape.lineDashPattern = lengths
        default: return false
        }
        return true
    }
}
#endif
