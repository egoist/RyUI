#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// A Wheel: a UIPickerView of one component, its rows the core's items.
final class RyUIWheelView: UIPickerView, UIPickerViewDataSource, UIPickerViewDelegate {
    var items: [String] = [] { didSet { reloadAllComponents() } }
    var picked: ((Int) -> Void)?

    override init(frame: CGRect) {
        super.init(frame: frame)
        dataSource = self
        delegate = self
    }

    required init?(coder: NSCoder) { fatalError() }

    func numberOfComponents(in p: UIPickerView) -> Int { 1 }
    func pickerView(_ p: UIPickerView, numberOfRowsInComponent c: Int) -> Int { items.count }
    func pickerView(_ p: UIPickerView, titleForRow row: Int, forComponent c: Int) -> String? { items[row] }
    func pickerView(_ p: UIPickerView, didSelectRow row: Int, inComponent c: Int) { picked?(row) }
}

@MainActor extension RyUIRuntime {
    func makeWheel(_ id: UInt32) -> UIView {
        let w = RyUIWheelView()
        w.picked = { [weak self] i in self?.emit(id, .selected) { RyUIEvents.selected(id, Int64(i)) } }
        return w
    }

    func makeStepper(_ id: UInt32) -> UIView {
        let s = UIStepper()
        s.addAction(UIAction { [weak self, weak s] _ in
            guard let s else { return }
            let v = s.value
            self?.emit(id, .valueChanged) { RyUIEvents.valueChanged(id, v) }
        }, for: .valueChanged)
        return s
    }

    func makeColorPicker(_ id: UInt32) -> UIView {
        let w = UIColorWell()
        w.supportsAlpha = false
        w.addAction(UIAction { [weak self, weak w] _ in
            guard let w, let c = w.selectedColor else { return }
            let v = ryuiRGBA(c.resolvedColor(with: w.traitCollection))
            self?.emit(id, .colorChanged) { RyUIEvents.colorChanged(id, v) }
        }, for: .valueChanged)
        return w
    }

    func setWheelProp(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) -> Bool {
        switch (n.view, prop) {
        case (let w as RyUIWheelView, RyUIProp.Wheel.items):
            guard case .bytes(let b) = v, b.count >= 4 else { return true }
            var r = RyUIReader(b)
            let selected = w.selectedRow(inComponent: 0)
            w.items = (0..<Int(r.u32())).map { _ in r.string() }
            if selected >= 0, selected < w.items.count { w.selectRow(selected, inComponent: 0, animated: false) }
        case (let w as RyUIWheelView, RyUIProp.Wheel.selected):
            let i = Int(v.int)
            if i >= 0, i < w.items.count { w.selectRow(i, inComponent: 0, animated: w.window != nil) }
        case (let s as UIStepper, RyUIProp.Stepper.value): s.value = v.float
        case (let s as UIStepper, RyUIProp.Stepper.min): s.minimumValue = v.float
        case (let s as UIStepper, RyUIProp.Stepper.max): s.maximumValue = v.float
        case (let s as UIStepper, RyUIProp.Stepper.step): s.stepValue = v.float
        case (let w as UIColorWell, RyUIProp.ColorPicker.value): w.selectedColor = ryuiColor(v.color)
        case (let w as UIColorWell, RyUIProp.ColorPicker.alpha): w.supportsAlpha = v.bool
        case (let w as UIColorWell, RyUIProp.ColorPicker.title): w.title = v.string
        default: return false
        }
        return true
    }
}
/// A color as RyUI's RGBA Color (ops.RGBA): sRGB, each channel clamped
/// (the picker's wide-gamut colors fall back to their nearest).
func ryuiRGBA(_ c: UIColor) -> Int64 {
    var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
    c.getRed(&r, green: &g, blue: &b, alpha: &a)
    func u8(_ x: CGFloat) -> Int64 { Int64((min(max(x, 0), 1) * 255).rounded()) }
    return 2 << 32 | u8(r) << 24 | u8(g) << 16 | u8(b) << 8 | u8(a)
}
#endif
