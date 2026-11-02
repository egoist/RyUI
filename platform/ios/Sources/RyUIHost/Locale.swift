#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// What the environment says of the user's languages and regional formats.
/// Reading them isn't free (ICU, the preferences) and a screen reports its
/// environment as it lays out: they're kept until they change.
@MainActor struct RyUILanguages {
    var languages = ""
    var firstWeekday: UInt8 = 0
    var hourCycle: UInt8 = 0

    private static var kept: RyUILanguages?

    static var current: RyUILanguages {
        if let k = kept { return k }
        var l = RyUILanguages()
        l.languages = preferred().joined(separator: ",")
        let first = Calendar.current.firstWeekday
        l.firstWeekday = first >= 1 && first <= 7 ? UInt8(first) : 0
        l.hourCycle = hourCycle()
        kept = l
        return l
    }

    static func invalidate() { kept = nil }

    /// The app's own language (AppleLanguages of its defaults, which Settings'
    /// per-app language and SaveLocale write) first, then the system's.
    /// Locale.preferredLanguages alone won't do: with the app's own it says
    /// that one and nothing else, so it can't tell what following the system
    /// again would be.
    static func preferred() -> [String] {
        let list = own() + system()
        var seen = Set<String>()
        return list.filter { seen.insert($0).inserted }
    }

    /// AppleLanguages of the app's own defaults.
    static func own() -> [String] {
        guard let id = Bundle.main.bundleIdentifier else { return [] }
        return UserDefaults.standard.persistentDomain(forName: id)?[key] as? [String] ?? []
    }

    /// The system's languages, without the app's. A launch argument
    /// (-AppleLanguages) replaces them, as it does Locale.preferredLanguages.
    static func system() -> [String] {
        if let args = UserDefaults.standard.volatileDomain(forName: UserDefaults.argumentDomain)[key] as? [String] { return args }
        if let global = UserDefaults(suiteName: ".GlobalPreferences")?.stringArray(forKey: key), !global.isEmpty { return global }
        let mine = own()
        return Locale.preferredLanguages.filter { !mine.contains($0) }
    }

    static let key = "AppleLanguages"

    /// 12 or 24, by the user's setting: what the locale's hour pattern ("j")
    /// has become with it, without its quoted literals (Spanish's 'h').
    private static func hourCycle() -> UInt8 {
        guard let f = DateFormatter.dateFormat(fromTemplate: "j", options: 0, locale: Locale.current) else { return 0 }
        var quoted = false
        for c in f {
            if c == "'" { quoted.toggle(); continue }
            if quoted { continue }
            if c == "h" || c == "K" { return 12 }
            if c == "H" || c == "k" { return 24 }
        }
        return 0
    }
}

extension RyUIEnvironment {
    @MainActor mutating func setLanguages() {
        let l = RyUILanguages.current
        locale = Locale.current.identifier
        languages = l.languages
        firstWeekday = l.firstWeekday
        hourCycle = l.hourCycle
    }
}

@MainActor extension RyUINode {
    /// Whether the view runs right to left: its own direction, else the app's.
    var isRTL: Bool {
        switch direction {
        case .leftToRight: return false
        case .rightToLeft: return true
        case .inherit: return RyUIRuntime.shared.rtl
        }
    }

    /// Style.TextAlign, or the side the language starts on: End is the
    /// other one, whichever way the view runs.
    func applyTextAlign() {
        let a: NSTextAlignment
        switch RyUITextAlign(rawValue: textAlign) {
        case .center: a = .center
        case .end: a = isRTL ? .left : .right
        case .justify: a = .justified
        default: a = RyUIRuntime.shared.startAlignment(isRTL)
        }
        switch view {
        case let l as UILabel: l.textAlignment = a
        case let t as UITextField: t.textAlignment = a
        case let t as RyUITextView:
            t.textAlignment = a
            t.placeholderLabel.textAlignment = a
        default: break
        }
    }

    /// What keeps what it drew as the direction changes, once it's laid out:
    /// a slider's fill stays until its value changes, a selection's rectangles
    /// until it does.
    func redoAfterDirection() {
        switch view {
        case let s as UISlider:
            let v = s.value
            s.value = v == s.minimumValue ? s.maximumValue : s.minimumValue
            s.layoutIfNeeded()
            s.value = v
            s.layoutIfNeeded()
        case let a as RyUISelectionArea:
            a.input.geometryChanged()
        default:
            break
        }
    }

    /// What depends on the view's direction, and doesn't follow it by itself.
    func directionChanged() {
        applyTextAlign()
        switch view {
        case let r as RyUIRichTextView:
            r.base.rtl = isRTL
            r.base.writing = direction == .rightToLeft ? 1 : direction == .leftToRight ? 0 : -1
        case let s as RyUIScrollView:
            s.keepEnd()
        case let s as RyUISymbolView:
            s.refresh()
        case is RyUIDatePickerView:
            view.setNeedsLayout()
        default:
            break
        }
        list?.directionChanged()
    }
}

@MainActor extension RyUIRuntime {
    /// The direction UIKit gives the process, from its language and its
    /// bundle's localizations. It's fixed as the process starts.
    var systemRTL: Bool { UIApplication.shared.userInterfaceLayoutDirection == .rightToLeft }

    /// What a view of the app has: nothing where the app goes the way
    /// UIKit does by itself, which leaves the usual case as it always was.
    var appAttribute: UISemanticContentAttribute {
        rtl == systemRTL ? .unspecified : rtl ? .forceRightToLeft : .forceLeftToRight
    }

