#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A Toggle that's a checkbox: iOS has none of its own, so it's a circle
/// that fills with a checkmark in the tint, as Reminders' and Notes'
/// lists show it. VoiceOver reads it as a toggle button, checked or not.
final class AsukaCheckbox: UIButton {
    var isOn = false { didSet { update() } }

    convenience init(_ onChange: @escaping (Bool) -> Void) {
        self.init(configuration: .plain())
        // Its circle alone, aligned with what's beside it; touches reach
        // it from 44 points around (point(inside:)).
        configuration?.contentInsets = .zero
        configuration?.imagePadding = 0
        configuration?.preferredSymbolConfigurationForImage = UIImage.SymbolConfiguration(textStyle: .title2)
        addAction(UIAction { [weak self] _ in
            guard let self else { return }
            self.isOn.toggle()
            onChange(self.isOn)
        }, for: .primaryActionTriggered)
        update()
    }

    override func point(inside p: CGPoint, with event: UIEvent?) -> Bool {
        bounds.insetBy(dx: min(0, (bounds.width - 44) / 2), dy: min(0, (bounds.height - 44) / 2)).contains(p)
    }

    private func update() {
        configuration?.image = UIImage(systemName: isOn ? "checkmark.circle.fill" : "circle")
        configuration?.baseForegroundColor = isOn ? nil : .tertiaryLabel // the tint when on
        // Read as a toggle, checked or not, not as its symbol's name.
        accessibilityLabel = ""
        if #available(iOS 17.0, *) { accessibilityTraits = [.button, .toggleButton] }
        accessibilityValue = isOn ? "1" : "0"
    }
}

@MainActor extension AsukaRuntime {
    /// Makes a Toggle a checkbox: its switch is replaced.
    func makeCheckbox(_ n: AsukaNode) {
        guard let old = n.view as? UISwitch else { return }
        let id = n.id
        let c = AsukaCheckbox { [weak self] on in self?.emit(id, .toggled) { AsukaEvents.toggled(id, on) } }
        c.isOn = old.isOn
        c.isEnabled = old.isEnabled
        c.tintColor = old.onTintColor
        if let p = old.superview {
            p.insertSubview(c, aboveSubview: old)
            c.frame = old.frame
            old.removeFromSuperview()
        }
        n.view = c
    }
}
#endif
