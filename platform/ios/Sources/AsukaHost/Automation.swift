#if canImport(UIKit) && DEBUG
import UIKit
import notify
#if canImport(AsukaOps)
import AsukaOps
#endif

/// Drives a debug build from outside, for development and tests:
/// write commands to <data container>/tmp/asuka-commands.txt, then post
/// the dev.asuka.automation notification:
///
///	xcrun simctl spawn booted notifyutil -p dev.asuka.automation
///
/// Commands, one per line: `tap <text>` presses the Button, View or
/// Toggle whose title, text or accessibility label is <text>; `type
/// <text>` replaces the focused text field's text (or the first text
/// field's); `submit` presses return in it; `switch <n>` flips the
/// n-th switch (from 0, in creation order); `scroll <points>` scrolls
/// the first vertical scroll view or list down by that much; `dump`
/// writes what the screen shows (each label's text, button's title or
/// view's accessibility label, a line each, in creation order) to
/// tmp/asuka-dump.txt; `back` pops the shown stack's top screen, as its
/// back button does; `tab <title>` selects the tab with that title, as
/// tapping it does.
@MainActor enum AsukaAutomation {
    static var token: Int32 = 0

    static func start() {
        notify_register_dispatch("dev.asuka.automation", &token, .main) { _ in
            MainActor.assumeIsolated { run() }
        }
    }

    static func run() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("asuka-commands.txt")
        guard let text = try? String(contentsOf: url, encoding: .utf8) else { return }
        try? FileManager.default.removeItem(at: url)
        for line in text.split(separator: "\n") {
            let parts = line.split(separator: " ", maxSplits: 1)
            guard let cmd = parts.first else { continue }
            let arg = parts.count > 1 ? String(parts[1]) : ""
            switch cmd {
            case "tap": tap(arg)
            case "type": type(arg)
            case "submit": submit()
            case "switch": flip(Int(arg) ?? 0)
            case "scroll": scroll(CGFloat(Double(arg) ?? 0))
            case "dump": dump()
            case "back": back()
            case "tab": tab(arg)
            default: break
            }
        }
    }

    static var rt: AsukaRuntime { AsukaRuntime.shared }

    /// The containers the core made (stacks, tabs) that are on screen,
    /// newest last (a sheet's after the screen under it).
    static func containers() -> [UIViewController] {
        rt.nodes.values.sorted(by: { $0.id < $1.id }).compactMap { n in
            guard let vc = n.vc, vc.isViewLoaded, vc.view.window != nil else { return nil }
            return vc
        }
    }

    static func back() {
        for case let s as UINavigationController in containers().reversed() where s.viewControllers.count > 1 {
            s.popViewController(animated: true)
            return
        }
    }

    static func tab(_ title: String) {
        for case let t as UITabBarController in containers() {
            guard let vcs = t.viewControllers, let i = vcs.firstIndex(where: { $0.tabBarItem.title == title }) else { continue }
            t.selectedIndex = i
            t.delegate?.tabBarController?(t, didSelect: vcs[i])
            return
        }
    }

    static func dump() {
        var lines: [String] = []
        for n in rt.nodes.values.sorted(by: { $0.id < $1.id }) {
            let v = n.view
            guard v.window != nil, !v.isHidden, v.alpha > 0 else { continue }
            var hidden = false
            var p = v.superview
            while let x = p { if x.isHidden { hidden = true; break }; p = x.superview }
            if hidden { continue }
            let text: String?
            switch v {
            case let b as UIButton: text = b.configuration?.title ?? b.accessibilityLabel
            case let l as UILabel: text = l.text
            case let f as UITextField: text = f.text
            default: text = v.accessibilityLabel
            }
            if let text, !text.isEmpty { lines.append(text) }
        }
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("asuka-dump.txt")
        // Written whole, then moved: a reader never sees half of it.
        let tmp = url.appendingPathExtension("part")
        try? (lines.joined(separator: "\n") + "\n").write(to: tmp, atomically: false, encoding: .utf8)
        _ = try? FileManager.default.replaceItemAt(url, withItemAt: tmp)
    }

    static func find(_ text: String) -> AsukaNode? {
        rt.nodes.values.sorted(by: { $0.id < $1.id }).first { n in
            switch n.view {
            case let b as UIButton: return b.configuration?.title == text || b.accessibilityLabel == text
            case let l as UILabel: return l.text == text
            default: return n.view.accessibilityLabel == text
            }
        }
    }

    static func tap(_ text: String) {
        guard var n = find(text) else { print("asuka automation: nothing reads \(text)"); return }
        if n.view is UILabel, let p = n.parent { n = p } // a label inside a pressable view
        switch n.view {
        case let b as UIButton:
            b.sendActions(for: .primaryActionTriggered)
        case let s as UISwitch:
            s.setOn(!s.isOn, animated: true)
            s.sendActions(for: .valueChanged)
        default:
            rt.emit(n.id, .press) { AsukaEvents.press(n.id) }
        }
    }

    static func flip(_ i: Int) {
        let switches = rt.nodes.values.sorted(by: { $0.id < $1.id }).compactMap { $0.view as? UISwitch }
        guard i < switches.count else { return }
        let s = switches[i]
        s.setOn(!s.isOn, animated: true)
        s.sendActions(for: .valueChanged)
    }

    static func scroll(_ dy: CGFloat) {
        guard let s = rt.nodes.values.sorted(by: { $0.id < $1.id }).compactMap({ $0.view as? UIScrollView }).first(where: { !(($0 as? AsukaScrollView)?.horizontal ?? false) }) else { return }
        let maxY = max(0, s.contentSize.height - s.bounds.height + s.adjustedContentInset.bottom)
        s.setContentOffset(CGPoint(x: s.contentOffset.x, y: min(maxY, max(0, s.contentOffset.y + dy))), animated: false)
        s.layoutIfNeeded()
    }

    /// The field typed into: the one with the keyboard, else the first; a
    /// one-line field's UITextField or a multiline one's text view.
    static func field() -> UIView? {
        let fields = rt.nodes.values.sorted(by: { $0.id < $1.id }).compactMap { n -> UIView? in
            (n.view as? AsukaTextField) ?? (n.view as? AsukaTextView)
        }
        return fields.first(where: { $0.isFirstResponder }) ?? fields.first
    }

    static func type(_ text: String) {
        switch field() {
        case let f as AsukaTextField:
            f.becomeFirstResponder()
            f.text = text
            f.sendActions(for: .editingChanged)
        case let t as AsukaTextView:
            t.becomeFirstResponder()
            t.text = text
            rt.textViewDidChange(t)
        default:
            break
        }
    }

    static func submit() {
        guard let f = field() as? AsukaTextField else { return }
        _ = rt.textFieldShouldReturn(f)
    }
}
#endif