    /// TextAlign Start in a view going that way: natural where UIKit goes
    /// that way by itself. It doesn't fully follow the view's attribute,
    /// though: a text with no direction of its own, as a number, then aligns
    /// the process's way.
    func startAlignment(_ rtl: Bool) -> NSTextAlignment {
        rtl == systemRTL ? .natural : rtl ? .right : .left
    }

    /// A view was made: it goes the app's way before it's in a window, where
    /// the appearance would reach it, as it's measured and laid out before.
    func adoptDirection(_ n: RyUINode) {
        let to = appAttribute
        if to != .unspecified { setAttribute(n.view, from: .unspecified, to: to, forced: [:], redo: false) }
        if to != .unspecified || n.view is RyUIRichTextView { n.directionChanged() }
    }

    /// Sets v's direction, and that of the views UIKit made under it, which
    /// have what the app's had. Those the app forced stay as they are, and a
    /// view UIKit keeps one way (a video's controls, playback) stays too.
    func setAttribute(_ v: UIView, from: UISemanticContentAttribute, to: UISemanticContentAttribute,
                      forced: [ObjectIdentifier: UISemanticContentAttribute], redo: Bool) {
        var to = to
        if let f = forced[ObjectIdentifier(v)] {
            to = f
            if v.semanticContentAttribute != f { v.semanticContentAttribute = f }
        } else if v.semanticContentAttribute == from || v.semanticContentAttribute == .unspecified {
            v.semanticContentAttribute = to
        }
        if redo {
            v.setNeedsLayout()
            v.setNeedsDisplay()
        }
        for s in v.subviews { setAttribute(s, from: from, to: to, forced: forced, redo: redo) }
    }

    /// The Locale command: flags (bit 0 right to left), then the tag.
    func setLocale(_ args: UnsafeRawBufferPointer) {
        guard let flags = args.first else { return }
        // VoiceOver reads the app in its language, not the system's.
        let tag = String(decoding: args.dropFirst(), as: UTF8.self)
        UIApplication.shared.accessibilityLanguage = tag.isEmpty ? nil : tag
        setAppDirection(flags & 1 != 0)
    }

    /// Makes UIKit go that way, whatever the system's language: the views to
    /// come, by the appearance, and those there are, wherever they are.
    func setAppDirection(_ rtl: Bool) {
        guard rtl != self.rtl else { return }
        let from = appAttribute
        self.rtl = rtl
        let to = appAttribute
        UIView.appearance().semanticContentAttribute = to
        for n in nodes.values { n.directionChanged() }
        // Once Go has returned: a collection view asks for its cells again as
        // it changes direction, at once, and binding them calls into Go. This
        // is the same turn of the run loop: nothing's drawn in between.
        pendingDirection = { [self] in
            var forced: [ObjectIdentifier: UISemanticContentAttribute] = [:]
            for n in nodes.values where n.direction != .inherit {
                forced[ObjectIdentifier(n.view)] = n.direction == .rightToLeft ? .forceRightToLeft : .forceLeftToRight
            }
            UIView.performWithoutAnimation {
                for w in windows {
                    setAttribute(w, from: from, to: to, forced: forced, redo: true)
                    setWindowDirection(w)
                }
                if let v = controller?.viewIfLoaded { setAttribute(v, from: from, to: to, forced: forced, redo: true) }
                // Also those out of the windows: screens under others, unselected
                // tabs, the rows a Scroll took out, the cells waiting for reuse.
                for n in nodes.values {
                    if n.view.window == nil { setAttribute(n.view, from: from, to: to, forced: forced, redo: true) }
                    if let s = n.search?.controller.searchBar { setAttribute(s, from: from, to: to, forced: forced, redo: true) }
                }
                for w in windows { w.layoutIfNeeded() }
                for n in nodes.values { n.redoAfterDirection() }
            }
        }
    }

    /// The window's traits go the way of its views: UIKit picks some images
    /// by the traits (a navigation bar's back chevron) and not the attribute.
    func setWindowDirection(_ w: UIWindow) {
        if #available(iOS 17, *) {
            if rtl == systemRTL {
                w.traitOverrides.remove(UITraitLayoutDirection.self)
            } else {
                w.traitOverrides.layoutDirection = rtl ? .rightToLeft : .leftToRight
            }
        }
    }

    /// The LayoutDirection prop: the view goes that way, or the app's again.
    func setDirection(_ n: RyUINode, _ d: RyUILayoutDirection) {
        n.direction = d
        let to: UISemanticContentAttribute = d == .inherit ? appAttribute : d == .rightToLeft ? .forceRightToLeft : .forceLeftToRight
        setAttribute(n.view, from: n.view.semanticContentAttribute, to: to, forced: [:], redo: n.view.window != nil)
        n.directionChanged()
    }

    /// The SaveLocale command: AppleLanguages of the app's defaults, which
    /// Settings shows and UIKit reads at the next launch; empty follows the
    /// system again. The environment says it from now on.
    func saveLocale(_ tag: String) {
        let d = UserDefaults.standard
        if tag.isEmpty {
            d.removeObject(forKey: RyUILanguages.key)
        } else {
            d.set([tag], forKey: RyUILanguages.key)
        }
        localeChanged()
    }

    /// The languages or the formats changed: everything reports them again.
    func localeChanged() {
        RyUILanguages.invalidate()
        controller?.sendEnvironment()
        reportScreens()
    }

    var windows: [UIWindow] {
        var out: [UIWindow] = []
        for case let s as UIWindowScene in UIApplication.shared.connectedScenes { out += s.windows }
        return out
    }
}
#endif
