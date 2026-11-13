// Action menus (ops/actions.go): a view's Menu, a list cell's swipe
// actions, a screen's bar buttons and an alert's buttons. Written by
// hand, beside the generated decoder.

/// An item of an action menu: Action events carry its index, its number
/// depth first (a submenu or section before its own items).
public struct AsukaActionItem: Equatable {
    public var index: Int64
    public var role: Int32
    public var title: String
    public var icon: String
    public var flags: UInt8
    /// A submenu's or section's items (flags has submenu).
    public var items: [AsukaActionItem]

    public init(index: Int64 = 0, role: Int32 = 0, title: String, icon: String = "", flags: UInt8 = 0, items: [AsukaActionItem] = []) {
        self.index = index
        self.role = role
        self.title = title
        self.icon = icon
        self.flags = flags
        self.items = items
    }

    public static let disabledFlag: UInt8 = 2
    public static let checkedFlag: UInt8 = 4
    public static let submenuFlag: UInt8 = 16
    public static let inlineFlag: UInt8 = 32

    public var disabled: Bool { flags & Self.disabledFlag != 0 }
    public var checked: Bool { flags & Self.checkedFlag != 0 }
    /// It opens a submenu (or, inline, is a section).
    public var submenu: Bool { flags & Self.submenuFlag != 0 }
    /// A section of its menu, shown in place.
    public var inline: Bool { submenu && flags & Self.inlineFlag != 0 }
}

public enum AsukaActions {
    /// A Screen's Action events (ops.ScreenToolbarActions,
    /// ops.ScreenPrimaryAction): its Toolbar's carry toolbarBase + the
    /// item's number, its Primary's primaryBase + the number; its bar
    /// buttons' the number alone.
    public static let toolbarBase: Int64 = 1 << 32
    public static let primaryBase: Int64 = 2 << 32

    /// A menu's items (ops.DecodeActions): u32 count, per item u8 role, a
    /// u32-length title, a u32-length icon and u8 flags, then a submenu's
    /// items; numbered depth first. What's cut short is left out.
    public static func decode(_ b: UnsafeRawBufferPointer) -> [AsukaActionItem] {
        var i = 0
        var next: Int64 = 0
        var cut = false
        func u8() -> UInt8? {
            guard i < b.count else { cut = true; return nil }
            i += 1
            return b[i - 1]
        }
        func u32() -> Int? {
            guard i + 4 <= b.count else { cut = true; return nil }
            let v = UInt32(b[i]) | UInt32(b[i + 1]) << 8 | UInt32(b[i + 2]) << 16 | UInt32(b[i + 3]) << 24
            i += 4
            return Int(v)
        }
        func string() -> String? {
            guard let n = u32(), n <= b.count - i else { cut = true; return nil }
            let s = String(decoding: UnsafeRawBufferPointer(rebasing: b[i..<i + n]), as: UTF8.self)
            i += n
            return s
        }
        func menu(_ depth: Int) -> [AsukaActionItem] {
            guard let n = u32() else { return [] }
            var out: [AsukaActionItem] = []
            for _ in 0..<min(n, b.count) {
                guard let role = u8(), let title = string(), let icon = string(), let flags = u8() else { break }
                var it = AsukaActionItem(index: next, role: Int32(role), title: title, icon: icon, flags: flags)
                next += 1
                if it.submenu {
                    if depth >= 16 { cut = true; break }
                    it.items = menu(depth + 1)
                }
                out.append(it)
                if cut { break }
            }
            return out
        }
        return menu(0)
    }
}
