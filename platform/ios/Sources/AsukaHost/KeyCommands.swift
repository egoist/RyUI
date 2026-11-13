#if canImport(UIKit)
import UIKit
#if canImport(AsukaOps)
import AsukaOps
#endif

// A hardware keyboard's keys (an iPad's, a Bluetooth one, the
// simulator's Mac keyboard), as the Keys prop routes them: from the
// focused view out through its ancestors, the nearest whose Keys has the
// key takes it, ahead of the focused control.
//
// As UIKeyCommands, which AsukaViewController gives for the responder
// chain: UIKit asks the chain from the first responder (or, with none,
// the key window's root controller) for its key commands as a key goes
// down and runs the one that matches, and wantsPriorityOverSystemBehavior
// puts it ahead of what the system does with the key (a text field's
// caret on the arrows and on Control-N and Control-P, its deleting, Tab's
// focus moving). A pressesBegan above the first responder couldn't: a
// UITextField takes the arrows' presses itself, never passing them up.
// Held, a key command repeats with the keyboard's repeat. They have no
// title, so the ⌘-held shortcuts overlay doesn't list them. While an
// input method composes text (marked text), there are none: its keys are
// its own.

extension AsukaNode {
    /// Its Keys as key commands, in the Keys prop's order: the key's
    /// index is its command's.
    func setKeys(_ keys: [AsukaKey]) {
        let id = id
        keyCommands = keys.enumerated().compactMap { i, k in
            guard let input = uiKeyInput(k.name) else { return nil }
            let c = UIKeyCommand(title: "", action: #selector(AsukaViewController.asukaKey(_:)), input: input,
                                 modifierFlags: uiKeyModifiers(k.mods), propertyList: [Int(id), i])
            c.wantsPriorityOverSystemBehavior = true
            // The key as declared: never mirrored for right to left
            // (left is the left arrow), nor moved to another key on
            // another keyboard layout.
            c.allowsAutomaticMirroring = false
            c.allowsAutomaticLocalization = false
            if #available(iOS 26.0, *) { c.repeatBehavior = .repeatable }
            return c
        }
    }
}

/// UIKeyCommand's input for a Keys name: a named key's, or the character.
func uiKeyInput(_ name: String) -> String? {
    switch name {
    case "enter": return "\r"
    case "escape": return UIKeyCommand.inputEscape
    case "tab": return "\t"
    case "space": return " "
    case "delete": return UIKeyCommand.inputDelete
    case "backspace": return "\u{8}"
    case "up": return UIKeyCommand.inputUpArrow
    case "down": return UIKeyCommand.inputDownArrow
    case "left": return UIKeyCommand.inputLeftArrow
    case "right": return UIKeyCommand.inputRightArrow
    case "home": return UIKeyCommand.inputHome
    case "end": return UIKeyCommand.inputEnd
    case "pageup": return UIKeyCommand.inputPageUp
    case "pagedown": return UIKeyCommand.inputPageDown
    case "f1": return UIKeyCommand.f1
    case "f2": return UIKeyCommand.f2
    case "f3": return UIKeyCommand.f3
    case "f4": return UIKeyCommand.f4
    case "f5": return UIKeyCommand.f5
    case "f6": return UIKeyCommand.f6
    case "f7": return UIKeyCommand.f7
    case "f8": return UIKeyCommand.f8
    case "f9": return UIKeyCommand.f9
    case "f10": return UIKeyCommand.f10
    case "f11": return UIKeyCommand.f11
    case "f12": return UIKeyCommand.f12
    default:
        return name.unicodeScalars.count == 1 ? name : nil
    }
}

/// The modifier flags of Keys' bits: 1 primary (⌘), 2 shift, 4 alt (⌥),
/// 8 control.
func uiKeyModifiers(_ mods: UInt8) -> UIKeyModifierFlags {
    var f: UIKeyModifierFlags = []
    if mods & 1 != 0 { f.insert(.command) }
    if mods & 2 != 0 { f.insert(.shift) }
    if mods & 4 != 0 { f.insert(.alternate) }
    if mods & 8 != 0 { f.insert(.control) }
    return f
}

/// Finds the first responder: an action sent to no target goes to it.
private weak var asukaFirstResponder: UIResponder?

extension UIResponder {
    @objc fileprivate func asukaFindFirstResponder(_ sender: Any?) {
        asukaFirstResponder = self
    }
}

extension AsukaRuntime {
    /// The key commands of the Keys of the views the keyboard's focus is
    /// in: the first responder's node's, then its ancestors' out to the
    /// root (the root's alone when nothing of the app's has the focus).
    /// Of two with the same key, the nearer's. UIKit asks as a key goes
    /// down, so they follow the focus and the props as they are then.
    func keyCommands() -> [UIKeyCommand] {
        guard !keyed.isEmpty else { return [] }
        asukaFirstResponder = nil
        UIApplication.shared.sendAction(#selector(UIResponder.asukaFindFirstResponder(_:)), to: nil, from: nil, for: nil)
        let first = asukaFirstResponder
        asukaFirstResponder = nil
        if let t = first as? UITextInput, t.markedTextRange != nil { return [] } // an input method composing
        var n = first.flatMap(node(of:)) ?? nodes[rootID]
        var out: [UIKeyCommand] = []
        var seen = Set<String>()
        while let m = n {
            for c in m.keyCommands where seen.insert("\(c.modifierFlags.rawValue) \(c.input ?? "")").inserted {
                out.append(c)
            }
            // A list's row: its cell has no parent node, its list's view
            // holds it.
            n = m.parent ?? m.view.superview.flatMap(node(of:))
        }
        return out
    }

    /// The node of the nearest responder from r out (its views'
    /// superviews, a screen's controller) that's one of the app's views.
    func node(of r: UIResponder) -> AsukaNode? {
        var r: UIResponder? = r
        while let x = r {
            let v = (x as? UIView) ?? (x as? UIViewController)?.viewIfLoaded
            if let v, v.tag > 0, let n = nodes[UInt32(truncatingIfNeeded: v.tag)], n.view === v || n.vc === x {
                return n
            }
            r = x.next
        }
        return nil
    }

    /// A key command of a node's Keys ran: Key, with the key's index.
    func keyCommand(_ c: UIKeyCommand) {
        guard let p = c.propertyList as? [Int], p.count == 2 else { return }
        let id = UInt32(truncatingIfNeeded: p[0])
        let i = Int64(p[1])
        emit(id, .key) { AsukaEvents.key(id, i) }
    }

    /// Sets a node's Keys prop.
    func setKeys(_ n: AsukaNode, _ v: AsukaValue) {
        let keys: [AsukaKey]
        if case .bytes(let b) = v { keys = AsukaKeys.decode(b) } else { keys = [] }
        n.setKeys(keys)
        if n.keyCommands.isEmpty { keyed.remove(n.id) } else { keyed.insert(n.id) }
    }
}
#endif
