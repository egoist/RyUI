// A SplitView's sidebar (ops/sidebar.go): Split's Items. Written by hand,
// beside the generated decoder.

/// A destination in a sidebar.
public struct AsukaSidebarItem: Equatable {
    public var title: String
    public var icon: String
    /// Shown at the row's trailing end (empty: none).
    public var badge: String

    public init(title: String, icon: String = "", badge: String = "") {
        self.title = title
        self.icon = icon
        self.badge = badge
    }
}

/// A titled group of a sidebar's destinations.
public struct AsukaSidebarSection: Equatable {
    public var title: String
    public var items: [AsukaSidebarItem]

    public init(title: String, items: [AsukaSidebarItem]) {
        self.title = title
        self.items = items
    }
}

public enum AsukaSidebar {
    /// The sections (ops.DecodeSidebar): u32 count, per section a
    /// u32-length title and u32 item count, per item a u32-length title,
    /// icon and badge. What's cut short is left out, items whole.
    public static func decode(_ b: UnsafeRawBufferPointer) -> [AsukaSidebarSection] {
        var i = 0
        func u32() -> Int? {
            guard i + 4 <= b.count else { return nil }
            let v = UInt32(b[i]) | UInt32(b[i + 1]) << 8 | UInt32(b[i + 2]) << 16 | UInt32(b[i + 3]) << 24
            i += 4
            return Int(v)
        }
        func string() -> String? {
            guard let n = u32(), n <= b.count - i else { i = b.count; return nil }
            let s = String(decoding: UnsafeRawBufferPointer(rebasing: b[i..<i + n]), as: UTF8.self)
            i += n
            return s
        }
        guard let count = u32() else { return [] }
        var out: [AsukaSidebarSection] = []
        for _ in 0..<min(count, b.count) {
            guard let title = string(), let n = u32() else { break }
            var items: [AsukaSidebarItem] = []
            var cut = false
            for _ in 0..<min(n, b.count) {
                guard let t = string(), let icon = string(), let badge = string() else { cut = true; break }
                items.append(.init(title: t, icon: icon, badge: badge))
            }
            out.append(.init(title: title, items: items))
            if cut { break }
        }
        return out
    }

    /// Whether a and b have as many sections, each as many items
    /// (ops.SameSidebarShape): the rows are updated in place.
    public static func sameShape(_ a: [AsukaSidebarSection], _ b: [AsukaSidebarSection]) -> Bool {
        a.count == b.count && zip(a, b).allSatisfy { $0.items.count == $1.items.count }
    }
}
