#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A ProgressRing. UIKit has no determinate circle, and SwiftUI's
/// circular ProgressView ignores its value on iOS (it spins), so it draws
/// what SwiftUI's determinate circle, the circular capacity Gauge, draws:
/// a round-capped stroke in the tint, clockwise from the top, over a
/// track of the tint at 30%, its width a tenth of the ring's size (58
/// points by default, a 6-point stroke). Value changes animate. Without a
/// value it's a UIActivityIndicatorView, as SwiftUI's circular
/// ProgressView is. VoiceOver reads it as UIProgressView: its percentage,
/// updating as it changes.
final class AsukaProgressRing: UIView {
    /// The circular Gauge's size, in points.
    static let size: CGFloat = 58
    private let track = CAShapeLayer()
    private let fill = CAShapeLayer()
    private(set) var spinner: UIActivityIndicatorView?
    var color: UIColor? { didSet { colors() } }
    private(set) var value: CGFloat = 0

    override init(frame: CGRect) {
        super.init(frame: frame)
        for l in [track, fill] {
            l.fillColor = nil
            l.lineCap = .round
            layer.addSublayer(l)
        }
        fill.strokeEnd = 0
        isAccessibilityElement = true
        accessibilityTraits = .updatesFrequently
        accessibilityValue = Self.percent(0)
        colors()
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Indeterminate: the system's spinner in place of the ring.
    var indeterminate: Bool {
        get { spinner != nil }
        set {
            guard newValue != indeterminate else { return }
            if newValue {
                let s = UIActivityIndicatorView(style: .medium)
                s.color = color ?? .secondaryLabel
                s.startAnimating()
                s.isAccessibilityElement = false // the ring is the element
                addSubview(s)
                spinner = s
                setNeedsLayout()
            } else {
                spinner?.removeFromSuperview()
                spinner = nil
            }
            track.isHidden = newValue
            fill.isHidden = newValue
            accessibilityValue = newValue ? nil : Self.percent(value)
            accessibilityTraits = newValue ? [] : .updatesFrequently
        }
    }

    /// Sets the value, from 0 to 1: animated as the core's animation
    /// runs, or as UIProgressView's setProgress(_:animated:) does.
    func set(_ v: CGFloat, _ animation: (Double, Int32)?) {
        value = v
        accessibilityValue = indeterminate ? nil : Self.percent(v)
        guard window != nil, !UIAccessibility.isReduceMotionEnabled || animation != nil else {
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            fill.strokeEnd = v
            CATransaction.commit()
            return
        }
        let from = fill.presentation()?.strokeEnd ?? fill.strokeEnd
        let a: CABasicAnimation
        if let anim = animation, AsukaAnimationCurve(rawValue: anim.1) == .spring {
            let s = CASpringAnimation(keyPath: "strokeEnd")
            s.damping = 18
            s.stiffness = 180
            s.duration = s.settlingDuration
            a = s
        } else {
            a = CABasicAnimation(keyPath: "strokeEnd")
            a.duration = animation?.0 ?? 0.25
            a.timingFunction = CAMediaTimingFunction(name: {
                switch animation.flatMap({ AsukaAnimationCurve(rawValue: $0.1) }) {
                case .linear: return .linear
                case .easeIn: return .easeIn
                case .easeOut: return .easeOut
                default: return .easeInEaseOut
                }
            }())
        }
        a.fromValue = from
        a.toValue = v
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        fill.strokeEnd = v
        CATransaction.commit()
        fill.add(a, forKey: "strokeEnd")
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let side = min(bounds.width, bounds.height)
        let w = side / 10
        let c = CGPoint(x: bounds.midX, y: bounds.midY)
        // From the top, clockwise on screen (y down: Core Graphics'
        // clockwise flag set true), whichever way the text runs.
        let path = UIBezierPath(arcCenter: c, radius: max(side / 2 - w / 2, 0),
                                startAngle: -.pi / 2, endAngle: .pi * 1.5, clockwise: true).cgPath
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        for l in [track, fill] {
            l.frame = bounds
            l.path = path
            l.lineWidth = w
        }
        CATransaction.commit()
        if let s = spinner {
            s.center = c
        }
    }

    /// The size it takes: the Gauge's, or the spinner's.
    override func sizeThatFits(_ size: CGSize) -> CGSize {
        if let s = spinner { return s.intrinsicContentSize }
        return CGSize(width: Self.size, height: Self.size)
    }

    private func colors() {
        let tint = color ?? tintColor ?? .tintColor
        traitCollection.performAsCurrent {
            fill.strokeColor = tint.cgColor
            track.strokeColor = tint.withAlphaComponent(0.3).cgColor
        }
        spinner?.color = color ?? .secondaryLabel
    }

    override func tintColorDidChange() {
        super.tintColorDidChange()
        colors()
    }

    override func traitCollectionDidChange(_ previous: UITraitCollection?) {
        super.traitCollectionDidChange(previous)
        colors() // a CGColor is one appearance's
    }

    /// Read as UIKit's own are, unless the app labels it: "Progress",
    /// as a UIProgressView, and its percentage; without a value, the
    /// spinner's "In progress". UIKit's words, so in the user's language.
    override var accessibilityLabel: String? {
        get { super.accessibilityLabel ?? (spinner?.accessibilityLabel ?? Self.progressLabel) }
        set { super.accessibilityLabel = newValue }
    }

    private static let progressLabel = UIProgressView(progressViewStyle: .default).accessibilityLabel

    private static let formatter: NumberFormatter = {
        let f = NumberFormatter()
        f.numberStyle = .percent
        f.maximumFractionDigits = 0
        return f
    }()

    /// What VoiceOver reads, as UIProgressView's: "50%", in the user's locale.
    static func percent(_ v: CGFloat) -> String {
        formatter.string(from: NSNumber(value: Double(v))) ?? ""
    }
}
#endif
