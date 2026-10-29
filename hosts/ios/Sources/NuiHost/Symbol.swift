#if canImport(UIKit)
import UIKit
#if canImport(NuiOps)
import NuiOps
#endif

/// A Symbol: an SF Symbol at its node's font, made with the app's text
/// size (as rich text measures) so that what's measured is what's drawn.
final class NuiSymbolView: UIImageView {
    var name = "" { didSet { if name != oldValue { refresh() } } }
    weak var node: NuiNode?

    override init(frame: CGRect) {
        super.init(frame: frame)
        contentMode = .center
        tintColor = .label
        isAccessibilityElement = false // decoration, unless the app labels it
        if #available(iOS 17, *) {
            registerForTraitChanges([UITraitPreferredContentSizeCategory.self]) { (v: NuiSymbolView, _: UITraitCollection) in v.refresh() }
        }
    }
    required init?(coder: NSCoder) { fatalError() }

    func refresh() {
        guard !name.isEmpty else { image = nil; return }
        let traits = UITraitCollection(traitsFrom: [traitCollection, UITraitCollection(preferredContentSizeCategory: UIApplication.shared.preferredContentSizeCategory)])
        var font = UIFont.preferredFont(forTextStyle: .body)
        traits.performAsCurrent { font = node?.font ?? .preferredFont(forTextStyle: .body, compatibleWith: traits) }
        image = UIImage(systemName: name, withConfiguration: UIImage.SymbolConfiguration(font: font, scale: .medium))
    }
}
#endif

#if canImport(UIKit)
/// Holds a compact UIDatePicker, which computes NaN frames when UIKit
/// resizes it from a zero size: it's laid out here, only once there's room.
final class NuiDatePickerView: UIView {
    let picker = UIDatePicker()

    override init(frame: CGRect) {
        super.init(frame: frame)
        autoresizesSubviews = false
        picker.preferredDatePickerStyle = .compact
        picker.datePickerMode = .date
        picker.autoresizingMask = []
        addSubview(picker)
    }
    required init?(coder: NSCoder) { fatalError() }

    /// Its own size: a compact picker asked to fit a size takes all of it.
    override func sizeThatFits(_ size: CGSize) -> CGSize {
        picker.systemLayoutSizeFitting(UIView.layoutFittingCompressedSize)
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard bounds.width > 0, bounds.height > 0 else { return }
        // At its own size, at the end (as a trailing control sits).
        let s = sizeThatFits(bounds.size)
        let w = min(s.width, bounds.width)
        picker.frame = CGRect(x: bounds.width - w, y: (bounds.height - s.height) / 2, width: w, height: s.height)
    }

    override var tintColor: UIColor! {
        get { super.tintColor }
        set { super.tintColor = newValue; picker.tintColor = newValue }
    }
}
#endif

#if canImport(UIKit)
@MainActor extension NuiNode {
    /// A background gradient (Style.Gradient): a layer under the subviews.
    func setGradient(_ b: UnsafeRawBufferPointer) {
        var r = NuiReader(b)
        let n = b.count >= 4 ? Int(r.u32()) : 0
        guard n > 0 else {
            gradient?.removeFromSuperlayer()
            gradient = nil
            return
        }
        var colors: [UIColor?] = []
        var locations: [NSNumber] = []
        for _ in 0..<n {
            colors.append(nuiColor(r.u64()))
            locations.append(NSNumber(value: r.f32()))
        }
        let g = gradient ?? CAGradientLayer()
        if gradient == nil {
            view.layer.insertSublayer(g, at: 0)
            gradient = g
        }
        gradientColors = colors
        g.locations = locations
        g.startPoint = CGPoint(x: CGFloat(r.f32()), y: CGFloat(r.f32()))
        g.endPoint = CGPoint(x: CGFloat(r.f32()), y: CGFloat(r.f32()))
        resolveLayerColors()
        updateLayers()
    }

    /// Resolves the layers' dynamic colors with the view's traits (dark mode).
    func resolveLayerColors() {
        let t = view.traitCollection
        if borderColor != 0 { view.layer.borderColor = nuiColor(borderColor)?.resolvedColor(with: t).cgColor }
        gradient?.colors = gradientColors.map { ($0 ?? .clear).resolvedColor(with: t).cgColor }
        if let c = shadowColor { view.layer.shadowColor = c.resolvedColor(with: t).cgColor }
    }

    /// Fits the shadow's path and the gradient to the view's bounds.
    func updateLayers() {
        let b = view.bounds
        if let g = gradient {
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            g.frame = b
            g.cornerRadius = view.layer.cornerRadius
            g.cornerCurve = view.layer.cornerCurve
            CATransaction.commit()
        }
        if view.layer.shadowOpacity > 0 {
            view.layer.shadowPath = UIBezierPath(roundedRect: b, cornerRadius: view.layer.cornerRadius).cgPath
        }
    }
}

@MainActor extension NuiRuntime {
    /// Layer colors (borders, shadows, gradients) don't follow dark mode
    /// as views' do: they're resolved again when it changes.
    func resolveLayerColors() {
        for n in nodes.values { n.resolveLayerColors() }
    }
}
#endif
