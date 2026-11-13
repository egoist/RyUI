#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A Picker: SwiftUI's Picker in its menu style, a plain UIButton showing
/// the chosen item, tinted, with the pop-up indicator (up and down
/// chevrons), whose menu chooses (changesSelectionAsPrimaryAction: the
/// menu checks the chosen item and the button takes its title). With
/// none chosen it shows its label, and UIKit, which would choose the
/// first, doesn't manage the selection until the user chooses.
final class AsukaPickerButton: UIButton {
    var items: [String] = []
    var chosen = -1
    var label = ""
    var picked: ((Int) -> Void)?

    static func make() -> AsukaPickerButton {
        var c = UIButton.Configuration.plain()
        c.indicator = .popup
        c.titleLineBreakMode = .byTruncatingTail
        let b = AsukaPickerButton(configuration: c)
        b.showsMenuAsPrimaryAction = true
        return b
    }

    /// Builds the menu again from items, chosen and label.
    func refresh() {
        let has = chosen >= 0 && chosen < items.count
        let actions = items.enumerated().map { i, title in
            UIAction(title: title, state: has && i == chosen ? .on : .off) { [weak self] _ in
                guard let self else { return }
                let was = self.chosen
                self.chosen = i
                if was != i {
                    if !self.changesSelectionAsPrimaryAction { self.refresh() } // none was chosen
                    else { self.describe() }
                    self.picked?(i)
                }
            }
        }
        // The menu's choice changes the title; set it first, so it's
        // never the first item's when none is chosen.
        changesSelectionAsPrimaryAction = false
        var c = configuration ?? .plain()
        c.title = has ? items[chosen] : label
        configuration = c
        menu = UIMenu(options: .singleSelection, children: actions)
        changesSelectionAsPrimaryAction = has
        describe()
    }

    /// VoiceOver reads the label, then the chosen item: "Sort by, Name,
    /// pop up button".
    func describe() {
        let has = chosen >= 0 && chosen < items.count
        accessibilityLabel = label.isEmpty ? nil : label
        accessibilityValue = has && !label.isEmpty ? items[chosen] : nil
        invalidateIntrinsicContentSize()
    }
}

@MainActor extension AsukaRuntime {
    func makePicker(_ id: UInt32) -> UIView {
        let b = AsukaPickerButton.make()
        b.picked = { [weak self] i in self?.emit(id, .selected) { AsukaEvents.selected(id, Int64(i)) } }
        b.refresh()
        return b
    }

    func setPickerProp(_ n: AsukaNode, _ prop: UInt16, _ v: AsukaValue) -> Bool {
        guard n.kind == .picker, let b = n.view as? AsukaPickerButton else { return false }
        switch prop {
        case AsukaProp.Picker.items:
            guard case .bytes(let d) = v, d.count >= 4 else { return true }
            var r = AsukaReader(d)
            b.items = (0..<Int(r.u32())).map { _ in r.string() }
        case AsukaProp.Picker.selected:
            b.chosen = Int(v.int)
        case AsukaProp.Picker.label:
            b.label = v.string
        default:
            return false
        }
        b.refresh()
        return true
    }
}
#endif
